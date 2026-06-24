# 性能优化

本文集中记录 Suna 当前已经实现的性能优化。它只描述当前代码事实，不把 `plans/` 中的历史方案或未来设想当作已完成能力。

Suna 的性能目标不是通过降低交互体验来省资源，而是在保持 TUI 实时反馈、模型流式输出和安全边界的前提下，减少不必要的传输、渲染、上下文和本地计算开销。

## 总体原则

- **业务语义不下放到 TUI**：TUI 只做交互和渲染，daemon / agent / runner 负责模型、工具、安全、记忆和持久化。
- **稳定前缀优先**：system prompt、项目指令、Skill index 和 tool schema 尽量稳定，降低模型前缀缓存失效概率。
- **长历史不全量重放或渲染**：模型上下文使用 Session State + recent window；TUI Chat transcript 使用业务层虚拟滚动、流式尾部窗口渲染和展示历史预算，不把完整历史交给 viewport。
- **关键事件不延迟**：stream/reasoning 文本可以短周期合并，tool、ask、guard、done、error、usage 等关键事件必须及时 flush。
- **有界缓存和有界输出**：渲染缓存、工具结果、文件变化统计和 compact 输入都设置边界，避免大输出导致 CPU 或内存爆炸。

## Daemon / IPC 流式传输

### 文本 micro-batching

daemon 在 agent 事件出口对 `agent.stream` 和 `agent.reasoning` 做传输级 micro-batching：

- 默认 8ms flush 一次。
- 单类文本 batch 超过 32KB 立即 flush。
- 只合并文本 delta，不合并 tool、ask、guard、done、error、cancelled、usage 等状态事件。
- 遇到关键事件前会先 flush pending 文本，避免结束状态被旧 delta 堵住。

相关代码：

- `internal/daemon/service.go`
- `internal/daemon/stream_batcher.go`
- `internal/daemon/stream_batcher_test.go`

这个优化减少 local JSON-RPC notification 数量和 TUI 消息压力，但不改变模型输出文本，也不丢弃 delta。

### 有界事件缓冲

provider chunk channel 和 agent event channel 使用有界缓冲，吸收高速碎片化 SSE 的短时尖峰，同时避免 daemon 常驻内存无限增长。

相关代码：

- `internal/model/provider.go`
- `internal/agent/events.go`

### 连接和写阻塞边界

local transport 发送带 context timeout；daemon 可以支持多个本地连接，并按连接维护 event sink。TUI 的 notification 读取不直接在 UI 渲染上阻塞，避免客户端渲染速度轻易反压 daemon 事件生产。

相关代码：

- `internal/transport/local`
- `internal/tui/local_commands.go`
- `internal/tui/events/events.go`

## Provider streaming 兼容性

OpenAI Responses 和 OpenAI-compatible Chat streaming 使用 `openai-go` stream；Suna 注册兼容 `text/event-stream` decoder：

- 跳过 heartbeat / comment-only SSE event。
- 跳过空 `data:` event。
- 正常 JSON data 和 `[DONE]` 保持原语义。
- OpenAI-compatible header normalizer 不覆盖 `Accept`，避免破坏 SDK SSE 协议协商。

相关代码：

- `internal/model/sse_decoder.go`
- `internal/model/sse_decoder_test.go`
- `internal/model/openai_chat.go`
- `internal/model/openai_responses.go`

这个优化提升中转站兼容性，避免空 SSE payload 被当作 JSON 解析导致 `unexpected end of JSON input`。

## 上下文与模型请求

### Session State + recent window

长对话不靠无限追加完整历史维持连续性。自动或手动 compact 后，Suna 使用：

```text
稳定前缀：system / project instructions / skill index / tool schema
低频变化：Session State
近期内容：recent messages
靠近最新用户输入：user profile memory
```

关键点：

