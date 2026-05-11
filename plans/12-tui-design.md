# 12 — TUI 交互设计

> 本文档只涉及 TUI 前端的视觉、布局、交互设计。不涉及 daemon/IPC/业务逻辑。
> 技术栈：Bubble Tea v2 + bubbles v2 + lipgloss v2 + glamour v2

## 设计目标

1. **接近 GUI 行为**：终端里也要有首页、主界面、弹层、焦点、状态和渐进披露。
2. **Chat 优先**：Chat 是 suna 的核心使用场景，首页和配置页都服务于快速进入 Chat。
3. **信息不重复**：同一类状态只出现一次，避免顶栏、底栏重复显示模型/token。
4. **默认干净**：常用信息常驻；低频信息通过 `?`、`Ctrl+T`、`/` 等方式展开。
5. **用成熟组件**：Markdown 用 glamour，导航/帮助/输入使用 bubbles，不继续维护自写 Markdown 解析器。
6. **全程 i18n**：TUI 中所有用户可见文本必须来自 i18n，不允许在 Go 代码里硬编码英文/中文文案。

---

## 页面模型

```
suna 启动
  │
  ├─ 无 config → Config(setup mode) → Welcome
  │
  └─ 有 config → 连接 daemon → Welcome
                            │
                            ├─ New / Resume → Chat
                            ├─ Config       → Config
                            └─ Help         → Help
```

| 页面 | 定位 | 入口 | 组件 |
|---|---|---|---|
| Setup | 首次配置流程，不是独立实现 | 自动进入 Config setup mode | Config form |
| Welcome | 首页入口 | 启动后 | list |
| Chat | 核心对话 | Welcome / Resume | viewport + textarea + spinner |
| Config | 配置管理 | Welcome / `Ctrl+O` | list + textinput overlay |
| Help | 快捷键和命令速查 | `?` / `F1` / `/help` | viewport + help |

Setup 和 Config 是同一条配置链路：

- 首次启动无 config 时，不实现第二套 setup UI，而是打开 `Config` 的 `setup mode`。
- `setup mode` 强制完成最小配置：Provider type (openai/anthropic/openai-compatible) + Model + API Key + Endpoint (如需)。
- setup 保存成功后进入 Welcome；取消 setup 时返回空状态首页。
- 普通 Config 模式复用同一套 provider 列表、provider 表单、校验和保存逻辑（凭证存 credentials.toml）。
- 这样可以避免 Setup 和 Config 两套表单字段、校验规则、i18n 文案不一致。

---

## i18n 约束

TUI 的所有用户可见文本都必须走 `internal/i18n`，包括页面标题、菜单项、按钮、表单 label、placeholder、help 文案、错误提示、命令说明和状态文案。

示例图中的英文只是视觉占位，不能作为实现时的硬编码文本。

必须走 i18n 的文本类型：

| 类型 | 示例 key |
|---|---|
| 页面标题 | `tui.welcome.title`, `tui.config.title`, `tui.help.title` |
| Welcome 菜单 | `tui.welcome.new`, `tui.welcome.resume`, `tui.welcome.config` |
| Chat 角色名 | `tui.chat.you`, `tui.chat.suna`, `tui.chat.thinking` |
| 状态字段 | `tui.status.model`, `tui.status.uptime`, `tui.status.memory` |
| 快捷键说明 | `tui.key.send`, `tui.key.back`, `tui.key.new_session` |
| 命令说明 | `tui.command.new.desc`, `tui.command.compact.desc` |
| Config 表单 | `tui.config.provider.name`, `tui.config.provider.endpoint` |
| 校验错误 | `tui.error.required`, `tui.error.invalid_endpoint` |

实现规则：

- Go 代码里只能硬编码 i18n key、图标、快捷键、Provider/Model 动态值。
- `Suna` 作为产品名不翻译；`Provider`、`Model` 等 UI label 需要翻译。
- slash command 名称（如 `/new`、`/compact`）不翻译，命令描述必须翻译。
- `key.Binding` 的 help 描述使用 i18n 文案生成，不直接写 `send/back/new`。
- 测试和 snapshot 要允许中英文切换，不能依赖固定英文 UI 文案。

---

## 视觉语言

### 宠物 Logo

Logo 采用圆角小机器人。它不是空心字符画，而是 **边框内部用 lipgloss Background 填充的暖黄色色块**。

```
idle (待机):              working (工作中):         thinking (思考中):
    ╭────────╮               ╭────────╮               ╭────────╮
    │  ◠  ◠  │               │  ▶  ◀  │               │  ○  ○  │
    │   ω    │               │   ω    │               │   △    │
    ╰────────╯               ╰───⚡────╯               ╰────────╯
```

规则：

