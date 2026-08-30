package guard

import (
	"regexp"
	"strings"
)

// execQuotedTildePathPattern 匹配引号内的 ~ 开头路径（sh -c "cat ~/.ssh/id_rsa"）。
// 敏感检查专用：workspace 检查不拦 ~ 展开后的 home 路径，敏感数据与 workspace 无关。
var execQuotedTildePathPattern = regexp.MustCompile(`["'][^"']*\s(~[^"'\s][^"'\s]*)`)

// execQuotedContentPattern 匹配引号内完整内容（sh -c "cat .env" → cat .env），
// 供解释器场景分词后检查敏感文件名。
var execQuotedContentPattern = regexp.MustCompile(`["']([^"']*)["']`)

// execSensitivePaths 提取 exec 命令中的路径候选，供敏感文件检查使用。
// exec 是敏感文件检查的绕过口（readfile 拦截但 exec cat 放行），
// 这里复用与 workspace 检查同一套路径提取逻辑（isPathCandidate / 引号内正则），
// 避免两套提取不一致。覆盖三路径：AST 参数、解释器引号内路径、Windows fallback 正则。
// 与 workspace 检查不同：敏感检查不要求路径候选格式（相对路径 .env 也查），
// 因为敏感规则匹配文件名（.env/.pem/.npmrc 等），不依赖绝对路径。
// analysis 由 Check 主流程传入（一次解析多处消费）；nil 时自行解析（独立调用入口）。
func (g *Guard) execSensitivePaths(params map[string]any, analysis *ExecAnalysis) []string {
	command, _ := params["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	shell, _ := params["shell"].(string)
	var paths []string
	if analysis == nil {
		if a, ok := newExecAnalyzer().Analyze(command, shell); ok {
			analysis = &a
		}
	}
	if analysis != nil && !analysis.ParseFailed {
		// AST 参数中的路径候选（cat ~/.ssh/id_rsa、cat .env）。
		for _, cmd := range analysis.Commands {
			for _, arg := range cmd.Args {
				if isPathCandidate(arg) || isSensitiveFileName(arg) {
					paths = append(paths, arg)
				}
			}
		}
		// 解释器场景（sh -c "cat ~/.ssh/id_rsa"）的引号内路径与内容分词。
		if analysisHasInterpreter(*analysis) {
			paths = append(paths, execQuotedSensitivePaths(command)...)
			paths = append(paths, execQuotedContentSensitivePaths(command)...)
		}
		// 重定向目标（cat f > ~/.ssh/out）。
		for _, r := range analysis.Redirects {
			if r.Op == "fd" || r.Target == "" || r.Target == "/dev/null" {
				continue
			}
			paths = append(paths, r.Target)
		}
		return paths
	}
	// fallback：解析失败（Windows 保守解析）时用正则提取，能力不低于旧实现。
	// 引号内路径与内容分词只在解释器场景启用（echo "~/.ssh/id_rsa" 的引号内是文本，不执行），
	// 与 POSIX 分支的 analysisHasInterpreter 保护语义一致。
	if fallbackHasInterpreter(command) {
		paths = append(paths, execQuotedSensitivePaths(command)...)
		paths = append(paths, execQuotedContentSensitivePaths(command)...)
	}
	// 裸文件名（type .env）不匹配 execPathTokenPattern（要求路径边界开头），
	// 用分词器提取字段后按敏感文件名检查，避免 Windows fallback 漏掉。
	if fields, ok := shellFields(command); ok {
		for _, field := range fields {
			if isSensitiveFileName(field) {
				paths = append(paths, field)
			}
		}
	}
	for _, match := range execPathTokenPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		path := trimShellPathToken(match[1])
		if path == "" || path == "." || strings.HasPrefix(path, "./-") || strings.Contains(path, "://") {
			continue
		}
		paths = append(paths, path)
	}
	for _, match := range execRedirectionPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		path := trimShellPathToken(match[1])
		if path == "" || isShellDescriptor(path) {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// isSensitiveFileName 判断参数是否直接是敏感文件名（.env、.pem、.npmrc 等）。
// 要求参数本身是单个文件名（不含空格）：cat .env 是明确的文件访问，
// 而 echo "not .env file" 的引号文本只是打印内容，不应误拦。
// 敏感规则按文件名匹配，相对路径参数（cat .env）也应纳入检查。
func isSensitiveFileName(arg string) bool {
	if arg == "" || strings.HasPrefix(arg, "-") || strings.Contains(arg, "$") {
		return false
	}
	// 含空格/引号的参数是文本或复合表达式，不是文件名。
	if strings.ContainsAny(arg, " \t'\"") {
		return false
	}
	base := arg
	if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
		base = base[idx+1:]
	}
	if base == "" || base == "." || base == ".." {
		return false
	}
	sensitive, _ := IsSensitivePath(base)
	return sensitive
}

// execQuotedSensitivePaths 提取引号内路径（解释器场景，与 workspace 检查同构）。
// 额外覆盖 ~ 开头路径（sh -c "cat ~/.ssh/id_rsa"），workspace 检查不拦 ~ 展开后的 home 路径，
// 但敏感检查必须独立覆盖（敏感数据与 workspace 无关）。
// 注意：引号内容分词（execQuotedContentSensitivePaths）不在此处，
// 由调用方在确认解释器场景后单独调用，避免 echo "cat .env" 文本误拦。
func execQuotedSensitivePaths(command string) []string {
	var paths []string
	for _, match := range execQuotedAbsPathPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		paths = append(paths, match[1])
	}
	for _, match := range execQuotedPathTokenPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		paths = append(paths, match[1])
	}
	for _, match := range execQuotedTildePathPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		paths = append(paths, match[1])
	}
	for _, match := range execQuotedStandaloneSlashPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		// 独立斜杠不是敏感路径，跳过减少噪音。
		if match[1] != "/" {
			paths = append(paths, match[1])
		}
	}
	return paths
}

// execQuotedContentSensitivePaths 提取引号内内容分词后的敏感文件名
// （sh -c "cat .env" → .env）。只在解释器场景调用：解释器会执行引号内容，
// 而 echo "cat .env" 的引号内只是文本，不应误拦。
func execQuotedContentSensitivePaths(command string) []string {
	var paths []string
	for _, match := range execQuotedContentPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		if fields, ok := shellFields(match[1]); ok {
			for _, field := range fields {
				if isSensitiveFileName(field) {
					paths = append(paths, field)
				}
			}
		}
	}
	return paths
}

// fallbackHasInterpreter 判断命令是否含解释器（Windows fallback 场景，
// 无 AST 可用时用分词器近似判断）。与 analysisHasInterpreter 的命令名列表一致。
func fallbackHasInterpreter(command string) bool {
	fields, ok := shellFields(command)
	if !ok {
		return false
	}
	for _, field := range fields {
		name := strings.ToLower(strings.TrimSpace(field))
		if idx := strings.LastIndexAny(name, "/\\"); idx >= 0 {
			name = name[idx+1:]
		}
		switch name {
		case "sh", "bash", "zsh", "fish", "cmd", "powershell", "pwsh",
			"eval", "source", "python", "python3", "node", "ruby", "perl", "php":
			return true
		}
	}
	return false
}
