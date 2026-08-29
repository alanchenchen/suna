package guard

// ExecAnalysis 是单次 exec 命令解析的结构化结果，risk 判断与 workspace 检查共享消费。
// 一次解析两处使用，消除旧的"risk 用分词器、workspace 用正则"两套独立解析的不一致。
type ExecAnalysis struct {
	Commands    []ExecCommand  // 每个简单命令：名称 + 参数 + 只读标记
	Redirects   []ExecRedirect // 每个重定向：目标 + 操作 + fd
	DynamicCmds []string       // 动态表达式内部的命令名（$(git ...) → ["git"]）
	HasDynamic  bool           // 是否含动态表达式（$()、反引号等）
	Pipelines   [][]string     // 管道链：每条管道按顺序的命令名（curl x | sh → ["curl", "sh"]）
	ParseFailed bool           // 解析失败（语法错误）→ 调用方保守处理
}

// ExecCommand 表示一个简单命令（AST CallExpr 或保守分词的近似）。
type ExecCommand struct {
	Name string   // 命令名（AST 精确提取，引号内文本不是命令）
	Args []string // 参数（含路径候选）
}

// ExecRedirect 表示一个重定向。
type ExecRedirect struct {
	Target string // 重定向目标（引号/变量已展开或保守保留）
	Op     string // "write" / "append" / "read" / "fd"
	FD     int    // 文件描述符（2> 是 2；fd 重定向如 2>&1 的 Op 为 "fd"）
}

// ExecAnalyzer 由平台实现：POSIX 用 mvdan.cc/sh AST，Windows 保守 fallback。
// ok=false 表示无法可靠解析（调用方保守处理，安全方向）。
type ExecAnalyzer interface {
	Analyze(command, shell string) (ExecAnalysis, bool)
}
