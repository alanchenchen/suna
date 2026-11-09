package daemon

import (
	"context"
	"encoding/json"
	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/runner"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type captureEventSink struct {
	mu     sync.Mutex
	events []protocol.Event
}

func (s *captureEventSink) Emit(_ context.Context, event protocol.Event) error {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	return nil
}

func (s *captureEventSink) Events() []protocol.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Event(nil), s.events...)
}

func TestServiceKeepsPendingInteractionsWhenRuntimeDetached(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)

	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	svc.pendingAsks.Store("ask", pendingInteraction{sessionID: snapshot.Session.ID, reply: make(chan string, 1)})
	svc.pendingGuards.Store("guard", pendingInteraction{sessionID: snapshot.Session.ID, reply: make(chan string, 1)})

	// detach 不取消正在执行的 run，等待中的 ask/guard 交互必须保留：
	// attach 回来的客户端仍可回复，run 才能继续；清理发生在 run 结束路径。
	manager.detach("client-a")
	if _, ok := svc.pendingAsks.Load("ask"); !ok {
		t.Fatal("pending ask was removed on detach, want kept for resumed client")
	}
	if _, ok := svc.pendingGuards.Load("guard"); !ok {
		t.Fatal("pending guard was removed on detach, want kept for resumed client")
	}
}

func TestRunLifecycleBroadcastsCatalogStateWithoutLeakingDetailedEvents(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	_, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "catalog-observer": observerSink}}
	svc := newService(d)
	_, sessionID, runID, err := svc.beginAgentRun(ctx, "client-a")
	if err != nil {
		t.Fatalf("beginAgentRun error = %v", err)
	}
	if !receivedSessionStatus(observerSink.Events(), sessionID, protocol.SessionStatusRunning, 1) {
		t.Fatalf("unattached observer did not receive running catalog state: %#v", observerSink.Events())
	}
	if receivedMethod(observerSink.Events(), protocol.NotifyAgentRun) {
		t.Fatalf("unattached observer received agent.run: %#v", observerSink.Events())
	}

	events := make(chan agent.Event, 6)
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusWaitingLLM}
	events <- agent.Event{Type: agent.EventStream, Content: "private output"}
	events <- agent.Event{Type: agent.EventToolCall, ToolCallID: "tool-1", ToolName: "test-tool"}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)
	svc.runAgentEvents(ctx, "client-a", sessionID, runID, "input", events, ownerSink)

	if !receivedSessionStatus(observerSink.Events(), sessionID, protocol.SessionStatusIdle, 1) {
		t.Fatalf("unattached observer did not receive idle catalog state: %#v", observerSink.Events())
	}
	for _, method := range []string{protocol.NotifyAgentRun, protocol.NotifyAgentDelta, protocol.NotifyToolStart} {
		if receivedMethod(observerSink.Events(), method) {
			t.Fatalf("unattached observer received detailed %s event: %#v", method, observerSink.Events())
		}
	}
	for _, method := range []string{protocol.NotifyAgentRun, protocol.NotifyAgentDelta, protocol.NotifyToolStart} {
		if !receivedMethod(ownerSink.Events(), method) {
			t.Fatalf("attached owner did not receive detailed %s event: %#v", method, ownerSink.Events())
		}
	}
}

func TestResumeRunBroadcastsRunningCatalogStateToUnattachedObserver(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "catalog-observer": observerSink}}
	svc := newService(d)
	if _, err := svc.handleResumeRun(ctx, protocol.Request{ConnID: "client-a"}, ownerSink); err != nil {
		t.Fatalf("resume error = %v", err)
	}

	if !receivedSessionStatus(observerSink.Events(), snapshot.Session.ID, protocol.SessionStatusRunning, 1) {
		t.Fatalf("unattached observer did not receive resume running state: %#v", observerSink.Events())
	}
	if receivedMethod(observerSink.Events(), protocol.NotifyAgentRun) {
		t.Fatalf("unattached observer received resume agent.run: %#v", observerSink.Events())
	}
}

