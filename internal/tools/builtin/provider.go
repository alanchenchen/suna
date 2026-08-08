package builtin

import (
	"context"

	"github.com/alanchenchen/suna/internal/tools"
)

// Provider 将 Suna 内置本地工具接入统一 tools.Manager。
type Provider struct {
	tools        []Tool
	byName       map[string]Tool
	execRegistry *execRegistry
}

func NewProvider(extra ...Tool) *Provider {
	registry := newExecRegistry()
	items := []Tool{
		EditFile{},
		Exec{registry: registry},
		FileSystem{},
		HTTP{},
		ListDir{},
		ReadFile{},
		Search{},
		WriteFile{},
	}
	items = append(items, extra...)
	byName := make(map[string]Tool, len(items))
	for _, item := range items {
		name := item.Spec().Name
		if _, exists := byName[name]; !exists {
			// 保持原有线性查找的 first-wins 语义，重复名称不能覆盖核心工具。
			byName[name] = item
		}
	}
	return &Provider{tools: items, byName: byName, execRegistry: registry}
}

func (p *Provider) Specs(ctx context.Context) ([]tools.Spec, error) {
	out := make([]tools.Spec, 0, len(p.tools))
	for _, item := range p.tools {
		out = append(out, item.Spec())
	}
	return out, nil
}

func (p *Provider) Execute(ctx context.Context, call tools.Call) (tools.Result, bool) {
	item, ok := p.byName[call.Name]
	if !ok {
		return tools.Result{}, false
	}
	return item.Execute(ctx, call.Params), true
}

// CleanupRun 只回收身份匹配的 run 作用域任务；空 BoundaryID 表示该 run 的全部边界。
func (p *Provider) CleanupRun(ctx context.Context, execCtx tools.ExecutionContext) error {
	if p.execRegistry == nil {
		return nil
	}
	return p.execRegistry.cleanup(ctx, "run", func(job *execJob) bool {
		return job.scope == execScopeRun &&
			job.owner.SessionID == execCtx.SessionID &&
			job.owner.RunID == execCtx.RunID &&
			(execCtx.BoundaryID == "" || job.owner.BoundaryID == execCtx.BoundaryID)
	})
}

// CleanupSession 回收 session 下所有 run 与 session 作用域任务。
func (p *Provider) CleanupSession(ctx context.Context, sessionID string) error {
	if p.execRegistry == nil {
		return nil
	}
	return p.execRegistry.cleanup(ctx, "session", func(job *execJob) bool { return job.owner.SessionID == sessionID })
}

// Close 回收 Provider 持有的全部后台进程。
func (p *Provider) Close(ctx context.Context) error {
	if p.execRegistry == nil {
		return nil
	}
	return p.execRegistry.close(ctx)
}
