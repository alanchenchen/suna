package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/core"
	"github.com/alanchenchen/suna/internal/i18n"
)

type TUI struct {
	agent   *core.Agent
	i18n    *i18n.I18n
	cfgPath string
	cfg     *config.Config

	mode    string // "setup" | "chat"
	setup   *setupState
	program *tea.Program

	messages []chatMsg
	input    string
	loading  bool
	width    int
	height   int
	ready    bool

	// 统计信息
	streamStart    time.Time
	sessionInputTok  int
	sessionOutputTok int
	sessionCachedTok int
	lastInputTok   int
	lastOutputTok  int
	lastCachedTok  int
	lastDuration   time.Duration
}

type chatMsg struct {
	role    string
	content string
}

// setupState 配置向导状态
// 步骤：0=欢迎 → 1=选provider(上下键) → 2=选model(上下键) → 3=输入base_url(仅Other) → 4=输入API key → done
type setupState struct {
	step      int
	cursor    int    // 上下键高亮位置
	provider  string
	baseURL   string
	modelName string
	apiKeyEnv string
	apiKey    string
	error     string
}

var (
	styleAccent  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleUser    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleAgent   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	styleTool    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	styleError   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	styleSystem  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleHL      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	styleCursor  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	styleLogo    = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleLogoDim = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

const logo = `
       ███████╗██╗   ██╗███╗   ███╗
       ██╔════╝██║   ██║████╗ ████║
       ███████╗██║   ██║██╔████╔██║
       ╚════██║██║   ██║██║╚██╔╝██║
       ███████║╚██████╔╝██║ ╚═╝ ██║
       ╚══════╝ ╚═════╝ ╚═╝     ╚═╝
              ◇  stateful AI agent
`

type providerPreset struct {
	name     string
	provider string
	baseURL  string
	keyEnv   string
	models   []string
}

var presets = []providerPreset{
	{"Zhipu (智谱)", "openai", "https://open.bigmodel.cn/api/paas/v4", "GLM_API_KEY", []string{"glm-4-plus", "glm-4", "glm-4-flash"}},
	{"OpenAI", "openai", "https://api.openai.com/v1", "OPENAI_API_KEY", []string{"gpt-4o", "gpt-4o-mini", "o1"}},
	{"DeepSeek", "openai", "https://api.deepseek.com", "DEEPSEEK_API_KEY", []string{"deepseek-chat", "deepseek-reasoner"}},
	{"Moonshot (Kimi)", "openai", "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY", []string{"moonshot-v1-auto", "moonshot-v1-8k", "moonshot-v1-32k"}},
	{"Anthropic", "anthropic", "", "ANTHROPIC_API_KEY", []string{"claude-sonnet-4-20250514", "claude-haiku-4-20250514", "claude-opus-4-20250514"}},
	{"Other (OpenAI compatible)", "openai", "CUSTOM", "LLM_API_KEY", nil},
}

func New(cfgPath string, locale i18n.LocaleID) *TUI {
	return &TUI{
		i18n:   i18n.New(locale),
		cfgPath: cfgPath,
		mode:   "setup",
		setup:  &setupState{step: 0, cursor: 0},
	}
}

// SetAgent 注入已创建的 Agent 和 Config，直接进入 chat 模式（跳过 setup wizard）
func (t *TUI) SetAgent(agent *core.Agent, cfg *config.Config) {
	t.agent = agent
	t.cfg = cfg
	t.mode = "chat"
	mc := cfg.Models["default"]
	providerName := mc.ProviderName
	if providerName == "" {
		providerName = mc.Provider
		if mc.Provider == "openai" && mc.BaseURL != "" {
			providerName = friendlyProviderName(mc.BaseURL)
		}
	}
	t.messages = []chatMsg{
		{role: "system", content: fmt.Sprintf("Ready! Model: %s | Provider: %s\nType your message to start chatting.\n", mc.Model, providerName)},
	}
}

// friendlyProviderName 根据 base URL 返回用户友好的 provider 名
func friendlyProviderName(baseURL string) string {
	switch {
	case strings.Contains(baseURL, "bigmodel.cn"):
		return "Zhipu (智谱)"
	case strings.Contains(baseURL, "openai.com"):
		return "OpenAI"
	case strings.Contains(baseURL, "deepseek.com"):
		return "DeepSeek"
	case strings.Contains(baseURL, "moonshot.cn"):
		return "Moonshot (Kimi)"
	default:
		return baseURL
	}
}

func (t *TUI) Run(ctx context.Context) error {
	p := tea.NewProgram(t, tea.WithAltScreen())
	t.program = p
	_, err := p.Run()
	return err
}

// === bubbletea Model ===

func (t *TUI) Init() tea.Cmd { return nil }

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width = m.Width
		t.height = m.Height
		t.ready = true
		return t, nil

	case tea.KeyMsg:
		if t.mode == "setup" {
			return t.handleSetupKey(m)
		}
		return t.handleChatKey(m)

	case agentEvent:
		t.handleAgentEvent(m.evt)
		return t, nil
	case agentDone:
		t.loading = false
		return t, nil
	}

	return t, nil
}

