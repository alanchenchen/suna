# 01 — 有状态实体架构

## 范式转变

传统 agent 是无状态循环：收到消息 → 构建 prompt → 调用 LLM → 执行工具 → 回复 → 等待。每一轮都是独立事件。

Suna 是**持续运行的有状态实体**：感知环境、积累经验、主动预测、按需行动。

```
传统 Agent = 算命先生（你说一句他答一句，说完就忘）
Suna       = 学徒（持续在场，越久越懂你，你不说话他也在观察）
```

## 三层架构

```
┌──────────────────────────────────────────────────────────┐
│  sunad (守护进程，常驻)                                    │
│                                                            │
│  ┌──────────┐  ┌──────────┐                               │
│  │ 输入层    │  │ 记忆层    │                               │
│  │ Input    │  │ Memory   │                               │
│  │          │  │          │                               │
│  │ 用户消息  │  │ Active   │                               │
│  │ Protocol │→│ Memory   │                               │
│  │ AskUser  │  │ 异步整理  │                               │
│  │ Guard    │  │ 用户画像  │                               │
│  │ Config   │  │ 短上下文  │                               │
│  └──────────┘  └──────────┘                               │
│         │                              │                  │
│         ▼                              ▼                  │
│  ┌──────────────────────────────────────────────────────┐ │
│  │ 行动层 Act                                            │ │
│  │                                                      │ │
│  │ Agent / Runner / Subtask (LLM 驱动)                   │ │
│  │ 7 registry tools + 2 agent built-ins                  │ │
│  │ 多模型路由 (main-agent delegated)                     │ │
│  │ Guard (4 mode: readonly/ask/auto/smart)               │ │
│  │ Skills + MCP                                          │ │
│  └──────────────────────────────────────────────────────┘ │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐ │
│  │ Protocol / Transport                                 │ │
│  │ protocol: 请求、响应、事件、Service、Transport 挂载接口 │ │
│  │ transport: Local socket / Named Pipe / 远期 Web       │ │
│  └──────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```

各层文档：
- 感知层 → [07-trigger.md](07-trigger.md) (当前为预留/设计，不是已运行模块)
- 记忆层 → [06-memory.md](06-memory.md)
- 行动层 → [03-tools.md](03-tools.md) + [04-guard.md](04-guard.md) + [02-model-router.md](02-model-router.md)

## Protocol / Transport 边界

Suna 对外通信应收敛为三层：

```text
client -> transport implementation -> protocol.Service -> daemon/agent -> provider
```

`protocol` 是 daemon/agent 的稳定通信协议，不绑定具体通信方式。它定义：

- 请求 / 响应 schema，例如 `SendMessageRequest`、`StatusRequest`。
- 事件 schema，例如 stream、tool_start、tool_guard、tool_end、ask_user、guard_confirm、daemon_status。
- 多模态输入 schema，例如 `MessagePart`、`AttachmentRef`。
- `Service` 接口，由 daemon/agent 侧实现。
- `EventSink` 接口，用于 service 向当前客户端推送事件。
- `Transport` 挂载接口，用于 daemon 统一启动不同 transport。

示意接口：

```go
type Service interface {
    SendMessage(ctx context.Context, req SendMessageRequest, sink EventSink) (SendMessageResponse, error)
    Cancel(ctx context.Context, req CancelRequest) (CancelResponse, error)
    Status(ctx context.Context, req StatusRequest) (StatusResponse, error)
    RestoreSession(ctx context.Context, req RestoreSessionRequest, sink EventSink) (RestoreSessionResponse, error)
    AskReply(ctx context.Context, req AskReplyRequest) (AskReplyResponse, error)
    GuardReply(ctx context.Context, req GuardReplyRequest) (GuardReplyResponse, error)
}

type EventSink interface {
    Emit(ctx context.Context, event Event) error
}

type Transport interface {
    Name() string
    Mount(ctx context.Context, svc Service) error
    Close(ctx context.Context) error
}
```

`transport` 只实现某种具体线协议如何承载 `protocol`：

```text
internal/transport/local
  - TUI 本地连接
  - macOS/Linux: Unix socket
  - Windows: Named Pipe
  - 当前可继续使用 NDJSON + JSON-RPC 作为本地线协议

internal/transport/web (远期)
  - Web UI 连接
  - 可使用 HTTP / WebSocket / SSE
  - 不要求使用 JSON-RPC
```

JSON-RPC 只是 local transport 的线协议细节，不属于 `protocol`。未来 Web UI 可以用 HTTP route、SSE 或 WebSocket frame 承载同一套 `protocol` schema。

