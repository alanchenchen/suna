# 12 — TUI 交互设计

> 最后更新: 2026-05-20
> 本文档以当前 `internal/tui` 实现为准，描述已经可用的 TUI 行为、仍需保留的设计约束，以及尚未实现的功能。
> 范围只包含 TUI 前端视觉、布局、交互和 IPC 展示数据，不定义 daemon/core 业务逻辑。

---

## 当前结论

TUI 现在已经能跑通基本使用流：启动后进入 Welcome，进入 Chat 后可以发消息、看流式回复、看工具执行、处理 AskUser 选项、处理 Guard confirm、切换模型、手动 compact、搜索记忆，并能通过 Config 管理模型连接、语言、主题和 Guard Mode。

当前实现不是完整 GUI 化配置中心，设计重点应收敛到“能稳定对话 + 能完成模型配置 + 能清楚展示 agent 运行状态”。复杂配置和远期多渠道能力不要塞进 TUI MVP。

当前页面只有四个 mode：

| Mode | 文件 | 状态 | 说明 |
|---|---|---|---|
| Welcome | `internal/tui/welcome.go` | 已实现 | 宠物 logo、模型/daemon 概览、菜单入口 |
| Chat | `internal/tui/chat.go`, `chat_render.go` | 已实现 | 对话、流式输出、工具事件、AskUser、命令、状态栏 |
| Config | `internal/tui/config.go`, `config_model.go` | 可用但不完整 | 模型连接、凭证、context window、语言、主题 |
| Help | `internal/tui/help.go` | 已实现基础版 | Chat 通用快捷键和命令说明，Config/Welcome 特有快捷键待补齐 |

---

## 实现状态矩阵

| 功能 | 当前状态 | 关键文件 | 备注 |
|---|---|---|---|
| Bubble Tea 主状态机 | 已实现 | `app.go` | mode 分发、AltScreen、IPC notification 分发 |
| Welcome 菜单 | 已实现 | `welcome.go` | `New`、`Resume`、`Config`、`Help` |
| Chat 布局 | 已实现 | `chat_render.go` | mini pet 顶栏、viewport、textarea、命令建议、底栏 |
| 流式回答 | 已实现 | `app.go`, `chat.go` | `agent.stream` 追加 assistant 内容 |
| Reasoning/Thinking | 已实现 | `app.go`, `chat.go` | 默认折叠，`Ctrl+T` 展开 |
| Tool 展示 | 已实现 | `app.go`, `chat.go`, `chat_render.go` | running/done/error，详情最多展示 10 行结果 |
| AskUser 选项 | 已实现 | `app.go`, `chat.go` | options 为 `[]string`，支持上下选择、Enter、数字输入、自定义答案 |
| Slash command | 已实现 | `commands.go` | `/new`, `/model`, `/memory search`, `/compact`, `/config`, `/help` |
| Model picker | 已实现 | `commands.go`, `chat_render.go` | `/model` 无参数时打开列表 |
| Markdown 渲染 | 已实现 | `markdown.go` | Glamour v2，assistant 和 expanded thinking 使用 |
| Compact 面板 | 已实现 | `ui.go`, `commands.go` | 展示 before/after、context window 百分比、压缩轮数 |
| Config 模型管理 | 已实现基础版 | `config.go`, `config_model.go` | add/edit/delete/activate model |
| Provider kind 选择 | 已实现 | `config.go` | openai-compatible / openai / anthropic |
| Context Window | 已打通 | `config.go`, `message.go`, `chat_render.go` | config/IPc/顶栏/compact 均可使用 |
| Credentials | 已实现基础版 | `config.go`, daemon config IPC | API Key 通过 IPC 保存到 credentials，不在 TUI 明文展示 |
| 语言切换 | 已实现 | `i18n.go`, `i18n_keys.go`, `config_model.go` | 中文/英文内置翻译，Config 可切换 |
| 主题切换 | 已实现 | `theme.go`, `config_model.go` | auto/dark/light |
| Provider Test | 未实现真实 ping | `config.go` | `T` 只显示 `local config only; API ping not implemented` |
| Guard confirm overlay | 已实现 | `chat_render.go`, `chat.go` | 独立面板显示 tool/risk/reason/suggestion/params |
| Guard Mode 配置 | 已实现 | `config_model.go` | Config home 可切换 ask→smart→auto→readonly |
| 外部 i18n 文件加载 | 未接入主流程 | `i18n.go` | 有 `LoadLocale`，但当前主要用内置翻译表 |
| Config 高级项 | 未实现 | `config.go` | 未编辑 guard/hooks/max_model_rps/cost_per_1k |

