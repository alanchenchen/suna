package capability

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
Capability 是 Suna 的程序记忆。

设计原则（来自 05-capability.md）：
  - 能力 = 知识 (SKILL.md) + 可选程序 (main.js) + 可选外部服务 (mcp.json)
  - 三种类型：declarative（纯知识）、script（知识+JS）、mcp（知识+MCP）
  - 目录结构：~/.suna/capabilities/<name>/SKILL.md
  - 注入方式：摘要列表始终注入 system prompt，LLM 按需通过 [LOAD_SKILL: name] 加载完整内容
  - LLM 自主决定加载哪个能力，用户不配置能力选择

加载流程：
  1. Agent 启动时扫描 ~/.suna/capabilities/
  2. 每个子目录读取 SKILL.md → 解析 name, prompt, type, tools
  3. 摘要列表注入 system prompt（第一层：始终可见）
  4. LLM 回复中含 [LOAD_SKILL: name] → 内核拦截 → 注入完整 SKILL.md（第二层）
*/

type Type string

const (
	TypeDeclarative Type = "declarative"
	TypeScript      Type = "script"
	TypeMCP         Type = "mcp"
)

type Capability struct {
	Name        string
	Description string
	Prompt      string
	Type        Type
	Tools       []string
	Dir         string
}

type Info struct {
	Name        string
	Description string
	Type        Type
}

type Loader struct {
	capabilities map[string]*Capability
	loaded       map[string]bool
}

func NewLoader() *Loader {
	return &Loader{
		capabilities: make(map[string]*Capability),
		loaded:       make(map[string]bool),
	}
}

func (l *Loader) LoadAll(ctx context.Context, dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read capabilities dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); os.IsNotExist(err) {
			continue
		}
		cap, err := parseCapabilityDir(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		l.capabilities[cap.Name] = cap
	}
	return nil
}

func parseCapabilityDir(dir string) (*Capability, error) {
	skillPath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, err
	}

	cap := parseSKILLMD(string(data))
	if cap.Name == "" {
		cap.Name = filepath.Base(dir)
	}
	cap.Dir = dir

	// 检查类型
	if _, err := os.Stat(filepath.Join(dir, "main.js")); err == nil {
		cap.Type = TypeScript
	} else if _, err := os.Stat(filepath.Join(dir, "mcp.json")); err == nil {
		cap.Type = TypeMCP
	} else {
		cap.Type = TypeDeclarative
	}

	return cap, nil
}

func parseSKILLMD(content string) *Capability {
	cap := &Capability{
		Type:  TypeDeclarative,
		Tools: []string{},
	}

	body := content

	// 检查 frontmatter (--- 包裹的 YAML 块)
	if strings.HasPrefix(content, "---") {
		end := strings.Index(content[3:], "---")
		if end > 0 {
			fm := content[3 : end+3]
			body = content[end+6:]
			if len(body) > 0 && body[0] == '\n' {
				body = body[1:]
			}
			parseFrontmatter(fm, cap)
		}
	}

	// 检查 footer meta (--- 分隔符后的元数据)
	if footerIdx := strings.LastIndex(body, "\n---\n"); footerIdx > 0 {
		meta := body[footerIdx+5:]
		body = body[:footerIdx]
		parseFooterMeta(meta, cap)
	}

	// name 从 H1 标题提取
	if cap.Name == "" {
		cap.Name = extractH1(body)
	}

	// 描述：H1 后的第一段非空文本
	if cap.Description == "" {
		cap.Description = extractDescription(body)
	}

	// 全部内容作为 prompt
	cap.Prompt = strings.TrimSpace(body)

	return cap
}

func parseFrontmatter(fm string, cap *Capability) {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "name":
			cap.Name = val
		case "tools":
			for _, t := range strings.Split(val, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					cap.Tools = append(cap.Tools, t)
				}
			}
		case "type":
			cap.Type = Type(val)
		}
	}
}

func parseFooterMeta(meta string, cap *Capability) {
	for _, line := range strings.Split(meta, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "tools":
			if len(cap.Tools) == 0 {
				for _, t := range strings.Split(val, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						cap.Tools = append(cap.Tools, t)
					}
				}
			}
		case "type":
			if cap.Type == TypeDeclarative {
				cap.Type = Type(val)
			}
		}
	}
}

func extractH1(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

func extractDescription(body string) string {
	foundH1 := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			foundH1 = true
			continue
		}
		if foundH1 && line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
			if len(line) > 200 {
				return line[:200]
			}
			return line
		}
	}
	return ""
}

// Summary 生成能力摘要列表，注入 system prompt
func (l *Loader) Summary() string {
	if len(l.capabilities) == 0 {
		return ""
	}
	var lines []string
	for _, cap := range l.capabilities {
		lines = append(lines, fmt.Sprintf("- %s: %s", cap.Name, cap.Description))
	}
	return strings.Join(lines, "\n")
}

// LoadSkill 处理 [LOAD_SKILL: name] 标记，返回完整的 SKILL.md 内容
func (l *Loader) LoadSkill(name string) (string, bool) {
	cap, ok := l.capabilities[name]
	if !ok {
		return "", false
	}
	l.loaded[name] = true
	return cap.Prompt, true
}

// IsLoaded 检查能力是否已被加载
func (l *Loader) IsLoaded(name string) bool {
	return l.loaded[name]
}

// List 返回所有能力的摘要信息
func (l *Loader) List() []Info {
	var infos []Info
	for _, cap := range l.capabilities {
		infos = append(infos, Info{
			Name:        cap.Name,
			Description: cap.Description,
			Type:        cap.Type,
		})
	}
	return infos
}

// Get 获取指定能力的完整定义
func (l *Loader) Get(name string) (*Capability, bool) {
	cap, ok := l.capabilities[name]
	return cap, ok
}

// ProcessLoadMarkers 扫描 LLM 输出中的 [LOAD_SKILL: name] 标记
// 返回处理后的文本和加载的能力名称列表
func (l *Loader) ProcessLoadMarkers(text string) (string, []string) {
	var loaded []string
	result := text

	for {
		start := strings.Index(result, "[LOAD_SKILL:")
		if start < 0 {
			break
		}
		end := strings.Index(result[start:], "]")
		if end < 0 {
			break
		}
		end += start

		name := strings.TrimSpace(result[start+12 : end])
		if _, ok := l.capabilities[name]; ok {
			loaded = append(loaded, name)
		}
		result = result[:start] + result[end+1:]
	}

	return result, loaded
}

// Reload 重新加载能力目录
func (l *Loader) Reload(ctx context.Context, dir string) error {
	l.capabilities = make(map[string]*Capability)
	l.loaded = make(map[string]bool)
	return l.LoadAll(ctx, dir)
}
