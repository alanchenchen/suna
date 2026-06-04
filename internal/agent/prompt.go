package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func (a *Agent) buildSystemPrompt(ctx context.Context) (string, error) {
	env := getEnvInfo()
	projectConfig := ""
	wd, _ := os.Getwd()
	if data, err := os.ReadFile(filepath.Join(wd, "AGENTS.md")); err == nil {
		projectConfig = string(data)
	}
	skills := ""
	if a.skills != nil {
		skills = a.skills.Summary()
	}
	return a.prompts.RenderSystem(prompt.SystemPromptData{
		OS: env["OS"], Arch: env["Arch"], WorkDir: env["WorkDir"], ActiveModel: a.activeModelSummary(),
		ModelRouting: a.modelRoutingSummary(), ProjectConfig: projectConfig, Skills: skills, SkillsDir: a.cfg.SkillsDir(),
	})
}

func (a *Agent) buildRequestMessages(ctx context.Context) []model.Message {
	msgs := a.working.Messages()
	if a.memories == nil {
		return msgs
	}
	brief, _, _ := a.memories.BuildBrief(ctx, memory.DefaultUserID, a.working.LastUserText())
	if strings.TrimSpace(brief) == "" {
		return msgs
	}
	contextBlock := "<internal_context>\n" +
		"This block is internal background context, not a user request.\n" +
		"Use it only when relevant. Current user instructions override this context.\n\n" +
		"<active_memory>\n" + brief + "\n</active_memory>\n" +
		"</internal_context>"
	out := make([]model.Message, 0, len(msgs)+1)
	insert := latestUserMessageIndex(msgs)
	if insert < 0 {
		out = append(out, model.NewTextMessage(model.RoleUser, contextBlock))
		out = append(out, msgs...)
		return out
	}
	out = append(out, msgs[:insert]...)
	out = append(out, model.NewTextMessage(model.RoleUser, contextBlock))
	out = append(out, msgs[insert:]...)
	return out
}

func latestUserMessageIndex(msgs []model.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser {
			return i
		}
	}
	return -1
}

func (a *Agent) activeModelSummary() string {
	if a.router == nil {
		return "none configured"
	}
	return a.router.ActiveRef()
}

func (a *Agent) modelRoutingSummary() string {
	if a.router == nil {
		return "- No models configured. Configure a model before using spawn."
	}
	refs := a.router.ListProviders()
	sort.Strings(refs)
	if len(refs) == 0 {
		return "- No models configured. Configure a model before using spawn."
	}
	lines := make([]string, 0, len(refs))
	for _, ref := range refs {
		mc, err := a.router.ModelConfig(ref)
		if err != nil || mc == nil {
			lines = append(lines, fmt.Sprintf("- %s", ref))
			continue
		}
		var attrs []string
		if len(mc.Strengths) > 0 {
			attrs = append(attrs, strings.Join(mc.Strengths, ", "))
		}
		if mc.ContextWindow > 0 {
			attrs = append(attrs, fmt.Sprintf("ctx %s", formatContextWindow(mc.ContextWindow)))
		}
		if len(attrs) == 0 {
			lines = append(lines, fmt.Sprintf("- %s", ref))
		} else {
			lines = append(lines, fmt.Sprintf("- %s: %s", ref, strings.Join(attrs, "; ")))
		}
	}
	return strings.Join(lines, "\n")
}

func (a *Agent) buildToolDefs() []model.ToolDef {
	tools := a.registry.All()
	defs := make([]model.ToolDef, 0, len(tools)+3)
	for _, t := range tools {
		defs = append(defs, model.ToolDef{Name: t.Name(), Description: t.Description(), Parameters: withIntentParameter(t.Parameters())})
	}
	if a.skills != nil {
		for _, def := range a.skills.ToolDefs(withIntentParameter) {
			defs = append(defs, model.ToolDef{Name: def.Name, Description: def.Description, Parameters: def.Parameters})
		}
	}
	defs = append(defs, model.ToolDef{Name: "askuser", Description: "Ask the user for missing information or a decision. Provide several concise options when helpful. Keep allow_custom=true or omit it for normal questions so the user can type freely; use allow_custom=false only for strict system/workflow confirmations that must choose one provided option.", Parameters: withIntentParameter(map[string]any{
		"type": "object", "properties": map[string]any{"question": map[string]any{"type": "string", "description": "Question to ask the user"}, "options": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional quick-pick answers for the user"}, "allow_custom": map[string]any{"type": "boolean", "description": "Whether the user may type a custom answer. Default true."}}, "required": []string{"question"},
	})})
	spawnToolNames := a.availableSpawnTools()
	defs = append(defs, model.ToolDef{Name: "spawn", Description: "Delegate an isolated subtask to a selected model. It sees only task/context, allowed tools, and images explicitly passed with input_images; it does not inherit main history or images.", Parameters: withIntentParameter(map[string]any{
		"type": "object", "properties": map[string]any{"task": map[string]any{"type": "string", "description": "Self-contained task for the subtask"}, "model": map[string]any{"type": "string", "description": "Exact model ref from Available subtask models"}, "system": map[string]any{"type": "string", "description": "Optional subtask system prompt"}, "tools": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": spawnToolNames}, "description": "Allowed tools for the isolated subtask; use [] for model-only tasks"}, "input_images": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Indexes of images attached to the current user message only, e.g. [0]. Not prior-turn/restored image summaries; spawn does not inherit images unless listed."}, "timeout": map[string]any{"type": "integer", "description": "Entire subtask timeout in seconds; default 300."}, "context": map[string]any{"type": "string", "description": "Extra context"}}, "required": []string{"task", "model", "tools"},
	})})
	return defs
}

func withIntentParameter(params map[string]any) map[string]any {
	props, ok := params["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		params["properties"] = props
	}
	props["intent"] = map[string]any{"type": "string", "description": "Natural-language reason for this tool call. Explain what you are trying to accomplish for the user. Do not put file contents, secrets, or raw parameters here."}
	return params
}

func getEnvInfo() map[string]string {
	wd, _ := os.Getwd()
	return map[string]string{"OS": runtime.GOOS, "Arch": runtime.GOARCH, "WorkDir": wd}
}

func resolveModelID(cfg *config.Config, modelName string) string {
	if mc, ok := cfg.ModelByRef(modelName); ok {
		return mc.Model
	}
	if mc, ok := cfg.ActiveModelConfig(); ok {
		return mc.Model
	}
	return modelName
}

func formatContextWindow(n int) string {
	if n >= 1000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