---

## 设计原则

1. **当前实现优先**：文档描述必须和 `internal/tui` 当前行为一致，未实现功能放入“未实现清单”，不能混在主流程里。
2. **Chat 优先**：Chat 是核心页面，常驻信息只保留身份、模型、context、输入和运行状态。
3. **TUI 纯前端**：TUI 不持有业务逻辑、数据库连接或模型执行逻辑，只渲染 UI 并通过 IPC 与 daemon 通信。
4. **配置足够可用**：MVP 只要求能配置模型连接、凭证、context window、语言和主题；高级配置后续分组补齐。
5. **信息不重复**：Chat 顶栏显示 provider/model/context/连接状态；底栏只显示 token 和速度；快捷键通过 help 查看。
6. **全程 i18n**：用户可见文本必须走 `internal/tui/i18n_keys.go` 或 translator，不在页面代码里散落硬编码文案。
7. **不伪造 usage**：token/cache/context/speed 只能来自 provider usage 或 daemon 状态；没有 usage 时显示未知。

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
- 展示 pet、产品名、subtitle、setup hint。
- 展示模型、今日 usage、uptime、memory、sessions。
- 未配置模型时仍允许进入 Config/Help，New 会引导到 Config setup form。
- Resume 仅在 daemon status 有 `Sessions.LastID` 时显示。

菜单项：

| 条目 | 条件 | 行为 |
|---|---|---|
| New | 总是显示 | 无模型时进入 setup form，有模型时新建会话并进入 Chat |
| Resume | 有 last session 时显示 | 调 `session.restore` 并进入 Chat |
| Config | 总是显示 | 进入 Config home |
| Help | 总是显示 | 进入 Help 页面 |

快捷键：

| Key | 行为 |
|---|---|
| `↑↓` / `j k` | 菜单移动 |
| `Enter` | 选择 |
| `n` | New |
| `r` | Resume |
| `Ctrl+O` | Config |
| `?` / `F1` | Help |
| `Ctrl+C` | Quit |

当前不足：Welcome 底部 help 文案没有完整列出 `n/r/Ctrl+O/?/j/k`。

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
token status bar
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
- IPC 连接点。

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

### 消息渲染

| 类型 | 当前行为 |
|---|---|
| user | inline 用户消息，原样文本，不走 Markdown |
| assistant | 标题 `Suna` + Glamour Markdown |
| reasoning | Thinking box，默认折叠，`Ctrl+T` 展开，展开时走 Markdown |
| tool | running/done/error 行，`Ctrl+T` 展示参数和最多 10 行结果 |
| system | dim 色系统消息 |
| error | error 色错误消息 |

### Loading 和阶段

当前 phase：

| Phase | 含义 |
|---|---|
| `phaseIdle` | 无运行中请求 |
| `phaseFirstLLM` | 用户消息已发，等待首包 |
| `phaseLLM` | 正在接收普通回答 |
| `phaseThinking` | 正在接收 reasoning |
| `phaseTool` | 正在执行工具 |

Chat pet 和 spinner 根据 phase 切换。tool 完成后回到 LLM phase；stream done 后 reset 到 idle。

### AskUser Options

AskUser 已实现，不再是缺失项。

IPC 数据结构：

```go
type AskUserParams struct {
    Question string   `json:"question"`
    Options  []string `json:"options,omitempty"`
    ID       string   `json:"id"`
}
```

当前行为：

- 收到 `agent.ask_user` 后追加系统问题。
- 如果有 options，在 viewport 中渲染选项列表。
- `↑↓` 移动选项光标。
- 输入为空时按 `Enter` 直接选择当前选项。
- 输入 `1`、`2` 等数字时映射到对应选项。
- 也允许输入自定义答案。
- 回复通过 `agent.askReply` 回传 daemon。

限制：

- options 当前只是 `[]string`，没有 `{label,value}`、默认值、禁用态或多选。
- 当前不支持鼠标点击选择。

### Guard Confirm Overlay

Guard confirm 使用独立 overlay 面板，不复用 AskUser 事件。

IPC 数据结构：

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
- 键位：`←→` / `j/k` 移动焦点，`Enter` / `Y` 确认，`Esc` / `N` 拒绝。
- 默认选中 Reject（安全优先）。
- 回复通过 `agent.guardReply` 回传 `approve` 或 `reject`。
- 所有文案走 i18n（`i18n_keys.go`）。

说明：

