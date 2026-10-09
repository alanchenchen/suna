package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alanchenchen/suna/internal/protocol"
)

// 使用真实键盘事件，避免直接设置草稿绕过根 Update 的资格比较。
func incrementalDependencyKey(code rune, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Text: text})
}

func newIncrementalSubtaskDependencyTUI() *TUI {
	tui := newIncrementalTestTUI()
	tui.appendNonToolMessage(chatMsg{Role: "user", Content: userMessageContent{Text: "较早输入"}})
	tui.appendNonToolMessage(chatMsg{Role: "user", Content: userMessageContent{Text: "最近输入"}})
	block := tui.chat.EnsureToolBlock()
	// 无子工具的面板必须让方向键落到输入历史，仍保留 Enter 页脚资格。
	block.Add(&toolEntry{ID: "subtask", RawName: "spawn", Intent: "测试任务", Status: toolRunning})
	tui.syncContent()
	return tui
}

func incrementalDependencyText(tui *TUI) string {
	return tui.chat.SelectionPlainText(0, tui.chat.TranscriptTotalLines-1)
}

func assertIncrementalSubtaskFooter(t *testing.T, tui *TUI, visible bool) {
	t.Helper()
	// 整条帮助可能被盒子截断，只检查稳定的开头，且检查已同步的正文而非即时渲染器。
	if got := strings.Contains(incrementalDependencyText(tui), "Tab 切换子任务"); got != visible {
		t.Fatalf("cached subtask footer visible = %v, want %v: %q", got, visible, incrementalDependencyText(tui))
	}
}

func updateIncrementalDependency(t *testing.T, tui *TUI, msg tea.Msg, draft string, changed bool) {
	t.Helper()
	before := incrementalDependencyText(tui)
	_, cmd := tui.Update(msg)
	if got := tui.chat.Textarea.Value(); got != draft {
		t.Fatalf("%T draft = %q, want %q", msg, got, draft)
	}
	if tui.transcriptSyncDirty != changed || tui.transcriptSyncScheduled != changed {
		t.Fatalf("%T frame dirty/scheduled = %v/%v, want %v", msg, tui.transcriptSyncDirty, tui.transcriptSyncScheduled, changed)
	}
	if changed && cmd == nil {
		t.Fatalf("%T did not return a frame command", msg)
	}
	if got := incrementalDependencyText(tui); got != before {
		t.Fatalf("%T rebuilt body before the scheduled frame", msg)
	}
	// 手动投递帧消息，不执行光标闪烁或 spinner 命令，也不等待真实定时器。
	tui.Update(transcriptSyncMsg{})
	if tui.transcriptSyncDirty || tui.transcriptSyncScheduled {
		t.Fatal("frame did not clear transcript scheduling")
	}
	assertIncrementalSubtaskFooter(t, tui, strings.TrimSpace(draft) == "")
}

// 空草稿与非空草稿的边界才改变页脚；普通连续输入和删除不重建正文。
func TestRootIncrementalSubtaskFooterTypingAndBackspace(t *testing.T) {
	tui := newIncrementalSubtaskDependencyTUI()
	assertIncrementalSubtaskFooter(t, tui, true)
	for _, step := range []struct {
		key     tea.KeyPressMsg
		draft   string
		changed bool
	}{
		{incrementalDependencyKey('a', "a"), "a", true},
		{incrementalDependencyKey('b', "b"), "ab", false},
		{incrementalDependencyKey(tea.KeyBackspace, ""), "a", false},
		{incrementalDependencyKey(tea.KeyBackspace, ""), "", true},
		{incrementalDependencyKey(' ', " "), " ", false},
		{incrementalDependencyKey('c', "c"), " c", true},
		{incrementalDependencyKey(tea.KeyBackspace, ""), " ", true},
	} {
		updateIncrementalDependency(t, tui, step.key, step.draft, step.changed)
	}
	assertRootTranscriptParity(t, tui)
}

// 历史浏览经过同一事件边界，恢复空草稿时必须重新显示缓存页脚。
func TestRootIncrementalSubtaskFooterInputHistory(t *testing.T) {
	tui := newIncrementalSubtaskDependencyTUI()
	updateIncrementalDependency(t, tui, incrementalDependencyKey(tea.KeyUp, ""), "最近输入", true)
	if !tui.chat.InputHistoryActive {
		t.Fatal("up did not enter input history")
	}
	updateIncrementalDependency(t, tui, incrementalDependencyKey(tea.KeyUp, ""), "较早输入", false)
	updateIncrementalDependency(t, tui, incrementalDependencyKey(tea.KeyDown, ""), "最近输入", false)
	updateIncrementalDependency(t, tui, incrementalDependencyKey(tea.KeyDown, ""), "", true)
	if tui.chat.InputHistoryActive {
		t.Fatal("down did not restore the original draft")
	}
	assertRootTranscriptParity(t, tui)
}

