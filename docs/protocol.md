# Suna Protocol 语义参考

本文面向 Suna 维护者、transport 实现者和高级集成者，说明 Suna protocol 的稳定语义、分层边界和兼容性约束。

如果你是第三方 UI / 桌面端 / IDE 插件 / 本地 Web 服务开发者，想快速接入 Suna TCP daemon，请优先阅读：

```txt
docs/tcp-client.md
```

`tcp-client.md` 是第三方客户端开发者接入手册，包含 daemon 启动、TCP JSON-RPC/NDJSON、握手、session handoff 和最小请求流程。本文不重复完整参数示例，只描述协议设计原则和实现约束。

---

## 1. 核心原则

Suna 只有一套 protocol。官方 TUI、第三方 TCP 客户端和未来 transport 都必须遵守同一套业务语义。

核心规则：

- **method request**：客户端主动请求，必须返回明确 `result` 或结构化 `error`。
- **notification**：daemon 主动推送的异步事件或状态变化，不对应某个 request 的直接返回。
- **response 不能伪装成 notification**：客户端不应把 method response 当成 daemon event；daemon 也不应复用 method 名作为 notification 名。
- **TUI 也是 protocol 客户端**：TUI 不直接访问 agent、runner、tools、guard、memory、skill 或 MCP 业务包，交互必须走 protocol。
- **UI 不解析自由文本判断状态**：状态、错误、retry、resume、usage 等必须通过结构化字段表达。
- **transport 不改变业务语义**：同一个 method / notification 在 local、TCP 或未来 WebSocket 上含义一致。

---

## 2. Transport 边界

当前已使用的 transport：

| Transport | 用途 | Framing | Hello policy | Lifecycle |
|---|---|---|---|---|
| local | 官方 TUI 和本地 CLI 管理命令 | NDJSON over Unix socket / Named Pipe | 不强制 | `idle_exit` |
| tcp | 第三方本地 UI / 客户端 | NDJSON over loopback TCP | 强制 `runtime.hello` | `idle_exit` |

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

local / TCP 当前都使用 JSON-RPC 风格消息，framing 是 NDJSON。JSON-RPC 是承载细节；业务语义以 `internal/protocol` 的 method、notification、params 和 result 为准。

TCP 客户端的限制：

- 客户端 request 必须带整数 `id`。
- 暂不支持 string id。
- 暂不支持客户端 notification。
- daemon response 会回传相同整数 `id`。
- 没有 `id` 且有 `method` 的 daemon 消息是 notification。
- TCP connection 中的 JSON-RPC response / notification 都通过同一条 socket 下发；CLI 的 stdout/stderr 只用于 `suna serve --json` 的启动结果和人类诊断。

完整接入流程见 `docs/tcp-client.md`。

---

## 4. Runtime 握手与功能清单

TCP client 连接后，第一条 request 必须是 `runtime.hello`。请求只需要携带客户端自身信息；daemon 返回 Git tag 来源的 Runtime 版本和公开功能清单。

```json
{"jsonrpc":"2.0","id":1,"method":"runtime.hello","params":{"client":{"name":"my-ui","version":"1.0.0","type":"desktop"}}}
```

功能清单固定分为三组：

- `catalog.methods`：客户端可以主动调用的公开 RPC；
- `catalog.notifications`：Runtime 可能主动发送的公开事件；
- `catalog.features`：不能只从 method / notification 名称推断的细粒度能力。

客户端应按清单渐进启用功能，不应根据 `runtime_version` 推断能力。`runtime_version` 只用于展示和诊断。

### Methods

| Method | 语义 |
|---|---|
| `runtime.hello` | TCP transport 握手和能力发现。 |
| `agent.sendMessage` | 发送用户消息并创建新 run；response 只表示已接收，模型输出通过 notification 下发。 |
| `agent.steer` | 当前 run owner 在运行中排队一条文本消息；消息在下一安全边界进入同一 run。 |
| `agent.steerRemove` | 按精确消息 ID 撤回一条尚未应用的运行中消息。 |
| `agent.resumeRun` | 当前 run 失败且可恢复时，继续未完成 turn。 |
| `agent.cancel` | 取消当前 run。 |
| `agent.askReply` | 回复 `agent.ask_user`。 |
| `agent.guardReply` | 回复 `agent.guard_confirm`。 |
| `session.list` | 获取全局轻量 Session Catalog 快照。 |
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

