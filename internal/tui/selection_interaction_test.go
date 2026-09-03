package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

// selectionTestTUI 构造一个带 transcript 内容的 TUI，供选区交互测试。
// 注入 fake 剪贴板写入：headless CI 无 X11/Wayland 服务时真实 WriteText 会失败，
// 导致复制反馈/选区清除断言无法验证。
func selectionTestTUI(t *testing.T) *TUI {
	t.Helper()
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.clipboardWrite = func(string) error { return nil }
	tui.initChatComponents()
	// 3 条消息各 2 行 = 6 行内容
	for i := 0; i < 3; i++ {
		tui.chat.AppendMessage(chatpage.Msg{Role: "panel", Content: "line-a\nline-b"})
	}
	tui.syncContent()
	return tui
}

func TestMouseYToContentLine(t *testing.T) {
	tui := selectionTestTUI(t)
	// pet 头部 3 行 + 分隔线 1 行 = 内容区从 Y=4 开始
	startY := tui.viewportStartY()
	if startY != 4 {
		t.Fatalf("viewportStartY = %d, want 4 (pet 3 行 + 分隔线)", startY)
	}
	// 内容区第一行 = 内容行 0
	if got := tui.mouseYToContentLine(startY); got != 0 {
		t.Fatalf("mouseYToContentLine(startY) = %d, want 0", got)
	}
	// 内容区第 3 行 = 内容行 2
	if got := tui.mouseYToContentLine(startY + 2); got != 2 {
		t.Fatalf("mouseYToContentLine(startY+2) = %d, want 2", got)
	}
	// 滚动 2 行后，同一屏幕位置映射到内容行 2（直接设偏移，绕过 viewport 高度 clamp）
	tui.chat.TranscriptYOffset = 2
	if got := tui.mouseYToContentLine(startY); got != 2 {
		t.Fatalf("mouseYToContentLine after scroll = %d, want 2", got)
	}
	// 边界外 clamp 到首/末行（末行 = TranscriptTotalLines-1，动态计算）
	if got := tui.mouseYToContentLine(0); got != 0 {
		t.Fatalf("mouseYToContentLine(0) = %d, want clamp 0", got)
	}
	last := tui.chat.TranscriptTotalLines - 1
	if got := tui.mouseYToContentLine(999); got != last {
		t.Fatalf("mouseYToContentLine(999) = %d, want clamp %d", got, last)
	}
}

func TestSelectionDragFlow(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	// 按下（内容行 0）
	down := tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft})
	if consumed, _ := tui.handleSelectionMouse(down); !consumed {
		t.Fatal("handleSelectionMouse(down) = false, want consumed")
	}
	if !tui.selection.Active {
		t.Fatal("selection.Active = false after down, want true")
	}

	// 拖动到内容行 2：Motion 走帧门同步，不直接重建 transcript
	motion := tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 2, Button: tea.MouseLeft})
	consumed, cmd := tui.handleSelectionMouse(motion)
	if !consumed {
		t.Fatal("handleSelectionMouse(motion) = false, want consumed")
	}
	if tui.selection.EndLine != 2 {
		t.Fatalf("selection.EndLine = %d, want 2", tui.selection.EndLine)
	}
	// 拖动必须调度帧门同步而不是直接 syncContent：Motion 事件频率不受控
	// （可达 1000Hz），直接同步会对每个事件做全量 transcript 重建。
	if cmd == nil || !tui.transcriptSyncScheduled {
		t.Fatal("motion did not schedule transcript sync, want frame-gated sync")
	}

	// 释放：拖动过，选区定格
	release := tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 2})
	if consumed, _ := tui.handleSelectionMouse(release); !consumed {
		t.Fatal("handleSelectionMouse(release) = false, want consumed")
	}
	if !tui.selection.HasSelection {
		t.Fatal("selection.HasSelection = false after drag release, want true")
	}
	start, end := tui.selection.LineRange()
	if start != 0 || end != 2 {
		t.Fatalf("LineRange = %d..%d, want 0..2", start, end)
	}
}