- 不显示天线，避免占用垂直空间；状态只通过眼睛、嘴巴和底边符号表达。
- 边框字符 `╭╮╰╯│─` 不着色，保持干净轮廓。
- 边框内部每一行整体用 `Background(ColorBrand)` 渲染，空格也被填色。
- 眼睛和嘴巴用深色前景色叠加在填充色上。
- Welcome 使用完整 4 行版本，Chat 使用 3 行小 logo 版本。不使用 1 行 mini，因为它无法保留宠物识别度。

实现约束：

```go
bodyFill := lipgloss.NewStyle().
    Background(ColorBrand).
    Foreground(lipgloss.Color("0"))

eyeRow := "│" + bodyFill.Render("  ◠  ◠  ") + "│"
mouthRow := "│" + bodyFill.Render("   ω    ") + "│"
```

Chat 小 logo 必须保留 idle / working / thinking 三种状态，但压缩成 3 行，以便在 Chat 顶部常驻显示。Welcome 仍展示完整 4 行 logo，但完整 logo 主要是品牌展示，不适合承载运行时状态；运行时状态主要通过 Chat 小 logo 表达。

```go
idleSmall := strings.Join([]string{
    "╭──────╮",
    "│" + bodyFill.Render(" ◠  ◠ ") + "│",
    "╰──────╯",
}, "\n")

workingSmall := strings.Join([]string{
    "╭──────╮",
    "│" + bodyFill.Render(" ▶  ◀ ") + "│",
    "╰──⚡──╯",
}, "\n")

thinkingSmall := strings.Join([]string{
    "╭──────╮",
    "│" + bodyFill.Render(" ○  ○ ") + "│",
    "╰──△──╯",
}, "\n")
```

三态规则：

| 状态 | 触发条件 | 小 logo |
|---|---|---|
| idle | 无请求进行中 | 眼睛 `◠  ◠`，普通底边 |
| working | tool 调用或 daemon 操作进行中 | 眼睛 `▶  ◀`，底边 `⚡` |
| thinking | LLM reasoning / 首包等待 | 眼睛 `○  ○`，底边 `△` |

小 logo 仍然使用完整 logo 的 `bodyFill`，通过背景色保留宠物识别感；不要退化成纯文本图标或 1 行 badge。

完整 logo 与小 logo 的职责：

| 位置 | 尺寸 | 作用 | 状态 |
|---|---|---|---|
| Welcome | 4 行 | 品牌展示和入口氛围 | 默认 idle，可根据 daemon 初始状态短暂显示 working/thinking，但不作为主要状态入口 |
| Chat 顶部 | 3 行 | 常驻身份和运行状态 | 必须支持 idle / working / thinking 三态 |

不要为了在 Welcome 展示三态而增加首页复杂度；三态价值应该体现在 Chat 使用过程中。Chat 不提供 1 行 mini fallback，窗口高度不足时也保持 3 行小 logo，优先保证品牌识别度。

### 配色

```go
var (
    ColorBrand = lipgloss.Color("14") // logo, spinner, selected
    ColorDim   = lipgloss.Color("8")  // secondary text, borders
    ColorUser  = lipgloss.Color("12") // user role, input tokens
    ColorAgent = lipgloss.Color("10") // agent role, output tokens, connected
    ColorTool  = lipgloss.Color("11") // tool pill
    ColorError = lipgloss.Color("9")  // errors
    ColorHL    = lipgloss.Color("15") // highlight text
)
```

| 语义 | 颜色 | 用途 |
|---|---|---|
| Brand | `14` | 宠物、选中态、spinner |
| User | `12` | `▶ You`、输入 token `↑` |
| Agent | `10` | `● Suna`、输出 token `↓`、连接 `●` |
| Tool | `11` | tool pill、进行中状态 |
| Error | `9` | tool 失败、错误提示 |
| Dim | `8` | 辅助文字、边框、cache token `⟳` |

---

## Welcome 页面

Welcome 是入口页，只做三件事：展示 suna、展示 daemon 概况、进入 Chat/Config/Help。

下方示例使用英文占位，实际渲染必须通过 i18n 输出当前语言文案。

```
┌──────────────────────────────────────────────────────────────┐
│                                                              │
│     ╭────────╮        Suna                                   │
│     │  ◠  ◠  │        your stateful AI companion             │
│     │   ω    │                                               │
│     ╰────────╯        Model    glm/glm-4                       │
│                       Uptime   2h 34m                        │
│                       Memory   1,247 ep · 389 ent            │
│                       Session  3 active · 12 done            │
│                                                              │
│   ┌─────────────────────────────────────────────────────┐    │
│   │  ▶ New Conversation                                 │    │
│   │    Resume Last Session                              │    │
│   │    Switch Model                                     │    │
│   │    Config                                           │    │
│   │    Help                                             │    │
│   └─────────────────────────────────────────────────────┘    │
│                                                              │
│   ↑↓ navigate · enter select · esc back · ctrl+c quit        │
└──────────────────────────────────────────────────────────────┘
```

实现：

