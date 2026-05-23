package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var execPathTokenPattern = regexp.MustCompile(`(?:^|[\s=])["']?((?:~|\.\.?|/|[A-Za-z0-9._-]+/)[^"'\s;|&<>]*)`)
var execRedirectionPattern = regexp.MustCompile(`[<>]{1,2}\s*([^\s;|&]+)`)
var execQuotedAbsPathPattern = regexp.MustCompile(`["'](/[^"']*)["']`)
var execShellExpansionPattern = regexp.MustCompile(`\$(?:\{|[A-Za-z_])`)

func normalizeWorkspaceRoot(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = expandPathForCheck(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

func (g *Guard) checkWorkspace(tool string, params map[string]any) (bool, string) {
	if g == nil || g.workspace == "" {
		return false, ""
	}
	switch tool {
	case "readfile", "listdir", "writefile", "editfile":
		path, _ := params["path"].(string)
		return g.checkWorkspacePath(tool, "path", path, g.workspace)
	case "exec":
		cwd, _ := params["cwd"].(string)
		if cwd == "" {
			cwd = g.workspace
		}
		if blocked, reason := g.checkWorkspacePath(tool, "cwd", cwd, ""); blocked {
			return true, reason
		}
		command, _ := params["command"].(string)
		return g.checkExecWorkspacePaths(command, cwd)
	default:
		return false, ""
	}
}

func (g *Guard) checkWorkspacePath(tool string, field string, path string, baseDir string) (bool, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return true, fmt.Sprintf("workspace boundary: %s.%s is required when guard.workspace is set to %s", tool, field, g.workspace)
	}
	resolved, err := resolveWorkspaceTarget(path, baseDir)
	if err != nil {
		return true, fmt.Sprintf("workspace boundary: cannot resolve %s.%s %q: %v", tool, field, path, err)
	}
	if !isPathInside(g.workspace, resolved) {
		return true, fmt.Sprintf("workspace boundary: %s.%s %q resolves to %q outside workspace %q", tool, field, path, resolved, g.workspace)
	}
	return false, ""
}

func (g *Guard) checkExecWorkspacePaths(command string, cwd string) (bool, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return false, ""
	}
	if execShellExpansionPattern.MatchString(command) {
		return true, "workspace boundary: exec.command uses shell expansion that cannot be safely checked against workspace"
	}
	if blocked, reason := g.checkExecPathTokens(command, cwd); blocked {
		return true, reason
	}
	for _, match := range execQuotedAbsPathPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		if blocked, reason := g.checkWorkspacePath("exec", "command", match[1], cwd); blocked {
			return true, reason
		}
	}
	for _, match := range execRedirectionPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		path := trimShellPathToken(match[1])
		if path == "" || isShellDescriptor(path) {
			continue
		}
		if blocked, reason := g.checkWorkspacePath("exec", "redirection", path, cwd); blocked {
			return true, reason
		}
	}
	return false, ""
}

func (g *Guard) checkExecPathTokens(command string, cwd string) (bool, string) {
	for _, match := range execPathTokenPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		path := trimShellPathToken(match[1])
		if path == "" || path == "." || strings.HasPrefix(path, "./-") || strings.Contains(path, "://") {
			continue
		}
		if runtime.GOOS == "windows" && strings.HasPrefix(path, "/") && len(path) > 1 && path[1] != '/' {
			continue
		}
		if blocked, reason := g.checkWorkspacePath("exec", "command", path, cwd); blocked {
			return true, reason
		}
	}
	return false, ""
}

func resolveWorkspaceTarget(path string, baseDir string) (string, error) {
	expanded := expandWorkspacePath(path, baseDir)
	if real, err := filepath.EvalSymlinks(expanded); err == nil {
		return filepath.Clean(real), nil
	}
	parent, leaf := nearestExistingParent(expanded)
	if parent == "" {
		return "", fmt.Errorf("no existing parent")
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	if leaf == "" {
		return filepath.Clean(realParent), nil
	}
	return filepath.Clean(filepath.Join(realParent, leaf)), nil
}

func expandWorkspacePath(path string, baseDir string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		if home != "" {
			path = filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if baseDir == "" {
		baseDir, _ = os.Getwd()
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func nearestExistingParent(path string) (string, string) {
	path = filepath.Clean(path)
	var missing []string
	for {
		if _, err := os.Lstat(path); err == nil {
			for i, j := 0, len(missing)-1; i < j; i, j = i+1, j-1 {
				missing[i], missing[j] = missing[j], missing[i]
			}
			return path, filepath.Join(missing...)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", ""
		}
		missing = append(missing, filepath.Base(path))
		path = parent
	}
}

func isPathInside(root string, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if samePath(root, target) {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func samePath(a string, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func trimShellPathToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, `"'`)
	for strings.HasSuffix(token, ",") || strings.HasSuffix(token, ")") {
		token = token[:len(token)-1]
	}
	return token
}

func isShellDescriptor(path string) bool {
	return path == "&1" || path == "&2" || path == "-" || strings.HasPrefix(path, "&")
}
