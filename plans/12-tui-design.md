# 12 — TUI 交互设计

> 最后更新: 2026-06-05
> 本文档以当前 `internal/tui` 实现为准，描述已经可用的 TUI 行为、仍需保留的设计约束，以及尚未实现的功能。
> 范围只包含 TUI 前端视觉、布局、交互和 protocol/local transport 展示数据，不定义 daemon/agent 业务逻辑。
> 本轮 TUI 已按 Bubble Tea 最佳实践拆分为 root/pages/components/events/transport；稳定维护约定见 [`../docs/tui.md`](../docs/tui.md)。

---

## 当前结论

TUI 现在已经能跑通基本使用流：启动后进入 Welcome，进入 Chat 后可以发消息、看流式回复、看工具执行、处理 AskUser 选项、处理 Guard confirm、切换模型、手动 compact、查看 active memory，并能通过 Config 管理模型连接、语言、主题、Guard Mode 和 Workspace。

当前实现不是完整 GUI 化配置中心，设计重点应收敛到“能稳定对话 + 能完成模型配置 + 能清楚展示 agent 运行状态”。复杂配置和远期多渠道能力不要塞进 TUI MVP。

当前页面只有四个 mode：

| Mode | 文件 | 状态 | 说明 |
|---|---|---|---|
| Welcome | `internal/tui/pages/welcome`, root adapter | 已实现 | 宠物 logo、版本、模型/daemon 概览、Guard/Workspace 状态、菜单入口 |
| Chat | `internal/tui/pages/chat`, `chat.go`, `chat_render.go`, `chat_view.go` | 已实现 | 对话、流式输出、工具事件、AskUser、命令、状态栏 |
| Config | `internal/tui/pages/config`, `config.go`, `config_forms.go`, `config_view.go` | 可用但不完整 | 模型连接、凭证、context window、语言、主题、Guard Mode、Workspace |
| Help | `internal/tui/pages/help`, root adapter | 已实现 | 功能发现 + 场景式说明：Start Here、Chat、Commands、Details、Tools & Safety、Config、Troubleshooting |

---

## 实现状态矩阵

| 功能 | 当前状态 | 关键文件 | 备注 |
|---|---|---|---|
| Bubble Tea 主状态机 | 已实现 | `app_model.go`, `app_update.go`, `app_view.go`, `pages.go` | 页面路由、AltScreen、local notification 分发 |
| Welcome 菜单 | 已实现 | `pages/welcome`, root adapter | 有 last session 时 `Resume` 置顶；状态区显示版本、模型、usage、uptime、memory、Guard、Workspace |
| Chat 布局 | 已实现 | `pages/chat`, `chat_render.go`, `chat_view.go` | mini pet 顶栏、viewport、textarea、命令建议、底栏、复制模式提示 |
| 流式回答 | 已实现 | `events/`, `events.go`, `chat.go` | `agent.stream` 追加 assistant 内容；高频 delta 由 event batcher 合并 |
| Reasoning/Thinking | 已实现 | `events/`, `chat.go`, `chat_render.go` | 默认折叠，`Ctrl+R` 展开 |
| Tool 展示 | 已实现 | `components/toolview`, `tool.go`, `chat_render.go` | running/done/error，展示 Guard 与 file metadata 注解，归入 Suna 回合，`Ctrl+T` 打开 tool detail overlay |
| AskUser 选项 | 已实现 | `pages/chat`, `chat.go` | options 为 `[]string`，支持上下选择、Enter、数字输入、自定义答案 |
| Slash command | 已实现 | `pages/chat/commands.go`, `chat.go`, `local_commands.go` | `/new`, `/model`, `/memory`, `/skills`, `/compact`, `/config`, `/help` |
| Model picker | 已实现 | `pages/chat/model_picker*.go`, `chat_render.go` | `/model` 无参数时打开列表 |
| Markdown 渲染 | 已实现 | `markdown.go` | Glamour v2，assistant 和 expanded thinking 使用 |
| Compact 面板 | 已实现 | `ui.go`, `chat.go`, `local_commands.go` | 展示 before/after、context window 百分比、压缩消息数 |
| Config 模型管理 | 已实现基础版 | `pages/config`, `config.go`, `config_forms.go`, `config_view.go` | add/edit/delete/activate model；底部根据当前选中行显示 context-aware help |
| Config 本地文件入口 | 已实现 | `pages/config`, `open_dir_*.go` | Config Home 展示 config/credentials 路径，`Enter` 打开 config 目录 |
| Provider kind 选择 | 已实现 | `pages/config`, `config_forms.go` | openai-compatible / openai / anthropic |
| Context Window | 已打通 | `pages/config`, `protocol/messages.go`, `chat_render.go` | config/protocol/顶栏/compact 均可使用 |
| Credentials | 已实现基础版 | `config_forms.go`, daemon config protocol | API Key 通过 protocol 保存到 credentials，不在 TUI 明文展示 |
| 语言切换 | 已实现 | `i18n.go`, `i18n_keys.go`, `pages/config` | 中文/英文内置翻译，Config 可切换 |
| 主题切换 | 已实现 | `theme.go`, `pages/config` | auto/dark/light |
| Provider Test | 未实现真实 ping | `config.go` | `T` 只显示 `local config only; API ping not implemented` |
| Guard confirm overlay | 已实现 | `pages/chat/guard*.go`, `chat.go`, `chat_render.go` | 独立面板显示 tool/risk/reason/suggestion/params，支持 `Y/A` approve、`N/R/Esc` reject |
| Copy mode | 已实现 | `app_view.go`, `chat_render.go`, `pages/help` | 默认保留鼠标滚轮，`Ctrl+Y` 临时关闭鼠标捕获以便终端原生选中文本 |
| Guard Mode 配置 | 已实现 | `pages/config`, `config.go` | Config home 可切换 ask→smart→auto→readonly |
| Workspace 配置 | 已实现 | `pages/config`, `config_forms.go` | Config home 可编辑/清空 `guard.workspace`，保存后回到 Config home |
| Config 删除确认 | 已实现 | `pages/config/delete.go`, `config.go` | 按钮式确认，默认 Cancel，`←→` 选择、`Enter` 确认、`Esc` 取消 |
| 图片附件 | 已实现 MVP | `components/attachment`, `pages/chat/attachments.go` | Ctrl+V 检测图片 path/url/data URI，确认后加入附件列表；只发送 path/url/attachment ref |
| 附件空间管理 | 已实现基础版 | `pages/config`, protocol `attachment.*` | Config Home 展示附件占用，Enter 一键清理 |
| 外部 i18n 文件加载 | 未接入主流程 | `i18n.go` | 有 `LoadLocale`，但当前主要用内置翻译表 |
| Config 高级项 | 未实现 | `config.go` | 未编辑 guard rules/hooks/max_model_rps |