// Steering 通知与撤回响应只恢复输入，不改变消息正文，仍需更新页脚依赖。
func TestRootIncrementalSubtaskFooterSteeringRestoration(t *testing.T) {
	for _, state := range []protocol.SteeringState{protocol.SteeringRemoved, protocol.SteeringRejected} {
		for _, response := range []bool{false, true} {
			name := string(state) + "/notification"
			if response {
				name = string(state) + "/method_response"
			}
			t.Run(name, func(t *testing.T) {
				tui := newIncrementalSubtaskDependencyTUI()
				tui.activeRunID = "run"
				p := protocol.SteeringMessage{ID: "returned", RunID: "run", State: state, CanControl: true, Parts: textPartsForTUI("退回草稿")}
				var msg tea.Msg = steeringMsg{Params: p}
				if response {
					msg = steerRemoveResultMsg{Message: p}
				}
				body := tui.notificationBodyState()
				updateIncrementalDependency(t, tui, msg, "退回草稿", true)
				if tui.notificationBodyState() != body {
					t.Fatal("steering restoration changed body state")
				}
				// 重复终态不能再次恢复草稿或无条件登记正文同步。
				updateIncrementalDependency(t, tui, msg, "退回草稿", false)
				assertRootTranscriptParity(t, tui)
			})
		}
	}
}

// 其他客户端接入只改变提示块高度；贴底意图与手动阅读锚点都不能丢失。
func TestRootIncrementalSessionMetadataLayoutScroll(t *testing.T) {
	for _, response := range []bool{false, true} {
		for _, mode := range []string{"following", "at_bottom", "paused", "paused_follow_flag"} {
			name := "notification/" + mode
			if response {
				name = "method_response/" + mode
			}
			t.Run(name, func(t *testing.T) {
				tui := newIncrementalTestTUI()
				tui.currentSession.ClientCount = 1
				tui.currentSession.Status = protocol.SessionStatusIdle
				tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: "```text\n" + strings.Repeat("保留阅读位置\n", 80) + "```"})
				tui.layoutChat()
				tui.syncContent()
				// 回复导航提示由首次同步建立，再按稳定提示高度记录布局基线。
				tui.layoutChat()
				tui.chat.SetTranscriptYOffset(tui.chat.TranscriptMaxYOffset())
				tui.chat.FollowBottom = mode != "at_bottom" && mode != "paused"
				paused := strings.HasPrefix(mode, "paused")
				if paused {
					tui.chat.SetTranscriptYOffset(7)
					tui.chat.ManualScrollPaused = true
				}
				oldHeight, oldOffset := tui.chat.Viewport.Height(), tui.chat.TranscriptYOffset
				oldAnchor := tui.chat.SelectionPlainText(oldOffset, oldOffset)
				body, total, text := tui.notificationBodyState(), tui.chat.TranscriptTotalLines, incrementalDependencyText(tui)
				if tui.chat.TranscriptMaxYOffset() == 0 {
					t.Fatal("fixture does not overflow viewport")
				}
				for _, count := range []int{2, 1} {
					session := tui.currentSession
					session.ClientCount = count
					var msg tea.Msg = sessionStateMsg{Params: protocol.SessionStateParams{Session: session}}
					if response {
						msg = sessionMetadataResultMsg{Session: session}
					}
					tui.Update(msg)
					if tui.currentSession.ClientCount != count {
						t.Fatalf("client count = %d, want %d", tui.currentSession.ClientCount, count)
					}
					if tui.transcriptSyncDirty || tui.transcriptSyncScheduled || tui.chat.NewContentWhilePaused {
						t.Fatal("body-neutral metadata scheduled body sync or marked new content")
					}
					if tui.notificationBodyState() != body || tui.chat.TranscriptTotalLines != total || incrementalDependencyText(tui) != text {
						t.Fatal("metadata changed retained transcript")
					}
					if count == 2 && tui.chat.Viewport.Height() >= oldHeight {
						t.Fatal("joining client did not shrink viewport for the handoff hint")
					}
					if count == 1 && tui.chat.Viewport.Height() != oldHeight {
						t.Fatalf("leaving client did not restore viewport height: got %d, want %d; hint=%q", tui.chat.Viewport.Height(), oldHeight, tui.renderPreInputHint())
					}
					if paused {
						if tui.chat.TranscriptYOffset != oldOffset || tui.chat.SelectionPlainText(oldOffset, oldOffset) != oldAnchor || !tui.chat.ManualScrollPaused {
							t.Fatal("metadata resize lost manual reading anchor")
						}
					} else if !tui.chat.TranscriptAtBottom() || tui.chat.TranscriptYOffset != tui.chat.TranscriptMaxYOffset() {
						t.Fatal("metadata resize lost bottom-following intent")
					}
				}
			})
		}
	}
}
