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

func TestFormatCompactDurationUsesReadableUnits(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "sub millisecond", duration: 500 * time.Microsecond, want: "<1ms"},
		{name: "milliseconds", duration: 428 * time.Millisecond, want: "428ms"},
		{name: "fractional seconds", duration: 3200 * time.Millisecond, want: "3.2s"},
		{name: "whole seconds", duration: 12 * time.Second, want: "12s"},
		{name: "minutes", duration: 72 * time.Second, want: "1m12s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatCompactDuration(tt.duration); got != tt.want {
				t.Fatalf("FormatCompactDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderOrdinaryToolUsesCompactDuration(t *testing.T) {
	entry := &Entry{Name: "Readfile", RawName: "readfile", Intent: "读取配置", Status: StatusDone, Duration: 428 * time.Millisecond}
	rendered := RenderEntry(entry, false, RenderDeps{Width: 96})
	if !strings.Contains(rendered, "428ms") || strings.Contains(rendered, "0.4s") {
		t.Fatalf("RenderEntry() duration = %q, want compact milliseconds", rendered)
	}
}

func TestRenderExecForegroundUsesTwoLineUserSummary(t *testing.T) {
	entry := &Entry{
		ID:       "exec-1",
		Name:     "Exec",
		RawName:  "exec",
		Status:   StatusDone,
		Duration: mustDuration(t, "5.1s"),
		ParamsRaw: map[string]any{
			"command": "go test ./internal/tui/...",
		},
		Metadata: map[string]any{
			"kind":        "exec",
			"action":      "run",
			"exec_status": "exited",
			"exit_code":   0,
			"duration_ms": 5100,
		},
	}

	rendered := RenderEntry(entry, false, RenderDeps{Width: 96, Labels: execTestLabels()})
	for _, want := range []string{"✓ 运行命令  go test ./internal/tui/...", "↳ 命令  已完成", "共运行 5.1s"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderEntry() missing %q:\n%s", want, rendered)
		}
	}
	for _, unwanted := range []string{"已退出", "退出码 0", "范围 run", "清理 complete"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("RenderEntry() contains technical noise %q:\n%s", unwanted, rendered)
		}
	}
	if got := len(strings.Split(strings.TrimSpace(rendered), "\n")); got != 2 {
		t.Fatalf("Exec default summary has %d lines, want 2:\n%s", got, rendered)
	}
}

func TestRenderExecLongCommandStaysOnOneHeaderLine(t *testing.T) {
	entry := &Entry{
		Name:    "Exec",
		RawName: "exec",
		Status:  StatusDone,
		ParamsRaw: map[string]any{
			"command": "gofmt -w internal/tui/chat.go internal/tui/chat_render.go internal/tui/i18n_keys.go internal/tui/pages/chat/transcript.go && go test ./internal/tui ./internal/tui/pages/chat",
		},
		Metadata: map[string]any{"kind": "exec", "action": "run", "exec_status": "exited", "exit_code": 0, "duration_ms": 5100},
	}

	rendered := RenderEntry(entry, false, RenderDeps{Width: 56, Labels: execTestLabels()})
	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	if len(lines) != 2 {
		t.Fatalf("long Exec summary has %d lines, want 2:\n%s", len(lines), rendered)
	}
	if !strings.Contains(lines[0], "运行命令") || !strings.Contains(lines[0], "…") || !strings.Contains(lines[1], "已完成") {
		t.Fatalf("long Exec summary did not preserve operation/outcome:\n%s", rendered)
	}
}

