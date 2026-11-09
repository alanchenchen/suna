package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	"github.com/alanchenchen/suna/internal/tui/theme"
	"github.com/charmbracelet/x/ansi"
)

func TestBackgroundCompactDoesNotTakeOverRunOrPublishEarlyGain(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleZH)}
	ui.initChatComponents()
	ui.chat.Loading = true
	ui.contextTokens = 900
	ui.runStartedAt = time.Now().Add(-time.Minute)
	started := ui.runStartedAt
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "one", Kind: "tools", State: "preparing"})
	if ui.chat.Compacting || !ui.chat.Loading || ui.contextTokens != 900 || len(ui.chat.Messages) != 0 {
		t.Fatal("background preparation changed foreground")
	}
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "one", State: "ready"})
	if ui.contextTokens != 900 || ui.backgroundCompactLabel() != "" {
		t.Fatal("ready changed context or remained busy")
	}
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "one", Kind: "tools", State: "applied", Result: &protocol.CompactResult{BeforeTokens: 900, AfterTokens: 600, ContextWindow: 1000}})
	if ui.contextTokens != 600 || len(ui.chat.Messages) != 1 || !ui.chat.Loading || ui.runStartedAt != started {
		t.Fatal("applied changed run lifecycle")
	}
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "one", State: "preparing"})
	if ui.compactState.State != "applied" {
		t.Fatal("late preparing reactivated terminal state")
	}
}
func TestManualCompactTerminalClearsOnlyManualWait(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleZH)}
	ui.initChatComponents()
	ui.manualCompactPending = true
	ui.manualCompactID = "one"
	ui.chat.Loading = true
	ui.chat.Compacting = true
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "one", State: "skipped", Reason: "no_source"})
	if ui.chat.Loading || ui.chat.Compacting || ui.manualCompactPending || len(ui.chat.Messages) != 1 {
		t.Fatal("manual wait did not finish")
	}
}

