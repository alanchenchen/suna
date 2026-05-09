package prompt

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed templates/*.md
var templatesFS embed.FS

// Loader 提示词模板加载器。
// 模板通过 go:embed 编译进二进制，用户不可覆盖。
// 系统提示词是内核行为规范，改坏会失控；用户有 SUNA.md / 语义记忆 / capability 三层定制入口。
type Loader struct {
	templates map[string]*template.Template
}

// New 创建 Loader 并预解析所有内嵌模板
func New() (*Loader, error) {
	l := &Loader{
		templates: make(map[string]*template.Template),
	}
	files := []string{"system", "guard", "compress", "extract"}
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

// Render 渲染指定模板，data 为模板变量
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

// RenderSystem 渲染系统提示词
func (l *Loader) RenderSystem(data SystemPromptData) (string, error) {
	return l.Render("system", map[string]any{
		"OS":               data.OS,
		"Arch":             data.Arch,
		"WorkDir":          data.WorkDir,
		"User":             data.User,
		"Time":             data.Time,
		"ProjectConfig":    data.ProjectConfig,
		"UserPreferences":  data.UserPreferences,
		"RecalledMemories": data.RecalledMemories,
		"Capabilities":     data.Capabilities,
	})
}

// RenderGuard 渲染 Guard 审查提示词
func (l *Loader) RenderGuard(data map[string]any) (string, error) {
	return l.Render("guard", data)
}

// RenderCompress 渲染压缩摘要提示词
func (l *Loader) RenderCompress(content string) (string, error) {
	return l.Render("compress", map[string]any{
		"Content": content,
	})
}

// RenderExtract 渲染记忆提取提示词
func (l *Loader) RenderExtract(userInput, agentOutput string) (string, error) {
	return l.Render("extract", map[string]any{
		"UserInput":   userInput,
		"AgentOutput": agentOutput,
	})
}

// SystemPromptData 系统提示词模板变量
type SystemPromptData struct {
	OS               string
	Arch             string
	WorkDir          string
	User             string
	Time             string
	ProjectConfig    string
	UserPreferences  string
	RecalledMemories string
	Capabilities     string
}
