package tui

import (
	"fmt"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSessionSwitchClearsNativeListState(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()
	tui.currentSession = protocol.SessionInfo{ID: "session-a"}
	tui.chat.SetSkills([]protocol.SkillInfo{{Name: "previous-skill", Valid: true}})
	tui.chat.SetMCPServers([]protocol.MCPServerInfo{{Name: "previous-server", State: protocol.MCPServerActive}})
	tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())
	tui.chat.SkillsList.List().SetFilterText("previous")
	tui.chat.MCPList.List().SetFilterText("previous")

	tui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session-b"}})

	if got := tui.chat.MCPList.List().FilterValue(); got != "" {
		t.Fatalf("MCP filter after session switch = %q, want empty", got)
	}
}

func TestSessionSnapshotDoesNotReactivateCompletedRun(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80, height: 18}
	tui.initChatComponents()
	tui.currentSession = protocol.SessionInfo{ID: "session-1"}
	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunDone})

	tui.applySessionSnapshot(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning},
		CurrentRun: &protocol.CurrentRunView{
			RunID:  "run-1",
			Status: protocol.SessionStatusRunning,
		},
	})

	if tui.chat.Loading {
		t.Fatal("Loading = true after stale completed run snapshot, want false")
	}
	if tui.currentRunCanControl {
		t.Fatal("currentRunCanControl = true after stale completed run snapshot, want false")
	}
}

func TestSessionSnapshotKeepsLegacyRunWithoutIdentity(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80, height: 18}
	tui.initChatComponents()
	tui.currentSession = protocol.SessionInfo{ID: "session-1"}
	tui.completedRunID = "run-1"

	tui.applySessionSnapshot(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning},
		CurrentRun: &protocol.CurrentRunView{
			Status: protocol.SessionStatusRunning,
		},
	})

	if !tui.chat.Loading {
		t.Fatal("Loading = false for legacy run snapshot, want true")
	}
}

func TestStreamErrorUsesStructuredResumeFlag(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()

	tui.handleAgentRunNotification(protocol.AgentRunParams{State: protocol.AgentRunFailed, Error: &protocol.ModelError{Kind: protocol.ModelErrorHTTP, StatusCode: 502, Message: "Bad Gateway"}, ResumeAvailable: true})

	if !tui.chat.ResumeAvailable {
		t.Fatal("ResumeAvailable = false, want true")
	}
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("messages = %d, want 1", got)
	}
	if got := tui.chat.Messages[0].Role; got != "error" {
		t.Fatalf("message role = %q, want error", got)
	}
	content, _ := tui.chat.Messages[0].Content.(string)
	if !strings.Contains(content, "模型请求失败：HTTP 502") || !strings.Contains(content, "按 Enter") {
		t.Fatalf("message content = %v, want structured retryable model error", content)
	}
}

func TestSessionModelUnavailableErrorUsesLocalizedRecoveryGuidance(t *testing.T) {
	for _, tt := range []struct {
		name   string
		locale LocaleID
		wants  []string
	}{
		{name: "Chinese", locale: LocaleZH, wants: []string{"当前会话使用的模型「openai/gpt-5.6」已不存在", "请使用 /model"}},
		{name: "English", locale: LocaleEN, wants: []string{"openai/gpt-5.6", "Use /model"}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tui := &TUI{i18n: newTranslator(tt.locale), width: 80, height: 18}
			tui.initChatComponents()
			tui.handleAgentRunNotification(protocol.AgentRunParams{
				State:    protocol.AgentRunFailed,
				RunError: &protocol.RunError{Kind: protocol.RunErrorSessionModelUnavailable, ModelRef: "openai/gpt-5.6"},
			})
			if got := len(tui.chat.Messages); got != 1 {
				t.Fatalf("messages = %d, want 1", got)
			}
			content, _ := tui.chat.Messages[0].Content.(string)
			for _, want := range tt.wants {
				if !strings.Contains(content, want) {
					t.Fatalf("message content = %q, want %q", content, want)
				}
			}
		})
	}
}