- Guard confirm 是独立事件类型 `EventGuardConfirm`，不复用 AskUser。
- TUI 只负责展示和回传，不做安全判断。

### 命令

当前 TUI 有 6 个 slash command 入口：

| 命令 | 行为 |
|---|---|
| `/new` | 新建 session，清空当前 Chat 状态 |
| `/model [ref]` | 无参数时打开模型选择器；带 ref 时切换模型；没有 provider 前缀时使用当前 provider |
| `/memory search <q>` | 调 `memory.search`，显示结果 |
| `/compact` | 调 `session.compact`，显示 compact 结果面板 |
| `/config` | 进入 Config home |
| `/help` | 进入 Help 页面 |

说明：`/model` 的无参/带参是同一个命令入口。

命令建议：

- 输入以 `/` 开头且第一个空格前仍在命令位置时显示。
- 最多显示 4 项。
- `↑↓` 选择，`Enter` 接受。
- `Tab` 不用于命令建议。

当前未暴露的 IPC 能力：

- `/daemon status|stop|restart`
- `/trigger ...`
- `/skill ...`
- `/usage`
- `/memory facts/status`

这些不属于当前 TUI MVP。

### Chat 快捷键

| Key | 行为 |
|---|---|
| `Enter` | 发送；有命令建议时接受建议；有 AskUser options 且输入为空时确认当前选项 |
| `Shift+Enter` / `Alt+Enter` | 换行 |
| `Esc` | 关闭 help；运行中则 cancel；输入为空则回 Welcome；输入非空则清空输入 |
| `Ctrl+N` | 新会话 |
| `Ctrl+O` | Config |
| `Ctrl+T` | 展开/收起 tool 和 thinking 详情 |
| `Ctrl+U` / `PgUp` | viewport 上滚 |
| `Ctrl+D` / `PgDown` | viewport 下滚 |
| `↑↓` | 命令建议或 AskUser options 移动 |
| `?` / `F1` | Chat help overlay |
| `Ctrl+C` | Quit |

---

## Markdown

当前使用 `charm.land/glamour/v2`，代码在 `markdown.go`。

规则：

- assistant 内容使用 Glamour 渲染。
- expanded thinking 使用 Glamour 渲染。
- user 内容不渲染 Markdown，按普通文本展示。
- renderer 按宽度和主题缓存。
- Chat viewport `SoftWrap=false`，避免 Glamour wrap 后再次 wrap。

注意：当前实现没有 50ms debounce 渲染队列。流式 chunk 到达后直接更新消息，最终渲染策略应以后续性能优化为准，不再把 debounce 写成已实现要求。

---

## Compact 面板

`/compact` 当前通过 IPC 调 `session.compact`，结果由 `session.compact_result` 推回 TUI。

面板展示：

- compact 完成提示。
- before tokens 和 context window 百分比。
- after tokens 和 context window 百分比。
- 保留最近轮数说明。
- 被压缩轮数和 summary token。
- 被截断工具输出数量。

限制：

- `SummaryTokens` 当前可能是 daemon 侧估算值，不应在 UI 文案里暗示来自真实 provider usage。
- TUI 没有 auto-compact 开关。
- Compact 不是独立页面。

---

## Config

Config 当前是可用的模型连接管理，不是完整配置中心。

入口：

- Welcome 菜单 `Config`。
- Chat `Ctrl+O`。
- Chat `/config`。
- 无模型配置时，Welcome `New` 进入 setup form。

当前页面层级：

```text
Config home
  ├─ Model Connections → models list
  │    ├─ Enter → detail
  │    ├─ A → provider kind overlay → provider form
  │    ├─ E → provider form
  │    ├─ D → delete confirm
  │    └─ Space → activate
  ├─ Language → toggle zh/en
  ├─ Theme → toggle auto/dark/light
  └─ Guard Mode → toggle ask→smart→auto→readonly→ask
```

### Config Home

当前展示：

- Model Connections section。
- Active model。
- Providers summary。
- Language。
- Theme。
- Guard Mode。

当前没有单独 General 子页。Language 和 Theme 在 home 直接切换。

### Models List

当前展示：

- 每个 model 以 `provider/model` 为 ref。
- 标记：`◉` active，`○` inactive，`!` incomplete。
- 摘要：active、missing api key、provider、model、context window、endpoint configured、strengths。
- 列表按 ref 排序。

操作：

