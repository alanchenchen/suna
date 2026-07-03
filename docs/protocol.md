# Protocol

Suna 的 TUI、第三方 runtime 客户端和 daemon 通过 `internal/protocol` 定义统一协议。transport 只负责连接、framing、握手策略和生命周期策略，不改变业务语义。

当前已使用的承载方式：

- local transport：Unix socket / Named Pipe，供官方 TUI 和本地 CLI 管理命令使用。
- stdio runtime：`suna runtime --transport stdio`，供第三方 UI、桌面端、IDE 插件或本地 Web 服务使用。

local / stdio 当前都使用 JSON-RPC 2.0 风格消息；local / stdio 的 framing 是 NDJSON。JSON-RPC 是传输承载细节，协议语义以 `internal/protocol` 的 method、notification、params 和 result 为准。

## 设计原则

- Suna 只有一套 protocol；TUI 也是这套 protocol 的官方客户端。
- method request 表示客户端主动请求，必须返回明确 result 或结构化 error。
- notification 表示 daemon 主动推送的异步事件或状态变化。
- 客户端不应把 method response 当成 notification，也不应复用 method 名作为 notification 名。
- TUI 不直接访问 agent、runner、tools、memory、guard、skill 或 MCP 业务包。
- daemon 对外通知必须显式建模，不让客户端解析自由文本判断状态。
- 文本流、运行生命周期和统计信息分开：
  - `agent.delta`：assistant / reasoning 内容增量。
  - `agent.run`：run 生命周期、retry、失败和恢复能力。
  - `agent.usage`：token、context 和耗时统计。
- 错误面向协议使用结构化对象；UI 可以本地化展示，但不应根据错误字符串猜测业务语义。

## Transport 策略

transport 可以决定：

- framing：local / stdio 是 NDJSON；未来 WebSocket 可使用 message frame。
- hello policy：stdio runtime 强制 `runtime.hello`；local/TUI 当前不强制。
- lifecycle retention：local 是 `idle_exit`，stdio 是 `client_bound`，未来 server transport 可用 `persistent`。
- auth：stdio/local 当前不需要；未来网络 transport 应增加鉴权和 Origin 检查。

transport 不应该决定：

- method 名称。
- params / result schema。
- notification schema。
- agent、session、config、memory、skill、MCP 的业务语义。

## JSON-RPC 消息模型

Request：

```json
{"jsonrpc":"2.0","id":1,"method":"config.get","params":{}}
```

Response：

```json
{"jsonrpc":"2.0","id":1,"result":{"models":[],"active_model":""}}
```

Notification：

```json
{"jsonrpc":"2.0","method":"agent.delta","params":{"kind":"assistant","content":"你好"}}
```

规则：

- Suna runtime v0 只支持带整数 `id` 的客户端 request；暂不支持客户端 notification 或 string id。
- daemon response 会回传相同整数 `id`。
- 没有 `id` 且有 `method` 的 daemon 消息是 notification。
- stdio 中 stdout 只能输出 JSON-RPC response / notification；诊断信息写 stderr。

## Runtime handshake：`runtime.hello`

stdio runtime 的第一条 request 必须是 `runtime.hello`。`transport` 由承载层写入 result，客户端不需要、也不能通过 params 声明 transport。

Request：

```json
{
  "protocol_version":"0.1",
  "client":{
    "name":"example-ui",
    "version":"0.1.0",
    "type":"node"
  }
}
```

Result：

```json
{
  "protocol_version":"0.1",
  "runtime_version":"0.5.0",
  "transport":"stdio",
  "capabilities":{
    "agent":true,
    "streaming":true,
    "tools":true,
    "guard":true,
    "ask_user":true,
    "session":true,
    "config":true,
    "memory":true,
    "skills":true,
    "mcp":true
  },
  "content_sources":{
    "text":true,
    "image_path":true,
    "image_url":true
  }
}
```

未握手调用其它 method 时返回：

```json
{
  "code":-32010,
  "message":"runtime.hello is required before other methods",
  "data":{"kind":"handshake_required"}
}
```

## 核心请求