func TestCompactNotificationBeforeAcceptedMatchesOnlyReturnedID(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleZH)}
	ui.initChatComponents()
	ui.manualCompactPending = true
	ui.chat.Loading = true
	ui.chat.Compacting = true
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "old", State: "skipped"})
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "new", State: "skipped", Reason: "no_source"})
	if !ui.manualCompactPending || len(ui.chat.Messages) != 0 {
		t.Fatal("unconfirmed notification completed manual request")
	}
	ui.handleProtocolResultMsg(compactAcceptedMsg{ID: "new"})
	if ui.manualCompactPending || ui.chat.Loading || len(ui.chat.Messages) != 1 {
		t.Fatal("matching terminal was not consumed after accepted")
	}
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "new", State: "skipped"})
	if len(ui.chat.Messages) != 1 {
		t.Fatal("duplicate terminal was replayed")
	}
}
func TestManualCompactPendingRetainsMatchingTerminalAmongStaleCandidates(t *testing.T) {
	for _, state := range []string{"applied", "skipped", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			ui := &TUI{i18n: newTranslator(LocaleEN), currentSession: protocol.SessionInfo{ID: "s"}}
			ui.initChatComponents()
			ui.manualCompactPending = true
			ui.chat.Loading = true
			ui.chat.Compacting = true
			ui.contextTokens = 900
			ui.compactTerminalID = "known-old"
			ui.handleCompactStateNotification(protocol.CompactState{SessionID: "other", CompactID: "other", State: "applied"})
			ui.handleCompactStateNotification(protocol.CompactState{SessionID: "s", CompactID: "known-old", State: "failed"})
			for i := 0; i < 8; i++ {
				ui.handleCompactStateNotification(protocol.CompactState{SessionID: "s", CompactID: fmt.Sprintf("old-%d", i), State: "skipped"})
				if len(ui.manualCompactEvents) > 4 {
					t.Fatalf("unbounded pending candidates: %d", len(ui.manualCompactEvents))
				}
			}
			terminal := protocol.CompactState{SessionID: "s", CompactID: "new", Kind: "tools", State: state, Reason: "no_source", Error: "exact failure"}
			if state == "applied" {
				terminal.Result = &protocol.CompactResult{BeforeTokens: 900, AfterTokens: 600, ContextWindow: 1000}
			}
			ui.handleCompactStateNotification(terminal)
			// 同 ID 的迟到准备、ready 或终态不能覆盖已保存的精确终态。
			for _, late := range []protocol.CompactState{
				{SessionID: "s", CompactID: "new", State: "preparing", Wait: "manual"},
				{SessionID: "s", CompactID: "new", State: "ready"},
				{SessionID: "s", CompactID: "new", State: "cancelled"},
				{SessionID: "s", CompactID: "other-live", State: "preparing"},
			} {
				ui.handleCompactStateNotification(late)
			}
			if got := bufferedCompactState(ui, "new"); got.State != state || got.Reason != terminal.Reason || got.Error != terminal.Error || got.Result != terminal.Result {
				t.Fatalf("saved terminal = %+v, want %+v", got, terminal)
			}
			if bufferedCompactState(ui, "other").CompactID != "" {
				t.Fatal("foreign session notification retained")
			}
			if bufferedCompactState(ui, "known-old").CompactID != "" {
				t.Fatal("oldest candidate was not evicted")
			}
			ui.handleProtocolResultMsg(compactAcceptedMsg{ID: "new", SessionID: "other"})
			if !ui.manualCompactPending || len(ui.chat.Messages) != 0 {
				t.Fatal("foreign acknowledgement changed pending request")
			}
			ui.handleProtocolResultMsg(compactAcceptedMsg{ID: "new", SessionID: "s"})
			if ui.manualCompactPending || ui.chat.Loading || ui.chat.Compacting || ui.manualCompactEvents != nil || len(ui.chat.Messages) != 1 {
				t.Fatalf("manual completion pending/loading/compacting/events/messages = %v/%v/%v/%v/%d", ui.manualCompactPending, ui.chat.Loading, ui.chat.Compacting, ui.manualCompactEvents, len(ui.chat.Messages))
			}
			want := ui.tr("compact.no_source")
			switch state {
			case "applied":
				want = ui.tr("compact.manual_applied", ui.tr("compact.tools_applied"), fmtTok(900), fmtTok(600), fmtTok(1000), fmtTok(300), toolview.FormatCompactDuration(0))
				if ui.contextTokens != 600 {
					t.Fatalf("context tokens = %d, want 600", ui.contextTokens)
				}
			case "failed":
				want = ui.tr("compact.failed_detail", "exact failure")
			case "cancelled":
				want = ui.tr("compact.cancelled")
			}
			if got, _ := ui.chat.Messages[0].Content.(string); got != want {
				t.Fatalf("message = %q, want %q", got, want)
			}
			ui.handleCompactStateNotification(terminal)
			if len(ui.chat.Messages) != 1 {
				t.Fatal("duplicate terminal replayed")
			}
		})
	}
}

func TestManualCompactCanReuseAlreadyDisplayedBackgroundTerminal(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleEN), compactTerminalID: "reused"}
	ui.initChatComponents()
	ui.manualCompactPending = true
	ui.chat.Loading = true
	ui.chat.Compacting = true
	// 旧背景候选可能被本次手动请求复用；未确认前不得提前完成。
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "reused", State: "skipped", Reason: "no_source"})
	if len(ui.chat.Messages) != 0 {
		t.Fatal("unconfirmed terminal completed request")
	}
	ui.handleProtocolResultMsg(compactAcceptedMsg{ID: "reused"})
	if ui.manualCompactPending || len(ui.chat.Messages) != 1 {
		t.Fatal("matching reused terminal before acknowledgement was suppressed")
	}
	ui.handleCompactStateNotification(protocol.CompactState{CompactID: "reused", State: "skipped"})
	if len(ui.chat.Messages) != 1 {
		t.Fatal("reused terminal was replayed")
	}
}

func TestCompactAcceptedFirstIgnoresDifferentID(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleEN), currentSession: protocol.SessionInfo{ID: "s"}}
	ui.initChatComponents()
	ui.manualCompactPending = true
	ui.chat.Loading = true
	ui.chat.Compacting = true
	ui.handleProtocolResultMsg(compactAcceptedMsg{ID: "current", SessionID: "s"})
	ui.handleCompactStateNotification(protocol.CompactState{SessionID: "s", CompactID: "old", State: "failed"})
	if !ui.manualCompactPending || len(ui.chat.Messages) != 0 {
		t.Fatal("unrelated terminal completed confirmed request")
	}
	ui.handleCompactStateNotification(protocol.CompactState{SessionID: "s", CompactID: "current", State: "skipped"})
	if ui.manualCompactPending || len(ui.chat.Messages) != 1 {
		t.Fatal("confirmed request did not finish on matching terminal")
	}
}

