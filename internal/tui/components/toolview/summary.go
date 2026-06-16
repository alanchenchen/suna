package toolview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func SemanticSummary(te *Entry, maxWidth int, labels RenderLabels) string {
	if te == nil || te.ParamsRaw == nil {
		return ""
	}
	pick := func(key string) string {
		if v, ok := te.ParamsRaw[key]; ok {
			return strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		return ""
	}
	switch te.RawName {
	case "search":
		mode := pick("mode")
		if mode == "" {
			mode = "auto"
		}
		if mode == "content" {
			mode = defaultLabel(labels.ModeContent, mode)
		}
		query := pick("pattern")
		if query == "" {
			query = compactTermsLabel(te.ParamsRaw["terms"])
		}
		return searchSummary(mode, query, pick("path"), maxWidth)
	case "filesystem":
		action := pick("action")
		path := pick("path")
		dst := pick("destination")
		if dst != "" {
			return compactText(fmt.Sprintf("%s %s → %s", action, CompactPath(path, maxWidth/3), CompactPath(dst, maxWidth/3)), maxWidth)
		}
		return compactText(fmt.Sprintf("%s %s", action, CompactPath(path, maxWidth-lipWidth(action)-1)), maxWidth)
	case "http":
		method := pick("method")
		if method == "" {
			method = "GET"
		}
		return compactText(method+" "+pick("url"), maxWidth)
	case "exec":
		return compactText(pick("command"), maxWidth)
	case "readfile", "listdir", "writefile", "editfile":
		return CompactPath(pick("path"), maxWidth)
	default:
		return ""
	}
}

func compactTermsLabel(value any) string {
	terms := stringsFromAny(value)
	if len(terms) == 0 {
		return ""
	}
	if len(terms) == 1 {
		return terms[0]
	}
	label := strings.Join(terms, " | ")
	return label
}

func stringsFromAny(value any) []string {
	switch v := value.(type) {
	case []string:
		return compactNonEmptyStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case nil:
		return nil
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func compactNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func searchSummary(mode, query, path string, maxWidth int) string {
	if strings.TrimSpace(query) == "" && strings.TrimSpace(path) == "" {
		return ""
	}
	prefix := strings.TrimSpace(mode)
	if prefix == "" {
		prefix = "auto"
	}
	pathLabel := CompactPath(path, maxInt(8, maxWidth/3))
	queryBudget := maxInt(8, maxWidth-lipWidth(prefix)-lipWidth(pathLabel)-6)
	queryLabel := compactText(query, queryBudget)
	return compactText(fmt.Sprintf("%s %q in %s", prefix, queryLabel, pathLabel), maxWidth)
}

func compactText(s string, maxWidth int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return ""
	}
	if maxWidth <= 0 || lipWidth(s) <= maxWidth {
		return s
	}
	const ellipsis = "…"
	if maxWidth <= lipWidth(ellipsis) {
		return ellipsis
	}
	runes := []rune(s)
	for len(runes) > 0 && lipWidth(string(runes)+ellipsis) > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimSpace(string(runes)) + ellipsis
}

func lipWidth(s string) int {
	return lipgloss.Width(s)
}
