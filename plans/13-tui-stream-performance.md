# 13 — TUI 流式渲染性能设计

> 最后更新: 2026-05-29
> 范围：只描述 TUI 前端的 stream/reasoning 渲染性能与滚动交互，不改变 daemon/agent 协议语义。

## 背景

之前 TUI 对每个 `agent.stream` / `agent.reasoning` delta 都立即投递 Bubble Tea 事件，并在每个事件里全量 `syncContent()`、重跑 assistant Markdown。长回复或大上下文时，UI 消费速度会落后于 daemon 事件生产速度，表现为 daemon 已结束但 TUI 仍在补播历史 delta。

## 当前实现

### 事件合并

TUI 在 notification pump 层合并连续文本流：

- 只合并 `agent.stream` 和 `agent.reasoning`。
- 默认约 16ms flush 一次，接近帧级刷新。
- 遇到 `done`、tool、ask、guard、error 等非文本事件时，先 flush pending 文本，再即时投递状态事件。
- local transport read loop 仍不阻塞在 `program.Send` 上，避免反向卡住 daemon 写入。

### 流式轻量渲染

流式中的 assistant/reasoning 不再每个 delta 都跑 Glamour：

- assistant streaming：轻量 wrap + indent，保证实时顺滑。
- reasoning detail streaming：轻量 wrap，保留实时查看能力。
- 收到 stream `done` 后清除 streaming 标记，再对最终 assistant 内容使用完整 Glamour Markdown。

最终展示给用户的 assistant/reasoning detail 仍是 Markdown，不降级最终体验。

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

## 设计约束

- 不要求 daemon 降低 stream 事件速度。
- 不合并 tool/ask/guard/done 等状态事件，保证交互及时。
- `done` 必须优先触发 pending 文本 flush，避免结束状态被大量旧 delta 堵住。
- 流式轻量渲染只用于运行态；最终展示必须保持 Markdown。
- 新增代码文件必须保持小文件，避免继续扩大 `app.go` / `chat.go`。
