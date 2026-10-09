package chat

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func retainedTestDeps(calls *int) TranscriptDeps {
	return TranscriptDeps{Width: 80, MarkdownWidth: 72, Theme: "test",
		RenderSunaHeader: func(label string) string { return "header\n" },
		RenderAssistant: func(msg *Msg) string {
			*calls++
			text, _ := msg.Content.(string)
			if msg.Stream != nil {
				text = msg.Stream.Text()
			}
			msg.Render = MsgRenderCache{Width: 72, Theme: "test", ContentLen: len(text), LineCount: blockLines(text), Output: text}
			return text
		},
		RenderReasoning:   func(msg *Msg) string { text, _ := msg.Content.(string); return text },
		RenderUserMessage: func(v any, _ int) string { return fmt.Sprint(v) },
		RenderToolBlock: func(block *toolview.Block) string {
			var lines []string
			for _, id := range block.Order {
				entry := block.Entries[id]
				lines = append(lines, fmt.Sprintf("%s:%d:%s", id, entry.Status, entry.Result))
			}
			return strings.Join(lines, "\n")
		},
		RenderSubtaskBlock: func(block *toolview.Block) string { return "subtask" },
		RenderSkillLoad:    func(view *SkillLoadView) string { return view.Name + ":" + view.Status },
		RenderAskSelected:  func(opt string) string { return ">" + opt + "\n" },
		RenderAskOption:    func(opt string) string { return opt + "\n" },
		RenderAskHelp:      func(help string) string { return help + "\n" },
	}
}

func TestRetainedSyncOnlyRendersDirtyAndHydratesWindow(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(5)
	calls := 0
	deps := retainedTestDeps(&calls)
	for i := 0; i < 1000; i++ {
		m.AppendMessage(Msg{Role: "assistant", Content: fmt.Sprintf("answer-%d", i)})
	}
	m.SyncTranscript(deps)
	if !strings.Contains(m.Viewport.GetContent(), "answer-999") {
		t.Fatal("initial bottom hydration missing")
	}
	calls = 0
	m.SyncTranscript(deps)
	if calls != 0 {
		t.Fatalf("unchanged sync rendered %d messages", calls)
	}
	m.Messages[999].Content = "changed"
	m.MarkMessageDirty(999)
	m.SyncTranscript(deps)
	if calls != 1 {
		t.Fatalf("single changed message rendered %d messages", calls)
	}
	if !strings.Contains(m.Viewport.GetContent(), "changed") {
		t.Fatal("changed message not applied")
	}
	calls = 0
	m.SetTranscriptYOffset(0)
	if !strings.Contains(m.Viewport.GetContent(), "answer-0") {
		t.Fatal("scroll hydration missing")
	}
	if calls > 10 {
		t.Fatalf("scroll rendered %d messages", calls)
	}
	for i := 10; i < 990; i++ {
		slot, endSlot := m.transcriptMessageRange(i)
		for j := slot; j < endSlot; j++ {
			if m.TranscriptBlocks[j].Text != "" {
				t.Fatalf("offscreen transcript text retained for %d", i)
			}
		}
	}
	if got := m.SelectionPlainText(0, 10); !strings.Contains(got, "answer-0") {
		t.Fatalf("selection = %q", got)
	}
	if m.displayBudget.total != estimateMessagesDisplayBytes(m.Messages) {
		m.updateDisplayBudget()
		if m.displayBudget.total != estimateMessagesDisplayBytes(m.Messages) {
			t.Fatal("accounting diverged")
		}
	}
}

