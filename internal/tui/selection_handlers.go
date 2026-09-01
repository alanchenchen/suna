package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/tui/clipboard"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

// 选区边缘自动滚动参数：拖动到内容区边缘 2 行内触发，每帧滚动 1 行（约 16 行/秒，
// 比之前的 2 行/40ms 慢一倍，避免过于灵敏）。
const (
	selectionEdgeZoneRows = 2
	selectionEdgeStepRow  = 1
	// selectionEdgeScrollInterval 是 edge scroll 的帧间隔（60ms），
	// 低于 transcript 帧同步（16ms），滚动平滑且不抢渲染。
	selectionEdgeScrollInterval = 60 * time.Millisecond
	// selectionCopyFeedbackDuration 是状态栏“已复制”反馈的显示时长。
	selectionCopyFeedbackDuration = 1500 * time.Millisecond
)

// selectionEdgeScrollMsg 驱动 edge scroll 的 tick 消息。
type selectionEdgeScrollMsg struct{}

// viewportStartY 返回 chat 页 transcript 内容区在终端中的起始行（0 基）。
// 布局：pet 头部（2-3 行）+ 顶部分隔线 1 行。头部行数由 MiniPet 渲染决定，
// 与 viewChat 的布局保持一致。
func (t *TUI) viewportStartY() int {
	petLines := chatpage.RenderedLineCount(renderMiniPet(t.chatPetState(), t.petFrame))
	if petLines < 2 {
		petLines = 2
	}
	return petLines + 1 // pet 头部 + 分隔线
}

// mouseYToContentLine 把鼠标 Y（终端绝对坐标）映射为 transcript 内容行索引。
// 边界外 clamp 到首/末行，供选区与边缘检测使用。
func (t *TUI) mouseYToContentLine(mouseY int) int {
	startY := t.viewportStartY()
	line := mouseY - startY + t.chat.TranscriptYOffset
	total := t.chat.TranscriptTotalLines
	if total <= 0 {
		return 0
	}
	return min(max(line, 0), total-1)
}

// mouseInSelectionEdge 判断鼠标 Y 是否位于内容区边缘（用于 edge scroll）。
// 返回 0=不在边缘，-1=顶部边缘（向上滚动），1=底部边缘（向下滚动）。
func (t *TUI) mouseInSelectionEdge(mouseY int) int {
	startY := t.viewportStartY()
	height := t.chat.Viewport.Height()
	if height <= 0 {
		return 0
	}
	if mouseY <= startY+selectionEdgeZoneRows {
		return -1
	}
	if mouseY >= startY+height-selectionEdgeZoneRows-1 {
		return 1
	}
	return 0
}

// pauseTranscriptFollowDuringSelection 在选区按下时暂停 transcript 自动跟随：
// 拖动中若有消息流式到达，内容跳动会导致选区锚定行错位（浏览器中选中文本时
// 页面也不会自动滚动）。释放/清除时由 restoreTranscriptFollowAfterSelection 恢复。
func (t *TUI) pauseTranscriptFollowDuringSelection() {
	t.chat.ManualScrollPaused = true
}

// restoreTranscriptFollowAfterSelection 选区结束时按当前位置恢复跟随：
// 在底部则恢复自动跟随，否则保持暂停（与手动滚动后的行为一致）。
func (t *TUI) restoreTranscriptFollowAfterSelection() {
	t.updateTranscriptFollowAfterNavigation()
}

