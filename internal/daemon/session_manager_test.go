package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/protocol"
)

func newTestSessionManager(t *testing.T) *sessionManager {
	return newTestSessionManagerWithActiveModel(t, "test/model")
}

func newTestSessionManagerWithActiveModel(t *testing.T, activeModel string) *sessionManager {
	t.Helper()
	cfg := &config.Config{
		DataDir:     t.TempDir(),
		ActiveModel: activeModel,
		Models: []config.ModelConfig{
			{Provider: "test", Model: "model", BaseURL: "https://api.example.com/v1", ContextWindow: 128000, MaxOutputTokens: 8192, APIKey: "test-key"},
			{Provider: "test", Model: "alternate", BaseURL: "https://api.example.com/v1", ContextWindow: 256000, MaxOutputTokens: 16384, APIKey: "test-key"},
		},
	}
	if err := cfg.EnsureDataDirs(); err != nil {
		t.Fatalf("EnsureDataDirs error = %v", err)
	}
	ag, err := agent.NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent error = %v", err)
	}
	t.Cleanup(func() { ag.Close() })
	store := ag.SessionStore()
	return newSessionManager(ag, store)
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

func TestSessionManagerDeleteRemovesPersistedStateAndAttachments(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	current, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create current error = %v", err)
	}
	target, err := m.create(ctx, "client-b", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create target error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, target.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	if err := m.root.SessionStateStore().Save(ctx, target.Session.ID, "state", nil, memory.ToolSummary{}); err != nil {
		t.Fatalf("Save session state error = %v", err)
	}
	attachmentRoot := filepath.Join(m.root.Config().AttachmentsDir(), target.Session.ID)
	if err := os.MkdirAll(filepath.Join(attachmentRoot, "nested"), 0755); err != nil {
		t.Fatalf("MkdirAll attachments error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(attachmentRoot, "nested", "note.txt"), []byte("attachment"), 0644); err != nil {
		t.Fatalf("WriteFile attachment error = %v", err)
	}
	m.detach("client-b")

	if err := m.delete(ctx, "client-a", target.Session.ID); err != nil {
		t.Fatalf("delete target error = %v", err)
	}
	if meta, err := m.store.Get(ctx, target.Session.ID); err != nil || meta != nil {
		t.Fatalf("Get deleted session = (%#v, %v), want (nil, nil)", meta, err)
	}
	if state, err := m.root.SessionStateStore().Load(ctx, target.Session.ID); err != nil || state != nil {
		t.Fatalf("Load deleted session state = (%#v, %v), want (nil, nil)", state, err)
	}
	if _, err := os.Stat(attachmentRoot); !os.IsNotExist(err) {
		t.Fatalf("attachment root stat error = %v, want not exist", err)
	}
	if m.isClientAttached("client-a", current.Session.ID) == false {
		t.Fatal("current session unexpectedly detached")
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
	updated, err := m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, Title: &title})
	if err != nil {
		t.Fatalf("title update while running error = %v", err)
	}
	if got := updated.Session.Title; got != title {
		t.Fatalf("updated title = %q, want %q", got, title)
	}
	modelRef := "test/alternate"
	if _, err := m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, ModelRef: &modelRef}); err == nil {
		t.Fatal("model update while running error = nil, want busy")
	}
	m.setStatus(snap.Session.ID, sessionIdle)
	updated, err = m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, Title: &title})
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