daemon 的职责是实现 `protocol.Service`，并挂载已配置的 transport：

```go
svc := NewProtocolService(agent, memory, config, ...)
for _, tr := range transports {
    go tr.Mount(ctx, svc)
}
```

这样 daemon 不需要知道每个客户端如何连接；不同客户端只需要实现或使用对应 transport。

### 平台实现约束

Local transport 的平台差异必须使用 Go build tags 做编译期隔离，不做运行时 OS 判断。

```text
transport/local/transport_unix.go       //go:build !windows
transport/local/transport_windows.go    //go:build windows
```

Unix socket 和 Windows named pipe 是同一个 local transport 的不同平台实现；它们应导出相同构造函数或满足同一接口，让 daemon 挂载时不关心当前平台。

### 多模态输入位置

多模态输入应进入 `protocol` schema，而不是进入某个具体 transport：

```go
type SendMessageParams struct {
    ClientMsgID string        `json:"client_msg_id,omitempty"`
    Parts       []MessagePart `json:"parts"`
}

type MessagePart struct {
    Type   string        `json:"type"` // text | image
    Text   string        `json:"text,omitempty"`
    Source AttachmentRef `json:"source,omitempty"`
}

type AttachmentRef struct {
    Kind     string `json:"kind"` // path | url | attachment
    Path     string `json:"path,omitempty"`
    URL      string `json:"url,omitempty"`
    MimeType string `json:"mime_type,omitempty"`
    Name     string `json:"name,omitempty"`
    Size     int64  `json:"size,omitempty"`
}
```

TUI/local transport 只传 `path`、`url` 或 `attachment` 引用。粘贴的 `data:image/...;base64,...` 必须先在 TUI 本地保存到默认数据目录的 `attachments/`（当前默认 `~/.suna/attachments`），再作为 `attachment` 发送。daemon 侧二次校验并规范化为 `model.ContentBlock{MediaRef}`；agent、runner、subtask 只传轻量引用；provider 请求阶段再通过 media resolver 临时转成各协议需要的 URL/base64。raw media 不进入 working memory、conversation_state 或 user_memory。

当前 TUI 的 `agent.stream`、`agent.tool_start`、`agent.tool_guard`、`agent.tool_end`、`agent.ask_user`、`agent.guard_confirm` 等事件也应归入 `protocol`，因为它们是 daemon 对外一致事件流，不是 local transport 私有事件。

## Agent / Runner / Subtask

行动层拆为三层代码边界：`internal/agent` 是唯一对外编排层，`internal/runner` 是通用 agent loop 引擎，`internal/subtask` 是 spawn 创建的一次性轻量任务执行器。当前输入来自 `protocol` 用户消息、AskUser/Guard 回传和管理命令；Timer/Watcher/Webhook/Stream 感知信号仍是预留设计。

术语约定：`spawn` 是 main agent 可调用的工具/动作；`subtask` 是由 `spawn` 创建的隔离运行单元；`subtask_system.md` 是 subtask 的独立系统提示词模板。

```
┌─────────────────────────────────────────────────────────────┐
│  1. 接收输入                                                  │
│     ├── 用户消息 (直接指令，最高优先级)                        │
│     ├── AskUser / Guard 回传                                  │
│     └── Subtask 返回                                         │
│                                                               │
│  2. 构建请求                                                  │
│     ├── System Prompt (固定模板 + AGENTS.md + active skill index)   │
│     ├── 工具定义 (main 暴露内置工具；subtask 只暴露授权工具)    │
│     ├── 对话历史 / compact summary                            │
│     ├── Active Memory brief (最多 5 条，见 06-memory.md)      │
│     └── 当前用户消息                                           │
│                                                               │
│  3. 调用模型 (streaming 能力取决于 provider，路由见 02)       │
│     └── 输出文本 + 可能的 tool_calls                           │
│                                                               │
│  4. 处理输出                                                  │
│     ├── 纯文本 → 流式推送给用户                               │
│     ├── 同一批 Tool Calls → 并发执行，结果按原顺序回填           │
│     │   ├── Perceive 工具 → 直接执行                          │
│     │   ├── Act 工具 → 经过 Guard 审查 → 执行                 │
│     │   │   (Exec 中可证明只读命令经轻量 shell analyzer 放行)   │
│     │   └── Spawn 工具 → 创建 subtask (仅 main)               │
│     └── 工具结果 → 追加到对话历史 → 回到步骤 2                 │
│                                                               │
│  5. 终止条件: 模型不再发起 tool_call + 输出结束                │
│                                                               │
│  6. 记忆提取 (见 06-memory.md)                                │
│     └── 异步: 写入提取队列，daemon 后台 worker 处理             │
│         不阻塞 agent loop，不受 TUI 生命周期影响                │
└─────────────────────────────────────────────────────────────┘
```

