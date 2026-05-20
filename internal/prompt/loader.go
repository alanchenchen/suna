package prompt

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed templates/*.md
var templatesFS embed.FS

type Loader struct {
	templates map[string]*template.Template
}

func New() (*Loader, error) {
	l := &Loader{
		templates: make(map[string]*template.Template),
	}
	files := []string{
		"system", "guard", "guard_review", "compress", "extract",
		"extract_batch", "spawn_system",
	}
	for _, name := range files {
		data, err := templatesFS.ReadFile("templates/" + name + ".md")
		if err != nil {
			return nil, err
		}
		tmpl, err := template.New(name).Parse(string(data))
		if err != nil {
			return nil, err
		}
		l.templates[name] = tmpl
	}
	return l, nil
}

func (l *Loader) Render(name string, data map[string]any) (string, error) {
	tmpl, ok := l.templates[name]
	if !ok {
		return "", nil
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (l *Loader) RenderSystem(data SystemPromptData) (string, error) {
	return l.Render("system", map[string]any{
		"OS":               data.OS,
		"Arch":             data.Arch,
		"WorkDir":          data.WorkDir,
		"ActiveModel":      data.ActiveModel,
		"ModelRouting":     data.ModelRouting,
		"ProjectConfig":    data.ProjectConfig,
		"UserPreferences":  data.UserPreferences,
		"RecalledMemories": data.RecalledMemories,
		"Capabilities":     data.Capabilities,
	})
}

func (l *Loader) RenderCompress(content string) (string, error) {
	return l.Render("compress", map[string]any{
		"Content": content,
	})
}

func (l *Loader) RenderExtract(userInput, agentOutput string) (string, error) {
	return l.Render("extract", map[string]any{
		"UserInput":   userInput,
		"AgentOutput": agentOutput,
	})
}

func (l *Loader) RenderGuardReview(data GuardReviewData) (string, error) {
	return l.Render("guard_review", map[string]any{
		"ToolName":      data.ToolName,
		"ToolParams":    data.ToolParams,
		"Target":        data.Target,
		"RecentContext": data.RecentContext,
	})
}

func (l *Loader) RenderExtractBatch(interactions []ExtractInteraction) (string, error) {
	return l.Render("extract_batch", map[string]any{
		"Interactions": interactions,
	})
}

func (l *Loader) RenderSpawnSystem(data SpawnPromptData) (string, error) {
	return l.Render("spawn_system", map[string]any{
		"Task":    data.Task,
		"Tools":   data.Tools,
		"Context": data.Context,
		"OS":      data.OS,
		"Arch":    data.Arch,
		"WorkDir": data.WorkDir,
	})
}

type SystemPromptData struct {
	OS               string
	Arch             string
	WorkDir          string
	ActiveModel      string
	ModelRouting     string
	ProjectConfig    string
	UserPreferences  string
	RecalledMemories string
	Capabilities     string
}

type GuardReviewData struct {
	ToolName      string
	ToolParams    string
	Target        string
	RecentContext string
}

type SpawnPromptData struct {
	Task    string
	Tools   string
	Context string
	OS      string
	Arch    string
	WorkDir string
}

type ExtractInteraction struct {
	Index       int
	UserInput   string
	AgentOutput string
}
