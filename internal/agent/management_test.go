package agent

import (
	"context"
	"errors"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/runner"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompactSyncsRuntimeBeforeBindingSessionModel(t *testing.T) {
	cfg := &config.Config{Models: []config.ModelConfig{{
		Provider:        "test",
		Model:           "current",
		BaseURL:         "https://example.invalid/v1",
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
		APIKey:          "test-key",
	}}}
	router, err := model.NewRouter(cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	root := &Agent{prompts: loader, cfg: cfg, router: router, compressor: memory.NewCompressor()}
	session := &Agent{
		runtime: root,
		// 模拟配置重载前 session 保存的旧 router；syncRuntime 后必须改用 root 的新 router。
		router:   nil,
		modelRef: "test/current",
		working:  memory.NewWorkingMemory(),
	}
	session.working.AddMessage(memory.NewTextMessage(model.RoleUser, "keep this turn"))

	err = session.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact() error = %v, want runtime-synced router to bind session model", err)
	}
}

func TestSessionAttachRetriesUnsavedKnownHistoryWithoutReloadingStaleDB(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.DB().Exec(`INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	a := &Agent{sessionID: "s", stateStore: memory.NewSessionStateStore(store.DB()), working: memory.NewWorkingMemory(), contextMemory: runner.NewContextMemory(""), saveErr: errors.New("previous save failed")}
	a.working.AddMessage(memory.NewTextMessage(model.RoleUser, "known input"))
	a.working.AddMessage(memory.Message{Role: model.RoleAssistant, ToolCalls: []memory.ToolCall{{ID: "call", Name: "exec", Arguments: `{}`}, {ID: "missing", Name: "exec", Arguments: `{}`}}})
	result := memory.NewTextMessage(model.RoleTool, "known side effect")
	result.ToolCallID = "call"
	result.ToolStatus = "completed"
	a.working.AddMessage(result)
	snapshot, err := a.LoadSessionState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.HasUnsavedMemory() || len(snapshot.Messages) != 2 || !strings.Contains(snapshot.Messages[1].Text(), "known side effect") || !strings.Contains(snapshot.Messages[1].Text(), "unknown") {
		t.Fatalf("snapshot=%+v dirty=%v", snapshot, a.HasUnsavedMemory())
	}
	persisted, err := a.stateStore.Load(context.Background(), "s")
	if err != nil || persisted == nil || len(persisted.Messages) != 2 {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
}
func TestRunAdmissionWaitsOnlyForIdleMemoryCommit(t *testing.T) {
	a := &Agent{}
	a.runMu.Lock()
	a.idleMemoryDone = make(chan struct{})
	done := make(chan bool, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { done <- a.tryRunLock(ctx) }()
	a.stateMu.Lock()
	a.runMu.Unlock()
	close(a.idleMemoryDone)
	a.idleMemoryDone = nil
	a.stateMu.Unlock()
	select {
	case acquired := <-done:
		if !acquired {
			t.Fatal("idle commit rejected new input")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if a.tryRunLock(ctx) {
		t.Fatal("active run accepted a second run")
	}
	a.runMu.Unlock()
}

func newCheckpointAgent(t *testing.T) (*Agent, *memory.Store) {
	t.Helper()
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for _, statement := range []string{
		`INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now'))`,
		`CREATE TABLE checkpoint_writes (kind TEXT)`,
		`CREATE TRIGGER count_checkpoint_insert AFTER INSERT ON session_state BEGIN INSERT INTO checkpoint_writes VALUES ('insert'); END`,
		`CREATE TRIGGER count_checkpoint_update AFTER UPDATE ON session_state BEGIN INSERT INTO checkpoint_writes VALUES ('update'); END`,
	} {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return &Agent{sessionID: "s", stateStore: memory.NewSessionStateStore(store.DB()), working: memory.NewWorkingMemory(), contextMemory: runner.NewContextMemory("")}, store
}

func checkpointWrites(t *testing.T, store *memory.Store) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM checkpoint_writes`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestFinishMemoryProjectsImagesWithoutCheckpoint(t *testing.T) {
	ctx := context.Background()
	a, store := newCheckpointAgent(t)
	ref := model.MediaRef{Kind: model.MediaAttachment, Name: "example.png", MimeType: "image/png"}
	a.working.AddMessage(memory.Message{Role: model.RoleUser, Origin: memory.OriginUser, Content: []model.ContentBlock{{Type: model.ContentText, Text: "first"}, {Type: model.ContentImage, Media: &ref}, {Type: model.ContentText, Text: "last"}}})
	if err := a.saveConversationState(ctx); err != nil {
		t.Fatal(err)
	}
	if got := a.working.Messages()[0].Content[1].Type; got != model.ContentImage {
		t.Fatalf("current-run block type = %q, want image", got)
	}
	a.finishMemory()
	if got := a.working.Messages()[0].Text(); got != "first\n[image: example.png, image/png, source=attachment:example.png]\nlast" {
		t.Fatalf("finished projection = %q", got)
	}
	if got := checkpointWrites(t, store); got != 1 {
		t.Fatalf("writes after RAM cleanup = %d, want 1", got)
	}
}

func TestLaterCheckpointCanPersistKnownResultsAfterFailure(t *testing.T) {
	ctx := context.Background()
	a, store := newCheckpointAgent(t)
	a.working.AddMessage(memory.NewTextMessage(model.RoleUser, "known input"))
	if err := a.saveConversationState(ctx); err != nil {
		t.Fatal(err)
	}
	a.working.AddMessage(memory.Message{Role: model.RoleAssistant, ToolCalls: []memory.ToolCall{{ID: "known", Name: "exec", Arguments: `{}`}, {ID: "missing", Name: "exec", Arguments: `{}`}}})
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_checkpoint BEFORE UPDATE ON session_state BEGIN SELECT RAISE(ABORT, 'save rejected'); END`); err != nil {
		t.Fatal(err)
	}
	firstErr := a.saveConversationState(ctx)
	if firstErr == nil || !a.HasUnsavedMemory() {
		t.Fatalf("checkpoint error = %v, dirty = %v", firstErr, a.HasUnsavedMemory())
	}
	if _, err := store.DB().Exec(`DROP TRIGGER reject_checkpoint`); err != nil {
		t.Fatal(err)
	}
	result := memory.NewTextMessage(model.RoleTool, "known side effect")
	result.ToolCallID = "known"
	result.ToolStatus = "completed"
	a.working.AddMessage(result)
	a.finishMemory()
	if got := checkpointWrites(t, store); got != 1 || !a.HasUnsavedMemory() {
		t.Fatalf("cleanup writes = %d, dirty = %v, want 1 and true", got, a.HasUnsavedMemory())
	}

	// 后续真实结果边界仍可全量保存已知事实，不被先前检查点失败封死。
	if err := a.saveConversationState(ctx); err != nil {
		t.Fatalf("later checkpoint error = %v", err)
	}
	persisted, err := a.stateStore.Load(ctx, a.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted == nil || len(persisted.Messages) != 3 || persisted.Messages[2].ToolCallID != "known" || persisted.Messages[2].Text() != "known side effect" {
		t.Fatalf("persisted known results = %+v", persisted)
	}
	if got := checkpointWrites(t, store); got != 2 || a.HasUnsavedMemory() {
		t.Fatalf("later checkpoint writes = %d, dirty = %v, want 2 and false", got, a.HasUnsavedMemory())
	}
}