### 并发模型

```
Main agent:  模型请求按 loop iteration 串行
             同一 assistant message 返回的多个 tool_calls 并发执行
             结果按原 tool_call 顺序写回 working memory

Subtasks:    Spawn 作为 tool call 执行
              多个 Spawn 出现在同一批 tool_calls 时会并发运行
              每个 subtask 有独立 working memory 和 timeout
              Subtask 的 tool call/guard/guard confirm/result 通过 main 事件流转发
              Subtask tool ID 使用 spawn:<parentToolCallID>:<subToolCallID>，Guard 事件使用同一 ID 挂载到对应子工具
              Subtask stream/reasoning 不对外展示
              Main 收集全部 tool result 后继续下一轮 LLM
```

## Main / Subtask 二分法

只有两种产品角色，但共享同一个通用 `runner`。`runner` 不知道 main/subtask，也不拥有 session、TUI、conversation restore 或 memory extraction。

### Main Agent (`internal/agent`)

- 拥有全部 9 个对模型暴露的工具定义：7 个 registry tools，加 `askuser`/`spawn` 两个 agent built-ins（不注册到通用 tool registry）
- 负责任务理解、拆分、调度、结果汇总
- 管理所有 subtask 的生命周期
- 系统提示词固定，由 Suna 内核生成
- 可以同时运行多个 subtask（goroutine 并发）
- 是唯一对外事件流 owner；TUI/local/Web 等客户端只订阅 main agent 事件
- 保存 usage、conversation state，并触发异步 memory extraction

### Runner (`internal/runner`)

- 只负责通用 loop：LLM 请求、stream 聚合、tool call、tool result 回填、usage callback、Guard hook、自动 compact
- 支持手动 `Compact` 入口
- `MaxTurns` / `MaxToolCalls` / `RetryPolicy` 是预留口子；默认不限制、不重试
- 不知道 spawn、TUI、session、memory extraction 或 main/subtask 产品语义

### Subtask (`internal/subtask`)

- 系统提示词由 `subtask_system.md` 模板生成，针对具体子任务
- 工具权限由 main 精确授权（subset of 9 tools，不含 Spawn 和 AskUser）
- 模型由 main 在 spawn.model 中显式指定（必填）
- tools 由 main 在 spawn.tools 中显式指定（必填，`[]` 表示纯模型任务；不能包含 `spawn`/`askuser`）
- daemon 校验 model ref 和 tool name
- 有独立的上下文窗口，不继承 main conversation、working memory、active memory、restored conversation state 或 main system prompt
- 数据流单向进入 subtask：`spawn.task`、`spawn.context`、授权 tools、`spawn.input_images` 指定的当前用户图片、自己的 tool results；执行完毕后自动销毁，只把最终结果回传给 main LLM
- usage 记录绑定 main session，不创建独立 session
- 继承全局 Guard policy、blocked/allowed、audit DB；smart review 使用 subtask 自己的 working context；需要用户确认时由 main 事件流负责
- tool call/guard/guard confirm/result 通过 main 事件流转发，使用统一 namespaced tool id 挂到 TUI 子工具行；stream/reasoning 不外显

### 嵌套限制

Subtask 不能创建嵌套 subtask（工具列表不含 Spawn）。

## 上下文管理

### 上下文窗口分配

```
模型上下文窗口 (以 128K 为例):
┌──────────────────────────────────────────────┐
│ System Prompt          尽量短且稳定           │
│   ├── 固定指令                               │
│   ├── AGENTS.md 项目指令 (如存在)             │
│   └── active skill index                      │
├──────────────────────────────────────────────┤
│ 工具 schemas           稳定排序               │
├──────────────────────────────────────────────┤
│ Working Memory         当前会话短期上下文      │
│   ├── compact summary (如已压缩)              │
│   ├── prior conversation                      │
│   ├── Active Memory brief (当前轮背景)         │
│   └── current user message                    │
├──────────────────────────────────────────────┤
│ 当前工具结果            ~20K tokens           │
├──────────────────────────────────────────────┤
│ 模型输出空间            ~4K tokens            │
└──────────────────────────────────────────────┘
```