---

## 设计原则

1. **当前实现优先**：文档描述必须和 `internal/tui` 当前行为一致，未实现功能放入“未实现清单”，不能混在主流程里。
2. **Chat 优先**：Chat 是核心页面，常驻信息只保留身份、模型、context、输入和运行状态。
3. **TUI 纯前端**：TUI 只持有 UI 展示/输入状态，不持有业务逻辑、数据库连接或模型执行逻辑，只渲染 UI 并通过 local transport 承载 protocol 与 daemon 通信。
4. **配置足够可用**：MVP 只要求能配置模型连接、凭证、context window、语言和主题；高级配置后续分组补齐。
5. **信息不重复**：Chat 顶栏显示 provider/model/context/连接状态；底栏只显示 token 和速度；快捷键通过 help 查看。
6. **全程 i18n**：用户可见文本必须走 `internal/tui/i18n_keys.go` 或 translator，不在页面代码里散落硬编码文案。
7. **不伪造 usage**：token/cache/context/speed 只能来自 provider usage 或 daemon 状态；没有 usage 时显示未知。
8. **菜单优先，快捷键次要**：常用路径优先通过 `↑↓`、`Enter`、`Esc` 完成；快捷键保留为高级入口，但不在主 UI 中堆叠展示。

---

## 页面模型

当前实现流程：

```text
suna 启动
  │
  └─ 连接 daemon → Welcome
                  │
                  ├─ New
                  │   ├─ 无模型配置 → Config setup form → Welcome
                  │   └─ 有模型配置 → Chat
                  ├─ Resume → Chat
                  ├─ Config → Config
                  └─ Help → Help
```

说明：

- 当前没有独立 `Setup` mode。首次配置复用 Config 的 provider form，并通过 `configSetupMode` 标记。
- 当前没有独立 `Compact` 页面。compact 是 Chat 命令和结果面板。
- 当前没有全局 footer。Chat 有 token 底栏，Welcome 有状态概览，Config/Help 只显示页面内 help 文案。
- `Ctrl+C` 是唯一全局退出快捷键。`Esc` 用于取消、关闭浮层、返回上一层或回 Welcome。
- Welcome/Config/Help 都优先展示用户可发现的菜单和说明；隐藏快捷键只保留在 Help 中，不在主 UI 堆叠。

---

## i18n

TUI 用户可见文本必须集中在 `internal/tui/i18n_keys.go` 或 translator 中。

已实现：

- 内置中文和英文翻译表。
- Config 中可切换语言，并通过 `config.set update_general` 持久化。
- translator 支持 fallback 到英文。
- key.Binding 的 help 文案来自 i18n。

当前限制：

- `translator.LoadLocale(path)` 存在，但主启动流程未接入外部 locale 文件加载。
- 部分动态状态仍需要继续检查，避免在新代码中新增硬编码英文/中文提示。

允许硬编码的内容：

- i18n key。
- 图标和状态符号，例如 `●`, `○`, `✓`, `✗`, `⋯`。
- 快捷键字面量，例如 `Ctrl+N`, `Esc`。
- slash command 名称，例如 `/new`, `/compact`。
- Provider/Model/API 返回的动态值。

---

## 视觉语言

### 宠物 Logo

宠物 logo 已落地在 `pet.go`，分为 Welcome 完整版和 Chat mini 版。

规则：

- Welcome 使用完整 pet，主要做品牌入口。
- Chat 顶栏使用 3 行 mini pet，承担运行时状态。
- Chat mini pet 状态：idle / working / thinking。
- 不使用 1 行 mini fallback，避免失去宠物识别度。

状态映射：

| 状态 | 触发条件 | 表现 |
|---|---|---|
| idle | `loading=false` | 普通眼睛 |
| working | LLM 响应或 tool 执行 | working pet + 彩色连接点 |
| thinking | reasoning 阶段 | thinking pet + brand 色连接点 |

### 配色

当前基础色集中在 `ui.go`：

| 语义 | 变量 | 用途 |
|---|---|---|
| Brand | `ColorBrand` | logo、spinner、cursor、selected |
| Dim | `ColorDim` | 辅助文字、边框、未知状态 |
| User | `ColorUser` | 用户消息、input token |
| Agent | `ColorAgent` | assistant、output token、connected |
| Tool | `ColorTool` | tool pill、working 状态 |
| Error | `ColorError` | 错误、失败状态 |
| Highlight | `ColorHL` | 标题、高亮文本 |

主题系统已支持 `auto` / `dark` / `light`，位于 `theme.go`。

---

## Welcome

定位：入口页，只负责展示状态摘要和进入 Chat/Config/Help。

当前实现：

- 使用 `bubbles/list` 渲染菜单。
- 菜单隐藏 title/status/pagination/filter help，只保留入口项。
- 菜单项不显示单字母快捷键，避免 Welcome 变成快捷键学习页。
- 展示 pet、产品名、subtitle、setup hint。
- 展示版本、模型、今日 usage、uptime、memory、Guard Mode、Workspace 状态。
- 不展示 session stats；Welcome 只通过菜单表达“是否能恢复上次会话”，避免右上状态区出现对用户价值低的 `active/done` 计数。
- 未配置模型时仍允许进入 Config/Help，New 会引导到 Config setup form。
- Resume 仅在 daemon status 有 `Sessions.LastID` 时显示。
- 有 Resume 时置顶显示，方便用户优先继续上次对话；没有 Resume 时 New 仍是首项。

菜单项：