func TestRetainedLayoutMatchesStandaloneRenderer(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(8)
	calls := 0
	deps := retainedTestDeps(&calls)
	m.DisplayDiscard = DisplayDiscardSummary{Turns: 1, Messages: 2, Bytes: 20}
	m.AppendMessage(Msg{Role: "user", Content: "question"})
	m.AppendMessage(Msg{Role: "reasoning", Content: "thinking"})
	m.AppendMessage(Msg{Role: "assistant", Content: "answer\nsecond"})
	block := m.EnsureToolBlock()
	block.Add(&toolview.Entry{ID: "a", Status: toolview.StatusDone, Result: "done"})
	m.AppendMessage(Msg{Role: "skill", Content: &SkillLoadView{ID: "s", Name: "skill", Status: "loaded"}})
	m.AppendMessage(Msg{Role: "panel", Content: "panel"})
	m.EnqueueAskUser(protocol.AskUserParams{ID: "ask", Options: []string{"one", "two"}})
	for _, change := range []func(){func() {}, func() { m.Messages[1].Role = "panel"; m.MarkMessageDirty(1) }, func() { m.Messages[1].Role = "reasoning"; m.MarkMessageDirty(1) }, func() { m.Messages[2].Content = "long\nanswer\nchanged"; m.MarkMessageDirty(2) }} {
		change()
		m.SyncTranscript(deps)
		full, nav := m.RenderTranscriptWithNav(deps)
		m.hydrateTranscriptRange(0, m.TranscriptTotalLines)
		got := strings.Join(m.visibleTranscriptLines(0, m.TranscriptTotalLines), "\n")
		if got != full {
			t.Fatalf("retained output differs\ngot %q\nwant %q", got, full)
		}
		if m.TranscriptTotalLines != blockLines(full) {
			t.Fatalf("total = %d want %d", m.TranscriptTotalLines, blockLines(full))
		}
		if m.LastAssistantStartLine != nav.StartLine || m.LastAssistantLineCount != nav.LineCount {
			t.Fatalf("nav differs: %+v", nav)
		}
	}
}

func TestRetainedLateToolAndSkillEvents(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(20)
	calls := 0
	deps := retainedTestDeps(&calls)
	now := time.Now()
	m.StartTool(protocol.ToolStartParams{Tool: "readfile"}, "tool", now)
	m.StartSkillLoad(protocol.ToolStartParams{Tool: "skill_load", Params: map[string]any{"name": "skill"}}, "skill", now)
	m.SyncTranscript(deps)
	m.CurrentToolBlock = nil
	m.ActiveTools = map[string]*toolview.Entry{}
	m.EndTool(protocol.ToolEndParams{Result: "late result"}, "tool", now.Add(time.Second))
	m.SyncTranscript(deps)
	if !strings.Contains(m.Viewport.GetContent(), "late result") {
		t.Fatal("late result missing")
	}
	m.ActiveTools["skill"] = &toolview.Entry{ID: "skill", RawName: "skill_load", StartedAt: now}
	m.EndSkillLoad(protocol.ToolEndParams{}, "skill", now.Add(time.Second))
	m.SyncTranscript(deps)
	if !strings.Contains(m.Viewport.GetContent(), "skill:loaded") {
		t.Fatal("late skill completion missing")
	}
	m.updateDisplayBudget()
	if m.displayBudget.total != estimateMessagesDisplayBytes(m.Messages) {
		t.Fatal("event accounting differs")
	}
}

func TestHeightIndexAppendUpdateAndLookup(t *testing.T) {
	var h heightIndex
	h.reset(nil)
	random := rand.New(rand.NewSource(17))
	values := make([]int, 2000)
	for i := range values {
		values[i] = random.Intn(4)
		h.append(values[i])
	}
	for k := 0; k < 100; k++ {
		i, value := random.Intn(len(values)), random.Intn(8)
		values[i] = value
		h.set(i, value)
	}
	sum := 0
	for i, value := range values {
		if h.value(i) != value {
			t.Fatalf("point at %d differs", i)
		}
		if h.prefix(i) != sum {
			t.Fatalf("prefix at %d differs", i)
		}
		for line := sum; line < sum+value; line++ {
			if h.lowerBound(line) != i {
				t.Fatalf("lookup %d = %d want %d", line, h.lowerBound(line), i)
			}
		}
		sum += value
	}
	if h.prefix(len(values)) != sum || h.lowerBound(sum) != len(values) {
		t.Fatal("total/boundary differs")
	}
}