- 菜单使用 `bubbles/list`。
- `list.New(items, delegate, width, height)` 参数顺序必须是 `items, delegate, width, height`。
- 列表隐藏 title/status/pagination，只保留菜单本身。
- `Resume Last Session` 仅在存在历史会话时显示或启用。
- `n` 进入 New Conversation，`r` 进入 Resume Last Session。退出只使用 `Ctrl+C`。

---

## Chat 页面

Chat 是主界面，常驻区域必须少而稳定。

下方示例中的角色名、thinking 标题、命令描述都必须通过 i18n 渲染；Provider/Model、token 数值和用户/模型内容是动态值。

```
┌──────────────────────────────────────────────────────────────┐
│  ╭──────╮                                                     │
│  │ ◠  ◠ │  glm/glm-4    ctx 12k/128k                      ●  │
│  ╰──────╯                                                     │
│──────────────────────────────────────────────────────────────│
│                                                              │
│  ▶ You                                                       │
│  帮我重构认证模块，现在代码太乱了                              │
│                                                              │
│  ● Suna                                                      │
│  我先看一下当前的代码结构。                                    │
│                                                              │
│    ⋯ Read(auth.go) 3.2s                                      │
│    ✓ Read(session.go) 0.2s                                   │
│                                                              │
│  ┌─ ◎ Thinking ────────────────────────────────────────┐    │
│  │ 考虑了 3 种方案 → 选择方案 B（Ctrl+T 展开）           │    │
│  └──────────────────────────────────────────────────────┘    │
│                                                              │
│  ● Suna                                                      │
│  当前有 3 个问题需要处理：                                    │
│                                                              │
│  ┌──────────┬──────────┬──────────┐                         │
│  │ 文件     │ 问题     │ 优先级   │                         │
│  ├──────────┼──────────┼──────────┤                         │
│  │ auth     │ 硬编码   │ P0       │                         │
│  │ session  │ 竞态     │ P1       │                         │
│  └──────────┴──────────┴──────────┘                         │
│                                                              │
│──────────────────────────────────────────────────────────────│
│  > 好的，先处理 P0 的问题_                                   │
│  ↑3.2k ↓1.8k ⟳0.8k · 45t/s                                  │
└──────────────────────────────────────────────────────────────┘
```

### 区域划分

| 区域 | 高度 | 内容 |
|---|---|---|
| 顶部身份区 | 3 行 | 左侧小 logo + 右侧模型/context/连接状态信息块 |
| 对话区 | 动态 | viewport，消息、thinking、tools、loading 都在这里 |
| 输入区 | 1~6 行 | textarea，动态高度 |
| 命令联想 | 0~4 行 | 输入 `/` 时显示，平时不占高度 |
| 底栏 | 1 行 | token + speed |

高度计算：

```
viewportHeight = totalHeight - identityHeader(3) - separators(2) - textareaHeight - suggestionHeight - bottombar(1)
```

### 顶部身份区

```
╭──────╮
│ ◠  ◠ │  glm/glm-4    ctx 12k/128k                      ●
╰──────╯
```

- 顶部身份区采用 flex 布局：左侧固定 3 行小 logo，中间信息和右侧状态点垂直居中。
- 小 logo 已经承担产品身份，不再重复显示 `suna` 文本。
- 第 1 行只渲染 logo 顶边，不放额外文本。
- 第 2 行是主信息行：logo 中线、Provider/Model、context、最右状态图标都在同一行。
- 第 3 行只保留 logo 的底边，不放额外文本。
- 顶部身份区只说明身份、模型和当前上下文：不放 token，不放快捷键。
- `●` 表示 connected，`○` 表示 disconnected；颜色表达 idle / working / thinking，不额外显示状态文本。
- 小 logo 使用宠物色块的 3 行版本，必须保留外框和双眼。
- 不提供 1 行 mini fallback，避免看不出宠物形象。
- 顶部身份区需要同时显示当前会话上下文和模型最大上下文，例如 `ctx 12k/128k`。

布局规则：

- logo 宽度固定为 8 列外框；信息和状态点垂直居中到 logo 的第 2 行。
- Provider/Model 和 context 之间保留至少 4 个空格，状态点靠最右。
- 第 1、3 行严格只渲染 logo，本身不附加状态或说明文本。
- 如果宽度不足，优先保留 Provider/Model，再保留 context，最后保留状态点。

### 底栏

```
↑3.2k ↓1.8k ⟳0.8k · 45t/s
```

| 字段 | 含义 |
|---|---|
| `↑3.2k` | input tokens |
| `↓1.8k` | output tokens |
| `⟳0.8k` | cache tokens |
| `45t/s` | output speed |
底栏不显示快捷键，也不重复显示 daemon 状态。快捷键通过 `?` 浮层查看，daemon 状态只在顶部身份区显示。

### Help 浮层

在 Chat 中按 `?` 或 `F1` 弹出帮助浮层，再次按 `?` 或 `Esc` 关闭。

