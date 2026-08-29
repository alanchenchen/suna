package guard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/tools"
)

var execRedirectionPattern = regexp.MustCompile(`[<>]{1,2}\s*([^\s;|&]+)`)

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

func (g *Guard) checkWorkspace(ctx context.Context, tool string, params map[string]any) (bool, string, string) {
	return g.checkWorkspaceWithAnalysis(ctx, tool, params, nil)
}

// checkWorkspaceWithAnalysis 支持复用已解析的 ExecAnalysis（Check 主流程传 analysis，
// 避免同一命令多次 AST 解析）；analysis 为 nil 时自行解析（独立调用入口）。
func (g *Guard) checkWorkspaceWithAnalysis(ctx context.Context, tool string, params map[string]any, analysis *ExecAnalysis) (bool, string, string) {
	if g == nil || g.workspace == "" {
		return false, "", ""
	}
	switch tool {
	case "readfile", "listdir", "writefile", "editfile", "search":
		path, _ := params["path"].(string)
		return g.checkWorkspacePath(tool, "path", path, g.workspace)
	case "filesystem":
		path, _ := params["path"].(string)
		if blocked, reason, auditReason := g.checkWorkspacePath(tool, "path", path, g.workspace); blocked {
			return true, reason, auditReason
		}
		if dst, _ := params["destination"].(string); strings.TrimSpace(dst) != "" {
			return g.checkWorkspacePath(tool, "destination", dst, g.workspace)
		}
		return false, "", ""
	case "http":
		// file:// URL 是本地文件访问，属于 workspace 边界；网络 URL 不是路径语义，不检查。
		if u, _ := params["url"].(string); strings.HasPrefix(u, "file://") {
			path := strings.TrimPrefix(u, "file://")
			return g.checkWorkspacePath(tool, "url", path, g.workspace)
		}
		return false, "", ""
	case "exec":
		// status/stop 仅操作受管任务标识，不解析 cwd 或空 command。
		if action := execAction(params); action == "status" || action == "stop" {
			return false, "", ""
		}
		requestedCWD, _ := params["cwd"].(string)
		cwd, err := tools.EffectiveCWD(ctx, requestedCWD)
		if err != nil {
			return true, fmt.Sprintf("workspace boundary: cannot resolve exec.cwd: %v", err), "workspace_unavailable"
		}
		if blocked, reason, auditReason := g.checkWorkspacePath(tool, "cwd", cwd, ""); blocked {
			return true, reason, auditReason
		}
		command, _ := params["command"].(string)
		shell, _ := params["shell"].(string)
		return g.checkExecWorkspacePathsWithAnalysis(command, cwd, shell, analysis)
	default:
		return false, "", ""
	}
}

func (g *Guard) checkWorkspacePath(tool string, field string, path string, baseDir string) (bool, string, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return true, fmt.Sprintf("workspace boundary: %s.%s is required when guard.workspace is set to %s", tool, field, g.workspace), "workspace_path_missing"
	}
	resolved, err := resolveWorkspaceTarget(path, baseDir)
	if err != nil {
		return true, fmt.Sprintf("workspace boundary: cannot resolve %s.%s %q: %v", tool, field, path, err), "workspace_unavailable"
	}
	// Suna 自有数据目录始终允许访问。用户经常会让 Suna 排查 ~/.suna 下的配置、日志或 Skill；
	// 默认数据目录由 config paths 统一给出，避免在 Guard 内部硬编码路径字符串。
	if isPathInside(config.DefaultDataDir(), resolved) {
		return false, "", ""
	}
	if !isPathInside(g.workspace, resolved) {
		return true, fmt.Sprintf("workspace boundary: %s.%s %q resolves to %q outside workspace %q", tool, field, path, resolved, g.workspace), workspaceAuditReason(field)
	}
	return false, "", ""
}

func workspaceAuditReason(field string) string {
	switch field {
	case "cwd":
		return "workspace_cwd_outside"
	case "redirection":
		return "workspace_redirect_outside"
	default:
		return "workspace_path_outside"
	}
}

func (g *Guard) checkExecWorkspacePaths(command string, cwd string, shell string) (bool, string, string) {
	return g.checkExecWorkspacePathsWithAnalysis(command, cwd, shell, nil)
}