| 条目 | 条件 | 行为 |
|---|---|---|
| Resume | 有 last session 时显示并置顶 | 调 `session.restore` 并进入 Chat |
| New | 总是显示 | 无模型时进入 setup form，有模型时新建会话并进入 Chat |
| Config | 总是显示 | 进入 Config home |
| Help | 总是显示 | 进入 Help 页面 |

快捷键：

| Key | 行为 |
|---|---|
| `↑↓` / `j k` | 菜单移动 |
| `Enter` | 选择 |
| `Ctrl+C` | Quit |

说明：Welcome 不再保留隐藏的 `n`、`r`、`Ctrl+O` 快捷键；入口都通过菜单项完成。

---

## Chat

Chat 是当前 TUI 的主界面，代码入口在 `chat.go` 和 `chat_render.go`。

### 布局

当前布局：

```text
mini pet + provider/model + ctx used/window + connection dot
────────────────────────────────────────────
viewport: messages / reasoning / tools / ask user / model picker
────────────────────────────────────────────
textarea
command suggestions, only when typing slash command
token status bar / copy mode hint
```

高度计算由 `layoutChat()` 处理：

- viewport 使用剩余高度。
- textarea 动态高度，最大 6 行。
- 命令建议最多 4 项。
- viewport `SoftWrap=false`，Markdown 由 Glamour 按宽度 wrap。

### 顶栏

顶栏展示：

- 3 行 mini pet。
- 当前 `provider/model`。
- `ctx used/window`。
- local transport 连接点。

规则：

- `context_window` 优先来自 active model 配置或 daemon stream/status。
- `context_tokens` 只在 daemon/provider 提供 usage 时更新；未知时显示 `ctx ?/128.0k` 这类形式。
- 顶栏不显示快捷键，不显示 token 输入输出。

### 底栏

底栏展示最近一轮 usage：

```text
↑3.2k ↓1.8k ⟳0.8k · 45t/s
```

规则：

- 有 provider usage 时显示 input/output/cache/speed。
- 没有 usage 时显示 `↑? ↓? ⟳? · ?t/s`。
- 不在 TUI 本地估算 token，避免把近似值伪装成真实值。
- 进入 copy mode 时追加 `copy mode [Ctrl+Y/Esc]` / `复制模式 [Ctrl+Y/Esc]`，提示当前可用终端原生拖拽选择文本。

### 消息渲染

| 类型 | 当前行为 |
|---|---|
| user | inline 用户消息，原样文本，不走 Markdown |
| assistant | 标题 `Suna` + Glamour Markdown |
| reasoning | 归入当前 `Suna` 信息块，Thinking box 默认折叠，`Ctrl+R` 展开，展开时走 Markdown |
| tool | 归入当前 `Suna` 信息块，默认只显示 intent 和错误摘要；`Ctrl+T` 打开当前选中 tool 详情 |
| system | dim 色系统消息 |
| error | error 色错误消息 |

Suna 回合分组规则：

- 连续的 `reasoning`、`tool`、`assistant` 都属于同一个 `Suna` 信息块，只渲染一次 `Suna` 标题。
- 用户消息和后续 `Suna` 信息块之间保留空行，避免 thinking/tool 贴在用户消息下方造成归属不清。
- `Thinking box`、tool tree、running tool 行和 assistant 正文都相对 `Suna` 标题向内缩进，形成清楚层级。
- `system` 和 `error` 不并入 `Suna` 信息块，会打断当前 Suna 回合。

### Loading 和阶段

当前 phase：

| Phase | 含义 |
|---|---|
| `phaseIdle` | 无运行中请求 |
| `phaseFirstLLM` | 用户消息已发，等待首包 |
| `phaseLLM` | 正在接收普通回答 |
| `phaseThinking` | 正在接收 reasoning |
| `phaseTool` | 正在执行工具 |
| `phaseWaitingAfterTool` | 工具已完成，等待下一轮模型响应 |

Chat pet 根据 phase 切换。运行中状态在当前 Suna 回合底部只显示一条 current status line，避免 thinking、tool running、phase spinner 同时跳动。tool 完成后回到 LLM phase；stream done 后 reset 到 idle。

Current status line：

- 只在 `loading=true` 且 phase 已开始时显示。
- 格式为 `spinner + 当前状态 + elapsed + Esc cancel`。
- `phaseFirstLLM` 显示等待 LLM。
- `phaseLLM` 显示正在回复。
- `phaseThinking` 显示思考中，但不额外插入空 thinking box。
- 有 running tool 时优先显示 `Executing tool... intent`。
- 历史 reasoning/tool 事件仍保留在 Suna 回合里，运行中只强调“当前正在做什么”。
- Tool 展示使用并发状态模型：`tool_start` 创建 running 项，`tool_guard` 更新 Guard 注解，`tool_end` 更新 done/error，不等待前一个 tool 完成。
- Guard 注解显示为工具行下方 `Guard` badge，包含决策、风险和短 reason；完整 reason/suggestion 在 tool detail overlay 展示。
- 文件变更 metadata 显示为工具行下方 `File` badge，路径、operation、`+/-` 行数使用高亮/背景色，避免重要变化淹没在 dim 文本里。
- Subtask 内部 tool 通过 `spawn:<parentToolCallID>:<subToolCallID>` 归到父 spawn 下，渲染为树形结构；subtask 的 `tool_guard` / `guard_confirm` 使用同一 id，因此 Guard 注解和用户确认结果挂到对应子工具行。
- 主聊天流不渲染完整 params/result，避免频繁展开导致 viewport 重排卡顿。
- Tool error 必须在主线展示短错误摘要；完整 params/result 只在详情面板展示。
- Tool detail 使用 overlay 面板，不再插入聊天流；overlay 显示当前类型、当前位置、tool、intent、params/result，并提示 `↑/↓ switch · Ctrl+T/Esc close`。
- 聊天主线不显示选中箭头，避免 overlay 打开后 chat 与详情焦点混淆。
- `spawn` 在主线标记为 `Subtask · ...`，subtask 内部工具只显示自身 intent，并依靠树结构表达归属。
- Subtask detail 优先展示 `model`、`tools`、`task`，普通工具 detail 展示完整 params。
- Loading 状态只显示工具执行中和 running 数量，不再拼接当前 tool name/intent，避免和 tool tree 重复。
- Tool/subtask 固定 UI 文案走 i18n；主线只在父 spawn 显示 `Subtask` 标识，子工具依靠树结构表达归属，不重复加 `Subtask tool`。
- Tool detail overlay 底部提示根据当前位置动态显示 `previous`/`next`，首项不显示 previous，末项不显示 next。
- Tool detail overlay 限制内容高度，避免 result 过长撑破 box；详情内容使用 `virtualLineSource` 虚拟滚动，只渲染可见窗口，不完整生成长 params/result 的 `[]string`。
- Chat 当前使用 `bubbles/viewport` 做显示窗口滚动，但 `syncContent()` 仍会生成完整聊天内容；这是显示层滚动，不是完整渲染虚拟化。后续若长会话卡顿，应优先复用 `virtual_scroll.go` 的 line source 思路拆分消息级虚拟渲染。
- Guard confirm payload 携带 `tool_call_id`；用户 reject 后，TUI 立即把对应 tool 标为 error，而不是只追加系统消息。