```
┌─ Shortcuts ────────────────────────────────────┐
│  enter send · esc back · ctrl+n new            │
│  ctrl+t detail · ctrl+o config · ctrl+u/d scroll│
│                                                │
│  Commands                                      │
│  /new · /model <name> · /compact               │
│  /memory search <q> · /help                    │
└────────────────────────────────────────────────┘
```

实现：

- 浮层是渲染 overlay，不改变 Chat 区域高度。
- 快捷键部分用 `bubbles/help` 的 `ShortHelpView`。
- 命令列表手动渲染。
- `showHelp` 为 true 时 overlay 盖在 viewport 上方。

### 输入区

- 使用 `bubbles/textarea`。
- `Enter` 发送，`Shift+Enter` 换行。
- 最大高度 6 行，超过后 textarea 内部滚动。
- Resume 会话时，如果 daemon 返回 `unsent_input`，自动填入 textarea 并把光标放到末尾。

### 命令联想

输入以 `/` 开头且第一个空格前仍在命令位置时显示建议。

```
> /com_
┌──────────────────────────────────────────────────────┐
│  /compact  Compact context                           │
│  /config   Open configuration                        │
└──────────────────────────────────────────────────────┘
```

规则：

- `↑↓` 选择，`Enter` 填充。
- `Tab` 不用于联想，保留给 textarea 缩进行为。
- 最多显示 4 项。
- 输入不再匹配命令时自动隐藏。

---

## Chat 内容渲染

### 用户消息

```
  ▶ You
  帮我看一下这个文件的权限问题
```

- `▶ You` 使用 `ColorUser` 粗体。
- 用户内容原样显示，不渲染 Markdown。

### Agent 消息

```
  ● Suna
  当前有 **3 个问题** 需要处理：
```

- `● Suna` 使用 `ColorAgent` 粗体。
- Agent 内容使用 glamour 渲染 Markdown。

### Tool 调用

```
  ⋯ Read(auth.go) 3.2s
  ✓ Read(auth.go) 0.3s
  ✓ Read(session.go) 0.2s
  ✗ Exec(go test ./...) 2.1s
```

规则：

- tool 开始时显示 `⋯ Tool(args) elapsed`。
- tool 完成后替换为 `✓` 或 `✗`，固定耗时。
- 展开详情由 `Ctrl+T` 控制，缩进显示完整参数和最多 10 行返回值。
- 不使用 LLM 最后一段自然语言推断意图，直接展示函数名和参数摘要。

### Thinking / Reasoning

Thinking 是 LLM 的推理过程，不应打断回答主线，默认折叠。

进行中：

```
  ┌─ ◎ Thinking ──────────────────────────────┐
  │  分析代码结构，寻找重构切入点...  3.2s      │
  └────────────────────────────────────────────┘
```

完成后折叠：

```
  ┌─ ◎ Thinking ───────────────────────────────────────┐
  │  考虑了 3 种方案 → 选择方案 B    [Ctrl+T 展开]      │
  └─────────────────────────────────────────────────────┘
```

展开：

```
  ┌─ ◎ Thinking ──────────────────────────────────────┐
  │  首先，我分析了当前的代码结构，发现有 3 个问题：    │
  │  1. auth 模块有硬编码密钥                           │
  │  2. session 管理存在竞态条件                        │
  │  3. 缺少统一错误处理                                │
  │                                                    │
  │  综合考虑，选择渐进重构。                            │
  │                                                    │
  │  [Ctrl+T 收起]                                     │
  └────────────────────────────────────────────────────┘
```

规则：

- 进行中只显示最新摘要行 + 计时。
- 完成后提取最后一句或前 80 字符作为摘要；无法提取时显示 `已思考 Xs`。
- 展开内容用 glamour 渲染，最多显示 15 行，超出显示 `...`。
- Thinking 和紧随其后的 Agent 回答属于同一轮，中间不额外空行。

### Loading

| 类型 | 位置 | 结束条件 |
|---|---|---|
| LLM 首包等待 | 用户消息后独立一行 spinner | 收到首个 stream/reasoning chunk |
| Thinking | Thinking box 内 | reasoning done |
| Tool | 每个 tool 行内 | tool end |

---

## Markdown 渲染

使用 `charm.land/glamour/v2` 替换自写 Markdown 解析器。

原因：

- 自写解析器很难正确处理表格、代码块、列表、链接、嵌套格式。
- LLM 输出的 Markdown 经常不完全规范，需要成熟解析器兜底。
- glamour 是 Charm 官方库，与 Bubble Tea/lipgloss 生态兼容。

实现约束：

```go
renderer, err := glamour.NewTermRenderer(
    glamour.WithStylesFromJSONBytes([]byte(sunaStyleJSON)),
    glamour.WithWordWrap(width),
)
if err != nil {
    renderer, _ = glamour.NewTermRenderer(
        glamour.WithStandardStyle("dark"),
        glamour.WithWordWrap(width),
    )
}
```

注意：