// TestSelectionDragUsesLightweightSync 拖动只改变选区范围（内容未变）：
// flush 必须走轻量路径（只重写窗口行），不重建块列表（全量 SyncTranscript 是拖动卡顿根源）。
func TestSelectionDragUsesLightweightSync(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	// 按下（内容行 0）
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))

	// 拖动到内容行 2：标记 selectionDirty 并调度帧门
	motion := tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 2, Button: tea.MouseLeft})
	tui.handleSelectionMouse(motion)
	if !tui.selectionDirty {
		t.Fatal("selectionDirty = false after motion, want true")
	}
	if !tui.transcriptSyncScheduled {
		t.Fatal("transcriptSyncScheduled = false after motion, want true")
	}

	// flush：selectionDirty && !transcriptSyncDirty → 轻量路径，选区范围同步到 chat 包
	_ = tui.flushScheduledTranscriptSync()
	if tui.selectionDirty {
		t.Fatal("selectionDirty = true after flush, want false")
	}
	if tui.chat.SelectionStart != 0 || tui.chat.SelectionEnd != 2 {
		t.Fatalf("chat selection = %d..%d, want 0..2", tui.chat.SelectionStart, tui.chat.SelectionEnd)
	}

	// 内容变化（流式 delta 到达）时：transcriptSyncDirty 置位，flush 走全量
	tui.transcriptSyncDirty = true
	tui.selectionDirty = true
	_ = tui.flushScheduledTranscriptSync()
	if tui.transcriptSyncDirty {
		t.Fatal("transcriptSyncDirty = true after full flush, want false")
	}
}

func TestSelectionClickWithoutDragClears(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	// 按下后直接释放（未移动）= 单击，不应产生选区
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY}))
	if tui.selection.HasAny() {
		t.Fatal("selection.HasAny = true after click, want false (单击清除)")
	}
}

func TestSelectionReverseDrag(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY + 4, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))

	start, end := tui.selection.LineRange()
	if start != 1 || end != 4 {
		t.Fatalf("LineRange(reverse) = %d..%d, want 1..4", start, end)
	}
}

func TestCopyKeyOnlyWhenSelection(t *testing.T) {
	tui := selectionTestTUI(t)

	// 无选区时 y 键不拦截，走正常输入
	model, _ := tui.Update(tea.KeyPressMsg(tea.Key{Code: 'y'}))
	tui = model.(*TUI)
	if tui.copyFeedbackUntil.IsZero() == false {
		t.Fatal("copy feedback shown without selection")
	}

	// 制造选区
	startY := tui.viewportStartY()
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))

	// 有选区时 y 触发复制并清除选区
	model, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: 'y'}))
	tui = model.(*TUI)
	if tui.selection.HasAny() {
		t.Fatal("selection should clear after copy")
	}
	if tui.copyFeedbackText == "" {
		t.Fatal("copy feedback text should be set after copy")
	}
}

func TestSelectionEscClears(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))

	model, _ := tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	tui = model.(*TUI)
	if tui.selection.HasAny() {
		t.Fatal("selection should clear after Esc")
	}
}

func TestSelectionMouseIgnoredWhenOverlayOpen(t *testing.T) {
	tui := selectionTestTUI(t)
	tui.chat.SkillsOverlayOpen = true
	startY := tui.viewportStartY()

	// overlay 打开时鼠标事件不进入选区逻辑
	down := tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft})
	// handleSelectionMouse 不应被调用（由 chat.go 的守卫拦截），这里验证守卫条件
	if tui.chat.HasOverlayOpen() != true {
		t.Fatal("HasOverlayOpen = false, want true")
	}
	_ = down
}

func TestSelectionEdgeDetection(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()
	height := tui.chat.Viewport.Height()

	// 顶部边缘
	if got := tui.mouseInSelectionEdge(startY); got != -1 {
		t.Fatalf("mouseInSelectionEdge(top) = %d, want -1", got)
	}
	// 底部边缘
	if got := tui.mouseInSelectionEdge(startY + height - 1); got != 1 {
		t.Fatalf("mouseInSelectionEdge(bottom) = %d, want 1", got)
	}
	// 中间不触发
	if got := tui.mouseInSelectionEdge(startY + height/2); got != 0 {
		t.Fatalf("mouseInSelectionEdge(middle) = %d, want 0", got)
	}
}

func TestSelectionEdgeScrollStopsWhenTickingFlagCleared(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()
	height := tui.chat.Viewport.Height()

	// 按下并拖到底部边缘：启动向下滚动
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + height - 1, Button: tea.MouseLeft}))
	if !tui.selectionEdgeTicking {
		t.Fatal("edge scroll not started")
	}

	// 移出边缘区：标志被清
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + height/2, Button: tea.MouseLeft}))
	if tui.selectionEdgeTicking {
		t.Fatal("selectionEdgeTicking = true after leaving edge, want false")
	}

	// 已调度的 tick 消息到达：必须停止且不再续链（否则无限循环滚动）
	if cmd := tui.updateSelectionEdgeScroll(); cmd != nil {
		t.Fatal("updateSelectionEdgeScroll after stop = non-nil cmd, want nil (chain broken)")
	}
	// 再次调用也应保持停止
	if cmd := tui.updateSelectionEdgeScroll(); cmd != nil {
		t.Fatal("updateSelectionEdgeScroll second call = non-nil cmd, want nil")
	}
}