| Key | 行为 |
|---|---|
| `A` | 新增 model，先选择 provider kind |
| `E` | 编辑当前 model |
| `D` | 删除当前 model，进入确认状态 |
| `Space` | 激活当前 model |
| `Enter` | 进入 detail |
| `Esc` | 返回 home |

### Detail

当前展示：

- Status。
- Provider type。
- Endpoint。
- API Key 是否 configured。
- Model。
- Context Window。
- Last check。

操作：

| Key | 行为 |
|---|---|
| `E` | 编辑当前 model |
| `T` | 显示未实现提示 |
| `Esc` | 返回 models list |

Provider Test 当前未实现真实 API ping。文档和 UI 都应把它视为占位功能。

### Provider Kind Overlay

新增 model 时先选择 provider kind：

- openai-compatible
- openai
- anthropic

选择后进入 provider form。`openai` 和 `anthropic` 会填入默认 endpoint 提示，`anthropic` context window placeholder 为 `200000`。

### Provider Form

当前表单字段：

| 字段 | 状态 | 说明 |
|---|---|---|
| Provider | 必填 | 当前兼作 provider id/type/name，后续可拆分展示名和 provider kind |
| Model | 必填 | 实际模型 ID |
| API Key | setup 必填，编辑可留空 | 密码回显；留空表示不修改已有凭证 |
| Endpoint | openai/anthropic 可空，openai-compatible 必填 | 非空时校验 URL |
| Context Window | 可空 | 非空必须为正整数；保存到 `context_window` |
| Strengths | 可空 | 逗号分隔，保存为 `[]string`，用于路由偏好 |

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

当前 General 只有三个轻量偏好：

- Language: zh/en。
- Theme: auto/dark/light。
- Guard Mode: ask/smart/auto/readonly。

Guard Mode 切换通过 `config.set` 持久化到 `guard.mode` 配置。TUI 中显示当前 mode 和简要说明。

未实现：

- 单独 General 页面。
- Guard rules 编辑。
- Hooks 编辑。
- max_model_rps。
- cost_per_1k。
- model pricing / usage budget。

---

## Help

Help 有两个形态：

- Chat/Config overlay：`?` / `F1` 打开。
- 独立 Help 页面：Welcome 中选择 Help 或 Chat 输入 `/help`。

当前内容：

- 使用 `bubbles/help` 渲染 key.Binding。
- 展示 Chat 通用快捷键。
- 展示当前 6 个 slash command。
- Help 页面使用 viewport，可滚动。

当前不足：

- Config 的 `A/E/D/T/Space/j/k` 未在完整 Help 中集中展示。
- Welcome 的 `n/r/Ctrl+O/?/j/k` 未在完整 Help 中集中展示。
- Help 内容仍偏 Chat，需要按页面分组补齐。

---

## 快捷键总表

| Key | Welcome | Chat | Config | Help |
|---|---|---|---|---|
| `Enter` | 选择 | 发送/接受建议/确认 AskUser option | 打开/保存/切换 | - |
| `Shift+Enter` / `Alt+Enter` | - | 换行 | - | - |
| `Esc` | 无操作 | 关闭 overlay/cancel/清空输入/回 Welcome | 返回/取消 | 返回来源页 |
| `Ctrl+C` | 退出 | 退出 | 退出 | 退出 |
| `↑↓` | 移动菜单 | 命令建议或 AskUser options | 移动列表/表单焦点 | - |
| `j/k` | 移动菜单 | model picker 中可用 | 移动列表 | - |
| `n` | New | - | - | - |
| `r` | Resume | - | - | - |
| `Ctrl+N` | - | 新会话 | - | - |
| `Ctrl+O` | Config | Config | - | - |
| `Ctrl+T` | - | 展开/收起 tool/thinking | - | - |
| `Ctrl+U/D` | - | viewport 半页滚动 | - | Help viewport 半页滚动 |
| `PgUp/PgDown` | - | viewport 半页滚动 | - | Help viewport 半页滚动 |
| `?` / `F1` | Help 页面 | help overlay | help overlay | - |
| `A` | - | - | models list 新增 | - |
| `E` | - | - | models/detail 编辑 | - |
| `D` | - | - | models list 删除 | - |
| `Space` | - | - | models list 激活 | - |
| `T` | - | - | detail check，占位 | - |

---

## IPC 展示数据

TUI 需要 daemon 提供的展示数据：

