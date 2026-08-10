package toolview

import (
	"fmt"
	"strings"
	"time"
)

func RenderFSChangeSummary(metadata map[string]any, prefix string, deps RenderDeps) string {
	action, _ := metadata["action"].(string)
	path, _ := metadata["path"].(string)
	if action == "" || path == "" {
		return ""
	}
	entryKind, _ := metadata["entry_kind"].(string)
	recursive, _ := metadata["recursive"].(bool)
	overwritten, _ := metadata["overwritten"].(bool)
	entries := MetadataInt(metadata["entries"])
	size := MetadataInt(metadata["size"])

	s := deps.Styles
	// 文件系统操作的主行已经展示了 action/path/destination，这里沿用文件变更摘要的紧凑样式，
	// 避免在下一行重复长路径；保留 kind、recursive、entries、size 等关键结果信息。
	parts := []string{s.Dim.Render(defaultLabel(deps.Labels.FSBadge, "FS")), renderFSAction(action, deps.Labels, s)}
	if entryKind != "" && entryKind != "missing" {
		parts = append(parts, s.ToolDim.Render(entryKind))
	}
	if recursive {
		parts = append(parts, s.GuardWarn.Render(defaultLabel(deps.Labels.Recursive, "recursive")))
	}
	if overwritten {
		parts = append(parts, s.GuardWarn.Render(defaultLabel(deps.Labels.Overwrote, "overwrote")))
	}
	if entries > 1 {
		parts = append(parts, s.ToolDim.Render(countLabel(entries, defaultLabel(deps.Labels.Entries, "entries"))))
	}
	if size > 0 {
		parts = append(parts, s.ToolDim.Render(FormatTinyBytes(size)))
	}
	return prefix + "  " + s.Dim.Render("↳ ") + strings.Join(parts, "  ")
}

func renderFSAction(action string, labels RenderLabels, s RenderStyles) string {
	switch action {
	case "remove":
		return s.GuardErr.Render(defaultLabel(labels.FSDeleted, "PERMANENTLY DELETED"))
	case "mkdir":
		return s.GuardOK.Render(defaultLabel(labels.FSCreatedDir, "CREATED DIR"))
	case "move":
		return s.MetaPill.Render(defaultLabel(labels.FSMoved, "MOVED"))
	case "copy":
		return s.MetaPill.Render(defaultLabel(labels.FSCopied, "COPIED"))
	default:
		return s.ToolDim.Render(strings.ToUpper(action))
	}
}

func RenderSearchSummary(metadata map[string]any, prefix string, deps RenderDeps) string {
	matches := MetadataInt(metadata["matches"])
	filesMatched := MetadataInt(metadata["files_matched"])
	filesScanned := MetadataInt(metadata["files_scanned"])
	truncated, _ := metadata["truncated"].(bool)
	text := formatTwoCount(defaultLabel(deps.Labels.SearchMatchesInFiles, "{} matches in {} files"), matches, filesMatched)
	var parts []string
	if n := MetadataInt(metadata["path_matches"]); n > 0 {
		parts = append(parts, fmt.Sprintf("%d path", n))
	}
	if n := MetadataInt(metadata["symbol_matches"]); n > 0 {
		parts = append(parts, fmt.Sprintf("%d symbol", n))
	}
	if n := MetadataInt(metadata["content_matches"]); n > 0 {
		parts = append(parts, fmt.Sprintf("%d content", n))
	}
	if len(parts) > 0 {
		text += "  " + strings.Join(parts, " / ")
	}
	if filesScanned > 0 {
		text += "  " + formatOneCount(defaultLabel(deps.Labels.SearchScanned, "scanned {}"), filesScanned)
	}
	if truncated {
		text += "  " + defaultLabel(deps.Labels.SearchTruncated, "truncated")
	}
	return prefix + "  " + deps.Styles.Dim.Render("↳ ") + deps.Styles.ToolDim.Render(text)
}

func RenderHTTPSummary(metadata map[string]any, prefix string, deps RenderDeps) string {
	method, _ := metadata["method"].(string)
	status := MetadataInt(metadata["status"])
	bodyBytes := MetadataInt(metadata["body_bytes"])
	text := fmt.Sprintf("HTTP %s  %d", method, status)
	if bodyBytes > 0 {
		text += "  " + FormatTinyBytes(bodyBytes)
	}
	return prefix + "  " + deps.Styles.Dim.Render("↳ ") + deps.Styles.ToolDim.Render(text)
}

