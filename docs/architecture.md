# 架构说明

本文描述 Suna 当前稳定架构，用于补充 README 的功能介绍。`plans/` 目录保留规划、调研和历史设计；本文只记录当前代码应遵守的事实边界。

## 总体分层

```text
CLI / main.go
    ↓
TUI / runtime 命令入口
    ↓ protocol + local / stdio transport
Daemon
    ↓
Agent / Runner / Model / Tools / Guard / Memory / Skill / MCP
```

核心原则：**TUI 只负责交互和渲染，业务状态、工具执行、模型调用、安全策略和持久化都由 daemon 侧模块承担**。

## CLI 与 daemon

`main.go` 负责命令分发：

- `suna`：启动 TUI，必要时自动拉起 daemon。
- `suna status`：查询 daemon 状态。
- `suna stop`：停止 daemon。
- `suna runtime --transport stdio`：启动单进程 headless runtime，供第三方 UI / 客户端通过 stdio 接入。

CLI 不承载业务逻辑，只做进程管理、入口适配和本地 transport 连接。

## TUI

TUI 是用户交互层，职责包括：

- 渲染聊天、配置、帮助、欢迎页。
- 接收键盘、粘贴、窗口尺寸等终端事件；剪贴板图片读取只作为 TUI 用户输入 fallback，不进入 daemon。
- 将用户操作转换成 protocol request。
- 将 daemon notification 和 method response 转成 Bubble Tea 消息并更新 UI 状态。

TUI 不应直接调用 runner、agent、tools、memory、guard 等业务包。

## Protocol 与 transport

TUI、第三方 runtime 客户端和 daemon 通过 `internal/protocol` 定义统一的方法、参数、结果和通知通信。Agent 输出按职责拆分为三类通知：`agent.delta` 只承载 assistant/reasoning 文本增量，`agent.run` 承载 run 生命周期、retry、失败错误和恢复能力，`agent.usage` 承载 token/context/耗时统计。

Transport 只负责连接、framing、握手策略和生命周期策略，不改变业务语义：

- `internal/transport/local`：Unix socket / Named Pipe，供官方 TUI 和本地 CLI 管理命令使用。
- `internal/transport/stdio`：`suna runtime --transport stdio`，供第三方 UI / 客户端使用。
- `internal/transport/jsonrpc`：local / stdio 共用的 JSON-RPC request、response、notification、结构化错误和 hello gate。

TUI 侧只保留适配层：

```text
internal/tui/transport
```

该适配层只负责：

- 连接 daemon。
- 发起 protocol request 并接收 method response。
- 接收 daemon notification。

连接建立本身只注册本地 event sink；TUI 初始展示状态通过 `daemon.status`、`config.get` 等 request 主动拉取，后续运行过程再消费 daemon notification。method response 不会被伪装成 daemon notification，而是转换为 TUI 本地 typed message。

## Daemon

daemon 是按需运行的本地后台服务，负责协调核心能力：

- 会话生命周期。
- 模型配置和状态。
- Agent 运行。
- 工具调用。
- Guard 审核。
- 记忆、Skill、附件、用量等本地状态。

TUI 重构或 UI 交互调整不应改变 daemon 的业务语义。

## Daemon 生命周期

当前版本是单 daemon、多 session 形态：daemon 可以持有多个持久化 session；每个 session 同一时间最多一个 active run，并按需建立自己的 session runtime。不提供 trigger/cowork/perception 等长期后台任务。daemon 生命周期由 transport 声明的 retention policy 和当前连接数共同决定：

- local transport 使用 `idle_exit`：打开 TUI 或执行需要 daemon 的 CLI 命令时，如果 daemon 未运行，会自动后台启动；最后一个客户端断开后，daemon 进入短暂宽限期，如果没有新连接，会取消当前 agent run 并退出。
- stdio runtime 使用 `client_bound`：父进程关闭 stdio / 连接结束后，runtime 退出。runtime v0 不支持多个 Suna 进程同时写入同一数据目录；第三方 UI 应独占启动 runtime，未来如需强制单 owner，应使用系统级文件锁或 named mutex，而不是普通 lock 文件。
- 未来 server transport 可使用 `persistent`：即使暂时没有客户端也保持监听。
- `suna stop`、`SIGTERM`、`SIGINT` 也会进入同一类关闭流程。

未来如果引入 trigger/cowork/perception，再通过明确的 activity/drain 机制扩展生命周期，不应把业务收尾隐式塞进资源 `Close`。

## Agent / Runner / Tools / Guard

- Agent 负责任务决策、上下文管理、Guard 编排和工具执行入口；session 相关请求必须传递当前 session runtime 的显式 `ModelBinding`。
- Runner 执行模型流式调用和工具调用循环，只依赖 Agent 提供的 tool schema、executor 和 binding。
- `internal/model.Router` 维护全局模型 provider/config registry，按 model ref 创建 binding，不拥有“当前模型”；`ModelBinding` 是一次显式模型选择的不可变快照，统一承载 provider、模型配置、限流、reasoning 注入、日志和调用校验。Guard、Skill、compact 等辅助调用复用同一 binding，禁止隐式回退到 `active_model`。
- `internal/tools` 是统一工具目录和执行路由，所有模型可见工具都应通过 Provider 注册到 `tools.Manager`。
- `internal/tools/builtin` 提供本地内置工具，`internal/tools/skilltools` 适配 Skill Runtime，`internal/tools/agenttools` 适配 `askuser` / `spawn` 这类 Agent runtime 工具，`internal/tools/mcptools` 将已连接的 MCP tools 适配为模型可见工具。
- `internal/mcp` 管理 MCP server 生命周期、stdio transport、JSON-RPC、tools/list 和 tools/call；当前只承诺 tools-only 的基础 MCP，不支持 resources、prompts、sampling、OAuth 或 sandbox。
- Guard 对写文件、执行命令、HTTP 写请求等行动类操作做风险控制；工具是否跳过 Guard 由工具 `Spec` 的 Guard policy 声明，默认应走 Guard。