func TestCompactSnapshotDoesNotReplayTerminalGain(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleZH)}
	ui.initChatComponents()
	ui.contextTokens = 900
	ui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusIdle}, Compact: &protocol.CompactState{SessionID: "s", CompactID: "past", State: "applied", Result: &protocol.CompactResult{AfterTokens: 100, ContextWindow: 1000}}})
	if ui.contextTokens != 900 || len(ui.chat.Messages) != 0 {
		t.Fatal("snapshot replayed stale compact result")
	}
}
func TestCompactResultUsesThemedSystemRenderingAndKeepsDraft(t *testing.T) {
	oldTheme := currentTheme
	t.Cleanup(func() { applyThemePalette(oldTheme) })
	for _, background := range []theme.Background{theme.LightBackground, theme.DarkBackground} {
		applyThemePalette(theme.Adapt(theme.Default, theme.DefaultColors(), background))
		for _, width := range []int{60, 110} {
			for _, kind := range []string{"tools", "history"} {
				ui := &TUI{i18n: newTranslator(LocaleZH), width: width, height: 28}
				ui.initChatComponents()
				ui.manualCompactPending, ui.manualCompactID = true, "one"
				ui.chat.Textarea.SetValue("kept draft")
				ui.handleCompactStateNotification(protocol.CompactState{CompactID: "one", Kind: kind, State: "applied", WaitElapsedMs: 21000, Result: &protocol.CompactResult{BeforeTokens: 18300, AfterTokens: 7700, ContextWindow: 400000}})
				message := ui.chat.Messages[0]
				if message.Role != "system" || ui.chat.Textarea.Value() != "kept draft" {
					t.Fatalf("role = %q, draft = %q, want system and kept draft", message.Role, ui.chat.Textarea.Value())
				}
				content := message.Content.(string)
				if content != ansi.Strip(content) {
					t.Fatal("stored message contains pre-rendered ANSI")
				}
				ui.syncContent()
				var rendered strings.Builder
				for _, block := range ui.chat.TranscriptBlocks {
					rendered.WriteString(block.Text)
				}
				plain := ansi.Strip(rendered.String())
				label := "工具记录已生成摘要"
				if kind == "history" {
					label = "较早对话已生成摘要"
				}
				for _, want := range []string{"◆ " + label, "18.3k → 7.7k / 400.0k tokens", "腾出约 10.6k tokens", "等待 21s"} {
					if !strings.Contains(plain, want) {
						t.Fatalf("kind = %s, width = %d, rendered = %q, missing %q", kind, width, plain, want)
					}
				}
				var contentLines []string
				for _, line := range strings.Split(plain, "\n") {
					if strings.TrimSpace(line) != "" {
						contentLines = append(contentLines, line)
					}
				}
				if strings.Contains(plain, "**") || strings.Contains(plain, "{}") || len(contentLines) != 3 {
					t.Fatalf("rendered result = %q, want three formatted content lines", plain)
				}
				for _, line := range strings.Split(plain, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("line width = %d, want <= %d", ansi.StringWidth(line), width)
					}
				}
			}
		}
	}
}

