# Suna Protocol 语义参考

本文面向 Suna 维护者、transport 实现者和高级集成者，说明 Suna protocol 的稳定语义、分层边界和兼容性约束。

如果你是第三方 UI / 桌面端 / IDE 插件 / 本地 Web 服务开发者，想快速接入 `suna runtime --transport stdio`，请优先阅读：

```txt
docs/runtime-stdio.md
```

`runtime-stdio.md` 是 public runtime v0.2 的开发者接入手册，包含启动方式、JSON-RPC/NDJSON 细节、Node.js 最小客户端、method/notification 参数表和示例。本文不重复完整参数示例，只描述协议设计原则和实现约束。

---

## 1. 核心原则

Suna 只有一套 protocol。官方 TUI、第三方 stdio runtime 客户端和未来 transport 都必须遵守同一套业务语义。

核心规则：

- **method request**：客户端主动请求，必须返回明确 `result` 或结构化 `error`。
- **notification**：daemon 主动推送的异步事件或状态变化，不对应某个 request 的直接返回。
- **response 不能伪装成 notification**：客户端不应把 method response 当成 daemon event；daemon 也不应复用 method 名作为 notification 名。
- **TUI 也是 protocol 客户端**：TUI 不直接访问 agent、runner、tools、guard、memory、skill 或 MCP 业务包，交互必须走 protocol。
- **UI 不解析自由文本判断状态**：状态、错误、retry、resume、usage 等必须通过结构化字段表达。
- **transport 不改变业务语义**：同一个 method / notification 在 local、stdio 或未来 WebSocket 上含义一致。

---

## 2. Transport 边界

当前已使用的 transport：

| Transport | 用途 | Framing | Hello policy | Lifecycle |
|---|---|---|---|---|
| local | 官方 TUI 和本地 CLI 管理命令 | NDJSON over Unix socket / Named Pipe | 不强制 | `idle_exit` |
| stdio | 第三方 UI / 客户端 headless runtime | NDJSON over stdin/stdout | 强制 `runtime.hello` | `client_bound` |

transport 可以决定：

- 连接方式和 framing。
- 是否强制 `runtime.hello`。
- lifecycle retention，例如 `client_bound`、`idle_exit`、`persistent`。
- 未来网络 transport 的鉴权、Origin 检查和连接策略。

transport 不可以决定：

- method 名称。
- params / result schema。
- notification 名称和 schema。
- agent、session、config、memory、skill、MCP 的业务语义。
- 模型 retry、工具 Guard、askuser、session attach/snapshot 等运行语义。

---

## 3. JSON-RPC 承载约束

local / stdio 当前都使用 JSON-RPC 风格消息，framing 是 NDJSON。JSON-RPC 是承载细节；业务语义以 `internal/protocol` 的 method、notification、params 和 result 为准。

public stdio runtime v0.2 的限制：

- 客户端 request 必须带整数 `id`。
- 暂不支持 string id。
- 暂不支持客户端 notification。
- daemon response 会回传相同整数 `id`。
- 没有 `id` 且有 `method` 的 daemon 消息是 notification。
- stdout 只输出 JSON-RPC response / notification；stderr 只输出人类诊断日志。

完整 request / response / notification 示例见 `docs/runtime-stdio.md`。

---

## 4. Method 总览

public runtime v0.2 主推的 method：

