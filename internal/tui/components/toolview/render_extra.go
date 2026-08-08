package toolview

import (
	"fmt"
	"strings"
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

// RenderExecSummary 只解释一次 exec 工具调用的结果边界，不把后台作业误呈现为实时进程面板。
func RenderExecSummary(metadata map[string]any, prefix string, deps RenderDeps) string {
	action := metadataString(metadata, "action")
	status := metadataString(metadata, "exec_status")
	parts := []string{deps.Styles.Dim.Render(defaultLabel(deps.Labels.ExecBadge, "Exec"))}

	semantic := execSemanticLabel(action, status, deps.Labels)
	if semantic != "" {
		parts = append(parts, deps.Styles.MetaPill.Render(semantic))
	}
	if jobID := metadataString(metadata, "job_id"); jobID != "" {
		parts = append(parts, deps.Styles.ToolDim.Render(defaultLabel(deps.Labels.ExecJobID, "job")+" "+jobID))
	}
	if scope := metadataString(metadata, "scope"); scope != "" {
		parts = append(parts, deps.Styles.ToolDim.Render(defaultLabel(deps.Labels.ExecScope, "scope")+" "+scope))
	}
	if timeout := MetadataInt(metadata["timeout_seconds"]); timeout > 0 {
		parts = append(parts, deps.Styles.ToolDim.Render(fmt.Sprintf("%s %ds", defaultLabel(deps.Labels.ExecTimeout, "timeout"), timeout)))
	}
	if code, ok := MetadataIntOK(metadata["exit_code"]); ok {
		parts = append(parts, deps.Styles.ToolDim.Render(fmt.Sprintf("%s %d", defaultLabel(deps.Labels.ExecExitCode, "exit"), code)))
	}
	if cleanup := metadataString(metadata, "cleanup_status"); cleanup != "" {
		parts = append(parts, deps.Styles.ToolDim.Render(defaultLabel(deps.Labels.ExecCleanupStatus, "cleanup")+" "+cleanup))
	}
	if status == "running" || action == "start" || action == "background" {
		scope := metadataString(metadata, "scope")
		cleanupLabel := deps.Labels.ExecRunCleanup
		cleanupFallback := "auto-cleaned when this run ends"
		if scope == "session" {
			cleanupLabel = deps.Labels.ExecSessionCleanup
			cleanupFallback = "kept until the session closes"
		}
		parts = append(parts, deps.Styles.ToolDim.Render(defaultLabel(cleanupLabel, cleanupFallback)))
	}
	return prefix + "  " + deps.Styles.Dim.Render("↳ ") + strings.Join(parts, "  ")
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func execSemanticLabel(action, status string, labels RenderLabels) string {
	switch status {
	case "running":
		return defaultLabel(labels.ExecRunning, "running in background")
	case "timeout", "timed_out":
		return defaultLabel(labels.ExecTimedOut, "timed out")
	case "cancelled", "canceled":
		return defaultLabel(labels.ExecCancelled, "cancelled")
	case "stopped":
		return defaultLabel(labels.ExecStopped, "stopped")
	case "exited", "done", "completed":
		return defaultLabel(labels.ExecExited, "exited")
	case "cleanup", "cleaned":
		return defaultLabel(labels.ExecCleanup, "cleaned up")
	}
	switch action {
	case "timeout":
		return defaultLabel(labels.ExecTimedOut, "timed out")
	case "cancel":
		return defaultLabel(labels.ExecCancelled, "cancelled")
	case "stop", "stopped":
		return defaultLabel(labels.ExecStopped, "stopped")
	case "cleanup":
		return defaultLabel(labels.ExecCleanup, "cleaned up")
	}
	return status
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
