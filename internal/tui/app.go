package tui

import (
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	"time"

	tea "charm.land/bubbletea/v2"
)

func New(locale LocaleID) *TUI {
	t := &TUI{
		i18n:  newTranslator(locale),
		mode:  uipage.Welcome,
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
		return tea.Batch(t.daemonStatusCmd(), t.configGetCmd(), t.attachmentStatusCmd(), t.listMCPCmd())()
	}
}

func (t *TUI) refreshDaemonStatusCmd() tea.Cmd {
	return t.daemonStatusCmd()
}

func (t *TUI) runAgent(input string, attachments []attachmentItem) tea.Cmd {
	t.startLLMWait()
	t.chat.ResumeAvailable = false
	t.chat.ResetToolState()
	return tea.Batch(t.sendMessageCmd(input, attachments), t.chat.Spinner.Tick)
}

func (t *TUI) resumeAgent() tea.Cmd {
	t.startLLMWait()
	t.chat.ResumeAvailable = false
	t.chat.ResetToolState()
	return tea.Batch(t.resumeRunCmd(), t.chat.Spinner.Tick)
}

func (t *TUI) startLLMWait() {
	t.chat.Textarea.Blur()
	t.chat.StartLLMWait(time.Now())
}

func (t *TUI) appendNonToolMessage(msg chatMsg) {
	t.chat.AppendMessage(msg)
}

func (t *TUI) appendStreamMessage(role, chunk string) {
	t.chat.AppendStreamMessage(role, chunk, time.Now())
}

func (t *TUI) finishStreamingMessages() {
	t.chat.FinishStreamingMessages(time.Now())
}
