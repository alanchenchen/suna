package builtin

import (
	"context"

	"github.com/alanchenchen/suna/internal/tools"
)

// Tool 是内置本地工具的最小实现接口。
// 定义和执行分离到统一 tools.Spec/tools.Result，避免 builtin 包再维护一套平行类型。
type Tool interface {
	Spec() tools.Spec
	Execute(ctx context.Context, params map[string]any) tools.Result
}

func builtinSpec(name, description string, category tools.Category, parameters map[string]any) tools.Spec {
	return tools.Spec{
		Name:        name,
		Description: description,
		Parameters:  parameters,
		Category:    category,
		Source:      tools.Source{Kind: tools.SourceBuiltin, ID: "core"},
	}
}