| Method | 用途 | Result |
|---|---|---|
| `runtime.hello` | runtime 握手和能力发现。 | `RuntimeHelloResult` |
| `agent.sendMessage` | 发送用户消息，内容通过 `parts` 承载文本和图片引用。 | `{status:"processing"}` |
| `agent.resumeRun` | 当前 run 失败后，在不新增 user message 的情况下恢复未完成 turn。 | `{status:"processing"}` |
| `agent.cancel` | 取消当前 run。 | `{status:"cancelled"}` |
| `agent.askReply` | 回复 `askuser` 交互。 | `{status:"ok"}` |
| `agent.guardReply` | 回复 Guard 确认。 | `{status:"ok"}` |
| `session.new` | 新建会话。 | `{status:"ok"}` |
| `session.restore` | 恢复最近会话展示状态。 | `{messages:n}`，并通过 restore notifications 下发可见消息。 |
| `session.compact` | 手动压缩当前会话上下文。 | `{status:"ok"}`，结果通过 `session.compact_result` 下发。 |
| `session.usage` | 查询用量摘要。 | `UsageResult` |
| `config.get` | 读取配置。 | `ConfigParams` |
| `config.set` | 更新配置。 | `ConfigParams` |
| `memory.list` | 查询 user profile memory。 | `MemoryListResult` |
| `memory.delete` | 删除 memory。 | `MemoryDeleteResult` |
| `memory.clear` | 清空 memory。 | `MemoryClearResult` |
| `skill.list` | 查询 Skill 状态。 | `SkillListResult` |
| `skill.set` | 启用或禁用 Skill。 | `SkillSetResult` |
| `mcp.list` | 查询 MCP server 状态。 | `MCPListResult` |
| `mcp.toggle` | 启用或禁用 MCP server。 | `MCPSetResult` |
| `mcp.reload` | 重载 MCP server。 | `MCPReloadResult` |
| `attachment.status` / `attachment.clear` | TUI 附件缓存管理。第三方 runtime v0 不主推。 | attachment 状态结果 |
| `daemon.status` / `daemon.stop` | local daemon 管理和诊断。第三方 runtime v0 不主推。 | daemon 状态 / 停止状态 |

## 核心通知

| Notification | 用途 |
|---|---|
| `agent.delta` | assistant / reasoning 文本增量。 |
| `agent.run` | run 生命周期、retry、失败、取消和恢复能力。 |
| `agent.usage` | token、context、耗时和速度统计。 |
| `agent.tool_start` | 工具开始执行。 |
| `agent.tool_guard` | 工具执行前 Guard 决策状态。 |
| `agent.tool_end` | 工具执行结束。 |
| `agent.ask_user` | agent 请求用户输入。 |
| `agent.guard_confirm` | 高风险工具操作请求用户确认。 |
| `session.restore_message` | 恢复会话时下发可见 user/assistant 消息。 |
| `session.restore_status` | 恢复会话结束状态、compact 标记和上一轮有界工具摘要。 |
| `session.compact_result` | compact running / done / error / result 状态。 |
| `config.state` | 配置变更后的主动状态通知。 |
| `memory.state` | memory 变更后的主动状态通知。 |
| `skill.load` | Skill load 生命周期通知。 |
| `skill.review` | Skill review 生命周期通知。 |
| `daemon.full_status` | local/TUI 使用的 daemon 聚合快照通知。第三方 runtime v0 不主推。 |

## 消息输入：`agent.sendMessage`

纯文本：

```json
{
  "parts":[
    {"type":"text","text":"hello"}
  ]
}
```

图片路径：

```json
{
  "parts":[
    {"type":"text","text":"分析这张图片"},
    {
      "type":"image",
      "source":{
        "kind":"path",
        "path":"/absolute/path/image.png",
        "mime_type":"image/png"
      }
    }
  ]
}
```

图片 URL：

```json
{
  "parts":[
    {
      "type":"image",
      "source":{
        "kind":"url",
        "url":"https://example.com/image.png",
        "mime_type":"image/png"
      }
    }
  ]
}
```

`attachment` kind 主要服务官方 TUI 的附件缓存；第三方 UI 可以自行管理上传和缓存，向 runtime 传 path 或 url。

## Agent 内容流：`agent.delta`

`agent.delta` 只表示模型输出的一段内容。它不表示完成、失败、恢复能力或上下文统计。

```json
{"kind":"assistant","content":"你好"}
{"kind":"reasoning","content":"I need to inspect the files..."}
```

字段：

| 字段 | 说明 |
|---|---|
| `run_id` | 可选，预留给未来持久 Run/任务队列。 |
| `kind` | `assistant` 或 `reasoning`。 |
| `content` | 本次文本增量。 |

传输层可以合并连续 `agent.delta`，但不能丢弃内容。遇到 tool、usage、run 状态、ask/guard 等非文本事件前，应先 flush pending delta。

## Agent 生命周期：`agent.run`

`agent.run` 表示一次 agent run 的生命周期和运行状态。

```json
{"state":"running","phase":"model"}
{"state":"retrying","phase":"model","attempt":2,"max_attempts":4,"delay_ms":8000}
{"state":"done"}
```

失败示例：

```json
{
  "state":"failed",
  "phase":"model",
  "resume_available":true,
  "error":{
    "kind":"http",
    "message":"Service Unavailable",
    "status_code":503,
    "type":"overloaded_error"
  }
}
```

字段：

