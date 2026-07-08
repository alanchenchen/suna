package daemon

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

func newTestSessionManager(t *testing.T) *sessionManager {
	t.Helper()
	cfg := &config.Config{DataDir: t.TempDir()}
	if err := cfg.EnsureDataDirs(); err != nil {
		t.Fatalf("EnsureDataDirs error = %v", err)
	}
	ag, err := agent.NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent error = %v", err)
	}
	t.Cleanup(func() { ag.Close() })
	store := ag.SessionStore()
	states := ag.SessionStateStore()
	return newSessionManager(ag, store, states)
}

func TestSessionManagerBeginRunAllowsSingleWriter(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, err := m.attach(ctx, "client-b", snap.Session.ID, false); err != nil {
		t.Fatalf("attach error = %v", err)
	}
	if _, _, err := m.beginRun("client-a"); err != nil {
		t.Fatalf("first beginRun error = %v", err)
	}
	if _, _, err := m.beginRun("client-b"); err == nil {
		t.Fatal("second beginRun error = nil, want busy")
	}
	m.setStatus(snap.Session.ID, sessionIdle)
	if _, _, err := m.beginRun("client-b"); err != nil {
		t.Fatalf("beginRun after idle error = %v", err)
	}
}

func TestSessionManagerActiveAttachReturnsCurrentRunView(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, err := m.beginRun("owner"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	m.appendStream(snap.Session.ID, "partial answer")
	joined, err := m.attach(ctx, "observer", snap.Session.ID, false)
	if err != nil {
		t.Fatalf("attach error = %v", err)
	}
	if joined.CurrentRun == nil {
		t.Fatal("CurrentRun = nil, want running view")
	}
	if got, want := joined.CurrentRun.AssistantBuffer, "partial answer"; got != want {
		t.Fatalf("AssistantBuffer = %q, want %q", got, want)
	}
}

func TestSessionManagerDeleteOnlyAllowsDetachedIdleNonCurrentSession(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	current, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create current error = %v", err)
	}
	if err := m.delete(ctx, "client-a", current.Session.ID); err == nil {
		t.Fatal("delete current idle session error = nil, want rejected")
	}
	if _, _, err := m.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	if err := m.delete(ctx, "client-a", current.Session.ID); err == nil {
		t.Fatal("delete running current session error = nil, want busy")
	}
	m.setStatus(current.Session.ID, sessionIdle)

	target, err := m.create(ctx, "client-b", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create target error = %v", err)
	}
	if err := m.delete(ctx, "client-a", target.Session.ID); err == nil {
		t.Fatal("delete attached target error = nil, want rejected")
	}
	if err := m.store.SetMessageCount(ctx, target.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	m.detach("client-b")
	if err := m.delete(ctx, "client-a", target.Session.ID); err != nil {
		t.Fatalf("delete detached idle target error = %v", err)
	}
}

func TestSessionManagerUpdateRequiresAttachedIdleSession(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	title := "renamed"
	_, err = m.update(ctx, "stranger", protocol.SessionUpdateParams{SessionID: snap.Session.ID, Title: &title})
	if err == nil {
		t.Fatal("update by stranger error = nil, want session_required")
	}
	if _, _, err := m.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	_, err = m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, Title: &title})
	if err == nil {
		t.Fatal("update while running error = nil, want busy")
	}
	m.setStatus(snap.Session.ID, sessionIdle)
	updated, err := m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, Title: &title})
	if err != nil {
		t.Fatalf("update idle owner error = %v", err)
	}
	if got := updated.Session.Title; got != title {
		t.Fatalf("updated title = %q, want %q", got, title)
	}
}

func TestSessionManagerPruneInactiveSkipsActiveSession(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	m.pruneInactive(ctx, 1)
	meta, err := m.store.Get(ctx, snap.Session.ID)
	if err != nil {
		t.Fatalf("get error = %v", err)
	}
	if meta == nil {
		t.Fatal("active empty session was pruned")
	}
}

func TestSessionManagerAttachRequireActive(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	idle, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create idle error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, idle.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	m.detach("client-a")

	if _, err := m.attach(ctx, "joiner", idle.Session.ID, true); err == nil {
		t.Fatal("attach requireActive to idle session error = nil, want rejected")
	}
	if _, err := m.attach(ctx, "resumer", idle.Session.ID, false); err != nil {
		t.Fatalf("attach resume idle error = %v", err)
	}

	active, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create active error = %v", err)
	}
	if _, err := m.attach(ctx, "observer", active.Session.ID, true); err != nil {
		t.Fatalf("attach requireActive to active session error = %v", err)
	}
}

func TestSessionManagerAttachSwitchDetachesPreviousSession(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	first, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create first error = %v", err)
	}
	second, err := m.create(ctx, "client-b", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create second error = %v", err)
	}
	if _, err := m.attach(ctx, "client-a", second.Session.ID, false); err != nil {
		t.Fatalf("switch attach error = %v", err)
	}
	if m.isClientAttached("client-a", first.Session.ID) {
		t.Fatal("client-a still attached to first session")
	}
	if !m.isClientAttached("client-a", second.Session.ID) {
		t.Fatal("client-a not attached to second session")
	}
	m.mu.RLock()
	firstRT := m.runtime[first.Session.ID]
	firstClients := 0
	if firstRT != nil {
		firstClients = len(firstRT.clients)
	}
	m.mu.RUnlock()
	if firstClients != 0 {
		t.Fatalf("first session clients = %d, want 0", firstClients)
	}
}
