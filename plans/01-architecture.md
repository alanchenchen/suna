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
│  Entity (持续运行的有状态进程)                              │
│                                                            │
│  ┌──────────┐  ┌──────────┐                               │
│  │ 感知层    │  │ 记忆层    │                               │
│  │ Sense    │  │ Memory   │                               │
│  │          │  │          │                               │
│  │ 用户消息  │  │ 层次化    │                               │
│  │ 文件变化  │→│ 时间推理  │                               │
│  │ 时间事件  │  │ 自动提取  │                               │
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
│  │ 通信层 IO                                             │ │
│  │ TUI / CLI / (远期: 消息平台)                           │ │
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
│     └── 每轮交互后自动提取，不等任务完成                        │
│         从本轮交互中提取记忆片段 → 写入情景记忆                │
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
│   └── 用户认知摘要                           │
├──────────────────────────────────────────────┤
│ 相关记忆                ~8K tokens           │
│   └── 多信号检索 top-k 结果                   │
├──────────────────────────────────────────────┤
│ 对话历史               ~92K tokens           │
│   ├── 近 N 轮完整保留                        │
│   └── 更早的对话 → 压缩为摘要                │
├──────────────────────────────────────────────┤
│ 当前工具结果            ~20K tokens           │
├──────────────────────────────────────────────┤
│ 模型输出空间            ~4K tokens            │
└──────────────────────────────────────────────┘
```

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
  超过 10 轮的部分，调用 fast 模型压缩为摘要

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
    3. 用户认知摘要 (很少变)
    4. 相关记忆 (每轮变化，但位置靠后)
    5. 对话历史 (每轮追加)
    6. 当前消息 (最新)
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

方式: fast 模型判断操作结果是否符合预期
成本: 极低 (fast 模型 + 短输入)
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
  3. SOUL.md (人格)
  4. 语义记忆 (用户偏好)
  5. 默认系统提示词
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

## 运行模式

Suna 以 TUI 进程运行，无 daemon 模式。感知源（Timer/Watcher/Webhook/Stream）在 TUI 进程内工作：TUI 打开时感知源活跃，TUI 关闭时进程退出、感知源停止。

远期如需后台运行，可通过 `suna &` 或 systemd user service 实现，不需要特殊的 daemon 架构。

### 数据目录

```
~/.suna/
├── config.toml
├── memory.db          # SQLite (情景记忆 + 语义记忆 + 实体索引 + 向量)
├── capabilities/      # 程序记忆
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

agent 内核与 I/O 完全解耦。

### 接口

```go
type IO interface {
    Send(event Event) error
    Receive() (<-chan UserInput, error)
}

type UserInput struct {
    Content []ContentBlock
    Source  string          // "tui" / "cli" / "webhook" / "sense"
}
```

### 实现方

```
TUI:       bubbletea 实现 IO 接口 → 终端渲染
CLI:       suna "xxx" → 单次执行模式
Webhook:   HTTP 请求 → UserInput (远期)
(远期) Web UI / 消息平台
```

### 命令行接口

```
suna                    # 启动 TUI
suna "帮我分析日志"      # 单次执行模式 (非交互)
suna skill list         # 查看能力
suna trigger list       # 查看感知源
```

## 会话管理

### TUI 内命令

```
/compact              手动触发上下文压缩
/new                  新建会话
/reset                重置当前会话 (清空工作记忆)
/usage                查看 token 用量
/verbose on|off       显示工具调用细节
/think low|medium|high 控制思考深度
/model <name>         切换当前模型
/session save "备注"   手动保存会话
/session list         查看历史会话
/audit                查看审计日志
/skill                能力管理
/intent               意图管理 (远期)
/trigger              感知源管理
/file <path>          添加文件（图片/音频等）到对话
/memory               记忆管理
```

### Thinking 控制

```
通过 /think 命令或 Spawn 参数控制:

low:    快速响应，不深度思考
medium: 默认，适度思考
high:   深度思考，适合复杂推理任务

实现:
  OpenAI:     reasoning_effort parameter
  Anthropic:  thinking 参数 + budget_tokens
  其他模型:   通过系统提示词引导

路由层配合:
  /think high → 优先路由到 reasoning 模型
  /think low  → 优先路由到 fast 模型
```

## 成本追踪

```
SQLite 表: usage_log
| session_id | model | input_tokens | output_tokens | cost | created_at |

/usage 命令输出:
┌──────────────────────────────────────┐
│ 今日用量                              │
│                                      │
│ 模型         输入      输出    费用    │
│ glm-4        12.5K    3.2K   ¥0.05  │
│ kimi         8.1K     2.1K   ¥0.03  │
│ claude       45.2K    12.1K  ¥0.85  │
│ ───────────────────────────────────  │
│ 合计         65.8K    17.4K  ¥0.93  │
│                                      │
│ 本周合计: ¥4.52                       │
│ 本月合计: ¥18.30                      │
└──────────────────────────────────────┘
```

## 人格定义

### SOUL.md

```
~/.suna/SOUL.md (可选)

作用: 定义 agent 的沟通风格和人格特质
格式: 纯 Markdown

示例:
──────────────────────────────────
# 人格

你是我的私人助手。

## 沟通风格
- 简洁直接，不说废话
- 用中文交流
- 不要用 emoji
- 给结论，不要给选项

## 专业领域
- 我是一名医生
- 医疗相关的回答要严谨，标注引用来源
- 非医疗领域可以随意一些

## 禁忌
- 不要替我做临床决策
- 不要引用过时的医学文献
──────────────────────────────────

加载: SOUL.md 内容注入 system prompt 的身份部分
优先级: SOUL.md > 语义记忆 > 默认提示词
```
