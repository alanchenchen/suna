# TUI 架构

本文记录 `internal/tui` 当前重构后的结构和维护约定。TUI 使用 Bubble Tea 组织状态更新，目标是让页面、组件、事件和 transport 边界清晰，同时避免业务逻辑进入 UI 层。

## 目录结构

```text
internal/tui/
├── app_*.go              # root Bubble Tea model、Update、View
├── chat*.go              # root 侧 Chat 适配逻辑和旧状态兼容层
├── config*.go            # root 侧 Config 适配逻辑
├── events.go             # root notification handler
├── local_commands.go     # tea.Cmd 形式的 daemon 请求
├── components/           # 无状态或低状态复用组件
├── events/               # daemon notification 解码与流式合并
├── pages/                # 页面级 model/view/state
└── transport/            # TUI 到 daemon 的 local transport 适配
```

## Root TUI 职责

root TUI 负责全局 glue：

- 持有 Bubble Tea program、窗口尺寸、当前页面和复制模式。
- 管理 daemon 连接和 notification pump。
- 把 daemon notification 分发给对应页面状态。
- 注入主题、i18n、样式和 daemon 命令。
- 在迁移期间保留少量 Chat/Config 兼容适配逻辑。

root TUI 不应承载新的业务逻辑。新增功能如果属于 daemon 状态、工具行为、模型调用或安全策略，应放在对应核心包，并通过 protocol 暴露给 TUI。

## pages

`internal/tui/pages` 放页面级状态和纯页面行为。

当前主要页面：

- `pages/chat`：聊天运行态、输入策略、附件面板、模型选择、Skill 面板、guard 浮层、transcript 编排。
- `pages/config`：配置页状态、表单、模型列表、reasoning 参数、删除确认。
- `pages/welcome`：欢迎页列表和渲染。
- `pages/help`：帮助页渲染。
- `pages/page`：页面枚举，避免裸字符串散落。

页面包可以维护自身状态机，但不直接访问 daemon，也不直接执行副作用。需要副作用时返回结构化结果，由 root 转成 `tea.Cmd`。

## Chat transcript 性能设计

Chat transcript 遵循“完整数据在页面 model、渲染只取可见窗口”的策略，参考 Bubbles `table` / `list` 这类官方组件的窗口化思路，而不是把完整历史长期塞进 viewport。

当前实现边界：

- `pages/chat.Model.Messages` 保留完整消息和原始内容，是会话展示的业务数据源。
- `TranscriptBlocks` 维护消息到行数的布局信息，`TranscriptYOffset` 是全局滚动位置。
- 每次同步 transcript 时，只把当前窗口加 overscan 的 lines 传给 Bubbles viewport，viewport 不持有完整历史。
- 已完成 assistant 的 Markdown render cache 有内部预算；裁剪时只删除旧 rendered output，不删除原始消息和行数元数据。
- streaming assistant 继续走纯文本渲染，完成后再 Markdown 渲染，避免半截 Markdown 导致行数抖动和高频重排。
- 鼠标模式保持 Bubble Tea 的 cell motion，以保留触控板滚动；优化重点是降低每个 wheel event 后面的 transcript 工作量，而不是关闭 mouse capture。
- window signature 完全一致时跳过重复 `SetContentLines`；这不是帧率限制，不应改成基于时间的节流。

维护约定：

1. 不新增 fullscreen/classic/scrollback 双模式，除非有明确产品决策。
2. 不自己实现 terminal renderer、mouse protocol、clipboard 或原生 scrollback。
3. 不把 streaming 文本改为高频 Markdown 渲染。
4. 不用明显降帧、延迟 chunk 或合并用户可感知输出的方式解决性能问题。
5. 如果继续优化，优先保持 block/window/cache 边界清晰，并补充状态语义测试。

## components

`internal/tui/components` 放可复用 UI 组件，优先保持纯函数或低状态：

- `attachment`：附件识别和展示模型。
- `overlay`：简单浮层叠放。
- `scroll`：虚拟滚动数据源和窗口渲染。
- `text`：文本处理辅助。
- `toolview`：工具块和工具详情渲染。

组件包不应读取 root TUI 的全局状态，也不应直接使用 i18n。需要文案、样式或渲染依赖时，由调用方通过 deps/labels/styles 注入。

## events

`internal/tui/events` 负责将 daemon 原始 notification 转成 Bubble Tea 可处理的强类型消息：

- `Decode`：按 protocol method 解码参数。
- `Batcher`：合并高频 stream/reasoning delta。
- `NotificationMsg`：进入 root `Update` 后的强类型消息集合。

必要逻辑：

- 文本流可以按短间隔合并，减少 UI 重绘压力。
- 非文本事件必须先 flush 已合并文本再发送，避免 tool/done 被历史 delta 堵住。
- 解码失败要转成错误消息，不应 panic。

## transport

`internal/tui/transport` 是 TUI 侧 local transport 适配层，只负责 protocol request/notification：

- 不保存业务状态。
- 不直接修改 UI 状态。
- 不绕过 daemon 调用核心包。
- request 必须由 root 通过 `tea.Cmd` 间接调用，避免阻塞 Bubble Tea `Update` 主循环。

轻量请求使用默认超时；`compact` 这类可能触发模型总结的请求使用更长超时，避免 daemon 仍在处理时 TUI 先报本地 deadline。

## Update / Cmd 约定

Bubble Tea 的维护约定：

1. `Update` 只做状态转换和返回 `tea.Cmd`。
2. 所有可能阻塞的 daemon request 必须放入 `tea.Cmd`。
3. local transport 的通知读取 goroutine 不直接阻塞在 UI 更新上，统一通过 notification pump 入队。
4. UI 状态只能在 Bubble Tea 事件循环内修改。
5. 页面和组件尽量返回结构化意图，由 root 决定是否执行副作用。

## 注释约定

TUI 代码中的注释使用中文。以下情况必须保留或补充注释：

- goroutine、channel、batcher、timer 等并发逻辑。
- Bubble Tea `Update` 中不直观的状态转移。
- root 与 page/component 的迁移边界。
- 为避免阻塞、背压、重复渲染所做的特殊处理。
- 与 daemon/protocol 语义相关的适配逻辑。

过时注释应随代码调整同步清理，避免描述旧架构。

## 测试建议

TUI 改动优先补以下类型测试：

- 输入锁定和发送策略。
- askuser / guard / cancel 回归。
- notification decode 和 stream batching。
- 配置表单保存参数。
- 工具块渲染和详情浮层。
- 附件识别与删除。

局部验证命令：

```bash
go test ./internal/tui/...
```