func TestSessionManagerPruneInactiveRemovesAttachments(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	attachmentRoot := filepath.Join(m.root.Config().AttachmentsDir(), snap.Session.ID)
	if err := os.MkdirAll(attachmentRoot, 0755); err != nil {
		t.Fatalf("MkdirAll attachments error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(attachmentRoot, "note.txt"), []byte("attachment"), 0644); err != nil {
		t.Fatalf("WriteFile attachment error = %v", err)
	}
	m.detach("client-a")

	m.pruneInactive(ctx, time.Hour)
	if meta, err := m.store.Get(ctx, snap.Session.ID); err != nil || meta != nil {
		t.Fatalf("Get pruned session = (%#v, %v), want (nil, nil)", meta, err)
	}
	if _, err := os.Stat(attachmentRoot); !os.IsNotExist(err) {
		t.Fatalf("attachment root stat error = %v, want not exist", err)
	}
}
func TestSessionManagerAttachDoesNotRecreateConcurrentlyDeletedSession(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if err := m.store.SetMessageCount(ctx, snap.Session.ID, 1); err != nil {
		t.Fatalf("SetMessageCount error = %v", err)
	}
	m.detach("owner")

	m.mu.Lock()
	m.deleting[snap.Session.ID] = true
	m.mu.Unlock()
	if err := m.deletePersistedSession(ctx, snap.Session.ID); err != nil {
		t.Fatalf("delete persisted session error = %v", err)
	}
	m.mu.Lock()
	delete(m.deleting, snap.Session.ID)
	delete(m.runtime, snap.Session.ID)
	m.mu.Unlock()

	if _, err := m.attach(ctx, "joiner", snap.Session.ID, false); err == nil {
		t.Fatal("attach deleted session error = nil, want rejection")
	}
	m.mu.RLock()
	_, exists := m.runtime[snap.Session.ID]
	m.mu.RUnlock()
	if exists {
		t.Fatal("attach recreated runtime for deleted session")
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

func TestSessionManagerAttachSwitchHandsOffRemainingClients(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	first, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create first error = %v", err)
	}
	if _, err := m.attach(ctx, "observer", first.Session.ID, false); err != nil {
		t.Fatalf("attach observer error = %v", err)
	}
	second, err := m.create(ctx, "other", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create second error = %v", err)
	}

	var gotConnID, gotSessionID string
	m.onClientDetached = func(connID, sessionID string) {
		gotConnID, gotSessionID = connID, sessionID
	}
	if _, err := m.attach(ctx, "owner", second.Session.ID, false); err != nil {
		t.Fatalf("switch attach error = %v", err)
	}
	if gotConnID != "owner" || gotSessionID != first.Session.ID {
		t.Fatalf("handoff = (%q, %q), want (%q, %q)", gotConnID, gotSessionID, "owner", first.Session.ID)
	}
}

func TestSessionManagerModelUpdateDoesNotCommitWhenSnapshotReadFails(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	modelRef := "test/alternate"
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.update(cancelled, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, ModelRef: &modelRef}); err == nil {
		t.Fatal("update with cancelled snapshot context error = nil, want failure")
	}
	meta, err := m.store.Get(ctx, snap.Session.ID)
	if err != nil {
		t.Fatalf("get session error = %v", err)
	}
	if meta == nil || meta.ModelRef != "test/model" {
		t.Fatalf("persisted model_ref = %#v, want test/model", meta)
	}
	if got := m.runtime[snap.Session.ID].agent.ModelRef(); got != "test/model" {
		t.Fatalf("runtime model_ref = %q, want test/model", got)
	}
}

func TestSessionManagerCreateBlocksConcurrentAttachUntilCreationCompletes(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	if _, err := m.create(ctx, "creator", t.TempDir(), "previous"); err != nil {
		t.Fatalf("create previous error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	m.beforeAttachStateLoad = func() {
		close(entered)
		<-release
	}
	createDone := make(chan error, 1)
	go func() {
		_, err := m.create(ctx, "creator", t.TempDir(), "new")
		createDone <- err
	}()
	<-entered

	m.mu.RLock()
	var creatingID string
	for id, owner := range m.creating {
		if owner == "creator" {
			creatingID = id
			break
		}
	}
	m.mu.RUnlock()
	if creatingID == "" {
		t.Fatal("creating session id not found")
	}
	if _, err := m.attach(ctx, "observer", creatingID, false); err == nil {
		t.Fatal("attach during creation error = nil, want rejection")
	}
	close(release)
	if err := <-createDone; err != nil {
		t.Fatalf("create error = %v", err)
	}
	if !m.isClientAttached("creator", creatingID) {
		t.Fatal("creator is not attached to completed session")
	}
	if got := m.currentSessionID("observer"); got != "" {
		t.Fatalf("observer attachment = %q, want empty", got)
	}
}

func TestSessionManagerCreateAttachFailureRollsBackAndPreservesPreviousAttachment(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	previous, err := m.create(ctx, "client-a", t.TempDir(), "previous")
	if err != nil {
		t.Fatalf("create previous error = %v", err)
	}

	attachCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.beforeAttachStateLoad = cancel
	if _, err := m.create(attachCtx, "client-a", t.TempDir(), "failed"); err == nil {
		t.Fatal("create error = nil, want attach state-load failure")
	}
	m.beforeAttachStateLoad = nil

	if !m.isClientAttached("client-a", previous.Session.ID) {
		t.Fatal("client-a did not retain its previous attachment")
	}
	m.mu.RLock()
	previousRT := m.runtime[previous.Session.ID]
	attachedID := m.attached["client-a"]
	runtimeCount := len(m.runtime)
	m.mu.RUnlock()
	if previousRT == nil || !previousRT.clients["client-a"] || attachedID != previous.Session.ID {
		t.Fatalf("previous runtime attachment = (%#v, %q), want client-a on %q", previousRT, attachedID, previous.Session.ID)
	}
	if runtimeCount != 1 {
		t.Fatalf("runtime count = %d, want only the previous runtime", runtimeCount)
	}
	metas, err := m.store.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(metas) != 1 || metas[0].ID != previous.Session.ID {
		t.Fatalf("persisted sessions = %#v, want only %q", metas, previous.Session.ID)
	}
}
func TestSessionManagerCreateRejectsMissingDefaultModelWithoutPersisting(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManagerWithActiveModel(t, "  ")

	if _, err := m.create(ctx, "client-a", t.TempDir(), ""); err == nil {
		t.Fatal("create error = nil, want missing default model rejection")
	}
	metas, err := m.store.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("persisted sessions = %#v, want none", metas)
	}
}

func TestSessionManagerCreateRejectsInvalidDefaultModelWithoutPersisting(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManagerWithActiveModel(t, "missing/model")

	if _, err := m.create(ctx, "client-a", t.TempDir(), ""); err == nil {
		t.Fatal("create error = nil, want invalid default model rejection")
	}
	metas, err := m.store.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("persisted sessions = %#v, want none", metas)
	}
}

