# 16. Multi-session and Handoff Design

## 背景

Suna 当前是单 daemon、单 agent、单会话模型。这个模型在普通对话里足够简单，但在 coding 场景会遇到明显限制：用户可能同时在多个 cwd / 项目中使用 Suna，目前只能共用一个会话上下文，容易互相污染，也无法自然支持多个 TUI 或未来 GUI 连接同一个运行时。

本设计目标是在保持 Suna「本地终端 AI Agent」定位的前提下，引入多 session，并为多个 UI 接力使用同一 session 预留 Handoff 能力。

## 结论

采用以下模型：

```text
single daemon
  global runtime resources
  session manager
    session A
    session B
    session C
  client manager
    conn -> attached session
```

核心原则：

- 仍然只有一个 daemon 进程。
- config、model router、skills、MCP、memory store 等 runtime resources 全局共享。
- session 是对话、上下文、cwd、附件和 run state 的隔离单位。
- 每个 session 内仍是 single-agent，不引入多 agent cowork。
- client 必须 attach 到某个 session 后才能执行 agent 操作。
- TUI 的 New / Resume / Join active 只是 UI 语义；protocol 只提供 session 原语。
- 当前阶段做 Handoff，不做真正 cowork。

## 目标

- 支持一个 daemon 管理多个 session。
- 支持多个 TUI 在不同 cwd 下分别使用独立 session。
- 支持通过 attach 加入 active session，实现 Handoff。
- session cwd 成为 Agent 默认工作目录。
- 附件按 session 隔离。
- 保持 TUI 当前体验：当前 cwd 有历史 session 时默认 Resume，可选 New；有 active session 时可选 Join。
- 支持 stdio / 第三方 UI 通过 protocol 自行管理多 session。

## 非目标

- 不做多 agent cowork。
- 不做多人同时编辑同一个 session。
- 不引入 Workspace 一等概念。
- 不绑定 git branch。
- 不持久化完整 tool event / stream delta event。
- 不做 soft delete。
- v1 不做 pin。
- 不为旧 protocol 做兼容；只做 DB migration。

## 概念定义

### Daemon

全局唯一运行时，持有全局资源和 session/client 管理器。

### Runtime resources

全局共享资源：

- config.toml
- model router / providers
- skills runtime
- MCP runtime
- memory store / SQLite
- tool provider registry
- logging

运行中修改 config 时，config 是 daemon-level 设置，影响之后的新 run。已经开始的 run 使用启动时的 runtime snapshot，不在中途切换。

### Session

session 是 agent 上下文单位，包含：

- session id
- cwd
- title，可为空
- visible messages
- compacted session state
- tool summary
- per-session working memory
- per-session guard
- per-session attachment root
- per-session run state

cwd 是 session 的默认工作目录，不是 daemon 的访问控制策略。

### Client

client 是一个 TUI / GUI / stdio UI / 未来 Web UI 连接。

一个 client 同时只 attach 一个 session。attach 新 session 时自动 detach 原 session。

### Handoff

Handoff 是多个 UI attach 到同一个 active session 的接力使用能力。

v1 语义：

- 多个 client 可以 attach 同一个 session。
- running 时非 run owner 只能观察。
- askuser / guard confirm 只由 run owner 处理。
- idle 后任意 attached client 可以发送下一条消息。

这不是 cowork；cowork 留到未来阶段。

## 数据模型