- Session State 不拼进 system prompt，而是作为独立上下文字段注入 provider 请求。
- compact 后 working memory 只保留 budget-aware recent window。
- recent window 由代码按 token budget 选择，不能交给 LLM 随意决定。
- compact 失败时不 fallback、不伪压缩、不硬裁剪继续，避免带着不可靠状态撞 provider context limit。
- Suna 采用 proactive compaction：优先在安全边界前用 Session State 做高质量压缩，而不是把上下文塞到 provider 极限后依赖 overflow retry。
- 基础输入预算按模型配置计算：`context_window - max_output_tokens - margin`，其中 `margin = max(2048, context_window / 200)`。`max_output_tokens` 会作为完整输出预留扣除，因此比只预留默认输出长度更安全，但也可能更早 compact。
- 自动 compact 不直接使用 provider 返回的 `input_tokens` / `context_tokens`。这些 usage 只用于统计、对账和诊断；压缩判断使用 Suna 请求前的本地估算。
- 实际判断使用 `compact_context_tokens = estimated_context_tokens + estimator_safety_tokens`，其中 `estimator_safety_tokens = max(8192, estimated_context_tokens / 16)`。TUI `ctx` 显示 raw `estimated_context_tokens`，不会把 safety 混入 UI 数字。
- Suna 不再使用 `context_window * 0.8` 作为压缩阈值，也不提供单独的 `reserved_output_tokens` / `default_output_tokens`；如需降低 compact 频率，不能简单降低 `max_output_tokens`，因为它也会限制真实输出。
- `max_output_tokens` 是所有 LLM 请求的默认硬输出上限，包括 chat、subtask、Guard review、Skill review、Session State compact 和 memory compact。

相关代码：

- `internal/runner/compression.go`
- `internal/memory/compress.go`
- `internal/model/session_state.go`
- `internal/model/openai_chat.go`
- `internal/model/openai_responses.go`
- `internal/model/anthropic.go`

### 缓存友好请求结构

模型请求保持自然前缀稳定：

- tool schema 由 `tools.Manager` 统一维护，顺序和 schema 尽量稳定。
- tool definitions 每次请求都计入上下文，因此 description 和参数说明保持紧凑、单意，避免冗长重复；默认值/上限统一写成 `Default X, max Y` 形式。
- user profile memory 靠近 latest user，而不是插在 prior conversation 前破坏大段前缀。
- Suna 不默认注入 provider-specific `cache_control` / `prompt_cache_key`，避免为单个厂商破坏 OpenAI-compatible 通用结构。

相关文档：

- `docs/design.md`
- `docs/architecture.md`

## 记忆与后台整理

user profile memory 不保存完整对话，也不保存项目任务日志。主链路只用规则提取结构化候选进入 `memory_queue`，daemon worker 再批量合并：

- 候选提取不调用 LLM，避免每轮对话增加额外模型请求。
- `memory_queue` 只保存结构化候选，不保存原始长对话。
- worker 默认按 batch size / timeout 处理，少量候选不会永远停留在队列。
- daemon 退出时不强制 drain 未处理队列；pending item 留在 SQLite，下次启动后继续处理。

相关代码：

- `internal/memory/queue.go`
- `internal/memory/worker.go`
- `internal/memory/active.go`
- `internal/memory/store.go`

## 工具和大输出控制

### 工具结果上下文截断

工具结果进入模型上下文前会做截断，避免 raw logs、大文件内容或超长命令输出吞掉上下文窗口。TUI 展示级工具结果也有单独大小限制，完整细节优先通过工具详情 overlay 查看，而不是塞进主 transcript。

相关代码：

- `internal/memory/compress.go`
- `internal/runner/runner.go`
- `internal/daemon/service.go`
- `internal/agent/tools.go`

### 文件变化统计有上限

文件写入 / 编辑返回摘要时，不生成完整 diff。行数统计只在变化片段较小时使用 LCS；变化过大时退化为前后缀差异统计，避免 `O(n*m)` 计算爆炸。

相关代码：

- `internal/tools/builtin/filechange.go`
- `internal/tools/builtin/writefile.go`
- `internal/tools/builtin/editfile.go`

### 内置工具输出边界

- `exec` 对 stdout / stderr 做有界收集和截断。
- `http` 默认限制响应 body，可通过 `max_body_bytes` 调整。
- `readfile` / tail 相关逻辑按行范围、tail 或块读取，避免无意读取超大内容。
- `search` 支持目录和单文件搜索，`auto` 模式按 path / symbol / content 分组返回少量上下文；其中 symbol 是文档标题、配置段/key、常见定义/声明等轻量结构入口，不限于代码。仍默认排除常见依赖、构建、缓存和敏感文件，并限制单文件大小、扫描文件数、结果数和输出字节数。空结果或截断诊断写入正文，不破坏 TUI metadata contract。

相关代码：

- `internal/tools/builtin/exec.go`
- `internal/tools/builtin/http.go`
- `internal/tools/builtin/readfile.go`
- `internal/tools/builtin/readtail.go`
- `internal/tools/builtin/search.go`
- `internal/tools/builtin/search_format.go`