设计原则：固定 System Prompt 和 tool schemas 尽量稳定；query-based active memory 仍然召回，但注入到最新 user message 之前，作为当前轮背景，避免挡在 prior conversation 前面破坏连续对话前缀。Suna 不把长期会话历史作为上下文来源，只保留当前 working memory、上一轮恢复快照和少量 active memory。

### 何时压缩

```
自动触发条件:
  完整 LLM 请求估算 token 数 > 上下文窗口 × 80%

完整 LLM 请求包括:
  - system prompt
  - active memory / internal context 注入
  - working memory messages
  - tool schemas
  - max output reserve (请求 MaxTokens，默认 8192，由 `model.DefaultMaxTokens` 统一维护)

不触发:
  - 完整请求未超过 80% 安全阈值
  - 手动 compact 且 working memory 不超过最近保留上限

自动压缩发生在每次发起 LLM 请求前，由 runner 统一处理；main 和 subtask 都可以启用。手动 compact 通过 runner 的 `Compact` 入口触发。
```

### 如何压缩

```
压缩对象:
  只压缩 working memory。
  不压缩 system prompt、active memory、tool schemas 或 max output reserve。

自动 compact:
  1. 先构造完整候选 LLM 请求并估算 token。
  2. 超过 80% 安全阈值时，计算 fixed cost:
       system prompt + tool schemas + max output reserve + 非 working 注入消息。
  3. 用剩余预算一次性决定保留多少 recent working messages。
       最多保留最近 10 条；预算不足时保留更少；至少保留最新 1 条。
  4. 将 recent 之前的 working memory prefix 压缩成一条 system summary。
  5. 重建完整请求后再次估算。
       如果仍超过安全阈值，直接返回明确错误，不继续撞 provider context limit。

手动 compact:
  - 默认 prefix compact + 保留最近最多 10 条原始 working messages。
  - 如果 working memory 不超过 10 条，视为 no-op，不报错。

摘要内容:
  压缩摘要只服务当前会话瘦身，不进入长期 user_memory。
  summary 必须保留任务目标、明确约束、当前状态、关键决策、重要工具结果、未完成事项和下一步。
  丢弃长日志、原始输出、重复推理、过期假设和礼貌性填充。
```

关键区别：Suna 不追求完整历史回溯。压缩后的细节可能被丢弃，只有对未来交互有长期价值的偏好、习惯、纠错和约束会进入 active memory。

### 缓存友好

```
不变的内容放前面，变化的内容放后面:

System Prompt + tool schemas + prior conversation → 尽量稳定
Active Memory brief → query-based，放在最新 user message 之前
Current user message / new tool results → 放在尾部

Suna 不默认注入 provider-specific cache_control / prompt_cache_key。
缓存命中依赖服务端策略，但 Suna 保证自然前缀稳定。
```

## 任务拆解策略

不需要专门的 Plan/Todo 工具。任务拆解通过 Spawn + 系统提示词引导实现。

### 拆解判断原则

```
应该拆解:
  - 任务涉及 3 个以上独立子任务
  - 某个子任务适合不同模型处理
  - 子任务之间无强依赖，可并行

不应拆解:
  - 任务简单，直接做更快
  - 子任务之间强依赖，串行更可靠
  - 拆分后协调成本 > 直接做
```

### 长任务模式

```
用户: "重构整个认证模块"

Main: Spawn("分析现有认证模块结构") → 结果: 3 个文件需要改
Main: Spawn("重写 auth middleware") + Spawn("重写 JWT 工具") → 并行
Main: 收到两个结果 → Spawn("跑测试")
Main: 测试通过 → 通知用户完成

全程: Main 的上下文只包含摘要和结果，不会爆炸
      每个 Subtask 的上下文是独立的，互不干扰
```

## 意图层 (Intent) — 远期探索

意图层是 Suna 区别于所有现有 Claw 的潜在创新方向，但 MVP 不实现。完整设计归档在 [10-stateful-entity.md](10-stateful-entity.md)。

核心概念：传统 agent 只有"收到消息→响应"。意图层增加"感知环境→识别意图→主动行动"模式。例如：用户连续 3 次改 .go 后跑测试 → agent 主动问"要不要我自动做？"→ 用户确认后保存 .go 自动触发 go test。

当前状态：归档。Timer/Watcher/Webhook/Stream 触发执行尚未接入当前 agent.Run 主路径。

## 自省能力 — 设计归档

当前实现没有独立的自省/重试控制器。工具失败会作为 tool result 写回 working memory，由下一轮 LLM 自行调整；失败信号也会参与记忆显著性判断。以下快速检查、深度检查和最多 3 次重试策略是目标设计，不是当前已实现行为。

