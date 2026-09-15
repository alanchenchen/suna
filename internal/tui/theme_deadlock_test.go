package tui

import (
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// 主题切换必须由 Update 返回命令，不能在 Update 内直接 program.Send。
// bubbletea 的消息 channel 无缓冲，事件循环处理 Update 时不会回读，
// 同步 Send 会永久阻塞事件循环，连 Ctrl+C 都收不到（只能强杀终端）。
//
// 这里用真实 program 复现原始故障路径：另一个 TUI 在 Chat 模式下收到
// config.state 广播（主题被改动）。事件循环必须处理完这条广播并继续运行；
// 若回归为同步 Send，Update 永不返回，事件循环停转。
func TestThemeSwitchKeepsEventLoopAlive(t *testing.T) {
	tui := newChatThemeTUI(t)

	applied := make(chan string, 8)
	prog := tea.NewProgram(&probeTUI{TUI: tui, applied: applied}, tea.WithoutRenderer(), tea.WithInput(nil), tea.WithOutput(io.Discard))
	tui.program = prog
	go func() { _, _ = prog.Run() }()
	defer prog.Kill()

	// 等事件循环起来，再投递 config.state（主题变更广播）。
	time.Sleep(50 * time.Millisecond)
	prog.Send(configStateMsg{Params: protocol.ConfigParams{Theme: "probe"}})

	// 主题值只在 program goroutine 内读取并经 channel 传出，避免数据竞争。
	deadline := time.After(2 * time.Second)
	for {
		select {
		case got := <-applied:
			if got == "probe" {
				return
			}
		case <-deadline:
			t.Fatal("event loop stalled after theme switch: Update must not call program.Send synchronously")
		}
	}
}

// 修复不仅要避免死锁，还必须保留原设计意图：Chat 里的 transcript 仍要重建。
// 主题切换会让 markdown 缓存失效，因此必须安排一次帧门同步。
func TestThemeSwitchInChatSchedulesTranscriptRebuild(t *testing.T) {
	tui := newChatThemeTUI(t)
	// 非 nil program 才会走异步帧门路径（测试环境否则同步重建）。
	tui.program = tea.NewProgram(tui, tea.WithoutRenderer(), tea.WithInput(nil))

	_, cmd := tui.Update(configStateMsg{Params: protocol.ConfigParams{Theme: "probe"}})
	if tui.theme != "probe" {
		t.Fatalf("theme = %q, want probe", tui.theme)
	}
	if cmd == nil {
		t.Fatal("theme switch in chat must return a command scheduling the transcript rebuild")
	}
	if !tui.transcriptSyncDirty {
		t.Fatal("theme switch must mark the transcript dirty so it is rebuilt")
	}

	// 帧门消息到达后必须真正结算（dirty 清空），证明重建被安排并执行。
	if _, _ = tui.Update(transcriptSyncMsg{}); tui.transcriptSyncDirty {
		t.Fatal("transcript sync must be flushed after the frame gate fires")
	}
}

// newChatThemeTUI 构造 Chat 模式 TUI，并登记一个可切换的 probe 主题。
func newChatThemeTUI(t *testing.T) *TUI {
	t.Helper()
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 120, height: 40, mode: uipage.Chat, ready: true}
	tui.initChatComponents()
	tui.reloadThemeSpecs()
	tui.themeSpecs = append(tui.themeSpecs, themesys.Spec{Name: "probe", Colors: themesys.DefaultColors()})
	tui.setTheme(themesys.Default)
	return tui
}

// probeTUI 在每次 Update 后把当前主题经 channel 传出，用于判定事件循环是否停转。
type probeTUI struct {
	*TUI
	applied chan string
}

func (p *probeTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := p.TUI.Update(msg)
	select {
	case p.applied <- p.theme:
	default:
	}
	// 必须返回探针自身，否则后续 Update 会绕过计数直接落到内层 *TUI。
	return p, cmd
}

func (p *probeTUI) View() tea.View { return p.TUI.View() }