| 字段 | 说明 |
|---|---|
| `run_id` | 可选，预留给未来持久 Run/事件回放。 |
| `state` | `running`、`retrying`、`done`、`failed`、`cancelled`。 |
| `phase` | 可选，当前阶段：`model`、`tool`、`compact`、`guard`、`ask`、`skill`。 |
| `message` | 可选的人类可读补充信息。 |
| `attempt` / `max_attempts` / `delay_ms` | retry 状态使用。 |
| `error` | 失败时的结构化模型错误。 |
| `resume_available` | run 失败后是否可调用 `agent.resumeRun`。 |

`retrying` 不是失败终态。客户端应显示等待状态，但不应插入最终错误消息。只有 `state=failed` 或 `state=cancelled` 才表示当前 run 结束。

## Usage：`agent.usage`

`agent.usage` 承载模型使用量和上下文统计：

| 字段 | 说明 |
|---|---|
| `input_tokens` / `output_tokens` / `cached_tokens` | 模型 usage。 |
| `context_tokens` | provider 返回或推导的上下文 token。 |
| `estimated_context_tokens` | Suna 请求前估算的上下文 token。 |
| `context_window` | 当前模型配置的 context window。 |
| `duration_ms` / `tokens_per_sec` | 本次请求耗时和输出速度。 |

客户端不应从 `agent.delta` 或 `agent.run` 推导 token/context。

## 结构化错误

JSON-RPC error 使用标准 `code` / `message`，并尽量带结构化 `data`：

```json
{
  "code":-32602,
  "message":"content is required",
  "data":{"kind":"invalid_request"}
}
```

常见 `data.kind`：

| kind | 含义 |
|---|---|
| `handshake_required` | stdio runtime 未先调用 `runtime.hello`。 |
| `invalid_request` | 请求或参数无效。 |
| `unsupported_method` | method 不存在。 |
| `unsupported_capability` | 当前 runtime 或协议版本不支持。 |
| `internal_error` | daemon 内部错误。 |

## ModelError

模型错误使用结构化对象：

```json
{
  "kind":"http",
  "message":"rate limit exceeded",
  "status_code":429,
  "code":"rate_limit_exceeded",
  "type":"rate_limit_error"
}
```

字段：

| 字段 | 说明 |
|---|---|
| `kind` | `http`、`network`、`cancelled`、`internal`、`unknown`。 |
| `message` | 必须尽量保留上游可读错误信息。 |
| `status_code` | HTTP 错误状态码。 |
| `code` / `type` | provider 提供的错误 code/type。 |
| `provider` / `model` | 可选诊断信息，UI 默认不必展示。 |

`ModelError` 只描述错误事实，不承载 retry、attempt、delay 或 resume 语义；这些属于 `agent.run`。

## Model request recovery

Runner 会在主循环的模型请求边界做内置 recovery：

- 自动重试次数：3 次；因此总尝试次数为 4。
- retry 间隔：8 秒。
- 仅在尚未产生 assistant/reasoning/tool call 输出前 retry。
- 只根据结构化状态判断：HTTP `408`、`429`、`500`、`502`、`503`、`504`，以及 network / timeout。
- 不根据错误字符串判断是否 retry。

retry 期间 daemon 发送：

```json
{"state":"retrying","phase":"model","attempt":2,"max_attempts":4,"delay_ms":8000}
```

如果 recovery 耗尽或遇到不可 retry 错误，daemon 发送 `agent.run state=failed`；若 `resume_available=true`，客户端可以让用户通过 `agent.resumeRun` 手动继续。

日志边界：Router 仍只记录单次物理 LLM request；Runner 单独记录 `llm/recovery`，用于表达 retrying、recovered 或 exhausted 等恢复语义。

## Tool / Guard / Ask 事件

工具和交互事件保持独立：

| Notification | 用途 |
|---|---|
| `agent.tool_start` | 工具开始执行。 |
| `agent.tool_guard` | 工具执行前 Guard 决策状态。 |
| `agent.tool_end` | 工具执行结束，`result` 是 UI 展示内容，不是模型内部完整 tool result。 |
| `agent.ask_user` | agent 请求用户输入。 |
| `agent.guard_confirm` | 高风险工具操作请求用户确认。 |

这些事件是 agent run 的组成部分，但不应混入 `agent.delta`。

## Session restore

`session.restore` 使用：

- method response：返回恢复消息数量。
- `session.restore_message`：逐条恢复可见 user/assistant 消息。
- `session.restore_status`：恢复结束状态、compact 标记和上一轮有界工具摘要。

工具摘要是 TUI 展示状态，不作为原始工具历史重新注入模型。

## Compact result

`session.compact_result` 目前继续承载手动 compact 结果和 auto compact 的 running/error 状态。未来如果引入持久 Run/任务队列，可以把 compact lifecycle 逐步迁移到 `agent.run phase=compact`，但当前不强制合并，避免破坏现有 TUI compact 语义。
