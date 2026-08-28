//go:build windows

package guard

// windowsAnalyzer 是 Windows 保守 fallback：Go 生态没有 cmd/powershell 的完整 parser
// （调研确认）。返回 ok=false 让消费方走旧的分词器/正则路径（Windows CI 验证过的行为），
// 不引入手写分段逻辑——引号内分隔符、fd 重定向、复合分隔符等边界无穷，无法穷尽。
type windowsAnalyzer struct{}

// newExecAnalyzer 是唯一的平台判断点（Windows 侧）。
func newExecAnalyzer() ExecAnalyzer {
	return windowsAnalyzer{}
}

func (windowsAnalyzer) Analyze(command, shell string) (ExecAnalysis, bool) {
	return ExecAnalysis{ParseFailed: true}, false
}
