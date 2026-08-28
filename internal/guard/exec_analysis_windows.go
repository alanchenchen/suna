//go:build windows

package guard

import (
	"regexp"
	"strings"
)

// windowsAnalyzer 是 Windows 保守 fallback：Go 生态没有 cmd/powershell 的完整 parser
// （调研确认），因此复用现有正则提取路径/重定向，能力不低于现状。
// 永远返回 ok=true（部分可靠），保守策略由消费方按"无法完整解析"处理。
type windowsAnalyzer struct{}

// newExecAnalyzer 是唯一的平台判断点（Windows 侧）。
func newExecAnalyzer() ExecAnalyzer {
	return windowsAnalyzer{}
}

// Windows 同时接受原生 drive 路径和 shell 中常见的正斜杠相对路径。
var windowsPathTokenPattern = regexp.MustCompile(`(?:^|[\s=])["']?((?:~(?:[/\\]|$)|\.\.?(?:[/\\]|$)|[A-Za-z]:\\|[^"'\s;|&<>]+[/\\]\.\.?(?:[/\\]|$))[^"'\s;|&<>]*)`)

// PowerShell 丢弃输出惯用法（等价于 POSIX 的 /dev/null）。
var windowsNullPattern = regexp.MustCompile(`(?i)\$null`)

// Windows NUL 设备（> NUL、2>NUL），等价于 POSIX /dev/null，无文件副作用。
var windowsNULPattern = regexp.MustCompile(`(?i)^nul$`)

// windowsRedirectionPattern 提取重定向目标（与 POSIX 侧同构的正则近似）。
var windowsRedirectionPattern = regexp.MustCompile(`[<>]{1,2}\s*([^\s;|&]+)`)

func (windowsAnalyzer) Analyze(command, shell string) (ExecAnalysis, bool) {
	analysis := ExecAnalysis{}
	// 重定向提取：$null 视为丢弃输出，不参与 workspace 检查（消费方按 Target=="" 跳过）。
	for _, match := range windowsRedirectionPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		target := strings.Trim(match[1], `"'`)
		if target == "" || target == "&1" || target == "&2" || strings.HasPrefix(target, "&") {
			continue
		}
		if windowsNullPattern.MatchString(target) || windowsNULPattern.MatchString(target) {
			analysis.Redirects = append(analysis.Redirects, ExecRedirect{Op: "write", Target: ""})
			continue
		}
		analysis.Redirects = append(analysis.Redirects, ExecRedirect{Op: "write", Target: target})
	}
	// 路径提取：drive 路径与相对路径。
	for _, match := range windowsPathTokenPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		path := strings.Trim(match[1], `"'`)
		if path == "" || strings.Contains(path, "://") {
			continue
		}
		analysis.Commands = append(analysis.Commands, ExecCommand{Name: "", Args: []string{path}})
	}
	return analysis, true
}
