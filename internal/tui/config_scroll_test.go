package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	coreconfig "github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

// protocolConfigStateWithModels 构造含 n 个模型的配置状态，供滚动/渲染测试使用。
func protocolConfigStateWithModels(n int) protocol.ConfigParams {
	models := make([]protocol.ConfigModel, 0, n)
	for i := 1; i <= n; i++ {
		models = append(models, protocol.ConfigModel{
			Provider: "provider-a", Model: "model-" + strconv.Itoa(i),
			Protocol: "openai_chat", BaseURL: "https://api.example.com/v1",
			ContextWindow: 128000, MaxOutputTokens: 8192, HasAPIKey: true,
		})
	}
	return protocol.ConfigParams{ActiveModel: "provider-a/model-1", Models: models}
}

// TestConfigViewRendersPickerOverForm 验证模型选择浮层打开时，viewConfig 在表单之上渲染浮层，
// 而不是只显示表单（浮层"隐形"的回归保护）。
func TestConfigViewRendersPickerOverForm(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 40}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a", Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model: "model", BaseURL: "https://api.example.com/v1",
	})
	tui.modelsCache = map[string][]string{"provider-a": {"gpt-5.2"}}
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = false after model field enter")
	}

	view := tui.viewConfig()
	plain := stripANSIForTest(view)
	if !strings.Contains(plain, "Filter:") {
		t.Fatalf("view = %q, want picker filter bar rendered over form", plain)
	}
}

// TestConfigKeysForwardToPickerWhileOpen 验证浮层打开时按键转发给浮层处理，
// 而不是走表单导航（否则 enter 打开浮层后无法在浮层内选择/输入）。
func TestConfigKeysForwardToPickerWhileOpen(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 40}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a", Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model: "model", BaseURL: "https://api.example.com/v1",
	})
	tui.modelsCache = map[string][]string{"provider-a": {"gpt-5.2", "gpt-5.6-sol"}}
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})

	// 浮层打开后按 down：应移动选择器光标，而不是表单 InputFocus 变化。
	tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := tui.config.InputFocus; got != tuiconfig.ProviderFormModelIndex {
		t.Fatalf("InputFocus = %d, want model field unchanged while picker open", got)
	}
	if !tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = false after down, want still open")
	}
	if name, ok := tui.modelCombobox.Selected(); !ok || name != "gpt-5.6-sol" {
		t.Fatalf("selected = %q,%v, want second candidate after down", name, ok)
	}
}

// TestConfigViewScrollsWhenRowsExceedHeight 验证配置页行数超出终端高度时，
// 渲染截取可见行并显示滚动提示，而不是整页溢出。
func TestConfigViewScrollsWhenRowsExceedHeight(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 12}
	tui.config = tuiconfig.Model{Page: "models"}
	tui.configState = protocolConfigStateWithModels(8)
	tui.config.Cursor = 0

	view := tui.viewConfigPage()
	plain := stripANSIForTest(view)
	if !strings.Contains(plain, "more below") {
		t.Fatalf("view = %q, want scroll hint when rows exceed height", plain)
	}
	// 渲染行数不能超过可用高度（header 两行 + 提示一行）。
	if got := strings.Count(plain, "\n") + 1; got > 12 {
		t.Fatalf("rendered lines = %d, want <= 12", got)
	}
}

// TestConfigScrollFollowsCursor 验证 cursor 移到不可见行时滚动自动跟随，
// 用户按 down 到列表末尾不会停留在空白区。
func TestConfigScrollFollowsCursor(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 12}
	tui.config = tuiconfig.Model{Page: "models"}
	tui.configState = protocolConfigStateWithModels(8)
	tui.config.Cursor = 0

	// 连续下移 cursor 到接近末尾（MoveCursor 在末尾会循环回开头，
	// 因此只下移 10 次：cursor 从 1 到 11，仍在末尾附近）。
	for i := 0; i < 10; i++ {
		tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyDown})
		tui.viewConfigPage()
	}
	if tui.config.Scroll == 0 {
		t.Fatal("Scroll = 0 after moving cursor past viewport, want scrolled")
	}
	// 渲染时 cursor 行必须可见。
	view := tui.viewConfigPage()
	plain := stripANSIForTest(view)
	if !strings.Contains(plain, "model-"+strconv.Itoa(tui.config.Cursor)) {
		t.Fatalf("view = %q, want cursor row visible after follow", plain)
	}
}

// TestConfigMouseWheelIgnored 验证配置页忽略滚轮：滚动完全由 cursor 跟随逻辑
// 负责，避免手动 Scroll 与跟随逻辑打架造成闪烁。
func TestConfigMouseWheelIgnored(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 12}
	tui.config = tuiconfig.Model{Page: "models"}
	tui.configState = protocolConfigStateWithModels(8)

	tui.updateConfig(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if tui.config.Scroll != 0 {
		t.Fatalf("Scroll = %d after wheel down, want 0 (wheel ignored)", tui.config.Scroll)
	}
}

// TestConfigScrollResetsAfterFormSaved 验证表单保存后回到列表页时 Scroll 重置：
// 残留的 Scroll 会把首个 provider 的名字和边框裁掉。
func TestConfigScrollResetsAfterFormSaved(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 12}
	tui.config = tuiconfig.Model{Page: "models"}
	tui.configState = protocolConfigStateWithModels(8)
	// 模拟在列表底部打开表单前滚动过页面。
	tui.config.Scroll = 5
	tui.config.FormOpen = true
	tui.config.EditingName = "provider-a/model-1"

	tui.afterConfigFormSaved()
	if tui.config.Scroll != 0 {
		t.Fatalf("Scroll = %d after form saved, want 0", tui.config.Scroll)
	}
}

// TestConfigScrollKeepsProviderHeaderVisible 验证 cursor 在 provider 的模型间
// 移动时，该 provider 的分组头保持可见：头行被滚出视口会让用户失去
// "当前在哪个 provider"的上下文（首个 provider 名字和边框被裁的回归保护）。
func TestConfigScrollKeepsProviderHeaderVisible(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 30}
	tui.config = tuiconfig.Model{Page: "models"}
	tui.configState = protocolConfigStateWithModels(8)

	// 下移到本 provider 中间某个模型行后渲染。
	for i := 0; i < 4; i++ {
		tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	tui.viewConfigPage()

	view := tui.viewConfigPage()
	plain := stripANSIForTest(view)
	if !strings.Contains(plain, "provider-a") {
		t.Fatalf("view = %q, want provider header visible while cursor inside its group", plain)
	}
}
