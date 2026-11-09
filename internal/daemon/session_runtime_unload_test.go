package daemon

import (
	"context"
	"database/sql"
	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"testing"
	"time"
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
	if err := m.root.SessionStateStore().Save(ctx, snap.Session.ID, "state", nil); err != nil {
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

func TestDeleteFailureKeepsDirtyRuntimeAttachable(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = time.Hour
	snapshot, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtimeForSession(t, m, snapshot.Session.ID)
	db, err := sql.Open("sqlite", m.root.Config().DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TRIGGER reject_checkpoint BEFORE INSERT ON session_state BEGIN SELECT RAISE(ABORT, 'save rejected'); END`,
		`CREATE TRIGGER reject_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'delete rejected'); END`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// 只走输入检查点失败，不会到达物理模型请求。
	for range rt.agent.Run(ctx, agent.TextInput("known unsaved input")) {
	}
	if !rt.agent.HasUnsavedMemory() {
		t.Fatal("dirty = false after rejected input checkpoint")
	}
	m.detach("owner")
	if err := m.delete(ctx, "deleter", snapshot.Session.ID); err == nil {
		t.Fatal("delete error = nil, want injected database failure")
	}
	if got := runtimeForSession(t, m, snapshot.Session.ID); got != rt {
		t.Fatal("delete failure replaced the dirty runtime")
	}
	for _, statement := range []string{`DROP TRIGGER reject_checkpoint`, `DROP TRIGGER reject_delete`} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.attach(ctx, "returning", snapshot.Session.ID, false); err != nil {
		t.Fatalf("attach after failed delete error = %v", err)
	}
	persisted, err := m.root.SessionStateStore().Load(ctx, snapshot.Session.ID)
	if err != nil || persisted == nil || len(persisted.Messages) != 1 || persisted.Messages[0].Text() != "known unsaved input" || rt.agent.HasUnsavedMemory() {
		t.Fatalf("persisted = %+v, dirty = %v, error = %v", persisted, rt.agent.HasUnsavedMemory(), err)
	}
}

func TestSuccessfulDeleteRetiresOldRuntime(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = time.Hour
	snapshot, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtimeForSession(t, m, snapshot.Session.ID)
	m.detach("owner")
	if err := m.delete(ctx, "deleter", snapshot.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.agent.LoadSessionState(ctx); err == nil {
		t.Fatal("deleted runtime accepted state load")
	}
	if got := runtimeForSession(t, m, snapshot.Session.ID); got != nil {
		t.Fatal("deleted runtime remains registered")
	}
}

func TestUnloadWaitDoesNotHoldManagerLockOrRetireReattachedRuntime(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	m.runtimeUnloadDelay = time.Hour
	snapshot, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtimeForSession(t, m, snapshot.Session.ID)
	// 仅预留串行准入，不运行压缩模型；模拟卸载遇到尚未退出的短提交边界。
	_, run, err := rt.agent.ReserveCompact(ctx)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			_ = run()
		}
	}()
	m.detach("owner")
	m.mu.RLock()
	version := m.runtimeUnloadVersion[snapshot.Session.ID]
	m.mu.RUnlock()
	done := make(chan struct{})
	go func() {
		m.unloadRuntimeAfter(snapshot.Session.ID, rt, version, time.Nanosecond)
		close(done)
	}()

	observed := make(chan struct{})
	go func() {
		for {
			m.mu.RLock()
			waiting := rt.stateOps > 0
			m.mu.RUnlock()
			if waiting {
				close(observed)
				return
			}
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("unload blocked the manager lock while waiting for Agent")
	}
	// 重新附着沿现有 stateOps 排队，不应把取消候选变成永久退休。
	attached := make(chan error, 1)
	go func() { _, err := m.attach(ctx, "returning", snapshot.Session.ID, false); attached <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.RLock()
		queued := rt.stateOps == 2
		m.mu.RUnlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("attach did not queue behind the pending state operation")
		}
		time.Sleep(time.Millisecond)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	released = true
	select {
	case err := <-attached:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reattach did not finish")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unload did not finish")
	}
	if got := runtimeForSession(t, m, snapshot.Session.ID); got != rt {
		t.Fatal("unload removed the reattached runtime")
	}
	if _, err := rt.agent.LoadSessionState(ctx); err != nil {
		t.Fatalf("reattached runtime was retired: %v", err)
	}
}

func TestActiveSnapshotKeepsUnclosedNativeTail(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snapshot, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	messages := []memory.Message{memory.NewTextMessage(model.RoleUser, "input"), {Role: model.RoleAssistant, ToolCalls: []memory.ToolCall{{ID: "pending", Name: "exec", Arguments: `{}`}}}}
	if err := m.root.SessionStateStore().Save(ctx, snapshot.Session.ID, "", messages); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := m.beginRun("owner"); err != nil {
		t.Fatal(err)
	}
	rt := runtimeForSession(t, m, snapshot.Session.ID)
	got, err := rt.agent.SnapshotState(ctx)
	if err != nil || len(got.Messages) != 2 || len(got.Messages[1].ToolCalls) != 1 {
		t.Fatalf("active snapshot = %+v, error = %v", got, err)
	}
	if recovered := memory.RecoverMessages(got.Messages); len(recovered[1].ToolCalls) != 0 {
		t.Fatal("test fixture was not an interrupted tail")
	}
}
