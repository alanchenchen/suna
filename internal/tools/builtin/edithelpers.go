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
