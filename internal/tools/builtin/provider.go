package builtin

import (
	"context"

	"github.com/alanchenchen/suna/internal/tools"
)

// Provider 将 Suna 内置本地工具接入统一 tools.Manager。
type Provider struct {
	tools []Tool
}

func NewProvider(extra ...Tool) *Provider {
	items := []Tool{
		EditFile{},
		Exec{},
		FileSystem{},
		HTTP{},
		ListDir{},
		ReadFile{},
		Search{},
		WriteFile{},
	}
	items = append(items, extra...)
	return &Provider{tools: items}
}

func (p *Provider) Specs(ctx context.Context) ([]tools.Spec, error) {
	out := make([]tools.Spec, 0, len(p.tools))
	for _, item := range p.tools {
		out = append(out, item.Spec())
	}
	return out, nil
}

func (p *Provider) Execute(ctx context.Context, call tools.Call) (tools.Result, bool) {
	for _, item := range p.tools {
		if item.Spec().Name == call.Name {
			return item.Execute(ctx, call.Params), true
		}
	}
	return tools.Result{}, false
}

func (p *Provider) Close(ctx context.Context) error { return nil }
