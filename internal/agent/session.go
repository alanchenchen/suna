package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/skill"
)

// SessionSnapshot 是 daemon attach/create 后给 UI 渲染用的会话快照。
type SessionSnapshot struct {
	Messages  []memory.Message
	Compacted bool
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func canonicalCWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd = mustGetwd()
	}
	abs, err := filepath.Abs(cwd)
	if err == nil {
		cwd = abs
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return cwd
}

func (a *Agent) NewSessionAgentWithProjectSkills(sessionID, cwd, modelRef string, projectSkills *skill.Catalog) *Agent {
	cwd = canonicalCWD(cwd)
	root := a.root()
	root.configMu.RLock()
	cfg := root.cfg
	router := root.router
	tools := root.tools
	usage := root.usage
	sessionStore := root.sessionStore
	stateStore := root.stateStore
	memories := root.memories
	compressor := root.compressor
	calibrator := root.calibrator
	prompts := root.prompts
	store := root.store
	skills := root.skills
	mcp := root.mcp
	extractQueue := root.extractQueue
	extractWorker := root.extractWorker
	root.configMu.RUnlock()
	s := &Agent{
		runtime:       root,
		cfg:           cfg,
		router:        router,
		tools:         tools,
		usage:         usage,
		sessionStore:  sessionStore,
		stateStore:    stateStore,
		memories:      memories,
		mediaStore:    media.NewStore(filepath.Join(cfg.AttachmentsDir(), sessionID)),
		compressor:    compressor,
		calibrator:    calibrator,
		prompts:       prompts,
		store:         store,
		skills:        skills,
		projectSkills: projectSkills,
		mcp:           mcp,
		extractQueue:  extractQueue,
		extractWorker: extractWorker,
		sessionID:     sessionID,
		cwd:           cwd,
		modelRef:      strings.TrimSpace(modelRef),
		working:       memory.NewWorkingMemory(),
	}
	s.guard = s.newGuardForSession(sessionID)
	return s
}

func (a *Agent) LoadSessionState(ctx context.Context) (SessionSnapshot, error) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	// 断线不丢RAM中的真实结果；重新附着允许在同一串行点先重试保存。
	if a.HasUnsavedMemory() {
		if err := a.prepareMemory(ctx); err != nil {
			return SessionSnapshot{}, err
		}
	}
	a.stateMu.Lock()
	state := a.contextMemory
	saveErr := a.saveErr
	retired := a.memoryRetired
	a.stateMu.Unlock()
	if retired {
		return SessionSnapshot{}, fmt.Errorf("session memory has been disposed")
	}
	if saveErr != nil {
		return SessionSnapshot{}, fmt.Errorf("session has unsaved results: %w", saveErr)
	}
	if state != nil {
		return SessionSnapshot{Messages: memory.ProjectMessages(a.working.Messages()), Compacted: state.Summary() != ""}, nil
	}
	if a.stateStore == nil || a.sessionID == "" {
		return SessionSnapshot{}, nil
	}
	st, err := a.stateStore.Load(ctx, a.sessionID)
	if err != nil || st == nil {
		return SessionSnapshot{}, err
	}
	a.summary = strings.TrimSpace(st.Summary)
	a.stateMu.Lock()
	a.contextMemory = a.newContextMemory(a.summary)
	a.stateMu.Unlock()
	st.Messages = memory.RecoverMessages(st.Messages)
	a.working.Clear()
	for _, msg := range st.Messages {
		a.working.AddMessage(msg)
	}
	return SessionSnapshot{Messages: st.Messages, Compacted: a.summary != ""}, nil
}

func (a *Agent) SnapshotState(ctx context.Context) (SessionSnapshot, error) {
	// 活跃 attach 只读已提交 DB；SQLite 自身保证整行一致，不能用轻量状态锁包住慢读取。
	return a.snapshotStored(ctx)
}

func (a *Agent) snapshotStored(ctx context.Context) (SessionSnapshot, error) {
	// Store 属于 root 生命周期且不会被配置重建；不读 run 起点同步中的 session 指针。
	store := a.root().stateStore
	if store == nil || a.sessionID == "" {
		return SessionSnapshot{}, nil
	}
	st, err := store.Load(ctx, a.sessionID)
	if err != nil || st == nil {
		return SessionSnapshot{}, err
	}
	return SessionSnapshot{Messages: st.Messages, Compacted: strings.TrimSpace(st.Summary) != ""}, nil
}

func (a *Agent) SessionID() string { return a.sessionID }
func (a *Agent) CWD() string       { return a.cwd }
func (a *Agent) ModelRef() string  { return a.modelRef }
func (a *Agent) ProjectSkills() *skill.Catalog {
	if a == nil {
		return nil
	}
	return a.projectSkills
}

func (a *Agent) SetModelRef(modelRef string) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	a.CancelMemory()
	a.modelRef = strings.TrimSpace(modelRef)
}

func (a *Agent) root() *Agent {
	if a == nil {
		return nil
	}
	if a.runtime != nil {
		return a.runtime
	}
	return a
}

// syncRuntime 在每轮 run 前复制全局 runtime 指针，保证 session 使用最新 config/router/tools。
func (a *Agent) syncRuntime() {
	root := a.root()
	if root == nil || root == a {
		return
	}
	root.configMu.RLock()
	defer root.configMu.RUnlock()
	a.cfg = root.cfg
	a.router = root.router
	a.tools = root.tools
	a.usage = root.usage
	a.sessionStore = root.sessionStore
	a.stateStore = root.stateStore
	a.memories = root.memories
	a.compressor = root.compressor
	a.calibrator = root.calibrator
	a.prompts = root.prompts
	a.store = root.store
	a.skills = root.skills
	a.mcp = root.mcp
	a.extractQueue = root.extractQueue
	a.extractWorker = root.extractWorker
	a.guard = a.newGuardForSession(a.sessionID)
}

func (a *Agent) attachmentRoot() string {
	if a.mediaStore == nil {
		return ""
	}
	return a.mediaStore.Root
}