func TestRetainedBudgetMatchesOriginalTrim(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(3)
	for i := 0; i < 10; i++ {
		m.AppendMessage(Msg{Role: "user", Content: strings.Repeat("u", 100)})
		m.AppendMessage(Msg{Role: "panel", Content: strings.Repeat("a", 100)})
	}
	calls := 0
	deps := retainedTestDeps(&calls)
	m.SyncTranscript(deps)
	m.Messages[3].Content = strings.Repeat("changed", 100)
	m.MarkMessageDirty(3)
	legacy := Model{Messages: append([]Msg(nil), m.Messages...)}
	limit := 4000
	// 使用原估算值和 turn 边界计算预期，验证 low watermark 与丢弃摘要不变。
	used := estimateMessagesDisplayBytes(legacy.Messages)
	cutoff := 0
	discarded := DisplayDiscardSummary{}
	if used > limit {
		for used > limit*4/5 {
			next := nextUserMessageIndex(legacy.Messages, cutoff+1)
			if next < 0 {
				break
			}
			for i := cutoff; i < next; i++ {
				discarded.Messages++
				discarded.Bytes += estimateMsgDisplayBytes(&legacy.Messages[i])
				if legacy.Messages[i].Role == "user" {
					discarded.Turns++
				}
			}
			used -= estimateMessagesDisplayBytes(legacy.Messages[cutoff:next])
			cutoff = next
		}
	}
	if !m.TrimDisplayHistory(limit) {
		t.Fatal("expected trim")
	}
	if len(m.Messages) != len(legacy.Messages)-cutoff || m.DisplayDiscard != discarded {
		t.Fatalf("trim differs: %+v cutoff %d", m.DisplayDiscard, cutoff)
	}
	m.SyncTranscript(deps)
	if !strings.Contains(m.Viewport.GetContent(), strings.Repeat("a", 100)) {
		t.Fatal("trimmed history hydration missing")
	}
}

func TestRetainedMarkdownBudgetKeepsOriginalProtection(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(4)
	large := strings.Repeat("x", 2*1024*1024)
	for i := 0; i < 20; i++ {
		m.AppendMessage(Msg{Role: "panel", Content: fmt.Sprintf("message-%d", i), Render: MsgRenderCache{Output: large}})
	}
	m.SyncTranscript(TranscriptDeps{Width: 80})
	m.SetTranscriptYOffset(30)
	for i := range m.Messages {
		m.Messages[i].Render.Output = large
		m.MarkMessageDirty(i)
	}
	m.updateDisplayBudget()
	expected := Model{Messages: append([]Msg(nil), m.Messages...), TranscriptBlocks: append([]transcriptBlock(nil), m.TranscriptBlocks...), TranscriptWindowStart: m.TranscriptWindowStart, TranscriptWindowEnd: m.TranscriptWindowEnd}
	expected.trimMarkdownRenderCache()
	m.trimMarkdownRenderCache()
	for i := range m.Messages {
		if (m.Messages[i].Render.Output == "") != (expected.Messages[i].Render.Output == "") {
			t.Fatalf("cache protection differs for %d", i)
		}
	}
	if m.displayBudget.total != estimateMessagesDisplayBytes(m.Messages) {
		t.Fatal("trim cache accounting differs")
	}
}

func TestRetainedInvalidationReleasesHistoryReferences(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(4)
	calls := 0
	deps := retainedTestDeps(&calls)
	block := m.EnsureToolBlock()
	block.Add(&toolview.Entry{ID: "old", Status: toolview.StatusDone})
	m.SyncTranscript(deps)
	m.Messages[0] = Msg{Role: "panel", Content: "replacement"}
	m.MarkMessageDirty(0)
	m.SyncTranscript(deps)
	if len(m.transcript.blocks) != 0 || len(m.transcript.tools) != 0 {
		t.Fatal("replaced tool block reference retained")
	}
	m.Messages = []Msg{{Role: "assistant", Content: "same length history replacement"}}
	m.InvalidateTranscript()
	m.SyncTranscript(deps)
	if !strings.Contains(m.Viewport.GetContent(), "same length history replacement") {
		t.Fatal("invalidated history stale")
	}
	m.ResetRuntime()
	if m.transcript.ready || len(m.displayBudget.bytes) != 0 {
		t.Fatal("reset retained history")
	}
}