### 快速检查 (确定性，零延迟)

```
工具返回后立即判断:
  - Exec: exit_code != 0 → 失败
  - ReadFile: content 为空且文件应该有内容 → 异常
  - WriteFile: 写入字节数为 0 → 异常
  - WriteHTTP: status 4xx/5xx → 失败
  - Spawn: success=false → subtask 失败

快速检查命中率: ~80% 的失败场景
处理: 直接记录失败记忆 → agent 决定重试或调整
```

### 深度检查 (LLM)

```
触发条件:
  - 快速检查未发现问题，但模型预期和结果明显不符
  - 操作成功但结果看起来不对
  - 连续 2 次重试仍然失败

方式: active_model 判断操作结果是否符合预期
成本: 极低 (短输入)
```

### 自省后的归因

```
失败后的归因:

  是理解错? (LLM 误判用户意图)
    → 向用户确认意图 → 重新执行

  是工具错? (参数错误、路径错误)
    → 修正参数 → 重试

  是模型能力不足?
    → 换模型重试 → 如果配置了更强模型则用，否则向用户求助

  是缺少能力? (同类任务反复失败)
    → 触发 Skill authoring workflow (见 05-capability.md)

  不确定原因?
    → 向用户展示操作和结果 → 请求判断
```

### 重试策略

```
- 最多重试 3 次
- 每次重试前必须修正策略 (不能盲目重试相同操作)
- 第 2 次重试: 修正参数或换思路
- 第 3 次重试: 换模型 (如果可用) 或 AskUser
- 3 次都失败: 记录失败记忆 → 向用户说明 → 放弃该子任务
```

## 系统提示词结构

### 项目级配置

除默认数据目录下的全局 `config.toml` 外（当前默认 `~/.suna/config.toml`），Suna 支持项目级 agent 指令文件：

```
工作目录/
├── AGENTS.md            # 项目级 agent 指令 (自动加载)
└── ...
```

加载规则: 只读取当前工作目录下的 `AGENTS.md`。不读取 `SUNA.md` 或 `.suna/AGENTS.md`。

内容: 纯 Markdown，与主流 agent 的 `AGENTS.md` 项目指令风格兼容。

```
# AGENTS.md 示例

## 项目信息
Go 1.22 + Gin 框架的 REST API 服务

## 代码规范
- 使用 golangci-lint
- 测试用 goconvey 风格
- 错误处理用 fmt.Errorf + %w

## 工具偏好
- 不要用 go mod tidy，手动管理 go.mod
- 测试只跑 go test ./...
```

### 配置层级

```
优先级 (高→低):
  1. 用户消息中的显式指令 (当前对话)
  2. AGENTS.md (项目级)
  3. Active Memory (用户偏好/习惯/纠错)
  4. 默认系统提示词
```

### System Prompt 模板

```
You are Suna, a general-purpose main agent...
Tool calls: include intent...
Delegation: use spawn only for isolated subtasks...
Memory: active memory is lightweight background...
Environment: OS/arch, cwd, active model...
Spawnable models: ...
Project instructions from AGENTS.md: ...
Active Skills: ...
```

环境信息的作用:
  1. 引导 LLM 生成正确的命令 (Windows 用 dir 不用 ls)
  2. 引导 LLM 使用正确的路径分隔符
  3. 避免跨平台命令误操作 (Windows 上 rm -rf 无效但 rmdir /s /q 危险)
  4. 提供工作目录上下文，LLM 生成相对路径时更准确

## Daemon 架构

### 为什么需要 Daemon

核心逻辑与 TUI 进程生命周期耦合会导致三个根本问题：

1. **记忆提取受限** — 进程随时退出，提取被迫同步/阻塞，无法做最优批量策略
2. **后台任务失效** — TUI 关闭后记忆整理、会话状态和正在运行的 agent loop 不应随 UI 退出
3. **状态丢失** — 会话切换、进程崩溃都会丢失未处理的任务

Daemon 模式将核心逻辑与 UI 完全解耦，解决以上所有问题。

### 架构