| Method | 语义 |
|---|---|
| `runtime.hello` | stdio runtime 握手和能力发现。 |
| `agent.sendMessage` | 发送用户消息；response 只表示已接收，模型输出通过 notification 下发。 |
| `agent.resumeRun` | 当前 run 失败且可恢复时，继续未完成 turn。 |
| `agent.cancel` | 取消当前 run。 |
| `agent.askReply` | 回复 `agent.ask_user`。 |
| `agent.guardReply` | 回复 `agent.guard_confirm`。 |
| `session.list` | 列出 sessions。 |
| `session.create` | 创建 session；客户端必须传 cwd，返回 snapshot。 |
| `session.attach` | attach 到已有 session；Resume 和 Join 都使用该方法，返回 snapshot。 |
| `session.detach` | 当前连接离开当前 session。 |
| `session.update` | 更新当前 attached session 的 title 或模型选择；只更新 title 时可在运行中执行，更新 `model_ref` 时必须 idle。 |
| `session.delete` | 删除非当前、非 active、无人 attached 的 idle session。 |
| `session.compact` | 手动压缩当前会话上下文。 |
| `session.usage` | 查询用量摘要。 |
| `config.get` | 读取配置。 |
| `config.set` | 更新配置。 |
| `memory.list` / `memory.delete` / `memory.clear` | 查询、删除或清空 memory。 |
| `skill.list` / `skill.set` | 查询、启用或禁用 Skill。 |
| `mcp.list` / `mcp.toggle` / `mcp.reload` | 查询、启用/禁用或重载 MCP server。 |

不主推给第三方 runtime v0.2 依赖的 method：

| Method | 说明 |
|---|---|
| `daemon.status` | 可用于 smoke test 或诊断面板，但不是聊天主流程必需能力。 |
| `daemon.stop` | local daemon 管理语义；stdio runtime 通常通过关闭 stdin 退出。 |
| `attachment.status` / `attachment.clear` | 官方 TUI 附件缓存管理；第三方 UI 应自行管理上传和缓存，并向 `agent.sendMessage` 传 image path/url。 |

完整参数表和示例见 `docs/runtime-stdio.md`。

---

## 5. Notification 总览

public runtime v0.2 主推的 notification：

| Notification | 语义 |
|---|---|
| `agent.delta` | assistant / reasoning 文本增量。 |
| `agent.run` | run 生命周期、retry、失败、取消和恢复能力。 |
| `agent.usage` | token、context、耗时和速度统计。 |
| `agent.tool_start` | 工具开始执行。 |
| `agent.tool_guard` | 工具执行前 Guard 决策状态。 |
| `agent.tool_end` | 工具执行结束；`result` 是 UI 展示内容，可能被截断。 |
| `agent.ask_user` | agent 请求用户输入；带 `can_reply`。 |
| `agent.guard_confirm` | 高风险工具操作请求用户确认；带 `can_reply`。 |
| `agent.interaction_resolved` | ask/guard 已处理，其他 UI 应关闭残留交互。 |
| `session.user_message` | 同 session 其他客户端新增 user turn。 |
| `session.updated` | session metadata/status/client_count 更新。 |
| `session.compact_result` | compact running / done / error / result 状态。 |
| `config.state` | 配置变更后的主动状态通知。 |
| `memory.state` | memory 变更后的主动状态通知。 |
| `skill.load` | Skill load 生命周期通知。 |
| `skill.review` | Skill review 生命周期通知。 |

偏官方 TUI / local 管理用途的 notification：

| Notification | 说明 |
|---|---|
| `daemon.full_status` | daemon 聚合快照，主要供 TUI 刷新状态面板。第三方 UI 可用于诊断，但不应依赖它完成聊天主流程。 |

完整参数表和示例见 `docs/runtime-stdio.md`。

---

## 6. Agent 事件分层

Agent 运行事件必须按语义拆分，避免 UI 从文本流里推导状态。

### `agent.delta`

只表示模型输出的一段内容：

- `kind=assistant`：assistant 可见回复。
- `kind=reasoning`：reasoning 增量。

`agent.delta` 不表示 run 是否完成、失败、retry、usage 或 resume 能力。

### `agent.run`

表示 run 生命周期：

- `running`：run 正在执行。
- `retrying`：模型请求临时失败，Runner 将自动重试。
- `done`：run 正常结束。
- `failed`：run 失败。
- `cancelled`：run 被取消。

`retrying` 不是终态。客户端可以展示等待/重试状态，但不应插入最终错误消息。只有 `done`、`failed`、`cancelled` 表示当前 run 结束。