Catalog 只列第三方客户端公开支持面。`daemon.status`、`daemon.stop`、`attachment.*`、`debug.*` 等 local/官方管理接口即使存在，也不属于公开 Catalog。

daemon lifecycle 使用 `starting / ready / stopping`。`ready` 只表示核心 runtime 可服务，不表示所有 MCP 已 active；非 `ready` 时，除 `runtime.hello / daemon.status / daemon.stop` 外的请求返回 `runtime_unavailable`，并通过 `reason` 与 `retryable` 表达恢复语义。

完整参数表和示例见 `docs/tcp-client.md`。

### Notifications

| Notification | 语义 |
|---|---|
| `agent.delta` | assistant / reasoning 文本增量。 |
| `agent.run` | run 生命周期、retry、失败、取消和恢复能力。 |
| `agent.steering` | 运行中消息的 queued / applied / removed / rejected 状态；客户端按 `id` 幂等合并。 |
| `agent.usage` | token、context、耗时和速度统计。 |
| `agent.tool_start` | 工具开始执行。 |
| `agent.tool_guard` | 工具执行前 Guard 决策状态。 |
| `agent.tool_end` | 工具执行结束；`result` 是 UI 展示内容，可能被截断。 |
| `agent.ask_user` | agent 请求用户输入；带 `can_reply`。 |
| `agent.guard_confirm` | 高风险工具操作请求用户确认；带 `can_reply`。 |
| `agent.interaction_resolved` | ask/guard 已处理，其他 UI 应关闭残留交互。 |
| `session.user_message` | 同 session 新增的正式 user turn；运行中消息只有在 applied 后才通过该通知出现。 |
| `session.updated` | 全局轻量 Session Catalog 增量；session metadata/status/client_count 变化时向所有已连接且完成握手的客户端广播。 |
| `session.compact_result` | compact running / done / error / result 状态。 |
| `config.state` | 配置变更后的主动状态通知。 |
| `memory.state` | memory 变更后的主动状态通知。 |
| `mcp.updated` | 单个 MCP server 的完整状态增量；按 `server.id` 覆盖本地快照。 |
| `skill.load` | Skill load 生命周期通知。 |
| `skill.review` | Skill review 生命周期通知。 |

客户端必须忽略自己不认识的 notification，不能因此关闭连接。Catalog 用于提前初始化功能，但实际接收端仍应保持宽松。

`mcp.list` 与 `mcp.updated` 采用相同的 snapshot + delta 语义。MCP server 的 `state` 只能是 `disabled / starting / active / error`；daemon core ready 不等待 MCP，只有 `active` server 的工具进入模型 Tool Catalog。多个 server 短时间完成时，Agent 会合并刷新目录，并在目录发布完成后再发送 `active` 增量。

`session.list` 与 `session.updated` 共同维护全局轻量 Session Catalog：连接建立后用 `session.list` 获取初始快照，之后用 `session.updated` 合并 metadata、`status` 与 `client_count` 变化。详细 agent 事件仍只发送给已 attach 目标 session 的客户端。

### Features

| Feature | 相关协议 | 客户端行为 |
|---|---|---|
| `agent.steer.text` | `agent.steer`、`agent.steerRemove`、`agent.steering` |允许 current run owner 排队和撤回纯文本消息。缺失时隐藏运行中输入。 |
| `config.model.auth_mode.bearer` | `ConfigModel.auth_mode="bearer"` |在 Anthropic 模型配置中提供 Bearer 选项。 |
| `config.model.auth_mode.both` | `ConfigModel.auth_mode="both"` |提供双认证头选项；只应向已知兼容端点展示。 |
| `session.handoff` | multi-attach、`can_control`、`can_reply` |允许 observer 在 owner 离开后接管交互。 |
| `skill.project` | `skill.list` 的 project scope、精确 `path` |展示项目 Skill，且不提供 toggle。 |

