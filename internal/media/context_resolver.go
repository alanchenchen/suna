package media

import (
	"context"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

// ContextResolver 优先使用工具执行上下文中的 session 附件目录解析 attachment。
type ContextResolver struct {
	Fallback *Store
}

func NewContextResolver(root string) *ContextResolver {
	return &ContextResolver{Fallback: NewStore(root)}
}

func (r *ContextResolver) Resolve(ctx context.Context, ref model.MediaRef, mode model.ResolveMode) (model.ResolvedMedia, error) {
	if execCtx, ok := tools.ExecutionContextFrom(ctx); ok && execCtx.AttachmentDir != "" {
		return NewStore(execCtx.AttachmentDir).Resolve(ctx, ref, mode)
	}
	if r == nil || r.Fallback == nil {
		fallback := &Store{}
		return fallback.Resolve(ctx, ref, mode)
	}
	return r.Fallback.Resolve(ctx, ref, mode)
}