func (t *TUI) View() string {
	if !t.ready {
		return "Initializing..."
	}
	if t.mode == "setup" {
		return t.viewSetup()
	}
	return t.viewChat()
}

// ═══════════════════════════════════════
// Setup Wizard
// ═══════════════════════════════════════

func (t *TUI) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := t.setup
	s.error = ""

	switch msg.Type {
	case tea.KeyCtrlC:
		return t, tea.Quit
	case tea.KeyUp:
		if s.step == 1 {
			if s.cursor > 0 {
				s.cursor--
			}
		} else if s.step == 2 {
			if s.cursor > 0 {
				s.cursor--
			}
		}
		return t, nil
	case tea.KeyDown:
		if s.step == 1 {
			if s.cursor < len(presets)-1 {
				s.cursor++
			}
		} else if s.step == 2 {
			preset := t.selectedPreset()
			if preset != nil && s.cursor < len(preset.models)-1 {
				s.cursor++
			}
		}
		return t, nil
	case tea.KeyEnter:
		return t.handleSetupEnter()
	case tea.KeySpace:
		t.input += " "
		return t, nil
	case tea.KeyBackspace:
		runes := []rune(t.input)
		if len(runes) > 0 {
			t.input = string(runes[:len(runes)-1])
		}
		return t, nil
	default:
		if msg.Type == tea.KeyRunes {
			t.input += string(msg.Runes)
		}
		return t, nil
	}
}

func (t *TUI) handleSetupEnter() (tea.Model, tea.Cmd) {
	s := t.setup

	switch s.step {
	case 0:
		s.step = 1
		s.cursor = 0

	case 1:
		preset := presets[s.cursor]
		s.provider = preset.provider
		s.baseURL = preset.baseURL
		s.apiKeyEnv = preset.keyEnv

		if preset.baseURL == "CUSTOM" {
			// Other provider：需要手动输入 base URL
			s.baseURL = ""
			s.step = 3
		} else if len(preset.models) > 0 {
			// 有预设 model 列表 → 进入 model 选择
			s.step = 2
			s.cursor = 0
		} else {
			// 无预设 model → 手动输入 model name
			s.step = 4
		}

	case 2:
		preset := t.selectedPreset()
		if preset != nil && s.cursor < len(preset.models) {
			s.modelName = preset.models[s.cursor]
		}
		s.step = 5 // 直接跳到输入 API key

	case 3:
		// Other provider: 输入 base URL
		url := strings.TrimSpace(t.input)
		if url == "" {
			s.error = "Base URL is required"
			return t, nil
		}
		s.baseURL = url
		t.input = ""
		s.step = 4 // 接着输入 model name

	case 4:
		// 手动输入 model name（Other provider 或无预设 model）
		name := strings.TrimSpace(t.input)
		if name == "" {
			s.error = "Model name is required"
			return t, nil
		}
		s.modelName = name
		t.input = ""
		s.step = 5

	case 5:
		// 输入 API key
		key := strings.TrimSpace(t.input)
		if key == "" {
			s.error = "API key is required"
			return t, nil
		}
		s.apiKey = key
		t.input = ""
		return t, t.finishSetup()
	}

	t.input = ""
	return t, nil
}