`tools.Manager` 只维护工具目录、稳定 schema 和执行路由，不应做安全决策；Guard 仍由 Agent 持有当前会话上下文后统一处理。工具 schema 应保持稳定顺序，避免影响模型前缀缓存命中。

这些模块的默认值、超时、权限边界应在各自所属层或统一配置处维护，避免由 TUI 猜测或层层透传。

## 配置与持久化

运行数据默认位于 `~/.suna/`：

- `config.toml`：主配置。
- `credentials.toml`：凭据。
- `memory.db`：记忆、会话、用量等本地数据。
- `skills/`：Skill 目录。
- `attachments/`：附件缓存。
- `logs/app.log`：日志。

MCP server 配置位于 `config.toml` 的 `[mcp.servers.<name>]`。daemon 启动时会尝试启动 enabled 的 stdio server；单个 server 启动失败不会阻塞 Suna，错误通过 MCP 状态接口和 TUI `/mcp` 面板展示。MCP 工具公共名使用 `mcp__<server>__<tool>`，二进制结果会保存到附件目录并以文本引用返回。

TUI 通过 protocol 获取 attachment root，用于保存用户粘贴的 data URI / 剪贴板图片；这属于 UI 输入落盘，最终仍以普通 attachment ref 发送给 daemon。daemon 不读取系统剪贴板。

TUI 可以缓存配置快照用于展示，但真实持久化状态以 daemon 为准。

## 记忆与会话状态

Suna 当前是单 daemon、多 session 形态。全局 config/runtime 共享；session 隔离 cwd、对话状态、附件、working state 和 run state。

- global runtime：daemon 级共享运行时，持有配置快照、Model Router、工具目录、Guard 基础设施、Skill/MCP runtime，以及 memory worker 等跨 session 资源；它不代表某个 session 的当前模型或会话上下文。
- session runtime：按 `session_id` 建立的单次 session 运行上下文，从 `sessions.model_ref` 解析一次显式 `ModelBinding`。主 run、Guard、Skill、compact 和 memory candidate extraction 等 session 相关调用都必须复用该 binding。

模型选择边界如下：`active_model` 只用于新 session 的默认模型，以及 legacy session 首次迁移时的一次性 materialize；既有 session 的实际模型由 `sessions.model_ref` 决定。`memory_queue` 只持久化 `model_ref`，不保存 `session_id`，worker 按该 ref 解析 binding 后执行异步提取，不能回退到当前 `active_model`。

记忆系统分工如下：

- `user_profile_memory`：长期 user profile memory，只保存少量跨会话稳定的用户偏好、习惯、约束和纠错。
- `sessions`：session 元数据，包含 title、cwd、`model_ref`、message_count 和时间戳；`model_ref` 是该 session 实际使用的模型，不随 `active_model` 后续变化。
- `session_state`：每个 session 的 Session State、最近可见 user/assistant 消息和 TUI-only 有界工具摘要；attach/create snapshot 返回这些展示状态，不回放完整 tool timeline。
- `memory_queue`：user profile memory 的临时提取队列，每条 item 只记录不可变的 `model_ref`，不绑定 `session_id`；daemon worker 按批量策略处理后删除；daemon 退出不会为未开始的队列强制触发记忆提取，pending item 会留在 SQLite 中等待下次启动恢复。

模型请求的缓存友好结构为：稳定 system/project/skill/tool schema 前缀 + 低频变化的 Session State + append-only recent messages + 靠近 latest user 的 user profile memory。Session State 不拼进 system prompt；user profile memory 也不放在 prior conversation 前面。

自动 compact 按模型能力参数计算输入预算：`context_window - max_output_tokens - margin`，其中 `margin = max(2048, context_window / 200)`；触发判断使用 `estimated_context_tokens + estimator_safety_tokens`，`estimator_safety_tokens = max(8192, estimated_context_tokens / 16)`。compact 成功后，`session_state.compacted_state` / `CompletionRequest.SessionState` 保存新的会话状态，working memory 只保留 budget-aware recent window；改写 `WorkingMemory` 时会复制新的消息 slice，避免 compact 后的 recent window 继续持有旧历史 backing array。compact 失败时不使用 fallback、不硬裁剪继续，并通过 TUI 显示错误。

## 文档分工

- `README.md`：项目门面，突出亮点、快速开始、常用操作、安全提醒和 docs 入口。
- `docs/README.md`：文档索引和推荐阅读路径。
- `docs/runtime-stdio.md`：第三方 UI / 客户端通过 stdio runtime 接入 Suna。
- `docs/protocol.md`：统一 method、result、notification、错误和消息 schema。
- `docs/design.md`：关键设计和取舍，包括架构、安全、上下文、性能、记忆、Skill、MCP 等。
- `docs/architecture.md`：稳定架构、模块边界和 daemon 生命周期。
- `docs/code-map.md`：功能到代码位置、主要包职责和核心流程。
- `docs/current-implementation.md`：当前实现事实和未完成边界。
- `docs/configuration.md`：配置字段和示例。
- `docs/development.md`：构建、测试和维护约定。
- `plans/`：规划、调研、历史设计和阶段性记录，不作为当前实现依据。
- 子包 README：仅当某个包足够复杂且必须贴近代码维护时再新增。