// checkExecWorkspacePathsWithAnalysis 复用已解析的 ExecAnalysis（Check 主流程传 analysis，
// 避免同一命令多次 AST 解析）；analysis 为 nil 时自行解析（独立调用入口）。
func (g *Guard) checkExecWorkspacePathsWithAnalysis(command string, cwd string, shell string, analysis *ExecAnalysis) (bool, string, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return false, "", ""
	}
	// 优先用 ExecAnalysis（POSIX AST 精确提取命令参数与重定向）。
	// ok=true 时完全信任 AST：路径来自 Commands.Args（引号/变量已展开或保守保留），
	// 重定向来自 Redirects（op/fd 精确，/dev/null 与 fd 重定向天然豁免）。
	if analysis == nil {
		a, ok := newExecAnalyzer().Analyze(command, shell)
		if !ok {
			analysis = &ExecAnalysis{ParseFailed: true}
		} else {
			analysis = &a
		}
	}
	if !analysis.ParseFailed {
		// 命令参数中的路径候选。
		for _, cmd := range analysis.Commands {
			for _, arg := range cmd.Args {
				if !isPathCandidate(arg) {
					continue
				}
				if blocked, reason, auditReason := g.checkWorkspacePath("exec", "command", arg, cwd); blocked {
					return true, reason, auditReason
				}
			}
		}
		// 引号内路径检查只针对解释器场景（sh -c "cat /"、printf 'ls / ' | sh）。
		// 数据场景（printf '%s' "mentions /tmp here"）的引号内路径只是文本，printf 不会访问它，
		// 不应拦截（旧实现无差别拦截是误判）。真实参数访问（cat "/tmp/x"）已由上方 AST 参数检查覆盖。
		hasInterpreter := analysisHasInterpreter(*analysis)
		if hasInterpreter {
			for _, match := range execQuotedAbsPathPattern.FindAllStringSubmatch(command, -1) {
				if len(match) < 2 {
					continue
				}
				if blocked, reason, auditReason := g.checkWorkspacePath("exec", "command", match[1], cwd); blocked {
					return true, reason, auditReason
				}
			}
			for _, match := range execQuotedPathTokenPattern.FindAllStringSubmatch(command, -1) {
				if len(match) < 2 {
					continue
				}
				if blocked, reason, auditReason := g.checkWorkspacePath("exec", "command", match[1], cwd); blocked {
					return true, reason, auditReason
				}
			}
			for _, match := range execQuotedStandaloneSlashPattern.FindAllStringSubmatch(command, -1) {
				if len(match) < 2 {
					continue
				}
				if blocked, reason, auditReason := g.checkWorkspacePath("exec", "command", match[1], cwd); blocked {
					return true, reason, auditReason
				}
			}
		}
		// 重定向目标。
		for _, r := range analysis.Redirects {
			if r.Op == "fd" || r.Target == "" {
				continue
			}
			if r.Target == "/dev/null" {
				// 丢弃输出设备，无文件副作用，不参与 workspace 检查。
				continue
			}
			if blocked, reason, auditReason := g.checkWorkspacePath("exec", "redirection", r.Target, cwd); blocked {
				return true, reason, auditReason
			}
		}
		return false, "", ""
	}
	// fallback：解析失败（POSIX 语法错误）或 Windows 保守解析时用正则（能力不低于现状）。
	quotedRanges, parsed, supported := shellQuotedRangesForWorkspace(command, shell)
	if supported && !parsed {
		return true, "workspace boundary: cannot parse exec.command safely", "workspace_unavailable"
	}
	if !execWorkspaceProseExemptionSafe(command, shell) {
		quotedRanges = nil
	}
	if blocked, reason, auditReason := g.checkExecPathTokens(command, cwd, quotedRanges); blocked {
		return true, reason, auditReason
	}
	for _, match := range execQuotedAbsPathPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		if blocked, reason, auditReason := g.checkWorkspacePath("exec", "command", match[1], cwd); blocked {
			return true, reason, auditReason
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
		if blocked, reason, auditReason := g.checkWorkspacePath("exec", "redirection", path, cwd); blocked {
			return true, reason, auditReason
		}
	}
	return false, "", ""
}

// isPathCandidate 判断参数是否值得做 workspace 检查：
// 绝对路径、~ 开头、./ 或 ../ 开头、Windows drive 路径（C:\foo 或 C:/foo）、
// 以及 ..（cd .. 可能越出 workspace）。纯命令标志（-r、--force）不是路径。
// 含 $ 的变量展开路径不检查（$(pwd)/x、$dir/x 值无法静态确定，与旧正则语义一致）。
// 必须以路径边界字符开头（/ ~ . \\ drive 字母），排除代码片段（perl 正则的 /g、
// python 的 (x)/y 等——旧 execPathTokenPattern 要求路径前有开头/空格/等号边界）。
func isPathCandidate(arg string) bool {
	if arg == "" || arg == "." {
		return false
	}
	if strings.HasPrefix(arg, "-") {
		return false
	}
	if strings.Contains(arg, "$") {
		return false
	}
	if arg == ".." || strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "~/") ||
		strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") ||
		strings.Contains(arg, "\\") || isWindowsDrivePath(arg) {
		return true
	}
	// 含 / 但以非路径字符开头（( /g、{/x、)/y）是代码片段不是路径；
	// 但含 /../ 或 /./ 段的相对路径（foo/../bar）是真实路径候选（旧正则 [^...]+/\.\.?/ 分支）。
	if strings.Contains(arg, "/") {
		first := arg[0]
		if first == '.' || first == '/' || first == '~' || first == '\\' {
			return true
		}
		return strings.Contains(arg, "/../") || strings.Contains(arg, "/./")
	}
	return false
}

