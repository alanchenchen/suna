package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	coreconfig "github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	tuievents "github.com/alanchenchen/suna/internal/tui/events"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

func TestConfigModelsResultFillsPickerAndCache(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a", Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model: "model", BaseURL: "https://api.example.com/v1",
	})
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	tui.modelPickerLoading = true

	tui.handleConfigModelsResultNotification(protocol.ConfigModelsResultParams{
		Provider: "provider-a",
		Models:   []string{"gpt-5.2", "gpt-5.6-sol"},
	})
	if tui.modelPickerLoading {
		t.Fatal("modelPickerLoading = true after result, want false")
	}
	if got := tui.modelCombobox.Count(); got != 2 {
		t.Fatalf("combobox count = %d, want 2", got)
	}
	if got := tui.modelsCache["provider-a"]; len(got) != 2 {
		t.Fatalf("cache = %v, want 2 models", got)
	}
}

// 浮层等待拉取结果时收到请求级错误（未连接、超时等）：必须在浮层内展示并清除
// 加载态，否则加载指示永远卡住；错误也不能写到浮层背后的表单上。
func TestConfigModelsRequestErrorClearsPickerLoading(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a", Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model: "model", BaseURL: "https://api.example.com/v1",
	})
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	tui.modelPickerLoading = true

	tui.handleRequestErrorNotification(tuievents.RequestErrorMsg{Scope: tuievents.NotifyConfigError, Message: "not connected"})
	if tui.modelPickerLoading {
		t.Fatal("modelPickerLoading = true after request error, want false")
	}
	if got := tui.modelPickerError; got != "not connected" {
		t.Fatalf("modelPickerError = %q, want %q", got, "not connected")
	}
	if tui.config.Error != "" {
		t.Fatalf("config.Error = %q, want empty (error belongs to overlay)", tui.config.Error)
	}
}

func TestConfigModelsResultErrorShowsInPicker(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a", Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model: "model", BaseURL: "https://api.example.com/v1",
	})
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})

	tui.handleConfigModelsResultNotification(protocol.ConfigModelsResultParams{
		Provider:     "provider-a",
		ErrorMessage: "unauthorized",
	})
	if tui.modelPickerError == "" {
		t.Fatal("modelPickerError = empty, want error message")
	}
	if _, ok := tui.modelsCache["provider-a"]; ok {
		t.Fatal("cache written on error, want not cached")
	}
}

func TestConfigModelsResultWritesCacheWhenPickerClosed(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a", Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model: "model", BaseURL: "https://api.example.com/v1",
	})
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	// esc 直接关闭浮层（同步选择器无过滤态）。
	tui.updateProviderModelPicker("esc", tea.KeyPressMsg{Code: tea.KeyEsc})

	// 浮层已关闭，通知仍应写入缓存（下次打开直接命中），但不刷新已关闭的浮层。
	tui.handleConfigModelsResultNotification(protocol.ConfigModelsResultParams{
		Provider: "provider-a",
		Models:   []string{"gpt-5.2"},
	})
	if got := tui.modelsCache["provider-a"]; len(got) != 1 {
		t.Fatalf("cache = %v, want 1 model after picker closed", got)
	}
	if tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = true after closed picker result, want false")
	}
}
