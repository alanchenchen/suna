//go:build windows

package guard

// windowsAnalyzer 是 Windows 保守 fallback：Go 生态没有 cmd/powershell 的完整 parser
// （调研确认），因此用保守分段提取命令名与重定向，能力不低于现状。
// 永远返回 ok=true（部分可靠），保守策略由消费方按"无法完整解析"处理。
type windowsAnalyzer struct{}

// newExecAnalyzer 是唯一的平台判断点（Windows 侧）。
func newExecAnalyzer() ExecAnalyzer {
	return windowsAnalyzer{}
}

func (windowsAnalyzer) Analyze(command, shell string) (ExecAnalysis, bool) {
	return windowsAnalyze(command), true
}
