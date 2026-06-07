package builtin

import (
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
	"strings"
)

// 行数统计只在变化片段较小时做 LCS；大文件退化为前后缀差异，避免 O(n*m) 爆炸。
const maxLineDeltaLCSCells = 1_000_000

type fileChange struct {
	Path         string
	Operation    string
	OldContent   string
	NewContent   string
	OldExists    bool
	Replacements int
}

func fileChangeResult(change fileChange) tools.Result {
	added, removed := lineDelta(change.OldContent, change.NewContent)
	before := len(change.OldContent)
	after := len(change.NewContent)

	// Content 保持单行，减少 LLM 上下文占用；结构化信息放入 Metadata 供 TUI 渲染。
	metadata := map[string]any{
		"kind":          "file_change",
		"path":          change.Path,
		"operation":     change.Operation,
		"added_lines":   added,
		"removed_lines": removed,
		"size_after":    after,
	}
	if change.OldExists {
		metadata["size_before"] = before
	}
	if change.Replacements > 0 {
		metadata["replacements"] = change.Replacements
	}

	return tools.Result{Content: fileChangeContent(change, added, removed, before, after), Metadata: metadata}
}

func fileChangeContent(change fileChange, added, removed, before, after int) string {
	parts := []string{fmt.Sprintf("+%d -%d", added, removed)}
	if change.Replacements > 0 {
		label := "replacement"
		if change.Replacements != 1 {
			label = "replacements"
		}
		parts = append(parts, fmt.Sprintf("%d %s", change.Replacements, label))
	}
	if change.OldExists && before != after {
		parts = append(parts, fmt.Sprintf("%dB -> %dB", before, after))
	} else {
		parts = append(parts, fmt.Sprintf("%dB", after))
	}
	return fmt.Sprintf("file %s: %s (%s)", change.Operation, change.Path, strings.Join(parts, ", "))
}

func lineDelta(oldContent, newContent string) (int, int) {
	oldLines := splitContentLines(oldContent)
	newLines := splitContentLines(newContent)
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix && oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	oldMid := oldLines[prefix : len(oldLines)-suffix]
	newMid := newLines[prefix : len(newLines)-suffix]
	if len(oldMid)*len(newMid) <= maxLineDeltaLCSCells {
		lcs := lineLCS(oldMid, newMid)
		return len(newMid) - lcs, len(oldMid) - lcs
	}
	return len(newMid), len(oldMid)
}

func splitContentLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func lineLCS(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
		for j := range cur {
			cur[j] = 0
		}
	}
	return prev[len(b)]
}
