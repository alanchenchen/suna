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
│  │ 感知层    │  │ 记忆层    │                               │
│  │ Sense    │  │ Memory   │                               │
│  │          │  │          │                               │
│  │ 用户消息  │  │ 层次化    │                               │
│  │ 文件变化  │→│ 时间推理  │                               │
│  │ 时间事件  │  │ 异步提取  │                               │
│  │ Webhook  │  │ 实体关联  │                               │
│  │ 数据流   │  │ 多信号检索│                               │
│  └──────────┘  └──────────┘                               │
│         │                              │                  │
│         ▼                              ▼                  │
│  ┌──────────────────────────────────────────────────────┐ │
│  │ 行动层 Act                                            │ │
│  │                                                      │ │
│  │ Agent Loop (LLM 驱动)                                 │ │
│  │ 9 工具 + Skill + MCP                                  │ │
│  │ 多模型路由                                            │ │
│  │ Guard (硬规则 + LLM 审查 + 渐进信任)                   │ │
│  │ Hooks                                                │ │
│  └──────────────────────────────────────────────────────┘ │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐ │
│  │ IPC Server (Transport 抽象)                           │ │
│  │ Unix Socket / Named Pipe / (远期: WebSocket)          │ │
│  │ JSON-RPC 2.0                                         │ │
│  └──────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```

各层文档：
- 感知层 → [07-trigger.md](07-trigger.md) (感知源)
- 记忆层 → [06-memory.md](06-memory.md)
- 行动层 → [03-tools.md](03-tools.md) + [04-guard.md](04-guard.md) + [02-model-router.md](02-model-router.md)

## Agent Loop

行动层的核心循环。输入来自用户消息和感知层信号。

```
┌─────────────────────────────────────────────────────────────┐
│  1. 接收输入                                                  │
│     ├── 用户消息 (直接指令，最高优先级)                        │
│     ├── 感知层信号 (Timer/Watcher/Webhook/Stream)             │
│     └── Sub agent 返回                                       │
│                                                               │
│  2. 构建请求                                                  │
│     ├── System Prompt (固定模板 + 能力提示词 + 用户认知摘要)    │
│     ├── 相关记忆 (多信号检索 top-k，见 06-memory.md)          │
│     ├── 对话历史 (带压缩)                                     │
│     └── 工具定义 (根据 agent 类型动态生成)                     │
│                                                               │
│  3. 调用模型 (streaming，路由选择见 02-model-router.md)       │
│     └── 输出文本 + 可能的 tool_calls                           │
│                                                               │
│  4. 处理输出                                                  │
│     ├── 纯文本 → 流式推送给用户                               │
│     ├── Tool Call → 路由到对应工具执行                         │
│     │   ├── Perceive 工具 → 直接执行                          │
│     │   ├── Act 工具 → 经过 Guard 审查 → 执行                 │
│     │   │   (Exec 中只读命令经 isReadOnlyCommand 快速放行)      │
│     │   └── Spawn 工具 → 创建 sub agent (仅 main)             │
│     └── 工具结果 → 追加到对话历史 → 回到步骤 2                 │
│                                                               │
│  5. 自省检查 (见下文)                                         │
│                                                               │
│  6. 终止条件: 模型不再发起 tool_call + 输出结束                │
│                                                               │
│  7. 记忆提取 (见 06-memory.md)                                │
│     └── 异步: 写入提取队列，daemon 后台 worker 处理             │
│         不阻塞 agent loop，不受 TUI 生命周期影响                │
└─────────────────────────────────────────────────────────────┘
```

### 并发模型

```
Main agent:  单 goroutine，串行处理 tool_calls
             (模型生成是串行的，无法并行)

Sub agents:  每个 sub agent 独立 goroutine
             Main 可以同时发起多个 Spawn → 多个 sub 并行运行
             Main 等待所有 sub 完成后汇总结果