- glamour v2 没有 `WithStylesFromString`，内嵌样式使用 `WithStylesFromJSONBytes`。
- glamour 已经通过 `WithWordWrap(width)` 换行，viewport 应设置 `SoftWrap = false`，避免双重换行。
- renderer 按宽度缓存，窗口宽度变化时重新创建。

流式渲染策略：

- raw markdown 始终完整保存在消息对象里。
- chunk 到达时标记 dirty，不立即每 chunk 渲染。
- 用 50ms debounce 批量调用 glamour 渲染。
- stream done 后做一次最终渲染。

---

## Config 页面

Config 管理模型连接和少量全局设置，不承担 Chat 状态展示。

Config 的设计目标不是暴露底层配置文件，而是帮助用户完成三件事：

1. 看清当前正在使用哪个模型连接。
2. 添加、修复或切换 provider/model。
3. 修改少量不会打断 Chat 的全局偏好。

Provider 配置采用“列表 → 详情 → 编辑”的渐进披露，不在 Config 首页直接堆完整表单。首次 setup 仍复用相同字段定义、校验和保存逻辑，但使用更短的 Quick Setup 流程。

Config 有两种模式：

| 模式 | 入口 | 行为 |
|---|---|---|
| setup mode | 首次启动无 config | 打开 Quick Setup，强制完成首个可用模型连接；保存后进入 Welcome；Esc 取消并返回空状态首页 |
| manage mode | Welcome / Chat `Ctrl+O` | 打开 Config 首页，管理 provider/model/general；Esc 只返回上一页面 |

两种模式复用同一套字段 schema、校验逻辑、凭证保存逻辑和 i18n 文案。区别只在信息披露层级：setup mode 只展示最小字段；manage mode 允许进入详情页编辑高级字段。

全局退出规则：

- `Ctrl+C` 是唯一退出 TUI 的全局快捷键。
- `Esc` 只做取消、关闭 overlay、返回上一层或返回首页，不退出程序。
- `q` 不作为退出快捷键，也不在页面提示里出现。

下方示例使用英文占位，实际 label/button/help/error 必须通过 i18n 渲染。

### Config 首页

Config 首页只做导航和状态概览，不直接编辑敏感字段。

```
┌──────────────────────────────────────────────────────────────┐
│  Config                                         [Esc] Back   │
│──────────────────────────────────────────────────────────────│
│                                                              │
│  ▸ Model Connections                                         │
│    Active    GLM / glm-4                                     │
│    Providers 2 configured · 1 needs attention                │
│                                                              │
│  ▸ General                                                   │
│    Language   中文 / English                                 │
│    Theme      Default                                        │
│                                                              │
│──────────────────────────────────────────────────────────────│
│  ↑↓ Navigate · Enter Open · Ctrl+E Edit General              │
└──────────────────────────────────────────────────────────────┘
```

首页条目：

| 条目 | 进入后 | 说明 |
|---|---|---|
| Model Connections | Provider 列表 | 管理 provider、model、active model |
| General | General 设置 | 语言、主题等轻量全局偏好 |

首页快捷键：

| 操作 | 按键 |
|---|---|
| 进入选中条目 | `Enter` |
| 返回来源页面 | `Esc` |
| 编辑 General | `Ctrl+E` |

`A` / `E` / `D` 不在首页生效，避免用户误以为可以在概览层直接做 provider CRUD。

### Provider 列表

Provider 列表负责查看、选择、激活和进入详情。列表里的信息必须短，只显示识别 provider 所需的状态，不显示完整 API Key 或长 endpoint。

```
┌──────────────────────────────────────────────────────────────┐
│  Model Connections                              [Esc] Back   │
│──────────────────────────────────────────────────────────────│
│                                                              │
│  ◉ GLM                                                       │
│    active · openai-compatible · glm-4 · endpoint configured  │
│                                                              │
│  ○ Anthropic                                                 │
│    inactive · anthropic · claude-sonnet-4-20250514           │
│                                                              │
│  ! Local OpenAI                                              │
│    missing API key · openai-compatible · llama3              │
│                                                              │
│──────────────────────────────────────────────────────────────│
│  ↑↓ Navigate · Enter Details · A Add · Space Activate · D Delete │
└──────────────────────────────────────────────────────────────┘
```

Provider 状态：

| 标记 | 含义 |
|---|---|
| `◉` | 当前 active provider/model |
| `○` | 已配置但未激活 |
| `!` | 配置不完整或校验失败 |

Provider 列表操作：

| 操作 | 按键 |
|---|---|
| 新增 | `A` |
| 删除 | `D` |
| 查看详情 | `Enter` |
| 激活 | `Space` |

规则：

- `Enter` 进入 Provider 详情，不直接激活，避免误操作。
- `Space` 激活当前 provider 的默认 model；如果 provider 配置不完整，显示错误并进入详情。
- 删除 active provider 前必须二次确认；删除后如果没有可用 provider，进入 Quick Setup 或停留在列表并提示必须配置一个 provider。
- API Key 只显示是否存在，不显示明文或掩码后的完整值。
- Endpoint 超长时只显示 `endpoint configured` 或域名摘要，完整值放到详情页。