`resume_available=true` 只在失败后表示客户端可以提供“继续/恢复”按钮，并调用 `agent.resumeRun`。

### `agent.usage`

只承载模型使用量和上下文统计。客户端不应从 `agent.delta` 或 `agent.run` 推导 token/context。

### 工具、AskUser、Guard

工具和交互事件保持独立：

- `agent.tool_start` / `agent.tool_guard` / `agent.tool_end` 用于工具展示和 Guard 状态。
- `agent.ask_user` 表示 Agent 需要用户输入；客户端必须调用 `agent.askReply` 回复。
- `agent.guard_confirm` 表示高风险工具操作需要用户确认；客户端必须调用 `agent.guardReply` 回复。

---

## 7. 错误模型

Suna 有三类主要错误对象：

1. JSON-RPC method response error。
2. 模型请求已经发起后的 `ModelError`。
3. 模型请求开始前的结构化 `RunError`。

### JSON-RPC error

method 参数错误、未握手、未知 method、内部错误等通过 JSON-RPC `error` 返回。

结构：

```json
{
  "code":-32602,
  "message":"content is required",
  "data":{"kind":"invalid_request"}
}
```

`data.kind` 是稳定分类，UI/SDK 应根据它做分支，不要解析 `message`。

常见 kind：

| kind | 含义 |
|---|---|
| `parse_error` | 输入行不是合法 JSON。 |
| `invalid_request` | 请求或参数无效。 |
| `unsupported_method` | method 不存在。 |
| `unsupported_capability` | 当前 runtime 或协议版本不支持。 |
| `handshake_required` | stdio runtime 未先调用 `runtime.hello`。 |
| `internal_error` | daemon 内部错误。 |

### ModelError

模型请求失败不作为 `agent.sendMessage` response error 返回。`agent.sendMessage` 的 response 只表示“消息已接收”；后续模型失败通过：

```txt
agent.run state=failed error=ModelError
```

下发。

`ModelError` 描述错误事实，不承载 retry、attempt、delay 或 resume 语义；这些属于 `agent.run`。

字段语义：

| 字段 | 说明 |
|---|---|
| `kind` | `http`、`network`、`cancelled`、`internal`、`unknown`。 |
| `message` | 上游可读错误信息。 |
| `status_code` | HTTP 错误状态码。 |
| `code` / `type` | provider 提供的错误 code/type。 |
| `provider` / `model` | 可选诊断信息。 |

### RunError

`RunError` 表示模型请求开始前无法满足的运行前置条件，通过失败的 `agent.run.run_error` 下发；它不替代上游请求失败的 `ModelError`。

| kind | 含义 | 客户端恢复动作 |
|---|---|---|
| `no_model_configured` | 尚未配置可用于新会话的模型。 | 引导用户配置模型。 |
| `session_model_unavailable` | 当前 session 选择的 `model_ref` 已不在模型目录中。 | 保留该 session，不做默认模型 fallback；引导用户更新 `session.update.model_ref`。 |

客户端必须按 `kind` 分支，并可使用 `model_ref` 展示失效的会话模型；不能解析 daemon 的自由文本错误。

---

## 8. Model request recovery

Runner 在模型请求边界做自动 recovery：

- 自动重试次数：3 次，因此总尝试次数为 4。
- retry 间隔：8 秒。
- 仅在尚未产生 assistant/reasoning/tool call 可见输出前自动 retry。
- 只根据结构化状态判断：HTTP `408`、`429`、`500`、`502`、`503`、`504`，以及 network / timeout。
- 不根据错误字符串判断是否 retry。

retry 期间 daemon 发送：

```txt
agent.run state=retrying phase=model attempt=N max_attempts=4 delay_ms=8000
```

如果 recovery 耗尽或遇到不可 retry 错误，daemon 发送：

```txt
agent.run state=failed
```

如果同时带 `resume_available=true`，客户端可以让用户通过 `agent.resumeRun` 手动继续。

日志边界：Router 只表示单次物理模型请求；Runner 单独记录 recovery 语义，例如 retrying、recovered 或 exhausted。