func TestSessionManagerCreatesSessionWithDefaultModelRef(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if got, want := snap.Session.ModelRef, "test/model"; got != want {
		t.Fatalf("created session model_ref = %q, want %q", got, want)
	}
}

func TestSessionManagerModelChangeDoesNotChangeDefault(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	first, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create first error = %v", err)
	}
	modelRef := "test/alternate"
	if _, err := m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: first.Session.ID, ModelRef: &modelRef}); err != nil {
		t.Fatalf("update model_ref error = %v", err)
	}
	if got, want := m.root.Config().ActiveModel, "test/model"; got != want {
		t.Fatalf("default model changed to %q, want %q", got, want)
	}
	second, err := m.create(ctx, "client-b", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create second error = %v", err)
	}
	if got, want := second.Session.ModelRef, "test/model"; got != want {
		t.Fatalf("new session model_ref = %q, want %q", got, want)
	}
}

func TestSessionManagerUpdateTitleAndInvalidModelIsAtomic(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "original")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	title := "renamed"
	invalidModel := "missing/model"
	if _, err := m.update(ctx, "client-a", protocol.SessionUpdateParams{
		SessionID: snap.Session.ID,
		Title:     &title,
		ModelRef:  &invalidModel,
	}); err == nil {
		t.Fatal("update error = nil, want invalid model rejection")
	}

	meta, err := m.store.Get(ctx, snap.Session.ID)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if meta == nil {
		t.Fatal("session disappeared after rejected update")
	}
	if got, want := meta.Title, "original"; got != want {
		t.Fatalf("persisted title = %q, want %q", got, want)
	}
	if got, want := meta.ModelRef, "test/model"; got != want {
		t.Fatalf("persisted model_ref = %q, want %q", got, want)
	}
	m.mu.RLock()
	rt := m.runtime[snap.Session.ID]
	m.mu.RUnlock()
	if rt == nil || rt.agent.ModelRef() != "test/model" {
		t.Fatalf("runtime model_ref = %#v, want test/model", rt)
	}
}

func TestSessionManagerUpdateModelRef(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	modelRef := "test/alternate"
	updated, err := m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, ModelRef: &modelRef})
	if err != nil {
		t.Fatalf("update error = %v", err)
	}
	if got := updated.Session.ModelRef; got != modelRef {
		t.Fatalf("updated model_ref = %q, want %q", got, modelRef)
	}
	meta, err := m.store.Get(ctx, snap.Session.ID)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if meta == nil || meta.ModelRef != modelRef {
		t.Fatalf("persisted model_ref = %#v, want %q", meta, modelRef)
	}
}

func TestSessionManagerLegacySessionMaterializesDefaultModel(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	if err := m.store.Create(ctx, memory.SessionMeta{ID: "legacy", CWD: t.TempDir()}); err != nil {
		t.Fatalf("create legacy session error = %v", err)
	}
	snap, err := m.attach(ctx, "client-a", "legacy", false)
	if err != nil {
		t.Fatalf("attach legacy session error = %v", err)
	}
	if got, want := snap.Session.ModelRef, "test/model"; got != want {
		t.Fatalf("legacy session model_ref = %q, want %q", got, want)
	}
	meta, err := m.store.Get(ctx, "legacy")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if meta == nil || meta.ModelRef != "test/model" {
		t.Fatalf("persisted legacy model_ref = %#v, want test/model", meta)
	}
	if _, _, err := m.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun after default materialization error = %v", err)
	}
}

func TestSessionManagerLegacySessionMaterializesModelOnceUnderConcurrentAttach(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	if err := m.store.Create(ctx, memory.SessionMeta{ID: "legacy", CWD: t.TempDir()}); err != nil {
		t.Fatalf("create legacy session error = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, connID := range []string{"client-a", "client-b"} {
		connID := connID
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap, err := m.attach(ctx, connID, "legacy", false)
			if err == nil && snap.Session.ModelRef != "test/model" {
				err = fmt.Errorf("snapshot model_ref = %q, want test/model", snap.Session.ModelRef)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	meta, err := m.store.Get(ctx, "legacy")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if meta == nil || meta.ModelRef != "test/model" {
		t.Fatalf("persisted legacy model_ref = %#v, want test/model", meta)
	}
}
func TestSessionManagerRejectsUnknownModelRef(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snap, err := m.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	modelRef := "missing/model"
	if _, err := m.update(ctx, "client-a", protocol.SessionUpdateParams{SessionID: snap.Session.ID, ModelRef: &modelRef}); err == nil {
		t.Fatal("update error = nil, want unavailable model rejection")
	}
	meta, err := m.store.Get(ctx, snap.Session.ID)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if meta == nil || meta.ModelRef == modelRef {
		t.Fatalf("persisted model_ref = %#v, must not accept unavailable model", meta)
	}
}