// IsExec 判断条目是否为 exec；运行中尚无结果 metadata 时仍可通过工具名识别。
func IsExec(te *Entry) bool {
	if te == nil {
		return false
	}
	if te.RawName == "exec" {
		return true
	}
	kind, _ := te.Metadata["kind"].(string)
	return kind == "exec"
}

// HasExecSummary 判断结构化结果是否足够生成用户结论；缺失时必须保留通用错误行。
func HasExecSummary(te *Entry) bool {
	return IsExec(te) && metadataString(te.Metadata, "exec_status") != ""
}

// ExecMainLabel 把协议 action 翻译为用户动作；默认卡片只保留短任务标识，完整参数留在详情。
func ExecMainLabel(te *Entry, maxWidth int, labels RenderLabels) string {
	if te == nil {
		return ""
	}
	action := execEntryValue(te, "action")
	background, _ := te.ParamsRaw["background"].(bool)
	if action == "" {
		action = "run"
	}

	operation := defaultLabel(labels.ExecRunCommand, "Run command")
	switch action {
	case "status":
		operation = defaultLabel(labels.ExecCheckTask, "Check background task")
	case "stop":
		operation = defaultLabel(labels.ExecStopTask, "Stop background task")
	case "run":
		if background || metadataString(te.Metadata, "job_id") != "" {
			operation = defaultLabel(labels.ExecStartTask, "Start background task")
		}
	}

	var object string
	if action == "run" {
		object = compactLabelText(te.Intent)
		if object == "" {
			object = execEntryParam(te, "command")
		}
	}
	if jobID := shortExecJobID(execEntryValue(te, "job_id")); jobID != "" {
		return execMainLabelWithJob(operation, object, "#"+jobID, maxWidth)
	}
	if object == "" {
		return compactText(operation, maxWidth)
	}
	return compactText(operation+"  "+object, maxWidth)
}

// execMainLabelWithJob 优先保留操作和短任务号，长命令或 intent 只占剩余空间。
func execMainLabelWithJob(operation, object, jobLabel string, maxWidth int) string {
	base := operation + "  "
	if object == "" {
		return compactText(base+jobLabel, maxWidth)
	}
	suffix := " · " + jobLabel
	objectWidth := maxWidth - lipWidth(base) - lipWidth(suffix)
	if objectWidth < 4 {
		return compactText(base+jobLabel, maxWidth)
	}
	return base + compactText(object, objectWidth) + suffix
}

// RenderExecSummary 只展示用户结论；原始状态、完整 UUID、scope、timeout 和 cleanup 仍保留在工具详情结果中。
func RenderExecSummary(te *Entry, prefix string, deps RenderDeps) string {
	if te == nil || te.Metadata == nil {
		return ""
	}
	action := metadataString(te.Metadata, "action")
	status := metadataString(te.Metadata, "exec_status")
	code, hasCode := MetadataIntOK(te.Metadata["exit_code"])
	cleanup := metadataString(te.Metadata, "cleanup_status")
	labels := deps.Labels

	outcome := execOutcomeLabel(action, status, code, hasCode, cleanup, labels)
	if outcome == "" {
		return ""
	}
	parts := []string{deps.Styles.Dim.Render(defaultLabel(labels.ExecBadge, "Exec")), renderExecOutcome(outcome, status, code, hasCode, cleanup, deps.Styles)}

	// scope 只有在任务仍运行、仍会影响未来生命周期时才对用户有意义。
	if status == "running" {
		switch metadataString(te.Metadata, "scope") {
		case "session":
			parts = append(parts, deps.Styles.ToolDim.Render(defaultLabel(labels.ExecSessionLifetime, "kept until the session closes")))
		case "run":
			parts = append(parts, deps.Styles.ToolDim.Render(defaultLabel(labels.ExecRunLifetime, "stops when this run ends")))
		}
	}

	if duration := execDurationLabel(action, status, te.Metadata, labels); duration != "" {
		parts = append(parts, deps.Styles.ToolDim.Render(duration))
	}
	if status == "exited" && hasCode && code != 0 {
		parts = append(parts, deps.Styles.ToolDim.Render(fmt.Sprintf("%s %d", defaultLabel(labels.ExecExitCode, "exit code"), code)))
	}
	if cleanup == "partial" {
		if action == "stop" {
			parts = append(parts, deps.Styles.ToolDim.Render(defaultLabel(labels.ExecSeeDetails, "see details")))
		} else {
			parts = append(parts, deps.Styles.GuardWarn.Render(defaultLabel(labels.ExecCleanupPartial, "process cleanup incomplete")))
		}
	}

	return prefix + "  " + deps.Styles.Dim.Render("↳ ") + strings.Join(parts, "  ")
}

