package toolview

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestRenderBlockUsesTitledContainerWithoutChangingEntryContent(t *testing.T) {
	block := &Block{}
	block.Add(&Entry{ID: "tool-1", Name: "Search", RawName: "search", Intent: "查找文件", Summary: "内容 \"prompt\" in .", Status: StatusDone})
	block.Add(&Entry{ID: "tool-2", Name: "Readfile", RawName: "readfile", Intent: "读取文件", Summary: "internal/tui/chat_view.go", Status: StatusRunning})

	rendered := RenderBlock(block, RenderDeps{
		Width:   72,
		Spinner: "⣾",
		Labels:  RenderLabels{Tools: "工具", Actions: "个操作"},
		Styles:  RenderStyles{},
	})

	if !strings.Contains(rendered, "╭─ ⣾ 工具 · 2个操作") {
		t.Fatalf("missing titled tool container, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "查找文件") || !strings.Contains(rendered, "读取文件") {
		t.Fatalf("tool entry content changed unexpectedly, got:\n%s", rendered)
	}
	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("got %d lines, want titled box with content", len(lines))
	}
	width := lipgloss.Width(strings.TrimPrefix(lines[0], "    "))
	for _, line := range lines[1:] {
		got := lipgloss.Width(strings.TrimPrefix(line, "    "))
		if got != width {
			t.Fatalf("box line width mismatch: got %d want %d line=%q\n%s", got, width, line, rendered)
		}
	}
}

func TestRenderBlockUsesSpinnerForRunningEntry(t *testing.T) {
	entry := &Entry{ID: "tool-1", Name: "Search", RawName: "search", Intent: "查找文件", Status: StatusRunning}

	rendered := RenderEntry(entry, false, RenderDeps{Spinner: "⣾", Styles: RenderStyles{}})
	if !strings.Contains(rendered, "⣾ 查找文件") {
		t.Fatalf("RenderEntry() = %q, want injected running spinner", rendered)
	}
}

func TestRenderBlockTitleShowsFailureStatus(t *testing.T) {
	block := &Block{}
	block.Add(&Entry{ID: "tool-1", Name: "Search", RawName: "search", Intent: "查找文件", Status: StatusError, Result: "boom"})

	rendered := RenderBlock(block, RenderDeps{
		Width:  72,
		Labels: RenderLabels{Tools: "工具", Actions: "个操作"},
		Styles: RenderStyles{},
	})

	if !strings.Contains(rendered, "╭─ ✗ 工具 · 1个操作") {
		t.Fatalf("missing failed tool status in title, got:\n%s", rendered)
	}
}

func TestRenderBlockWidthFollowsContentUpToMax(t *testing.T) {
	block := &Block{}
	block.Add(&Entry{ID: "tool-1", Name: "Search", RawName: "search", Intent: "短任务", Status: StatusDone})

	rendered := RenderBlock(block, RenderDeps{
		Width:  120,
		Labels: RenderLabels{Tools: "工具", Actions: "个操作"},
		Styles: RenderStyles{},
	})

	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("got empty render")
	}
	width := lipgloss.Width(strings.TrimPrefix(lines[0], "    "))
	if width >= 116 {
		t.Fatalf("got full-width tool box, want content-sized width, width=%d\n%s", width, rendered)
	}
	for _, line := range lines[1:] {
		got := lipgloss.Width(strings.TrimPrefix(line, "    "))
		if got != width {
			t.Fatalf("box line width mismatch: got %d want %d line=%q\n%s", got, width, line, rendered)
		}
	}
}

func TestRenderBlockKeepsDurationVisibleForLongCommand(t *testing.T) {
	block := &Block{}
	block.Add(&Entry{
		ID:       "tool-1",
		Name:     "Exec",
		RawName:  "exec",
		Intent:   "格式化并运行 TUI 状态线相关测试",
		Status:   StatusDone,
		Duration: mustDuration(t, "5.1s"),
		ParamsRaw: map[string]any{
			"command": "gofmt -w internal/tui/chat.go internal/tui/chat_render.go internal/tui/i18n_keys.go internal/tui/pages/chat/transcript.go internal/tui/pages/chat/input_view.go && go test ./internal/tui ./internal/tui/pages/chat",
		},
	})

	rendered := RenderBlock(block, RenderDeps{
		Width:  96,
		Labels: RenderLabels{Tools: "工具", Actions: "个操作"},
		Styles: RenderStyles{},
	})

	if !strings.Contains(rendered, "5.1s") {
		t.Fatalf("duration should remain visible for long command, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "\n│ scrip") || strings.Contains(rendered, "\n│ scri") {
		t.Fatalf("long command should be truncated on the header line instead of wrapping awkwardly, got:\n%s", rendered)
	}
}

func mustDuration(t *testing.T, value string) time.Duration {
	t.Helper()
	d, err := time.ParseDuration(value)
	if err != nil {
		t.Fatalf("parse duration: %v", err)
	}
	return d
}
