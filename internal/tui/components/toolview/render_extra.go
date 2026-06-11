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
	dst, _ := metadata["destination"].(string)
	entryKind, _ := metadata["entry_kind"].(string)
	recursive, _ := metadata["recursive"].(bool)
	overwritten, _ := metadata["overwritten"].(bool)
	entries := MetadataInt(metadata["entries"])
	size := MetadataInt(metadata["size"])

	s := deps.Styles
	parts := []string{s.MetaPill.Render(defaultLabel(deps.Labels.FSBadge, "FS"))}
	pathText := CompactPath(path, maxInt(10, deps.width()/3))
	if dst != "" {
		pathText += " → " + CompactPath(dst, maxInt(10, deps.width()/3))
	}
	parts = append(parts, s.FilePath.Render(pathText), renderFSAction(action, deps.Labels, s))
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
