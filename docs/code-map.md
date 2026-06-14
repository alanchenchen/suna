# 代码地图

本文帮助读者从功能定位到代码，并用流程图说明核心链路。更高层的设计取舍见 [关键设计](design.md)，模块边界见 [架构说明](architecture.md)。本文只记录当前实现，不以 `plans/` 为准。

## 功能到代码位置

| 功能 | 主要代码位置 | 说明 |
|---|---|---|
| CLI 启动 | `main.go`, `daemon_cmd.go` | `suna` 打开 TUI；`status` / `stop` 管理 daemon。 |
| daemon 自动拉起 | `daemon_cmd.go`, `internal/transport/local` | TUI 连接失败时后台启动同一可执行文件作为 daemon。 |
| daemon 服务 | `internal/daemon` | 协调配置、会话、Agent、附件、Skill、MCP、状态通知。 |
| daemon 生命周期 | `internal/daemon/lifecycle.go` | 最后客户端断开后短暂等待并退出；停止时取消当前 run。 |
| protocol | `internal/protocol` | 定义 request、notification、事件和多模态消息结构。 |
| 本地 transport | `internal/transport/local` | macOS/Linux Unix socket，Windows Named Pipe。 |
| TUI 主体 | `internal/tui` | Bubble Tea app、页面切换、事件适配、主题、i18n。 |
| Chat 页面 | `internal/tui/pages/chat`, `internal/tui/chat*.go` | 对话、输入、附件、工具展示、Guard、AskUser、模型/Skill/MCP 浮层；transcript 使用全局 offset + visible window 渲染长历史。 |
| Chat transcript 性能 | `internal/tui/pages/chat/transcript.go`, `internal/tui/chat.go`, `internal/tui/chat_render.go` | 维护 transcript blocks、全局滚动 offset、visible window、Markdown render cache 和滚轮/PageUp/PageDown 适配。 |
| Config 页面 | `internal/tui/pages/config`, `internal/tui/config*.go` | 模型、Guard、Workspace、UI、附件等配置。 |
| Welcome 页面 | `internal/tui/pages/welcome` | 版本、active model、用量、daemon、memory、Guard、Workspace 状态。 |
| Help 页面 | `internal/tui/pages/help` | 快捷键和 slash commands。 |
| 附件识别 | `internal/tui/components/attachment` | 识别本地图片路径、图片 URL、data URI。 |
| 附件存储 | `internal/media`, `internal/daemon/attachments.go` | 本地附件缓存和消息附件提交。 |
| 模型路由 | `internal/model/router.go` | 根据 provider / model 配置选择 provider。 |
| OpenAI Responses | `internal/model/openai_responses.go` | `provider = "openai"` 的请求和流式响应适配。 |
| Anthropic Messages | `internal/model/anthropic.go` | `provider = "anthropic"` 的 Messages 请求适配；当前为非 streaming 调用，尚未归一输出 thinking chunk。 |
| OpenAI-compatible | `internal/model/openai_chat.go` | 其它 provider 默认走 Chat Completions 兼容协议。 |
| Agent 编排 | `internal/agent` | 构造上下文、处理工具、Guard、记忆、Skill、MCP、Subtask。 |
| Runner | `internal/runner` | 模型流式调用、tool call 循环、上下文压缩。 |
| 工具目录 | `internal/tools` | 工具 Provider、schema、Manager、执行路由。 |
| 内置工具 | `internal/tools/builtin` | 文件、目录、搜索、命令、HTTP 等内置工具。 |
| Agent runtime 工具 | `internal/tools/agenttools` | `askuser`、`spawn`。 |
| Skill 工具 | `internal/tools/skilltools` | `skill_load`、`skill_start`。 |
| MCP 工具适配 | `internal/tools/mcptools` | 将 MCP tools 注册为 `mcp__<server>__<tool>`。 |
| Guard | `internal/guard` | 工具风险识别、敏感路径、Workspace、Smart Review 审查输入。 |
| 配置 | `internal/config` | 数据目录、默认配置、TOML 读写、凭据文件。 |
| 记忆和会话 | `internal/memory` | user profile memory、memory queue、conversation state、compact 支撑。 |
| Skill runtime | `internal/skill` | Skill 扫描、导入、检查、review、启用状态和运行时索引。 |
| MCP runtime | `internal/mcp` | stdio server 生命周期、JSON-RPC、tools/list、tools/call。 |
| Subtask | `internal/subtask`, `internal/agent/tools.go`, `internal/tools/agenttools` | 主 Agent 通过 `spawn` 动态选择模型、上下文、图片和工具白名单，创建独立上下文子任务。 |
| Prompt 模板 | `internal/prompt/templates` | system、compact、memory extract、guard review、skill review、subtask。 |
| 日志 | `internal/logging` | 应用日志，默认写入 `~/.suna/logs/app.log`。 |