```
┌───────────────────────────────────────────────────────┐
│  sunad (守护进程，常驻)                                 │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────┐ │
│  │ 输入层    │  │ 记忆层    │  │ 行动层               │ │
│  │ Input    │  │ Memory   │  │ Act                  │ │
│  │          │  │          │  │                      │ │
│  │Protocol  │  │ channel   │  │ Agent Loop           │ │
│  │ TUI      │  │ 批量提取  │  │ 7 tools + 2 built-ins│ │
│  │ Guard    │  │ 去重      │  │ Guard + Router       │ │
│  │ AskUser  │  │ SQLite   │  │ Declarative Skills   │ │
│  └──────────┘  └──────────┘  └──────────────────────┘ │
│                                                         │
│  ┌───────────────────────────────────────────────────┐ │
│  │ Protocol Service + Transports                      │ │
│  │ protocol: schema / Service / EventSink / Transport │ │
│  │ local: Unix Socket / Named Pipe + JSON-RPC framing │ │
│  └───────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────┘
         │              │
    Unix Socket    Named Pipe
    (macOS/Linux)  (Windows)
         │              │
┌────────┴──────────────┴────────┐
│ suna (TUI 客户端)               │
│ Bubble Tea → local client      │
└────────────────────────────────┘
```

### 单二进制多模式

用户下载一个二进制，通过参数决定运行模式：

```bash
suna              # 自动: daemon 未运行 → 后台启动 → 连接 → 进入 TUI
suna              # 自动: daemon 已运行 → 直接连接 → 进入 TUI
suna start        # 后台启动 daemon
suna stop         # 通过 protocol 请求 daemon 优雅退出，失败时走本机进程 fallback
suna status       # 查看 daemon 状态
```

实现方式：`suna start` 是用户可见的后台启动命令。父进程通过 `SUNA_RUN_DAEMON=1 exec.Command(os.Args[0])` 拉起同一个二进制作为 daemon 子进程；这个环境变量是内部实现细节，不出现在 CLI help。

### Daemon 生命周期

```
启动:
  1. CLI 先通过 local transport 调用 daemon.status
  2. protocol 可达 → daemon 活着 → 不启动新的
  3. protocol 不可达 → 后台启动 daemon 进程 → 等待 daemon.status 可用
  4. PID 文件只作为 fallback/debug 信号，不作为首要运行态判断

运行中:
  - 无客户端连接时: 记忆 worker 继续处理 pending memory_queue
  - 有客户端连接: 正常交互
  - agent loop 正在执行: 不接受退出信号

自动退出:
  - 最后一个客户端断开 → 等 30 分钟
  - 30 分钟内无客户端重连 → 优雅退出
  - 当前没有已接入的活跃感知源判断；触发器保留为后续设计
  - 无客户端且满足退出策略 → 退出

手动管理 (通过 CLI，非 TUI 命令):
   suna start        → 后台启动 daemon
   suna status       → 优先通过 daemon.status 显示 PID、运行时间、连接数
   suna stop         → 优先通过 daemon.stop 请求优雅退出；不可达时才使用本机进程 fallback
```

### Protocol / Transport

`protocol` 是 daemon 对外业务协议；`transport` 是具体通信方式。daemon 只持有 `[]protocol.Transport`，不直接依赖 Unix socket、Named Pipe、JSON-RPC、HTTP 或 WebSocket。具体 transport 由入口层组装后注入 daemon，`main` 是当前单二进制的 composition root。

#### Protocol Transport 接口

```go
type Transport interface {
	Name() string
	Mount(ctx context.Context, svc Service) error
	Close(ctx context.Context) error
	ConnectionCount() int
}
```

#### Transport 实现

| Transport | 平台 | 路径 | 安全 | 用途 |
|---|---|---|---|---|
| `transport/local` Unix Domain Socket | macOS/Linux | 默认数据目录下的 `sunad.sock`，当前默认 `~/.suna/sunad.sock` | 文件权限 0600 | 本地 TUI/CLI |
| `transport/local` Named Pipe | Windows | `\\.\pipe\sunad` | DACL 仅创建者 | 本地 TUI/CLI |
| `transport/web` | 远期 | HTTP/WebSocket/SSE | token/TLS/本机策略 | Web UI |

local 两个平台实现都基于 OS 权限保证安全——只有当前用户能连接。平台差异通过 `transport_unix.go` / `transport_windows.go` 的 build tags 编译期隔离。

`transport/local` 同时提供本地 client 侧封装，供 TUI 和 CLI 管理命令复用。TUI 只保留 UI 通知分发和交互包装；CLI 的 `status` / `stop` / 启动等待也通过同一套 local protocol client 调用 `daemon.status` / `daemon.stop`。

#### Protocol 方法

client → daemon (请求-响应):