---

## 9. Session attach 和 compact 语义

### `session.attach`

`session.attach` 是 Resume 和 Join Active 的共同原语，method response 直接返回 snapshot：

- `session`：session metadata，包括 cwd、`model_ref`、status、client_count 和 message_count。
- `messages`：最近可见 user/assistant 文本消息。
- `compacted`：较早上下文是否已压缩为 Session State。
- `tool_summary`：上一轮有界工具摘要，仅供 UI 展示。
- `current_run`：Join running session 时的轻量当前 run 视图。

`session.attach.require_active=true` 只用于 Join Active 的陈旧 UI 防护；Resume 应传 false 或省略。

snapshot 不保证完整 tool timeline / event replay。

### `session.detach`

`session.detach` 表示当前连接离开当前 session，但保持 transport 连接。官方 TUI 从 Chat 回 Welcome 时会调用它。

### `session.compact`

`session.compact_result` 当前继续承载手动 compact 结果和 auto compact 的 running/error 状态。

未来如果引入持久 Run/任务队列，可以把 compact lifecycle 逐步迁移到 `agent.run phase=compact`，但当前不强制合并，避免破坏现有 TUI compact 语义。

---

## 10. Handoff 语义

Handoff 在 daemon 中只表现为 multi-attach、run owner 和权限字段，不引入 host/guest：

- 同一 session 可被多个 client attach。
- 同一 session 同一时间只能一个 active run。
- 当前 run owner 收到 `can_control=true`。
- ask/guard owner 在线时只有 owner `can_reply=true`。
- owner 断开后，daemon 可把 pending ask/guard 重新发给仍 attached 的 client，并让实际回复者成为新的 run owner。

TUI 的“本会话 / 已加入 / 观察中”是 UI 根据 attach 方式、client_count、status 和权限字段派生的产品表达。

---

## 11. Public / internal 边界

public runtime v0.2 主推：

- runtime handshake。
- agent 消息和事件。
- session list/create/attach/detach/update/delete/compact/usage。
- config get/set。
- memory list/delete/clear。
- skill list/set。
- MCP list/toggle/reload。

不主推或偏内部：

- `daemon.stop`：local daemon 管理语义。
- `daemon.full_status`：官方 TUI 聚合状态快照。
- `attachment.*`：官方 TUI 附件缓存管理。
- local transport endpoint、PID 文件、Named Pipe / Unix socket 细节。

第三方 UI 不应该直接读取 `.suna` 内部状态，也不应该自己实现 agent loop。推荐通过 `suna runtime --transport stdio` 接入。

---

## 12. 兼容性规则

修改 protocol 时必须遵守：

- 新增字段应为 optional，不能破坏旧客户端。
- 不改变已有字段语义。
- 不复用 method 名作为 notification 名。
- 不把 method response 伪装成 notification。
- 不让 transport 改变业务语义。
- 结构化错误新增 kind 时，应保持旧 kind 的含义稳定。
- `agent.delta`、`agent.run`、`agent.usage` 的职责边界不能混淆。
- public runtime v0.2 暂不承诺 string id 或客户端 notification；如果未来支持，应在 JSON-RPC 层保持 id 原样 round-trip，避免污染 daemon 业务层。
- protocol 0.2 不兼容旧 `session.new` / `session.restore` 主流程；旧客户端需要迁移到 `session.create` / `session.attach`。

---

## 13. 文档分工

| 文档 | 面向对象 | 职责 |
|---|---|---|
| `docs/runtime-stdio.md` | 第三方 UI 开发者 | 如何启动 runtime、写 JSON-RPC client、调用 method、处理 notification 和错误。 |
| `docs/protocol.md` | Suna 维护者 / transport 实现者 / 高级集成者 | protocol 语义边界、分层约束、错误模型、recovery 和兼容性规则。 |
| `docs/architecture.md` | 架构读者 | CLI、TUI、daemon、agent、transport、config、memory、skill、MCP 的整体分层。 |
