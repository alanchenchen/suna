package combobox

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func newTestModel(t *testing.T, items []string) Model {
	t.Helper()
	m := New("filter")
	m.Focus() // textinput 仅在聚焦时处理按键，真实链路由页面调用 Focus
	m.SetItems(items)
	return m
}

// styleNoop 返回无装饰样式，测试只关心文本内容不关心颜色。
func styleNoop() lipgloss.Style { return lipgloss.NewStyle() }

func pressRunes(t *testing.T, m *Model, runes []rune) {
	t.Helper()
	for _, r := range runes {
		m.UpdateInput(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// 输入即过滤：输入值同步收窄候选，光标回到首行。
func TestRefilterSyncsImmediately(t *testing.T) {
	m := newTestModel(t, []string{"gpt-5.2", "gpt-5.6-sol", "claude-opus"})
	pressRunes(t, &m, []rune("gp"))
	if got, want := m.Count(), 2; got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got, want := m.InputValue(), "gp"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 到底后继续向下环绕回首行，到顶后继续向上回到底部（无缝循环）。
func TestMoveCursorWrapsAround(t *testing.T) {
	m := newTestModel(t, []string{"a", "b", "c"})

	m.MoveCursor(1)
	m.MoveCursor(1) // 到底部 "c"
	if name, ok := m.Selected(); !ok || name != "c" {
		t.Fatalf("got %q,%v, want \"c\",true", name, ok)
	}
	m.MoveCursor(1) // 环绕回顶部
	if name, ok := m.Selected(); !ok || name != "a" {
		t.Fatalf("got %q,%v, want \"a\",true", name, ok)
	}
	m.MoveCursor(-1) // 从顶部向上环绕回底部
	if name, ok := m.Selected(); !ok || name != "c" {
		t.Fatalf("got %q,%v, want \"c\",true", name, ok)
	}
}

// 环绕后内部滚动窗口跟随光标，可见行始终包含光标行。
func TestFollowCursorAfterWrap(t *testing.T) {
	items := []string{"i0", "i1", "i2", "i3", "i4", "i5", "i6", "i7", "i8", "i9"}
	m := newTestModel(t, items)
	m.SetSize(40, 4)

	m.MoveCursor(-1) // 顶部向上环绕到 "i9"
	if name, ok := m.Selected(); !ok || name != "i9" {
		t.Fatalf("got %q,%v, want \"i9\",true", name, ok)
	}
	view := m.View(Styles{Cursor: styleNoop(), Value: styleNoop(), Dim: styleNoop()})
	if !strings.Contains(view, "i9") || strings.Contains(view, "i0") {
		t.Fatalf("viewport should show i9 without i0, got:\n%s", view)
	}
}

// 输入值未变化时（如光标闪烁消息）不重算过滤，光标保持不动。
func TestBlinkDoesNotResetCursor(t *testing.T) {
	m := newTestModel(t, []string{"a", "b", "c"})
	m.MoveCursor(1)
	m.UpdateInput(cursor.BlinkMsg{})
	if name, ok := m.Selected(); !ok || name != "b" {
		t.Fatalf("got %q,%v, want \"b\",true", name, ok)
	}
}