## TUI 流式与渲染

### TUI notification batching

TUI notification pump 会合并连续 `agent.stream` / `agent.reasoning` 文本：

- 默认约 8ms flush 一次。
- 非文本事件前先 flush pending 文本。
- local transport read loop 不阻塞在 Bubble Tea `program.Send` 上。

这里的 8ms 是 TUI 接收 daemon notification 后的文本事件合并间隔，不是聊天正文最终同步到 viewport 的帧率上限。Chat transcript 还有单独的脏标记同步帧，默认约 16ms flush 一次，把正文视觉刷新限制在约 60fps；daemon 收流、关键事件和文本 delta 语义不受影响。

相关代码：

- `internal/tui/events/events.go`
- `internal/tui/local_commands.go`

### streaming 轻量渲染、增量 wrap cache 与 spinner 协调

assistant streaming 阶段使用轻量 wrap + indent，不高频运行 Glamour Markdown。收到 done 后清除 streaming 标记，再对最终 assistant 内容做完整 Markdown 渲染。

streaming 文本只追加时，TUI 会把原文写入 append-only buffer，完成时再 materialize 成最终 assistant 文本；渲染侧复用当前消息已经 wrap 好的尾部行，并只增量处理新 delta、换行状态和最后一行宽度。窗口宽度变化时才完整重算当前 streaming 文本。这个优化不丢弃 delta、不延迟 done/tool/guard 等关键事件，而是避免 `prev + chunk` 反复复制和长回复 O(n²) 重排 CPU。

流式渲染缓存只保留尾部行窗口，主界面无需每帧 join 已生成全文；完整原文仍在 streaming buffer 中，done 后进入最终 Markdown 渲染。因此超长单次回复的流式 CPU 峰值主要与尾部窗口和新 delta 相关，而不是与当前回复全文线性绑定。

TUI 聊天正文同步使用 16ms 左右的帧间隔合并 dirty transcript 更新，约等于 60fps。这个合帧只发生在 TUI 视觉同步层，不改变 daemon 传输 micro-batching，也不降低模型流式读取或关键事件投递语义。

这个设计保留流式实时反馈，同时避免半截 Markdown 造成行数抖动、viewport 抖动和高频重排。最终 completed 内容仍是 Markdown，不降低最终阅读体验。

spinner tick 不再无条件触发完整 transcript sync：当最近 120ms 内收到 assistant / reasoning 文本流，且当前仍有 streaming 文本消息时，文本流事件本身负责驱动 UI 刷新，spinner tick 只更新 spinner 状态；如果文本流停顿或尚未开始，spinner tick 仍会刷新等待状态。这个策略不降低 stream flush 频率，也不关闭 spinner，只避免长回复期间多一条重复的 transcript 同步来源。

相关代码：

- `internal/tui/chat_render.go`
- `internal/tui/chat.go`
- `internal/tui/events.go`
- `internal/tui/pages/chat/runtime.go`

### Chat transcript windowed rendering / 虚拟滚动

长历史 Chat transcript 使用窗口化渲染。这里的“虚拟滚动”指 Suna 在 Chat 业务层维护当前 TUI 展示历史和全局滚动位置，但只把当前可见窗口交给 viewport；它不是自定义 terminal renderer，也不是依赖宿主终端原生 scrollback：

- 当前 TUI 展示消息保留在 `pages/chat.Model.Messages`，它不是 daemon 模型上下文，也不是完整 transcript 归档。
- `TranscriptBlocks` 维护消息行数和布局信息。
- `TranscriptYOffset` 是全局滚动位置。
- 每次只把当前可见区域上下各一屏 overscan 的 lines 传给 Bubbles viewport。
- viewport 不再持有完整 transcript。
- 鼠标滚轮、触控板、PageUp/PageDown、跳到回复开头/底部和复制模式语义保持不变。
- 滚动仍然即时应用；只有跨出当前 transcript window 时才重新同步 transcript，在当前 overscan window 内只移动 viewport offset，避免连续滚动时每个 wheel event 都触发完整同步。

相关代码：

- `internal/tui/pages/chat/transcript.go`
- `internal/tui/pages/chat/model.go`
- `internal/tui/chat.go`
- `internal/tui/chat_view.go`
- `internal/tui/pages/chat/transcript_test.go`