```

## Main / Sub 二分法

只有两种角色，无模式区分。所有"模式"通过工具权限组合实现。

### Main Agent

- 拥有全部 9 个工具的访问权限（包含 Spawn）
- 负责任务理解、拆分、调度、结果汇总
- 管理所有 sub agent 的生命周期
- 系统提示词固定，由 Suna 内核生成
- 可以同时运行多个 sub agent（goroutine 并发）

### Sub Agent

- 系统提示词由 main 动态生成，针对具体子任务
- 工具权限由 main 精确授权（subset of 9 tools，不含 Spawn）
- 模型由 main 根据任务类型指定
- 有独立的上下文窗口，不与 main 共享对话历史
- 执行完毕后自动销毁，结果回传给 main

### 嵌套限制

Sub agent 不能创建 sub-sub-agent（工具列表不含 Spawn）。

## 上下文管理

### 上下文窗口分配

```
模型上下文窗口 (以 128K 为例):
┌──────────────────────────────────────────────┐
│ System Prompt          ~4K tokens            │
│   ├── 固定指令                               │
│   ├── 能力提示词 (按相关性筛选)               │
│   └── 项目配置 (SUNA.md)                     │
├──────────────────────────────────────────────┤
│ 相关记忆               ~100K tokens          │
│   ├── 用户偏好 (语义记忆摘要)                 │
│   ├── 多信号检索 top-k 结果                   │
│   └── 未提取的最近对话原文 (会话切换时)       │
│   注: 压缩后的对话摘要也归入此区块            │
│       不单独分"对话历史"块                    │
├──────────────────────────────────────────────┤
│ 当前工具结果            ~20K tokens           │
├──────────────────────────────────────────────┤
│ 模型输出空间            ~4K tokens            │
└──────────────────────────────────────────────┘
```

设计原则：LLM 只看到两类内容 — System Prompt + 相关记忆。对话历史经过压缩后也以记忆片段形式注入相关记忆区块，不做独立的"对话历史"块。提示词越精简，cache 命中率越高。

### 何时压缩

```
触发条件: 估算 token 数 > 上下文窗口 × 80%

不触发:
  - 刚开始对话
  - sub agent（独立上下文，通常不会太长）
```

### 如何压缩

```
Layer 1: 工具输出截断
  工具返回超过阈值时，只保留前 N 行 + "... (truncated, X lines total)"

Layer 2: 历史消息摘要
  超过 10 轮的部分，调用 active_model 压缩为摘要
  压缩结果以记忆片段形式注入 System Prompt 的"相关记忆"区块

Layer 3: 对话结构保留
  即使压缩了内容，骨架要保留:
  - 用户发起了什么请求
  - agent 做了哪些关键操作
  - 哪些操作成功/失败
  - 当前进展到哪一步
```

关键区别：传统 agent 压缩后信息就丢了。Suna 压缩的只是对话历史中的展示，**原始信息仍保留在情景记忆中**，可通过 `/memory search` 精准检索。

### 缓存友好

```
不变的内容放前面，变化的内容放后面:

System Prompt → 几乎不变 → cache 命中率高
  构建时按固定顺序拼接:
    1. 固定系统指令 (永远不变)
    2. 能力提示词列表 (只在能力变更时变)
    3. 相关记忆 (含用户偏好 + 检索记忆 + 压缩摘要)
    4. 当前消息 (最新)

只有两部分：System Prompt + 相关记忆，结构最简。
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
      每个 Sub 的上下文是独立的，互不干扰
```

## 意图层 (Intent) — 远期探索

意图层是 Suna 区别于所有现有 Claw 的潜在创新方向，但 MVP 不实现。完整设计归档在 [10-stateful-entity.md](10-stateful-entity.md)。

核心概念：传统 agent 只有"收到消息→响应"。意图层增加"感知环境→识别意图→主动行动"模式。例如：用户连续 3 次改 .go 后跑测试 → agent 主动问"要不要我自动做？"→ 用户确认后保存 .go 自动触发 go test。

当前状态：归档。感知层的信号直接驱动行动层（通过 agent.Run），不经过意图层。

## 自省能力

自省是 agent loop 的内置行为，不是工具。每次工具执行后自动触发。

### 快速检查 (确定性，零延迟)

```
工具返回后立即判断:
  - Exec: exit_code != 0 → 失败
  - ReadFile: content 为空且文件应该有内容 → 异常
  - WriteFile: 写入字节数为 0 → 异常
  - WriteHTTP: status 4xx/5xx → 失败
  - Spawn: success=false → sub agent 失败

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
    → 触发能力学习流程 (见 05-capability.md)

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

除全局 `~/.suna/config.toml` 外，Suna 支持项目级配置文件：

```
工作目录/
├── SUNA.md              # 项目级 agent 指令 (自动加载)
├── .suna/
│   └── AGENTS.md        # 等效 (二选一)
└── ...
```

加载优先级: SUNA.md > .suna/AGENTS.md

内容: 纯 Markdown，与 CLAUDE.md / OpenClaw AGENTS.md 格式兼容

```
# SUNA.md 示例

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
  2. SUNA.md / .suna/AGENTS.md (项目级)
  3. 语义记忆 (用户偏好)
  4. 默认系统提示词
```

### System Prompt 模板

