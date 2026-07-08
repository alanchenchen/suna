package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

const chatMaxCommandSuggestions = chatpage.MaxCommandSuggestions

type phase = chatpage.Phase

type manualCompactRequestMsg struct{}
type transcriptSyncMsg struct{}
type inputCursorBlinkMsg struct{}
type clipboardImagePasteMsg struct {
	StartedAt time.Time
	Pending   pendingImagePaste
	Blocked   bool
	Err       error
}

// transcriptSyncFrameInterval 只限制 TUI 聊天正文的同步频率，不影响 daemon 收流；
// 16ms 约等于 60fps，比 8ms/125fps 更适合终端渲染，能降低 VSCode renderer 压力。
const transcriptSyncFrameInterval = 16 * time.Millisecond

const (
	phaseIdle             = chatpage.PhaseIdle
	phaseFirstLLM         = chatpage.PhaseFirstLLM
	phaseLLM              = chatpage.PhaseLLM
	phaseThinking         = chatpage.PhaseThinking
	phaseTool             = chatpage.PhaseTool
	phaseWaitingAfterTool = chatpage.PhaseWaitingAfterTool
)

var (
	styleUserLine   = lipgloss.NewStyle().Foreground(ColorUser).Bold(true)
	styleAgentLine  = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolPill   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorTool).Padding(0, 1).Bold(true)
	styleToolOk     = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolErr    = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleToolRun    = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styleToolDim    = lipgloss.NewStyle().Foreground(ColorDim)
	styleToolIntent = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	styleToolAdd    = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolDel    = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleMetaPill   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorBrand).Padding(0, 1).Bold(true)
	styleGuardOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorAgent).Padding(0, 1).Bold(true)
	styleGuardWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorTool).Padding(0, 1).Bold(true)
	styleGuardErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ColorError).Padding(0, 1).Bold(true)
	styleFilePath   = lipgloss.NewStyle().Foreground(ColorHL).Bold(true)
	styleSysLine    = lipgloss.NewStyle().Foreground(ColorDim)
	styleErrLine    = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
)

type toolStatus = toolview.Status

const (
	toolRunning = toolview.StatusRunning
	toolDone    = toolview.StatusDone
	toolError   = toolview.StatusError
)

type toolEntry = toolview.Entry
type guardInfo = toolview.GuardInfo
type toolBlock = toolview.Block

type commandSpec = chatpage.CommandSpec

const (
	// displayMemoryLimitBytes 限制 TUI 自身可控的聊天展示数据；超过后按 turn 从顶部释放到低水位。
	displayMemoryLimitBytes = 32 * 1024 * 1024
)

func (t *TUI) initChatComponents() tea.Cmd {
	t.chat.InitComponents(chatpage.ComponentDeps{
		Placeholder:    t.tr("tui.chat.input_placeholder"),
		TextareaStyles: textareaStyles(),
		SpinnerStyle:   lipgloss.NewStyle().Foreground(ColorBrand),
	})

	t.syncContent()
	t.layoutChat()
	t.syncContent()
	t.chat.RestorePendingInput()

	return tea.Batch(t.syncInputFocus(), t.startInputCursorBlink())
}

func (t *TUI) syncContent() {
	t.transcriptSyncDirty = false
	t.chat.SyncTranscript(chatpage.TranscriptDeps{
		Width:         t.width,
		MarkdownWidth: max(24, t.width-8),
		Theme:         currentTheme.Name,
		ReasoningMode: reasoningRenderMode(t.chat.ShowReasoningDetail),
		SunaLabel:     t.tr("tui.chat.suna"),
		AskHelp:       t.tr("tui.ask.help"),
		AskChoiceHelp: t.tr("tui.ask.choice_help"),
		RenderSunaHeader: func(label string) string {
			return "\n  " + styleAgentLine.Render("● "+label) + "\n"
		},
		RenderDisplayDiscard: t.renderDisplayDiscardSummary,
		RenderUserMessage:    t.renderUserMessage,
		RenderAssistant:      t.renderAssistantMessage,
		RenderReasoning:      t.renderReasoningMessage,
		RenderToolBlock:      t.renderToolBlock,
		RenderSubtaskBlock:   t.renderSubtaskBlock,
		RenderError:          t.renderErrorMessage,
		RenderRestoreSummary: t.renderRestoreSummaryBox,
		RenderSkillLoad:      t.renderSkillLoadMessage,
		RenderSkillReview:    t.renderSkillReviewMessage,
		RenderSystem: func(content string) string {
			return t.renderSystemMessage(content)
		},
		RenderAskSelected: func(opt string) string {
			return fmt.Sprintf("  %s %s\n", styleToolOk.Render("●"), styleAgentLine.Render(opt))
		},
		RenderAskOption: func(opt string) string {
			return fmt.Sprintf("  %s %s\n", styleToolDim.Render("○"), styleSysLine.Render(opt))
		},
		RenderAskHelp: func(help string) string {
			return styleDim.Render("  "+help) + "\n\n"
		},
		RenderModelPicker:       t.renderModelPicker,
		RenderStatusLine:        t.renderCurrentStatusLine,
		RenderCompactStatusLine: t.renderCompactStatusLine,
		HasVisibleProgress:      t.hasVisibleActiveProgress,
	})
}

func (t *TUI) scheduleTranscriptSync() tea.Cmd {
	t.transcriptSyncDirty = true
	if t.transcriptSyncScheduled {
		return nil
	}
	t.transcriptSyncScheduled = true
	return tea.Tick(transcriptSyncFrameInterval, func(time.Time) tea.Msg {
		return transcriptSyncMsg{}
	})
}

