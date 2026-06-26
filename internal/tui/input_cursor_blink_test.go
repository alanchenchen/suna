package tui

import (
	"testing"

	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

// startInputCursorBlink 应保证全局只存在一条 tick 链：首次启动返回非空 cmd，重复启动返回 nil。
func TestStartInputCursorBlinkIsSingleton(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat}

	if cmd := tui.startInputCursorBlink(); cmd == nil {
		t.Fatal("startInputCursorBlink() first call cmd = nil, want non-nil")
	}
	if !tui.inputCursorBlinking {
		t.Fatal("inputCursorBlinking = false, want true after start")
	}
	if cmd := tui.startInputCursorBlink(); cmd != nil {
		t.Fatal("startInputCursorBlink() second call cmd != nil, want nil to avoid duplicate chains")
	}
}

// 闪烁 tick 链在非 chat 页面也必须续接，否则离开 chat 后链会永久死亡。
func TestInputCursorBlinkKeepsTickingOutsideChat(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), ready: true, mode: uipage.Chat}
	tui.startInputCursorBlink()

	tui.mode = uipage.Config
	if cmd := tui.updateInputCursorBlink(); cmd == nil {
		t.Fatal("updateInputCursorBlink() cmd = nil outside chat, want non-nil to keep chain alive")
	}
	// 非 chat 页面光标保持常亮，不参与翻转。
	if !tui.inputCursorVisible {
		t.Fatal("inputCursorVisible = false outside chat, want true")
	}
}

// chat 页面下 tick 应翻转可见性，形成闪烁效果。
func TestInputCursorBlinkTogglesInChat(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), ready: true, mode: uipage.Chat}
	tui.startInputCursorBlink()

	before := tui.inputCursorVisible
	tui.updateInputCursorBlink()
	if tui.inputCursorVisible == before {
		t.Fatalf("inputCursorVisible did not toggle in chat: got %v, want %v", tui.inputCursorVisible, !before)
	}
}
