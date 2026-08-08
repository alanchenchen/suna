package toolview

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestInvalidSubtaskParentRendersAsMainEntry(t *testing.T) {
	block := &Block{}
	block.Add(&Entry{ID: "spawn-1", Name: "Search", RawName: "search", Intent: "主工具", Status: StatusDone})
	block.Add(&Entry{ID: "spawn:spawn-1:read-1", ParentID: "spawn-1", Name: "Readfile", RawName: "readfile", Intent: "不应归入子任务", Status: StatusDone})

	if children := SubtaskChildren(block, "spawn-1"); len(children) != 0 {
		t.Fatalf("SubtaskChildren() = %d, want 0 for non-spawn parent", len(children))
	}
	entries := VisibleMainEntries(block)
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(VisibleMainEntries) = %d, want %d", got, want)
	}
}

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

func TestRenderExecBackgroundSummaryKeepsToolDone(t *testing.T) {
	entry := &Entry{
		ID:      "exec-1",
		Name:    "Exec",
		RawName: "exec",
		Intent:  "启动后台服务",
		Status:  StatusDone,
		Metadata: map[string]any{
			"kind":            "exec",
			"action":          "background",
			"exec_status":     "running",
			"job_id":          "job-42",
			"scope":           "run",
			"timeout_seconds": 30,
		},
	}
	deps := RenderDeps{Labels: RenderLabels{
		ExecBadge:      "执行",
		ExecRunning:    "后台运行中",
		ExecJobID:      "任务",
		ExecScope:      "范围",
		ExecTimeout:    "超时",
		ExecRunCleanup: "本轮结束时自动清理",
	}}

	rendered := RenderEntry(entry, false, deps)
	for _, want := range []string{"✓", "后台运行中", "任务 job-42", "范围 run", "超时 30s", "本轮结束时自动清理"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderEntry() missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderExecTerminalSemanticSummaries(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "timeout", want: "已超时"},
		{status: "cancelled", want: "已取消"},
		{status: "stopped", want: "已停止"},
		{status: "exited", want: "已退出"},
		{status: "cleanup", want: "已清理"},
	}
	labels := RenderLabels{ExecBadge: "执行", ExecTimedOut: "已超时", ExecCancelled: "已取消", ExecExited: "已退出", ExecStopped: "已停止", ExecCleanup: "已清理"}
	for _, tt := range tests {
		rendered := RenderEntry(&Entry{ID: tt.status, Name: "Exec", Status: StatusDone, Metadata: map[string]any{"kind": "exec", "exec_status": tt.status}}, false, RenderDeps{Labels: labels})
		if !strings.Contains(rendered, tt.want) {
			t.Fatalf("status %q missing %q: %s", tt.status, tt.want, rendered)
		}
	}
}

func TestRenderExecSessionCleanupBoundary(t *testing.T) {
	entry := &Entry{Name: "Exec", Status: StatusDone, Metadata: map[string]any{"kind": "exec", "action": "background", "exec_status": "running", "scope": "session"}}
	labels := RenderLabels{ExecRunning: "后台运行中", ExecRunCleanup: "本轮结束时自动清理", ExecSessionCleanup: "会话关闭时清理"}
	rendered := RenderEntry(entry, false, RenderDeps{Labels: labels})
	if !strings.Contains(rendered, "会话关闭时清理") || strings.Contains(rendered, "本轮结束时自动清理") {
		t.Fatalf("session cleanup boundary = %q", rendered)
	}
}

func TestRenderCancellingAndCancelledToolStatuses(t *testing.T) {
	deps := RenderDeps{Spinner: "⣾", Labels: RenderLabels{Cancelling: "正在取消", Cancelled: "已取消"}}
	cancelling := RenderEntry(&Entry{Name: "Exec", Status: StatusCancelling}, false, deps)
	if !strings.Contains(cancelling, "⣾") || !strings.Contains(cancelling, "正在取消") {
		t.Fatalf("cancelling render = %q", cancelling)
	}
	cancelled := RenderEntry(&Entry{Name: "Exec", Status: StatusCancelled}, false, deps)
	if strings.Contains(cancelled, "⣾") || !strings.Contains(cancelled, "⊘") || !strings.Contains(cancelled, "已取消") {
		t.Fatalf("cancelled render = %q", cancelled)
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