func (t *TUI) finishSetup() tea.Cmd {
	s := t.setup
	os.Setenv(s.apiKeyEnv, s.apiKey)

	// 查找 preset 名称用于 provider_name 字段
	presetName := s.provider
	for _, p := range presets {
		if p.provider == s.provider && p.baseURL == s.baseURL {
			presetName = p.name
			break
		}
	}

	cfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"default": {
				Provider:     s.provider,
				ProviderName: presetName,
				Model:        s.modelName,
				BaseURL:      s.baseURL,
				APIKeyEnv:    s.apiKeyEnv,
			},
		},
		Router: config.RouterConfig{Default: "default"},
		TUI:    config.TUIConfig{Theme: "dark"},
	}
	homeDir, _ := os.UserHomeDir()
	cfg.DataDir = filepath.Join(homeDir, ".suna")

	if err := cfg.EnsureDataDir(); err != nil {
		s.error = err.Error()
		return nil
	}
	if err := cfg.Save(t.cfgPath); err != nil {
		s.error = err.Error()
		return nil
	}

	// 持久化 API key 到 ~/.suna/.credentials
	if err := config.SaveCredentials(cfg.DataDir, s.apiKeyEnv, s.apiKey); err != nil {
		s.error = "Failed to save credentials: " + err.Error()
		return nil
	}

	agent, err := core.NewAgent(cfg)
	if err != nil {
		s.error = err.Error()
		return nil
	}

	t.agent = agent
	t.cfg = cfg
	t.messages = []chatMsg{{role: "system", content: "Ready! Configuration saved to " + t.cfgPath + "\nType your message to start chatting.\n"}}
	t.mode = "chat"
	return nil
}

func (t *TUI) selectedPreset() *providerPreset {
	for i := range presets {
		if presets[i].provider == t.setup.provider && presets[i].baseURL == t.setup.baseURL {
			return &presets[i]
		}
	}
	return nil
}

func (t *TUI) viewSetup() string {
	s := t.setup
	switch s.step {
	case 0:
		return t.viewWelcome()
	case 1:
		return t.viewProviderSelect()
	case 2:
		return t.viewModelSelect()
	case 3:
		return t.viewInputStep("Base URL", "e.g. https://api.example.com/v1", []string{
			"Provider: " + styleHL.Render(s.provider),
		})
	case 4:
		return t.viewInputStep("Model Name", "e.g. gpt-4o", []string{
			"Provider:  " + styleHL.Render(s.provider),
			"Base URL:  " + s.baseURL,
		})
	case 5:
		hint := ""
		if s.provider == "anthropic" {
			hint = "Get your key at https://console.anthropic.com/settings/keys"
		} else if s.baseURL == "https://open.bigmodel.cn/api/paas/v4" {
			hint = "Get your key at https://open.bigmodel.cn/usercenter/apikeys"
		} else if strings.Contains(s.baseURL, "openai.com") {
			hint = "Get your key at https://platform.openai.com/api-keys"
		} else if strings.Contains(s.baseURL, "deepseek.com") {
			hint = "Get your key at https://platform.deepseek.com/api_keys"
		}
		extra := []string{
			"Provider:  " + styleHL.Render(s.provider),
			"Model:     " + styleHL.Render(s.modelName),
		}
		if s.baseURL != "" {
			extra = append(extra, "Base URL:  "+s.baseURL)
		}
		if hint != "" {
			extra = append(extra, "")
			extra = append(extra, styleDim.Render(hint))
		}
		return t.viewInputStep("API Key", "paste your key here", extra)
	default:
		return ""
	}
}

