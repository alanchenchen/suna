package agent

// RunErrorKind 描述一次 Agent run 在请求模型前无法继续的稳定原因。
// 它只表达任务运行前置条件，不能承载上游 provider 的请求失败。
type RunErrorKind string

const (
	RunErrorNoModelConfigured       RunErrorKind = "no_model_configured"
	RunErrorSessionModelUnavailable RunErrorKind = "session_model_unavailable"
)

// RunError 仅保存 UI/SDK 恢复所需的机器可读上下文。
type RunError struct {
	Kind     RunErrorKind
	ModelRef string
}
