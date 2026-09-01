package tui

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"

	coreconfig "github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

// newPickerFormTUI 构造一个已打开 provider 表单并缓存模型列表的 TUI，
// 焦点停在 model 字段并按 enter 打开选择浮层。
func newPickerFormTUI(t *testing.T, models []string) *TUI {
	t.Helper()
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a",
		Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model:    "model",
		BaseURL:  "https://api.example.com/v1",
	})
	tui.modelsCache = map[string][]string{"provider-a": models}
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	return tui
}

func TestProviderFormModelEnterOpensPickerWithCache(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol"})
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex

	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = false, want true after model field enter")
	}
	if tui.modelPickerLoading {
		t.Fatal("modelPickerLoading = true with cache hit, want false")
	}
	if got := tui.modelCombobox.Count(); got != 2 {
		t.Fatalf("combobox count = %d, want 2 from cache", got)
	}
}

// TestProviderFormModelFieldIsPlainValueRow 验证 model 字段渲染为纯值行：
// 不用 ‹ › 箭头（那是循环切换语义）；未设置时显示提示，有值时显示模型名。
func TestProviderFormModelFieldIsPlainValueRow(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("", nil)

	view := stripANSIForTest(tui.viewProviderForm())
	if !strings.Contains(view, "not set · Enter to pick or type") {
		t.Fatalf("view = %q, want unset model hint", view)
	}
	// 已有值时显示当前模型名，且不出现箭头装饰。
	tui.config.Inputs[tuiconfig.ProviderFormModelIndex].SetValue("glm-5.1")
	view = stripANSIForTest(tui.viewProviderForm())
	if !strings.Contains(view, "Model: glm-5.1") {
		t.Fatalf("view = %q, want current model as plain value", view)
	}
	if strings.Contains(view, "‹ glm-5.1 ›") {
		t.Fatal("model row still renders arrows, want plain value")
	}
}

func TestProviderFormModelEnterRequiresProvider(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("", nil)
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex

	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	if tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = true without provider, want false")
	}
	if tui.config.Error == "" {
		t.Fatal("config error = empty, want provider required message")
	}
}

func TestProviderPickerEnterFillsModelField(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol"})
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	// 光标移到第二项后 enter 确认。
	tui.updateProviderModelPicker("down", tea.KeyPressMsg{Code: tea.KeyDown})

	tui.updateProviderModelPicker("enter", tea.KeyPressMsg{Code: tea.KeyEnter})
	if tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = true after enter, want closed")
	}
	if got := tui.config.Inputs[tuiconfig.ProviderFormModelIndex].Value(); got != "gpt-5.6-sol" {
		t.Fatalf("model field = %q, want gpt-5.6-sol", got)
	}
}

// TestProviderPickerEnterPrefersFilterInput 验证输入筛不出任何候选时
// Enter 用输入值作为自定义模型名。
func TestProviderPickerEnterPrefersFilterInput(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol"})
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'c', Text: "c"})
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'u', Text: "u"})
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 's', Text: "s"})
	if tui.modelCombobox.Count() != 0 {
		t.Fatalf("count after typing 'cus' = %d, want 0 (no candidate matches)", tui.modelCombobox.Count())
	}

	tui.updateProviderModelPicker("enter", tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := tui.config.Inputs[tuiconfig.ProviderFormModelIndex].Value(); got != "cus" {
		t.Fatalf("model field = %q, want cus from typed input", got)
	}
	if tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = true after enter, want closed")
	}
}

// TestProviderPickerEnterSelectsHighlightedAfterFilter 验证筛选后 Enter
// 选高亮候选而不是筛选词：筛选是选择手段，不是输入目的。
func TestProviderPickerEnterSelectsHighlightedAfterFilter(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol", "claude-opus"})
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'g', Text: "g"})
	if tui.modelCombobox.Count() != 2 {
		t.Fatalf("count after typing 'g' = %d, want 2", tui.modelCombobox.Count())
	}
	tui.updateProviderModelPicker("down", tea.KeyPressMsg{Code: tea.KeyDown})

	tui.updateProviderModelPicker("enter", tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := tui.config.Inputs[tuiconfig.ProviderFormModelIndex].Value(); got != "gpt-5.6-sol" {
		t.Fatalf("model field = %q, want highlighted gpt-5.6-sol, not filter text", got)
	}
}

// TestComboboxBlinkDoesNotResetCursor 验证光标闪烁等周期性消息不会把
// TestComboboxBlinkDoesNotResetCursor 验证光标闪烁等周期性消息不会把
// 选择器光标拽回首行：只有输入值真正变化时才重算过滤。
func TestProviderPickerBlinkDoesNotResetCursor(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol", "claude-opus"})
	tui.updateProviderModelPicker("down", tea.KeyPressMsg{Code: tea.KeyDown})
	if name, _ := tui.modelCombobox.Selected(); name != "gpt-5.6-sol" {
		t.Fatalf("selected = %q, want gpt-5.6-sol after down", name)
	}

	// 模拟 textinput 光标闪烁 tick：非按键消息，输入值未变化。
	tui.updateProviderModelPicker("", cursor.BlinkMsg{})
	if name, _ := tui.modelCombobox.Selected(); name != "gpt-5.6-sol" {
		t.Fatalf("selected = %q after blink msg, want cursor preserved", name)
	}
}