func TestHandleCompactBroadcastsCatalogStateToUnattachedObserver(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "catalog-observer": observerSink}}
	svc := newService(d)
	if _, err := svc.handleCompact(ctx, protocol.Request{ConnID: "client-a"}, ownerSink); err != nil {
		t.Fatalf("compact error = %v", err)
	}

	if !receivedSessionStatus(observerSink.Events(), snapshot.Session.ID, protocol.SessionStatusCompacting, 1) {
		t.Fatalf("unattached observer did not receive compacting catalog state: %#v", observerSink.Events())
	}
	// 异步化后结果由独立 goroutine 发送；轮询等待终态，避免竞态断言。
	if !waitForCompactResult(t, ownerSink) {
		t.Fatalf("attached owner did not receive compact result: %#v", ownerSink.Events())
	}
	if !receivedSessionStatus(observerSink.Events(), snapshot.Session.ID, protocol.SessionStatusIdle, 1) {
		t.Fatalf("unattached observer did not receive compact idle state: %#v", observerSink.Events())
	}
	if receivedMethod(observerSink.Events(), protocol.NotifyCompactState) {
		t.Fatalf("unattached observer received compact details: %#v", observerSink.Events())
	}
}

func waitForCompactResult(t *testing.T, sink *captureEventSink) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range sink.Events() {
			if event.Method != protocol.NotifyCompactState {
				continue
			}
			params, ok := event.Params.(protocol.CompactState)
			if ok && (params.State == "applied" || params.State == "skipped" || params.State == "failed" || params.State == "cancelled") {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func receivedSessionStatus(events []protocol.Event, sessionID string, want protocol.SessionStatus, clientCount int) bool {
	for _, event := range events {
		params, ok := event.Params.(protocol.SessionStateParams)
		if event.Method == protocol.NotifySessionUpdated && ok && params.Session.ID == sessionID && params.Session.Status == want && params.Session.ClientCount == clientCount {
			return true
		}
	}
	return false
}

func receivedMethod(events []protocol.Event, method string) bool {
	for _, event := range events {
		if event.Method == method {
			return true
		}
	}
	return false
}

func TestRunAgentEventsKeepsSessionBusyUntilEventStreamCloses(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}

	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	events := make(chan agent.Event)
	done := make(chan struct{})
	go func() {
		svc.runAgentEvents(ctx, "client-a", snapshot.Session.ID, "run-1", "input", events, &captureEventSink{})
		close(done)
	}()

	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	if _, _, _, err := manager.beginRun("client-a"); err == nil {
		t.Fatal("beginRun before event stream close error = nil, want session_busy")
	}

	close(events)
	<-done
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun after event stream close error = %v", err)
	}
}

func TestCancellingSuppressesNonCancelledRunStates(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	runID := manager.currentRunID(snapshot.Session.ID)
	sink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": sink}}
	svc := newService(d)

	var publishCalls int
	newlyMarked, err := manager.markCancelling("client-a", func(_ *sessionRuntime, sessionID, gotRunID string, phase protocol.AgentRunPhase) {
		publishCalls++
		svc.emitAgentRun(ctx, sessionID, "client-a", protocol.AgentRunParams{RunID: gotRunID, State: protocol.AgentRunCancelling, Phase: phase})
	})
	if err != nil || !newlyMarked {
		t.Fatalf("first cancel = %v, %v", newlyMarked, err)
	}
	newlyMarked, err = manager.markCancelling("client-a", func(*sessionRuntime, string, string, protocol.AgentRunPhase) { publishCalls++ })
	if err != nil || newlyMarked || publishCalls != 1 {
		t.Fatalf("duplicate cancel = %v, %v, calls=%d", newlyMarked, err, publishCalls)
	}

	events := make(chan agent.Event, 3)
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusWaitingLLM}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusLLMRetrying}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)
	svc.runAgentEvents(ctx, "client-a", snapshot.Session.ID, runID, "input", events, sink)

	var states []protocol.AgentRunState
	for _, event := range sink.Events() {
		if event.Method != protocol.NotifyAgentRun {
			continue
		}
		params := event.Params.(protocol.AgentRunParams)
		states = append(states, params.State)
		if params.State == protocol.AgentRunCancelling && params.CanControl {
			t.Fatal("cancelling CanControl = true")
		}
	}
	want := []protocol.AgentRunState{protocol.AgentRunCancelling, protocol.AgentRunCancelled}
	if len(states) != len(want) || states[0] != want[0] || states[1] != want[1] {
		t.Fatalf("states = %#v, want %#v", states, want)
	}
}

