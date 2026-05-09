package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	setup    *setupState
	width    int
	height   int
	ready    bool
	loading  bool

	messages      []chatMsg
	pendingInput  string
	pendingAskID  string
	cmdSuggestion string

	providerName string
	modelName    string

	vp     viewport.Model
	helpVP viewport.Model
	ta     textarea.Model
	sp     spinner.Model

	showToolDetail    bool
	phase             phase
	phaseStart        time.Time
	activeTools       map[string]*toolEntry
	toolStartTimes    map[string]time.Time
	lastAssistantText string
	welcomeCursor     int
	configCursor      int
	configSetupMode   bool
	configFormOpen    bool
	configFormTitle   string
	configInputs      []textinput.Model
	configInputFocus  int
	configError       string
	configFromMode    string
	configModels      []string
	configEditingName string
	showHelp          bool
	cmdSuggestions    []commandSpec
	cmdSuggestionIdx  int
	daemonStatus      ipc.DaemonStatusParams
	configState       ipc.ConfigParams

	streamStart      time.Time
	sessionInputTok  int
	sessionOutputTok int
	sessionCachedTok int
	lastInputTok     int
	lastOutputTok    int
	lastCachedTok    int
	lastDuration     time.Duration
	contextWindow    int
}

type chatMsg struct {
	role    string
	content any
}

type setupState struct {
	step      int
	cursor    int
	provider  string
	baseURL   string
	modelName string
	apiKeyEnv string
	apiKey    string
	error     string
	input     string
}

func New(cfgPath string, locale LocaleID) *TUI {
	t := &TUI{
		i18n:    newTranslator(locale),
		cfgPath: cfgPath,
		mode:    "welcome",
		setup:   &setupState{step: 0, cursor: 0},
	}
	if cfgPath != "" {
		if _, err := loadConfigFile(cfgPath); err != nil {
			t.mode = "config"
			t.configSetupMode = true
			t.configFormOpen = true
			t.configFormTitle = "tui.config.setup_title"
			t.initProviderForm(nil)
		}
	}
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

	go func() {
		t.ipcCli.DaemonStatus()
		t.ipcCli.ConfigGet()
	}()
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

func (t *TUI) Init() tea.Cmd { return nil }

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func toolIntent(name string, params map[string]any) string {
	switch name {
	case "readfile":
		if p, ok := params["path"].(string); ok {
			return truncateContent(p, 60)
		}
	case "listdir":
		if p, ok := params["path"].(string); ok {
			return truncateContent(p, 60)
		}
	case "exec":
		if c, ok := params["command"].(string); ok {
			return truncateContent(c, 60)
		}
	case "writefile":
		if p, ok := params["path"].(string); ok {
			return truncateContent(p, 60)
		}
	case "editfile":
		if p, ok := params["path"].(string); ok {
			return truncateContent(p, 60)
		}
	case "readhttp":
		if u, ok := params["url"].(string); ok {
			return truncateContent(u, 60)
		}
	case "writehttp":
		if u, ok := params["url"].(string); ok {
			return truncateContent(u, 60)
		}
	case "spawn":
		if task, ok := params["task"].(string); ok {
			return truncateContent(task, 60)
		}
	case "askuser":
		if q, ok := params["question"].(string); ok {
			return truncateContent(q, 60)
		}
	}
	return ""
}

func (t *TUI) handleIPCNotification(notif ipcNotification) {
	switch notif.method {
	case ipc.NotifyStream:
		var p ipc.StreamParams
		json.Unmarshal(notif.params, &p)
		if p.Done {
			t.resetPhase()
			if p.InputTokens > 0 || p.OutputTokens > 0 {
				t.lastInputTok = p.InputTokens
				t.lastOutputTok = p.OutputTokens
				t.lastCachedTok = p.CachedTokens
				t.lastDuration = time.Since(t.streamStart)
				t.sessionInputTok += p.InputTokens
				t.sessionOutputTok += p.OutputTokens
				t.sessionCachedTok += p.CachedTokens
			}
			return
		}
		if t.phase == phaseFirstLLM {
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
		intent := toolIntent(p.Tool, p.Params)
		t.lastAssistantText = ""
		te := &toolEntry{
			id:     id,
			name:   displayName,
			intent: intent,
			status: toolRunning,
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
				te.result = truncateContent(p.Result, 200)
			} else {
				te.status = toolDone
				te.result = truncateContent(p.Result, 200)
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
	case "session.restore_message":
		var p struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		json.Unmarshal(notif.params, &p)
		if p.Content != "" {
			t.messages = append(t.messages, chatMsg{role: p.Role, content: p.Content})
		}
	case "daemon.full_status":
		json.Unmarshal(notif.params, &t.daemonStatus)
		if t.daemonStatus.Provider != "" {
			t.providerName = t.daemonStatus.Provider
		}
		if t.daemonStatus.Model != "" {
			t.modelName = t.daemonStatus.Model
		}
	case "config.state":
		json.Unmarshal(notif.params, &t.configState)
	}
}