func TestRetainedViewportHeightDoesNotRebuildHistory(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(5)
	calls := 0
	deps := retainedTestDeps(&calls)
	for i := 0; i < 2000; i++ {
		m.AppendMessage(Msg{Role: "assistant", Content: fmt.Sprintf("answer-%d", i)})
	}
	m.SyncTranscript(deps)
	calls = 0
	m.Viewport.SetHeight(8)
	m.SyncTranscript(deps)
	if calls > 15 {
		t.Fatalf("height change rendered %d historical messages", calls)
	}
	if !strings.Contains(m.Viewport.GetContent(), "answer-1999") {
		t.Fatal("lost bottom after height change")
	}
	if len(m.transcript.ids) != 0 {
		t.Fatal("ordinary messages unnecessarily indexed by ID")
	}
}

func TestRetainedInitialSyncUsesOffscreenMetadata(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(5)
	calls := 0
	deps := retainedTestDeps(&calls)
	for i := 0; i < 1000; i++ {
		text := fmt.Sprintf("answer-%d", i)
		m.AppendMessage(Msg{Role: "assistant", Content: text, Render: MsgRenderCache{Width: 72, Theme: "test", ContentLen: len(text), LineCount: 1}})
	}
	m.SyncTranscript(deps)
	if calls > 15 {
		t.Fatalf("initial sync rendered %d cached historical messages", calls)
	}
	if !strings.Contains(m.Viewport.GetContent(), "answer-999") {
		t.Fatal("bottom not hydrated")
	}
}

func TestRetainedDuplicateToolOwnersAndLateEvents(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(30)
	calls := 0
	deps := retainedTestDeps(&calls)
	first := &toolview.Entry{ID: "dup", Status: toolview.StatusRunning}
	second := &toolview.Entry{ID: "dup", Status: toolview.StatusRunning}
	a := &toolview.Block{Entries: map[string]*toolview.Entry{"dup": first}, Order: []string{"dup"}}
	b := &toolview.Block{Entries: map[string]*toolview.Entry{"dup": second}, Order: []string{"dup"}}
	m.AppendMessage(Msg{Role: "tool", Content: a})
	m.AppendMessage(Msg{Role: "tool", Content: b})
	m.SyncTranscript(deps)
	// 逆序补水不能改变首个历史所有者。
	m.refreshTranscriptMessage(1, true)
	if m.FindTool("dup") != first {
		t.Fatal("historical owner is not first")
	}
	m.ApplyToolGuard(protocol.ToolGuardParams{ToolCallID: "dup", Decision: "allow"})
	if first.Guard == nil || second.Guard != nil || !m.transcript.dirty[0] {
		t.Fatal("late guard marked wrong owner")
	}
	m.SyncTranscript(deps)
	m.CurrentToolBlock = b
	m.ApplyToolGuard(protocol.ToolGuardParams{ToolCallID: "dup", Decision: "deny"})
	if second.Guard == nil || !m.transcript.dirty[1] {
		t.Fatal("current-block guard marked wrong owner")
	}
	m.SyncTranscript(deps)
	m.CurrentToolBlock = nil
	m.ActiveTools = map[string]*toolview.Entry{"dup": second}
	m.EndTool(protocol.ToolEndParams{Result: "second result"}, "dup", time.Now())
	if !m.transcript.dirty[1] || second.Status != toolview.StatusDone || first.Status != toolview.StatusRunning {
		t.Fatal("active duplicate ended wrong owner")
	}
	m.SyncTranscript(deps)
	m.EndTool(protocol.ToolEndParams{Result: "first result"}, "dup", time.Now())
	if !m.transcript.dirty[0] || first.Result != "first result" {
		t.Fatal("late historical end marked wrong owner")
	}
	m.Messages[0] = Msg{Role: "panel", Content: "replacement"}
	m.MarkMessageDirty(0)
	m.SyncTranscript(deps)
	if m.FindTool("dup") != second {
		t.Fatal("replacement lost duplicate surviving owner")
	}
}