func TestRestoreStatusShowsCompactedNoticeAtEnd(t *testing.T) {
	for _, tt := range []struct {
		name   string
		locale LocaleID
		want   []string
	}{
		{name: "English", locale: LocaleEN, want: []string{"summarized for the model", "no longer shown in full"}},
		{name: "Chinese", locale: LocaleZH, want: []string{"已生成摘要供模型参考", "不再完整展示原文"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tui := &TUI{i18n: newTranslator(tt.locale), width: 80, height: 18}
			tui.initChatComponents()
			tui.applySessionSnapshot(protocol.SessionSnapshot{Messages: []protocol.SnapshotMessage{{Role: "user", Content: "earlier request"}}, Compacted: true})
			if got := len(tui.chat.Messages); got != 2 {
				t.Fatalf("messages = %d, want 2", got)
			}
			last := tui.chat.Messages[len(tui.chat.Messages)-1]
			if last.Role != "system" {
				t.Fatalf("last role = %q, want system", last.Role)
			}
			content, _ := last.Content.(string)
			for _, want := range tt.want {
				if !strings.Contains(content, want) {
					t.Fatalf("got compacted notice = %q, want %q", content, want)
				}
			}
		})
	}
}

func TestActiveRestoredToolAcceptsSavedResultWithoutReplayingStart(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleZH), currentSession: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusRunning}}
	ui.initChatComponents()
	ui.restoreToolMessage(protocol.SnapshotMessage{Role: "assistant", ToolCalls: []protocol.SnapshotToolCall{{ID: "call", Name: "readfile", Arguments: `{}`}}})
	ui.handleToolEndNotification(protocol.ToolEndParams{SessionID: "s", ID: "call", Tool: "readfile", Result: "saved result"})
	got := ui.chat.FindTool("call")
	if got == nil || got.Result != "saved result" || got.Status != toolDone || !got.StartedAt.IsZero() {
		t.Fatalf("restored entry=%+v", got)
	}
}

func TestRestoreToolResultsUseLiveDisplayLimit(t *testing.T) {
	// 这是 daemon 的 tool_end 上限，与 transcript 内存预算无关。
	const liveLimit = 16 * 1024
	cases := []struct {
		name, content string
		truncated     bool
		want          string
		wantTruncated bool
	}{
		{name: "raw failure", content: "provider: exact_error at example.go:12\nraw line", want: "provider: exact_error at example.go:12\nraw line"},
		{name: "exact limit", content: strings.Repeat("x", liveLimit), want: strings.Repeat("x", liveLimit)},
		{name: "ascii beyond limit", content: strings.Repeat("x", liveLimit) + "z", want: strings.Repeat("x", liveLimit), wantTruncated: true},
		{name: "split utf8", content: strings.Repeat("x", liveLimit-1) + "界END", want: strings.Repeat("x", liveLimit-1), wantTruncated: true},
		{name: "utf8 at boundary", content: strings.Repeat("x", liveLimit-3) + "界END", want: strings.Repeat("x", liveLimit-3) + "界", wantTruncated: true},
		{name: "upstream truncated", content: "original prefix", truncated: true, want: "original prefix", wantTruncated: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ui := &TUI{i18n: newTranslator(LocaleEN), width: 90, height: 24}
			ui.initChatComponents()
			messages := []protocol.SnapshotMessage{
				{Role: "assistant", ToolCalls: []protocol.SnapshotToolCall{{ID: "call", Name: "readfile", Arguments: `{}`}}},
				{Role: "tool", ToolCallID: "call", Content: tt.content, ToolStatus: "failed", Truncated: tt.truncated},
			}
			ui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusIdle}, Messages: messages})
			entry := ui.chat.FindTool("call")
			if entry == nil {
				t.Fatal("missing restored tool entry")
			}
			if entry.Status != toolError || entry.Result != tt.want || entry.ResultTruncated != tt.wantTruncated || entry.ResultBytes != len(tt.content) {
				t.Fatalf("restored status/bytes/length/truncated = %v/%d/%d/%v; want error/%d/%d/%v", entry.Status, entry.ResultBytes, len(entry.Result), entry.ResultTruncated, len(tt.content), len(tt.want), tt.wantTruncated)
			}
			if len(entry.Result) > liveLimit || !utf8.ValidString(entry.Result) {
				t.Fatalf("restored result is not bounded UTF-8: length = %d", len(entry.Result))
			}
			if ui.chat.Loading || ui.chat.Compacting || !entry.StartedAt.IsZero() || !entry.EndedAt.IsZero() {
				t.Fatal("restoring tool result started a run or timer")
			}
			if messages[1].Content != tt.content || messages[1].Truncated != tt.truncated {
				t.Fatal("restoring display result modified the snapshot")
			}
			detail := toolview.DetailLineSource(entry, ui.toolDetailDeps())
			var lines []string
			for i := 0; i < detail.Len(); i++ {
				lines = append(lines, stripANSIForTest(detail.Line(i)))
			}
			detailText := strings.Join(lines, "\n")
			if !strings.Contains(detailText, ui.tr("tui.tool.result")) || strings.Contains(detailText, ui.tr("tui.tool.truncated")) != tt.wantTruncated {
				t.Fatalf("incorrect result detail or truncation indicator: %q", detailText)
			}
			if tt.name == "raw failure" && !strings.Contains(detailText, tt.content) {
				t.Fatalf("detail changed raw failure: %q", detailText)
			}
		})
	}
}

