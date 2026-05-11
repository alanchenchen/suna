package core

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

/*
buildSystemPrompt 通过模板渲染系统提示词。

这里集中拼装“长期状态”：运行环境、项目配置、语义记忆、召回的情景记忆、能力摘要。
Run 循环每轮调用前都会重新构建，确保异步记忆提取和能力加载能自然进入下一次模型请求。
*/
func (a *Agent) buildSystemPrompt(ctx context.Context) (string, error) {
	env := getEnvInfo()

	projectConfig := ""
	wd, _ := os.Getwd()
	for _, name := range []string{"SUNA.md", ".suna/AGENTS.md"} {
		if data, err := os.ReadFile(filepath.Join(wd, name)); err == nil {
			projectConfig = string(data)
			break
		}
	}

	userPrefs := ""
	if summary, err := a.semantic.Summary(ctx); err == nil && summary != "" {
		userPrefs = summary
	}

	recalledMemories := ""
	if a.episodic != nil {
		lastUserInput := a.working.LastUserText()
		if lastUserInput != "" {
			memories, _ := a.episodic.Recall(ctx, lastUserInput, 3)
			if len(memories) > 0 {
				var lines []string
				for _, m := range memories {
					lines = append(lines, fmt.Sprintf("- [%s] %s", m.Timestamp.Format("2006-01-02"), m.Content))
				}
				recalledMemories = strings.Join(lines, "\n")
			}
		}
	}

	capabilities := ""
	if a.caps != nil {
		capabilities = a.caps.Summary()
	}

	return a.prompts.RenderSystem(prompt.SystemPromptData{
		OS:               env["OS"],
		Arch:             env["Arch"],
		WorkDir:          env["WorkDir"],
		User:             env["User"],
		Time:             env["Time"],
		ProjectConfig:    projectConfig,
		UserPreferences:  userPrefs,
		RecalledMemories: recalledMemories,
		Capabilities:     capabilities,
	})
}

// buildToolDefs 构建 LLM tool calling 定义。AskUser 和 Spawn 是 core 内建工具，动态追加。
func (a *Agent) buildToolDefs() []model.ToolDef {
	tools := a.registry.All()
	defs := make([]model.ToolDef, 0, len(tools)+2)

	for _, t := range tools {
		defs = append(defs, model.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  withIntentParameter(t.Parameters()),
		})
	}

	defs = append(defs, model.ToolDef{
		Name:        "askuser",
		Description: "Ask the user a question and wait for their reply",
		Parameters: withIntentParameter(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "description": "Question to ask"},
				"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Options"},
			},
			"required": []string{"question"},
		}),
	})

	defs = append(defs, model.ToolDef{
		Name:        "spawn",
		Description: "Create a sub agent to execute a sub-task",
		Parameters: withIntentParameter(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task":    map[string]any{"type": "string", "description": "Sub-task description"},
				"model":   map[string]any{"type": "string", "description": "Model to use"},
				"system":  map[string]any{"type": "string", "description": "Sub agent system prompt"},
				"tools":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Available tools"},
				"timeout": map[string]any{"type": "integer", "description": "Timeout seconds"},
				"context": map[string]any{"type": "string", "description": "Extra context"},
			},
			"required": []string{"task"},
		}),
	})

	return defs
}

func withIntentParameter(params map[string]any) map[string]any {
	props, ok := params["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		params["properties"] = props
	}
	props["intent"] = map[string]any{
		"type":        "string",
		"description": "Natural-language reason for this tool call. Explain what you are trying to accomplish for the user. Do not put file contents, secrets, or raw parameters here.",
	}
	return params
}

func getEnvInfo() map[string]string {
	wd, _ := os.Getwd()
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	return map[string]string{
		"OS":      runtime.GOOS,
		"Arch":    runtime.GOARCH,
		"WorkDir": wd,
		"User":    username,
		"Time":    time.Now().Format("2006-01-02 15:04:05"),
	}
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