```
agent.sendMessage    {client_msg_id?, parts[]} → 事件流返回
agent.cancel         {}                    → 中断当前生成
memory.list          {}
skill.list           {}
skill.set            {name, enabled}
session.restore      {}
session.new          {}
session.compact      {}
daemon.status        {}
daemon.stop          {}
config.get           {}
config.set           {action, ...}
agent.askReply       {id, answer}
agent.guardReply     {id, decision}
attachment.status    {}                    → {root, bytes, count}
attachment.clear     {}                    → {root, bytes_removed, count_removed, bytes, count}

预留但当前 server 未路由:
trigger.list/add/remove
```

daemon → client (事件):

```
agent.stream         {chunk, done, usage?}  // LLM 输出；streaming 能力取决于 provider
agent.reasoning      {content}              // reasoning/thinking 内容（provider 支持时）
agent.tool_start     {tool, params}         // 工具开始执行
agent.tool_guard     {tool_call_id, tool, risk, decision, source, reason?, suggestion?} // 工具执行前 Guard 决策来源
agent.tool_end       {tool, result}         // 工具执行完毕
agent.ask_user       {id, question, options?}
agent.guard_confirm  {id, tool, risk, reason, suggestion, params}
memory.list_result   {memories}
session.compact_result {before, after, ...}
daemon.state         {session_id, agent_status, current_task}  // 连接时推送
daemon.full_status   {pid, uptime, provider, model, ...}
config.state         {models, active_model, locale, theme, ...}

预留但当前 runtime 不发送:
perception.event
```

#### Local JSON-RPC framing

local transport 当前使用 NDJSON + JSON-RPC 承载 protocol 请求和事件。JSON-RPC 只是 `transport/local` 的线协议细节，不属于 `protocol`，未来 Web UI 不要求使用 JSON-RPC。

```
格式: NDJSON (每行一条 JSON，\n 分隔)
  {"jsonrpc":"2.0","method":"agent.stream","params":{"chunk":"你","id":"abc"}}
  {"jsonrpc":"2.0","method":"agent.stream","params":{"chunk":"好","id":"abc"}}
  {"jsonrpc":"2.0","method":"agent.stream","params":{"chunk":"。","id":"abc","done":true}}

规范:
  - 每条 JSON 必须单行
  - JSON 内不能有裸换行符 (用 \n 转义)
  - TUI 端按 \n 切分 → 逐条 json.Decode
```

#### 连接管理

```
多连接: Daemon 接受多个连接 (支持多个本地客户端)
  - transport 为每个连接分配 ConnID
  - daemon 保存 ConnID -> EventSink
  - agent 事件通过 protocol.EventSink 推送给对应连接

重连: TUI 崩溃重启后连上 daemon
  - daemon 主动推送 daemon.state (当前会话、agent 状态、最近输出)
  - TUI 据此恢复显示

写阻塞保护:
  - local conn.Send 带 context timeout
  - daemon 对 agent.stream / agent.reasoning 做短周期 micro-batching，减少 IPC/TUI 事件数量
  - 文本 delta 不丢弃；tool/usage/done/error 等关键事件前会先 flush pending 文本
```

OpenAI-compatible SSE:

- OpenAI Chat Completions 和 OpenAI Responses 都走 `openai-go` streaming decoder。
- Suna 注册兼容 `text/event-stream` decoder，跳过 heartbeat/comment-only/empty SSE event，避免空 payload 被当作 JSON 解析。
- OpenAI-compatible header normalizer 不覆盖 `Accept`，只清理 Stainless 追踪头并设置 `User-Agent`。

### 数据目录

默认数据目录由 `internal/config/paths.go` 统一定义。当前默认值是 `~/.suna/`；入口、daemon、transport、media、TUI 和 guard 都应通过 `config.DefaultDataDir()` / `config.Default*Path()` / `Config` 路径方法派生运行路径，不直接拼 `$HOME/.suna`。

```
~/.suna/
├── config.toml
├── credentials.toml    # API keys，权限 0600
├── sunad.pid           # Daemon PID 文件
├── sunad.sock          # Unix Socket (macOS/Linux)
├── memory.db           # SQLite (记忆 + 审计 + 触发器)
├── skills/             # 用户安装/生成的通用 Agent Skills
└── logs/
    └── audit.log
```

## Hooks 系统

Hooks 是用户自定义自动化钩子的目标设计。当前配置结构中保留 hooks 字段，但 core 执行链路尚未接入。Skill 不提供 hook runtime；外部工具/服务接入由 MCP 独立配置。

### Shell Hooks

在 config.toml 中配置，执行 shell 命令，简单快速：

```toml
[[hooks]]
event = "PostToolUse"
tool = "EditFile"
command = "npx prettier --write $FILE"

[[hooks]]
event = "PostToolUse"
tool = "WriteFile"
command = "gofmt -w $FILE"

[[hooks]]
event = "Notification"
command = "osascript -e 'display notification \"$MESSAGE\"'"
```