func (t *TUI) viewWelcome() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(styleLogo.Render(logo))
	sb.WriteString("\n")
	sb.WriteString("  Suna is a stateful AI agent with memory and perception.\n")
	sb.WriteString("  It learns from you over time and can act on your behalf.\n")
	sb.WriteString("\n")
	sb.WriteString("  Let's set up your first model to get started.\n")
	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("  Press Enter to continue..."))
	sb.WriteString("\n\n")
	if t.input != "" {
		sb.WriteString("> " + t.input)
	}
	sb.WriteString("█")
	return sb.String()
}

func (t *TUI) viewProviderSelect() string {
	var sb strings.Builder
	sb.WriteString(styleLogoDim.Render("  Suna Setup"))
	sb.WriteString("\n\n")
	sb.WriteString("  Choose your LLM provider:\n\n")

	for i, p := range presets {
		cursor := "   "
		style := lipgloss.NewStyle()
		if i == t.setup.cursor {
			cursor = styleCursor.Render(" > ")
			style = styleHL
		}
		sb.WriteString(cursor + style.Render(p.name) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("  ↑↓ navigate · Enter select"))
	sb.WriteString("\n\n> " + t.input + "█")
	return sb.String()
}

func (t *TUI) viewModelSelect() string {
	preset := t.selectedPreset()
	if preset == nil || len(preset.models) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(styleLogoDim.Render("  Suna Setup"))
	sb.WriteString("\n\n")
	sb.WriteString("  Provider: " + styleHL.Render(preset.name))
	sb.WriteString("\n\n")
	sb.WriteString("  Choose a model:\n\n")

	for i, m := range preset.models {
		cursor := "   "
		style := lipgloss.NewStyle()
		if i == t.setup.cursor {
			cursor = styleCursor.Render(" > ")
			style = styleHL
		}
		sb.WriteString(cursor + style.Render(m) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styleDim.Render("  ↑↓ navigate · Enter select"))
	sb.WriteString("\n\n> " + t.input + "█")
	return sb.String()
}

func (t *TUI) viewInputStep(label, placeholder string, info []string) string {
	var sb strings.Builder
	sb.WriteString(styleLogoDim.Render("  Suna Setup"))
	sb.WriteString("\n\n")

	for _, line := range info {
		sb.WriteString("  " + line + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString("  " + label + ":\n")
	sb.WriteString(styleDim.Render("  " + placeholder))
	sb.WriteString("\n\n")

	if t.setup.error != "" {
		sb.WriteString("  " + styleError.Render("✗ "+t.setup.error) + "\n\n")
	}

	sb.WriteString("> ")
	if t.setup.step == 5 {
		// API key 输入时隐藏内容
		sb.WriteString(strings.Repeat("•", len(t.input)))
	} else {
		sb.WriteString(t.input)
	}
	sb.WriteString("█")
	return sb.String()
}

// ═══════════════════════════════════════
// Chat 模式
// ═══════════════════════════════════════

func (t *TUI) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		return t, tea.Quit
	case tea.KeyEnter:
		if t.loading {
			return t, nil
		}
		input := strings.TrimSpace(t.input)
		t.input = ""
		if input == "" {
			return t, nil
		}
		t.messages = append(t.messages, chatMsg{role: "user", content: input})
		if strings.HasPrefix(input, "/") {
			t.handleCommand(input)
			return t, nil
		}
		return t, t.runAgent(input)
	case tea.KeySpace:
		t.input += " "
		return t, nil
	case tea.KeyBackspace:
		runes := []rune(t.input)
		if len(runes) > 0 {
			t.input = string(runes[:len(runes)-1])
		}
		return t, nil
	default:
		if msg.Type == tea.KeyRunes {
			t.input += string(msg.Runes)
		}
		return t, nil
	}
}

func (t *TUI) viewChat() string {
	var sb strings.Builder

	for _, msg := range t.messages {
		var label string
		var style lipgloss.Style
		switch msg.role {
		case "user":
			label, style = t.i18n.T("label.you"), styleUser
		case "assistant":
			label, style = t.i18n.T("label.suna"), styleAgent
		case "tool":
			label, style = t.i18n.T("label.tool"), styleTool
		case "error":
			label, style = t.i18n.T("label.error"), styleError
		default:
			label, style = t.i18n.T("label.system"), styleSystem
		}
		sb.WriteString(style.Render(fmt.Sprintf("[%s]", label)))
		sb.WriteString(" " + msg.content + "\n")
	}

	if t.loading {
		elapsed := time.Since(t.streamStart)
		sb.WriteString(styleSystem.Render(fmt.Sprintf("● %s (%.1fs)", t.i18n.T("status.thinking"), elapsed.Seconds())))
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("> %s█", t.input))
	sb.WriteString("\n")

	sb.WriteString(t.renderStatusBar())
	return sb.String()
}

func (t *TUI) renderStatusBar() string {
	mc := config.ModelConfig{}
	if t.cfg != nil {
		mc = t.cfg.Models["default"]
	}

	var leftParts []string
	var rightParts []string

	// 左侧：Provider/Model
	if mc.Provider != "" {
		pName := mc.ProviderName
		if pName == "" {
			pName = mc.Provider
		}
		leftParts = append(leftParts, fmt.Sprintf("%s/%s", pName, mc.Model))
	}

	// 左侧：本次请求 token（in+cached → out）
	if t.lastInputTok > 0 || t.lastOutputTok > 0 {
		inStr := fmt.Sprintf("in:%d", t.lastInputTok)
		if t.lastCachedTok > 0 {
			inStr = fmt.Sprintf("in:%d(cache:%d)", t.lastInputTok, t.lastCachedTok)
		}
		rightParts = append(rightParts, fmt.Sprintf("%s out:%d", inStr, t.lastOutputTok))
	}

	// 右侧：速度
	if t.lastOutputTok > 0 && t.lastDuration.Seconds() > 0 {
		speed := float64(t.lastOutputTok) / t.lastDuration.Seconds()
		rightParts = append(rightParts, fmt.Sprintf("%.0f tok/s", speed))
	}

	// 右侧：会话累计
	if t.sessionInputTok > 0 && t.lastInputTok > 0 && t.sessionInputTok > t.lastInputTok {
		rightParts = append(rightParts, fmt.Sprintf("session: %d+%d", t.sessionInputTok, t.sessionOutputTok))
	}

	left := strings.Join(leftParts, " ")
	right := strings.Join(rightParts, " │ ")

	if left == "" && right == "" {
		return styleDim.Render("  /help for commands")
	}

	if left != "" && right != "" {
		return styleDim.Render("  " + left + "  " + right)
	}
	if left != "" {
		return styleDim.Render("  " + left)
	}
	return styleDim.Render("  " + right)
}

func (t *TUI) handleCommand(input string) {
	if t.agent == nil {
		t.messages = append(t.messages, chatMsg{role: "error", content: "Agent not initialized"})
		return
	}
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}
	cmd := parts[0]

	switch cmd {
	case "/new":
		t.agent.NewSession()
		t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("session.new")})
	case "/model":
		t.messages = append(t.messages, chatMsg{role: "system", content: strings.Join(t.agent.ListModels(), ", ")})
	case "/memory":
		if len(parts) >= 2 && parts[1] == "search" {
			query := strings.Join(parts[2:], " ")
			results, err := t.agent.SearchMemory(context.Background(), query, 5)
			if err != nil {
				t.messages = append(t.messages, chatMsg{role: "error", content: err.Error()})
			} else if len(results) == 0 {
				t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("memory.not_found")})
			} else {
				var lines []string
				for _, m := range results {
					lines = append(lines, fmt.Sprintf("[%s] %s", m.Timestamp.Format("2006-01-02 15:04"), m.Content))
				}
				t.messages = append(t.messages, chatMsg{role: "system", content: strings.Join(lines, "\n")})
			}
		} else {
			t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("memory.search_hint")})
		}
	case "/compact":
		before, after, err := t.agent.Compact(context.Background())
		if err != nil {
			t.messages = append(t.messages, chatMsg{role: "error", content: err.Error()})
		} else {
			t.messages = append(t.messages, chatMsg{role: "system", content: fmt.Sprintf("compressed: %d → %d tokens", before, after)})
		}
	case "/help":
		help := fmt.Sprintf(`%s
  /new              - %s
  /model            - %s
  /memory search Q  - %s
  /compact          - %s
  /help             - %s`,
			t.i18n.T("cmd.help_title"),
			t.i18n.T("cmd.new"),
			t.i18n.T("cmd.model"),
			t.i18n.T("cmd.memory"),
			t.i18n.T("cmd.compact"),
			t.i18n.T("cmd.help"))
		t.messages = append(t.messages, chatMsg{role: "system", content: help})
	default:
		t.messages = append(t.messages, chatMsg{role: "error", content: t.i18n.T("cmd.unknown") + " " + cmd})
	}
}