### sessions

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    message_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    last_attached_at DATETIME
);
```

说明：

- `title` 可为空；UI 展示时可回退到第一条用户消息、cwd basename 或 Untitled。
- `cwd` 由 `session.create` 传入并 canonicalize。
- `message_count` 用于列表展示和判断空 session。
- 不存 `active`、`client_count`、`status`、`running`，这些都是 runtime 派生状态。

### session_state

```sql
CREATE TABLE IF NOT EXISTS session_state (
    session_id TEXT PRIMARY KEY,
    compacted_state TEXT NOT NULL DEFAULT '',
    last_messages TEXT NOT NULL DEFAULT '[]',
    tool_summary TEXT NOT NULL DEFAULT '[]',
    updated_at DATETIME NOT NULL
);
```

说明：

- `last_messages` 保存 TUI attach 后可见恢复内容。
- `compacted_state` 保存压缩后的上下文状态。
- `tool_summary` 只用于恢复展示，不持久化完整 tool timeline。

### DB migration

旧版 `conversation_state` 只有一条默认会话。升级时：

1. 如果旧 `conversation_state` 存在有效 `last_messages` 或 `session_state`，创建一个 migrated session。
2. 将旧 `session_state`、`last_messages`、`tool_summary` 写入新 `session_state`。
3. migrated session 的 cwd 使用可用的当前默认 cwd / config workspace 兜底。
4. 迁移事务成功后 drop 旧 `conversation_state` 表，避免长期保留兼容表和兼容读取路径。

迁移代码只保留从旧单会话到新多会话的必要升级逻辑；运行时不保留旧 `conversation_state` 兼容分支，也不需要兼容旧 protocol。

## Runtime 状态

运行态由 daemon 内存维护：

```go
type SessionStatus string

