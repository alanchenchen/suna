package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
)

func TestSessionManagerUnloadsDetachedIdleRuntime(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = 10 * time.Millisecond

	snap, err := m.create(ctx, "client-a", t.TempDir(), "saved session")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, snap.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	if err := m.root.SessionStateStore().Save(ctx, snap.Session.ID, "state", nil, memory.ToolSummary{}); err != nil {
		t.Fatalf("Save session state error = %v", err)
	}

	m.detach("client-a")
	waitForRuntimeUnloaded(t, m, snap.Session.ID)

	if meta, err := m.store.Get(ctx, snap.Session.ID); err != nil || meta == nil {
		t.Fatalf("Get persisted session = (%#v, %v), want existing metadata", meta, err)
	}
	if state, err := m.root.SessionStateStore().Load(ctx, snap.Session.ID); err != nil || state == nil {
		t.Fatalf("Load persisted session state = (%#v, %v), want existing state", state, err)
	}
	if _, err := m.attach(ctx, "client-b", snap.Session.ID, false); err != nil {
		t.Fatalf("attach after runtime unload error = %v", err)
	}
}

func TestSessionManagerKeepsEmptySessionAfterRuntimeUnload(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = 10 * time.Millisecond

	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	m.detach("client-a")
	waitForRuntimeUnloaded(t, m, snap.Session.ID)

	if meta, err := m.store.Get(ctx, snap.Session.ID); err != nil || meta == nil {
		t.Fatalf("Get empty persisted session = (%#v, %v), want existing metadata", meta, err)
	}
}

func TestSessionManagerReattachCancelsRuntimeUnload(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = 30 * time.Millisecond

	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, snap.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	original := runtimeForSession(t, m, snap.Session.ID)

	m.detach("client-a")
	if _, err := m.attach(ctx, "client-b", snap.Session.ID, false); err != nil {
		t.Fatalf("reattach error = %v", err)
	}
	time.Sleep(2 * m.runtimeUnloadDelay)
	if got := runtimeForSession(t, m, snap.Session.ID); got != original {
		t.Fatal("runtime was unloaded or replaced after reattach")
	}
}

func TestSessionManagerDefersRuntimeUnloadUntilRunBecomesIdle(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = 10 * time.Millisecond

	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, snap.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	if _, _, _, err := m.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}

	m.detach("client-a")
	time.Sleep(2 * m.runtimeUnloadDelay)
	if runtimeForSession(t, m, snap.Session.ID) == nil {
		t.Fatal("running session runtime was unloaded")
	}

	m.setStatus(snap.Session.ID, sessionIdle)
	waitForRuntimeUnloaded(t, m, snap.Session.ID)
}

func TestSessionManagerOrphansActiveRuntimeOnLastDetach(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = 10 * time.Millisecond

	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, snap.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	if _, _, _, err := m.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}

	// 正在执行的 run 不因最后一个客户端离开而取消：detach 只是退出观察，
	// run 继续跑，runtime 保持常驻，attach 回来可看结果。
	detached := m.detach("client-a")
	if !detached.orphaned {
		t.Fatal("detach orphaned = false, want true")
	}
	if detached.idle {
		t.Fatal("detach idle = true, want false for active runtime")
	}
	if detached.agent != nil {
		t.Fatal("detach agent != nil, want nil for running run (not cancelled)")
	}
	if runtimeForSession(t, m, snap.Session.ID) == nil {
		t.Fatal("running session runtime was unloaded on detach")
	}

	m.setStatus(snap.Session.ID, sessionIdle)
	waitForRuntimeUnloaded(t, m, snap.Session.ID)
}

func TestSessionManagerCancelsWaitingRunOnLastDetach(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = 10 * time.Millisecond

	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, snap.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	if _, _, _, err := m.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	m.setStatus(snap.Session.ID, sessionWaiting)

	// 等待 ask/guard 交互时最后一个客户端离开，无人能回复，run 无法继续：
	// 必须取消，否则 run 永远卡在 waiting、daemon 因 hasActiveRun 永不退出。
	detached := m.detach("client-a")
	if !detached.orphaned {
		t.Fatal("detach orphaned = false, want true")
	}
	if !detached.waiting {
		t.Fatal("detach waiting = false, want true for waiting run")
	}
	if detached.agent == nil {
		t.Fatal("detach agent = nil, want agent to cancel waiting run")
	}
}

func runtimeForSession(t *testing.T, m *sessionManager, sessionID string) *sessionRuntime {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runtime[sessionID]
}

func waitForRuntimeUnloaded(t *testing.T, m *sessionManager, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runtimeForSession(t, m, sessionID) == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("runtime %q was not unloaded", sessionID)
}