### AskUser Options

AskUser 已实现，不再是缺失项。

Protocol 数据结构：

```go
type AskUserParams struct {
    Question    string   `json:"question"`
    Options     []string `json:"options,omitempty"`
    ID          string   `json:"id"`
    AllowCustom bool     `json:"allow_custom"`
}
```

当前行为：

- 收到 `agent.ask_user` 后追加系统问题。
- 如果有 options，在 viewport 中渲染选项列表。
- `↑↓` 移动选项光标。
- 输入为空时按 `Enter` 直接选择当前选项。
- `allow_custom=true` 时允许输入自定义答案；普通 LLM 提问默认保持 true。
- `allow_custom=false` 时进入 choice-only，输入框锁定，只能用选项回答；用于严格 workflow 确认，例如 Skill workflow 的 review/enable 选择。
- 回复通过 `agent.askReply` 回传 daemon。

限制：

- options 当前只是 `[]string`，没有 `{label,value}`、默认值、禁用态或多选。
- 当前不支持鼠标点击选择。

### Guard Confirm Overlay

Guard confirm 使用独立 overlay 面板，不复用 AskUser 事件。

Protocol 数据结构：

```go
type GuardConfirmParams struct {
    Tool       string `json:"tool"`
    Risk       string `json:"risk"`
    Reason     string `json:"reason,omitempty"`
    Suggestion string `json:"suggestion,omitempty"`
    Params     string `json:"params,omitempty"`
    ID         string `json:"id"`
}
```

当前行为：

- 收到 `agent.guard_confirm` 后渲染独立 overlay 面板。
- 面板显示：tool name、risk level（颜色区分）、reason、suggestion、参数。
- 键位：`←→` / `j/k` 移动焦点，`Enter` 确认当前选项，`Y/A` 直接 approve，`N/R/Esc` 直接 reject。
- 面板底部明确显示 `Y approve · N/Esc reject · ←→ choose · Enter selected`。
- 默认选中 Approve，但 `Esc`/`N` 始终可直接拒绝；Guard 自身的 reject 不会进入 confirm overlay。
- 回复通过 `agent.guardReply` 回传 `approve` 或 `reject`。
- 所有文案走 i18n（`i18n_keys.go`）。

说明：

- Guard confirm 是独立事件类型 `EventGuardConfirm`，不复用 AskUser。
- TUI 只负责展示和回传，不做安全判断。

### 命令

当前 TUI 有这些 slash command 入口：

| 命令 | 行为 |
|---|---|
| `/new` | 新建 session，清空当前 Chat 状态 |
| `/model [ref]` | 无参数时打开模型选择器；带 ref 时切换模型；没有 provider 前缀时使用当前 provider |
| `/memory` | 调 `memory.list`，显示当前 active memory |
| `/compact` | 调 `session.compact`，显示 compact 结果面板 |
| `/config` | 进入 Config home |
| `/help` | 进入 Help 页面 |

说明：`/model` 的无参/带参是同一个命令入口。

命令建议：

- 输入以 `/` 开头且第一个空格前仍在命令位置时显示。
- 最多显示 4 项。
- 命令建议可以只显示匹配的前几项；完整命令清单必须在 Help 页面展示。
- `↑↓` 选择，`Enter` 接受。
- `Tab` 不用于命令建议。
- Chat help overlay 只展示常用操作、常用命令和少量更多操作，不再默认铺开完整快捷键清单。
- Help 页面按场景组织：Start Here、Chat、Commands、Details、Tools & Safety、Config、Troubleshooting；快捷键只是场景说明的一部分。

命令识别规则：

- 只有命中已注册命令入口时才作为 TUI 本地命令处理。
- 未注册的 `/...` 输入会作为普通用户消息发送给 agent，不再显示 unknown command 错误。
- 已注册入口以 `allCommands()` 为准，Help 页面直接从该列表生成，避免指令文档遗漏。

当前未暴露的 protocol 能力：

- `/daemon status|stop|restart`
- `/trigger ...`
- `/skill ...`
- `/usage`
- `/memory search|facts|status`

这些不属于当前 TUI MVP。

### 图片附件

TUI 只在粘贴时检测图片，不新增 `/attach` 命令，也不自动扫描普通提示词里的路径。

支持：

- 本地图片 path：`.png`, `.jpg`, `.jpeg`, `.webp`, `.gif`。
- 远程图片 URL：明显图片后缀的 `http/https` URL。
- `data:image/...;base64,...`：确认后保存到默认数据目录的 `attachments/sha256-*.png`（当前默认 `~/.suna/attachments/sha256-*.png`），再作为 attachment ref。

交互规则：

- 粘贴图片 path/url 后弹确认；确认则加入附件列表且不进入输入框，取消则按普通文本插入输入框。
- 粘贴 data URI 后弹确认；确认则保存临时文件并加入附件列表，取消则丢弃，不进入输入框。
- 疑似非图片大段 base64 会被拦截并提示，避免污染上下文。
- 有附件时 `↑/↓` 进入附件选择模式，`Delete/Backspace` 触发删除确认，`Esc` 回到输入框。

发送规则：

- 发送 `protocol.SendMessageParams.Parts`，包含一个 text part 和若干 image part。
- image part 的 `AttachmentRef` 只使用 `path` 或 `url`。
- 发送成功后清空输入框和附件列表。

