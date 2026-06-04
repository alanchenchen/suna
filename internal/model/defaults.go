package model

const (
	// DefaultContextWindow 是模型未显式配置 context_window 时的运行时默认上下文窗口。
	// Suna 不维护完整模型能力库，因此使用较保守且适合当前主流模型的统一默认值。
	DefaultContextWindow = 200000

	// DefaultMaxTokens 是未显式指定 MaxTokens 时的默认输出预算。
	// Runner 会在压缩预算计算前解析该值，Provider 保留同一兜底以支持直接调用。
	DefaultMaxTokens = 8192
)

// ResolveMaxTokens 统一解析请求输出预算，避免 runner/provider 各自维护默认值。
func ResolveMaxTokens(n int) int {
	if n > 0 {
		return n
	}
	return DefaultMaxTokens
}
