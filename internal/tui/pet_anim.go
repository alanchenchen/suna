package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// petTickMsg 驱动宠物动画帧推进；帧间隔由当前 pet 状态决定。
type petTickMsg struct{}

// startPetTick 启动或维持唯一的宠物动画 tick 链。
// 仿照 chatSpinnerTicking 的防重入模式：已存在 tick 链时直接返回 nil，
// 避免多次启动累积出多条链相互打架。
func (t *TUI) startPetTick() tea.Cmd {
	if t.petTicking {
		return nil
	}
	t.petTicking = true
	return tea.Tick(t.petTickInterval(), func(time.Time) tea.Msg { return petTickMsg{} })
}

// petTickInterval 返回当前 pet 状态对应的帧间隔：
// idle/happy 眨眼慢速，working/thinking 快速轮换。
func (t *TUI) petTickInterval() time.Duration {
	switch t.chatPetState() {
	case petWorking:
		return petWorkingFrameInterval
	case petThinking:
		return petThinkFrameInterval
	default:
		return petIdleBlinkInterval
	}
}

// updatePetTick 推进宠物动画帧并延续 tick 链。
// 与 inputCursorBlink 同理：链永不断，避免离开页面后回到 Chat/Welcome
// 时没有重启点导致宠物定格；帧只在对应页面渲染时被消费。
func (t *TUI) updatePetTick() tea.Cmd {
	t.petFrame++
	return tea.Tick(t.petTickInterval(), func(time.Time) tea.Msg { return petTickMsg{} })
}
