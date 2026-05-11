package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/ipc"
)

/*
TUI 纯前端，无业务逻辑。

设计原则（01-architecture.md I/O 抽象层）：
  - TUI 不持有任何业务逻辑、状态、数据库连接
  - TUI 只做两件事：渲染 UI、通过 IPC 与 daemon 通信
  - 所有输入 → JSON-RPC request → daemon
  - daemon notification → 渲染到终端
*/
type TUI struct {
	ipcCli  *ipcClient
	i18n    *translator
	cfgPath string
	program *tea.Program

	mode     string // "welcome" | "chat" | "config" | "help"
	prevMode string
	width    int
	height   int
	ready    bool
	loading  bool

	messages          []chatMsg
	pendingInput      string
	pendingAskID      string
	pendingAskOptions []string
	pendingAskCursor  int
	cmdSuggestion     string
	theme             string

	providerName string
	modelName    string

	vp     viewport.Model
	helpVP viewport.Model
	ta     textarea.Model
	sp     spinner.Model
	menu   list.Model

	showToolDetail      bool
	phase               phase
	phaseStart          time.Time
	activeTools         map[string]*toolEntry
	toolStartTimes      map[string]time.Time
	lastAssistantText   string
	welcomeCursor       int
	configCursor        int
	configSetupMode     bool
	configFormOpen      bool
	configKindOpen      bool
	configKindCursor    int
	configProviderKind  string
	configPage          string
	configDeleteConfirm string
	configLastCheck     string
	configDetailRef     string
	configFormTitle     string
	configInputs        []textinput.Model
	configInputFocus    int
	configError         string
	configFromMode      string
	configModels        []string
	configEditingName   string
	showHelp            bool
	cmdSuggestions      []commandSpec
	cmdSuggestionIdx    int
	modelPickerOpen     bool
	modelPickerCursor   int
	daemonStatus        ipc.DaemonStatusParams
	configState         ipc.ConfigParams

	streamStart      time.Time
	sessionInputTok  int
	sessionOutputTok int
	sessionCachedTok int
	lastInputTok     int
	lastOutputTok    int
	lastCachedTok    int
	lastDuration     time.Duration
	lastTokensPerSec float64
	hasUsage         bool
	contextTokens    int
	contextWindow    int
}

type chatMsg struct {
	role    string
	content any
}

func New(cfgPath string, locale LocaleID) *TUI {
	t := &TUI{
		i18n:    newTranslator(locale),
		cfgPath: cfgPath,
		mode:    "welcome",
		theme:   ThemeAuto,
	}
	t.setTheme(ThemeAuto)
	return t
}

func (t *TUI) Connect(client *ipcClient) {
	t.ipcCli = client
	t.mode = "welcome"
	t.contextWindow = 128000
	t.toolStartTimes = make(map[string]time.Time)
	t.activeTools = make(map[string]*toolEntry)
	t.phase = phaseIdle

	client.OnNotify(func(method string, params json.RawMessage) {
		if t.program != nil {
			t.program.Send(ipcNotification{method: method, params: params})
		}
	})
}

func (t *TUI) Run() error {
	p := tea.NewProgram(t)
	t.program = p
	_, err := p.Run()
	return err
}

func (t *TUI) doQuit() {
	if t.ipcCli != nil {
		t.ipcCli.Close()
		t.ipcCli = nil
	}
}

func (t *TUI) Init() tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli != nil {
			t.ipcCli.DaemonStatus()
			t.ipcCli.ConfigGet()
		}
		return nil
	}
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if notif, ok := msg.(ipcNotification); ok {
		t.handleIPCNotification(notif)
		if t.mode == "welcome" && t.ready {
			t.initWelcomeList()
		}
		if t.mode == "chat" {
			t.syncContent()
		}
		return t, nil
	}
	if !t.ready {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			t.width = ws.Width
			t.height = ws.Height
			t.ready = true
			if t.mode == "chat" {
				return t, t.initChatComponents()
			}
			return t, nil
		}
		return t, nil
	}

	switch t.mode {
	case "welcome":
		return t.updateWelcome(msg)
	case "config":
		return t.updateConfig(msg)
	case "help":
		return t.updateHelp(msg)
	default:
		return t.updateChat(msg)
	}
}

func (t *TUI) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if !t.ready {
		v.SetContent(t.viewWelcome())
		return v
	}
	switch t.mode {
	case "welcome":
		v.SetContent(t.viewWelcome())
	case "config":
		v.SetContent(t.viewConfig())
	case "chat":
		v.SetContent(t.viewChat())
	case "help":
		v.SetContent(t.viewHelp())
	}
	return v
}

type ipcNotification struct {
	method string
	params json.RawMessage
}