func (t *TUI) flushScheduledTranscriptSync() {
	t.transcriptSyncScheduled = false
	if !t.transcriptSyncDirty || t.mode != uipage.Chat {
		return
	}
	t.trimDisplayHistoryIfNeeded()
	t.syncContent()
}

func (t *TUI) trimDisplayHistoryIfNeeded() bool {
	return t.chat.TrimDisplayHistory(displayMemoryLimitBytes)
}

const textStreamSpinnerSuppressWindow = 120 * time.Millisecond

func (t *TUI) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case transcriptSyncMsg:
		t.flushScheduledTranscriptSync()
		return t, nil

	case tea.WindowSizeMsg:
		t.width = m.Width
		t.height = m.Height
		t.ready = true
		t.layoutChat()
		t.syncContent()
		return t, nil

	case tea.KeyPressMsg:
		return t.updateChatKey(m.String(), msg)

	case spinner.TickMsg:
		if t.chat.Loading || t.chat.Compacting {
			var cmd tea.Cmd
			t.chat.Spinner, cmd = t.chat.Spinner.Update(msg)
			// spinner 字符已用 spinnerPlaceholder 占位，viewChat() 最终输出时替换；
			// 此处只需推进 spinner 帧状态，不触发 transcript 全量重建。
			return t, cmd
		}
		t.chatSpinnerTicking = false
		return t, nil

	case manualCompactRequestMsg:
		return t, t.compactCmd()

	case tea.PasteMsg:
		if t.inputLocked() {
			return t, nil
		}
		t.lastPasteAt = time.Now()
		cmd := t.handlePaste(m.Content)
		t.syncContent()
		return t, cmd

	case clipboardImagePasteMsg:
		if t.inputLocked() || t.lastPasteAt.After(m.StartedAt) {
			return t, nil
		}
		if m.Blocked {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: t.tr("tui.attachment.clipboard_image_too_large")})
			t.syncContent()
			return t, nil
		}
		if m.Err != nil || len(m.Pending.Data) == 0 {
			return t, nil
		}
		t.chat.EnqueueImagePaste(m.Pending)
		t.layoutChat()
		t.syncContent()
		return t, nil

	case tea.MouseMsg:
		if t.chat.SubtaskToolDetailExpanded && t.hasActiveSubtaskPanel() {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				switch mm.Mouse().Button {
				case tea.MouseWheelUp:
					t.scrollSubtaskToolDetail(-t.chat.Viewport.MouseWheelDelta)
				case tea.MouseWheelDown:
					t.scrollSubtaskToolDetail(t.chat.Viewport.MouseWheelDelta)
				}
				t.syncContent()
				return t, nil
			}
		}
		if t.mouseInComposer(m) {
			return t, nil
		}
		if t.chat.ActiveInteractionKind() == chatpage.InteractionGuardConfirm {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				if mm.Mouse().Button == tea.MouseWheelUp {
					t.scrollGuardOverlay(-3)
				} else if mm.Mouse().Button == tea.MouseWheelDown {
					t.scrollGuardOverlay(3)
				}
				t.syncContent()
			}
			return t, nil
		}
		if t.chat.ShowToolDetail {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				if mm.Mouse().Button == tea.MouseWheelUp {
					t.scrollToolDetailOverlay(-3)
				} else if mm.Mouse().Button == tea.MouseWheelDown {
					t.scrollToolDetailOverlay(3)
				}
			}
			return t, nil
		}
		if mm, ok := any(m).(tea.MouseWheelMsg); ok {
			mouse := mm.Mouse()
			if !mouse.Mod.Contains(tea.ModShift) {
				delta := 0
				switch mouse.Button {
				case tea.MouseWheelUp:
					delta = -t.chat.Viewport.MouseWheelDelta
				case tea.MouseWheelDown:
					delta = t.chat.Viewport.MouseWheelDelta
				}
				if delta != 0 {
					// Chat viewport 只持有当前 transcript window 的切片，不能先交给 Bubble viewport
					// 处理垂直滚轮；否则会被当前窗口的局部 max offset 截断，无法跨 window 滚动。
					if t.chat.ScrollTranscript(delta) {
						t.syncContent()
					}
					return t, nil
				}
			}
		}
		var cmd tea.Cmd
		oldOffset := t.chat.Viewport.YOffset()
		t.chat.Viewport, cmd = t.chat.Viewport.Update(msg)
		delta := t.chat.Viewport.YOffset() - oldOffset
		if delta != 0 {
			if t.chat.ScrollTranscript(delta) {
				t.syncContent()
			}
		} else {
			t.chat.FollowBottom = t.chat.TranscriptAtBottom()
		}
		return t, cmd
	}

	if t.chat.HasDiscardDraftConfirm() {
		t.chat.CancelDiscardDraft()
	}

	var cmd tea.Cmd
	t.chat.Textarea, cmd = t.chat.Textarea.Update(msg)

	t.updateCmdSuggestionState()
	t.layoutChat()

	return t, cmd
}

func (t *TUI) recentTextStreamActive(now time.Time) bool {
	if t.lastTextStreamAt.IsZero() {
		return false
	}
	if now.Sub(t.lastTextStreamAt) > textStreamSpinnerSuppressWindow {
		return false
	}
	for i := len(t.chat.Messages) - 1; i >= 0; i-- {
		msg := t.chat.Messages[i]
		if msg.Streaming && (msg.Role == "assistant" || msg.Role == "reasoning") {
			return true
		}
		if msg.Role == "assistant" || msg.Role == "reasoning" || msg.Role == "user" || msg.Role == "error" || msg.Role == "system" {
			break
		}
	}
	return false
}