func TestRestoreToolStatusRequiresExplicitCompletion(t *testing.T) {
	for _, sessionStatus := range []protocol.SessionStatus{protocol.SessionStatusIdle, protocol.SessionStatusRunning} {
		for _, locale := range []LocaleID{LocaleEN, LocaleZH} {
			for _, tt := range []struct {
				status string
				want   toolStatus
			}{
				{status: "completed", want: toolDone},
				{status: "failed", want: toolError},
				{status: "", want: toolview.StatusUnknown},
				{status: "cancelled", want: toolCancelled},
				{status: "running", want: toolview.StatusUnknown},
				{status: "future_status", want: toolview.StatusUnknown},
				{status: "COMPLETED", want: toolview.StatusUnknown},
			} {
				t.Run(string(sessionStatus)+"/"+string(locale)+"/"+tt.status, func(t *testing.T) {
					const diagnostic = "provider: exact_error at example.go:12\nraw line"
					ui := &TUI{i18n: newTranslator(locale), width: 120, height: 24}
					ui.initChatComponents()
					ui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "s", Status: sessionStatus}, Messages: []protocol.SnapshotMessage{
						{Role: "assistant", ToolCalls: []protocol.SnapshotToolCall{{ID: "call", Name: "readfile", Arguments: `{}`}}},
						{Role: "tool", ToolCallID: "call", Content: diagnostic, ToolStatus: tt.status},
					}})
					entry := ui.chat.FindTool("call")
					if entry == nil || entry.Status != tt.want || entry.Result != diagnostic {
						t.Fatalf("restored entry = %#v, want status %v and unchanged diagnostic", entry, tt.want)
					}
					if !entry.StartedAt.IsZero() || !entry.EndedAt.IsZero() || entry.Duration != 0 {
						t.Fatalf("restored result has timer: %+v", entry)
					}
					block := &toolview.Block{}
					block.Add(entry)
					views := []string{stripANSIForTest(ui.renderToolEntry(entry, false)), stripANSIForTest(ui.renderToolBlock(block))}
					for _, view := range views {
						if strings.Contains(view, "suna-spinner") || strings.Contains(view, "⠿") {
							t.Fatalf("terminal result has spinner: %q", view)
						}
						switch tt.want {
						case toolview.StatusUnknown:
							want := "Result unknown"
							if locale == LocaleZH {
								want = "结果未确认"
							}
							if ui.tr("tui.tool.result_unknown") != want || !strings.Contains(view, want) || strings.ContainsAny(view, "✓✗⊘") {
								t.Fatalf("unknown result was misrepresented: %q", view)
							}
						case toolCancelled:
							if !strings.Contains(view, ui.tr("tui.tool.cancelled")) || !strings.Contains(view, "⊘") {
								t.Fatalf("explicit cancellation not rendered: %q", view)
							}
						case toolDone:
							if !strings.Contains(view, "✓") {
								t.Fatalf("completion not rendered: %q", view)
							}
						case toolError:
							if !strings.Contains(view, "✗") {
								t.Fatalf("failure not rendered: %q", view)
							}
						}
					}
				})
			}
		}
	}
}