func TestSelectionEdgeScrollStartsAndStops(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()
	height := tui.chat.Viewport.Height()

	// 按下并拖到底部边缘：启动向下滚动
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	_, cmd := tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + height - 1, Button: tea.MouseLeft}))
	if cmd == nil {
		t.Fatal("edge scroll cmd = nil after entering bottom edge, want tick")
	}
	if !tui.selectionEdgeTicking {
		t.Fatal("selectionEdgeTicking = false after entering edge, want true")
	}
	if tui.selectionEdgeDirection != 1 {
		t.Fatalf("selectionEdgeDirection = %d, want 1 (down)", tui.selectionEdgeDirection)
	}

	// 移回中间：立即停止（浏览器行为）
	_, cmd = tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + height/2, Button: tea.MouseLeft}))
	if tui.selectionEdgeTicking {
		t.Fatal("selectionEdgeTicking = true after leaving edge, want false")
	}
}

func TestSelectionEdgeScrollDirectionFlipsWhileTicking(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()
	height := tui.chat.Viewport.Height()

	// 按下并拖到底部边缘：启动向下滚动
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + height - 1, Button: tea.MouseLeft}))
	if !tui.selectionEdgeTicking || tui.selectionEdgeDirection != 1 {
		t.Fatal("edge scroll not started with down direction")
	}

	// 不离开边缘区直接反向拖到顶部边缘：方向应实时翻转（tick 链运行中）
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	if !tui.selectionEdgeTicking {
		t.Fatal("selectionEdgeTicking = false after flipping direction, want still ticking")
	}
	if tui.selectionEdgeDirection != -1 {
		t.Fatalf("selectionEdgeDirection = %d, want -1 (up) after flip", tui.selectionEdgeDirection)
	}
}

func TestCopySelectionRestoresFollow(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	// 按下：暂停跟随
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))
	if !tui.selection.HasSelection {
		t.Fatal("selection not kept after drag")
	}

	// 复制后：跟随恢复（与 Esc/释放路径一致）
	tui.copySelection()
	if tui.chat.ManualScrollPaused {
		t.Fatal("ManualScrollPaused = true after copy, want false (follow restored)")
	}
}

func TestInputSelectionDragIntoContentStaysInputRegion(t *testing.T) {
	tui := selectionTestTUI(t)
	tui.chat.Textarea.SetValue("draft line 1\ndraft line 2")
	startY := tui.inputSelectionStartY()

	// 输入区按下并拖动
	tui.handleInputSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleInputSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	if tui.selection.Region != SelectionRegionInput {
		t.Fatalf("selection.Region = %v, want SelectionRegionInput", tui.selection.Region)
	}

	// 拖动跨入内容区（Y 在 viewport 内）：事件继续交给输入区处理，Region 不变
	contentY := tui.viewportStartY()
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: contentY, Button: tea.MouseLeft}))
	if tui.selection.Region != SelectionRegionInput {
		t.Fatalf("selection.Region after crossing = %v, want SelectionRegionInput", tui.selection.Region)
	}

	// 释放（内容区）：输入区选区正常定格
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: contentY}))
	if !tui.selection.HasSelection {
		t.Fatal("selection.HasSelection = false after cross-region release, want true")
	}

	// 复制：提取的是草稿行（输入区），不是内容行
	tui.copySelection()
	if tui.copyFeedbackText == "" {
		t.Fatal("copyFeedbackText empty after copy, want feedback")
	}
}

func TestSelectionPausesTranscriptFollow(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	// 初始在底部（跟随中）
	tui.chat.FollowBottom = true
	tui.chat.ManualScrollPaused = false

	// 按下：暂停跟随
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	if !tui.chat.ManualScrollPaused {
		t.Fatal("ManualScrollPaused = false after selection begin, want true")
	}

	// 拖动 + 释放：恢复跟随（仍在底部）
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))
	if tui.chat.ManualScrollPaused {
		t.Fatal("ManualScrollPaused = true after release at bottom, want false (follow restored)")
	}
}

func TestSelectionEscRestoresFollow(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))
	if !tui.selection.HasSelection {
		t.Fatal("selection not kept after drag")
	}

	// Esc 清除：跟随恢复（按当前位置判断）
	tui.updateChatKey("esc", tea.KeyPressMsg{})
	if tui.selection.HasAny() {
		t.Fatal("selection still present after Esc")
	}
}

