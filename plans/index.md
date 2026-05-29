# Suna — 有状态 AI 实体

> Suna (सून्य / śūnya): 梵文"空"。出厂无形，遇缘则生。

## 设计理念

传统 Claw 是无状态循环：收到消息 → 调用 LLM → 执行工具 → 回复 → 等待。每一轮都是独立事件，记忆只是对话历史的附件。

Suna 是**持续运行的有状态实体**：感知环境、积累经验、主动预测、按需行动。

```
传统 Claw:  算命先生（你说一句他答一句，说完就忘）
Suna:       学徒（持续在场，越久越懂你，你不说话他也在观察和学习）
```

核心哲学：**从"聊天 + 工具"到"有状态的 AI 实体"。** 衡量标准不是"能做多少事"，而是"越用越懂你"。

## 定位

```
                专用 ←――――――――――――――→ 通用

Claude Code      ●  (coding 专用)

Codex           ●   (coding 专用)

                  Suna
                       ●  (通用 + 会学习 + 有状态)

OpenClaw                       ●   (通用但无状态、不会学习)
```

差异化目标：**有状态 + 轻量 active memory + Go 单二进制 + 多模型**。主动感知仍是预留设计，当前已落地重点是 daemon 化、agent loop、model routing、Guard 和 active memory。

## Daemon + TUI 双进程架构

Suna 由常驻守护进程 (sunad) 和前端 TUI 客户端组成，单二进制多模式运行。核心逻辑全部在 daemon 中，与 UI 生命周期完全解耦。

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
│  │ Guard (硬规则 + LLM 审查)                             │ │
│  │ Declarative Skill loading                            │ │
│  └──────────────────────────────────────────────────────┘ │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐ │
│  │ Protocol / Transport                                 │ │
│  │ protocol: schema / Service / Transport               │ │
│  │ local: Unix Socket / Named Pipe + JSON-RPC framing    │ │
│  └──────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘

┌──────────────┐  ┌──────────────┐
│ suna (TUI)   │  │ (远期: 其他   │
│              │  │  客户端)      │
│ Bubble Tea   │  │              │
│ Unix Socket  │  │ WebSocket    │
└──────────────┘  └──────────────┘
```

### 为什么需要 Daemon

核心逻辑与 TUI 生命周期耦合会导致三个根本问题：

1. **记忆提取受限** — 进程随时退出，提取被迫同步/阻塞，无法做最优批量策略
2. **后台任务失效** — TUI 关闭后记忆整理、会话状态和正在运行的 agent loop 不应随 UI 退出
3. **状态丢失** — 会话切换、进程崩溃都会丢失未处理的任务

Daemon 解决了这些问题：记忆异步批量提取、状态持久化和 agent 执行不受 UI 影响。感知源 24/7 运行是后续预留设计。

### 单二进制多模式

```bash
suna                    # 自动检测: daemon 未运行 → 启动 daemon → 启动 TUI
suna                    # daemon 已运行 → 只启动 TUI
suna start              # 后台启动 daemon
suna stop               # 停止 daemon
suna status             # 查看 daemon 状态
suna help               # 查看 CLI 帮助
```

单二进制优势不变。类似 `docker` CLI + `dockerd` 的关系，但打包在一起。

### Protocol / Transport 通信

Daemon 只挂载 `protocol.Transport`，当前 local transport 通过 Unix Domain Socket (macOS/Linux) 或 Named Pipe (Windows) 连接本地 TUI，并用 JSON-RPC 作为 local 线协议。未来 Web UI 可新增 Web transport，不要求使用 JSON-RPC。

详见 [01-architecture.md](01-architecture.md) 中"Daemon 架构"和"I/O 抽象层"章节。

## 三层架构 + 意图探索

| 层 | 核心创新 | vs 现有 Claw |
|---|---|---|
| **输入/感知层 Input/Sense** | 当前处理 TUI/local protocol 输入；Timer/Watcher/Webhook/Stream 为预留设计 | 传统 agent 是被动唤醒 |
| **记忆层 Memory** | 轻量 active memory + 异步 full compaction | 传统是线性对话 + 压缩/截断 |
| **行动层 Act** | 7 registry tools + 2 agent built-ins + Guard + declarative skills | 传统每次都走 LLM |

意图层 (Intent) 归档为远期探索方向，MVP 不实现。详见 [10-stateful-entity.md](10-stateful-entity.md) 中"意图层"章节。

## 文档索引

| # | 文档 | 对应层次 | 说明 |
|---|---|---|---|
| 1 | [01-architecture.md](01-architecture.md) | 全局 | Daemon 架构、Agent/Runner/Subtask、Protocol/Transport 通信 |
| 2 | [02-model-router.md](02-model-router.md) | 行动层 | 统一 Provider 接口、智能路由、缓存策略 |
| 3 | [03-tools.md](03-tools.md) | 行动层 | 7 个 registry tools、2 个 agent built-ins、多模态输入 |
| 4 | [04-guard.md](04-guard.md) | 行动层 | 硬规则 + LLM 审查；渐进信任为后续项 |
| 5 | [05-capability.md](05-capability.md) | 记忆层 | 当前 SKILL.md declarative loading；JS/MCP/hooks 为目标设计 |
| 6 | [06-memory.md](06-memory.md) | 记忆层 | 轻量 active memory、异步 full compaction、最近会话恢复 |
| 7 | [07-trigger.md](07-trigger.md) | 感知层 | Timer/Watcher/Webhook/Stream 预留设计；当前未接入 runtime |
| 8 | [08-tech-stack.md](08-tech-stack.md) | — | 技术选型、项目结构、依赖 |
| 9 | [09-competitive-review.md](09-competitive-review.md) | — | 竞品对比 |
| 10 | [10-stateful-entity.md](10-stateful-entity.md) | — | 范式转变的完整思考过程 |
| 12 | [12-tui-design.md](12-tui-design.md) | TUI | TUI 页面、交互与 protocol 展示约束 |
| 13 | [13-tui-stream-performance.md](13-tui-stream-performance.md) | TUI | TUI 流式渲染性能、Markdown 最终渲染和贴底滚动策略 |

## 关键差异化

```
1. 有状态实体 (vs 无状态循环)
   - Daemon 常驻，不只是被唤醒
   - 感知环境 24/7，不依赖 TUI