### Provider 详情

详情页用于解释一个 provider 当前为什么可用或不可用，并提供进入编辑表单的入口。详情页默认只展示和测试，不承担列表级操作。

```
┌──────────────────────────────────────────────────────────────┐
│  Provider: GLM                                  [Esc] Back   │
│──────────────────────────────────────────────────────────────│
│                                                              │
│  Status     Active                                           │
│  Type       openai-compatible                                │
│  Endpoint   https://open.bigmodel.cn/api/paas/v4             │
│  API Key    configured                                       │
│                                                              │
│  Models                                                      │
│  ◉ glm-4                                                     │
│  ○ glm-4-plus                                                │
│                                                              │
│  Last check  connected                                       │
│                                                              │
│──────────────────────────────────────────────────────────────│
│  E Edit · T Test · Esc Back                                   │
└──────────────────────────────────────────────────────────────┘
```

详情页操作：

| 操作 | 按键 |
|---|---|
| 编辑 provider | `E` |
| 测试连接 | `T` |
| 返回列表 | `Esc` |

规则：

- Provider 和 Model 在详情页分开表达：Provider 是连接来源，Model 是该来源下的可选模型。
- 一个 provider 至少要有一个 model 才能激活。
- 激活、删除和新增 provider/model 都是列表级操作，不在详情页执行。
- `T` 只做轻量连通性检查，错误展示在详情页，不弹出长日志。
- 详情页可以显示完整 endpoint，但仍不能显示 API Key 明文。

### Provider 编辑表单

Provider 编辑表单使用 overlay，只编辑 provider 级字段。新增 provider 后继续进入 Model 编辑表单，或在同一 overlay 的下一步填写首个 model。

```
┌─ Add Provider ─────────────────────────┐
│  Type:     [openai ▾               ]   │
│  Name:     [GLM                    ]   │
│  API Key:  [sk-****                   ]│
│  Endpoint: [https://open.bigmodel...  ]│
│                                        │
│  Enter Next · Esc Cancel               │
└────────────────────────────────────────┘
```

Provider 字段：

| 字段 | 说明 | setup mode | manage mode |
|---|---|---|---|
| Type | 必填，下拉选择: openai / anthropic / openai-compatible | 必填 | 必填 |
| Name | 展示名，同时作为默认 provider id 的来源；保存时必须归一化成稳定 id | 必填 | 必填 |
| API Key | 存入 credentials.toml，不写入 config.toml | 必填 | 编辑时可留空表示不修改 |
| Endpoint | Type=openai/anthropic 时有默认值，可覆盖；Type=openai-compatible 时必填 | 可选/必填 | 可选/必填 |

Provider id 规则：

- 用户看到的是 `Name`，内部保存使用稳定 `provider_id`。
- 新增时由 `Name` 派生 `provider_id`，例如 `GLM` → `glm`。
- 如果 id 冲突，追加短后缀，例如 `glm-2`。
- 编辑 `Name` 不应改变已有 `provider_id`，避免 credentials 失效。

### Model 编辑表单

Model 编辑表单只编辑 model 级字段。

```
┌─ Add Model ────────────────────────────┐
│  Model ID: [glm-4                  ]   │
│  Label:    [GLM 4                  ]   │
│  Default:  [yes ▾                  ]   │
│                                        │
│  Enter Save · Esc Back                 │
└────────────────────────────────────────┘
```

Model 字段：

| 字段 | 说明 | setup mode | manage mode |
|---|---|---|---|
| Model ID | 实际模型 ID，用于请求模型 | 必填 | 必填 |
| Label | 展示名；为空时使用 Model ID | 可选 | 可选 |
| Context Window | 模型最大上下文 token 数；写入 `context_window` | 可选，默认按 provider | 可选 |
| Default | 是否作为该 provider 默认 model | 默认 yes | 可选 |

`context_window` 当前已经是底层配置能力：`internal/config.ModelConfig.ContextWindow` 会保存到 `config.toml` 的 `[[models]].context_window`，provider 构造时会传入模型实现；OpenAI 兼容默认 `128000`，Anthropic 默认 `200000`。TUI 设计必须把它作为模型级字段暴露出来。

实现要求：

- `ipc.ConfigModel` 需要补充 `context_window` 字段，`config.get` / `config.set` 必须透传。
- `internal/tui/config.go` 的表单需要增加 Context Window 输入项。
- 空值表示使用 provider 默认，不写入 TOML。
- 非空必须校验为正整数。
- Provider 列表和详情页应显示上下文摘要，例如 `ctx 128k`。
- Chat 顶栏的最大上下文来自 active model 的 `context_window`，没有配置时使用 provider 默认值。

### General 设置

General 设置复杂度低，可以在 Config 首页就地编辑，不需要进入独立详情页。