func TestCompactMessagesUseLocalizedTemplates(t *testing.T) {
	for _, tt := range []struct {
		name, appliedTools, appliedHistory, manual, waiting, backgroundTools, backgroundHistory, unchanged, noSource, failed, cancelled string
		locale                                                                                                                          LocaleID
	}{
		{name: "English", locale: LocaleEN, appliedTools: "Tool records summarized · freed ~300 tokens of conversation space", appliedHistory: "Earlier conversation summarized · freed ~300 tokens of conversation space", manual: "**Tool records summarized**\n\n- Estimated context usage  900 → 600 / 1.0k tokens\n- Freed ~300 tokens · waited 2.0s", waiting: "Summarizing earlier content · waited 2.0s", backgroundTools: "Generating tool summaries in background · 2.0s", backgroundHistory: "Summarizing earlier conversation in background · 2.0s", unchanged: "No summary applied; original content unchanged", noSource: "No earlier content available to summarize; original content unchanged", failed: "Summarization failed; original content unchanged", cancelled: "Summarization cancelled; original content unchanged"},
		{name: "Chinese", locale: LocaleZH, appliedTools: "工具记录已生成摘要 · 腾出约 300 tokens 的对话空间", appliedHistory: "较早对话已生成摘要 · 腾出约 300 tokens 的对话空间", manual: "**工具记录已生成摘要**\n\n- 对话占用（估算） 900 → 600 / 1.0k tokens\n- 腾出约 300 tokens · 等待 2.0s", waiting: "正在为较早内容生成摘要 · 已等待 2.0s", backgroundTools: "后台生成工具记录摘要 · 2.0s", backgroundHistory: "后台生成较早对话摘要 · 2.0s", unchanged: "未采用摘要，原内容未变", noSource: "暂无可生成摘要的较早内容，原内容未变", failed: "摘要生成失败，原内容未变", cancelled: "摘要生成已取消，原内容未变"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, state := range []struct {
				name, kind, status, reason, detail, want string
				manual                                   bool
			}{
				{name: "tools applied", kind: "tools", status: "applied", want: tt.appliedTools},
				{name: "history applied", kind: "history", status: "applied", want: tt.appliedHistory},
				{name: "manual applied", kind: "tools", status: "applied", manual: true, want: tt.manual},
				{name: "unchanged", status: "skipped", manual: true, want: tt.unchanged},
				{name: "no source", status: "skipped", reason: "no_source", manual: true, want: tt.noSource},
				{name: "failed without detail", status: "failed", manual: true, want: tt.failed},
				{name: "failed with raw detail", status: "failed", detail: "provider: exact_error at example.go:12", manual: true, want: tt.failed},
				{name: "cancelled", status: "cancelled", manual: true, want: tt.cancelled},
			} {
				t.Run(state.name, func(t *testing.T) {
					ui := &TUI{i18n: newTranslator(tt.locale)}
					ui.initChatComponents()
					ui.manualCompactPending = state.manual
					if state.manual {
						ui.manualCompactID = "one"
					}
					p := protocol.CompactState{CompactID: "one", Kind: state.kind, State: state.status, Reason: state.reason, Error: state.detail, WaitElapsedMs: 2000}
					if state.status == "applied" {
						p.Result = &protocol.CompactResult{BeforeTokens: 900, AfterTokens: 600, ContextWindow: 1000}
					}
					ui.handleCompactStateNotification(p)
					if len(ui.chat.Messages) != 1 {
						t.Fatalf("got messages = %d, want 1", len(ui.chat.Messages))
					}
					got, _ := ui.chat.Messages[0].Content.(string)
					want := state.want
					if state.detail != "" {
						want = ui.tr("compact.failed_detail", state.detail)
						if !strings.Contains(got, state.detail) {
							t.Fatalf("got detail = %q, want original %q", got, state.detail)
						}
					}
					if got != want {
						t.Fatalf("got message = %q, want %q", got, want)
					}
				})
			}
			ui := &TUI{i18n: newTranslator(tt.locale)}
			if got, want := ui.compactElapsedLabel(), ui.tr("compact.running"); got != want {
				t.Fatalf("got initial label = %q, want %q", got, want)
			}
			ui.compactStartedAt = time.Now().Add(-2 * time.Second)
			if got := ui.compactElapsedLabel(); !strings.HasPrefix(got, strings.TrimSuffix(tt.waiting, "2.0s")) || strings.Contains(got, "{}") {
				t.Fatalf("got wait label = %q, want %q", got, tt.waiting)
			}
			ui.compactReceivedAt = time.Now()
			ui.compactState = protocol.CompactState{State: "preparing", Kind: "tools", PrepareElapsedMs: 2000}
			if got := ui.backgroundCompactLabel(); !strings.HasPrefix(got, strings.TrimSuffix(tt.backgroundTools, "2.0s")) || strings.Contains(got, "{}") {
				t.Fatalf("got background tools = %q, want %q", got, tt.backgroundTools)
			}
			ui.compactState.Kind = "history"
			if got := ui.backgroundCompactLabel(); !strings.HasPrefix(got, strings.TrimSuffix(tt.backgroundHistory, "2.0s")) || strings.Contains(got, "{}") {
				t.Fatalf("got background history = %q, want %q", got, tt.backgroundHistory)
			}
		})
	}
}