| 数据 | 当前用途 |
|---|---|
| `daemon.status` | Welcome 状态、active provider/model、context window、usage、memory、sessions |
| `daemon.state` | 连接时 provider/model/PID 等轻量状态 |
| `config.get` | Config models、active_model、locale、theme |
| `config.set` | upsert/delete/activate model，update general |
| `agent.stream` | assistant chunk、usage、context tokens/window、done |
| `agent.reasoning` | thinking 内容 |
| `agent.tool_start` | tool running 行 |
| `agent.tool_end` | tool done/error 行 |
| `agent.ask_user` | 用户确认或补充信息 |
| `agent.askReply` | TUI 回传用户答案 |
| `agent.guard_confirm` | Guard confirm overlay（tool/risk/reason/suggestion/params） |
| `agent.guardReply` | TUI 回传 Guard approve/reject |
| `session.compact_result` | compact 面板 |
| `memory.search_result` | Chat 中显示记忆搜索结果 |
| `session.restore_message` | Resume 恢复历史消息 |
| `session.restore_input` | Resume 恢复未发送输入 |

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
- `internal/ipc.ConfigModel.ContextWindow` 已存在。
- Config form 可编辑 Context Window。
- Chat 顶栏显示 `ctx used/window`。
- Compact 面板按 Context Window 计算百分比。

---

## 文件结构

当前实际结构：

```text
internal/tui/
├── app.go                # TUI struct, Init/Update/View, mode 切换、IPC notification 分发
├── chat.go               # Chat 状态、输入、AskUser、Guard confirm、tool/thinking 渲染核心
├── chat_render.go        # Chat 布局、顶栏/底栏、命令建议、model picker、Guard overlay、渲染辅助
├── commands.go           # slash commands
├── config.go             # Config 表单、provider kind、表单校验和保存
├── config_model.go       # Config 页面行模型、导航、渲染、Guard Mode 切换
├── help.go               # Help 页面、Chat/Config help overlay、key bindings
├── i18n.go               # translator 和 fallback
├── i18n_keys.go          # 内置中文/英文文案（含 Guard confirm 文案）
├── ipc_client.go         # TUI IPC client 接口和公共逻辑
├── ipc_client_unix.go    # Unix socket IPC client
├── ipc_client_windows.go # Windows named pipe IPC client
├── markdown.go           # Glamour renderer
├── pet.go                # Welcome pet 和 Chat mini pet
├── theme.go              # auto/dark/light 主题
└── ui.go                 # 通用样式、布局 helper、compact 面板
```

结构规则：

- 不新增独立 `setup.go`，首次配置继续复用 Config setup mode。
- 不新增独立 `compact` mode，compact 继续作为 Chat 命令和结果面板。
- Chat 继续保持 `chat.go` 负责状态和 Update，`chat_render.go` 负责布局和辅助渲染。
- Config 如果继续增长，可以按功能拆文件，但不要把同一层级的表单逻辑散落到多个文件里。
- `ui.go` 只放跨页面小工具和小面板，不承载页面状态机。

---

## 未实现清单

这些是文档中以前容易被误认为已经实现、但当前仍未完成的项。

| 项目 | 当前状态 | 建议优先级 |
|---|---|---|
| Provider API ping | `T` 只有占位提示 | 中 |
| Help 按页面分组 | 只覆盖 Chat 通用快捷键 | 中 |
| Config 高级配置 | 未覆盖 guard/hooks/rate/cost | 低到中 |
| Provider/Model 分离表单 | 当前是一张 model connection 表单 | 中 |
| 结构化 AskUser options | 当前只有 `[]string` | 低 |
| AskUser 鼠标点击选择 | 未实现 | 低 |
| 外部 locale 文件加载 | 有函数，未接入启动流程 | 低 |
| `/daemon` 命令 | IPC 有方法，TUI 未暴露 | 低 |
| `/trigger` 命令 | IPC 有方法，TUI 未暴露 | Phase 3 |
| `/skill` 命令 | IPC 有方法，TUI 未暴露 | Phase 3 |
| `/usage` 命令 | IPC 有方法，TUI 未暴露 | 低 |
| auto compact UI 开关 | 未实现 | 低 |
| 全局状态栏 | 未实现，当前也不建议 MVP 做 | 低 |

---

## 后续收尾建议

按当前可用状态，TUI 近期只建议做三类收尾：

1. **补齐真实可见缺口**：Provider Test、Help 页面分组、Welcome/Config help 文案。
2. **降低配置歧义**：把当前 `Provider` 字段在 UI 中解释清楚，或拆成 provider kind/name/model 的更明确表单。
3. **保持 Chat 稳定**：不要继续往 Chat 常驻区域堆信息；新增能力优先通过命令、overlay 或 Config 子页渐进披露。
