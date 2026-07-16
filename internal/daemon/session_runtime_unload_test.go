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
	if _, _, err := m.beginRun("client-a"); err != nil {
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
