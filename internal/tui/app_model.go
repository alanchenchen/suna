package tui

import (
	"time"

	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
	helppage "github.com/alanchenchen/suna/internal/tui/pages/help"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	welcomepage "github.com/alanchenchen/suna/internal/tui/pages/welcome"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
)

/*
TUI 纯前端，无业务逻辑。

设计原则（01-architecture.md I/O 抽象层）：
  - TUI 不持有任何业务逻辑、状态、数据库连接
  - TUI 只做两件事：渲染 UI、通过 local transport 与 daemon 通信
  - 所有输入 → protocol request → local JSON-RPC framing → daemon
  - daemon protocol event → typed tea.Msg → 渲染到终端
*/
type TUI struct {
	// Bubble Tea 运行时与 daemon I/O。副作用必须通过 tea.Cmd 或 notification pump 回到 Update。
	localCli *tuitransport.Client
	i18n     *translator
	program  *tea.Program
	notifyCh chan localNotification

	// 根应用状态：只负责页面路由和全局尺寸/复制模式。
	mode     uipage.Page
	prevMode uipage.Page
	width    int
	height   int
	ready    bool
	copyMode bool

	// 全局配置与 daemon 快照。真实持久化状态由 daemon 持有，TUI 只缓存用于显示。
	theme            string
	providerName     string
	modelName        string
	daemonStatus     protocol.DaemonStatusParams
	configState      protocol.ConfigParams
	attachmentStatus protocol.AttachmentStatusResult

	// Bubble Tea 基础组件。Welcome/Help 已是 child model；Chat 组件归属 pages/chat.Model。
	menu welcomepage.Model
	help helppage.Model

	// Welcome 页面状态。
	welcomeCursor int

	// Chat 页面状态。root 仅持有页面 model 与 transcript；运行态在 pages/chat.Model 内。
	chat chatpage.Model

	// Config 页面状态。页面内部状态归属 pages/config.Model；root 只负责 daemon/configState glue。
	config tuiconfig.Model

	// Help overlay 状态。
	showHelp bool

	// Compact UI mode: auto compact should say model will continue; manual /compact should not.
	compactAuto bool

	// Usage/context 统计，只用于状态栏展示。
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

type guardConfirmView = chatpage.GuardConfirmView

type chatMsg = chatpage.Msg
type msgRenderCache = chatpage.MsgRenderCache
type userMessageContent = chatpage.UserMessageContent