```
┌──────────────────────────────────────────────────────────────┐
│  Config                                         [Esc] Back   │
│──────────────────────────────────────────────────────────────│
│                                                              │
│  ▸ Model Connections                                         │
│    Active    GLM / glm-4                                     │
│    Providers 2 configured · 1 needs attention                │
│                                                              │
│  ▸ General                                                   │
│    Language   [中文 ▾]                                       │
│    Theme      [Default ▾]                                    │
│                                                              │
│──────────────────────────────────────────────────────────────│
│  ↑↓ Navigate · Enter Change · Esc Done                       │
└──────────────────────────────────────────────────────────────┘
```

General 编辑规则：

- 选中 General 行后，`Ctrl+E` 进入就地编辑状态。
- 编辑状态下 `↑↓` 在 General 字段之间移动，`Enter` 切换或打开候选值。
- `Esc` 退出 General 编辑状态并回到 Config 首页，不退出 TUI。
- General 只放低风险偏好项，例如 Language、Theme；复杂或敏感配置不放这里。

### Quick Setup

首次启动无 config 时进入 Quick Setup。它不是完整管理页，也不展示 General 设置。

```
┌─ Setup Model Connection ───────────────┐
│  Type:     [openai-compatible ▾    ]   │
│  Name:     [GLM                    ]   │
│  Model:    [glm-4                  ]   │
│  API Key:  [sk-****                ]   │
│  Endpoint: [https://open.bigmodel... ] │
│                                        │
│  Enter Save · Esc Back                 │
└────────────────────────────────────────┘
```

Quick Setup 规则：

- Quick Setup 可以把 provider 和首个 model 放在同一个短表单里，减少首次启动步骤。
- 保存时仍按 provider/model/credentials 三类数据落盘。
- 保存成功后设为 active model，连接 daemon，进入 Welcome。
- 取消 setup 时返回空状态首页，不进入 Chat，也不退出 TUI。
- 失败时错误显示在表单内，不跳转页面。

保存成功后：

- setup mode：写入 config.toml (provider/models/active_model) + credentials.toml (api_key)，连接 daemon，进入 Welcome。
- manage mode：写入 config.toml (provider/models/active_model 如有变化) + credentials.toml (api_key)，返回 Provider 详情或 Provider 列表，保留来源页面。

---

## Help 页面

Help 页面是完整快捷键和命令说明。Chat 内的 `?` 是浮层，`/help` 或 Welcome 中选择 Help 进入完整页面。

Help 页面所有说明文本通过 i18n 渲染；快捷键本身不翻译。