func TestFailedCandidateCommitDoesNotPublishState(t *testing.T) {
	ctx := context.Background()
	a, store := newCheckpointAgent(t)
	a.working.AddMessage(memory.NewTextMessage(model.RoleUser, "original"))
	if err := a.saveConversationState(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_checkpoint BEFORE UPDATE ON session_state BEGIN SELECT RAISE(ABORT, 'save rejected'); END`); err != nil {
		t.Fatal(err)
	}
	candidate := []memory.Message{memory.NewTextMessage(model.RoleAssistant, "observation")}
	if err := a.commitMemory(ctx, "candidate", candidate); err == nil {
		t.Fatal("commit error = nil, want failure")
	}
	if a.summary != "" || a.HasUnsavedMemory() {
		t.Fatalf("failed candidate summary = %q, dirty = %v", a.summary, a.HasUnsavedMemory())
	}
	if _, err := store.DB().Exec(`DROP TRIGGER reject_checkpoint`); err != nil {
		t.Fatal(err)
	}
	if err := a.commitMemory(ctx, "candidate", candidate); err != nil {
		t.Fatal(err)
	}
	if got := checkpointWrites(t, store); got != 2 || a.summary != "candidate" {
		t.Fatalf("successful commit writes = %d, summary = %q", got, a.summary)
	}
}

func TestActiveSnapshotDoesNotHoldStateMutexDuringSQLiteWait(t *testing.T) {
	a, store := newCheckpointAgent(t)
	store.DB().SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := store.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	done := make(chan error, 1)
	go func() { _, err := a.SnapshotState(ctx); done <- err }()
	for store.DB().Stats().WaitCount == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("snapshot did not wait for SQLite connection")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if !a.stateMu.TryLock() {
		t.Fatal("snapshot held stateMu while waiting for SQLite")
	}
	a.stateMu.Unlock()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStopMemoryDoesNotRetireKnownResultsButDisposeDoes(t *testing.T) {
	ctx := context.Background()
	a, _ := newCheckpointAgent(t)
	a.working.AddMessage(memory.NewTextMessage(model.RoleUser, "known input"))
	a.StopMemory()
	if err := a.prepareMemory(ctx); err != nil {
		t.Fatalf("prepare after cancelled delete error = %v", err)
	}
	if err := a.saveConversationState(ctx); err != nil {
		t.Fatal(err)
	}
	a.DisposeMemory()
	if err := a.prepareMemory(ctx); err == nil {
		t.Fatal("prepare after dispose error = nil, want retired memory")
	}
	if _, err := a.LoadSessionState(ctx); err == nil {
		t.Fatal("attach after dispose error = nil, want retired memory")
	}
}

func TestObservationOnlyMessageKeepsConversationCount(t *testing.T) {
	message := memory.NewTextMessage(model.RoleAssistant, "")
	message.Observation = &memory.Observation{Text: "saved evidence", Calls: []memory.ToolLabel{{Name: "readfile", Status: "completed"}}}
	if got := len(visibleMessagesForCount([]memory.Message{message})); got != 1 {
		t.Fatalf("visible count = %d, want observation retained as one historical message", got)
	}
}