该设计参考 Bubbles `table` / `list` 的窗口化思路：展示数据留在业务 model，View 只渲染当前窗口，而不是把无限增长的历史交给 viewport。因此滚动成本主要跟终端高度、overscan 和当前窗口内容相关。连续滚动不会被延迟或丢弃；优化点是跳过未跨窗口滚动时的 transcript 重建。

### TUI 展示历史预算

TUI 不把展示历史下沉成 daemon 归档语义。Chat 页面维护自己的展示内存软上限：超过阈值后，从最顶部按完整 turn 裁剪到低水位，裁后第一条真实消息必须仍是 user；顶部只保留一个累计摘要块。该裁剪只影响当前 TUI 进程的显示缓存，不影响 daemon `WorkingMemory`、`Session State`、模型上下文或会话恢复语义。

相关代码：

- `internal/tui/pages/chat/display_trim.go`
- `internal/tui/pages/chat/transcript.go`
- `internal/tui/chat.go`

### reasoning detail 可见窗口渲染

reasoning 展开/折叠不通过降低刷新率解决性能问题，而是只渲染主界面实际会显示的源文本窗口：

- 折叠态摘要只从 reasoning 尾部小窗口提取最后一句，避免长思考链每帧全量扫描。
- 运行中的 detail 只取尾部有限行数/字节后再 wrap，因为主界面只展示最新少量行。
- 已完成的 detail 只取头部有限行数/字节后再 Markdown，避免为了主界面的小框对完整 reasoning 做 Glamour 渲染。
- 已完成 reasoning 离屏时复用已缓存的 line count，不重复调用 reasoning 渲染函数；进入可见窗口时再按需渲染。
- tab 会先展开为空格再进行 wrap，避免终端 tab stop 和宽度计算不一致导致边框错位。

相关代码：

- `internal/tui/chat_render.go`
- `internal/tui/pages/chat/transcript.go`

### Markdown render cache 有界

已完成 assistant 的 Markdown render cache：

- 命中条件使用 width、theme、content length 和 content hash。
- cache 不保存额外完整原文。
- 超过内部预算时，只裁剪远离当前窗口的旧 rendered output。
- 保留原始消息和行数元数据，滚回旧内容时按需重新渲染。
- 保留当前窗口附近和最近消息，避免正常阅读时出现可见重渲染抖动。

相关代码：

- `internal/tui/chat_render.go`
- `internal/tui/pages/chat/transcript.go`

### 重复窗口刷新跳过

transcript viewport window 使用内容签名：window start/end、width/height、total lines 和 visible lines hash 完全一致时，跳过重复 `SetContentLines`。这不是帧率限制，也不是 stream 节流；Bubble Tea 仍正常处理输入和事件，只是不重复塞同一份内容给 viewport。滚动路径会区分“只移动当前窗口内 offset”和“跨出 window 需要重建可见内容”，避免未跨窗口滚动也触发完整 `syncContent()`。

相关代码：

- `internal/tui/pages/chat/transcript.go`

### 工具详情 overlay 虚拟滚动

工具详情 overlay 使用 `LineSource` 和窗口渲染：

- 工具详情被拆成 section。
- 只渲染当前可见窗口。
- wrap 后行不需要完整常驻缓存。
- overlay 内滚动不触发整个 Chat transcript 重建。

相关代码：

- `internal/tui/components/scroll/scroll.go`
- `internal/tui/components/toolview/detail.go`

## 仍然刻意不做的事

- 不为了性能关闭 mouse capture；Mac 触控板滚动依赖 Bubble Tea cell motion。
- 不把 streaming 文本改成高频 Markdown 渲染。
- 不靠明显降帧、延迟 chunk 或用户可感知的输出合并解决性能。
- 不自己实现 terminal renderer、mouse protocol、clipboard 或原生 scrollback 双模式。
- 不默认加入 provider-specific prompt cache 字段。
- 不把完整会话历史、完整 tool result 或完整 rendered transcript 无限塞进模型上下文或 viewport。
- 不把 TUI 展示裁剪升级成 daemon transcript 归档；不同 UI 可以有自己的展示缓存策略。

## 验证建议

性能相关改动至少运行：

```bash
gofmt -w <changed-go-files>
go test ./internal/tui ./internal/tui/pages/chat
go test ./internal/daemon ./internal/model ./internal/memory ./internal/runner ./internal/tools/builtin
git diff --check
```

较大范围改动建议执行：

```bash
go test ./...
```
