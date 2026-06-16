package toolview

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
)

func DisplayName(name string) string {
	switch name {
	case "readfile":
		return "Read"
	case "listdir":
		return "List"
	case "search":
		return "Search"
	case "filesystem":
		return "FS"
	case "http":
		return "HTTP"
	case "writefile":
		return "Write"
	case "editfile":
		return "Edit"
	case "exec":
		return "Exec"
	case "askuser":
		return "Ask"
	case "spawn":
		return "Spawn"
	default:
		return name
	}
}

func FormatParams(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	b, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", params)
	}
	return string(b)
}

func ParamSummary(name string, params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	pick := func(keys ...string) string {
		for _, key := range keys {
			if v, ok := params[key]; ok {
				s := fmt.Sprintf("%v", v)
				if s != "" {
					return textutil.TruncateRunes(s, 32)
				}
			}
		}
		return ""
	}
	switch name {
	case "readfile", "writefile", "editfile", "listdir":
		return pick("path")
	case "search":
		pattern := pick("pattern")
		if pattern == "" {
			pattern = pick("terms")
		}
		if pattern != "" {
			return pattern + " in " + pick("path")
		}
		return pick("path")
	case "filesystem":
		action := pick("action")
		path := pick("path")
		dst := pick("destination")
		if dst != "" {
			return action + " " + path + " → " + dst
		}
		return action + " " + path
	case "exec":
		return pick("command")
	case "http":
		method := pick("method")
		if method == "" {
			method = "GET"
		}
		return method + " " + pick("url")
	case "spawn":
		return pick("task")
	case "askuser":
		return pick("question")
	default:
		return pick("name", "id", "path", "query")
	}
}

func CompactPath(path string, maxWidth int) string {
	if maxWidth <= 0 || lipgloss.Width(path) <= maxWidth {
		return path
	}
	const ellipsis = "…"
	base := path
	sepIdx := strings.LastIndexAny(path, "/\\")
	if sepIdx >= 0 && sepIdx < len(path)-1 {
		base = path[sepIdx+1:]
	}
	if lipgloss.Width(base)+lipgloss.Width(ellipsis) <= maxWidth {
		return ellipsis + base
	}
	if sepIdx >= 0 && sepIdx < len(path)-1 {
		dir := strings.TrimRight(path[:sepIdx], "/\\")
		parent := dir
		if parentIdx := strings.LastIndexAny(dir, "/\\"); parentIdx >= 0 && parentIdx < len(dir)-1 {
			parent = dir[parentIdx+1:]
		}
		withParent := ellipsis + parent + string(path[sepIdx]) + base
		if lipgloss.Width(withParent) <= maxWidth {
			return withParent
		}
	}
	return truncateRunesKeepEnd(base, maxWidth)
}

func truncateRunesKeepEnd(s string, maxWidth int) string {
	const ellipsis = "…"
	if maxWidth <= lipgloss.Width(ellipsis) {
		return ellipsis
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(ellipsis+string(runes)) > maxWidth {
		runes = runes[1:]
	}
	return ellipsis + string(runes)
}

func FormatTinyBytes(n int) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%dB", n)
}

func MetadataInt(value any) int {
	n, _ := MetadataIntOK(value)
	return n
}

func MetadataIntOK(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i), true
		}
	}
	return 0, false
}