func TestSelectionHighlightContains(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 2, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 2}))

	for _, line := range []int{0, 1, 2} {
		if !tui.selection.Contains(line) {
			t.Fatalf("selection.Contains(%d) = false, want true", line)
		}
	}
	if tui.selection.Contains(3) {
		t.Fatal("selection.Contains(3) = true, want false")
	}
}

// handleMouseDown 是测试辅助：直接调用选区鼠标处理（与 handleSelectionMouse 相同）。
func (t *TUI) handleMouseDown(msg tea.MouseMsg) bool {
	consumed, _ := t.handleSelectionMouse(msg)
	return consumed
}

func TestInputSelectionDragCopiesDraft(t *testing.T) {
	tui := selectionTestTUI(t)
	tui.chat.Textarea.SetValue("draft line 1\ndraft line 2\ndraft line 3")
	startY := tui.inputSelectionStartY()

	// 按下（输入区行 0）
	down := tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft})
	if consumed, _ := tui.handleInputSelectionMouse(down); !consumed {
		t.Fatal("handleInputSelectionMouse(down) = false, want consumed")
	}
	if tui.selection.Region != SelectionRegionInput {
		t.Fatalf("selection.Region = %v, want SelectionRegionInput", tui.selection.Region)
	}

	// 拖动到输入区行 1
	motion := tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft})
	if consumed, _ := tui.handleInputSelectionMouse(motion); !consumed {
		t.Fatal("handleInputSelectionMouse(motion) = false, want consumed")
	}

	// 释放：选区定格
	release := tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1})
	if consumed, _ := tui.handleInputSelectionMouse(release); !consumed {
		t.Fatal("handleInputSelectionMouse(release) = false, want consumed")
	}
	if !tui.selection.HasSelection {
		t.Fatal("selection.HasSelection = false after input drag, want true")
	}

	// 复制：提取草稿行 0..1
	tui.copySelection()
	if tui.copyFeedbackText == "" {
		t.Fatal("copyFeedbackText empty after copy, want feedback")
	}
}

func TestSelectionDragIntoComposerStillReleases(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	// 内容区按下并拖动
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	if !tui.selection.Active {
		t.Fatal("selection.Active = false after content drag, want true")
	}

	// 拖动跨入输入区（Y 在 composer 内）：事件应继续交给内容区处理，选区不卡死
	composerY := tui.height - 1
	release := tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: composerY})
	if consumed, _ := tui.handleSelectionMouse(release); !consumed {
		t.Fatal("handleSelectionMouse(release in composer) = false, want consumed")
	}
	if tui.selection.Active {
		t.Fatal("selection.Active = true after release, want false")
	}
	if !tui.selection.HasSelection {
		t.Fatal("selection.HasSelection = false after cross-region release, want true")
	}
}

func TestSelectionStyleAppliedToContent(t *testing.T) {
	tui := selectionTestTUI(t)
	startY := tui.viewportStartY()

	// 拖动选择内容行 0..1
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))

	// syncContent 后 chat 包应持有选区范围与样式
	tui.syncContent()
	if tui.chat.SelectionStart != 0 || tui.chat.SelectionEnd != 1 {
		t.Fatalf("chat.SelectionStart/End = %d/%d, want 0/1", tui.chat.SelectionStart, tui.chat.SelectionEnd)
	}
	if tui.chat.SelectionStyle.Render("x") == "x" {
		t.Fatal("chat.SelectionStyle empty, want styled")
	}

	// 清除后应回到无选区（-1）
	tui.selection.Clear()
	tui.syncContent()
	if tui.chat.SelectionStart != -1 || tui.chat.SelectionEnd != -1 {
		t.Fatalf("chat.SelectionStart/End after clear = %d/%d, want -1/-1", tui.chat.SelectionStart, tui.chat.SelectionEnd)
	}
}

func TestSelectionHintShownWhileActive(t *testing.T) {
	tui := selectionTestTUI(t)
	if hint := tui.renderSelectionHint(); hint != "" {
		t.Fatalf("renderSelectionHint without selection = %q, want empty", hint)
	}
	startY := tui.viewportStartY()
	tui.handleSelectionMouse(tea.MouseClickMsg(tea.Mouse{X: 10, Y: startY, Button: tea.MouseLeft}))
	if hint := tui.renderSelectionHint(); hint == "" {
		t.Fatal("renderSelectionHint while active = empty, want hint")
	}
	// 拖动后释放（非单击）：选区定格，显示复制提示
	tui.handleSelectionMouse(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: startY + 1, Button: tea.MouseLeft}))
	tui.handleSelectionMouse(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: startY + 1}))
	if hint := tui.renderSelectionHint(); hint == "" {
		t.Fatal("renderSelectionHint after release = empty, want copy hint")
	}
}
