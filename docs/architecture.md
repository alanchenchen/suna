# 架构说明

本文描述 Suna 当前稳定架构，用于补充 README 的功能介绍。`plans/` 目录保留规划、调研和历史设计；本文只记录当前代码应遵守的事实边界。

## 总体分层

```text
CLI / main.go
    ↓
TUI / 命令入口
    ↓ protocol + local transport
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

CLI 不承载业务逻辑，只做进程管理、入口适配和本地 transport 连接。

## TUI

TUI 是用户交互层，职责包括：

- 渲染聊天、配置、帮助、欢迎页。
- 接收键盘、粘贴、窗口尺寸等终端事件。
- 将用户操作转换成 protocol request。
- 将 daemon notification 转成 Bubble Tea 消息并更新 UI 状态。

TUI 不应直接调用 runner、agent、tools、memory、guard 等业务包。

## Protocol 与 local transport

TUI 和 daemon 通过 `internal/protocol` 定义的方法、参数和通知通信。

本地连接由 `internal/transport/local` 承载，TUI 侧只保留适配层：

```text
internal/tui/transport
```

该适配层只负责：

- 连接 daemon。
- 发起 protocol request。
- 接收 daemon notification。
- 将少量同步查询结果转换为 TUI 可消费的通知。

连接建立本身只注册本地 event sink；TUI 初始展示状态通过 `daemon.status`、`config.get` 等 request 主动拉取，后续运行过程再消费 daemon notification。

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

当前版本是单 agent、单会话形态，没有 trigger/cowork/perception 等长期后台任务。daemon 生命周期按客户端连接驱动：

- 打开 TUI 或执行需要 daemon 的 CLI 命令时，如果 daemon 未运行，会自动后台启动。
- 每个 local transport 连接建立时注册 event sink，断开时注销；连接数是 daemon 是否继续运行的主要依据。
- 最后一个客户端断开后，daemon 进入短暂宽限期；如果没有新连接，会取消当前 agent run 并退出。
- `Close` 语义只释放资源，不启动新的业务工作；记忆整理需要由 worker 正常批量策略或未来显式 drain 流程触发。
- 未开始处理的 `memory_queue` 持久化在 SQLite 中，daemon 退出时不强制 compaction，下次启动后通过 recover signal 继续按批量策略处理。
- `suna stop`、`SIGTERM`、`SIGINT` 也会进入同一类关闭流程。

未来如果引入 trigger/cowork/perception，再通过明确的 activity/drain 机制扩展生命周期，不应把业务收尾隐式塞进资源 `Close`。

## Agent / Runner / Tools / Guard

- Agent 负责任务决策、上下文管理、Guard 编排和工具执行入口。
- Runner 执行模型流式调用和工具调用循环，只依赖 Agent 提供的 tool schema 与 executor。
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

TUI 可以缓存配置快照用于展示，但真实持久化状态以 daemon 为准。

## 记忆与会话状态

Suna 当前是单用户单当前会话形态，不提供多会话管理或完整历史搜索。记忆系统分工如下：

- `user_profile_memory`：长期 user profile memory，只保存少量跨会话稳定的用户偏好、习惯、约束和纠错。
- `conversation_state.session_state`：当前会话的 Session State，由 compact 生成/更新，保存 active context、完成任务/话题账本、用户要求、关键决策、tool facts 和 open threads。
- `conversation_state.last_messages`：TUI 恢复展示用的真实可见 user/assistant 对话；不保存 system state、原始 tool call/result 或 raw 结构。
- `conversation_state.tool_summary`：TUI-only 的工具摘要，恢复时展示给用户，不作为原始 tool 上下文注入模型。
- `memory_queue`：user profile memory 的临时提取队列，daemon worker 按批量策略处理后删除；daemon 退出不会为未开始的队列强制触发记忆提取，pending item 会留在 SQLite 中等待下次启动恢复。

模型请求的缓存友好结构为：稳定 system/project/skill/tool schema 前缀 + 低频变化的 Session State + append-only recent messages + 靠近 latest user 的 user profile memory。Session State 不拼进 system prompt；user profile memory 也不放在 prior conversation 前面。

自动 compact 按模型能力参数计算输入预算：`context_window - max_output_tokens - margin`，其中 `margin = max(2048, context_window / 200)`。compact 成功后，`conversation_state.session_state` / `CompletionRequest.SessionState` 保存新的会话状态，working memory 只保留 budget-aware recent window。compact 失败时不使用 fallback、不硬裁剪继续，并通过 TUI 显示错误。

## 文档分工

- `README.md`：项目门面，突出亮点、快速开始、常用操作、安全提醒和 docs 入口。
- `docs/README.md`：文档索引和推荐阅读路径。
- `docs/design.md`：关键设计和取舍，包括架构、安全、上下文、性能、记忆、Skill、MCP 等。
- `docs/architecture.md`：稳定架构、模块边界和 daemon 生命周期。
- `docs/code-map.md`：功能到代码位置、主要包职责和核心流程。
- `docs/current-implementation.md`：当前实现事实和未完成边界。
- `docs/configuration.md`：配置字段和示例。
- `docs/development.md`：构建、测试和维护约定。
- `plans/`：规划、调研、历史设计和阶段性记录，不作为当前实现依据。
- 子包 README：仅当某个包足够复杂且必须贴近代码维护时再新增。