func (t *TUI) runAgent(input string) tea.Cmd {
	t.loading = true
	t.phase = phaseFirstLLM
	t.phaseStart = time.Now()
	t.streamStart = time.Now()
	t.activeTools = make(map[string]*toolEntry)
	t.toolStartTimes = make(map[string]time.Time)
	go func() {
		if t.ipcCli != nil {
			t.ipcCli.SendMessage(input)
		}
	}()
	return t.sp.Tick
}

func extractLastSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	sentences := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '。' || r == '\n'
	})
	for i := len(sentences) - 1; i >= 0; i-- {
		s := strings.TrimSpace(sentences[i])
		if s != "" {
			if len(s) > 80 {
				return s[:80] + "..."
			}
			return s
		}
	}
	return ""
}

func formatToolParams(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	b, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", params)
	}
	return string(b)
}

func toolDisplayName(name string) string {
	switch name {
	case "readfile":
		return "Read"
	case "listdir":
		return "List"
	case "readhttp":
		return "HTTP"
	case "writefile":
		return "Write"
	case "editfile":
		return "Edit"
	case "writehttp":
		return "POST"
	case "exec":
		return "Exec"
	case "askuser":
		return "Ask"
	case "spawn":
		return "Spawn"
	default:
		return name
	}
}

func (t *TUI) handleIPCNotification(notif ipcNotification) {
	switch notif.method {
	case ipc.NotifyStream:
		var p ipc.StreamParams
		json.Unmarshal(notif.params, &p)
		if p.Done {
			if strings.HasPrefix(p.Chunk, "error:") || p.Chunk == "cancelled" {
				t.messages = append(t.messages, chatMsg{role: "error", content: p.Chunk})
			}
			t.resetPhase()
			t.hasUsage = p.HasUsage
			t.lastDuration = time.Since(t.streamStart)
			if p.HasUsage {
				t.lastInputTok = p.InputTokens
				t.lastOutputTok = p.OutputTokens
				t.lastCachedTok = p.CachedTokens
				t.lastTokensPerSec = p.TokensPerSec
				t.sessionInputTok += p.InputTokens
				t.sessionOutputTok += p.OutputTokens
				t.sessionCachedTok += p.CachedTokens
				t.contextTokens = p.ContextTokens
			} else {
				t.lastInputTok = 0
				t.lastOutputTok = 0
				t.lastCachedTok = 0
				t.lastTokensPerSec = 0
				t.contextTokens = 0
			}
			if p.ContextWindow > 0 {
				t.contextWindow = p.ContextWindow
			}
			return
		}
		if t.phase == phaseFirstLLM || t.phase == phaseThinking {
			t.phase = phaseLLM
			t.phaseStart = time.Now()
		}
		if p.Chunk != "" {
			t.lastAssistantText += p.Chunk
		}
		if len(t.messages) > 0 && t.messages[len(t.messages)-1].role == "assistant" {
			prev, _ := t.messages[len(t.messages)-1].content.(string)
			t.messages[len(t.messages)-1].content = prev + p.Chunk
		} else {
			t.messages = append(t.messages, chatMsg{role: "assistant", content: p.Chunk})
		}
	case ipc.NotifyReasoning:
		var p ipc.StreamParams
		json.Unmarshal(notif.params, &p)
		if t.phase == phaseFirstLLM || t.phase == phaseLLM {
			t.phase = phaseThinking
			t.phaseStart = time.Now()
		}
		if len(t.messages) > 0 && t.messages[len(t.messages)-1].role == "reasoning" {
			prev, _ := t.messages[len(t.messages)-1].content.(string)
			t.messages[len(t.messages)-1].content = prev + p.Chunk
		} else {
			t.messages = append(t.messages, chatMsg{role: "reasoning", content: p.Chunk})
		}
	case ipc.NotifyToolStart:
		var p ipc.ToolStartParams
		json.Unmarshal(notif.params, &p)
		t.phase = phaseTool
		t.phaseStart = time.Now()
		t.loading = true
		id := p.ID
		if id == "" {
			id = fmt.Sprintf("%s_%d", p.Tool, time.Now().UnixNano())
		}
		displayName := toolDisplayName(p.Tool)
		intent := p.Intent
		params := ""
		if len(p.Params) > 0 {
			params = formatToolParams(p.Params)
		}
		t.lastAssistantText = ""
		te := &toolEntry{
			id:      id,
			rawName: p.Tool,
			name:    displayName,
			intent:  intent,
			params:  params,
			summary: toolParamSummary(p.Tool, p.Params),
			status:  toolRunning,
		}
		t.activeTools[id] = te
		t.toolStartTimes[id] = time.Now()
	case ipc.NotifyToolEnd:
		var p ipc.ToolEndParams
		json.Unmarshal(notif.params, &p)
		id := p.ID
		if id == "" {
			id = fmt.Sprintf("%s_%d", p.Tool, time.Now().UnixNano())
		}
		if te, ok := t.activeTools[id]; ok {
			start, ok := t.toolStartTimes[id]
			if ok {
				te.duration = time.Since(start)
				delete(t.toolStartTimes, id)
			}
			if p.Error {
				te.status = toolError
				te.result = p.Result
			} else {
				te.status = toolDone
				te.result = p.Result
			}
			t.messages = append(t.messages, chatMsg{role: "tool", content: te})
			delete(t.activeTools, id)
		}
		if len(t.activeTools) == 0 {
			t.phase = phaseLLM
			t.phaseStart = time.Now()
			t.lastAssistantText = ""
		}
	case ipc.NotifyAskUser:
		var p ipc.AskUserParams
		json.Unmarshal(notif.params, &p)
		t.pendingAskID = p.ID
		t.pendingAskOptions = p.Options
		t.pendingAskCursor = 0
		t.messages = append(t.messages, chatMsg{role: "system", content: "❓ " + p.Question})
		t.resetPhase()
	case ipc.NotifyDaemonState:
		var p ipc.DaemonStateParams
		json.Unmarshal(notif.params, &p)
		if p.ProviderName != "" {
			t.providerName = p.ProviderName
		}
		if p.ModelName != "" {
			t.modelName = p.ModelName
		}
		if t.mode == "chat" && len(t.messages) == 0 {
			t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.Tf("status.daemon_connected", p.PID)})
		}
	case ipc.NotifyCompactResult:
		var p ipc.CompactResult
		json.Unmarshal(notif.params, &p)
		t.messages = append(t.messages, chatMsg{role: "system", content: t.renderCompactPanel(p)})
	case "compact.error":
		var p struct {
			Message string `json:"message"`
		}
		json.Unmarshal(notif.params, &p)
		t.messages = append(t.messages, chatMsg{role: "error", content: p.Message})
	case ipc.NotifyMemorySearchResult:
		var p ipc.MemorySearchResult
		json.Unmarshal(notif.params, &p)
		if len(p.Memories) == 0 {
			t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("memory.not_found")})
		} else {
			var lines []string
			for _, m := range p.Memories {
				lines = append(lines, fmt.Sprintf("  [%s] %s — %s", m.Timestamp, m.Type, m.Content))
			}
			t.messages = append(t.messages, chatMsg{role: "system", content: strings.Join(lines, "\n")})
		}
	case ipc.NotifySessionRestoreMsg:
		var p struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		json.Unmarshal(notif.params, &p)
		if p.Content != "" {
			t.messages = append(t.messages, chatMsg{role: p.Role, content: p.Content})
		}
	case ipc.NotifySessionRestoreInput:
		var p struct {
			Content string `json:"content"`
		}
		json.Unmarshal(notif.params, &p)
		if p.Content != "" {
			t.setInputValue(p.Content)
		}
	case "daemon.full_status":
		json.Unmarshal(notif.params, &t.daemonStatus)
		if t.daemonStatus.Provider != "" {
			t.providerName = t.daemonStatus.Provider
		}
		if t.daemonStatus.Model != "" {
			t.modelName = t.daemonStatus.Model
		}
		if t.providerName != "" && t.modelName != "" {
			t.configState.ActiveModel = t.providerName + "/" + t.modelName
		}
		if t.daemonStatus.ContextWindow > 0 {
			t.contextWindow = t.daemonStatus.ContextWindow
		}
		if t.daemonStatus.ContextTokens > 0 {
			t.contextTokens = t.daemonStatus.ContextTokens
		}
	case "config.state":
		json.Unmarshal(notif.params, &t.configState)
		t.configError = ""
		if t.configState.Locale != "" {
			t.i18n.SetLocale(LocaleID(t.configState.Locale))
		}
		if t.configState.Theme != "" {
			t.setTheme(t.configState.Theme)
		}
		if t.configDeleteConfirm != "" {
			t.configDeleteConfirm = ""
		}
		if t.configState.ActiveModel != "" {
			if mc, ok := t.activeConfigModel(); ok {
				t.providerName = mc.Provider
				t.modelName = mc.Model
				t.contextWindow = defaultContextWindow(mc)
			}
		}
		if t.configSetupMode && len(t.configState.Models) > 0 {
			t.configSetupMode = false
			t.configFormOpen = false
			t.configPage = "home"
			t.mode = "welcome"
			return
		}
		if t.configFormOpen {
			t.configFormOpen = false
			if t.configEditingName != "" {
				t.configDetailRef = t.configEditingName
				t.configPage = "detail"
			} else {
				t.configPage = "models"
			}
		}
		if t.mode == "welcome" && len(t.configState.Models) == 0 && !t.hasConfiguredModel() {
			t.mode = "config"
			t.configFromMode = "welcome"
			t.configSetupMode = true
			t.openProviderForm("", nil)
		}
	case "config.error":
		var p struct {
			Message string `json:"message"`
		}
		json.Unmarshal(notif.params, &p)
		t.configError = p.Message
	}
}