func TestRetainedSelectionRestoresWindowAndBoundsOutputs(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(4)
	calls := 0
	deps := retainedTestDeps(&calls)
	for i := 0; i < 1000; i++ {
		m.AppendMessage(Msg{Role: "assistant", Content: fmt.Sprintf("answer-%d", i)})
	}
	m.SyncTranscript(deps)
	offset, content := m.TranscriptYOffset, m.Viewport.GetContent()
	calls = 0
	text := m.SelectionPlainText(0, m.TranscriptTotalLines-1)
	if !strings.Contains(text, "answer-0") || !strings.Contains(text, "answer-999") {
		t.Fatal("copy lost history")
	}
	if len(m.transcript.held) > 15 {
		t.Fatalf("copy retained %d messages", len(m.transcript.held))
	}
	if m.TranscriptYOffset != offset || m.Viewport.GetContent() != content {
		t.Fatal("copy changed viewport")
	}
	if calls > 1015 {
		t.Fatalf("copy rendered history twice: %d", calls)
	}
	for i := 0; i < 980; i++ {
		slot, endSlot := m.transcriptMessageRange(i)
		for j := slot; j < endSlot; j++ {
			if m.TranscriptBlocks[j].Text != "" {
				t.Fatal("copied offscreen text retained")
			}
		}
	}
}

func TestRetainedSubtaskEligibilityAndReasoningHeight(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(10)
	calls, toolCalls, reasoningCalls := 0, 0, 0
	deps := retainedTestDeps(&calls)
	deps.ReasoningHeight = 40
	deps.RenderReasoning = func(msg *Msg) string {
		reasoningCalls++
		if msg.Render.Output != "" {
			return msg.Render.Output
		}
		text := strings.Repeat("thinking\n", deps.ReasoningHeight/10)
		msg.Render = MsgRenderCache{Width: 80, Theme: "test", Mode: "reasoning_collapsed", LineCount: blockLines(text), Output: text}
		return text
	}
	deps.RenderSubtaskBlock = func(*toolview.Block) string { toolCalls++; return fmt.Sprint(deps.SubtaskKeys) }
	m.AppendMessage(Msg{Role: "reasoning", Content: "thinking"})
	block := m.EnsureToolBlock()
	m.ExpandedBlock, m.ExpandedBoxKind = block, boxKindSubtask
	m.SyncTranscript(deps)
	toolCalls, reasoningCalls = 0, 0
	deps.SubtaskKeys = true
	m.SyncTranscript(deps)
	if toolCalls == 0 || reasoningCalls != 0 {
		t.Fatal("eligibility change did not refresh only live block")
	}
	before := m.TranscriptTotalLines
	deps.ReasoningHeight = 80
	m.SyncTranscript(deps)
	if reasoningCalls == 0 || m.TranscriptTotalLines <= before {
		t.Fatal("terminal height kept stale reasoning layout")
	}
}

func TestRetainedFinishStreamingBeforeAndAfterSync(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(5)
	calls := 0
	deps := retainedTestDeps(&calls)
	for i := 0; i < 1000; i++ {
		m.AppendMessage(Msg{Role: "panel", Content: "history"})
	}
	m.SyncTranscript(deps)
	m.AppendStreamMessage("assistant", "one", time.Now())
	m.FinishStreamingMessages(time.Now())
	if m.Messages[1000].Streaming || m.Messages[1000].Stream != nil {
		t.Fatal("unsynced stream not finalized")
	}
	m.AppendStreamMessage("reasoning", "two", time.Now())
	m.SyncTranscript(deps)
	m.FinishStreamingMessages(time.Now())
	if m.Messages[1001].Streaming || m.Messages[1001].Content != "two" {
		t.Fatal("indexed stream not finalized")
	}
}

func BenchmarkRetainedTinyPanelsMetadata(b *testing.B) {
	for n := 0; n < b.N; n++ {
		var m Model
		m.InitComponents(ComponentDeps{})
		m.Viewport.SetHeight(5)
		for i := 0; i < 10000; i++ {
			m.AppendMessage(Msg{Role: "panel", Content: "x"})
		}
		m.SyncTranscript(TranscriptDeps{Width: 80})
	}
}

func TestRetainedOffscreenMutationAndToolAnchor(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(3)
	calls := 0
	deps := retainedTestDeps(&calls)
	m.AppendMessage(Msg{Role: "assistant", Content: "same"})
	block := m.EnsureToolBlock()
	block.Add(&toolview.Entry{ID: "tool", Status: toolview.StatusDone})
	for i := 0; i < 100; i++ {
		m.AppendMessage(Msg{Role: "panel", Content: "tail"})
	}
	m.SyncTranscript(deps)
	before := m.TranscriptTotalLines
	// 字节数相同但高度不同，离屏缓存元数据已经过期。
	m.Messages[0].Content = "s\na\n"
	m.MarkMessageDirty(0)
	m.SyncTranscript(deps)
	if m.TranscriptTotalLines <= before {
		t.Fatal("offscreen dirty assistant kept stale height")
	}
	if !m.RestoreTranscriptAnchor(TranscriptAnchor{MessageID: m.Messages[1].ID}) {
		t.Fatal("tool anchor lost compact ID index")
	}
}