// handleSelectionMouse 处理内容区鼠标事件（按下/拖动/释放），驱动选区状态机。
// 返回是否已消费该事件（不再交给 viewport 处理）以及需要执行的 cmd（edge scroll tick）。
// 释放事件按消息类型判断（部分终端 SGR 协议下 release 的 Button 可能带按钮号），
// 不依赖 Button == MouseNone。
func (t *TUI) handleSelectionMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	total := t.chat.TranscriptTotalLines
	if total <= 0 {
		return false, nil
	}
	switch msg := msg.(type) {
	case tea.MouseReleaseMsg:
		// 释放：结束拖动（单击自动清除，拖动定格）。
		if !t.selection.Active {
			return false, nil
		}
		t.selection.Finish()
		t.stopSelectionEdgeScroll()
		t.restoreTranscriptFollowAfterSelection()
		t.syncContent()
		return true, nil
	case tea.MouseWheelMsg:
		return false, nil
	case tea.MouseMotionMsg:
		// 拖动中：更新终点行，并检测边缘启动/停止自动滚动。
		if !t.selection.Active {
			return false, nil
		}
		t.lastSelectionMouseY = msg.Mouse().Y
		t.selection.Extend(t.mouseYToContentLine(msg.Mouse().Y), total)
		var cmd tea.Cmd
		if edge := t.mouseInSelectionEdge(msg.Mouse().Y); edge != 0 {
			if !t.selectionEdgeTicking {
				cmd = t.startSelectionEdgeScroll(edge)
			} else if edge != t.selectionEdgeDirection {
				// 反向跨页：tick 链运行中也要更新方向，否则反向拖动不生效。
				t.selectionEdgeDirection = edge
			}
		} else if t.selectionEdgeTicking {
			// 移出边缘区：立即停止自动滚动（浏览器/编辑器行为）。
			t.stopSelectionEdgeScroll()
		}
		// 拖动高亮走帧门同步：Motion 事件频率取决于终端报告速率（可达 1000Hz），
		// 直接 syncContent 会对每次事件做全量 transcript 重建，长历史下 CPU 饱和；
		// 帧门后与流式渲染同 cadence（16ms），高亮延迟一帧无感。
		cmd = tea.Batch(cmd, t.scheduleTranscriptSync())
		return true, cmd
	default:
		// 按下（MouseClickMsg 或其它）：左键开始拖动，其它按钮忽略。
		m := msg.Mouse()
		if m.Button != tea.MouseLeft || t.selection.Active {
			return false, nil
		}
		t.selection.Begin(t.mouseYToContentLine(m.Y), SelectionRegionTranscript)
		t.lastSelectionMouseY = m.Y
		t.pauseTranscriptFollowDuringSelection()
		t.syncContent()
		return true, nil
	}
}

// startSelectionEdgeScroll 启动 edge scroll tick 链（防重入，仿 pet/spinner 模式）。
func (t *TUI) startSelectionEdgeScroll(direction int) tea.Cmd {
	t.selectionEdgeDirection = direction
	t.selectionEdgeTicking = true
	return tea.Tick(selectionEdgeScrollInterval, func(time.Time) tea.Msg {
		return selectionEdgeScrollMsg{}
	})
}

// stopSelectionEdgeScroll 停止 edge scroll tick 链：清标志并复位方向。
// tick 处理函数（updateSelectionEdgeScroll）检查标志，被清后不再重新调度，
// 链在此真正断掉（否则已调度的 tick 会继续滚动并无限续链）。
func (t *TUI) stopSelectionEdgeScroll() {
	t.selectionEdgeTicking = false
	t.selectionEdgeDirection = 0
}

// updateSelectionEdgeScroll 处理 edge scroll tick：滚动 transcript 并扩展选区。
// 浏览器/编辑器行为：贴边期间持续滚动（鼠标不动也滚），移出边缘区由
// Motion 分支停止 tick 链（见 handleSelectionMouse）。
// 注意：tick 消息可能在本帧已调度后标志才被清，因此这里必须检查标志，
// 被停后返回 nil（不再续链），否则会无限循环滚动。
func (t *TUI) updateSelectionEdgeScroll() tea.Cmd {
	if !t.selection.Active || !t.selectionEdgeTicking {
		t.stopSelectionEdgeScroll()
		return nil
	}
	if t.selectionEdgeDirection != 0 {
		delta := t.selectionEdgeDirection * selectionEdgeStepRow
		t.chat.ScrollTranscript(delta)
		t.selection.Extend(t.mouseYToContentLine(t.lastSelectionMouseY), t.chat.TranscriptTotalLines)
		t.syncContent()
	}
	return tea.Tick(selectionEdgeScrollInterval, func(time.Time) tea.Msg {
		return selectionEdgeScrollMsg{}
	})
}