```
你是 Suna，一个通用 AI agent。

## 身份
你是一个智能助手，能够通过工具感知和改变环境。

## 工作方式
- 优先使用已有能力（见下方能力列表）完成任务
- 遇到不确定的操作，先询问用户
- 操作失败时，分析原因并调整策略

## 工具使用原则
- Perceive 工具不需要确认，可以直接使用
- Act 工具会经过安全审查
- 复杂任务应该拆解为子任务并行处理
- 不要重复执行已经成功的操作

## 环境
操作系统: {{ runtime.GOOS }}/{{ runtime.GOARCH }}
Shell: {{ 自动检测: bash / powershell / cmd }}
路径分隔符: {{ os.PathSeparator }}
当前用户: {{ os.Username }}
工作目录: {{ os.Getwd }}
当前时间: {{ time.Now }}

注意: 使用当前操作系统兼容的命令和路径格式。

{{ 项目配置 (SUNA.md 内容，如有) }}

## 相关记忆
{{ 用户偏好 (语义记忆摘要) }}
{{ 检索到的情景记忆 (多信号检索 top-k) }}
{{ 压缩后的对话摘要 (如有) }}

## 当前能力
{{ 能力摘要列表 (每个能力前200字)，LLM 按需加载完整 SKILL.md }}
```

注：对话历史不作为独立区块。压缩后的对话摘要以记忆片段形式归入"相关记忆"。提示词只有两部分：固定指令 + 相关记忆，结构最简，cache 最友好。

### System Prompt 模板

```
你是 Suna，一个通用 AI agent。

## 身份
你是一个智能助手，能够通过工具感知和改变环境。

## 工作方式
- 优先使用已有能力（见下方能力列表）完成任务
- 遇到不确定的操作，先询问用户
- 操作失败时，分析原因并调整策略

## 工具使用原则
- Perceive 工具不需要确认，可以直接使用
- Act 工具会经过安全审查
- 复杂任务应该拆解为子任务并行处理
- 不要重复执行已经成功的操作

## 当前能力
{{ 能力摘要列表 (每个能力前200字)，LLM 按需加载完整 SKILL.md }}

## 用户偏好
{{ 从语义记忆中检索 }}

## 环境
操作系统: {{ runtime.GOOS }}/{{ runtime.GOARCH }}
Shell: {{ 自动检测: bash / powershell / cmd }}
路径分隔符: {{ os.PathSeparator }}
当前用户: {{ os.Username }}
工作目录: {{ os.Getwd() }}
当前时间: {{ time.Now }}

注意: 使用当前操作系统兼容的命令和路径格式。
```

环境信息的作用:
  1. 引导 LLM 生成正确的命令 (Windows 用 dir 不用 ls)
  2. 引导 LLM 使用正确的路径分隔符
  3. 避免跨平台命令误操作 (Windows 上 rm -rf 无效但 rmdir /s /q 危险)
  4. 提供工作目录上下文，LLM 生成相对路径时更准确
```

## Daemon 架构

### 为什么需要 Daemon

核心逻辑与 TUI 进程生命周期耦合会导致三个根本问题：

1. **记忆提取受限** — 进程随时退出，提取被迫同步/阻塞，无法做最优批量策略
2. **感知层失效** — TUI 关闭后 Timer/Watcher/Webhook 全部停止，"持续运行"是空话
3. **状态丢失** — 会话切换、进程崩溃都会丢失未处理的任务

Daemon 模式将核心逻辑与 UI 完全解耦，解决以上所有问题。

### 架构

```
┌───────────────────────────────────────────────────────┐
│  sunad (守护进程，常驻)                                 │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────┐ │
│  │ 感知层    │  │ 记忆层    │  │ 行动层               │ │
│  │ Sense    │  │ Memory   │  │ Act                  │ │
│  │          │  │          │  │                      │ │
│  │ Timer    │  │ channel   │  │ Agent Loop           │ │
│  │ Watcher  │  │ 批量提取  │  │ 9 工具 + Skill + MCP │ │
│  │ Webhook  │  │ 去重      │  │ Guard + Router       │ │
│  │ Stream   │  │ SQLite   │  │                      │ │
│  └──────────┘  └──────────┘  └──────────────────────┘ │
│                                                         │
│  ┌───────────────────────────────────────────────────┐ │
│  │ IPC Server                                        │ │
│  │ Transport 抽象: Unix Socket / Named Pipe / (远期)  │ │
│  │ 协议: JSON-RPC 2.0                                │ │
│  └───────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────┘
         │              │
    Unix Socket    Named Pipe
    (macOS/Linux)  (Windows)
         │              │