func TestUnknownToolBlockPrecedenceNeverShowsFalseSuccess(t *testing.T) {
	deps := toolview.RenderDeps{Width: 100, Spinner: "SPIN", Labels: toolview.RenderLabels{Tools: "Tools", Actions: "actions"}}
	for _, tt := range []struct {
		name   string
		other  toolview.Status
		prefix string
	}{
		{name: "done", other: toolview.StatusDone, prefix: "?"},
		{name: "error", other: toolview.StatusError, prefix: "✗"},
		{name: "cancelled", other: toolview.StatusCancelled, prefix: "⊘"},
		{name: "running", other: toolview.StatusRunning, prefix: "SPIN"},
		{name: "cancelling", other: toolview.StatusCancelling, prefix: "SPIN"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			block := &toolview.Block{}
			block.Add(&toolview.Entry{ID: "unknown", Name: "Readfile", Status: toolview.StatusUnknown})
			block.Add(&toolview.Entry{ID: "other", Name: "Other", Status: tt.other})
			view := toolview.RenderBlock(block, deps)
			title, _, _ := strings.Cut(view, "\n")
			if !strings.Contains(title, tt.prefix+" Tools") || !strings.Contains(view, "Result unknown") {
				t.Fatalf("block = %q, want header %q and neutral label", view, tt.prefix)
			}
			if tt.other != toolview.StatusRunning && tt.other != toolview.StatusCancelling && strings.Contains(view, "SPIN") {
				t.Fatalf("terminal block has spinner: %q", view)
			}
		})
	}
}

func TestRestoredToolWithNoResultIsRunningOnlyForActiveSession(t *testing.T) {
	for _, tt := range []struct {
		status protocol.SessionStatus
		want   toolview.Status
	}{
		{status: protocol.SessionStatusRunning, want: toolview.StatusRunning},
		{status: protocol.SessionStatusWaiting, want: toolview.StatusRunning},
		{status: protocol.SessionStatusCompacting, want: toolview.StatusRunning},
		{status: protocol.SessionStatusIdle, want: toolview.StatusUnknown},
		{status: "", want: toolview.StatusUnknown},
	} {
		t.Run(string(tt.status), func(t *testing.T) {
			ui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 24}
			ui.initChatComponents()
			ui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "s", Status: tt.status}, Messages: []protocol.SnapshotMessage{
				{Role: "assistant", ToolCalls: []protocol.SnapshotToolCall{{ID: "call", Name: "readfile", Arguments: `{}`}}},
			}})
			entry := ui.chat.FindTool("call")
			if entry == nil || entry.Status != tt.want || !entry.StartedAt.IsZero() || !entry.EndedAt.IsZero() {
				t.Fatalf("unreturned entry = %#v, want status %v without timer", entry, tt.want)
			}
			if tt.want == toolview.StatusUnknown {
				view := stripANSIForTest(ui.renderToolEntry(entry, false))
				if !strings.Contains(view, "Result unknown") || strings.ContainsAny(view, "✓✗⊘⠿") {
					t.Fatalf("idle unknown entry = %q", view)
				}
			}
		})
	}
}

