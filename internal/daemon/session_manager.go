package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/agent"
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
	root     *agent.Agent
	store    *memory.SessionStore
	states   *memory.SessionStateStore
	mu       sync.RWMutex
	runtime  map[string]*sessionRuntime
	attached map[string]string
	deleting map[string]bool
}

func newSessionManager(root *agent.Agent, store *memory.SessionStore, states *memory.SessionStateStore) *sessionManager {
	return &sessionManager{root: root, store: store, states: states, runtime: map[string]*sessionRuntime{}, attached: map[string]string{}, deleting: map[string]bool{}}
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
	id := uuid.New().String()
	cwd = canonicalCWD(cwd)
	meta := memory.SessionMeta{ID: id, Title: title, CWD: cwd, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := m.store.Create(ctx, meta); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	return m.attach(ctx, connID, id, false)
}

func (m *sessionManager) attach(ctx context.Context, connID, sessionID string, requireActive bool) (protocol.SessionSnapshot, error) {
	meta, err := m.store.Get(ctx, sessionID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if meta == nil {
		return protocol.SessionSnapshot{}, fmt.Errorf("session not found")
	}
	m.mu.RLock()
	deleting := m.deleting[sessionID]
	m.mu.RUnlock()
	if deleting {
		return protocol.SessionSnapshot{}, fmt.Errorf("session is being deleted")
	}
	rt := m.ensureRuntimeLocked(meta)
	loadState := false
	m.mu.Lock()
	if m.deleting[sessionID] {
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session is being deleted")
	}
	if requireActive && len(rt.clients) == 0 && rt.status == sessionIdle {
		m.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("session is no longer active")
	}
	var oldID string
	var oldMaybeDelete bool
	if old := m.attached[connID]; old != "" && old != sessionID {
		// 切换 session 必须复用 detach 的客户端计数语义，避免旧 session 在其他 TUI 中残留为 active。
		oldID, oldMaybeDelete = m.detachConnNoLock(connID)
	}
	rt.clients[connID] = true
	m.attached[connID] = sessionID
	active := rt.status != sessionIdle
	if !active {
		// idle attach 会重载会话快照；必须阻塞 beginRun，避免刚开始 run 就被清空 working memory。
		rt.stateOps++
		loadState = true
	}
	m.mu.Unlock()
	if oldID != "" && oldMaybeDelete {
		if oldMeta, err := m.store.Get(context.Background(), oldID); err == nil && oldMeta != nil && oldMeta.MessageCount == 0 {
			m.deleteInactive(context.Background(), oldID)
		}
	}
	_ = m.store.TouchAttached(ctx, sessionID)
	var snap agent.SessionSnapshot
	if loadState {
		rt.stateMu.Lock()
		snap, err = rt.agent.LoadSessionState(ctx)
		rt.stateMu.Unlock()
		m.finishStateOp(sessionID)
	} else {
		snap, err = rt.agent.SnapshotState(ctx)
	}
	if err != nil {
		m.detach(connID)
		return protocol.SessionSnapshot{}, err
	}
	return m.snapshotForConn(connID, *meta, rt, snap), nil
}

func (m *sessionManager) detach(connID string) string {
	m.mu.Lock()
	id, shouldMaybeDelete := m.detachConnNoLock(connID)
	m.mu.Unlock()
	if id != "" && shouldMaybeDelete {
		if meta, err := m.store.Get(context.Background(), id); err == nil && meta != nil && meta.MessageCount == 0 {
			m.deleteInactive(context.Background(), id)
		}
	}
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
	title := ""
	if params.Title != nil {
		title = *params.Title
	}
	m.mu.Lock()
	rt := m.runtime[params.SessionID]
	attached := m.attached[connID] == params.SessionID
	busy := rt != nil && (rt.status != sessionIdle || rt.stateOps > 0)
	if attached && !busy && rt != nil {
		rt.stateOps++
	}
	m.mu.Unlock()
	if !attached {
		return protocol.SessionSnapshot{}, fmt.Errorf("session_required")
	}
	if busy {
		return protocol.SessionSnapshot{}, fmt.Errorf("session_busy")
	}
	if updateTitle {
		if err := m.store.UpdateTitle(ctx, params.SessionID, title); err != nil {
			if rt != nil {
				m.finishStateOp(params.SessionID)
			}
			return protocol.SessionSnapshot{}, err
		}
	}
	meta, err := m.store.Get(ctx, params.SessionID)
	if err != nil {
		if rt != nil {
			m.finishStateOp(params.SessionID)
		}
		return protocol.SessionSnapshot{}, err
	}
	if meta == nil {
		if rt != nil {
			m.finishStateOp(params.SessionID)
		}
		return protocol.SessionSnapshot{}, fmt.Errorf("session not found")
	}
	if rt == nil {
		m.mu.Lock()
		rt = m.ensureRuntimeNoLock(meta)
		rt.stateOps++
		m.mu.Unlock()
	}
	rt.stateMu.Lock()
	snap, err := rt.agent.LoadSessionState(ctx)
	rt.stateMu.Unlock()
	m.finishStateOp(params.SessionID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	return m.snapshotForConn(connID, *meta, rt, snap), nil
}

func (m *sessionManager) delete(ctx context.Context, connID, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_required")
	}
	m.mu.Lock()
	currentID := m.attached[connID]
	if currentID == sessionID {
		m.mu.Unlock()
		return fmt.Errorf("cannot delete current session")
	}
	rt := m.runtime[sessionID]
	clientCount := 0
	status := sessionIdle
	stateOps := 0
	if rt != nil {
		clientCount = len(rt.clients)
		status = rt.status
		stateOps = rt.stateOps
	}
	deleteInProgress := m.deleting[sessionID]
	canDelete := status == sessionIdle && stateOps == 0 && clientCount == 0 && !deleteInProgress
	if canDelete {
		m.deleting[sessionID] = true
	}
	if canDelete && rt != nil {
		rt.stateOps++
	}
	m.mu.Unlock()
	if canDelete {
		defer func() {
			m.mu.Lock()
			delete(m.deleting, sessionID)
			m.mu.Unlock()
		}()
	}
	if deleteInProgress {
		return fmt.Errorf("session is busy")
	}
	if status != sessionIdle || stateOps > 0 {
		return fmt.Errorf("session is busy")
	}
	if clientCount > 0 {
		return fmt.Errorf("session has attached clients")
	}
	meta, err := m.store.Get(ctx, sessionID)
	if err != nil {
		if rt != nil {
			m.finishStateOp(sessionID)
		}
		return err
	}
	if meta == nil {
		if rt != nil {
			m.finishStateOp(sessionID)
		}
		return fmt.Errorf("session not found")
	}
	if err := m.states.Delete(ctx, sessionID); err != nil {
		if rt != nil {
			m.finishStateOp(sessionID)
		}
		return err
	}
	if err := m.store.Delete(ctx, sessionID); err != nil {
		if rt != nil {
			m.finishStateOp(sessionID)
		}
		return err
	}
	_ = os.RemoveAll(filepath.Join(m.root.Config().AttachmentsDir(), sessionID))
	m.mu.Lock()
	delete(m.runtime, sessionID)
	m.mu.Unlock()
	return nil
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
		rt.stateOps--
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

func (m *sessionManager) ensureRuntimeLocked(meta *memory.SessionMeta) *sessionRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureRuntimeNoLock(meta)
}

func (m *sessionManager) ensureRuntimeNoLock(meta *memory.SessionMeta) *sessionRuntime {
	if rt := m.runtime[meta.ID]; rt != nil {
		return rt
	}
	rt := &sessionRuntime{stateMu: &sync.Mutex{}, agent: m.root.NewSessionAgent(meta.ID, meta.CWD), status: sessionIdle, clients: map[string]bool{}}
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
	return protocol.SessionInfo{ID: meta.ID, Title: meta.Title, CWD: meta.CWD, MessageCount: meta.MessageCount, CreatedAt: formatTime(meta.CreatedAt), UpdatedAt: formatTime(meta.UpdatedAt), LastAttachedAt: formatTime(meta.LastAttachedAt), Status: protocol.SessionStatus(status), ClientCount: clientCount}
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

func (m *sessionManager) pruneInactive(ctx context.Context, age time.Duration) {
	if m == nil || m.store == nil || age <= 0 {
		return
	}
	items, err := m.store.List(ctx)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-age)
	for _, item := range items {
		if item.MessageCount <= 0 {
			m.deleteInactive(ctx, item.ID)
			continue
		}
		if item.UpdatedAt.IsZero() || item.UpdatedAt.After(cutoff) {
			continue
		}
		m.deleteInactive(ctx, item.ID)
	}
}

func (m *sessionManager) deleteInactive(ctx context.Context, sessionID string) {
	m.mu.Lock()
	if m.deleting[sessionID] {
		m.mu.Unlock()
		return
	}
	rt := m.runtime[sessionID]
	active := rt != nil && (len(rt.clients) > 0 || rt.status != sessionIdle || rt.stateOps > 0)
	if active {
		m.mu.Unlock()
		return
	}
	// prune 与 attach 共用 deleting 标记，避免后台清理删除正在 attach 的 session 存储。
	m.deleting[sessionID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.deleting, sessionID)
		delete(m.runtime, sessionID)
		m.mu.Unlock()
	}()
	_ = m.states.Delete(ctx, sessionID)
	_ = m.store.Delete(ctx, sessionID)
	_ = os.RemoveAll(filepath.Join(m.root.Config().AttachmentsDir(), sessionID))
}

func canonicalCWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return cwd
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
