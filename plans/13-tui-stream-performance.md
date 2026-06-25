# 13 — 流式传输与 TUI 渲染性能设计

> 最后更新: 2026-05-29
> 范围：描述 daemon 到 UI client 的流式传输背压控制，以及 TUI 前端 stream/reasoning 渲染性能与滚动交互。不改变 agent 业务语义。

## 背景

之前 TUI 对每个 `agent.delta` 都立即投递 Bubble Tea 事件，并在每个事件里全量 `syncContent()`、重跑 assistant Markdown。长回复或大上下文时，UI 消费速度会落后于 daemon 事件生产速度，表现为 daemon 已结束但 TUI 仍在补播历史 delta。

OpenAI-compatible 中转在高速碎片流下还会暴露另一类问题：部分中转会发送 heartbeat/comment-only/empty SSE event。`openai-go` 默认 decoder 会把这些空 payload 继续交给 `json.Unmarshal`，错误表现为 `unexpected end of JSON input`。因此当前设计同时保护三条路径：兼容空 SSE 事件；上游 LLM 读取不能被 UI 速度轻易拖住；UI 渲染也不能被每个小 delta 打爆。

## 当前实现

### SSE 空事件兼容

OpenAI-compatible provider 使用 `openai-go` 时，全局注册兼容 decoder：

- `text/event-stream` 使用 `internal/model/sse_decoder.go` 中的 `compatibleSSEDecoder`。
- 跳过 heartbeat/comment-only SSE event，例如 `: ping` 后跟空行。
- 跳过空 `data:` event。
- 正常 JSON data 和 `[DONE]` 原样交给 SDK 后续 stream 逻辑处理。

这个修复覆盖 OpenAI Chat Completions、OpenAI Responses 以及其他使用 `openai-go/packages/ssestream` 的 streaming 路径，不影响 Anthropic SDK。

同时，OpenAI-compatible header normalizer 不再覆盖 `Accept`，避免破坏 SDK 对 SSE 的协议协商；仍保留 `User-Agent` 和 Stainless 追踪头清理。

OpenAI-compatible Chat Completions 保留 `stream_options.include_usage=true`，不牺牲 usage 统计。如果服务端自行返回 usage，现有 usage 解析逻辑也会继续接收。

### daemon 传输微批处理

daemon 在 `runAgent` 出口对文本流做传输级 micro-batching：

- provider chunk channel 和 agent event channel 使用 2048 有界缓冲，用于吸收 LLM 服务在碎片化 SSE 下的短时尖峰，同时避免过高的 daemon 常驻内存。
- daemon 只合并 `agent.delta` 文本，默认 8ms flush 一次。
- 单类文本 batch 超过 32KB 时立即 flush，避免单个 JSON-RPC 事件过大。
- 遇到 usage、tool、ask、guard、done、error、cancelled 等关键事件时，先 flush pending 文本，再即时发送关键事件。
这个 batcher 是传输级优化，不是 UI 缓存。daemon 不保存 Markdown 渲染状态、滚动状态、折叠状态或 copy mode；这些仍由具体 UI client 负责。

### 事件合并

TUI 在 notification pump 层合并连续文本流：

- 只合并 `agent.delta`。
- 默认约 8ms flush 一次，保留接近字符级的 notification 反馈；Chat transcript 视觉同步另有约 16ms dirty frame，用于把 viewport 正文刷新限制在约 60fps。
- 遇到 `agent.run`、tool、ask、guard、error 等非文本事件时，先 flush pending 文本，再即时投递状态事件。
- local transport read loop 仍不阻塞在 `program.Send` 上，避免反向卡住 daemon 写入。

### 流式轻量渲染

流式中的 assistant/reasoning 不再每个 delta 都跑 Glamour：

- assistant streaming：轻量 wrap + indent，保证实时顺滑。
- reasoning detail streaming：轻量 wrap，保留实时查看能力。
- 收到 stream `done` 后清除 streaming 标记，再对最终 assistant 内容使用完整 Glamour Markdown。

最终展示给用户的 assistant/reasoning detail 仍是 Markdown，不降级最终体验。

### 长文本 overlay 虚拟滚动

工具详情 overlay 不再先完整生成所有展示行：

- `internal/tui/virtual_scroll.go` 提供 `virtualLineSource`，只暴露 `Len()` 和 `Line(index)`。
- tool detail 将 title/tool/intent/params/guard/result 拆成 section，并通过 `virtualScrollWindow` 只渲染当前可见窗口。
- `wrappedLineSection` 只保存原始逻辑行和 wrap 后行数；滚动到某一行时才切出目标展示行，不缓存完整 wrap 后文本。
- `Ctrl+T`、`Esc`、`PgUp/PgDn`、`↑/↓`、鼠标滚轮这类只影响 overlay 的操作不再调用 `syncContent()`，避免重算整个 chat viewport。
- Chat 当前 `bubbles/viewport` 只负责显示窗口滚动，`syncContent()` 使用消息级窗口化和约 16ms 视觉合帧；若长会话后续仍成为瓶颈，应按同一 line source 模型继续降低可见窗口切片成本。

### Markdown 缓存

已完成的 assistant 消息按以下维度缓存渲染结果：

- 内容；
- viewport 宽度；
- 当前主题。

历史消息不因后续 stream delta 反复重渲染。

### 贴底滚动

新增 `followBottom` 状态：

- 用户发送消息后立即恢复贴底并滚动到底部，让用户确认消息已发送。
- 等待回复和处于底部时，流式输出自动跟随底部。
- 用户 PgUp/鼠标上滚离开底部后，暂停自动跟随，避免打断阅读历史。
- 用户滚回底部或再次发送消息后，恢复自动跟随。

### 交互细节修正

- reasoning 在 streaming 阶段默认也保持折叠，只显示 compact 摘要和 `Ctrl+R` 提示；用户手动展开后才显示实时详情。
- reasoning 时长绑定到单条 reasoning message 的 `startedAt/endedAt`，完成后固定，不再继续跟随全局 phase 计时。
- LLM/tool 运行期间 composer 进入只读/失焦状态，显示当前状态与 Esc 取消提示；保留滚动、帮助、工具/思考详情快捷键。

## 设计约束

- daemon 只输出 UI 无关的语义事件，不输出 TUI 专用展示指令。
- daemon 可以做有界、短生命周期的传输缓冲和文本 micro-batching，但不做 UI 缓存。
- provider 读取上游 LLM stream 时应尽量避免被 client 渲染速度反压。
- OpenAI-compatible SSE decoder 必须忽略空 SSE 事件，避免中转 heartbeat 被当作 JSON 解析。
- 不合并 tool/ask/guard/done 等状态事件，保证交互及时。
- `done` 必须优先触发 pending 文本 flush，避免结束状态被大量旧 delta 堵住。
- 流式轻量渲染只用于运行态；最终展示必须保持 Markdown。
- 性能优化优先发生在 UI 视觉同步、窗口化和缓存层；不得延迟 tool/ask/guard/done 等关键事件，也不得改变 daemon 传输语义。
- 新增代码文件必须保持小文件，避免继续扩大 `app.go` / `chat.go`。

## 诊断

OpenAI Chat Completions 和 Responses stream 失败时记录最小诊断字段：

- `chunk_count`
- `assistant_bytes`
- `reasoning_bytes`
- `usage_received`
- `last_chunk_age_ms`

这些字段用于区分错误发生在开头、中途、usage final chunk 前后，还是长时间无 chunk 后。