// TestProviderPickerTypingFiltersSync 验证输入即过滤：无异步消息，
// 打字后候选立即收窄，光标回到首行。
func TestProviderPickerTypingFiltersSync(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol", "claude-opus"})
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'g', Text: "g"})
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'p', Text: "p"})

	if got := tui.modelCombobox.Count(); got != 2 {
		t.Fatalf("count after typing 'gp' = %d, want 2", got)
	}
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 't', Text: "t"})
	if got := tui.modelCombobox.Count(); got != 2 {
		t.Fatalf("count after 'gpt' = %d, want 2", got)
	}
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := tui.modelCombobox.Count(); got != 2 {
		t.Fatalf("count after backspace = %d, want 2", got)
	}
}

// TestProviderPickerEscClosesDirectly 验证 esc 直接关闭浮层并聚焦 model 字段：
// 同步选择器无过滤态，不再需要两段 esc。
func TestProviderPickerEscClosesDirectly(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol"})

	tui.updateProviderModelPicker("esc", tea.KeyPressMsg{Code: tea.KeyEsc})
	if tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = true after esc, want closed")
	}
	if got := tui.config.InputFocus; got != tuiconfig.ProviderFormModelIndex {
		t.Fatalf("InputFocus = %d, want model field", got)
	}
}

// TestProviderPickerEmptyListAcceptsCustomName 验证拉取失败（空列表）时
// 直接输入自定义名 + enter 仍可确认，不被空列表阻塞。
func TestProviderPickerEmptyListAcceptsCustomName(t *testing.T) {
	tui := newPickerFormTUI(t, nil)
	tui.modelPickerLoading = false
	tui.modelCombobox.SetItems(nil)
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'm', Text: "m"})
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'y', Text: "y"})

	tui.updateProviderModelPicker("enter", tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := tui.config.Inputs[tuiconfig.ProviderFormModelIndex].Value(); got != "my" {
		t.Fatalf("model field = %q, want my from typed input", got)
	}
}

// TestProviderPickerPageKeysNavigate 验证 pgup/pgdown 在选择器内翻动光标。
func TestProviderPickerPageKeysNavigate(t *testing.T) {
	models := make([]string, 0, 20)
	for i := 1; i <= 20; i++ {
		models = append(models, "gpt-"+strconv.Itoa(i))
	}
	tui := newPickerFormTUI(t, models)

	tui.updateProviderModelPicker("pgdown", tea.KeyPressMsg{Code: tea.KeyPgDown})
	if name, _ := tui.modelCombobox.Selected(); name != "gpt-9" {
		t.Fatalf("selected after pgdown = %q, want gpt-9 (8 rows per page)", name)
	}
	tui.updateProviderModelPicker("pgup", tea.KeyPressMsg{Code: tea.KeyPgUp})
	if name, _ := tui.modelCombobox.Selected(); name != "gpt-1" {
		t.Fatalf("selected after pgup = %q, want gpt-1", name)
	}
}

// TestProviderPickerWheelMovesCursor 验证浮层滚轮逐行移动光标。
func TestProviderPickerWheelMovesCursor(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "gpt-5.6-sol"})

	tui.updateProviderModelPicker("", tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if name, _ := tui.modelCombobox.Selected(); name != "gpt-5.6-sol" {
		t.Fatalf("selected after wheel down = %q, want gpt-5.6-sol", name)
	}
	tui.updateProviderModelPicker("", tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if name, _ := tui.modelCombobox.Selected(); name != "gpt-5.2" {
		t.Fatalf("selected after wheel up = %q, want gpt-5.2", name)
	}
}

// TestProviderPickerLoadingResultFillsItems 验证异步拉取结果到达时
// 候选被填充且保留用户已输入的筛选文本。
func TestProviderPickerLoadingResultFillsItems(t *testing.T) {
	tui := newPickerFormTUI(t, nil)
	// helper 传入 nil 缓存时浮层未打开（enter 需要 provider 缓存命中才不拉取），
	// 这里直接重开：无缓存 → 异步拉取 → loading。
	tui.modelPickerOpen = true
	tui.modelPickerLoading = true
	tui.modelPickerProvider = "provider-a"
	// 用户在拉取期间先输入筛选文本。
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'g', Text: "g"})

	tui.handleConfigModelsResultNotification(protocol.ConfigModelsResultParams{
		Provider: "provider-a",
		Models:   []string{"gpt-5.2", "claude-opus"},
	})
	if tui.modelPickerLoading {
		t.Fatal("modelPickerLoading = true after result, want false")
	}
	// 输入 "g" 已保留：2 个候选中只有 gpt-5.2 匹配。
	if got := tui.modelCombobox.Count(); got != 1 {
		t.Fatalf("count after result = %d, want 1 (filter g preserved)", got)
	}
	if name, ok := tui.modelCombobox.Selected(); !ok || name != "gpt-5.2" {
		t.Fatalf("selected = %q,%v, want gpt-5.2 kept filter input", name, ok)
	}
	if got := tui.modelCombobox.InputValue(); got != "g" {
		t.Fatalf("input value = %q, want g preserved", got)
	}
}

