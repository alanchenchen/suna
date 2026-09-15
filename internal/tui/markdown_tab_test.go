package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

// 代码块会保留源码缩进 tab，而 lipgloss 的宽度计算把 \t 当作 1 列，
// 终端却会跳到下一个 tab stop（8 列）。若不先展开 tab，按 1 列算出的
// 换行宽度小于终端实际渲染宽度，内容横向溢出：思考链表现为右边框断裂。
func TestThinkingBoxBorderStaysAlignedWithTabs(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 140, height: 50}
	tui.initChatComponents()

	content := "```go\nfunc f() {\n\tif x {\n\t\treturn\n\t}\n}\n```\n"
	started := time.Now().Add(-time.Second)
	ended := time.Now()

	for _, detail := range []bool{false, true} {
		out := tui.renderThinkingBoxMode(content, false, detail, started, ended)
		lines := strings.Split(out, "\n")
		want := 0
		for _, line := range lines {
			if w := lipgloss.Width(line); w > 0 {
				want = w
				break
			}
		}
		if want == 0 {
			t.Fatalf("detail=%v: rendered nothing", detail)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w != 0 && w != want {
				t.Fatalf("detail=%v line %d width = %d, want %d (border would break)", detail, i, w, want)
			}
		}
		if strings.Contains(stripANSIForTest(out), "\t") {
			t.Fatalf("detail=%v: output still contains raw tab", detail)
		}
	}
}

func TestMarkdownExpandsTabsBeforeWrapping(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 140, height: 50}
	tui.initChatComponents()

	width := max(24, tui.width-8) - 4
	out := RenderMarkdown("```go\nfunc f() {\n\treturn\n}\n```\n", width)
	if strings.Contains(stripANSIForTest(out), "\t") {
		t.Fatalf("RenderMarkdown() still contains raw tab: %q", stripANSIForTest(out))
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d width = %d, want at most %d", i, w, width)
		}
	}
}