持久化规则：

- 本轮 provider request 会带完整图片内容。
- working memory / conversation_state / user_memory 不保存 raw media、base64、完整 path/url。
- 对话展示和恢复只保留用户文本与附件 metadata。

### Chat 快捷键

| Key | 行为 |
|---|---|
| `Enter` | 发送；有命令建议时接受建议；有 AskUser options 且输入为空时确认当前选项 |
| `Shift+Enter` / `Ctrl+J` | 换行 |
| `Esc` | 关闭 help；运行中则 cancel；输入为空则回 Welcome；输入非空则清空输入 |
| `Ctrl+T` | 打开/关闭 tool detail overlay；overlay 中 `↑/↓` 切换 tool |
| `Ctrl+R` | 展开/收起 reasoning/thinking 详情 |
| `Ctrl+Y` | 进入/退出 copy mode；copy mode 下关闭鼠标捕获，可用终端原生拖拽选择文本 |
| `PgUp` | viewport 上滚 |
| `PgDown` | viewport 下滚 |
| `↑↓` | 命令建议或 AskUser options 移动 |
| `?` / `F1` | Chat help overlay |
| `Ctrl+C` | Quit |

### 鼠标与复制

默认鼠标模式为 `tea.MouseModeCellMotion`：

- 鼠标滚轮事件交给 TUI，因此 Chat 和 Help 的 viewport 可以用滚轮滚动。
- 终端原生拖拽选择文本通常不可用，因为鼠标事件已被应用捕获。

复制模式使用 `copyMode` 状态：

- `Ctrl+Y` 切换 copy mode。
- copy mode 下 `View()` 将 `MouseMode` 改为 `tea.MouseModeNone`，把鼠标拖拽还给终端。
- copy mode 下用户可以用终端原生选择文本并复制。
- `Ctrl+Y` 或 `Esc` 退出 copy mode 后恢复 `tea.MouseModeCellMotion`，鼠标滚轮重新控制 viewport。

设计取舍：

- 终端协议中，应用接收滚轮依赖鼠标上报；启用鼠标上报后普通拖拽会被 TUI 捕获。
- 当前不尝试实现“默认同时支持滚轮和原生拖拽选择”，而是采用可解释、可恢复的双模式。
- 键盘滚动 `PgUp` 和 `PgDown` 在两种模式下都可用。

---

## Markdown

当前使用 `charm.land/glamour/v2`，代码在 `markdown.go`。

规则：

- assistant 内容使用 Glamour 渲染。
- expanded thinking 使用 Glamour 渲染。
- user 内容不渲染 Markdown，按普通文本展示。
- renderer 按宽度和主题缓存。
- Chat viewport `SoftWrap=false`，避免 Glamour wrap 后再次 wrap。
- TUI 不维护自定义 Markdown parser/table renderer；Markdown 语义尽量交给 Glamour。
- Markdown style 使用 Glamour typed `ansi.StyleConfig`，不使用 JSON style 字符串，避免样式解析失败后悄悄 fallback 到 Glamour 默认 `dark`。
- Markdown 风格目标接近主流 Glow/TUI Markdown：标题渲染为品牌色加粗文本，不显示原始 `#` 层级前缀；strong 高亮、blockquote 有左边界、列表保留简洁 bullet、inline code 有轻微背景。
- 代码块和 inline code 分开处理：inline code 保留轻微背景用于强调路径/命令；code block 使用 Chroma theme 做语言感知高亮。
- fenced code block 有语言标注时保留原语言；没有语言标注时预处理为空 fence 补 `bash`，让 shell/目录树/命令输出类内容获得一致样式。
- 深色主题代码块使用 `monokai` Chroma theme，浅色主题使用 `github` Chroma theme；不自定义 token 级 Chroma 背景，避免回到大面积红底问题。
- 深色/浅色主题共用同一套 semantic palette 字段，不为单一背景硬编码颜色。

注意：当前实现没有 50ms debounce 渲染队列。流式 chunk 到达后直接更新消息，最终渲染策略应以后续性能优化为准，不再把 debounce 写成已实现要求。

---

## Compact 面板

`/compact` 当前通过 protocol 调 `session.compact`，结果由 `session.compact_result` 推回 TUI。

面板展示：

- compact 完成提示。
- before tokens 和 context window 百分比。
- after tokens 和 context window 百分比。
- 保留内容说明：Session State + 动态最近对话窗口。
- 被折叠进 Session State 的历史消息数。
- 被截断工具输出数量。
- no-op 说明：当没有可折叠内容且没有旧 Session State 时，提示暂无可压缩内容。

交互原则：

- Compact 是模型上下文管理，不是 UI 历史删除。
- 手动 compact 成功后，agent working memory 会立即变为 Session State + budget-aware recent messages。
- TUI chat transcript 不替换、不删除旧消息，只追加 compact 结果面板。
- 面板必须说明“较早聊天仍保留在界面中；模型将基于 Session State 和最近消息继续”，避免用户误以为模型仍逐条持有所有旧消息。
- 自动 compact 不弹出独立交互，也不显示结果面板；daemon 复用 `session.compact_result` 的 lifecycle 字段：`running=true` 显示 loading，`running=false` 结束 loading，`running=false,error` 清 loading 并追加错误消息。
- 自动 compact 失败时不使用 fallback、不继续模型请求，TUI 直接显示错误并解锁输入。

限制：

- TUI 没有 auto-compact 开关。
- Compact 不是独立页面。

### Restore 与错误恢复

`session.restore` 恢复会话时，daemon 先逐条推送 `session.restore_message`，再推送 `session.restore_status`。如果 `compacted=true`，TUI 在恢复消息末尾追加系统提示，说明较早对话已压缩为 Session State，当前只展示最近消息。

模型或上游服务错误导致 run 停止时，daemon 在 `agent.stream` 的完成事件中用结构化字段表达错误和恢复能力：`error=true`、`resume_available=true/false`。TUI 不解析错误文本；只有 `resume_available=true` 时才显示输入区提示。此时输入框为空按 `Enter` 调 `agent.resumeRun`，输入了新消息则继续走 `agent.sendMessage`。