func TestRenderExecBackgroundActionsAreExplicit(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]any
		metadata map[string]any
		wantMain string
		wantSub  []string
		notWant  []string
	}{
		{
			name:     "start run scope",
			params:   map[string]any{"action": "run", "background": true, "command": "npm run dev"},
			metadata: map[string]any{"kind": "exec", "action": "run", "exec_status": "running", "job_id": "fc71654b-be2e-49c0-b9a8-5a2e5a6324d1", "scope": "run", "timeout_seconds": 30},
			wantMain: "启动后台任务  npm run dev · #fc71654b",
			wantSub:  []string{"运行中", "随本轮结束自动停止"},
			notWant:  []string{"范围 run", "超时 30s", "fc71654b-be2e"},
		},
		{
			name:     "check session scope",
			params:   map[string]any{"action": "status", "job_id": "fc71654b-be2e-49c0-b9a8-5a2e5a6324d1"},
			metadata: map[string]any{"kind": "exec", "action": "status", "exec_status": "running", "job_id": "fc71654b-be2e-49c0-b9a8-5a2e5a6324d1", "scope": "session", "duration_ms": 7100},
			wantMain: "查看后台任务  #fc71654b",
			wantSub:  []string{"运行中", "保持到会话关闭", "已运行 7.1s"},
			notWant:  []string{"Exec", "范围 session", "fc71654b-be2e"},
		},
		{
			name:     "stop",
			params:   map[string]any{"action": "stop", "job_id": "fc71654b-be2e-49c0-b9a8-5a2e5a6324d1"},
			metadata: map[string]any{"kind": "exec", "action": "stop", "exec_status": "stopped", "job_id": "fc71654b-be2e-49c0-b9a8-5a2e5a6324d1", "scope": "run", "duration_ms": 12000, "exit_code": -1, "cleanup_status": "complete"},
			wantMain: "停止后台任务  #fc71654b",
			wantSub:  []string{"命令", "已停止", "共运行 12s"},
			notWant:  []string{"退出码 -1", "清理 complete", "随本轮结束"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &Entry{Name: "Exec", RawName: "exec", Status: StatusDone, ParamsRaw: tt.params, Metadata: tt.metadata}
			rendered := RenderEntry(entry, false, RenderDeps{Width: 96, Labels: execTestLabels()})
			if !strings.Contains(rendered, tt.wantMain) {
				t.Fatalf("RenderEntry() missing main %q:\n%s", tt.wantMain, rendered)
			}
			if !strings.Contains(rendered, "命令") {
				t.Fatalf("RenderEntry() missing Exec badge:\n%s", rendered)
			}
			for _, want := range tt.wantSub {
				if !strings.Contains(rendered, want) {
					t.Fatalf("RenderEntry() missing %q:\n%s", want, rendered)
				}
			}
			for _, unwanted := range tt.notWant {
				if strings.Contains(rendered, unwanted) {
					t.Fatalf("RenderEntry() contains %q:\n%s", unwanted, rendered)
				}
			}
			if got := len(strings.Split(strings.TrimSpace(rendered), "\n")); got != 2 {
				t.Fatalf("Exec default summary has %d lines, want 2:\n%s", got, rendered)
			}
		})
	}
}

func TestRenderExecTerminalOutcomesUseUserSemantics(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		status  string
		code    int
		hasCode bool
		cleanup string
		want    string
		notWant string
	}{
		{name: "completed", action: "run", status: "exited", code: 0, hasCode: true, want: "已完成", notWant: "退出码 0"},
		{name: "failed", action: "run", status: "exited", code: 7, hasCode: true, want: "执行失败", notWant: "已退出"},
		{name: "timed out", action: "run", status: "timed_out", code: -1, hasCode: true, want: "已超时", notWant: "退出码 -1"},
		{name: "cancelled", action: "run", status: "cancelled", code: -1, hasCode: true, want: "已取消", notWant: "退出码 -1"},
		{name: "stopped", action: "stop", status: "stopped", code: -1, hasCode: true, want: "已停止", notWant: "退出码 -1"},
		{name: "already completed", action: "stop", status: "exited", code: 0, hasCode: true, want: "任务已完成，无需停止", notWant: "已退出"},
		{name: "start failed", action: "run", status: "start_failed", want: "启动失败"},
		{name: "not found", action: "status", status: "not_found", want: "任务不存在或已过期"},
		{name: "access denied", action: "status", status: "access_denied", want: "无权访问任务"},
		{name: "partial cleanup", action: "stop", status: "running", cleanup: "partial", want: "未能完全停止"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := map[string]any{"kind": "exec", "action": tt.action, "exec_status": tt.status}
			if tt.hasCode {
				metadata["exit_code"] = tt.code
			}
			if tt.cleanup != "" {
				metadata["cleanup_status"] = tt.cleanup
			}
			status := StatusDone
			if tt.status == "start_failed" || tt.status == "not_found" || tt.status == "access_denied" || tt.cleanup == "partial" || (tt.status == "exited" && tt.code != 0) {
				status = StatusError
			}
			entry := &Entry{Name: "Exec", RawName: "exec", Status: status, ParamsRaw: map[string]any{"action": tt.action}, Metadata: metadata, Result: "raw exec protocol error"}
			rendered := RenderEntry(entry, false, RenderDeps{Labels: execTestLabels()})
			if !strings.Contains(rendered, tt.want) {
				t.Fatalf("RenderEntry() missing %q:\n%s", tt.want, rendered)
			}
			if tt.notWant != "" && strings.Contains(rendered, tt.notWant) {
				t.Fatalf("RenderEntry() contains %q:\n%s", tt.notWant, rendered)
			}
			if tt.cleanup == "partial" && !strings.Contains(rendered, "请查看详情") {
				t.Fatalf("RenderEntry() missing partial cleanup detail hint:\n%s", rendered)
			}
			if strings.Contains(rendered, "raw exec protocol error") {
				t.Fatalf("Exec card repeated raw detail error:\n%s", rendered)
			}
		})
	}
}