```
┌──────────────────────────────────────────────────────────────┐
│  Help                                           [Esc] Back   │
│──────────────────────────────────────────────────────────────│
│                                                              │
│  Shortcuts                                                   │
│  Enter        Send message                                   │
│  Shift+Enter  New line                                       │
│  Esc          Cancel / Back                                  │
│  Ctrl+N       New session                                    │
│  Ctrl+T       Toggle tool/thinking detail                    │
│  Ctrl+O       Open config                                    │
│  Ctrl+U/D     Scroll viewport                                │
│  ? / F1       Toggle help                                    │
│                                                              │
│  Commands                                                    │
│  /new              Start a new session                       │
│  /model <name>     Switch model                              │
│  /compact          Compact context                           │
│  /memory search Q  Search memory                             │
│  /help             Open help page                            │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

实现：

- 快捷键区用 `bubbles/help` 的 `FullHelpView`。
- 命令区手动渲染。
- 内容放入 viewport，支持滚动。

---

## 快捷键

| 快捷键 | Chat | Welcome | Config | Help |
|---|---|---|---|---|
| `Enter` | send / accept suggestion | select | select/save | - |
| `Shift+Enter` | newline | - | - | - |
| `Esc` | cancel/back/close overlay | back/home | back/cancel | back |
| `Ctrl+N` | new session | new session | - | - |
| `Ctrl+T` | toggle tool/thinking detail | - | - | - |
| `Ctrl+O` | config | config | - | - |
| `Ctrl+U/D` | scroll viewport | - | - | scroll |
| `?` / `F1` | toggle help overlay | help page | help overlay | - |
| `Ctrl+C` | quit | quit | quit | quit |

`keys.go` 统一定义 `key.Binding`，Help 浮层和 Help 页面复用同一份 keymap。

---

## 文件结构

```
internal/tui/
├── app.go              # TUI struct, Init/Update/View, mode 切换、IPC notification 分发
├── i18n.go             # TUI i18n helper，封装 key lookup 和 fallback
├── i18n_keys.go        # TUI 文案 key 注册表；所有用户可见文本必须来自这里或 internal/i18n
├── keys.go             # key.Binding 定义，Help 浮层和 Help 页面复用
├── styles.go           # lipgloss 样式和配色
├── pet.go              # 完整宠物 logo + Chat 3 行小 logo 三态
├── welcome.go          # Welcome 页面、入口菜单、daemon 概况
├── chat.go             # Chat 状态、Update、textarea、viewport、tool/thinking 运行态
├── chat_render.go      # Chat 视图布局、消息/tool/thinking 渲染拆分；若保留空文件需删除或补齐职责
├── compact_panel.go    # /compact 结果面板，展示压缩前后 token 和 context window 占比
├── config.go           # Config 首页、Provider 列表/详情、setup/manage mode、provider/model 表单 overlay
├── help.go             # Help 页面和 Chat help overlay
├── markdown.go         # glamour renderer
├── commands.go         # slash commands 和命令联想数据
├── statusbar.go        # token/context 格式化、topbar/bottombar 辅助函数
├── ipc_client.go       # TUI IPC client 接口和跨平台公共逻辑
├── ipc_client_unix.go  # Unix socket IPC client
└── ipc_client_windows.go # Windows named pipe IPC client
```

文件结构规则：

- 不新增独立 `setup.go`，除非只是一个很薄的 mode alias。首次配置走 `config.go` 的 setup mode，避免重复表单和重复 i18n key。
- 当前实现里 `chat.go` 同时承担 Update 和部分渲染，后续如果继续增长，可以把纯渲染函数迁移到 `chat_render.go`，但不要让同一类渲染逻辑散落在两个文件里。
- `statusbar.go` 不只负责底栏，也应承载 context/token 格式化辅助函数，避免 Chat 顶栏和 compact 面板各自实现格式化。
- `compact_panel.go` 已经存在，文档必须保留，因为它依赖 context window 信息。

---

## Bubbles / Glamour 使用清单

| 包 | 用途 |
|---|---|
| `viewport` | Chat 对话区、Help 页面滚动 |
| `textarea` | Chat 输入 |
| `spinner` | LLM 首包等待、tool 进行中 |
| `stopwatch` | thinking/tool 计时 |
| `help` | Chat help overlay、Help 页面快捷键区域 |
| `list` | Welcome 菜单、Config provider 列表 |
| `textinput` | Provider 表单 |
| `key` | 快捷键绑定和匹配 |
| `glamour` | Agent/Thinking Markdown 渲染 |

不使用 `table` 渲染 Chat 中的 Markdown 表格，因为 glamour 已经处理静态 Markdown 表格；`table` 更适合交互式数据表。

---

## IPC 需要的数据

TUI 需要 daemon 提供以下前端展示数据：

| 数据 | 用途 |
|---|---|
| active_model (provider/model) | Welcome、Chat 顶部身份区 |
| model list | Config provider 列表；每个模型需要包含 `context_window` |
| active model context_window | Chat 顶部身份区最大上下文、compact 面板百分比 |
| session current context tokens | Chat 顶部身份区当前上下文，例如 `ctx 12k/128k` |
| daemon connected/status | Welcome、Chat 顶部身份区 |
| session token input/output/cache | Chat 底栏 |
| token speed | Chat 底栏 |
| memory/session stats | Welcome 状态面板 |
| last session id/summary | Resume Last Session |
| unsent_input | Resume 后填充 textarea |

如果 `daemon.status` 已包含这些字段，优先扩展现有方法，不新增多余 IPC 方法。

Config setup/manage 复用相同配置 IPC：

| 方法 | 用途 |
|---|---|
| `config.get` | 读取 models/credentials/general 配置；`models[]` 必须包含 `context_window` |
| `config.set` | 保存 models/credentials/general 配置；upsert model 必须支持 `context_window` |
| `daemon.status` | 保存后刷新 active_model 状态 |

当前实现注意事项：

- `internal/config.ModelConfig` 已支持 `ContextWindow int` 和 TOML 字段 `context_window`。
- `internal/model.Provider.ContextWindow()` 已用于压缩和 provider 默认上下文。
- `internal/ipc.ConfigModel` 当前还缺少 `context_window`，需要补齐，否则 TUI 无法读写该配置。
- `internal/tui/config.go` 当前 provider 表单只有 Provider、Model、API Key、Endpoint、Strengths，也需要增加 Context Window。

---

## 实现优先级

1. **TUI i18n 基础**：封装 `tui.tr(key, args...)`，替换所有硬编码用户可见文案。
2. **Config/setup 统一链路**：Config 支持 setup/manage mode，Provider 表单只实现一套。
3. **Markdown 渲染**：用 glamour v2 替换自写解析器，解决表格和代码块问题。
4. **Chat 布局**：顶部身份区、对话区、输入区、命令联想、底栏五区域布局。
5. **消息渲染**：用户/Agent/Tool/Thinking 四类内容统一样式。
6. **Help 浮层**：Chat 内 `?` overlay，移除常驻帮助栏。
7. **命令联想**：`/` 前缀建议，`↑↓` 选择，`Enter` 填充。
8. **Welcome**：宠物 logo、状态面板、list 菜单。
9. **Resume**：恢复历史并填充 `unsent_input`。