// isWindowsDrivePath 判断是否为 Windows drive 路径（C:\foo、C:/foo）。
func isWindowsDrivePath(arg string) bool {
	if len(arg) < 3 {
		return false
	}
	if (arg[0] < 'A' || arg[0] > 'Z') && (arg[0] < 'a' || arg[0] > 'z') {
		return false
	}
	return arg[1] == ':' && (arg[2] == '\\' || arg[2] == '/')
}

// analysisHasInterpreter 判断命令是否含解释器（sh/bash/python 等）。
// 含解释器时引号内独立斜杠不豁免（解释器可能执行引号内容），复刻旧 execWorkspaceProseExemptionSafe 语义。
func analysisHasInterpreter(a ExecAnalysis) bool {
	for _, cmd := range a.Commands {
		switch strings.ToLower(cmd.Name) {
		case "sh", "bash", "zsh", "fish", "cmd", "powershell", "pwsh",
			"eval", "source", "python", "python3", "node", "ruby", "perl", "php":
			return true
		}
	}
	return false
}

type shellQuoteRange struct {
	start, end int
}

// shellQuotedRangesForWorkspace 只为 POSIX shell 定位闭合引号范围；cmd 与 PowerShell
// 的引号语义不同，不提供任何误判豁免。
func shellQuotedRangesForWorkspace(command, shell string) ([]shellQuoteRange, bool, bool) {
	lowerShell := strings.ToLower(strings.TrimSpace(shell))
	if lowerShell == "auto" {
		lowerShell = ""
	}
	if lowerShell == "cmd" || lowerShell == "powershell" || lowerShell == "pwsh" || (lowerShell == "" && runtime.GOOS == "windows") {
		return nil, true, false
	}
	var ranges []shellQuoteRange
	quote := byte(0)
	start := -1
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote == 0 {
			if ch == '\'' || ch == '"' {
				quote = ch
				start = i + 1
			}
			continue
		}
		if ch == quote {
			ranges = append(ranges, shellQuoteRange{start: start, end: i})
			quote = 0
			start = -1
		}
	}
	if quote != 0 || escaped {
		return nil, false, true
	}
	return ranges, true, true
}

// execWorkspaceProseExemptionSafe 复用现有 shell analyzer 的分段与字段解析。
// 只有能可靠解析且不含动态执行器的命令，才允许极窄的独立斜杠文字豁免。
func execWorkspaceProseExemptionSafe(command, shell string) bool {
	if hasDynamicShellSyntax(command, shell) {
		return false
	}
	segments, ok := splitShellSegments(command, shell)
	if !ok || len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		fields, ok := shellFields(segment)
		if !ok || len(fields) == 0 {
			return false
		}
		for _, field := range fields {
			name := strings.ToLower(filepath.Base(field))
			name = strings.TrimSuffix(name, ".exe")
			switch name {
			case "sh", "bash", "zsh", "fish", "cmd", "powershell", "pwsh",
				"eval", "source", ".", "python", "python3", "node", "ruby", "perl", "php":
				return false
			}
		}
	}
	return true
}

func (g *Guard) checkExecPathTokens(command string, cwd string, quotedRanges []shellQuoteRange) (bool, string, string) {
	for _, match := range execPathTokenPattern.FindAllStringSubmatchIndex(command, -1) {
		if len(match) < 4 || match[2] < 0 || match[3] < 0 {
			continue
		}
		path := trimShellPathToken(command[match[2]:match[3]])
		if path == "/" && quotedStandaloneSlash(command, match[2], match[3], quotedRanges) {
			continue
		}
		if path == "" || path == "." || strings.HasPrefix(path, "./-") || strings.Contains(path, "://") {
			continue
		}
		if runtime.GOOS == "windows" && strings.HasPrefix(path, "/") && len(path) > 1 && path[1] != '/' {
			continue
		}
		if blocked, reason, auditReason := g.checkWorkspacePath("exec", "command", path, cwd); blocked {
			return true, reason, auditReason
		}
	}
	return false, "", ""
}

func quotedStandaloneSlash(command string, start, end int, ranges []shellQuoteRange) bool {
	for _, item := range ranges {
		if start < item.start || end > item.end {
			continue
		}
		leftSpace := start == item.start || isShellSpace(command[start-1])
		rightSpace := end == item.end || isShellSpace(command[end])
		return leftSpace && rightSpace
	}
	return false
}

func isShellSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n'
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
