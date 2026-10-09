package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

func TestConfigThemeChangeRefreshesChatWithoutInput(t *testing.T) {
	for _, action := range []string{"apply", "cancel", "forced-close", "notification", "locale"} {
		for _, paused := range []bool{false, true} {
			name := action + "/bottom"
			if paused {
				name = action + "/paused"
			}
			t.Run(name, func(t *testing.T) {
				old := currentTheme
				t.Cleanup(func() { applyThemePalette(old) })
				tui := newIncrementalTestTUI()
				tui.themeSpecs = []themesys.Spec{{Name: "candidate", Colors: themesys.DefaultColors()}}
				tui.setTheme(themesys.Default)
				tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: strings.Repeat("visible-body\n", 80)})
				tui.syncContent()
				if paused {
					tui.chat.SetTranscriptYOffset(10)
					tui.chat.ManualScrollPaused = true
					tui.chat.FollowBottom = false
				}
				offset := tui.chat.TranscriptYOffset
				before := tui.chat.Viewport.GetContent()
				// 使用真实运行时的异步分支，但不依赖终端或启动事件循环。
				tui.program = tea.NewProgram(tui, tea.WithoutRenderer(), tea.WithInput(nil))
				tui.mode = uipage.Config
				tui.config.FromMode = uipage.Chat
				tui.config.Page = "home"
				switch action {
				case "notification":
					tui.Update(configStateMsg{Params: protocol.ConfigParams{Theme: "candidate", Locale: string(LocaleZH)}})
				case "locale":
					tui.Update(configStateMsg{Params: protocol.ConfigParams{Locale: string(LocaleEN)}})
				default:
					tui.themeBeforePreview = themesys.Default
					tui.chat.SetThemeItems([]chatpage.ThemeItem{{Name: themesys.Default}, {Name: "candidate"}}, themesys.Default)
					tui.chat.ThemeOverlayOpen = true
					tui.Update(tea.KeyPressMsg{Code: tea.KeyDown})
					if got := tui.chat.Viewport.GetContent(); got != before || len(tui.chat.TranscriptBlocks) == 0 {
						t.Fatal("theme preview discarded the visible transcript before chat sync")
					}
					if action == "apply" {
						tui.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					} else if action == "cancel" {
						tui.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
					} else {
						tui.leaveConfig()
					}
				}
				if action != "forced-close" {
					// 已有帧在配置页到达也不能丢失尚未结算的正文更新。
					tui.flushScheduledTranscriptSync()
				}
				var cmd tea.Cmd
				if action == "forced-close" {
					_, cmd = tui.Update(struct{}{})
				} else {
					_, cmd = tui.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				}
				if tui.mode != uipage.Chat || cmd == nil || !tui.transcriptSyncDirty {
					t.Fatalf("return to chat did not schedule refresh: mode=%v cmd-nil=%v dirty=%v", tui.mode, cmd == nil, tui.transcriptSyncDirty)
				}
				tui.viewChat()
				tui.Update(transcriptSyncMsg{})
				if got := ansi.Strip(tui.chat.Viewport.GetContent()); !strings.Contains(got, "visible-body") {
					t.Fatalf("chat remained blank without user input: got %q", got)
				}
				if tui.chat.TranscriptYOffset != offset || tui.chat.ManualScrollPaused != paused {
					t.Fatalf("reading position changed: got offset=%d paused=%v, want offset=%d paused=%v", tui.chat.TranscriptYOffset, tui.chat.ManualScrollPaused, offset, paused)
				}
				wantTheme := themesys.Default
				if action == "apply" || action == "notification" {
					wantTheme = "candidate"
				}
				if tui.theme != wantTheme {
					t.Fatalf("theme = %q, want %q", tui.theme, wantTheme)
				}
				if action == "locale" && tui.i18n.Locale() != LocaleEN {
					t.Fatalf("locale = %q, want %q", tui.i18n.Locale(), LocaleEN)
				}
				if tui.transcriptSyncPending || tui.transcriptSyncDirty || tui.transcriptSyncScheduled {
					t.Fatal("refresh flags were not settled after sync")
				}
				tui.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
				if tui.transcriptSyncDirty || tui.transcriptSyncScheduled {
					t.Fatal("settled refresh was scheduled again on ordinary input")
				}
			})
		}
	}
}

