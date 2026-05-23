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

	"github.com/alanchenchen/suna/internal/protocol"
)

/*
TUI 纯前端，无业务逻辑。

设计原则（01-architecture.md I/O 抽象层）：
  - TUI 不持有任何业务逻辑、状态、数据库连接
  - TUI 只做两件事：渲染 UI、通过 local transport 与 daemon 通信
  - 所有输入 → protocol request → local JSON-RPC framing → daemon
  - daemon protocol event → local notification → 渲染到终端
*/
type TUI struct {
	localCli *localClient
	i18n     *translator
	program  *tea.Program
	notifyCh chan localNotification

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
	pendingGuard      *guardConfirmView
	guardCursor       int
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
	showReasoningDetail bool
	phase               phase
	phaseStart          time.Time
	activeTools         map[string]*toolEntry
	toolStartTimes      map[string]time.Time
	currentToolBlock    *toolBlock
	selectedToolID      string
	lastAssistantText   string
	welcomeCursor       int
	configCursor        int
	configSetupMode     bool
	configFormOpen      bool
	configWorkspaceOpen bool
	configKindOpen      bool
	configKindCursor    int
	configDeleteCursor  int
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
	copyMode            bool
	cmdSuggestions      []commandSpec
	cmdSuggestionIdx    int
	attachments         []attachmentItem
	attachmentMode      bool
	attachmentCursor    int
	attachmentDelete    bool
	pendingImagePaste   *pendingImagePaste
	modelPickerOpen     bool
	modelPickerCursor   int
	daemonStatus        protocol.DaemonStatusParams
	configState         protocol.ConfigParams

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

type guardConfirmView struct {
	id         string
	toolCallID string
	tool       string
	params     map[string]any
	risk       string
	reason     string
	suggestion string
}

type chatMsg struct {
	role    string
	content any
}

type userMessageContent struct {
	text        string
	attachments []attachmentItem
}

func New(locale LocaleID) *TUI {
	t := &TUI{
		i18n:  newTranslator(locale),
		mode:  "welcome",
		theme: ThemeAuto,
	}
	t.setTheme(ThemeAuto)
	return t
}

func (t *TUI) Run() error {
	p := tea.NewProgram(t)
	t.program = p
	_, err := p.Run()
	return err
}

func (t *TUI) doQuit() {
	if t.localCli != nil {
		t.localCli.Close()
		t.localCli = nil
	}
}

func (t *TUI) Init() tea.Cmd {
	return func() tea.Msg {
		return tea.Batch(t.daemonStatusCmd(), t.configGetCmd())()
	}
}

func (t *TUI) refreshDaemonStatus() {
	if t.localCli != nil {
		go t.localCli.DaemonStatus()
	}
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if notif, ok := msg.(localNotification); ok {
		t.handleLocalNotification(notif)
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
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+y":
			t.copyMode = !t.copyMode
			return t, nil
		case "esc":
			if t.copyMode {
				t.copyMode = false
				return t, nil
			}
		}
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
	if t.copyMode {
		// 复制模式临时关闭鼠标捕获，把拖拽选择权还给终端；退出后恢复滚轮事件。
		v.MouseMode = tea.MouseModeNone
	}
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

func (t *TUI) runAgent(input string, attachments []attachmentItem) tea.Cmd {
	t.startLLMWait()
	t.activeTools = make(map[string]*toolEntry)
	t.toolStartTimes = make(map[string]time.Time)
	t.currentToolBlock = nil
	t.selectedToolID = ""
	return tea.Batch(t.sendMessageCmd(input, attachments), t.sp.Tick)
}

func (t *TUI) startLLMWait() {
	t.loading = true
	t.phase = phaseFirstLLM
	t.phaseStart = time.Now()
	t.streamStart = time.Now()
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

func (t *TUI) handleLocalNotification(notif localNotification) {
	switch notif.method {
	case protocol.NotifyStream:
		var p protocol.StreamParams
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
	case protocol.NotifyReasoning:
		var p protocol.StreamParams
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
	case protocol.NotifyToolStart:
		var p protocol.ToolStartParams
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
		block := t.ensureToolBlock()
		parentID, localID := parseSubtaskToolID(id)
		t.lastAssistantText = ""
		te := &toolEntry{
			id:        id,
			localID:   localID,
			parentID:  parentID,
			rawName:   p.Tool,
			name:      displayName,
			intent:    intent,
			params:    formatToolParams(p.Params),
			paramsRaw: p.Params,
			summary:   toolParamSummary(p.Tool, p.Params),
			status:    toolRunning,
			startedAt: time.Now(),
		}
		t.activeTools[id] = te
		t.toolStartTimes[id] = te.startedAt
		block.add(te)
		if t.selectedToolID == "" {
			t.selectedToolID = id
		}
	case protocol.NotifyToolEnd:
		var p protocol.ToolEndParams
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
			te.endedAt = time.Now()
			te.resultTruncated = p.ResultTruncated
			te.resultBytes = p.ResultBytes
			if p.Error {
				te.status = toolError
				te.result = p.Result
			} else {
				te.status = toolDone
				te.result = p.Result
			}
		}
		if !t.hasRunningTools() {
			t.phase = phaseLLM
			t.phaseStart = time.Now()
			t.lastAssistantText = ""
		}
	case protocol.NotifyAskUser:
		var p protocol.AskUserParams
		json.Unmarshal(notif.params, &p)
		t.pendingAskID = p.ID
		t.pendingAskOptions = p.Options
		t.pendingAskCursor = 0
		t.messages = append(t.messages, chatMsg{role: "system", content: "❓ " + p.Question})
		t.resetPhase()
	case protocol.NotifyGuardConfirm:
		var p protocol.GuardConfirmParams
		json.Unmarshal(notif.params, &p)
		t.pendingGuard = &guardConfirmView{id: p.ID, toolCallID: p.ToolCallID, tool: p.Tool, params: p.Params, risk: p.Risk, reason: p.Reason, suggestion: p.Suggestion}
		t.guardCursor = 1
		t.loading = false
		t.phase = phaseIdle
		t.phaseStart = time.Time{}
	case protocol.NotifyDaemonState:
		var p protocol.DaemonStateParams
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
	case protocol.NotifyCompactResult:
		var p protocol.CompactResult
		json.Unmarshal(notif.params, &p)
		t.messages = append(t.messages, chatMsg{role: "system", content: t.renderCompactPanel(p)})
	case "compact.error":
		var p struct {
			Message string `json:"message"`
		}
		json.Unmarshal(notif.params, &p)
		t.messages = append(t.messages, chatMsg{role: "error", content: p.Message})
	case protocol.NotifyMemoryListResult:
		var p protocol.MemoryListResult
		json.Unmarshal(notif.params, &p)
		if len(p.Memories) == 0 {
			t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("memory.not_found")})
		} else {
			t.messages = append(t.messages, chatMsg{role: "panel", content: t.renderMemoryList(p.Memories)})
		}
	case protocol.NotifySessionRestoreMsg:
		var p struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		json.Unmarshal(notif.params, &p)
		if p.Content != "" {
			t.messages = append(t.messages, chatMsg{role: p.Role, content: p.Content})
		}
	case protocol.NotifySessionRestoreInput:
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
		if t.configState.GuardMode == "" {
			t.configState.GuardMode = "ask"
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
			wasWorkspace := t.configWorkspaceOpen
			t.configFormOpen = false
			t.configWorkspaceOpen = false
			if wasWorkspace {
				t.configPage = "home"
			} else if t.configEditingName != "" {
				t.openConfigDetail(t.configEditingName)
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