┌────────┴──────────────┴────────┐
│ suna (TUI 客户端)               │
│ Bubble Tea → IPC Client        │
└────────────────────────────────┘
```

### 单二进制多模式

用户下载一个二进制，通过参数决定运行模式：

```bash
suna              # 自动: daemon 未运行 → 后台启动 → 连接 → 进入 TUI
suna              # 自动: daemon 已运行 → 直接连接 → 进入 TUI
suna daemon       # 前台启动 daemon (给 systemd/launchd 用)
suna stop         # 发送 SIGTERM 给 daemon
suna status       # 查看 daemon 状态
```

实现方式：`suna` 启动时 `exec.Command(os.Args[0], "--serve")` 后台拉起自身作为 daemon。

### Daemon 生命周期

```
启动:
  1. 检查 socket/pid 文件
  2. 尝试连接 → 连上了 → daemon 活着 → 不启动新的
  3. 连不上 → 删除残留 → 创建新 socket → 启动 daemon

运行中:
  - 无客户端连接时: 感知源继续工作，记忆 worker 继续处理
  - 有客户端连接: 正常交互
  - agent loop 正在执行: 不接受退出信号

自动退出:
  - 最后一个客户端断开 → 等 30 分钟
  - 30 分钟内无客户端重连 → 优雅退出
  - 但有活跃感知源 (已注册的 Timer/Webhook 等) → 不退出
  - 无活跃感知源也无客户端 → 退出

手动管理 (通过 CLI，非 TUI 命令):
   suna status       → 显示 PID, 运行时间, 连接数, 感知源数
   suna stop         → 请求优雅退出 (等任务完成)
```

### IPC 协议

Transport 层抽象，JSON-RPC 2.0 作为唯一协议。

#### Transport 接口

```go
type Transport interface {
    Listen(ctx context.Context) error
    Close() error
    OnConnect(func(Conn))
}

type Conn interface {
    Send(ctx context.Context, msg Message) error
    Receive() (Message, error)
    Close() error
}
```

#### Transport 实现

| Transport | 平台 | 路径 | 安全 | 用途 |
|---|---|---|---|---|
| Unix Domain Socket | macOS/Linux | `~/.suna/sunad.sock` | 文件权限 0600 | 默认 |
| Named Pipe | Windows | `\\.\pipe\sunad` | DACL 仅创建者 | 默认 |

两个实现都基于文件系统权限保证安全——只有当前 OS 用户能连接，无需 token/TLS。

#### JSON-RPC 方法

TUI → Daemon (请求-响应):

```
agent.sendMessage    {content}             → 流式 notification 返回
agent.cancel         {}                    → 中断当前生成
memory.search        {query, top_k}
memory.facts         {}
trigger.list         {}
trigger.add          {type, config}
trigger.remove       {id}
trigger.pause        {id}
trigger.resume       {id}
daemon.status        {}
daemon.stop          {}
config.get           {key}
config.set           {key, value}
skill.list           {}
skill.validate       {name}
```

Daemon → TUI (通知，无 ID):

```
agent.stream         {chunk, done}          // LLM 流式输出
agent.tool_start     {tool, params}         // 工具开始执行
agent.tool_end       {tool, result}         // 工具执行完毕
perception.event     {source_id, content}   // 感知事件
memory.updated       {count}                // 记忆提取完成
daemon.state         {session_id, agent_status, current_task}  // 连接时推送
```

#### Streaming

LLM 输出的流式推送通过 JSON-RPC notification 实现：

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
多连接: Daemon 接受多个连接 (支持多 TUI 实例)
  - agent.stream → 推送到发起请求的 Conn
  - perception.event → 广播到所有 Conn
  - 每个 Conn 有唯一 ID

重连: TUI 崩溃重启后连上 daemon
  - daemon 主动推送 daemon.state (当前会话、agent 状态、最近输出)
  - TUI 据此恢复显示

写阻塞保护:
  - Conn.Send 带 context timeout
  - 客户端渲染太慢 → 跳过旧的 stream chunk → 只发最新的
```

### 数据目录

```
~/.suna/
├── config.toml
├── credentials.toml    # API keys，权限 0600
├── sunad.pid           # Daemon PID 文件
├── sunad.sock          # Unix Socket (macOS/Linux)
├── memory.db           # SQLite (记忆 + 审计 + 触发器)
├── capabilities/       # 程序记忆
└── logs/
    └── audit.log
```

## Hooks 系统

Hooks 是用户自定义的自动化钩子。与 Guard 不同——Guard 是安全审查，Hooks 是用户自定义流程。

Hooks 有两个来源，执行顺序：Shell hooks（config.toml）→ Skill hooks（main.js，见 05-capability.md）。任一 hook 返回 reject → 立即停止。

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