## I/O 抽象层

agent 内核与 I/O 完全解耦。Daemon 内的 Agent Core 不直接与任何 UI 交互，所有输入输出通过 `protocol.Service` 和 `protocol.EventSink` 传递。

### Daemon 端

```go
type Service interface {
    Handle(ctx context.Context, req Request, sink EventSink) (any, error)
    OnConnect(ctx context.Context, connID string, sink EventSink)
    OnDisconnect(ctx context.Context, connID string)
}

type EventSink interface {
    Emit(ctx context.Context, event Event) error
}
```

Agent 的流式输出、工具状态、AskUser/GuardConfirm 都先转成 `protocol.Event`，再由当前 transport 编码发送。

### TUI 端

```
TUI (Bubble Tea):
  - 启动时连接 daemon (Unix Socket / Named Pipe)
  - 用户输入 → protocol.SendMessageParams(parts) → local JSON-RPC framing → daemon
  - daemon protocol.Event → local JSON-RPC notification → 渲染到终端
  - 只持有 UI 展示/输入状态，不持有业务状态、模型执行或数据库连接
```

### 命令行接口

```
suna                    # 启动 TUI (自动检测/启动 daemon)
suna "帮我分析日志"      # 单次执行模式 (非交互，连 daemon 执行后退出)
suna start              # 后台启动 daemon
suna stop               # 停止 daemon
suna status             # 查看 daemon 状态
suna help               # 查看 CLI 帮助
suna skill list         # 不作为主入口；Skill 主要通过对话和 /skills 管理
suna trigger list       # 预留: 查看感知源
```

## 会话管理

### TUI 交互设计

设计原则：命令数量最小化，常用操作通过键盘快捷键完成。

#### 键盘快捷键

```
Enter                 发送消息
Shift+Enter/Alt+Enter 换行
Esc                   取消当前生成 / 清空输入框 / 返回 Welcome
PgUp/PgDown           viewport 翻页
Ctrl+T                显示工具和 thinking 细节 (toggle，默认隐藏)
Ctrl+Y                copy mode，临时释放鼠标给终端原生选择
? / F1                help overlay
```

#### TUI 命令

```
/new                  新建会话 (清空工作记忆，新 session ID)
/model <name>         切换当前模型
/compact              手动触发上下文压缩
/memory               查看 active memory
/skills               打开 Skill overlay，查看并切换激活状态
/help                 显示帮助
```

#### 隐藏的高级操作

```
拖拽文件到终端       → 自动读取文件内容，作为上下文发送
Ctrl+T toggle        → 显示/隐藏工具调用细节 (默认隐藏，减少视觉噪音)
审计日志             → 作为内部运维数据保留，不通过 memory search 暴露
会话历史             → daemon 自动恢复上次会话，无需用户手动管理
```

注：/verbose, /session, /audit, /file, /think 等命令已移除。功能通过更自然的交互方式覆盖：
- 工具细节默认隐藏，按 Ctrl+T 切换显示
- 文件通过拖拽终端或 agent 自动识别路径读取
- 会话由 daemon 自动管理（恢复/持久化）
- 审计记录不进入 user_memory，避免污染用户画像
- thinking/reasoning 展示取决于 provider 是否返回 reasoning chunk

### Thinking 控制

```
当前没有独立的 thinking depth 用户命令。TUI 可以展示 provider 返回的 reasoning chunk。

当前实现:
- OpenAI-compatible provider 会透传 reasoning_content chunk（如果上游返回）
  - Anthropic provider 当前使用非 streaming Messages.New，暂未映射 thinking blocks
  - 用户可通过 /model 手动切换模型
```

## 用量追踪

```
SQLite 表: usage_log
| session_id | model | input_tokens | output_tokens | created_at |

用量信息在状态栏实时显示 (in/out/cache tokens + 速度)，前提是 provider 返回 usage；SQLite 只持久化 token 用量，不持久化价格或成本。
不单独暴露 /usage 命令，减少命令数量。
```

## 人格定义

Suna 不提供独立的人格定义文件（SOUL.md 已移除）。沟通风格优先通过 active memory 与用户生成的普通 Skill 表达。

用户可以说：

```text
以后和我交流都简洁一点，直接给结论。
把这个写作/沟通偏好保存成一个 Skill。
```

Suna 通过内置 Skill authoring workflow 生成普通 `~/.suna/skills/<name>/SKILL.md`，check 后由用户决定是否启用。