func TestCompactLabelsFollowLocaleAndTranslationOverrides(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleZH)}
	ui.compactState = protocol.CompactState{State: "preparing", Kind: "tools", PrepareElapsedMs: 2000}
	ui.compactReceivedAt = time.Now()
	if got := ui.backgroundCompactLabel(); !strings.Contains(got, "后台生成工具记录摘要") {
		t.Fatalf("got Chinese label = %q", got)
	}
	ui.i18n.SetLocale(LocaleEN)
	if got := ui.backgroundCompactLabel(); !strings.Contains(got, "Generating tool summaries in background") {
		t.Fatalf("got English label = %q", got)
	}
	// 覆盖一条已有译文不应影响语言判断；业务代码只能按key取词，不能比较译文。
	ui.i18n.keys["compact.running"][LocaleEN] = "Custom running label"
	ui.i18n.keys["compact.background_tools"][LocaleEN] = "Custom background: {}"
	if got := ui.compactElapsedLabel(); got != "Custom running label" {
		t.Fatalf("got overridden label = %q", got)
	}
	if got := ui.backgroundCompactLabel(); !strings.HasPrefix(got, "Custom background: ") || strings.Contains(got, "{}") {
		t.Fatalf("got overridden background = %q", got)
	}
}

func TestCompactTranslationKeysHaveBothLocalesAndMatchingParameters(t *testing.T) {
	for key, translations := range defaultTranslationKeys() {
		if !strings.HasPrefix(key, "compact.") && key != "session.restore_compacted" && key != "tui.tool.compacted" && key != "tui.sessions.compacting" && key != "tui.command.compact.desc" {
			continue
		}
		t.Run(key, func(t *testing.T) {
			en, zh := translations[LocaleEN], translations[LocaleZH]
			if en == "" || zh == "" {
				t.Fatalf("got translations = %#v, want both locales", translations)
			}
			if got, want := strings.Count(en, "{}"), strings.Count(zh, "{}"); got != want {
				t.Fatalf("got English parameters = %d, want Chinese parameters = %d", got, want)
			}
		})
	}
}