### Skill Hooks

在 skill 的 main.js 中声明（见 05-capability.md lifecycle hooks），执行 JS 函数，可以访问 agent 上下文（工具参数、host 函数），能力更强。支持 4 个 hook 点：OnSignal / PreLLM / PreToolUse / PostToolUse。

## I/O 抽象层

agent 内核与 I/O 完全解耦。Daemon 内的 Agent Core 不直接与任何 UI 交互，所有输入输出通过 IPC 传递。

### Daemon 端

```go
type ClientConn interface {
    Send(ctx context.Context, msg Message) error
    ID() string
}

type AgentCore struct {
    // Agent 不持有 UI 引用，只持有 ClientConn
    // agent 输出 → conn.Send → IPC → TUI 渲染
    // 用户输入 → IPC → daemon handler → agent.Run
}
```

Agent 的流式输出、工具状态、感知事件都通过 `conn.Send` 推送到 TUI。

### TUI 端

```
TUI (Bubble Tea):
  - 启动时连接 daemon (Unix Socket / Named Pipe)
  - 用户输入 → JSON-RPC request → daemon
  - daemon notification → 渲染到终端
  - 不持有任何业务逻辑、状态、数据库连接
```

### 命令行接口

```
suna                    # 启动 TUI (自动检测/启动 daemon)
suna "帮我分析日志"      # 单次执行模式 (非交互，连 daemon 执行后退出)
suna daemon             # 前台启动 daemon
suna stop               # 停止 daemon
suna status             # 查看 daemon 状态
suna skill list         # 查看能力 (直接查询 daemon)
suna trigger list       # 查看感知源 (直接查询 daemon)
```

## 会话管理

### TUI 交互设计

设计原则：命令数量最小化，常用操作通过键盘快捷键完成。

#### 键盘快捷键

```
Enter           发送消息
Alt+Enter       换行
Esc             取消当前生成 / 清空输入框
Ctrl+U          向上翻页
Ctrl+D          向下翻页
Ctrl+N          新建会话 (等同 /new)
Ctrl+T          显示工具调用细节 (toggle，默认隐藏)
Ctrl+K          切换模型
```

#### TUI 命令 (只有 5 个)

```
/new                  新建会话 (清空工作记忆，新 session ID)
/model <name>         切换当前模型
/compact              手动触发上下文压缩
/memory search <q>    搜索情景记忆
/help                 显示帮助
```

#### 隐藏的高级操作

```
拖拽文件到终端       → 自动读取文件内容，作为上下文发送
Ctrl+T toggle        → 显示/隐藏工具调用细节 (默认隐藏，减少视觉噪音)
审计日志             → 通过 /memory search "audit" 间接查询，不单独暴露命令
会话历史             → daemon 自动恢复上次会话，无需用户手动管理
```

注：/verbose, /session, /audit, /file, /think 等命令已移除。功能通过更自然的交互方式覆盖：
- 工具细节默认隐藏，按 Ctrl+T 切换显示
- 文件通过拖拽终端或 agent 自动识别路径读取
- 会话由 daemon 自动管理（恢复/持久化）
- 审计记录存于记忆中，可通过搜索查询
- 思考深度通过模型路由自动决定

### Thinking 控制

```
思考深度通过模型路由自动决定，无用户命令。

实现:
  - 路由基于 strengths 偏好标签，LLM 判断任务适合哪个模型
  - active_model 是默认 fallback
  - 用户可通过 /model 手动切换模型

  OpenAI:     reasoning_effort parameter
  Anthropic:  thinking 参数 + budget_tokens
  其他模型:   通过系统提示词引导
```

## 成本追踪

```
SQLite 表: usage_log
| session_id | model | input_tokens | output_tokens | cost | created_at |

用量信息在状态栏实时显示 (in/out/cache tokens + 速度)。
不单独暴露 /usage 命令，减少命令数量。
```

## 人格定义

Suna 不提供独立的人格定义文件（SOUL.md 已移除）。人格/沟通风格通过 capability 系统实现：

```
~/.suna/capabilities/persona/
  └── SKILL.md

  示例内容:
    # 沟通风格
    - 简洁直接，不说废话
    - 用中文交流
    - 不要用 emoji
    - 给结论，不要给选项

  用户可以随时让 agent 创建/修改这个 capability
  "帮我生成一个能力，定义你的沟通风格"
```

优势：
- 人格定义和能力系统统一，不引入额外概念
- 用户可以让 agent 自己学习和调整人格
- capability 的加载机制 (LOAD_SKILL) 自然适用于人格