Feature 名称一经公开，其语义保持不变。新增能力增加新名称，不通过全局协议版本或重复的 v1/v2 代际维护普通增量功能。

---

## 5. Agent 事件分层

Agent 运行事件必须按语义拆分，避免 UI 从文本流里推导状态。

### `agent.delta`

只表示模型输出的一段内容：

- `kind=assistant`：assistant 可见回复。
- `kind=reasoning`：reasoning 增量。

`agent.delta` 不表示 run 是否完成、失败、retry、usage 或 resume 能力。

### `agent.run`

表示 run 生命周期：

- `running`：run 正在执行；这是新 run 的首条 Agent 生命周期通知，先于该 run 的 delta/tool/终态事件。
- `retrying`：模型请求临时失败，Runner 将自动重试。
- `cancelling`：daemon 已接受取消请求，run 仍在收尾；`can_control=false`，重复 cancel 幂等且不重复通知。
- `done`：run 正常结束。
- `failed`：run 失败。
- `cancelled`：run 被取消。

`retrying` 和 `cancelling` 不是终态。进入 `cancelling` 后 daemon 不再发布该 run 的 `running`、`retrying` 或 `done`，context 取消及其他竞态终态统一收敛为唯一 `cancelled`。客户端可以在 cancelling 期间保留或编辑下一份本地草稿，但不得发送、排队消息或再次取消。只有 `done`、`failed`、`cancelled` 表示当前 run 结束。

`resume_available=true` 只在失败后表示客户端可以提供“继续/恢复”按钮，并调用 `agent.resumeRun`。

### 运行中消息

Feature `agent.steer.text` 支持 current run owner 在运行期间调用 `agent.steer` 排队文本消息。该能力不打断正在进行的模型请求或工具调用：

- 完整模型响应结束后才可能应用；Thinking 结束不是独立注入点。
- 模型返回 Tool Calls 时，必须等同一批全部 Tool Result 写回后再应用，不能破坏调用配对。
- 模型返回纯文本且队列非空时，先保留该 assistant 响应，再应用消息并继续同一个 run。
- 自动 Compact 期间可以排队；消息在 Compact 提交后进入 Working Memory，不会被折叠进刚生成的 Session State。
- AskUser / Guard 等待、cancelling 和终态不接受新消息；立即停止仍使用 `agent.cancel`。

`agent.steer` 第一版只接受 text parts。`client_msg_id` 在单个 run 内用于幂等：相同 ID 与相同文本返回 canonical 状态，不重复排队；相同 ID 与不同文本返回冲突。daemon 通过 `runtime.hello.limits` 声明消息数和总字节上限。

状态通过 `agent.steering` 通知：

- `queued`：daemon 已接受，但模型尚未看到；此时 owner 可按消息 `id` 撤回。
- `applied`：消息已写入 Working Memory，即将参与下一次模型请求；随后 `session.user_message` 将它作为正式 user turn 广播。
- `removed`：owner 已成功撤回；客户端可将内容恢复为草稿。
- `rejected`：run 失败或取消前未能应用；owner 应将内容恢复为草稿。

Method response 与 notification 共用连接，可能因并发先后到达。客户端必须按 `id` / `client_msg_id` 幂等合并，不能假设 response 一定先于 notification。`session.attach.current_run.pending_steering` 返回当前仍在内存队列中的消息，供重连或第三方客户端恢复；队列不承诺跨 daemon 进程崩溃持久化。

### `agent.usage`

只承载模型使用量和上下文统计。客户端不应从 `agent.delta` 或 `agent.run` 推导 token/context。

`cache_read_tokens` 与 `cache_creation_tokens` 是 `input_tokens` 的可选明细，已包含在 `input_tokens` 中；汇总时不得重复相加。

### 工具、AskUser、Guard

工具和交互事件保持独立：

- `agent.tool_start` / `agent.tool_guard` / `agent.tool_end` 用于工具展示和 Guard 状态。
- `agent.ask_user` 表示 Agent 需要用户输入；客户端必须调用 `agent.askReply` 回复。
- `agent.guard_confirm` 表示高风险工具操作需要用户确认；客户端必须调用 `agent.guardReply` 回复。

