package builtin

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// 候选只用有限上下文排序，不执行模糊替换；重复代码也不能触发无界的两两全文比较。
	editProbeLines = 8
	editProbeBytes = 4096
	editMinAnchor  = 4
)

type editCandidate struct {
	start      int
	matches    int
	similarity int
	ties       int
}

// editNotFoundError 只诊断暂存内容，不改变精确匹配、替换范围或原子写入语义。
func editNotFoundError(content, target string) *editOperationError {
	message := "old_string has no exact match in the current staged content. Read the relevant range with readfile and use its exact text without line numbers"
	candidate, ok := findEditCandidate(content, target)
	if !ok {
		return &editOperationError{Code: "EDIT_OLD_NOT_FOUND", Message: message}
	}
	actual := content[candidate.start:]
	diff := commonPrefixLen(actual, target)
	if diff == len(target) {
		// 此函数只在精确匹配失败后调用；防止独立诊断调用把已存在的内容误报为差异。
		return &editOperationError{Code: "EDIT_OLD_NOT_FOUND", Message: message}
	}
	// 公共字节前缀可能在合法多字节字符中间结束；无效字节本身必须单独报告，不能误退到前一个字符。
	diff = min(editRuneBoundary(actual, diff), editRuneBoundary(target, diff))
	position := candidate.start + diff
	line := strings.Count(content[:position], "\n") + 1
	lineStart := strings.LastIndexByte(content[:position], '\n') + 1
	column := utf8.RuneCountInString(content[lineStart:position]) + 1
	fileLine, _ := editLine(content, lineStart)
	requestedStart := strings.LastIndexByte(target[:diff], '\n') + 1
	requestedLine, _ := editLine(target, requestedStart)
	buffer := make([]byte, 0, len(fileLine)+len(requestedLine)+384)
	buffer = fmt.Appendf(buffer, "no exact match; closest candidate differs at line %d, column %d in staged content:\nFile:      ", line, column)
	buffer = strconv.AppendQuote(buffer, fileLine)
	buffer = append(buffer, "\nRequested: "...)
	buffer = strconv.AppendQuote(buffer, requestedLine)
	buffer = append(buffer, "\nDifference: file "...)
	buffer = appendEditCharacter(buffer, content, position, "end of file")
	buffer = append(buffer, "; requested "...)
	buffer = appendEditCharacter(buffer, target, diff, "end of old_string")
	if candidate.ties > 1 {
		buffer = fmt.Appendf(buffer, "\n%d equally ranked candidates; showing the first. Read the intended range with readfile before retrying", candidate.ties)
	} else {
		buffer = append(buffer, "\nFile/Requested are quoted strings: decode escapes and omit the surrounding quotes when correcting old_string. Match the intended block exactly; do not include line-number prefixes"...)
	}
	return &editOperationError{Code: "EDIT_OLD_NOT_FOUND", Message: string(buffer)}
}

// findEditCandidate 逐行扫描文件，不构造文件行切片；仅输入片段的前几行存放在栈上。
// 空白归一仅用于候选排序，真正差异始终按完整原文定位，不把近似位置称作精确匹配。
func findEditCandidate(content, target string) (editCandidate, bool) {
	var lines [editProbeLines]string
	count, anchor := 0, -1
	for offset := 0; offset < len(target) && count < len(lines); {
		line, next := editLine(target, offset)
		lines[count] = strings.Trim(line, " \t\r\n")
		if anchor < 0 && len(lines[count]) >= editMinAnchor {
			anchor = count
		}
		count++
		offset = next
	}
	if anchor < 0 {
		return editCandidate{}, false
	}
	first, _ := editLine(target, 0)
	first = strings.TrimSuffix(first, "\n")
	best := scanEditCandidates(content, first, lines[:count], anchor)
	return best, best.start >= 0
}

func scanEditCandidates(content, first string, lines []string, anchor int) editCandidate {
	best := editCandidate{start: -1}
	for offset := 0; offset < len(content); {
		line, next := editLine(content, offset)
		body := strings.Trim(line, " \t\r\n")
		if editLineSimilarity(body, lines[anchor]) >= editMinAnchor {
			start := offset
			valid := true
			for range anchor {
				if start == 0 {
					valid = false
					break
				}
				start = strings.LastIndexByte(content[:start-1], '\n') + 1
			}
			if valid {
				// old_string 是子串而非整行：不带缩进的首行允许从文件缩进之后开始。
				// 有精确首行时优先按其真实子串起点对齐，避免把行前正文当成差异。
				firstFile, _ := editLine(content, start)
				if first != "" {
					if index := strings.Index(firstFile, first); index >= 0 {
						start += index
					} else if first[0] != ' ' && first[0] != '\t' && first[0] != '\r' {
						start += len(firstFile) - len(strings.TrimLeft(firstFile, " \t"))
					}
				}
				matches, similarity := scoreEditCandidate(content, start, lines)
				if matches > best.matches || (matches == best.matches && similarity > best.similarity) {
					best = editCandidate{start: start, matches: matches, similarity: similarity, ties: 1}
				} else if matches == best.matches && similarity == best.similarity && best.start >= 0 {
					best.ties++
				}
			}
		}
		offset = next
	}
	return best
}

func scoreEditCandidate(content string, start int, target []string) (int, int) {
	// 每个候选最多读取固定字节与行数，文件大小与 old_string 长度不能共同放大诊断工作量。
	probe := content[start:min(len(content), start+editProbeBytes)]
	matches, similarity, offset := 0, 0, 0
	for _, want := range target {
		if offset >= len(probe) {
			break
		}
		line, next := editLine(probe, offset)
		got := strings.Trim(line, " \t\r\n")
		if len(want) >= editMinAnchor {
			if got == want {
				matches++
				similarity += min(256, len(want))
			} else {
				similarity += min(256, editLineSimilarity(got, want))
			}
		}
		offset = next
	}
	return matches, similarity
}

func editLineSimilarity(a, b string) int {
	if a == b {
		return len(a)
	}
	prefix := commonPrefixLen(a, b)
	a, b = a[prefix:], b[prefix:]
	suffix := 0
	for suffix < len(a) && suffix < len(b) && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return prefix + suffix
}

// editLine 保留原始行尾和所有空白；只有展示时转义，避免提示本身丢掉导致匹配失败的字符。
func editLine(text string, start int) (string, int) {
	if end := strings.IndexByte(text[start:], '\n'); end >= 0 {
		end += start + 1
		return text[start:end], end
	}
	return text[start:], len(text)
}

func appendEditCharacter(buffer []byte, text string, offset int, end string) []byte {
	if offset == len(text) {
		return append(buffer, end...)
	}
	r, size := utf8.DecodeRuneInString(text[offset:])
	if r == utf8.RuneError && size == 1 {
		return fmt.Appendf(buffer, "byte 0x%02X", text[offset])
	}
	buffer = strconv.AppendQuoteRune(buffer, r)
	return fmt.Appendf(buffer, " (U+%04X)", r)
}

func editRuneBoundary(text string, offset int) int {
	start := offset
	for start > 0 && start < len(text) && !utf8.RuneStart(text[start]) && offset-start < utf8.UTFMax-1 {
		start--
	}
	if _, size := utf8.DecodeRuneInString(text[start:]); size > 1 && start+size > offset {
		return start
	}
	return offset
}

func commonPrefixLen(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}
