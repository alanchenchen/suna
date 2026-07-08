package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

// SessionSnapshot 是 daemon attach/create 后给 UI 渲染用的会话快照。
type SessionSnapshot struct {
	Messages    []model.Message
	Compacted   bool
	ToolSummary memory.ToolSummary
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

func (a *Agent) NewSessionAgent(sessionID, cwd string) *Agent {
	cwd = canonicalCWD(cwd)
	s := &Agent{
		runtime:       a.root(),
		cfg:           a.cfg,
		router:        a.router,
		tools:         a.tools,
		usage:         a.usage,
		sessionStore:  a.sessionStore,
		stateStore:    a.stateStore,
		memories:      a.memories,
		mediaStore:    media.NewStore(filepath.Join(a.cfg.AttachmentsDir(), sessionID)),
		compressor:    a.compressor,
		calibrator:    a.calibrator,
		prompts:       a.prompts,
		store:         a.store,
		skills:        a.skills,
		mcp:           a.mcp,
		extractQueue:  a.extractQueue,
		extractWorker: a.extractWorker,
		sessionID:     sessionID,
		cwd:           cwd,
		working:       memory.NewWorkingMemory(),
	}
	s.guard = s.newGuardForSession(sessionID)
	return s
}

func (a *Agent) LoadSessionState(ctx context.Context) (SessionSnapshot, error) {
	if a.stateStore == nil || a.sessionID == "" {
		return SessionSnapshot{}, nil
	}
	st, err := a.stateStore.Load(ctx, a.sessionID)
	if err != nil || st == nil {
		return SessionSnapshot{}, err
	}
	a.sessionState = strings.TrimSpace(st.Compacted)
	a.toolSummary = st.ToolSummary.Normalize()
	a.working.Clear()
	for _, msg := range st.LastMessages {
		a.working.AddMessage(msg)
	}
	return SessionSnapshot{Messages: st.LastMessages, Compacted: a.sessionState != "", ToolSummary: a.toolSummary}, nil
}

func (a *Agent) SnapshotState(ctx context.Context) (SessionSnapshot, error) {
	if a.stateStore == nil || a.sessionID == "" {
		return SessionSnapshot{}, nil
	}
	st, err := a.stateStore.Load(ctx, a.sessionID)
	if err != nil || st == nil {
		return SessionSnapshot{}, err
	}
	return SessionSnapshot{Messages: st.LastMessages, Compacted: strings.TrimSpace(st.Compacted) != "", ToolSummary: st.ToolSummary.Normalize()}, nil
}

func (a *Agent) SessionID() string { return a.sessionID }
func (a *Agent) CWD() string       { return a.cwd }

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
	a.guard = a.newGuardForSession(a.sessionID)
}

func (a *Agent) attachmentRoot() string {
	if a.mediaStore == nil {
		return ""
	}
	return a.mediaStore.Root
}
