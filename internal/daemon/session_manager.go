package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/protocol"
)

type sessionStatus string

const (
	sessionIdle       sessionStatus = "idle"
	sessionRunning    sessionStatus = "running"
	sessionWaiting    sessionStatus = "waiting"
	sessionCompacting sessionStatus = "compacting"
)

type sessionRuntime struct {
	// stateMu 保护 idle 阶段会重载或替换 agent working state 的操作，避免 Handoff attach/update 与 run 起点交叉。
	stateMu *sync.Mutex
	// stateOps 记录正在排队或执行的 stateMu 操作；beginRun 用它做原子 busy 判断。
	stateOps    int
	agent       *agent.Agent
	status      sessionStatus
	clients     map[string]bool
	runOwner    string
	phase       protocol.AgentRunPhase
	assistant   strings.Builder
	reasoning   strings.Builder
	waitingType protocol.RunWaitingType
}

type sessionManager struct {
	root                  *agent.Agent
	store                 *memory.SessionStore
	mu                    sync.RWMutex
	legacyModelMu         sync.Mutex
	runtime               map[string]*sessionRuntime
	attached              map[string]string
	creating              map[string]string
	deleting              map[string]bool
	deleteVersion         map[string]uint64
	runtimeUnloadVersion  map[string]uint64
	runtimeUnloadDelay    time.Duration
	beforeAttachStateLoad func()
}

func newSessionManager(root *agent.Agent, store *memory.SessionStore) *sessionManager {
	return &sessionManager{root: root, store: store, runtime: map[string]*sessionRuntime{}, attached: map[string]string{}, creating: map[string]string{}, deleting: map[string]bool{}, deleteVersion: map[string]uint64{}, runtimeUnloadVersion: map[string]uint64{}, runtimeUnloadDelay: defaultRuntimeUnloadDelay}
}