func TestPendingChatRefreshSurvivesFrameOnOtherPage(t *testing.T) {
	old := currentTheme
	t.Cleanup(func() { applyThemePalette(old) })
	tui := newIncrementalTestTUI()
	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: "visible-body"})
	tui.syncContent()
	tui.program = tea.NewProgram(tui, tea.WithoutRenderer(), tea.WithInput(nil))
	tui.Update(configStateMsg{Params: protocol.ConfigParams{Theme: themesys.Default}})
	tui.mode = uipage.Config
	tui.config.FromMode = uipage.Chat
	tui.config.Page = "home"
	tui.Update(transcriptSyncMsg{})
	if !tui.transcriptSyncDirty || tui.transcriptSyncScheduled {
		t.Fatal("frame on another page discarded dirty state or kept an expired frame")
	}
	_, cmd := tui.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil || !tui.transcriptSyncScheduled {
		t.Fatal("return to chat did not reschedule the expired frame")
	}
	tui.Update(transcriptSyncMsg{})
	if !strings.Contains(ansi.Strip(tui.chat.Viewport.GetContent()), "visible-body") {
		t.Fatal("rescheduled frame did not restore chat")
	}
}

func TestPendingSelectionRefreshSurvivesFrameOnOtherPage(t *testing.T) {
	tui := newIncrementalTestTUI()
	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: strings.Repeat("selection-body\n", 40)})
	tui.syncContent()
	start := tui.chat.TranscriptYOffset
	tui.selection.Begin(start, SelectionRegionTranscript)
	tui.selection.Extend(start+1, tui.chat.TranscriptTotalLines)
	tui.selectionDirty = true
	tui.scheduleChatFrame()
	tui.mode = uipage.Config
	tui.config.FromMode = uipage.Chat
	tui.config.Page = "home"
	tui.Update(transcriptSyncMsg{})
	if !tui.selectionDirty || tui.transcriptSyncScheduled {
		t.Fatal("frame on another page lost the pending selection or kept an expired frame")
	}
	_, cmd := tui.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil || !tui.transcriptSyncScheduled || tui.transcriptSyncDirty {
		t.Fatal("return to chat must schedule selection-only refresh")
	}
	tui.Update(transcriptSyncMsg{})
	if tui.selectionDirty || tui.chat.SelectionStart != start || tui.chat.SelectionEnd != start+1 {
		t.Fatal("rescheduled selection refresh did not update highlight range")
	}
}

func TestThemeRefreshSettlesAfterSessionReplacement(t *testing.T) {
	old := currentTheme
	t.Cleanup(func() { applyThemePalette(old) })
	tui := newIncrementalTestTUI()
	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: "old-session-body"})
	tui.syncContent()
	tui.program = tea.NewProgram(tui, tea.WithoutRenderer(), tea.WithInput(nil))
	tui.mode = uipage.Config
	tui.Update(configStateMsg{Params: protocol.ConfigParams{Theme: themesys.Default}})
	tui.mode = uipage.Chat
	tui.applySessionSnapshot(protocol.SessionSnapshot{
		Session:  protocol.SessionInfo{ID: "new-session"},
		Messages: []protocol.SnapshotMessage{{Role: "assistant", Content: "new-session-body"}},
	})
	tui.syncContent()
	if got := ansi.Strip(tui.chat.Viewport.GetContent()); !strings.Contains(got, "new-session-body") || strings.Contains(got, "old-session-body") {
		t.Fatalf("snapshot refresh contains stale session: got %q", got)
	}
	if tui.takeTranscriptSyncCmd() != nil {
		t.Fatal("snapshot refresh left a redundant theme rebuild pending")
	}
}