func TestBackgroundCompactVisibleForOwnerObserverAndIdleWithoutTakingOver(t *testing.T) {
	for _, locale := range []LocaleID{LocaleEN, LocaleZH} {
		for _, tt := range []struct {
			name          string
			loading       bool
			canControl    bool
			sessionStatus protocol.SessionStatus
			wantSteer     bool
			wantLocked    bool
			foregroundKey string
			helpKey       string
		}{
			{name: "owner", loading: true, canControl: true, sessionStatus: protocol.SessionStatusRunning, wantSteer: true, foregroundKey: "status.waiting_llm", helpKey: "tui.chat.input_help_running"},
			{name: "observer", loading: true, sessionStatus: protocol.SessionStatusRunning, wantLocked: true, foregroundKey: "tui.chat.observe_input", helpKey: "tui.chat.input_help_observing"},
			{name: "idle", sessionStatus: protocol.SessionStatusIdle, foregroundKey: "tui.chat.input_placeholder"},
		} {
			t.Run(string(locale)+"/"+tt.name, func(t *testing.T) {
				ui := &TUI{i18n: newTranslator(locale), width: 120, height: 26, mode: uipage.Chat, currentSession: protocol.SessionInfo{ID: "s", Status: tt.sessionStatus}, currentRunCanControl: tt.canControl}
				ui.initChatComponents()
				ui.chat.Loading = tt.loading
				ui.chat.Phase = phaseFirstLLM
				ui.chat.PhaseStart = time.Now().Add(-time.Minute)
				ui.runStartedAt = time.Now().Add(-time.Minute)
				if tt.loading {
					ui.activeRunID = "run"
				}
				ui.layoutChat()
				before := ui.renderInputArea()
				foreground := stripANSIForTest(before.content)
				if !strings.Contains(foreground, ui.tr(tt.foregroundKey)) || tt.helpKey != "" && !strings.Contains(foreground, ui.tr(tt.helpKey)) {
					t.Fatalf("foreground before compact = %q", foreground)
				}
				if got := ui.canSteerCurrentRun(); got != tt.wantSteer {
					t.Fatalf("canSteer = %v, want %v", got, tt.wantSteer)
				}
				if got := ui.inputLocked(); got != tt.wantLocked {
					t.Fatalf("locked = %v, want %v", got, tt.wantLocked)
				}
				if tt.wantLocked {
					ui.chat.Textarea.Blur()
				} else {
					ui.chat.Textarea.Focus()
				}
				focused := ui.chat.Textarea.Focused()
				policy := ui.currentInputPolicy()
				phase, phaseStart, runStart := ui.chat.Phase, ui.chat.PhaseStart, ui.runStartedAt
				ui.chat.ManualScrollPaused = true
				ui.chat.FollowBottom = false
				offset := ui.chat.TranscriptYOffset

				ui.handleCompactStateNotification(protocol.CompactState{SessionID: "s", CompactID: "c", Kind: "tools", State: "preparing", PrepareElapsedMs: 2000})
				if ui.chat.Loading != tt.loading || ui.chat.Compacting || ui.chat.Phase != phase || ui.chat.PhaseStart != phaseStart || ui.runStartedAt != runStart {
					t.Fatal("background preparation took over the foreground run")
				}
				if ui.currentInputPolicy().Locked != policy.Locked || ui.currentInputPolicy().AllowCancel != policy.AllowCancel || ui.chat.Textarea.Focused() != focused || ui.chat.Textarea.Value() != "" || ui.canSteerCurrentRun() != tt.wantSteer {
					t.Fatal("background preparation changed input policy, focus, or draft")
				}
				after := ui.renderInputArea()
				got := stripANSIForTest(after.content)
				backgroundPrefix := strings.TrimSuffix(ui.tr("compact.background_tools", "2.0s"), "2.0s")
				foregroundIndex, backgroundIndex := strings.Index(got, ui.tr(tt.foregroundKey)), strings.Index(got, backgroundPrefix)
				if strings.Count(got, backgroundPrefix) != 1 || foregroundIndex < 0 || foregroundIndex > backgroundIndex {
					t.Fatalf("composer = %q, want one secondary background indicator", got)
				}
				if tt.helpKey != "" && !strings.Contains(got, ui.tr(tt.helpKey)) {
					t.Fatalf("composer = %q, lost foreground help", got)
				}
				if len(ui.chat.Messages) != 0 || !ui.chat.ManualScrollPaused || ui.chat.FollowBottom || ui.chat.TranscriptYOffset != offset {
					t.Fatal("background preparation changed transcript or scrolling")
				}
				if after.cursorOK != before.cursorOK || after.cursorX != before.cursorX || after.cursorY != before.cursorY {
					t.Fatalf("cursor moved inside composer: before %#v, after %#v", before, after)
				}
				if !tt.wantLocked {
					ui.chat.Textarea.SetValue("kept draft")
					if draftView := stripANSIForTest(ui.renderInputArea().content); !strings.Contains(draftView, "kept draft") || !strings.Contains(draftView, backgroundPrefix) {
						t.Fatalf("composer lost editable draft or background indicator: %q", draftView)
					}
				}

				ui.handleCompactStateNotification(protocol.CompactState{SessionID: "s", CompactID: "c", Kind: "tools", State: "ready"})
				if ready := stripANSIForTest(ui.renderInputArea().content); strings.Contains(ready, backgroundPrefix) {
					t.Fatalf("ready still shows preparing indicator: %q", ready)
				}
				if ui.chat.Loading != tt.loading || ui.chat.Compacting || ui.inputLocked() != tt.wantLocked {
					t.Fatal("ready changed foreground interaction")
				}
			})
		}
	}
}

func TestBackgroundCompactComposerWrapAndTranslationOverride(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleEN), width: 44, height: 24, mode: uipage.Chat, currentSession: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusIdle}}
	ui.initChatComponents()
	ui.i18n.keys["compact.background_tools"][LocaleEN] = "Custom background compact label with wrapping · {}"
	ui.handleCompactStateNotification(protocol.CompactState{SessionID: "s", CompactID: "c", Kind: "tools", State: "preparing", PrepareElapsedMs: 2000})
	view := stripANSIForTest(ui.renderInputArea().content)
	if !strings.Contains(view, "Custom background compact label") || strings.Contains(view, "{}") || chatpage.RenderedLineCount(view) < 3 {
		t.Fatalf("composer = %q, want wrapped translated label", view)
	}
	ui.layoutChat()
	render := ui.renderInputArea()
	x, y, ok := ui.textareaCursorScreenPos()
	if !ok || !render.cursorOK || x != render.cursorX || y != 5+ui.chat.Viewport.Height()+render.cursorY {
		t.Fatalf("cursor = (%d,%d,%v), render = %#v", x, y, ok, render)
	}
	ui.compactState.Wait = "hard_limit"
	if got := stripANSIForTest(ui.renderInputArea().content); strings.Contains(got, "Custom background compact") {
		t.Fatalf("foreground wait must not also show background indicator: %q", got)
	}
}