func execOutcomeLabel(action, status string, code int, hasCode bool, cleanup string, labels RenderLabels) string {
	if cleanup == "partial" && action == "stop" {
		return defaultLabel(labels.ExecStopIncomplete, "Could not fully stop")
	}
	switch status {
	case "running":
		return defaultLabel(labels.ExecRunning, "Running")
	case "timeout", "timed_out":
		return defaultLabel(labels.ExecTimedOut, "Timed out")
	case "cancelled", "canceled":
		return defaultLabel(labels.ExecCancelled, "Cancelled")
	case "stopped":
		return defaultLabel(labels.ExecStopped, "Stopped")
	case "start_failed":
		return defaultLabel(labels.ExecStartFailed, "Could not start")
	case "not_found":
		return defaultLabel(labels.ExecNotFound, "Task not found or expired")
	case "access_denied":
		return defaultLabel(labels.ExecAccessDenied, "Task access denied")
	case "exited", "done", "completed":
		failed := hasCode && code != 0
		if action == "stop" {
			if failed {
				return defaultLabel(labels.ExecAlreadyFailed, "Task already failed; no stop needed")
			}
			return defaultLabel(labels.ExecAlreadyCompleted, "Task already completed; no stop needed")
		}
		if failed {
			return defaultLabel(labels.ExecFailed, "Failed")
		}
		return defaultLabel(labels.ExecCompleted, "Completed")
	}
	return status
}

func renderExecOutcome(label, status string, code int, hasCode bool, cleanup string, styles RenderStyles) string {
	failed := status == "start_failed" || status == "not_found" || status == "access_denied" || ((status == "exited" || status == "done" || status == "completed") && hasCode && code != 0) || cleanup == "partial"
	if failed {
		return styles.GuardErr.Render(label)
	}
	if status == "exited" || status == "done" || status == "completed" {
		return styles.GuardOK.Render(label)
	}
	return styles.MetaPill.Render(label)
}

func execDurationLabel(action, status string, metadata map[string]any, labels RenderLabels) string {
	milliseconds, ok := MetadataIntOK(metadata["duration_ms"])
	if !ok || milliseconds < 0 || (action == "run" && status == "running") {
		return ""
	}
	duration := FormatCompactDuration(time.Duration(milliseconds) * time.Millisecond)
	if duration == "" {
		return ""
	}
	format := defaultLabel(labels.ExecTotal, "ran for {}")
	if status == "running" {
		format = defaultLabel(labels.ExecElapsed, "running for {}")
	}
	return strings.Replace(format, "{}", duration, 1)
}

// FormatCompactDuration 以适合紧凑工具卡片的单位展示耗时。
func FormatCompactDuration(duration time.Duration) string {
	if duration < time.Millisecond {
		return "<1ms"
	}
	if duration < time.Second {
		return fmt.Sprintf("%dms", duration/time.Millisecond)
	}
	if duration < 10*time.Second {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	return duration.Round(time.Second).String()
}

func execEntryValue(te *Entry, key string) string {
	if te == nil {
		return ""
	}
	if value := metadataString(te.Metadata, key); value != "" {
		return value
	}
	return execEntryParam(te, key)
}

func execEntryParam(te *Entry, key string) string {
	if te == nil || te.ParamsRaw == nil {
		return ""
	}
	value, ok := te.ParamsRaw[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func shortExecJobID(jobID string) string {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return ""
	}
	runes := []rune(jobID)
	if len(runes) > 8 {
		runes = runes[:8]
	}
	return string(runes)
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func defaultLabel(label string, fallback string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fallback
	}
	return label
}

func formatOneCount(format string, n int) string {
	return strings.Replace(format, "{}", fmt.Sprintf("%d", n), 1)
}

func formatTwoCount(format string, first int, second int) string {
	format = strings.Replace(format, "{}", fmt.Sprintf("%d", first), 1)
	return strings.Replace(format, "{}", fmt.Sprintf("%d", second), 1)
}