func TestRestoredUnknownSubtaskNeverRendersSpinnerOrSuccess(t *testing.T) {
	for _, locale := range []LocaleID{LocaleEN, LocaleZH} {
		for _, withResult := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/result=%v", locale, withResult), func(t *testing.T) {
				ui := &TUI{i18n: newTranslator(locale), width: 120, height: 24}
				ui.initChatComponents()
				messages := []protocol.SnapshotMessage{{Role: "assistant", ToolCalls: []protocol.SnapshotToolCall{{ID: "call", Name: "spawn", Arguments: `{}`}}}}
				if withResult {
					messages = append(messages, protocol.SnapshotMessage{Role: "tool", ToolCallID: "call", Content: "diagnostic", ToolStatus: "future_status"})
				}
				ui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusIdle}, Messages: messages})
				entry := ui.chat.FindTool("call")
				if entry == nil || entry.Status != toolview.StatusUnknown || entry.RawName != "spawn" {
					t.Fatalf("restored subtask = %#v", entry)
				}
				block := &toolview.Block{}
				block.Add(entry)
				if !toolview.IsSubtask(entry) {
					t.Fatal("restored source identity changed")
				}
				view := stripANSIForTest(ui.renderSubtaskBlock(block))
				if !strings.Contains(view, ui.tr("tui.tool.result_unknown")) || strings.Contains(view, "suna-spinner") || strings.ContainsAny(view, "✓✗⊘⠿") {
					t.Fatalf("unknown subtask rendered as success, failure, cancellation or running: %q", view)
				}
			})
		}
	}
}

func TestRestoreMediaReferencesPreservesMessageBlocks(t *testing.T) {
	first := model.MediaRef{Kind: model.MediaAttachment, Name: "first.png", MimeType: "image/png", Size: 1536}
	second := model.MediaRef{Kind: model.MediaURL, Name: "second.jpg", MimeType: "image/jpeg", URL: "https://example.com/second.jpg"}
	legacy := "[uploaded image: old.png, image/png, 2.0KB, source=attachment:old.png]"
	ref := func(image model.MediaRef) model.ContentBlock {
		return model.ContentBlock{Type: model.ContentImage, Media: &image}
	}
	text := func(value string) model.ContentBlock {
		return model.ContentBlock{Type: model.ContentText, Text: value}
	}
	firstSource := "source=attachment:first.png"
	secondSource := "source=https://example.com/second.jpg"
	cases := []struct {
		name    string
		blocks  []model.ContentBlock
		want    string
		sources []string
	}{
		{
			name:    "one image",
			blocks:  []model.ContentBlock{ref(first)},
			want:    "📷 Image · first.png · 1.5KB",
			sources: []string{firstSource},
		},
		{
			name:    "images retain order",
			blocks:  []model.ContentBlock{ref(first), ref(second)},
			want:    "📷 Image · first.png · 1.5KB\n📷 Image · second.jpg",
			sources: []string{firstSource, secondSource},
		},
		{
			name:    "minimal reference without mime or size",
			blocks:  []model.ContentBlock{ref(model.MediaRef{Kind: model.MediaPath, Name: "screenshot", Path: "/images/screenshot"})},
			want:    "📷 Image · screenshot",
			sources: []string{"source=/images/screenshot"},
		},
		{
			name:    "text after images stays in place",
			blocks:  []model.ContentBlock{ref(first), ref(second), text("Later text.\n\nDo not lose this.")},
			want:    "📷 Image · first.png · 1.5KB\n📷 Image · second.jpg\nLater text.\n\nDo not lose this.",
			sources: []string{firstSource, secondSource},
		},
		{
			name:    "legacy and current references",
			blocks:  []model.ContentBlock{text(legacy), ref(second), text("Following text")},
			want:    "📷 Image · old.png · 2.0KB\n📷 Image · second.jpg\nFollowing text",
			sources: []string{"source=attachment:old.png", secondSource},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			original := memory.Message{Role: model.RoleUser, Origin: memory.OriginUser, Content: tt.blocks}
			projected := memory.ProjectMessages([]memory.Message{original})[0]
			content := projected.Text()
			for _, source := range tt.sources {
				if !strings.Contains(content, source) {
					t.Fatalf("model history = %q, missing %q", content, source)
				}
			}
			snapshot := protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusIdle}, Messages: []protocol.SnapshotMessage{{Role: "user", Kind: protocol.SnapshotMessageKindMedia, Content: content}}}
			ui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 30}
			ui.initChatComponents()
			ui.applySessionSnapshot(snapshot)
			if len(ui.chat.Messages) != 1 {
				t.Fatalf("restored messages = %d, want 1", len(ui.chat.Messages))
			}
			got, _ := ui.chat.Messages[0].Content.(string)
			if got = stripANSIForTest(got); got != tt.want {
				t.Fatalf("restored content = %q, want %q", got, tt.want)
			}
			rendered := stripANSIForTest(ui.renderUserMessage(ui.chat.Messages[0].Content, 1000))
			wantRendered := "  ● " + strings.ReplaceAll(tt.want, "\n", "\n    ")
			if rendered != wantRendered {
				t.Fatalf("rendered content = %q, want %q", rendered, wantRendered)
			}
			if snapshot.Messages[0].Content != content || projected.Text() != content {
				t.Fatal("display conversion changed the model/snapshot source")
			}
			if tt.blocks[0].Type == model.ContentImage && tt.blocks[0].Media == nil {
				t.Fatal("display conversion changed the original image")
			}
		})
	}
}