const (
    SessionStatusIdle       SessionStatus = "idle"
    SessionStatusRunning    SessionStatus = "running"
    SessionStatusWaiting    SessionStatus = "waiting"
    SessionStatusCompacting SessionStatus = "compacting"
)
```

`active` 不入库，返回 session list 时派生：

```text
active = client_count > 0 || status != idle
```

`client_count` 来自当前 daemon 的 attached clients。

## Protocol

v1 只提供 session 原语。

### session.list

返回全部 session。daemon 不做 limit / paging。UI 自行过滤、排序、展示。

可选参数可以包含过滤条件，但不是访问策略：

```json
{
  "cwd": "/Users/alan/project-a",
  "active_only": false
}
```

返回项包含 DB 元数据和 runtime 派生状态：

```json
{
  "sessions": [
    {
      "id": "...",
      "title": "",
      "cwd": "/Users/alan/project-a",
      "message_count": 12,
      "created_at": "...",
      "updated_at": "...",
      "last_attached_at": "...",
      "status": "idle",
      "client_count": 0,
      "active": false
    }
  ]
}
```

### session.create

创建 session，并默认 attach 当前 client。

```json
{
  "cwd": "/Users/alan/project-a",
  "title": ""
}
```

返回 `SessionSnapshot`。

cwd 必须由 client 传入。daemon 不使用 daemon 进程 cwd 作为主要来源，只能作为兜底。

### session.attach

attach 到指定 session。

```json
{
  "session_id": "..."
}
```

返回 `SessionSnapshot`。

Resume / Join / Switch 在 protocol 层都是 attach，区别由 UI 定义。

### session.update

更新 session 元信息。

```json
{
  "session_id": "...",
  "title": "Fix daemon session manager",
  "cwd": "/Users/alan/project-a"
}
```

v1 至少支持 title；cwd update 可以保留但应谨慎暴露到 TUI。

### session.delete

hard delete。

```json
{
  "session_id": "..."
}
```

行为：

- 删除 session metadata。
- 删除 session_state。
- 删除该 session attachment 目录。
- running / waiting / compacting 禁止 delete。
- 有其他 client attached 时禁止 delete。
- 只有当前 client attached 且 idle 时允许 delete，并自动 detach 当前 client。

### SessionSnapshot

`session.create` 和 `session.attach` 返回：

```json
{
  "session": { ... },
  "messages": [ ... ],
  "compacted": false,
  "tool_summary": { ... },
  "current_run": { ... }
}
```

`current_run` 是可选 runtime view，只在 active running session 中返回。

## Attach 与历史恢复

不再使用旧 `session.restore` notification 语义。

`session.attach` 返回 snapshot：

- attach 当前 cwd 历史 session 时，TUI 渲染效果应与现有 restore 基本一致。
- attach 其他 cwd active session 时，TUI 进入 Handoff 视图。
- attach 后的新事件继续通过 notification 推送。

不持久化完整 tool / delta timeline，因此中途 join running session 时，历史 tool 卡片可能不完整。v1 接受这个取舍。

## CurrentRunView

为了让 Handoff 不至于完全断裂，daemon 可维护轻量内存态：

```go
type CurrentRunView struct {
    RunID           string
    Status          SessionStatus
    Phase           string
    AssistantBuffer string
    ReasoningBuffer string
    WaitingType     string
    OwnerConnID     string
}
```

不入库。daemon 重启后丢失。

新 client join active session 时：

1. 渲染 snapshot messages。
2. 如果存在 current run，补充当前 assistant / reasoning buffer 和 running 状态。
3. attach 后接收后续 streaming / tool / run 事件。

## TUI Welcome 交互

TUI 自己调用 `session.list` 并基于当前真实 pwd 做 UI 决策。

default behavior：

```text
current pwd sessions = cwd 与 TUI 当前 pwd 匹配的 sessions
active sessions = active == true 的 sessions
```

### 无当前 pwd session，无 active session

```text
New session
```

### 有当前 pwd session

保持现有体验，默认 Resume：

```text
Resume session
New session
```

### 有 active session

增加 Join active：

```text
Resume session
New session
Join active session
```

Join 只展示 active sessions。其他 cwd 的 inactive session 不在 Welcome 里出现。

## Session cwd 语义

session cwd 是 Agent 默认工作目录，影响：

- system prompt 中的 cwd 描述；
- project instructions 查找；
- exec 默认 cwd；
- file/search 工具相对路径解析；
- guard workspace 判断；
- attachment 关联上下文。

工具不应再默认使用 daemon 进程 `os.Getwd()`。如果工具参数显式传 cwd，则使用显式 cwd；否则使用 attached session cwd。

session cwd 不因单次 exec 的 cwd 参数自动改变。需要改变默认 cwd 时，应通过显式 `session.update` 或未来 slash command。

## Attachments

附件目录从全局改为 per-session：

```text
~/.suna/attachments/<session_id>/
```

现有 protocol 可保持不变：

- `attachment.status`
- `attachment.clear`

但 daemon service 语义改为作用于当前 attached session：

- `attachment.status` 返回当前 session 附件目录状态。
- `attachment.clear` 清理当前 session 附件目录。
- 未 attach session 时返回 `session_required`。

`session.delete` 同时删除对应附件目录。

## Config

config 是 daemon-level 全局配置，不属于 session。

UI 应明确展示：

```text
全局 daemon 设置，影响之后的新运行
```

实现规则：

- `config.set` 串行更新全局 config。
- 更新成功后广播 `config.state`。
- run 开始时获取 runtime snapshot。
- 已经 running 的 run 继续使用旧 snapshot。
- 后续新 run 使用新 config。

## Handoff v1 行为

- 多 client 可以 attach 同一 active session。
- event 广播给该 session 的所有 attached clients。
- session idle 时，任意 attached client 可以发送消息。
- session running / waiting / compacting 时，同一 session 的新 sendMessage 返回 `session_busy`。
- askuser / guard confirm 只允许 run owner 回复。
- 非 owner client 可展示只读 waiting 状态。

Handoff 解决 UI 接力，不解决多人同时编辑。

## 生命周期清理

不做 soft delete。删除即 hard delete。

规则：

- 空 session：detach 后可以立即删除。
- 非空 session：30 天未活跃自动删除。
- attached / running / waiting / compacting session 不删除。

30 天清理按 `updated_at` 判断。

`updated_at` 更新时机：

- 用户消息写入；
- assistant run 完成；
- compact 完成；
- session.update 修改 title / cwd。

`last_attached_at` 不参与 30 天清理，避免只是打开查看就延长生命周期。

## 包拆分与抽离建议

当前 `internal/agent.Agent` 同时持有全局 runtime resources 和单 session 状态。多 session 落地时建议按职责拆分，不做过度抽象。

### internal/daemon

保留 daemon 入口和 protocol service，新增：

- `SessionManager`：管理 session metadata、runtime status、attached clients、CurrentRunView。
- `ClientManager`：维护 `connID -> sessionID` attach 关系。
- service method 只做 protocol decode、session 查找和事件路由，不直接持有单一 agent。

### internal/agent

拆成全局 runtime 和 per-session agent：

- `Runtime` / `RuntimeResources`：config、router、skills、MCP、tool registry、store、memory worker。
- `SessionAgent`：working memory、session_state、tool_summary、guard、media store、cwd、runMu、cancelFn。

运行逻辑尽量从当前 `Agent.Run` 平移到 `SessionAgent.Run`，避免一次性重写 runner/model/tool 体系。

### internal/memory

新增 session store：

- `SessionStore`：`sessions` 表的 create/list/update/delete。
- `SessionStateStore`：`session_state` 的 load/save。

现有只记录 usage 的 `SessionStore` 需要改名或合并，避免和真正 session metadata 混淆。

旧 `ConversationStore` 仅保留为 migration helper；迁移完成后运行时不再读取 `conversation_state`。

### internal/media

`media.Store` 从全局 root 改为按 session 创建：

```text
media.NewStore(sessionAttachmentRoot(sessionID))
```

attachment protocol 不改，由 daemon 根据当前 attached session 路由。

### internal/tools

工具执行需要拿到 session context：

```go
type ExecutionContext struct {
    SessionID string
    CWD       string
}
```

exec/file/search 等内置工具默认 cwd 使用 session cwd，不再依赖 daemon 进程 `os.Getwd()`。

### internal/protocol

新增 session request/response 类型和 method 常量：

- `session.list`
- `session.create`
- `session.attach`
- `session.update`
- `session.delete`

`agent.*`、`session.compact`、`attachment.*` 等方法都要求当前 conn 已 attach session。

### internal/tui

保持现有 chat 渲染结构，主要改 Welcome 和 attach snapshot：

- Welcome 基于 `session.list` 计算 Resume / New / Join active。
- attach snapshot 替代旧 restore notification。
- attachment clear/status 仍用原 protocol，但展示为当前 session 附件。

## 实现阶段

### Phase 1: Session data and protocol

- 新增 `sessions` / `session_state`。
- 迁移旧 `conversation_state`。
- 实现 `session.list/create/attach/update/delete`。
- attach 返回 snapshot。
- connection 必须 attach session 后才能调用 agent/session/attachment 操作。

### Phase 2: Per-session agent state

- 拆分全局 runtime resources 和 per-session state。
- working memory、session_state、tool_summary、guard、media store 按 session 隔离。
- `exec` / file / search 等工具默认 cwd 改为 session cwd。
- 附件目录按 session 隔离。

### Phase 3: TUI Welcome and Handoff

- Welcome 支持 Resume / New / Join active。
- TUI attach snapshot 渲染替代 restore notification。
- 实现 active session 列表和 Handoff 只读 running 状态。
- 实现 session event broadcast。

### Phase 4: Runtime safety and cleanup

- run start 使用 runtime snapshot。
- config.set 串行更新并广播。
- 同一 session 内一次只允许一个用户 run；run 内部的工具调用、subtask 或模型恢复流程不受这个限制。
- 30 天未活跃 hard delete。
- 空 session detach 后清理。

## 待未来评估

- pin，防止自动清理重要 session。
- WebSocket transport，用于 GUI / Web / IDE UI。
- observer/controller/takeover 权限模型。
- 完整 session event log。
- 真正 cowork / 多人协作语义。
