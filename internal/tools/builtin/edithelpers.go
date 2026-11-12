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

// lineBody 返回行尾去空白后的内容，用于按行比较。
func lineBody(line string) string {
	return strings.TrimRight(line, " \t\r")
}

// lineIndent 返回行的缩进宽度（Tab 折算为 4）与去缩进后的内容。
func lineIndent(line string) (int, string) {
	trimmed := lineBody(line)
	width := 0
	i := 0
	for ; i < len(trimmed); i++ {
		switch trimmed[i] {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width, trimmed[i:]
		}
	}
	return width, ""
}

// locateMismatch 找到 old_string 与文件第一处不一致的位置，返回文件行号与真实行。
// 只用于诊断，不参与替换，因此不会影响文件内容。
//
// 只回显首行是不够的：多行 old_string 常常首行正确、后面某行记错，
// 那种情况下回显首行等于把模型已经写对的内容再发一遍，提供不了新信息。
// 这里按行定位第一处差异，命中位置才是模型真正需要修正的地方。
func locateMismatch(content, target string) (int, string, bool) {
	targetLines := strings.Split(target, "\n")
	contentLines := strings.Split(content, "\n")

	// 先找首行内容在文件中的候选位置，允许缩进与行尾空白不同。
	if len(targetLines) == 0 {
		return 0, "", false
	}
	_, wantFirst := lineIndent(targetLines[0])
	if wantFirst == "" {
		return 0, "", false
	}
	bestStart, bestScore := -1, 0
	for i, line := range contentLines {
		_, body := lineIndent(line)
		score := commonPrefixLen(body, wantFirst)
		if score > bestScore {
			bestStart, bestScore = i, score
		}
	}
	// 至少 4 个字符重合，避免把无关短行当成候选。
	if bestStart < 0 || bestScore < 4 {
		return 0, "", false
	}

	// 从候选位置起逐行比对，返回第一处内容不一致的文件行。
	for i := range targetLines {
		fileIndex := bestStart + i
		if fileIndex >= len(contentLines) {
			// old_string 比文件长：回显文件最后一行，提示目标已到文件末尾。
			last := len(contentLines) - 1
			return last + 1, lineBody(contentLines[last]), true
		}
		_, want := lineIndent(targetLines[i])
		_, got := lineIndent(contentLines[fileIndex])
		if want != got {
			return fileIndex + 1, lineBody(contentLines[fileIndex]), true
		}
	}
	// 内容逐行一致但精确匹配仍失败，说明差异只在空白或换行风格上，
	// 回显首行让模型看到文件里的真实缩进。
	return bestStart + 1, lineBody(contentLines[bestStart]), true
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
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