// TestProviderPickerErrorStillAllowsTyping 验证拉取失败时浮层仍可输入自定义名。
func TestProviderPickerErrorStillAllowsTyping(t *testing.T) {
	tui := newPickerFormTUI(t, nil)
	tui.modelPickerLoading = false
	tui.modelPickerError = "connect refused"
	tui.modelCombobox.SetItems(nil)

	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'x', Text: "x"})
	tui.updateProviderModelPicker("enter", tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := tui.config.Inputs[tuiconfig.ProviderFormModelIndex].Value(); got != "x" {
		t.Fatalf("model field = %q, want x from typed input despite error", got)
	}
}

// TestProviderPickerViewShowsFilterAndCandidates 验证浮层渲染：筛选行、候选、
// 空态提示与键位 footer。
func TestProviderPickerViewShowsFilterAndFooter(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"gpt-5.2", "claude-opus"})

	view := stripANSIForTest(tui.renderModelPickerOverlay(tui.width))
	for _, want := range []string{"Filter:", "gpt-5.2", "claude-opus", "Enter", "Esc"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
	// 输入后无匹配：显示空态提示（拉取失败同样走这里，仍可继续输入）。
	tui.updateProviderModelPicker("", tea.KeyPressMsg{Code: 'z', Text: "z"})
	view = stripANSIForTest(tui.renderModelPickerOverlay(tui.width))
	if !strings.Contains(view, "custom name") || !strings.Contains(view, "use typed name") {
		t.Fatalf("view = %q, want empty hint and use-typed-name footer", view)
	}
}

// TestProviderFormDynamicHintFollowsFocus 验证表单底部说明的动态部分跟随
// 焦点字段变化：model 字段提示 picker 能力，普通输入字段提示输入语义。
func TestProviderFormDynamicHintFollowsFocus(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("", nil)

	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	view := stripANSIForTest(tui.viewProviderForm())
	if !strings.Contains(view, "Enter picks from list") {
		t.Fatalf("view = %q, want model field hint", view)
	}
	tui.config.InputFocus = tuiconfig.ProviderFormEndpointIndex
	view = stripANSIForTest(tui.viewProviderForm())
	if !strings.Contains(view, "type to fill") {
		t.Fatalf("view = %q, want generic input hint", view)
	}
}

// TestProviderPickerWheelMovesCursorInOverlay 验证滚轮在选择器内逐行移动光标。
// 终端 resize 时浮层打开也不能丢失窗口尺寸更新，否则渲染宽度用旧值。
func TestProviderPickerOverlayHandlesWindowSize(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100, height: 40}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a", Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model: "model", BaseURL: "https://api.example.com/v1",
	})
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex
	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = false, want true")
	}

	tui.updateConfig(tea.WindowSizeMsg{Width: 80, Height: 30})
	if tui.width != 80 || tui.height != 30 {
		t.Fatalf("size = %dx%d, want 80x30", tui.width, tui.height)
	}
	if !tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = false after resize, want true (overlay stays)")
	}
}

func TestProviderPickerWheelMovesCursorInCombobox(t *testing.T) {
	tui := newPickerFormTUI(t, []string{"a", "b", "c"})

	tui.updateProviderModelPicker("", tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if name, _ := tui.modelCombobox.Selected(); name != "b" {
		t.Fatalf("selected after wheel down = %q, want b", name)
	}
	tui.updateProviderModelPicker("", tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if name, _ := tui.modelCombobox.Selected(); name != "a" {
		t.Fatalf("selected after wheel up = %q, want a", name)
	}
}

// TestProviderPickerDoesNotBlurChatInput 验证浮层打开/关闭不破坏 chat 输入框焦点：
// config 页的浮层链路不得触碰 chat Textarea，否则回到 chat 后无法输入。
func TestProviderPickerDoesNotBlurChatInput(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.initChatComponents()
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a",
		Protocol: coreconfig.ModelProtocolOpenAIChat,
		Model:    "model",
		BaseURL:  "https://api.example.com/v1",
	})
	tui.modelsCache = map[string][]string{"provider-a": {"gpt-5.2"}}
	tui.config.InputFocus = tuiconfig.ProviderFormModelIndex

	// 模拟回到 chat 前输入框是聚焦的（正常 chat → config 切换不改变焦点）。
	tui.chat.Textarea.Focus()

	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = false, want true")
	}
	if !tui.chat.Textarea.Focused() {
		t.Fatal("chat textarea blurred by opening model picker")
	}

	// esc 关闭浮层后焦点同样不被破坏。
	tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyEscape})
	if tui.modelPickerOpen {
		t.Fatal("modelPickerOpen = true, want false after esc")
	}
	if !tui.chat.Textarea.Focused() {
		t.Fatal("chat textarea blurred after closing model picker")
	}
}