// ═══════════════════════════════════════
// Agent 事件
// ═══════════════════════════════════════

type agentEvent struct{ evt core.Event }
type agentDone struct{}

// runAgent 启动 agent，在 goroutine 中通过 p.Send() 逐条推送事件到 bubbletea 主循环
func (t *TUI) runAgent(input string) tea.Cmd {
	t.loading = true
	return func() tea.Msg {
		go func() {
			events := t.agent.Run(context.Background(), input)
			for evt := range events {
				if evt.Type == core.EventAskUser && evt.Reply != nil {
					evt.Reply <- ""
				}
				t.program.Send(agentEvent{evt: evt})
			}
			t.program.Send(agentDone{})
		}()
		return nil
	}
}

func (t *TUI) handleAgentEvent(evt core.Event) {
	switch evt.Type {
	case core.EventStream:
		t.loading = false
		if len(t.messages) > 0 && t.messages[len(t.messages)-1].role == "assistant" {
			t.messages[len(t.messages)-1].content += evt.Content
		} else {
			t.messages = append(t.messages, chatMsg{role: "assistant", content: evt.Content})
		}
	case core.EventToolCall:
		params := truncateParams(evt.ToolParams)
		t.messages = append(t.messages, chatMsg{role: "tool", content: fmt.Sprintf("%s(%s)", evt.ToolName, params)})
	case core.EventToolResult:
		content := truncateContent(evt.ToolResult, 200)
		role := "tool"
		if evt.ToolError {
			role = "error"
		}
		t.messages = append(t.messages, chatMsg{role: role, content: content})
	case core.EventStatus:
		if evt.Content == "thinking" {
			t.loading = true
			t.streamStart = time.Now()
		} else if evt.Content == "done" {
			t.loading = false
			t.lastInputTok = evt.InputTokens
			t.lastOutputTok = evt.OutputTokens
			t.lastCachedTok = evt.CachedTokens
			t.lastDuration = time.Since(t.streamStart)
			t.sessionInputTok += evt.InputTokens
			t.sessionOutputTok += evt.OutputTokens
			t.sessionCachedTok += evt.CachedTokens
		} else {
			t.loading = false
			t.messages = append(t.messages, chatMsg{role: "system", content: evt.Content})
		}
	case core.EventAskUser:
		t.messages = append(t.messages, chatMsg{role: "system", content: "❓ " + evt.Question})
		t.loading = false
	}
}

func truncateParams(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for k, v := range params {
		s := fmt.Sprintf("%v", v)
		if len(s) > 50 {
			s = s[:50] + "..."
		}
		parts = append(parts, k+"="+s)
	}
	r := strings.Join(parts, ", ")
	if len(r) > 100 {
		r = r[:100] + "..."
	}
	return r
}

func truncateContent(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