---

## 6. 错误模型

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
| `handshake_required` | TCP 客户端未先调用 `runtime.hello`。 |
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

## 7. Model request recovery

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

## 8. Session attach 和 compact 语义

### `session.attach`

`session.attach` 是 Resume 和 Join Active 的共同原语，method response 直接返回 snapshot：

- `session`：session metadata，包括 cwd、`model_ref`、status、client_count 和 message_count。
- `messages`：最近可见 user/assistant 文本消息。
- `compacted`：较早上下文是否已压缩为 Session State。
- `tool_summary`：上一轮有界工具摘要，仅供 UI 展示。
- `current_run`：Join running session 时的轻量当前 run 视图，含稳定 `run_id`、`state` 和实时 `can_control`；`state=cancelling` 时 `can_control=false`。客户端应避免让同一 `run_id` 的迟到快照重新激活已终态运行。

`session.attach.require_active=true` 只用于 Join Active 的陈旧 UI 防护；Resume 应传 false 或省略。

snapshot 不保证完整 tool timeline / event replay。

### `session.detach`

`session.detach` 表示当前连接离开当前 session，但保持 transport 连接。官方 TUI 从 Chat 回 Welcome 时会调用它。

### `session.compact`

`session.compact_result` 当前继续承载手动 compact 结果和 auto compact 的 running/error 状态。

未来如果引入持久 Run/任务队列，可以把 compact lifecycle 逐步迁移到 `agent.run phase=compact`，但当前不强制合并，避免破坏现有 TUI compact 语义。

---

## 9. Handoff 语义

Handoff 在 daemon 中只表现为 multi-attach、run owner 和权限字段，不引入 host/guest：

- 同一 session 可被多个 client attach。
- 同一 session 同一时间只能一个 active run。
- 当前 run owner 收到 `can_control=true`。
- ask/guard owner 在线时只有 owner `can_reply=true`。
- owner 断开后，daemon 可把 pending ask/guard 重新发给仍 attached 的 client，并让实际回复者成为新的 run owner。

TUI 的“本会话 / 已加入 / 观察中”是 UI 根据 attach 方式、client_count、status 和权限字段派生的产品表达。

---

## 10. Public / internal 边界

公开支持面以 `runtime.hello.catalog` 为准。第三方 UI 不应该直接读取 `.suna` 内部状态，也不应该自己实现 agent loop。推荐通过 `suna serve --json` 获取 TCP endpoint 后接入。

以下接口偏官方 TUI / local 管理用途，不进入公开 Catalog：

- `daemon.status` / `daemon.stop`；
- `daemon.full_status`；
- `attachment.status` / `attachment.clear`；
- `debug.*`；
- local transport endpoint、PID 文件、Named Pipe / Unix socket 细节。

---

## 11. 演进规则

修改 protocol 时必须遵守：

- `runtime_version` 只用于展示和诊断，不用于判断功能。
-新增公开 method、notification 或 feature 时同步更新 `internal/protocol` Catalog 与本文对应分组。
-已有 Catalog 名称的语义保持稳定；增量能力增加新名称。
-客户端只调用 `catalog.methods` 中存在的方法，并按 `catalog.features` 渐进启用细粒度 UI。
-客户端必须忽略未知字段和 notification。
-未知或不支持的 method 只让当前请求失败，不能关闭连接。
-不复用 method 名作为 notification 名，不把 method response 伪装成 notification。
-不让 transport 改变业务语义。
- `agent.delta`、`agent.run`、`agent.usage` 的职责边界不能混淆。

---

## 12. 文档分工

| 文档 | 面向对象 | 职责 |
|---|---|---|
| `docs/tcp-client.md` | 第三方 UI 开发者 | 如何确保 daemon 已启动、连接 TCP、写 JSON-RPC client、调用 method、处理 notification 和错误。 |
| `docs/protocol.md` | Suna 维护者 / transport 实现者 / 高级集成者 | protocol 语义边界、分层约束、错误模型、recovery 和兼容性规则。 |
| `docs/architecture.md` | 架构读者 | CLI、TUI、daemon、agent、transport、config、memory、skill、MCP 的整体分层。 |