func TestRenderExecUnstructuredErrorKeepsErrorLine(t *testing.T) {
	entry := &Entry{RawName: "exec", Name: "Exec", Status: StatusError, Result: "tool validation failed", ParamsRaw: map[string]any{"action": "run"}}
	rendered := RenderEntry(entry, false, RenderDeps{Labels: execTestLabels()})
	if !strings.Contains(rendered, "运行命令") || !strings.Contains(rendered, "tool validation failed") {
		t.Fatalf("unstructured Exec error lost its fallback detail:\n%s", rendered)
	}
}

func TestExecMainLabelPreservesShortJobIDWhenObjectIsLong(t *testing.T) {
	entry := &Entry{
		RawName: "exec",
		Intent:  "启动一个名称非常长而且会占满终端宽度的后台开发服务",
		ParamsRaw: map[string]any{
			"action":     "run",
			"background": true,
		},
		Metadata: map[string]any{"job_id": "fc71654b-be2e-49c0-b9a8-5a2e5a6324d1"},
	}
	label := ExecMainLabel(entry, 38, execTestLabels())
	if !strings.Contains(label, "启动后台任务") || !strings.Contains(label, "#fc71654b") || !strings.Contains(label, "…") {
		t.Fatalf("ExecMainLabel() = %q, want operation, truncated object, and short job ID", label)
	}
	entry.Status = StatusDone
	entry.Metadata["kind"] = "exec"
	entry.Metadata["action"] = "run"
	entry.Metadata["exec_status"] = "running"
	entry.Metadata["scope"] = "run"
	rendered := RenderEntry(entry, false, RenderDeps{Width: 52, Labels: execTestLabels()})
	if !strings.Contains(rendered, "启动后台任务") || !strings.Contains(rendered, "#fc71654b") || !strings.Contains(rendered, "运行中") {
		t.Fatalf("narrow RenderEntry() lost operation, job ID, or outcome:\n%s", rendered)
	}
}

func TestExecDetailKeepsFullParamsAndResult(t *testing.T) {
	fullID := "fc71654b-be2e-49c0-b9a8-5a2e5a6324d1"
	entry := &Entry{
		RawName: "exec",
		Params:  `{"action":"status","job_id":"` + fullID + `"}`,
		Result:  "Exec job " + fullID + " is running. Scope: session. Timeout: 30 seconds.",
	}
	deps := DetailDeps{Labels: DetailLabels{DetailTitle: "工具详情", Tool: "工具", Params: "参数", Result: "结果"}}
	lines := make([]string, DetailLineSource(entry, deps).Len())
	for i := range lines {
		lines[i] = DetailLineSource(entry, deps).Line(i)
	}
	text := strings.Join(lines, "\n")
	compactDetail := strings.ReplaceAll(text, "\n", "")
	for _, want := range []string{fullID, "Scope: session", "Timeout: 30 seconds"} {
		if !strings.Contains(compactDetail, want) {
			t.Fatalf("detail missing %q:\n%s", want, text)
		}
	}
}

func execTestLabels() RenderLabels {
	return RenderLabels{
		ExecBadge:            "命令",
		ExecRunCommand:       "运行命令",
		ExecStartTask:        "启动后台任务",
		ExecCheckTask:        "查看后台任务",
		ExecStopTask:         "停止后台任务",
		ExecRunning:          "运行中",
		ExecCompleted:        "已完成",
		ExecFailed:           "执行失败",
		ExecTimedOut:         "已超时",
		ExecCancelled:        "已取消",
		ExecStopped:          "已停止",
		ExecStartFailed:      "启动失败",
		ExecNotFound:         "任务不存在或已过期",
		ExecAccessDenied:     "无权访问任务",
		ExecAlreadyCompleted: "任务已完成，无需停止",
		ExecAlreadyFailed:    "任务已经失败，无需停止",
		ExecRunLifetime:      "随本轮结束自动停止",
		ExecSessionLifetime:  "保持到会话关闭",
		ExecElapsed:          "已运行 {}",
		ExecTotal:            "共运行 {}",
		ExecExitCode:         "退出码",
		ExecCleanupPartial:   "进程清理未完成",
		ExecStopIncomplete:   "未能完全停止",
		ExecSeeDetails:       "请查看详情",
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
