package skilltools

import (
	"context"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/skill"
	"github.com/alanchenchen/suna/internal/tools"
)

const (
	ToolLoad  = "skill_load"
	ToolStart = "skill_start"
)

// Provider 只负责把 Skill Runtime 适配成统一工具来源；Skill 的领域逻辑仍留在 internal/skill。
type Provider struct {
	runtime *skill.Runtime
}

func NewProvider(runtime *skill.Runtime) *Provider {
	return &Provider{runtime: runtime}
}

func (p *Provider) Specs(ctx context.Context) ([]tools.Spec, error) {
	return []tools.Spec{
		{
			Name:        ToolLoad,
			Description: "Load full details for an enabled skill. Use only when you need the skill's full instructions; do not use just to list or summarize available skills.",
			Parameters:  loadParameters(),
			Category:    tools.Perceive,
			Source:      tools.Source{Kind: tools.SourceSkill, ID: "runtime"},
			Guard:       tools.GuardNever,
		},
		{
			Name:        ToolStart,
			Description: "Start the built-in Skill verification workflow. Use import to import a Skill source, or check after you prepared files under the skills directory. The workflow runs static check, asks the user whether to run LLM review, and asks whether to enable.",
			Parameters:  startParameters(),
			Category:    tools.Act,
			Source:      tools.Source{Kind: tools.SourceSkill, ID: "runtime"},
			Guard:       tools.GuardNever,
		},
	}, nil
}

func (p *Provider) Execute(ctx context.Context, call tools.Call) (tools.Result, bool) {
	if p == nil || p.runtime == nil {
		return tools.ErrorResult("skill runtime is not initialized"), call.Name == ToolLoad || call.Name == ToolStart
	}
	switch call.Name {
	case ToolLoad:
		return p.executeLoad(call.Params), true
	case ToolStart:
		return p.executeStart(ctx, call.Params), true
	default:
		return tools.Result{}, false
	}
}

func (p *Provider) Close(ctx context.Context) error { return nil }

func (p *Provider) executeLoad(params map[string]any) tools.Result {
	name, _ := params["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return tools.ErrorResult("name is required")
	}
	content, err := p.runtime.LoadContent(name)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	res := tools.TextResult(fmt.Sprintf("[Skill: %s]\n%s", name, content))
	res.Metadata = map[string]any{"skill_name": name}
	return res
}

func (p *Provider) executeStart(ctx context.Context, params map[string]any) tools.Result {
	res, err := p.runtime.Start(ctx, params)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	return tools.TextResult(skill.StartJSONResult(res))
}

func loadParameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string", "description": "Exact skill name from Available Skills"}}, "required": []string{"name"}}
}

func startParameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"import", "check"}, "description": "Skill workflow action. Use import for a source path/URL, check after you prepared files under the skills directory."},
		"name":   map[string]any{"type": "string", "description": "Skill name. Required for check; optional for import."},
		"source": map[string]any{"type": "string", "description": "Local directory path, zip path, or git/http/ssh URL for import."},
	}, "required": []string{"action"}}
}

func ParamKeys(name string) map[string]bool {
	switch name {
	case ToolLoad:
		return map[string]bool{"name": true}
	case ToolStart:
		return map[string]bool{"action": true, "name": true, "source": true}
	default:
		return nil
	}
}

func LoadNotificationFromResult(toolName string, params map[string]any, result tools.Result) (string, bool) {
	if toolName != ToolLoad || result.IsError {
		return "", false
	}
	name, _ := result.Metadata["skill_name"].(string)
	if name == "" {
		name, _ = params["name"].(string)
	}
	name = strings.TrimSpace(name)
	return name, name != ""
}