## 主要包职责

### 入口和 TUI

- `main.go`、`daemon_cmd.go`：CLI 命令、daemon 进程管理入口。
- `internal/tui`：终端 UI、页面、快捷键、slash command、daemon 事件适配。
- `internal/tui/transport`：TUI 侧本地连接适配，不承载业务语义。

TUI 不应直接调用 `agent`、`runner`、`tools`、`guard`、`memory`、`skill`、`mcp` 等业务包。

### 通信和 daemon

- `internal/protocol`：TUI 与 daemon 的方法、参数和通知类型。
- `internal/transport/local`：Unix socket / Named Pipe 本地 transport。
- `internal/daemon`：长期运行服务，协调配置、会话、Agent、附件和状态通知。

### Agent 核心

- `internal/agent`：主 Agent 编排、上下文、工具执行入口、Guard、Skill/MCP/Subtask 适配。
- `internal/runner`：模型调用循环、流式输出、工具调用循环和上下文压缩。
- `internal/model`：模型 provider、路由、请求/响应适配和 token 估算。
- `internal/subtask`：独立上下文的子任务执行器，由主 Agent 通过 `spawn` 动态分配模型、输入和工具白名单。

### 工具、安全和扩展

- `internal/tools`：统一工具目录、Provider、schema 和执行路由。
- `internal/tools/builtin`：文件、命令、HTTP 等内置工具。
- `internal/tools/agenttools`：`askuser`、`spawn`。
- `internal/tools/skilltools`：Skill 工具适配。
- `internal/tools/mcptools`：MCP tools 适配。
- `internal/guard`：风险识别、Smart Review 输入、Workspace 和敏感路径规则。
- `internal/skill`：Skill 加载、检查、review、启用和运行时索引。
- `internal/mcp`：MCP stdio tools-only runtime。

### 状态和配置

- `internal/config`：配置、凭据和默认路径。
- `internal/memory`：SQLite 存储、user profile memory、会话状态、memory worker 和 compact 辅助。
- `internal/media`：附件存储。
- `internal/logging`：日志。

## 核心流程

### 启动流程

```text
用户执行 suna
  ↓
CLI 检查 daemon 是否可连接
  ↓
不可连接则后台拉起 daemon
  ↓
TUI 通过 local transport 连接 daemon
  ↓
TUI 主动拉取 daemon.status / config.get 等初始状态
  ↓
展示 Welcome 或进入 Chat
```

### 一轮对话流程

```text
用户在 TUI 输入消息
  ↓
TUI 发送 protocol request
  ↓
daemon 接收并准备当前会话、附件和配置
  ↓
Agent 组装上下文、工具目录、记忆、Skill index、Session State
  ↓
Runner 调用 active model 并流式接收事件
  ↓
模型输出文本 / reasoning / tool call
  ↓
如果有 tool call，Agent 接管并进入工具流程
  ↓
工具结果返回 Runner，必要时继续模型循环
  ↓
daemon 把流式事件通知 TUI
  ↓
会话状态、工具摘要、记忆队列等写入本地存储
```