---

## Config

Config 当前是可用的模型连接管理，不是完整配置中心。

入口：

- Welcome 菜单 `Config`。
- Chat `/config`。
- 无模型配置时，Welcome `New` 进入 setup form。

当前页面层级：

```text
Config home
  ├─ Model Connections → models list
  │    ├─ Enter → detail
  │    ├─ Add Model Connection → provider kind overlay → provider form
  │    └─ Space → activate
  ├─ General
  │    ├─ Language → toggle zh/en
  │    ├─ Theme → toggle auto/dark/light
  │    ├─ Guard Mode → toggle ask→smart→auto→readonly→ask
  │    └─ Workspace → workspace form
  └─ Local Files
       ├─ Config path
       ├─ Credentials path
       └─ Open Config Folder → system file manager
```

### Config Home

当前展示：

- Model Connections section。
- Active model。
- Providers summary。
- Language。
- Theme。
- Guard Mode。
- Workspace 状态：未配置显示 disabled，已配置显示路径。
- Config file path: `config.DefaultConfigPath()` 的实际绝对路径，当前默认 `~/.suna/config.toml`。
- Credentials file path: `config.DefaultCredentialsPath()` 的实际绝对路径，当前默认 `~/.suna/credentials.toml`。
- Open Config Folder 可选菜单项，按 `Enter` 打开 `config.DefaultDataDir()` 指向的目录，当前默认 `~/.suna`。

当前没有单独 General 子页。Language、Theme、Guard Mode 在 home 直接切换；Workspace 在 home 进入单字段表单。

Config 页面底部 help 是 context-aware：根据当前选中行显示下一步动作，例如 Language 显示切换语言、Guard Mode 显示 mode 循环顺序、Workspace 显示编辑/清空语义、model 行显示 `Enter` 详情和 `Space` 激活。

本地文件交互原则：

- 不新增快捷键，沿用 Config Home 的 `↑↓` + `Enter` 菜单选择。
- TUI 只通过 `internal/config/paths.go` 派生 config/credentials/data dir 展示路径，不直接拼 `$HOME/.suna`。
- TUI 只打开配置目录，不默认打开编辑器或具体文件，避免跨平台编辑器偏好和阻塞问题。
- 打开目录逻辑封装在 `open_dir_*.go`，UI 层只调用 `openDirectory(path)`。
- Go 标准库没有统一的“打开系统文件管理器”API；当前实现使用 `os/exec` 直接启动平台命令，不经过 shell。

### Models List

当前展示：

- 每个 model 以 `provider/model` 为 ref。
- 标记：`◉` active，`○` inactive，`!` incomplete。
- 摘要：active、missing api key、provider、model、context window、endpoint configured、strengths。
- 列表按 ref 排序。
- 列表底部有 `Add Model Connection` 可选菜单项，按 `Enter` 新增模型。

操作：

| Key | 行为 |
|---|---|
| `Enter` | 进入 detail，或选择 `Add Model Connection` 新增 model |
| `Space` | 激活当前 model |
| `Esc` | 返回 home |

说明：`A` / `E` / `D` / `T` 已移除，避免 Config 页变成隐藏快捷键记忆游戏；`Space` 保留为高频激活 shortcut，并在 help 中展示。

### Detail

当前展示：

- Status。
- Provider type。
- Endpoint。
- API Key 是否 configured。
- Model。
- Context Window。
- Last check。
- Edit / Check Connection / Delete 作为可选菜单项；未激活模型额外显示 Activate。
- Action row 有语义颜色：新增/激活为 agent 色，删除为 error 色，进入/打开类动作为 brand 色。

操作：

| Key | 行为 |
|---|---|
| `Enter` | 选择 Edit / Activate / Check Connection / Delete 等动作 |
| `Esc` | 返回 models list |

说明：进入 detail 时默认焦点是上下文感知的：配置不完整或已激活时选中 `Edit`；配置完整但未激活时选中 `Activate`。已激活模型不再显示 `Activate` 操作，避免无意义重复操作。

删除确认：

- 使用按钮式确认，不再依赖 `Y/N`。
- 默认选中 `Cancel`，安全优先。
- `←→` / `↑↓` / `Tab` 切换，`Enter` 确认，`Esc` 取消。
- 确认面板内自带操作说明，页面底部不重复显示确认说明。

Provider Test 当前未实现真实 API ping。文档和 UI 都应把它视为占位功能。

### Provider Kind Overlay

新增 model 时先选择 provider kind：

- openai-compatible
- openai
- anthropic

选择后进入 provider form。`openai` 表示 OpenAI Responses 协议；`anthropic` 表示 Anthropic Messages 协议；`openai-compatible` 会让用户填写自定义 provider ID，并按 OpenAI-compatible Chat Completions 协议调用。TUI 会为 `openai` 预填 `https://api.openai.com/v1`，为 `anthropic` 预填 `https://api.anthropic.com`，但用户可以修改为中转站地址；daemon/core 不会补默认 endpoint。

### Provider Form

Provider form 是独立表单视图，不再覆盖在 detail 列表之上，避免用户误以为底层 detail 操作仍可点击或可选。

当前表单字段：

| 字段 | 状态 | 说明 |
|---|---|---|
| Provider | 必填 | `openai`/`anthropic` 是保留协议 ID；其他名称按 OpenAI-compatible Chat Completions provider 处理 |
| Model | 必填 | 实际模型 ID |
| API Key | setup 必填，编辑可留空 | 密码回显；留空表示不修改已有凭证 |
| Endpoint | 必填 | 所有 provider 都必须显式保存 `base_url`；TUI 只负责预填官方 URL，用户可修改；非空时校验 URL |
| Context Window | 可空 | 非空必须为正整数；保存到 `context_window` |
| Strengths | 可空 | 逗号分隔，保存为 `[]string`，用于路由偏好 |

Reasoning 编辑：

- 模型 detail 页有 `Edit Reasoning`。
- TUI 提供 `Clear Reasoning`、`GPT`、`Claude`、`DeepSeek V4`、`Custom Reasoning`。
- preset 不会保存 preset ID，只写入最终 `models.reasoning` 对象。
- GPT preset 根据 provider 协议生成不同格式：`provider = "openai"` 写 Responses 形态 `reasoning.effort`；其它 provider 写 Chat Completions 形态 `reasoning_effort`。
- Chat/Welcome 只在 `models.reasoning` 非空时展示反向匹配到的 label；匹配不到显示 `Custom Reasoning`。