func TestRestoreMediaLeavesNonReferenceTextUnchanged(t *testing.T) {
	const first = "[image: first.png, image/png, 10B, source=attachment:first.png]"
	const imageLabel = "📷 Image · first.png · 10B"
	const fake = "[image: ordinary text, source=example]"
	cases := []struct {
		name, content, want string
		kind                protocol.SnapshotMessageKind
	}{
		{"text-kind message", "Before " + first + " after", "Before " + first + " after", protocol.SnapshotMessageKindText},
		{"missing closing bracket", "[image: ordinary text, source=example", "[image: ordinary text, source=example", protocol.SnapshotMessageKindMedia},
		{"source outside brackets", "[image: ordinary text]\nsource=example", "[image: ordinary text]\nsource=example", protocol.SnapshotMessageKindMedia},
		{"source is empty", "[image: ordinary text, source=]", "[image: ordinary text, source=]", protocol.SnapshotMessageKindMedia},
		{"source word is not an image reference", fake, fake, protocol.SnapshotMessageKindMedia},
		{"adjacent prose is not part of the reference", first + " some [text]", first + " some [text]", protocol.SnapshotMessageKindMedia},
		{"later mention is original text", first + "\nAbout " + fake + "\nKeep source=literal", imageLabel + "\nAbout " + fake + "\nKeep source=literal", protocol.SnapshotMessageKindMedia},
		{"body followed by reference-shaped quote", first + "\nQuoted example:\n" + fake, imageLabel + "\nQuoted example:\n" + fake, protocol.SnapshotMessageKindMedia},
		{"malformed next line", first + "\n[image: no source]", imageLabel + "\n[image: no source]", protocol.SnapshotMessageKindMedia},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 30}
			ui.initChatComponents()
			ui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusIdle}, Messages: []protocol.SnapshotMessage{{Role: "user", Content: tt.content, Kind: tt.kind}}})
			if len(ui.chat.Messages) != 1 {
				t.Fatalf("restored messages = %d, want 1", len(ui.chat.Messages))
			}
			got, _ := ui.chat.Messages[0].Content.(string)
			if got = stripANSIForTest(got); got != tt.want {
				t.Fatalf("restored content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRestoreCompactedToolsMergeWithNativeBlockWithoutDetailOrReplay(t *testing.T) {
	for _, locale := range []LocaleID{LocaleEN, LocaleZH} {
		t.Run(string(locale), func(t *testing.T) {
			ui := &TUI{i18n: newTranslator(locale), width: 100, height: 30}
			ui.initChatComponents()
			snapshot := protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session", Status: protocol.SessionStatusIdle}, Messages: []protocol.SnapshotMessage{
				{Role: "assistant", Content: "original explanation", ObservedTools: []protocol.SnapshotToolLabel{{Name: "readfile", Intent: "inspect source", Status: "completed"}, {Name: "exec", Intent: "check build", Status: "failed"}}},
				{Role: "assistant", ObservedTools: []protocol.SnapshotToolLabel{{Name: "spawn", Intent: "review changes", Status: "future-status"}}},
				{Role: "assistant", ToolCalls: []protocol.SnapshotToolCall{{ID: "native", Name: "readfile", Arguments: `{"path":"current.go"}`, Intent: "read current source"}}},
				{Role: "tool", ToolCallID: "native", ToolStatus: "completed", Content: "native result"},
			}}
			ui.applySessionSnapshot(snapshot)
			if len(ui.chat.Messages) != 2 || ui.chat.Messages[0].Content != "original explanation" {
				t.Fatalf("restored messages = %#v, want original prose and one grouped block", ui.chat.Messages)
			}
			block, ok := ui.chat.Messages[1].Content.(*toolview.Block)
			if !ok || len(block.Order) != 4 || len(toolview.VisibleMainEntries(block)) != 4 {
				t.Fatalf("grouped tools = %#v", block)
			}
			for i, wantStatus := range []toolview.Status{toolview.StatusDone, toolview.StatusError, toolview.StatusUnknown} {
				entry := block.Entries[block.Order[i]]
				if !entry.Compacted || entry.Status != wantStatus || entry.Result != "" || entry.ParamsRaw != nil || entry.Params != "" || !entry.StartedAt.IsZero() || entry.Duration != 0 {
					t.Fatalf("compacted row %d = %#v, want static facts only", i, entry)
				}
				if toolview.IsSubtask(entry) {
					t.Fatal("compacted spawn opened a live/subtask result panel")
				}
			}
			native := block.Entries["native"]
			if native.Compacted || native.Result != "native result" || native.ParamsRaw["path"] != "current.go" || native.Status != toolview.StatusDone {
				t.Fatalf("native details changed: %#v", native)
			}
			if ui.chat.Loading || len(ui.chat.ActiveTools) != 0 || len(ui.chat.ToolStartTimes) != 0 {
				t.Fatal("history restoration replayed live execution")
			}
			ui.chat.ExpandedBlock, ui.chat.ExpandedBoxKind = block, "tool"
			if entries := ui.chat.ExpandedBlockEntries(); len(entries) != 1 || entries[0] != native {
				t.Fatalf("expandable entries = %#v, want native tool only", entries)
			}
			rendered := stripANSIForTest(ui.renderToolBlock(block))
			for _, want := range []string{"inspect source", "check build", "review changes", ui.tr("tui.tool.compacted")} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("rendered block missing %q: %s", want, rendered)
				}
			}
			ui.chat.ExpandedBlock = nil
			block.Order = block.Order[:3]
			delete(block.Entries, "native")
			if entries := toolview.ExpandableMainEntries(block); len(entries) != 0 {
				t.Fatalf("compacted-only block has details: %#v", entries)
			}
			if rendered := stripANSIForTest(ui.renderToolBlock(block)); strings.Contains(rendered, ui.tr("tui.tool.detail_hint")) {
				t.Fatalf("compacted-only block offered removed detail: %s", rendered)
			}
		})
	}
}

func TestRestoreCompactedToolsDoesNotGroupAcrossOriginalDiscussion(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 30}
	ui.initChatComponents()
	ui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session"}, Messages: []protocol.SnapshotMessage{
		{Role: "assistant", ObservedTools: []protocol.SnapshotToolLabel{{Name: "readfile", Status: "completed"}}},
		{Role: "assistant", Content: "explanation between tools"},
		{Role: "assistant", ObservedTools: []protocol.SnapshotToolLabel{{Name: "exec", Status: "failed"}}},
	}})
	if len(ui.chat.Messages) != 3 || ui.chat.Messages[0].Role != "tool" || ui.chat.Messages[1].Content != "explanation between tools" || ui.chat.Messages[2].Role != "tool" {
		t.Fatalf("restored order changed: %#v", ui.chat.Messages)
	}
}
