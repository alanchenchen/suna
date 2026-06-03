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
		"system", "guard", "guard_review", "skill_review", "compress", "extract_batch", "subtask_system",
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
		"OS":            data.OS,
		"Arch":          data.Arch,
		"WorkDir":       data.WorkDir,
		"ActiveModel":   data.ActiveModel,
		"ModelRouting":  data.ModelRouting,
		"ProjectConfig": data.ProjectConfig,
		"Skills":        data.Skills,
		"SkillsDir":     data.SkillsDir,
	})
}

func (l *Loader) RenderCompress(content string) (string, error) {
	return l.Render("compress", map[string]any{
		"Content": content,
	})
}

func (l *Loader) RenderGuardReview(data GuardReviewData) (string, error) {
	return l.Render("guard_review", map[string]any{
		"ToolName":         data.ToolName,
		"ToolParams":       data.ToolParams,
		"Target":           data.Target,
		"Risk":             data.Risk,
		"UserRequest":      data.UserRequest,
		"ToolIntent":       data.ToolIntent,
		"AssistantContext": data.AssistantContext,
		"RecentContext":    data.RecentContext,
	})
}

func (l *Loader) RenderSkillReview(data SkillReviewData) (string, error) {
	return l.Render("skill_review", map[string]any{
		"Name":        data.Name,
		"Description": data.Description,
		"Reasons":     data.Reasons,
		"Files":       data.Files,
		"UserRequest": data.UserRequest,
	})
}

func (l *Loader) RenderExtractBatch(interactions []ExtractInteraction) (string, error) {
	return l.Render("extract_batch", map[string]any{
		"Interactions": interactions,
	})
}

func (l *Loader) RenderMemoryCompact(data map[string]any) (string, error) {
	return l.Render("extract_batch", map[string]any{
		"MaxMemories": data["max_memories"],
		"MaxCore":     data["max_core"],
		"InputJSON":   data["input_json"],
	})
}

func (l *Loader) RenderSubtaskSystem(data SubtaskPromptData) (string, error) {
	return l.Render("subtask_system", map[string]any{
		"Task":    data.Task,
		"Tools":   data.Tools,
		"Context": data.Context,
		"OS":      data.OS,
		"Arch":    data.Arch,
		"WorkDir": data.WorkDir,
	})
}

type SystemPromptData struct {
	OS            string
	Arch          string
	WorkDir       string
	ActiveModel   string
	ModelRouting  string
	ProjectConfig string
	Skills        string
	SkillsDir     string
}

type GuardReviewData struct {
	ToolName         string
	ToolParams       string
	Target           string
	Risk             string
	UserRequest      string
	ToolIntent       string
	AssistantContext string
	RecentContext    string
}

type SubtaskPromptData struct {
	Task    string
	Tools   string
	Context string
	OS      string
	Arch    string
	WorkDir string
}

type SkillReviewData struct {
	Name        string
	Description string
	Reasons     []string
	Files       []SkillReviewFile
	UserRequest string
}

type SkillReviewFile struct {
	Path      string
	Content   string
	Truncated bool
}

type ExtractInteraction struct {
	Index       int
	UserInput   string
	AgentOutput string
}