保存行为：

- 通过 `config.set` 发送 `upsert_model`。
- API Key 独立传给 daemon，由 daemon 写 credentials。
- setup mode 保存时设置 `ActiveModel`。
- 保存后等待 daemon 推送 `config.get`/status 更新 TUI 状态。

当前和理想设计的差异：

- 没有拆成 Provider 表单和 Model 表单，当前是一张 model connection 表单。
- 没有独立 Provider ID 派生规则和展示名字段。
- 当前 `Provider` 字段同时承担 provider 标识，后续如要支持一个 provider 下多个 model，需要重新设计数据结构或表单层级。

### Setup Mode

当前 setup mode 是 Config form 的一个标记，不是独立页面。

规则：

- 无模型配置时，Welcome 选择 New 会打开 Config provider form。
- setup mode 下 API Key 必填。
- `Esc` 取消 form 后返回 Welcome，不退出 TUI。
- 保存成功后设置 active model。

### General

当前 General 包含四个轻量偏好：

- Language: zh/en。
- Theme: auto/dark/light。
- Guard Mode: ask/smart/auto/readonly。
- Workspace: 本地文件和 exec 的 workspace 边界。

Guard Mode 切换通过 `config.set` 持久化到 `guard.mode` 配置。TUI 中显示当前 mode 和简要说明。

Workspace 通过单字段表单编辑：

- 输入绝对路径或 `~/...` 路径，保存到 `guard.workspace`。
- 留空保存会关闭 workspace 边界。
- 保存成功后回到 Config home。
- 保存失败（目录不存在或不可访问）时停留在表单并显示 daemon 返回的 `config.error`。
- TUI 只负责输入和展示，目录存在性和 symlink 规范化由 daemon/config 层校验。

未实现：

- 单独 General 页面。
- Guard rules 编辑。
- Hooks 编辑。
- max_model_rps。

---

## Help

Help 有两个形态：

- Chat/Config overlay：`?` / `F1` 打开。
- 独立 Help 页面：Welcome 中选择 Help 或 Chat 输入 `/help`。

当前内容：

- 顶部 `Start Here` 帮助用户发现核心入口：输入 `/` 浏览命令、`/config` 配置模型/安全/workspace、`Ctrl+T`/`Ctrl+R` 排查工具和 reasoning。
- 展示 Chat 通用快捷键，包括发送、换行、滚动和 copy mode。
- 展示当前 slash command，命令列表由 `allCommands()` 生成。
- 展示 Details：`Ctrl+T` tool detail、`↑/↓` 切换 tool、`Ctrl+R` reasoning detail。
- 展示 Tools & Safety：说明高风险工具可能触发确认，Workspace 可限制本地文件和 exec 操作。
- 展示 Config：说明 Config 导航、model list 中 `Space` 激活、Workspace 留空关闭边界。
- Help 页面使用 viewport，可滚动。

当前不足：

- Help 仍是文字说明，不支持搜索或跳转目录。
- Help 不展示高级 guard.blocked/allowed 规则细节；高级配置以 `plans/04-guard.md` 和默认数据目录下的 `config.toml` 为准。

---

## 快捷键总表

| Key | Welcome | Chat | Config | Help |
|---|---|---|---|---|
| `Enter` | 选择 | 发送/接受建议/确认 AskUser option | 打开/保存/执行当前菜单动作 | - |
| `Shift+Enter` / `Ctrl+J` | - | 换行 | - | - |
| `Esc` | - | 关闭 overlay/cancel/清空输入/回 Welcome | 返回/取消 | 返回来源页 |
| `Ctrl+C` | 退出 | 退出 | 退出 | 退出 |
| `↑↓` | 移动菜单 | 命令建议或 AskUser options | 移动列表/表单焦点/确认按钮 | - |
| `j/k` | 移动菜单 | model picker 中可用 | 移动列表 | - |
| `Ctrl+T` | - | 打开/关闭 tool detail overlay | - | - |
| `Ctrl+R` | - | 展开/收起 reasoning/thinking | - | - |
| `Ctrl+Y` | - | 进入/退出 copy mode | - | - |
| `PgUp/PgDown` | - | viewport 半页滚动 | - | Help viewport 半页滚动 |
| `?` / `F1` | Help 页面 | help overlay | help overlay | - |
| `Space` | - | - | models list 激活 | - |

---

## Protocol 展示数据

TUI 需要 daemon 提供的展示数据：

| 数据 | 当前用途 |
|---|---|
| `daemon.status` | Welcome 状态、active provider/model、context window、usage、memory、last session 是否可恢复；TUI 连接后主动拉取初始状态 |
| `daemon.state` | 轻量 daemon 状态通知；当前不作为连接握手的必需欢迎消息 |
| `config.get` | Config models、active_model、locale、theme、guard_mode、workspace |
| `config.set` | upsert/delete/activate model，update general（locale/theme/guard_mode/workspace） |
| `agent.stream` | assistant chunk、结构化 error、resume availability、context tokens/window、done |
| `agent.resumeRun` | 错误中断后，输入框为空按 Enter 时恢复未完成 turn；不新增 user message |
| `agent.reasoning` | thinking 内容 |
| `agent.tool_start` | tool running 行 |
| `agent.tool_guard` | tool 下方 Guard 注解，包含 risk/decision/source/reason/suggestion |
| `agent.tool_end` | tool done/error 行 |
| `agent.ask_user` | 用户确认或补充信息 |
| `agent.askReply` | TUI 回传用户答案 |
| `agent.guard_confirm` | Guard confirm overlay（tool/risk/reason/suggestion/params） |
| `agent.guardReply` | TUI 回传 Guard approve/reject |
| `session.compact_result` | compact lifecycle/result：手动 compact 结果面板；自动 compact 的 `running=true/false/error` 状态 |
| `memory.list_result` | Chat 中显示 active memory 列表 |
| `session.restore_message` | Resume 逐条恢复最近可见历史消息 |
| `session.restore_status` | Resume 恢复完成状态；包含恢复消息数和是否已 compact，TUI 据此在末尾显示 Session State 提示 |

