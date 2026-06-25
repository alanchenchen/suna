# Protocol

Suna 的 TUI、CLI 命令和 daemon 通过 `internal/protocol` 定义请求、响应和通知。local transport 当前用 JSON-RPC 风格 NDJSON 承载这些结构，但 JSON-RPC 只是传输细节；协议语义以 `internal/protocol` 的 method、notification 和 params 为准。

## 设计原则

- TUI 不直接访问 agent、runner、tools、memory、guard、skill 或 MCP 业务包。
- daemon 对外通知必须显式建模，不让客户端解析自由文本判断状态。
- 文本流、运行生命周期和统计信息分开：
  - `agent.delta`：模型内容增量。
  - `agent.run`：run 生命周期、retry、失败和恢复能力。
  - `agent.usage`：token、context 和耗时统计。
- 错误面向协议使用结构化对象；UI 可以本地化展示，但不应根据错误字符串猜测业务语义。

## 核心请求

| Method | 用途 |
|---|---|
| `agent.sendMessage` | 发送用户消息，内容通过 `parts` 承载文本和附件引用。 |
| `agent.resumeRun` | 当前 run 失败后，在不新增 user message 的情况下恢复未完成 turn。 |
| `agent.cancel` | 取消当前 run。 |
| `agent.askReply` | 回复 `askuser` 交互。 |
| `agent.guardReply` | 回复 Guard 确认。 |
| `session.new` | 新建会话。 |
| `session.restore` | 恢复最近会话展示状态。 |
| `session.compact` | 手动压缩当前会话上下文。 |
| `config.get` / `config.set` | 读取或更新配置。 |
| `daemon.status` / `daemon.stop` | 查询 daemon 状态或停止 daemon。 |
| `memory.*`、`skill.*`、`mcp.*` | 管理 memory、Skill 和 MCP runtime 状态。 |

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
{"state":"retrying","phase":"model","attempt":2,"max_attempts":3,"delay_ms":8000}
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

- 总尝试次数：3。
- retry 间隔：8 秒。
- 仅在尚未产生 assistant/reasoning/tool call 输出前 retry。
- 只根据结构化状态判断：HTTP `408`、`429`、`500`、`502`、`503`、`504`，以及 network / timeout。
- 不根据错误字符串判断是否 retry。

retry 期间 daemon 发送：

```json
{"state":"retrying","phase":"model","attempt":2,"max_attempts":3,"delay_ms":8000}
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

- `session.restore_message`：逐条恢复可见 user/assistant 消息。
- `session.restore_status`：恢复结束状态、compact 标记和上一轮有界工具摘要。

工具摘要是 TUI 展示状态，不作为原始工具历史重新注入模型。

## Compact result

`session.compact_result` 目前继续承载手动 compact 结果和 auto compact 的 running/error 状态。未来如果引入持久 Run/任务队列，可以把 compact lifecycle 逐步迁移到 `agent.run phase=compact`，但当前不强制合并，避免破坏现有 TUI compact 语义。
