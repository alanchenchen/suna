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

// lineIndent 返回行的缩进宽度（Tab 折算为 4）与去缩进后的内容。
func lineIndent(line string) (int, string) {
	trimmed := strings.TrimRight(line, " \t\r")
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

// closestEditLine 找出与 old_string 首行最接近的行，供错误信息回显真实文本。
// 只用于诊断，不参与替换，因此不会影响文件内容。
// 返回的文本已去掉行尾 \r：错误信息会进模型上下文与 TUI，不能带换行符。
func closestEditLine(content, target string) (int, string, bool) {
	first := target
	if idx := strings.IndexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	_, probe := lineIndent(first)
	if probe == "" {
		return 0, "", false
	}
	bestLine, bestText, bestScore := 0, "", 0
	for i, line := range strings.Split(content, "\n") {
		_, body := lineIndent(line)
		score := commonPrefixLen(body, probe)
		if score > bestScore {
			bestLine, bestText, bestScore = i+1, strings.TrimRight(line, "\r"), score
		}
	}
	// 至少 4 个字符重合，避免把无关短行当成候选。
	if bestScore < 4 {
		return 0, "", false
	}
	return bestLine, bestText, true
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