func TestRetainedInitialLayoutTrimsOutputsIncrementally(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(3)
	large := strings.Repeat("x", 2*1024*1024)
	for i := 0; i < 30; i++ {
		m.AppendMessage(Msg{Role: "assistant", Content: "raw"})
	}
	deps := TranscriptDeps{Width: 80, MarkdownWidth: 72, Theme: "test", RenderAssistant: func(msg *Msg) string {
		used := 0
		for i := range m.Messages {
			used += len(m.Messages[i].Render.Output)
		}
		if used > markdownRenderCacheBudgetBytes {
			t.Fatalf("initial layout allowed output high-water %d", used)
		}
		msg.Render = MsgRenderCache{Width: 72, Theme: "test", ContentLen: 3, LineCount: 1, Output: large}
		return large
	}}
	m.SyncTranscript(deps)
	m.SelectionPlainText(0, m.TranscriptTotalLines-1)
	m.updateDisplayBudget()
	if m.displayBudget.outputBytes > markdownRenderCacheBudgetBytes {
		t.Fatal("copy left over-budget outputs")
	}
	if len(m.transcript.held) > 10 {
		t.Fatal("wide copy retained offscreen output text")
	}
}

// 以结构尺寸和实际容量约束小消息元数据，避免依赖运行时堆测量波动。
func TestRetainedTinyHistoryMetadata(t *testing.T) {
	if unsafe.Sizeof(transcriptSource{}) > 8 || unsafe.Sizeof(transcriptBlock{}) > 32 {
		t.Fatalf("metadata grew: source=%d block=%d", unsafe.Sizeof(transcriptSource{}), unsafe.Sizeof(transcriptBlock{}))
	}
	for _, count := range []int{1000, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var m Model
			m.InitComponents(ComponentDeps{})
			m.Viewport.SetHeight(5)
			deps := TranscriptDeps{Width: 80}
			// 模拟先初始化空会话，再批量恢复历史。
			m.SyncTranscript(deps)
			for i := 0; i < count; i++ {
				m.AppendMessage(Msg{Role: "panel", Content: "x"})
			}
			m.SyncTranscript(deps)
			if len(m.TranscriptBlocks) != count+2 || len(m.transcript.sources) != count {
				t.Fatal("tiny messages must use one slot each")
			}
			if len(m.transcript.owners) != 0 || len(m.displayBudget.outputs.tree) != 0 {
				t.Fatal("tiny messages must not allocate ownership or output indices")
			}
			bytes := uintptr(cap(m.TranscriptBlocks))*unsafe.Sizeof(transcriptBlock{}) +
				uintptr(cap(m.transcript.sources))*unsafe.Sizeof(transcriptSource{}) +
				uintptr(cap(m.transcript.heights.tree)+cap(m.displayBudget.bytes))*unsafe.Sizeof(int(0))
			if bytes > uintptr(count)*56+128 {
				t.Fatalf("tiny metadata capacity=%d exceeds compact representation", bytes)
			}
			if m.TranscriptTotalLines != count*3 || m.displayBudget.total != estimateMessagesDisplayBytes(m.Messages) {
				t.Fatal("compaction changed layout or display accounting")
			}
			m.Messages[count/2].Render.Output = "first cached output"
			m.MarkMessageDirty(count / 2)
			m.updateDisplayBudget()
			if m.displayBudget.outputBytes != len("first cached output") {
				t.Fatal("lazy output index did not account its first value")
			}
			m.AppendMessage(Msg{Role: "panel", Content: "tail", Render: MsgRenderCache{Output: "second"}})
			m.updateDisplayBudget()
			if m.displayBudget.outputBytes != len("first cached output")+len("second") {
				t.Fatal("lazy output index did not grow with appended messages")
			}
		})
	}
}
