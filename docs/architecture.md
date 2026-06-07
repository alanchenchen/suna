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
Agent / Runner / Model / Tools / Guard / Memory / Skill
```

核心原则：**TUI 只负责交互和渲染，业务状态、工具执行、模型调用、安全策略和持久化都由 daemon 侧模块承担**。

## CLI 与 daemon

`main.go` 负责命令分发：

- `suna`：启动 TUI，必要时自动拉起 daemon。
- `suna start`：后台启动 daemon。
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

daemon 是长期运行的本地服务，负责协调核心能力：

- 会话生命周期。
- 模型配置和状态。
- Agent 运行。
- 工具调用。
- Guard 审核。
- 记忆、Skill、附件、用量等本地状态。

TUI 重构或 UI 交互调整不应改变 daemon 的业务语义。

## Agent / Runner / Tools / Guard

- Agent 负责任务决策、上下文管理、Guard 编排和工具执行入口。
- Runner 执行模型流式调用和工具调用循环，只依赖 Agent 提供的 tool schema 与 executor。
- `internal/tools` 是统一工具目录和执行路由，所有模型可见工具都应通过 Provider 注册到 `tools.Manager`。
- `internal/tools/builtin` 提供本地内置工具，`internal/tools/skilltools` 适配 Skill Runtime，`internal/tools/agenttools` 适配 `askuser` / `spawn` 这类 Agent runtime 工具。
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

TUI 可以缓存配置快照用于展示，但真实持久化状态以 daemon 为准。

## 文档分工

- `README.md`：用户入口、功能说明、安装和常用操作。
- `docs/`：稳定的开发和架构文档。
- `plans/`：规划、调研、历史设计和阶段性记录。
- 子包 README：仅当某个包足够复杂且必须贴近代码维护时再新增。
