package tools

import "context"

type executionContextKey struct{}

// ExecutionContext 承载一次工具执行所属的 session、run 与执行边界。
// RunID 用于回收当前轮产生的短生命周期资源；BoundaryID 隔离主任务和各个子任务。
type ExecutionContext struct {
	SessionID     string
	RunID         string
	BoundaryID    string
	CWD           string
	AttachmentDir string
}

func WithExecutionContext(ctx context.Context, execCtx ExecutionContext) context.Context {
	return context.WithValue(ctx, executionContextKey{}, execCtx)
}

func ExecutionContextFrom(ctx context.Context) (ExecutionContext, bool) {
	v, ok := ctx.Value(executionContextKey{}).(ExecutionContext)
	return v, ok
}

// MergeExecutionContext 只用非空字段覆盖已有执行上下文，避免 Agent 补充路径时丢失 daemon 注入的 run 身份。
func MergeExecutionContext(ctx context.Context, update ExecutionContext) context.Context {
	current, _ := ExecutionContextFrom(ctx)
	if update.SessionID != "" {
		current.SessionID = update.SessionID
	}
	if update.RunID != "" {
		current.RunID = update.RunID
	}
	if update.BoundaryID != "" {
		current.BoundaryID = update.BoundaryID
	}
	if update.CWD != "" {
		current.CWD = update.CWD
	}
	if update.AttachmentDir != "" {
		current.AttachmentDir = update.AttachmentDir
	}
	return WithExecutionContext(ctx, current)
}
