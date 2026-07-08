package tools

import "context"

type executionContextKey struct{}

// ExecutionContext 承载一次工具执行所属的 session 运行上下文。
type ExecutionContext struct {
	SessionID     string
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