func TestManualCompactReturnsStableIDAndReleasesSessionBeforeTerminal(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snap, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	sink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": sink}}
	svc := newService(d)
	result, err := svc.handleCompact(ctx, protocol.Request{ConnID: "client-a"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	accepted := result.(protocol.CompactAccepted)
	if accepted.CompactID == "" {
		t.Fatal("empty accepted ID")
	}
	if !waitForCompactResult(t, sink) {
		t.Fatal("missing compact terminal")
	}
	running := false
	for _, event := range sink.Events() {
		if p, ok := event.Params.(protocol.AgentRunParams); ok && p.State == protocol.AgentRunRunning {
			running = p.CanControl && p.Phase == protocol.AgentRunPhaseCompact
		}
		if p, ok := event.Params.(protocol.CompactState); ok && (p.State == "skipped" || p.State == "failed" || p.State == "applied" || p.State == "cancelled") {
			if p.CompactID != accepted.CompactID {
				t.Fatalf("got ID %q, want %q", p.CompactID, accepted.CompactID)
			}
		}
	}
	if !running {
		t.Fatal("manual owner control was not published")
	}
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("session remained busy after terminal: %v", err)
	}
	manager.setStatus(snap.Session.ID, sessionIdle)
}

type compactAdmissionSink struct {
	manager *sessionManager
	runtime *sessionRuntime
	mu      sync.Mutex
	states  []protocol.CompactState
	busy    bool
}

func (s *compactAdmissionSink) Emit(_ context.Context, event protocol.Event) error {
	if event.Method != protocol.NotifyCompactState {
		return nil
	}
	state, ok := event.Params.(protocol.CompactState)
	if !ok {
		return nil
	}
	terminal := state.State == "applied" || state.State == "skipped" || state.State == "failed" || state.State == "cancelled"
	busy := false
	if terminal {
		s.manager.mu.RLock()
		busy = s.runtime.status != sessionIdle
		s.manager.mu.RUnlock()
		// 直接检查准入，不能在通知回调里递归取得同一 lifecycle 锁。
		if _, err := s.runtime.agent.LoadSessionState(context.Background()); err != nil {
			busy = true
		}
	}
	s.mu.Lock()
	s.states = append(s.states, state)
	s.busy = s.busy || busy
	s.mu.Unlock()
	return nil
}

func TestManualCompactAcceptsSameIDAndReleasesAdmissionBeforeTerminal(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	snapshot, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtimeForSession(t, m, snapshot.Session.ID)
	sink := &compactAdmissionSink{manager: m, runtime: rt}
	d := &Daemon{sessions: m, sinks: map[string]protocol.EventSink{"owner": sink}}
	svc := newService(d)
	result, err := svc.handleCompact(ctx, protocol.Request{ConnID: "owner"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	accepted, ok := result.(protocol.CompactAccepted)
	if !ok || accepted.CompactID == "" {
		t.Fatalf("accepted = %#v, want compact ID", result)
	}
	deadline := time.Now().Add(time.Second)
	for {
		sink.mu.Lock()
		states := append([]protocol.CompactState(nil), sink.states...)
		busy := sink.busy
		sink.mu.Unlock()
		if len(states) > 0 {
			state := states[len(states)-1]
			if state.State == "skipped" {
				if state.CompactID != accepted.CompactID || state.Wait != "" || state.Reason != "no_source" || busy {
					t.Fatalf("terminal = %+v, busy = %v, accepted ID = %q", state, busy, accepted.CompactID)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("manual compact did not finish: %+v", states)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUserSavedBroadcastsValidatedImagePartsToSameSessionObserver(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	created, err := m.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.attach(ctx, "observer", created.Session.ID, false); err != nil {
		t.Fatal(err)
	}
	rt := runtimeForSession(t, m, created.Session.ID)
	store := rt.agent.MediaStore()
	if err := os.MkdirAll(store.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	// macOS 的临时目录可能经过符号链接；附件边界与校验后的真实路径保持一致。
	root, err := filepath.EvalSymlinks(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	store.Root = root
	attachmentPath := filepath.Join(root, "upload.png")
	pathImage := filepath.Join(root, "local.jpg")
	fixture := []byte("image fixture")
	for _, path := range []string{attachmentPath, pathImage} {
		if err := os.WriteFile(path, fixture, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	ownerSink, observerSink, catalogSink := &captureEventSink{}, &captureEventSink{}, &captureEventSink{}
	d := &Daemon{sessions: m, sinks: map[string]protocol.EventSink{"owner": ownerSink, "observer": observerSink, "catalog": catalogSink}}
	svc := newService(d)
	params := protocol.SendMessageParams{Parts: []protocol.MessagePart{
		{Type: "text", Text: "  inspect these images\n"},
		{Type: "image", Source: protocol.AttachmentRef{Kind: "attachment", Path: attachmentPath, Name: "uploaded label", MimeType: "image/gif", Size: 999}},
		{Type: "text", Text: " \t "},
		{Type: "image", Source: protocol.AttachmentRef{Kind: "path", Path: pathImage}},
		{Type: "image", Source: protocol.AttachmentRef{Kind: "url", URL: "https://example.com/image.webp?original=1", Size: 42}},
	}}
	input, err := svc.agentInputFromParams(ctx, rt.agent, params)
	if err != nil {
		t.Fatal(err)
	}
	_, sessionID, runID, err := svc.beginAgentRun(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if receivedMethod(observerSink.Events(), protocol.NotifySessionUserMessage) {
		t.Fatal("user message was broadcast before EventUserSaved")
	}
	// 只消费已保存输入通知，不执行 Agent 或任何模型请求。
	events := make(chan agent.Event, 2)
	events <- agent.Event{Type: agent.EventUserSaved, UserInput: &input}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)
	svc.runAgentEvents(ctx, "owner", sessionID, runID, "images", events, ownerSink)

	want := []protocol.MessagePart{
		{Type: "text", Text: "  inspect these images\n"},
		{Type: "image", Source: protocol.AttachmentRef{Kind: "attachment", Path: attachmentPath, Name: "uploaded label", MimeType: "image/png", Size: int64(len(fixture))}},
		{Type: "text", Text: " \t "},
		{Type: "image", Source: protocol.AttachmentRef{Kind: "path", Path: pathImage, Name: "local.jpg", MimeType: "image/jpeg", Size: int64(len(fixture))}},
		{Type: "image", Source: protocol.AttachmentRef{Kind: "url", URL: "https://example.com/image.webp?original=1", Name: "image.webp", MimeType: "image/webp", Size: 42}},
	}
	count := 0
	for _, event := range observerSink.Events() {
		if event.Method != protocol.NotifySessionUserMessage {
			continue
		}
		count++
		got, ok := event.Params.(protocol.UserMessageParams)
		if !ok || got.SessionID != sessionID || !reflect.DeepEqual(got.Parts, want) {
			t.Fatalf("observer user message = %#v, want original validated parts %#v", event.Params, want)
		}
	}
	if count != 1 {
		t.Fatalf("observer user message count = %d, want 1", count)
	}
	for name, sink := range map[string]*captureEventSink{"owner": ownerSink, "catalog": catalogSink} {
		if receivedMethod(sink.Events(), protocol.NotifySessionUserMessage) {
			t.Fatalf("%s received observer-only user message", name)
		}
	}
}

func TestInputProtocolPartsIgnoresStoredProjectionAndInvalidBlocks(t *testing.T) {
	input := agent.Input{
		Blocks: []model.ContentBlock{
			{Type: model.ContentText, Text: " exact text \n"},
			{Type: model.ContentImage},
			{Type: model.ContentBlockType("unsupported"), Text: "not a message part"},
		},
		StoredBlocks: []model.ContentBlock{{Type: model.ContentText, Text: "stored projection only"}},
	}
	want := []protocol.MessagePart{{Type: "text", Text: " exact text \n"}}
	if got := inputProtocolParts(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %#v, want %#v", got, want)
	}
}

func TestHardWaitCompactStatusMatchesAttachAndResumesOnWaitingLLM(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	if _, err := m.create(ctx, "owner", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	ownerSink, catalogSink := &captureEventSink{}, &captureEventSink{}
	d := &Daemon{sessions: m, sinks: map[string]protocol.EventSink{"owner": ownerSink, "catalog": catalogSink}}
	svc := newService(d)
	rt, sessionID, runID, err := svc.beginAgentRun(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	state := runner.CompactState{ID: "hard-wait", State: "preparing", Wait: "hard_limit"}
	svc.handleMemoryState(sessionID, rt, state)
	svc.handleMemoryState(sessionID, rt, state)
	joined, err := m.attach(ctx, "observer", sessionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Session.Status != protocol.SessionStatusCompacting || joined.CurrentRun == nil || joined.CurrentRun.Status != protocol.SessionStatusCompacting || joined.CurrentRun.Phase != protocol.AgentRunPhaseCompact || joined.CurrentRun.RunID != runID {
		t.Fatalf("hard-wait attach = %+v, current run = %+v", joined.Session, joined.CurrentRun)
	}
	compactCount := 0
	for _, event := range ownerSink.Events() {
		if p, ok := event.Params.(protocol.AgentRunParams); ok && p.State == protocol.AgentRunRunning && p.Phase == protocol.AgentRunPhaseCompact {
			compactCount++
		}
	}
	if compactCount != 1 || !receivedSessionStatus(catalogSink.Events(), sessionID, protocol.SessionStatusCompacting, 1) {
		t.Fatalf("compact notifications = %d, catalog = %#v", compactCount, catalogSink.Events())
	}
	if receivedMethod(catalogSink.Events(), protocol.NotifyCompactState) {
		t.Fatal("unattached catalog received compact details")
	}

	beforeResume := len(ownerSink.Events())
	events := make(chan agent.Event, 2)
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusWaitingLLM}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)
	svc.runAgentEvents(ctx, "owner", sessionID, runID, "resume model", events, ownerSink)
	resumed := ownerSink.Events()[beforeResume:]
	modelPhase := false
	for _, event := range resumed {
		if p, ok := event.Params.(protocol.AgentRunParams); ok && p.State == protocol.AgentRunRunning && p.Phase == protocol.AgentRunPhaseModel {
			modelPhase = true
		}
	}
	if !modelPhase || !receivedSessionStatus(resumed, sessionID, protocol.SessionStatusRunning, 2) {
		t.Fatalf("StatusWaitingLLM did not restore model/running: %#v", resumed)
	}
}

func TestHardWaitDoesNotChangeCancellingRunPhaseOrStatus(t *testing.T) {
	ctx := context.Background()
	m := newTestSessionManager(t)
	if _, err := m.create(ctx, "owner", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	sink := &captureEventSink{}
	svc := newService(&Daemon{sessions: m, sinks: map[string]protocol.EventSink{"owner": sink}})
	rt, sessionID, _, err := svc.beginAgentRun(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if marked, err := m.markCancelling("owner", func(*sessionRuntime, string, string, protocol.AgentRunPhase) {}); err != nil || !marked {
		t.Fatalf("markCancelling = %v, %v", marked, err)
	}
	svc.handleMemoryState(sessionID, rt, runner.CompactState{ID: "late-wait", State: "preparing", Wait: "hard_limit"})
	joined, err := m.attach(ctx, "observer", sessionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if joined.CurrentRun == nil || joined.CurrentRun.State != protocol.AgentRunCancelling || joined.CurrentRun.Phase != protocol.AgentRunPhaseModel || joined.CurrentRun.Status != protocol.SessionStatusRunning {
		t.Fatalf("cancelling run changed on hard wait: %+v", joined.CurrentRun)
	}
}

func TestSnapshotForConnPreservesExactTextAndOnlyFiltersBlankNonTools(t *testing.T) {
	for _, tt := range []struct {
		name string
		msg  memory.Message
		skip bool
	}{
		{name: "tool diagnostic", msg: memory.Message{Role: model.RoleTool, Content: []model.ContentBlock{{Type: model.ContentText, Text: "\n\t stderr: exact detail \r\n  "}}, ToolCallID: "tool-1", ToolStatus: "failed", Truncated: true}},
		{name: "whitespace tool result", msg: memory.NewTextMessage(model.RoleTool, " \t\r\n")},
		{name: "empty tool result", msg: memory.NewTextMessage(model.RoleTool, "")},
		{name: "user text", msg: memory.NewTextMessage(model.RoleUser, "  exact prompt \n")},
		{name: "assistant text", msg: memory.NewTextMessage(model.RoleAssistant, "\n precise explanation\t")},
		{name: "blank user", msg: memory.NewTextMessage(model.RoleUser, " \t\n"), skip: true},
		{name: "blank assistant", msg: memory.NewTextMessage(model.RoleAssistant, " \r\n"), skip: true},
		{name: "blank assistant with tool call", msg: memory.Message{Role: model.RoleAssistant, TextContent: " \t\n", ToolCalls: []memory.ToolCall{{ID: "tool-2", Name: "exec", Arguments: `{}`, Intent: "diagnose"}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &sessionManager{runtime: map[string]*sessionRuntime{}}
			rt := newSessionRuntime(&agent.Agent{})
			snapshot := m.snapshotForConn("observer", memory.SessionMeta{ID: "session"}, rt, agent.SessionSnapshot{Messages: []memory.Message{tt.msg}})
			if tt.skip {
				if len(snapshot.Messages) != 0 {
					t.Fatalf("blank non-tool message was retained: %#v", snapshot.Messages)
				}
				return
			}
			if len(snapshot.Messages) != 1 {
				t.Fatalf("message count = %d, want 1", len(snapshot.Messages))
			}
			got := snapshot.Messages[0]
			if got.Content != tt.msg.Text() || got.ToolCallID != tt.msg.ToolCallID || got.ToolStatus != tt.msg.ToolStatus || got.Truncated != tt.msg.Truncated {
				t.Fatalf("snapshot message = %#v, source = %#v", got, tt.msg)
			}
			if len(got.ToolCalls) != len(tt.msg.ToolCalls) {
				t.Fatalf("tool calls = %#v, want %#v", got.ToolCalls, tt.msg.ToolCalls)
			}
			for i, call := range got.ToolCalls {
				if !reflect.DeepEqual(memory.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Intent: call.Intent}, tt.msg.ToolCalls[i]) {
					t.Fatalf("tool call = %#v, want %#v", call, tt.msg.ToolCalls[i])
				}
			}
		})
	}
}

func TestSnapshotProjectsObservationLabelsWithoutModelEvidence(t *testing.T) {
	m := &sessionManager{runtime: map[string]*sessionRuntime{}}
	rt := newSessionRuntime(&agent.Agent{})
	for _, prose := range []string{"original prose", ""} {
		message := memory.NewTextMessage(model.RoleAssistant, prose)
		message.Observation = &memory.Observation{Text: "model-only exact evidence", Calls: []memory.ToolLabel{{Name: "exec", Intent: "verify build", Status: "failed"}, {Name: "readfile", Status: "completed"}}}
		snapshot := m.snapshotForConn("observer", memory.SessionMeta{ID: "session"}, rt, agent.SessionSnapshot{Messages: []memory.Message{message}})
		if len(snapshot.Messages) != 1 {
			t.Fatalf("messages = %#v, want retained observation even with empty prose", snapshot.Messages)
		}
		got := snapshot.Messages[0]
		want := []protocol.SnapshotToolLabel{{Name: "exec", Intent: "verify build", Status: "failed"}, {Name: "readfile", Status: "completed"}}
		if got.Content != prose || !reflect.DeepEqual(got.ObservedTools, want) || len(got.ToolCalls) != 0 || got.ToolCallID != "" {
			t.Fatalf("UI projection = %#v, want prose and minimal labels", got)
		}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "model-only exact evidence") || strings.Contains(string(raw), `"arguments"`) || strings.Contains(string(raw), `"result"`) {
			t.Fatalf("UI snapshot retained removed tool detail: %s", raw)
		}
	}
}
