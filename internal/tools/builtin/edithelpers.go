package builtin

import (
	"fmt"
	"strconv"
	"strings"
)

func replaceOccurrence(content string, oldStr string, newStr string, occurrence int) string {
	if occurrence <= 0 {
		return content
	}
	start := 0
	for i := 1; i <= occurrence; i++ {
		idx := strings.Index(content[start:], oldStr)
		if idx < 0 {
			return content
		}
		idx += start
		if i == occurrence {
			return content[:idx] + newStr + content[idx+len(oldStr):]
		}
		start = idx + len(oldStr)
	}
	return content
}

// matchLineNumbers 返回 old_string 前 limit 次出现的起始行号（1-based）。
// 模型看不到行号就不知道目标到底在哪，只能反复重读文件。
// limit 必须由调用方给定：一处短字符串可能匹配上千次，全量输出会撑爆上下文。
func matchLineNumbers(content, oldString string, limit int) []int {
	if limit <= 0 {
		return nil
	}
	lines := make([]int, 0, limit)
	// 行号随扫描位置单调递增，只数新增段落的换行，避免每次重数前缀。
	lineNo, scanned := 1, 0
	for offset := 0; len(lines) < limit; {
		idx := strings.Index(content[offset:], oldString)
		if idx < 0 {
			break
		}
		abs := offset + idx
		lineNo += strings.Count(content[scanned:abs], "\n")
		scanned = abs
		lines = append(lines, lineNo)
		offset = abs + len(oldString)
	}
	return lines
}

// maxEditHintLines 限制错误提示里列举的行号个数。
const maxEditHintLines = 5

// formatMatchLines 把行号渲染成错误提示片段，超过上限时只列前几处并标注总数。
func formatMatchLines(content, oldString string, total int) string {
	lines := matchLineNumbers(content, oldString, maxEditHintLines)
	if len(lines) == 0 {
		return ""
	}
	if total > len(lines) {
		return fmt.Sprintf("line %s and %d more", formatLineNumbers(lines), total-len(lines))
	}
	return "line " + formatLineNumbers(lines)
}

// formatLineNumbers 把行号列表渲染成 "7" 或 "7, 12" 形式，供错误信息直接引用。
func formatLineNumbers(lines []int) string {
	parts := make([]string, len(lines))
	for i, line := range lines {
		parts[i] = strconv.Itoa(line)
	}
	return strings.Join(parts, ", ")
}