### 工具调用流程

```text
模型提出 tool call
  ↓
Runner 解析 tool call
  ↓
Agent 根据工具名查 tools.Manager
  ↓
Agent 调用 Guard 评估风险
  ↓
按 Guard mode 自动放行 / 请求确认 / Smart Review / 拒绝
  ↓
通过 tools.Manager 路由到具体 Provider 执行
  ↓
工具结果返回模型循环和 TUI 工具详情
```

### Guard 流程

```text
tool call + 当前会话上下文
  ↓
工具 Guard policy
  ↓
只读 / 行动类判断
  ↓
敏感路径、blocked rule、Workspace 边界
  ↓
Guard mode：readonly / ask / auto / smart
  ↓
执行、拒绝、请求用户确认或 Smart Review
```

### 上下文压缩流程

```text
准备模型请求
  ↓
估算完整请求是否接近上下文窗口安全阈值
  ↓
未超过：直接请求模型
  ↓
超过：调用 compact prompt 生成新的 Session State
  ↓
compact 成功：保存 Session State，保留 budget-aware recent window
  ↓
compact 失败：停止本轮请求并提示错误
```

### Skill 流程

```text
Skill 目录 / 导入源
  ↓
解析 SKILL.md 元信息
  ↓
静态检查
  ↓
可选 LLM review
  ↓
用户确认是否启用
  ↓
Agent 获得 active skill index
  ↓
模型需要时调用 skill_load(name) 读取完整 Skill
```

### MCP 流程

```text
读取 config.toml 中 enabled MCP server
  ↓
启动 stdio server
  ↓
initialize
  ↓
tools/list
  ↓
注册为 mcp__<server>__<tool>
  ↓
模型调用 MCP tool
  ↓
tools/call
  ↓
结果返回模型；二进制结果保存为附件引用
```

### Subtask 流程

```text
主 Agent 判断需要委派
  ↓
根据任务性质、模型能力和上下文窗口选择 model
  ↓
编写自包含 task，裁剪 context，选择 input_images
  ↓
按最小权限选择 tools 白名单
  ↓
spawn 调用 internal/agent.ExecuteSpawnTool
  ↓
校验模型存在、工具可授权、图片索引有效
  ↓
internal/subtask 创建新的 working memory
  ↓
runner 使用指定 model 运行子任务
  ↓
子任务工具调用经白名单、Guard、Workspace 和敏感路径规则
  ↓
最终结果以 spawn tool result 返回主 Agent
  ↓
主 Agent 汇总、采纳或继续执行
```

更多说明见 [Subtask 设计](subtask.md)。

## 常见阅读入口

- 想看“一轮对话怎么跑”：`internal/daemon/service.go` → `internal/agent/agent.go` → `internal/runner/runner.go`。
- 想看“工具怎么暴露给模型”：`internal/tools/manager.go` 和各 `internal/tools/*/provider.go`。
- 想看“风险操作怎么拦”：`internal/agent/tools.go`、`internal/guard/guard.go`、`internal/guard/tool_risk.go`。
- 想看“模型怎么接入”：`internal/model/provider.go`、`router.go` 和具体 provider 文件。
- 想看“TUI 怎么和 daemon 通信”：`internal/tui/transport/client.go`、`internal/protocol`、`internal/transport/local`。
- 想看“Skill 怎么生效”：`internal/skill/runtime.go`、`internal/tools/skilltools/provider.go`、`internal/agent/skill_adapters.go`。
- 想看“Subtask 如何动态分配模型和工具”：`docs/subtask.md`、`internal/agent/tools.go`、`internal/subtask/subtask.go`、`internal/tools/agenttools/provider.go`。
- 想看“MCP 怎么接入”：`internal/mcp/runtime.go` 和 `internal/tools/mcptools/provider.go`。