`agent.tool_end.result` 是 UI 展示内容，不是 agent 内部面向模型的 tool result。daemon 会把该字段限制到 16KB；如果结果被截断，payload 会带 `result_truncated=true` 和 `result_bytes=<原始字节数>`。Runner 写入 WorkingMemory 的 tool result 会另行按模型上下文预算截断，目前最多保留约 50KB 或 500 行，并在后续 compact 中压成 Tool facts。

TUI 断开但 daemon 中的 agent 仍在执行时，当前 TUI 只依赖 `conversation_state` 恢复最近可见消息，不能可靠恢复正在运行或等待 AskUser/GuardConfirm 的后台任务。模型/服务错误导致当前 run 停止时，daemon 可通过 `agent.stream.error + resume_available` 暴露“未完成 turn 可恢复”的结构化状态；TUI 只展示提示并在空输入 Enter 时调用 `agent.resumeRun`，不在本地猜测错误类型、不自动重发用户消息。更完整的后台任务恢复、幂等提交和事件回放仍应随 [07-trigger.md](07-trigger.md) 的持久化 Task/Run 能力一起实现。

已打通的关键结构：

```go
type ConfigModel struct {
    Provider      string   `json:"provider"`
    Model         string   `json:"model"`
    BaseURL       string   `json:"base_url,omitempty"`
    ContextWindow int      `json:"context_window,omitempty"`
    Strengths     []string `json:"strengths,omitempty"`
    HasAPIKey     bool     `json:"has_api_key,omitempty"`
}
```

`context_window` 当前已经不再是文档缺口：

- `internal/config.ModelConfig.ContextWindow` 可持久化到 TOML。
- `internal/protocol.ConfigModel.ContextWindow` 已存在。
- Config form 可编辑 Context Window。
- Chat 顶栏显示 `ctx used/window`。
- Compact 面板按 Context Window 计算百分比。

---

## 文件结构

当前实际结构：

```text
internal/tui/
├── app.go                  # TUI 构造与 Init
├── app_model.go            # root TUI 状态结构
├── app_update.go           # root Update 与页面路由
├── app_view.go             # root View 与复制模式鼠标捕获切换
├── pages.go                # Welcome/Chat/Config/Help 页面 glue
├── events.go               # root notification handler，分发到页面状态
├── local_commands.go       # 以 tea.Cmd 封装 daemon request，避免阻塞 Update
├── chat.go                 # Chat root adapter：输入、命令、AskUser、Guard、Skill glue
├── chat_render.go          # Chat transcript/工具/markdown 渲染适配
├── chat_view.go            # Chat 页面视图 glue
├── config.go               # Config root adapter 和 daemon 配置保存 glue
├── config_forms.go         # Config 表单保存和校验 glue
├── config_view.go          # Config 页面视图 glue
├── components/             # 附件、浮层、虚拟滚动、文本、工具视图等复用组件
├── events/                 # daemon notification 解码和 stream/reasoning batcher
├── pages/                  # chat/config/help/welcome/page 子页面 model/view/state
├── transport/              # TUI 到 daemon 的 local transport 适配
├── i18n.go                 # translator 和 fallback
├── i18n_keys.go            # 内置中文/英文文案
├── markdown.go             # Glamour renderer
├── pet.go                  # Welcome pet 和 Chat mini pet
├── theme.go                # auto/dark/light 主题
└── ui.go                   # 通用样式、布局 helper、compact 面板
```

结构规则：

- root TUI 只负责页面路由、daemon notification 分发、样式/i18n 注入和 tea.Cmd glue。
- `pages/*` 持有页面级状态和纯交互逻辑，不直接访问 daemon。
- `components/*` 优先保持纯渲染/低状态，不读取 root 全局状态，不直接使用 i18n。
- `events/*` 负责 notification 解码和 stream/reasoning 合并；非文本事件前必须 flush 已合并文本。
- `transport/*` 只做 protocol/local transport 适配，不持有业务状态，不绕过 daemon 调用核心包。
- 不新增独立 `setup.go`，首次配置继续复用 Config setup mode。
- 不新增独立 `compact` mode，compact 继续作为 Chat 命令和结果面板。
- `ui.go` 只放跨页面小工具和小面板，不承载页面状态机。
- 稳定维护约定以 `docs/tui.md` 为准；本文件保留交互设计和历史上下文。

---

## 未实现清单

这些是文档中以前容易被误认为已经实现、但当前仍未完成的项。

| 项目 | 当前状态 | 建议优先级 |
|---|---|---|
| Provider API ping | `T` 只有占位提示 | 中 |
| Help 按页面分组 | 只覆盖 Chat 通用快捷键 | 中 |
| Config 高级配置 | 未覆盖 guard/hooks/rate | 低到中 |
| Provider/Model 分离表单 | 当前是一张 model connection 表单 | 中 |
| 结构化 AskUser options | 当前只有 `[]string` | 低 |
| AskUser 鼠标点击选择 | 未实现 | 低 |
| 外部 locale 文件加载 | 有函数，未接入启动流程 | 低 |
| `/daemon` 命令 | protocol 有方法，TUI 未暴露 | 低 |
| `/trigger` 命令 | protocol 有方法，TUI 未暴露 | Phase 3 |
| `/skill` 命令 | protocol 有方法，TUI 未暴露 | Phase 3 |
| `/usage` 命令 | protocol 有方法，TUI 未暴露 | 低 |
| auto compact UI 开关 | 未实现 | 低 |
| 全局状态栏 | 未实现，当前也不建议 MVP 做 | 低 |

---

## 后续收尾建议

按当前可用状态，TUI 近期只建议做三类收尾：

1. **补齐真实可见缺口**：Provider Test、Help 页面分组、Welcome/Config help 文案。
2. **降低配置歧义**：把当前 `Provider` 字段在 UI 中解释清楚，或拆成 provider kind/name/model 的更明确表单。
3. **保持 Chat 稳定**：不要继续往 Chat 常驻区域堆信息；新增能力优先通过命令、overlay 或 Config 子页渐进披露。
