package builtin

import "strings"

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
