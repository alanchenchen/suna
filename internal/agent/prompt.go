package agent

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func (a *Agent) buildSystemPrompt(ctx context.Context) (string, error) {
	env := getEnvInfoForWorkDir(a.cwd)
	projectConfig := projectInstructions{}
	wd := a.cwd
	if strings.TrimSpace(wd) == "" {
		wd, _ = os.Getwd()
	}
	projectConfig = loadProjectInstructions(wd)
	skills := ""
	if a.skills != nil {
		skills = a.skills.Summary()
	}
	return a.prompts.RenderSystem(prompt.SystemPromptData{
		OS: env["OS"], Arch: env["Arch"], WorkDir: wd, Workspace: a.cfg.Guard.Workspace, DataDir: a.cfg.DataDir, ActiveModel: a.activeModelSummary(),
		ModelRouting: a.modelRoutingSummary(), ProjectConfig: projectConfig.Content, ProjectConfigSource: projectConfig.Source, Skills: skills, SkillsDir: a.cfg.SkillsDir(),
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
	return a.modelRef
}

func (a *Agent) modelRoutingSummary() string {
	if a.router == nil {
		return "- No models configured. Configure a model before using spawn."
	}
	refs := a.router.ListSpawnableModels(a.modelRef)
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
	if a.tools == nil {
		return nil
	}
	return a.tools.ToolDefs(withIntentParameter)
}

func withIntentParameter(params map[string]any) map[string]any {
	addIntentToObjectSchema(params)
	return params
}

func addIntentToObjectSchema(schema map[string]any) {
	if props, ok := schema["properties"].(map[string]any); ok {
		props["intent"] = toolIntentSchema()
	}
	// 封闭的组合分支也必须声明 intent，否则模型按 schema 传入 intent 时没有合法分支。
	for _, keyword := range []string{"oneOf", "anyOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			if child, ok := branch.(map[string]any); ok {
				addIntentToObjectSchema(child)
			}
		}
	}
}

func toolIntentSchema() map[string]any {
	return map[string]any{"type": "string", "description": "Brief user-facing reason for this tool call. Use the language of the user's current request. Do not include file contents, secrets, or raw parameters."}
}

func getEnvInfoForWorkDir(wd string) map[string]string {
	if strings.TrimSpace(wd) == "" {
		wd, _ = os.Getwd()
	}
	return map[string]string{"OS": runtime.GOOS, "Arch": runtime.GOARCH, "WorkDir": wd}
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
