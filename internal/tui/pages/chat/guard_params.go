package chat

import (
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func GuardBodyParams(g *GuardConfirmView) string {
	if g == nil {
		return ""
	}
	lines := semanticGuardLines(g)
	if len(lines) > 0 {
		return strings.Join(lines, "\n")
	}
	return toolview.FormatParams(g.Params)
}

func semanticGuardLines(g *GuardConfirmView) []string {
	switch g.Tool {
	case "filesystem":
		return filesystemGuardLines(g.Params)
	case "http":
		return httpGuardLines(g.Params)
	case "exec":
		return execGuardLines(g.Params)
	case "writefile", "editfile", "readfile", "listdir", "search":
		return pathGuardLines(g.Tool, g.Params)
	default:
		return nil
	}
}

func filesystemGuardLines(params map[string]any) []string {
	action := paramString(params, "action")
	path := paramString(params, "path")
	dst := paramString(params, "destination")
	var lines []string
	switch action {
	case "remove":
		lines = append(lines, "Permanent delete", "  "+path)
		if paramBool(params, "recursive") {
			lines = append(lines, "Recursive", "  yes")
		}
	case "move":
		lines = append(lines, "Move", "  "+path, "To", "  "+dst)
	case "copy":
		lines = append(lines, "Copy", "  "+path, "To", "  "+dst)
	case "mkdir":
		lines = append(lines, "Create directory", "  "+path)
	case "stat":
		lines = append(lines, "Inspect path", "  "+path)
	default:
		lines = append(lines, "Filesystem operation", "  "+action+" "+path)
	}
	if expected := paramString(params, "expected_kind"); expected != "" {
		lines = append(lines, "Expected kind", "  "+expected)
	}
	if paramBool(params, "overwrite") {
		lines = append(lines, "Overwrite", "  yes")
	}
	return lines
}

func httpGuardLines(params map[string]any) []string {
	method := paramString(params, "method")
	if method == "" {
		method = "GET"
	}
	return []string{"HTTP " + strings.ToUpper(method), "  " + paramString(params, "url")}
}

func execGuardLines(params map[string]any) []string {
	lines := []string{"Command", "  " + paramString(params, "command")}
	if cwd := paramString(params, "cwd"); cwd != "" {
		lines = append(lines, "Working directory", "  "+cwd)
	}
	return lines
}

func pathGuardLines(tool string, params map[string]any) []string {
	path := paramString(params, "path")
	if path == "" {
		return nil
	}
	return []string{fmt.Sprintf("%s path", tool), "  " + path}
}

func paramString(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	if v, ok := params[key]; ok {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return ""
}

func paramBool(params map[string]any, key string) bool {
	if params == nil {
		return false
	}
	v, _ := params[key].(bool)
	return v
}