2. 轻量 Active Memory (vs 线性对话 + 压缩)
   - 只保留用户偏好、习惯、性格、约束和纠错
   - 记忆会刷新、合并、替换、删除
   - 不保存完整历史，不依赖 embedding
   - 异步 full compaction，不阻塞主链路

3. 主动感知 (vs 纯被动，目标设计)
   - 文件变化 / 时间事件 / Webhook / 数据流 4 种感知源
   - 感知信号直接驱动行动层
   - 当前只保留 triggers 表和 protocol 常量预留，runtime 未接入

4. Go 单二进制 + 多模型
   - 无 Node.js/Python 运行时依赖
   - 智能路由，越用越准

5. 缓存友好上下文
   - 固定 system prompt 在前
   - 工具定义稳定排序
   - 动态 active memory 靠近当前用户消息，不挡在 prior conversation 前面
   - 提高 prompt cache 命中

6. 精简交互 (vs 命令爆炸)
   - 当前只有 6 个 TUI 命令 + 少量键盘快捷键
   - 人格统一到能力系统，不引入 SOUL.md
   - 高级功能通过自然语言交互

7. 意图层 (远期探索)
   - 主动预测、习惯学习、意图驱动行动
   - MVP 不实现，归档为探索方向
```

## 技术选型

详见 [08-tech-stack.md](08-tech-stack.md)

## MVP 阶段

| Phase | 内容 | 周期 |
|---|---|---|
| 1 | Daemon 基础 + 记忆层基础：sunad + protocol/local transport + active memory + memory_queue + 9 tool definitions + Guard stub | 5 周 |
| 2 | 行动层完善：Guard 完善 + 多模型路由；渐进信任和感知源仍是后续项 | 4 周 |
| 3 | 记忆深化 + 学习：active memory 质量评估 + declarative SKILL.md；JS/MCP 能力系统后续接入 | 4 周 |
| 4 | 完善 + 扩展：项目配置 (AGENTS.md) + 模型表现追踪 + 最近会话恢复 | 4 周 |
| 5 | 探索：意图层、多 I/O 渠道 (WebSocket)、能力市场、Docker sandbox | — |