func TestBackgroundCompactViewKeepsPausedTranscriptPosition(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleEN), width: 90, height: 24, mode: uipage.Chat, ready: true, currentSession: protocol.SessionInfo{ID: "s", Status: protocol.SessionStatusIdle}}
	ui.initChatComponents()
	for i := 0; i < 60; i++ {
		ui.appendNonToolMessage(chatMsg{Role: "system", Content: "retained transcript line"})
	}
	ui.layoutChat()
	ui.syncContent()
	ui.chat.SetTranscriptYOffset(10)
	ui.chat.FollowBottom = false
	ui.chat.ManualScrollPaused = true
	beforeOffset := ui.chat.TranscriptYOffset
	beforeInput := ui.renderInputArea()
	if beforeOffset != 10 {
		t.Fatalf("fixture offset = %d, want 10", beforeOffset)
	}

	// 模拟已接入窗口的通知、更新与布局边界。
	ui.Update(compactStateMsg{Params: protocol.CompactState{SessionID: "s", CompactID: "c", Kind: "history", State: "preparing", PrepareElapsedMs: 2000}})
	view := stripANSIForTest(ui.viewChat())
	if !strings.Contains(view, "Summarizing earlier conversation in background") || !strings.Contains(view, "retained transcript line") {
		t.Fatalf("chat view lost background indicator or transcript: %q", view)
	}
	if ui.chat.TranscriptYOffset != beforeOffset || !ui.chat.ManualScrollPaused || ui.chat.FollowBottom || ui.chat.NewContentWhilePaused {
		t.Fatalf("preparing changed paused transcript: offset=%d paused=%v follow=%v new=%v", ui.chat.TranscriptYOffset, ui.chat.ManualScrollPaused, ui.chat.FollowBottom, ui.chat.NewContentWhilePaused)
	}
	if got := chatpage.RenderedLineCount(ui.renderInputArea().content) - chatpage.RenderedLineCount(beforeInput.content); got != 1 {
		t.Fatalf("input area row delta = %d, want 1", got)
	}
	ui.Update(compactStateMsg{Params: protocol.CompactState{SessionID: "s", CompactID: "c", State: "ready"}})
	if got := stripANSIForTest(ui.viewChat()); strings.Contains(got, "Summarizing earlier conversation in background") {
		t.Fatalf("ready still shows background indicator: %q", got)
	}
	if ui.chat.TranscriptYOffset != beforeOffset || !ui.chat.ManualScrollPaused || ui.chat.FollowBottom {
		t.Fatal("ready changed paused transcript position")
	}
}

func TestSnapshotBackgroundPreparingShownForOwnerObserverAndIdle(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     protocol.SessionStatus
		currentRun *protocol.CurrentRunView
		locked     bool
	}{
		{name: "owner", status: protocol.SessionStatusRunning, currentRun: &protocol.CurrentRunView{RunID: "r", Status: protocol.SessionStatusRunning, CanControl: true}},
		{name: "observer", status: protocol.SessionStatusRunning, currentRun: &protocol.CurrentRunView{RunID: "r", Status: protocol.SessionStatusRunning}, locked: true},
		{name: "idle", status: protocol.SessionStatusIdle},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 24}
			ui.initChatComponents()
			ui.applySessionSnapshot(protocol.SessionSnapshot{
				Session:    protocol.SessionInfo{ID: "s", Status: tt.status},
				CurrentRun: tt.currentRun,
				Compact:    &protocol.CompactState{SessionID: "s", CompactID: "c", Kind: "tools", State: "preparing", PrepareElapsedMs: 2000},
			})
			if ui.chat.Loading != (tt.currentRun != nil) || ui.chat.Compacting || ui.inputLocked() != tt.locked {
				t.Fatalf("snapshot loading/compacting/locked = %v/%v/%v", ui.chat.Loading, ui.chat.Compacting, ui.inputLocked())
			}
			view := stripANSIForTest(ui.renderInputArea().content)
			if strings.Count(view, "Generating tool summaries in background") != 1 {
				t.Fatalf("snapshot composer missing background preparation: %q", view)
			}
			if len(ui.chat.Messages) != 0 {
				t.Fatal("snapshot preparation was replayed as transcript content")
			}
		})
	}
}

func bufferedCompactState(ui *TUI, id string) protocol.CompactState {
	for _, state := range ui.manualCompactEvents {
		if state.CompactID == id {
			return state
		}
	}
	return protocol.CompactState{}
}
