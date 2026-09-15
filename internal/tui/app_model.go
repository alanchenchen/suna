package tui

import (
	"time"

	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
	helppage "github.com/alanchenchen/suna/internal/tui/pages/help"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	welcomepage "github.com/alanchenchen/suna/internal/tui/pages/welcome"

	"github.com/alanchenchen/suna/internal/tui/components/combobox"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
)

/*
TUI 纯前端，无业务逻辑。

设计原则（见 docs/architecture.md 的 TUI / protocol 边界）：
  - TUI 不持有任何业务逻辑、状态、数据库连接
  - TUI 只做两件事：渲染 UI、通过 local transport 与 daemon 通信
  - 所有输入 → protocol request → local JSON-RPC framing → daemon
  - daemon protocol notification 和 method response → typed tea.Msg → 渲染到终端
*/
type TUI struct {
	// Bubble Tea 运行时与 daemon I/O。副作用必须通过 tea.Cmd 或 notification pump 回到 Update。
	localCli    *tuitransport.Client
	i18n        *translator
	program     *tea.Program
	notifyQueue *notificationQueue

	// clipboardWrite 是选区复制写入系统剪贴板的函数，测试可注入 fake 避免依赖
	// 真实桌面剪贴板（headless CI 无 X11/Wayland 服务时 WriteText 会失败）。
	clipboardWrite func(string) error

	// 根应用状态：只负责页面路由和全局尺寸。
	mode     uipage.Page
	prevMode uipage.Page
	width    int
	height   int
	ready    bool

	// terminalBackground 是 Bubble Tea 查询到的终端背景，用于主题深浅适配。
	// 同时保存深浅与真实颜色：真实颜色让对比度基于用户实际底色计算，
	// 终端不支持查询时（零值）按深浅取近似基准。
	terminalBackground themesys.Background
	// themeSpecs 是已加载的用户主题（~/.suna/themes/*.toml）。
	// 打开主题列表时重新扫描，不需要重启即可看到新增/修改的主题。
	// 解析失败的主题也保留在列表中（带 Err），供 UI 展示原因。
	themeSpecs []themesys.Spec
	// themeBeforePreview 记录打开主题列表前的主题名，供 Esc 取消预览时恢复。
	themeBeforePreview string
	// launchCWD 在 TUI 创建时缓存，避免每次 View 更新终端标题都查询文件系统。
	launchCWD string
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
	welcomeCursor        int
	sessions             []protocol.SessionInfo
	currentSession       protocol.SessionInfo
	currentRunCanControl bool
	// cancelling 表示已发出取消请求或收到 daemon 的 cancelling 状态，终态前保持输入锁定。
	cancelling bool
	// cancelNoticeRunID 记录已经展示取消终态提示的 run，避免重复通知追加相同文案。
	cancelNoticeRunID string
	// completedRunID 防止终态通知先到时，迟到的同一 run 快照重新激活 Loading。
	completedRunID string
	// runStartedAt、activeRunID 与 runHadToolCall 只服务于当前 TUI 的本轮耗时展示，不参与协议或持久化。
	runStartedAt         time.Time
	activeRunID          string
	runHadToolCall       bool
	handoffRole          string
	resumeSessionID      string
	welcomeActivePicker  bool
	welcomeIdlePicker    bool
	welcomeDeleteConfirm bool
	welcomeDeleteID      string

	// Chat 页面状态。root 仅持有页面 model 与 transcript；运行态在 pages/chat.Model 内。
	chat chatpage.Model

	// retryDeadline 仅用于展示模型自动重试的实时倒计时；真实重试时序始终由 daemon/Runner 控制。
	retryDeadline    time.Time
	retryAttempt     int
	retryMaxAttempts int

	// Config 页面状态。页面内部状态归属 pages/config.Model；root 只负责 daemon/configState glue。
	config tuiconfig.Model
	// 等待 daemon 确认配置写入后展示的一次性配置提示，避免保存失败时提前提示。
	pendingConfigNotice string
	// modelPickerProvider 记录模型选择浮层正在拉取列表的 provider；
	// 收到 config.models_result 通知时按 provider 匹配，避免跨 provider 误填充。
	modelPickerProvider string
	// modelPickerOpen 表示 provider 表单的模型选择浮层是否打开。
	// 独立于 chat 页的 ModelPickerOpen（chat 页浮层仍是 Bubbles list）。
	modelPickerOpen bool
	// modelPickerLoading 表示模型选择浮层正在等待 daemon 拉取列表。
	modelPickerLoading bool
	// modelPickerError 是模型列表拉取失败的错误提示，展示在浮层内。
	modelPickerError string
	// modelCombobox 是 provider 表单 model 字段的同步选择器：输入即过滤，
	// 无异步消息链路；候选为空时输入值即自定义模型名。
	modelCombobox combobox.Model
	// modelsCache 缓存 provider 的模型列表，避免同一 provider 重复拉取。
	modelsCache map[string][]string

	// Help overlay 状态。
	showHelp bool

	// Compact 计时，只用于压缩阶段的状态行展示。
	compactStartedAt time.Time

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
	lastTextStreamAt time.Time
	// lastPasteAt 用于让终端已经传入的 PasteMsg 优先于 Ctrl+V 剪贴板图片兜底，避免文本粘贴被图片读取抢占。
	lastPasteAt time.Time

	// transcript 同步由 daemon 通知触发时按帧合并，避免流式输出和工具事件风暴反复完整重渲染。
	transcriptSyncDirty     bool
	transcriptSyncScheduled bool

	// transcriptSyncPending 记录“主题切换需要重建 transcript”，由 Update 边界转成 tea.Cmd。
	// 不能在 Update 内直接 program.Send：bubbletea 的消息 channel 无缓冲，
	// 事件循环处理 Update 时不会回读，会永久阻塞（连 Ctrl+C 都会失效）。
	transcriptSyncPending bool

	// selectionDirty 标记拖动中选区范围变化（内容未变）：帧门 flush 时走轻量路径
	// （只重写窗口行，不重建块列表），与 transcriptSyncDirty（内容变化）区分。
	selectionDirty bool

	// chatSpinnerTicking 保证 loading/compacting 的 spinner 只有一条 tick 链；Join running session 时也会按需启动。
	chatSpinnerTicking bool

	// petFrame 是宠物动画当前帧；petTicking 保证全局只有一条 pet tick 链，
	// 离开 Chat/Welcome 时终止，避免多条链相互打架。
	petFrame   int
	petTicking bool
	// petHappyUntil 非零时宠物显示开心眼（run 完成后的奖励帧），到期后回到 idle。
	petHappyUntil time.Time

	// selection 是 transcript 内容行上的鼠标选区（拖动中或已定格），
	// 锚定内容行索引，滚动跟随内容不丢失；y 复制、单击/Esc 清除。
	selection Selection
	// selectionEdgeTicking 保证 edge scroll 只有一条 tick 链（拖动到边缘时启动）。
	selectionEdgeTicking   bool
	selectionEdgeDirection int
	lastSelectionMouseY    int
	// copyFeedbackUntil 非零时状态栏显示“已复制”反馈，到期后消失。
	copyFeedbackUntil time.Time
	copyFeedbackText  string
}

type guardConfirmView = chatpage.GuardConfirmView

type chatMsg = chatpage.Msg
type msgRenderCache = chatpage.MsgRenderCache
type userMessageContent = chatpage.UserMessageContent