// inputSelectionStartY 返回输入区在终端中的起始行（0 基），与 MouseInComposer 的
// composerStart 计算保持一致：输入区从底部向上数（预输入提示 + 输入区 + 建议 + 2 行固定）。
func (t *TUI) inputSelectionStartY() int {
	inputAreaH := max(1, chatpage.RenderedLineCount(t.renderInputArea()))
	suggestionH := max(0, chatpage.RenderedLineCount(t.renderCommandSuggestions()))
	preInputHintH := max(0, chatpage.RenderedLineCount(t.renderPreInputHint()))
	return t.height - (preInputHintH + inputAreaH + suggestionH + 2)
}

// handleInputSelectionMouse 处理输入区鼠标事件（按下/拖动/释放），驱动输入区选区。
// 输入区选区锚定输入区行索引（0..inputAreaH-1），用于复制输入框草稿。
// 输入区行数少，不做 edge scroll。
func (t *TUI) handleInputSelectionMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	inputAreaH := max(1, chatpage.RenderedLineCount(t.renderInputArea()))
	startY := t.inputSelectionStartY()
	switch msg := msg.(type) {
	case tea.MouseReleaseMsg:
		if !t.selection.Active || t.selection.Region != SelectionRegionInput {
			return false, nil
		}
		t.selection.Finish()
		t.syncContent()
		return true, nil
	case tea.MouseWheelMsg:
		return false, nil
	case tea.MouseMotionMsg:
		if !t.selection.Active || t.selection.Region != SelectionRegionInput {
			return false, nil
		}
		line := min(max(msg.Mouse().Y-startY, 0), inputAreaH-1)
		t.selection.Extend(line, inputAreaH)
		// 输入区选区同样走帧门：syncContent 会同步整个 transcript，拖动事件频率不受控。
		return true, t.scheduleTranscriptSync()
	default:
		m := msg.Mouse()
		if m.Button != tea.MouseLeft || t.selection.Active {
			return false, nil
		}
		line := min(max(m.Y-startY, 0), inputAreaH-1)
		t.selection.Begin(line, SelectionRegionInput)
		t.syncContent()
		return true, nil
	}
}

// copySelection 把选区纯文本写入系统剪贴板，并显示“已复制”反馈。
// 输入区选区提取 textarea 草稿对应行；transcript 选区提取内容行范围。
func (t *TUI) copySelection() {
	if !t.selection.HasSelection {
		return
	}
	var text string
	if t.selection.Region == SelectionRegionInput {
		start, end := t.selection.LineRange()
		lines := strings.Split(strings.TrimRight(t.chat.Textarea.Value(), "\n"), "\n")
		if start >= 0 && start < len(lines) {
			to := min(end, len(lines)-1)
			text = strings.Join(lines[start:to+1], "\n")
		}
	} else {
		start, end := t.selection.LineRange()
		text = t.chat.SelectionPlainText(start, end)
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	write := t.clipboardWrite
	if write == nil {
		write = clipboard.WriteText
	}
	if err := write(text); err != nil {
		return
	}
	t.copyFeedbackText = t.i18n.Tf("tui.selection.copied_chars", len([]rune(text)))
	t.copyFeedbackUntil = time.Now().Add(selectionCopyFeedbackDuration)
	t.selection.Clear()
	// 复制后恢复 transcript 跟随（选区按下时暂停了跟随），与 Esc/释放路径一致。
	t.restoreTranscriptFollowAfterSelection()
	t.syncContent()
}