func (m *sessionManager) list(ctx context.Context, activeOnly bool) ([]protocol.SessionInfo, error) {
	metas, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.SessionInfo, 0, len(metas))
	for _, meta := range metas {
		info := m.infoFor(meta)
		active := info.ClientCount > 0 || info.Status != protocol.SessionStatusIdle
		if activeOnly && !active {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

func (m *sessionManager) sessionInfo(ctx context.Context, sessionID string) (protocol.SessionInfo, bool) {
	meta, err := m.store.Get(ctx, sessionID)
	if err != nil || meta == nil {
		return protocol.SessionInfo{}, false
	}
	return m.infoFor(*meta), true
}

func (m *sessionManager) create(ctx context.Context, connID, cwd, title string) (protocol.SessionSnapshot, error) {
	// 新会话必须先绑定当前默认模型，再写入 sessions；否则失败时不能留下无法运行的孤立记录。
	cfg := m.root.Config()
	if cfg == nil {
		return protocol.SessionSnapshot{}, fmt.Errorf("no default model configured")
	}
	modelRef := strings.TrimSpace(cfg.ActiveModel)
	if modelRef == "" {
		return protocol.SessionSnapshot{}, fmt.Errorf("no default model configured")
	}
	if _, err := m.root.BindModel(modelRef); err != nil {
		return protocol.SessionSnapshot{}, err
	}

	id := uuid.New().String()
	cwd = canonicalCWD(cwd)
	meta := memory.SessionMeta{ID: id, Title: title, CWD: cwd, ModelRef: modelRef, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	// 创建门闩必须先于持久化记录建立，避免其他客户端在记录刚落库时 attach 半成品会话。
	m.mu.Lock()
	m.creating[id] = connID
	m.mu.Unlock()
	if err := m.store.Create(ctx, meta); err != nil {
		m.mu.Lock()
		delete(m.creating, id)
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, err
	}
	snapshot, err := m.attach(ctx, connID, id, false)
	if err == nil {
		return snapshot, nil
	}
	// create 对调用方是原子语义：首次 attach 或状态加载失败时，不能留下无法使用的新会话。
	m.mu.Lock()
	delete(m.creating, id)
	m.deleting[id] = true
	m.mu.Unlock()
	if cleanupErr := m.deletePersistedSession(context.Background(), id); cleanupErr != nil {
		logging.Error("session", "cleanup_failed_create", cleanupErr, logging.Event{"session_id": id})
	} else {
		m.mu.Lock()
		m.deleteVersion[id]++
		m.invalidateRuntimeUnloadNoLock(id)
		delete(m.runtime, id)
		delete(m.runtimeUnloadVersion, id)
		m.mu.Unlock()
		m.removeSessionAttachments(id)
	}
	m.mu.Lock()
	delete(m.deleting, id)
	m.mu.Unlock()
	return protocol.SessionSnapshot{}, err
}

func (m *sessionManager) attach(ctx context.Context, connID, sessionID string, requireActive bool) (protocol.SessionSnapshot, error) {
	// 在读取持久化元数据前捕获删除代际；后续锁内校验确保读取期间完成的删除
	// 不会让陈旧 meta 重建 runtime。
	m.mu.RLock()
	deleteVersion := m.deleteVersion[sessionID]
	m.mu.RUnlock()
	meta, err := m.store.Get(ctx, sessionID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if meta == nil {
		return protocol.SessionSnapshot{}, fmt.Errorf("session not found")
	}
	if strings.TrimSpace(meta.ModelRef) == "" {
		meta, err = m.materializeLegacyModelRef(ctx, sessionID)
		if err != nil {
			return protocol.SessionSnapshot{}, err
		}
	}

	// 附着关系只在状态读取成功后提交。这样 create 的内部读取失败时，原有附着不被替换，
	// 新会话随后删除也不会让连接指向已经移除的 runtime。
	m.mu.Lock()
	creatingOwner := m.creating[sessionID]
	if creatingOwner != "" && creatingOwner != connID {
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session is being created")
	}
	if m.deleting[sessionID] || m.deleteVersion[sessionID] != deleteVersion {
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session not found")
	}
	rt := m.runtime[sessionID]
	if requireActive && (rt == nil || (len(rt.clients) == 0 && rt.status == sessionIdle)) {
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session is no longer active")
	}
	if rt == nil {
		rt = m.ensureRuntimeNoLock(meta)
	}
	if rt == nil {
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session model is not configured")
	}
	m.invalidateRuntimeUnloadNoLock(sessionID)
	// 即使只读取 active 会话快照，也保留 runtime，避免读取和提交附着之间被删除。
	rt.stateOps++
	loadState := rt.status == sessionIdle
	m.mu.Unlock()

	if m.beforeAttachStateLoad != nil {
		m.beforeAttachStateLoad()
	}
	var snap agent.SessionSnapshot
	if loadState {
		rt.stateMu.Lock()
		snap, err = rt.agent.LoadSessionState(ctx)
		rt.stateMu.Unlock()
	} else {
		snap, err = rt.agent.SnapshotState(ctx)
	}
	if err != nil {
		m.finishStateOp(sessionID)
		return protocol.SessionSnapshot{}, err
	}

	m.mu.Lock()
	if m.deleting[sessionID] || m.deleteVersion[sessionID] != deleteVersion || m.runtime[sessionID] != rt {
		m.finishStateOpNoLock(sessionID, rt)
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session not found")
	}
	if requireActive && len(rt.clients) == 0 && rt.status == sessionIdle {
		m.finishStateOpNoLock(sessionID, rt)
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session is no longer active")
	}
	if creatingOwner := m.creating[sessionID]; creatingOwner != "" && creatingOwner != connID {
		m.finishStateOpNoLock(sessionID, rt)
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session is being created")
	}
	var oldID string
	var oldMaybeDelete bool
	if old := m.attached[connID]; old != "" && old != sessionID {
		// 切换 session 必须复用 detach 的客户端计数语义，避免旧 session 在其他 TUI 中残留为 active。
		oldID, oldMaybeDelete = m.detachConnNoLock(connID)
	}
	rt.clients[connID] = true
	m.attached[connID] = sessionID
	delete(m.creating, sessionID)
	if rt.stateOps > 0 {
		rt.stateOps--
	}
	m.mu.Unlock()

	if oldID != "" {
		m.handleDetachedSession(oldID, oldMaybeDelete)
	}
	_ = m.store.TouchAttached(ctx, sessionID)
	return m.snapshotForConn(connID, *meta, rt, snap), nil
}

// materializeLegacyModelRef 将升级前没有模型选择的 session 迁移为显式选择。
// 该路径只会在每个旧会话的首次 attach 命中，独立互斥避免阻塞正常 session 生命周期。
func (m *sessionManager) materializeLegacyModelRef(ctx context.Context, sessionID string) (*memory.SessionMeta, error) {
	m.legacyModelMu.Lock()
	defer m.legacyModelMu.Unlock()

	meta, err := m.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, fmt.Errorf("session not found")
	}
	if strings.TrimSpace(meta.ModelRef) != "" {
		return meta, nil
	}
	cfg := m.root.Config()
	if cfg == nil || strings.TrimSpace(cfg.ActiveModel) == "" {
		return nil, fmt.Errorf("no default model configured")
	}
	modelRef := strings.TrimSpace(cfg.ActiveModel)
	if _, err := m.root.BindModel(modelRef); err != nil {
		return nil, err
	}
	materialized, err := m.store.MaterializeModelRefIfEmpty(ctx, meta.ID, modelRef)
	if err != nil {
		return nil, err
	}
	if materialized {
		meta.ModelRef = modelRef
		return meta, nil
	}
	meta, err = m.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if meta == nil || strings.TrimSpace(meta.ModelRef) == "" {
		return nil, fmt.Errorf("session model is not configured")
	}
	return meta, nil
}

func (m *sessionManager) detach(connID string) string {
	m.mu.Lock()
	id, shouldMaybeDelete := m.detachConnNoLock(connID)
	m.mu.Unlock()
	m.handleDetachedSession(id, shouldMaybeDelete)
	return id
}

func (m *sessionManager) currentSessionID(connID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.attached[connID]
}

func (m *sessionManager) detachConnNoLock(connID string) (string, bool) {
	id := m.attached[connID]
	delete(m.attached, connID)
	if id == "" {
		return "", false
	}
	rt := m.runtime[id]
	if rt == nil {
		return id, false
	}
	delete(rt.clients, connID)
	return id, len(rt.clients) == 0 && rt.status == sessionIdle
}

func (m *sessionManager) attachedSession(connID string) (*sessionRuntime, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id := m.attached[connID]
	if id == "" {
		return nil, "", fmt.Errorf("session_required")
	}
	rt := m.runtime[id]
	if rt == nil {
		return nil, "", fmt.Errorf("session not loaded")
	}
	return rt, id, nil
}

func (m *sessionManager) sinksForSession(d *Daemon, sessionID string) []protocol.EventSink {
	connIDs := m.connIDsForSession(sessionID)
	sinks := make([]protocol.EventSink, 0, len(connIDs))
	for _, connID := range connIDs {
		if sink := d.sinkFor(connID, nil); sink != nil {
			sinks = append(sinks, sink)
		}
	}
	return sinks
}

func (m *sessionManager) connIDsForSession(sessionID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rt := m.runtime[sessionID]
	if rt == nil {
		return nil
	}
	connIDs := make([]string, 0, len(rt.clients))
	for connID := range rt.clients {
		connIDs = append(connIDs, connID)
	}
	return connIDs
}

func (m *sessionManager) runOwner(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt := m.runtime[sessionID]; rt != nil {
		return rt.runOwner
	}
	return ""
}

func (m *sessionManager) setRunOwner(sessionID, connID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.runtime[sessionID]
	if rt == nil || !rt.clients[connID] {
		return false
	}
	rt.runOwner = connID
	return true
}

func (m *sessionManager) update(ctx context.Context, connID string, params protocol.SessionUpdateParams) (protocol.SessionSnapshot, error) {
	updateTitle := params.Title != nil
	updateModelRef := params.ModelRef != nil
	title := ""
	modelRef := ""
	if updateTitle {
		title = *params.Title
	}
	if updateModelRef {
		modelRef = strings.TrimSpace(*params.ModelRef)
	}

	m.mu.RLock()
	rt := m.runtime[params.SessionID]
	attached := m.attached[connID] == params.SessionID
	busy := rt != nil && (rt.status != sessionIdle || rt.stateOps > 0)
	m.mu.RUnlock()
	if !attached {
		return protocol.SessionSnapshot{}, fmt.Errorf("session_required")
	}
	if rt == nil {
		return protocol.SessionSnapshot{}, fmt.Errorf("session not loaded")
	}

	// 纯标题更新不读取或重载 working state，运行期间也可安全执行，避免自动命名与首条消息争抢状态操作。
	// 请求在上方确认连接已附着目标会话后即被受理；其后的 detach 不撤销已受理的元数据写入。
	if updateTitle && !updateModelRef {
		if err := m.store.UpdateMetadata(ctx, params.SessionID, title, true, "", false); err != nil {
			return protocol.SessionSnapshot{}, err
		}
		meta, err := m.store.Get(ctx, params.SessionID)
		if err != nil {
			return protocol.SessionSnapshot{}, err
		}
		if meta == nil {
			return protocol.SessionSnapshot{}, fmt.Errorf("session not found")
		}
		return protocol.SessionSnapshot{Session: m.infoFor(*meta)}, nil
	}
	if busy {
		return protocol.SessionSnapshot{}, fmt.Errorf("session_busy")
	}
	m.mu.Lock()
	if current := m.runtime[params.SessionID]; current != rt || m.attached[connID] != params.SessionID || rt.status != sessionIdle || rt.stateOps > 0 {
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session_busy")
	}
	rt.stateOps++
	m.mu.Unlock()
	defer m.finishStateOp(params.SessionID)

	// 同时修改标题和模型时，先完成模型绑定校验，避免标题先落库而模型被拒绝。
	if updateModelRef {
		if modelRef == "" {
			return protocol.SessionSnapshot{}, fmt.Errorf("model_ref is required")
		}
		if _, err := m.root.BindModel(modelRef); err != nil {
			return protocol.SessionSnapshot{}, err
		}
	}
	// 所有可失败读取都在提交前完成，避免更新已生效却因返回快照读取失败而向调用方报告错误。
	meta, err := m.store.Get(ctx, params.SessionID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if meta == nil {
		return protocol.SessionSnapshot{}, fmt.Errorf("session not found")
	}
	rt.stateMu.Lock()
	snap, err := rt.agent.SnapshotState(ctx)
	rt.stateMu.Unlock()
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	// 单一事务提交所有元数据；成功后才切换运行时 Agent 的模型引用。
	if err := m.store.UpdateMetadata(ctx, params.SessionID, title, updateTitle, modelRef, updateModelRef); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if updateTitle {
		meta.Title = strings.TrimSpace(title)
	}
	if updateModelRef {
		meta.ModelRef = modelRef
		rt.agent.SetModelRef(modelRef)
	}
	return m.snapshotForConn(connID, *meta, rt, snap), nil
}

func (m *sessionManager) ensureAttached(connID string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.attached[connID] == "" {
		return fmt.Errorf("session_required")
	}
	return nil
}

func (m *sessionManager) isClientAttached(connID, sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.attached[connID] == sessionID
}

func (m *sessionManager) ensureRunOwner(connID string) (*sessionRuntime, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id := m.attached[connID]
	if id == "" {
		return nil, "", fmt.Errorf("session_required")
	}
	rt := m.runtime[id]
	if rt == nil {
		return nil, "", fmt.Errorf("session not loaded")
	}
	if rt.runOwner != "" && rt.runOwner != connID {
		return nil, "", fmt.Errorf("session_busy")
	}
	return rt, id, nil
}

func (m *sessionManager) beginRun(connID string) (*sessionRuntime, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.attached[connID]
	if id == "" {
		return nil, "", fmt.Errorf("session_required")
	}
	rt := m.runtime[id]
	if rt == nil {
		return nil, "", fmt.Errorf("session not loaded")
	}
	if rt.status != sessionIdle || rt.stateOps > 0 {
		return nil, "", fmt.Errorf("session_busy")
	}
	rt.runOwner = connID
	m.invalidateRuntimeUnloadNoLock(id)
	rt.phase = protocol.AgentRunPhaseModel
	rt.status = sessionRunning
	rt.waitingType = ""
	rt.assistant.Reset()
	rt.reasoning.Reset()
	return rt, id, nil
}

func (m *sessionManager) finishStateOp(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.runtime[sessionID]; rt != nil && rt.stateOps > 0 {
		m.finishStateOpNoLock(sessionID, rt)
	}
}

func (m *sessionManager) setStatus(sessionID string, status sessionStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.runtime[sessionID]; rt != nil {
		rt.status = status
		if status == sessionIdle {
			rt.runOwner = ""
			rt.waitingType = ""
			rt.phase = ""
			rt.assistant.Reset()
			rt.reasoning.Reset()
			m.scheduleRuntimeUnloadNoLock(sessionID)
		} else {
			m.invalidateRuntimeUnloadNoLock(sessionID)
		}
	}
}

func (m *sessionManager) setPhase(sessionID string, phase protocol.AgentRunPhase) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.runtime[sessionID]; rt != nil {
		rt.phase = phase
	}
}

func (m *sessionManager) setWaiting(sessionID string, waitingType protocol.RunWaitingType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.runtime[sessionID]; rt != nil {
		m.invalidateRuntimeUnloadNoLock(sessionID)
		rt.status = sessionWaiting
		rt.waitingType = waitingType
	}
}

func (m *sessionManager) appendStream(sessionID, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.runtime[sessionID]; rt != nil {
		rt.assistant.WriteString(content)
	}
}

func (m *sessionManager) appendReasoning(sessionID, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.runtime[sessionID]; rt != nil {
		rt.reasoning.WriteString(content)
	}
}

func (m *sessionManager) ensureRuntimeNoLock(meta *memory.SessionMeta) *sessionRuntime {
	if rt := m.runtime[meta.ID]; rt != nil {
		return rt
	}
	// 旧会话可能因 schema 升级保留空 model_ref；不得在 attach 时回退到全局默认模型，
	// 否则修改新会话默认值会静默改变历史会话。用户需显式选择模型后再运行。
	rt := &sessionRuntime{stateMu: &sync.Mutex{}, agent: m.root.NewSessionAgent(meta.ID, meta.CWD, meta.ModelRef), status: sessionIdle, clients: map[string]bool{}}
	m.runtime[meta.ID] = rt
	return rt
}

func (m *sessionManager) infoFor(meta memory.SessionMeta) protocol.SessionInfo {
	m.mu.RLock()
	rt := m.runtime[meta.ID]
	clientCount := 0
	status := sessionIdle
	if rt != nil {
		clientCount = len(rt.clients)
		status = rt.status
	}
	m.mu.RUnlock()
	return protocol.SessionInfo{ID: meta.ID, Title: meta.Title, CWD: meta.CWD, ModelRef: meta.ModelRef, MessageCount: meta.MessageCount, CreatedAt: formatTime(meta.CreatedAt), UpdatedAt: formatTime(meta.UpdatedAt), LastAttachedAt: formatTime(meta.LastAttachedAt), Status: protocol.SessionStatus(status), ClientCount: clientCount}
}

func (m *sessionManager) snapshotForConn(connID string, meta memory.SessionMeta, rt *sessionRuntime, snap agent.SessionSnapshot) protocol.SessionSnapshot {
	messages := make([]protocol.SnapshotMessage, 0, len(snap.Messages))
	for _, msg := range snap.Messages {
		text := strings.TrimSpace(msg.Text())
		if text == "" {
			continue
		}
		messages = append(messages, protocol.SnapshotMessage{Role: string(msg.Role), Content: text})
	}
	out := protocol.SessionSnapshot{Session: m.infoFor(meta), Messages: messages, Compacted: snap.Compacted}
	if summary := toolSummaryPayload(snap.ToolSummary); summary != nil {
		out.ToolSummary = summary
	}
	m.mu.RLock()
	if rt != nil && rt.status != sessionIdle {
		out.CurrentRun = &protocol.CurrentRunView{Status: protocol.SessionStatus(rt.status), Phase: rt.phase, AssistantBuffer: rt.assistant.String(), ReasoningBuffer: rt.reasoning.String(), WaitingType: rt.waitingType, CanControl: rt.runOwner != "" && rt.runOwner == connID}
	}
	m.mu.RUnlock()
	return out
}

func (m *sessionManager) cancelAllRuns() {
	m.mu.RLock()
	agents := make([]*agent.Agent, 0, len(m.runtime))
	for _, rt := range m.runtime {
		if rt != nil && rt.agent != nil {
			agents = append(agents, rt.agent)
		}
	}
	m.mu.RUnlock()
	for _, ag := range agents {
		ag.CancelCurrentRun()
	}
}
