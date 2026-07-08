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

`pages/config` 只负责模型配置表单、校验提示和 protocol 参数构造。`context_window` 与 `max_output_tokens` 在 UI 上都是必填字段，但模型能力语义、配置持久化和运行时使用仍由 daemon / agent / model 层处理，TUI 不直接调用 provider 或改写核心状态。

## Chat 输入与附件

Chat 输入区优先处理终端传入的 paste 内容。`tea.PasteMsg` 进入现有文本粘贴路径，可识别普通文本、图片路径、图片 URL 和 `data:image/...;base64`。如果 TUI 收到 `ctrl+v` key event 且短时间内没有对应 PasteMsg，会在 TUI 层读取系统剪贴板图片作为 fallback。

剪贴板图片读取位于 `internal/tui/clipboard`，属于 UI 输入能力：daemon 只通过 protocol 告诉 TUI attachment root，不直接访问系统剪贴板。图片 bytes 进入 `components/attachment` 的统一校验，必须通过 MIME/大小检查后才进入 pending overlay；用户确认后按 SHA-256 落盘到 attachment 目录并作为现有 attachment ref 发送。

## Chat transcript 性能设计

Chat transcript 遵循“完整数据在页面 model、渲染只取可见窗口”的策略，参考 Bubbles `table` / `list` 这类官方组件的窗口化思路，而不是把完整历史长期塞进 viewport。

当前实现边界：

- `pages/chat.Model.Messages` 保留当前 TUI 展示窗口的消息和原始内容，是会话展示的业务数据源；它不是 daemon 模型上下文，也不是完整 transcript 归档。
- `TranscriptBlocks` 维护消息到行数的布局信息，`TranscriptYOffset` 是全局滚动位置。
- 每次同步 transcript 时，只把当前窗口加 overscan 的 lines 传给 Bubbles viewport，viewport 不持有完整历史。
- 滚轮和 PageUp/PageDown 会立即更新全局 offset；只有跨出当前 overscan window 时才重新同步 transcript，窗口内滚动只移动 viewport offset。
- reasoning 展开/折叠只渲染主界面实际显示所需的头部或尾部小窗口；已完成 reasoning 离屏时复用 line count，避免长思考链在滚动或 spinner tick 中反复渲染。
- 已完成 assistant 的 Markdown render cache 有内部预算；裁剪时只删除旧 rendered output，不删除原始消息和行数元数据。
- streaming assistant 继续走纯文本渲染，完成后再 Markdown 渲染，避免半截 Markdown 导致行数抖动和高频重排；流式阶段原文进入 append-only buffer，渲染缓存只保留尾部窗口，降低超长单回复每帧 join 全文的 CPU 和内存压力。
- TUI notification pump 仍按短间隔合并文本事件；Chat transcript 自身再用约 16ms 的 dirty sync frame 把正文视觉同步限制在约 60fps。这个限制只作用于 TUI viewport 同步，不改变 daemon 收流和关键事件语义。
- TUI 展示历史有固定内存预算：超过阈值后从最顶部按完整 turn 释放到低水位，裁后第一条真实消息仍是 user，并在顶部保留一个累计摘要块；这个裁剪只影响当前 TUI 展示，不影响 daemon 的 WorkingMemory / Session State，也不持久化。
- session attach/create snapshot 只把真实 user/assistant 作为最近可见消息恢复到 transcript；结构化工具摘要通过 snapshot `tool_summary` 返回，由 TUI 根据 locale 渲染成本地化 summary block，不要求 daemon 生成展示文案。
- 文本流活跃时，spinner tick 不额外触发完整 transcript sync；文本流停顿或等待首 token 时，spinner 仍正常刷新等待状态。
- 鼠标模式保持 Bubble Tea 的 cell motion，以保留触控板滚动；优化重点是降低每个 wheel event 后面的 transcript 工作量，而不是关闭 mouse capture。
- window signature 完全一致时跳过重复 `SetContentLines`；这不是帧率限制，不应改成基于时间的节流。

维护约定：

1. 不新增 fullscreen/classic/scrollback 双模式，除非有明确产品决策。
2. 不自己实现 terminal renderer、mouse protocol 或原生 scrollback；系统剪贴板图片读取属于 TUI 输入能力，只能作为用户主动粘贴的 fallback，不能下沉到 daemon/core。
3. 不把 streaming 文本改为高频 Markdown 渲染。
4. 不把 TUI 展示裁剪下沉成 daemon transcript 归档语义；daemon 只负责模型上下文和会话状态，TUI 自己管理展示缓存。
5. 不绕过 daemon/protocol 直接读取核心状态；若要降低渲染压力，优先在 TUI 视觉同步层做可感知性很低的合帧、窗口化或缓存优化。
6. 如果继续优化，优先保持 block/window/cache 边界清晰，并补充状态语义测试；不要为了省 CPU 让 TUI 直接绕过 protocol/daemon 读取核心状态。

## components

`internal/tui/components` 放可复用 UI 组件，优先保持纯函数或低状态：

- `attachment`：附件识别、粘贴图片校验和展示模型。
- `overlay`：简单浮层叠放。
- `scroll`：虚拟滚动数据源和窗口渲染。
- `text`：文本处理辅助。
- `toolview`：工具块和工具详情渲染。

组件包不应读取 root TUI 的全局状态，也不应直接使用 i18n。需要文案、样式或渲染依赖时，由调用方通过 deps/labels/styles 注入。

## events

`internal/tui/events` 负责将 daemon 原始 notification 转成 Bubble Tea 可处理的强类型消息：

- `Decode`：按 protocol method 解码参数。
- `Batcher`：合并高频 `agent.delta` 文本增量。
- `NotificationMsg`：进入 root `Update` 后的强类型消息集合。

必要逻辑：

- 文本流可以按短间隔合并，减少 UI 重绘压力。
- 非文本事件必须先 flush 已合并文本再发送，避免 tool/done 被历史 delta 堵住。
- 解码失败要转成错误消息，不应 panic。

## transport

`internal/tui/transport` 是 TUI 侧 local transport 适配层，只负责 protocol request / response / notification：

- 不保存业务状态。
- 不直接修改 UI 状态。
- 不绕过 daemon 调用核心包。
- request 必须由 root 通过 `tea.Cmd` 间接调用，避免阻塞 Bubble Tea `Update` 主循环。

轻量请求使用默认超时；`compact` 这类可能触发模型总结的请求使用更长超时，避免 daemon 仍在处理时 TUI 先报本地 deadline。

## Update / Cmd 约定

Bubble Tea 的维护约定：

1. `Update` 只做状态转换和返回 `tea.Cmd`。
2. 所有可能阻塞的 daemon request 必须放入 `tea.Cmd`。
3. local transport 的通知读取 goroutine 不直接阻塞在 UI 更新上，统一通过 notification pump 入队；method response 通过 typed local `tea.Msg` 进入 Update。
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
