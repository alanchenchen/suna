package toolview

import (
	"fmt"
	"strings"
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
			mode = "content"
		}
		if mode == "content" {
			mode = defaultLabel(labels.ModeContent, mode)
		}
		return compactSummary(fmt.Sprintf("%s %q in %s", mode, pick("query"), pick("path")), maxWidth)
	case "filesystem":
		action := pick("action")
		path := pick("path")
		dst := pick("destination")
		if dst != "" {
			return compactSummary(fmt.Sprintf("%s %s → %s", action, path, dst), maxWidth)
		}
		return compactSummary(fmt.Sprintf("%s %s", action, path), maxWidth)
	case "http":
		method := pick("method")
		if method == "" {
			method = "GET"
		}
		return compactSummary(method+" "+pick("url"), maxWidth)
	case "exec":
		return compactSummary(pick("command"), maxWidth)
	case "readfile", "listdir", "writefile", "editfile":
		return compactSummary(pick("path"), maxWidth)
	default:
		return ""
	}
}

func compactSummary(s string, maxWidth int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return CompactPath(s, maxWidth)
}
