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

差异化：**有状态 + 仅添加式记忆 + 主动感知 + Go 单二进制 + 多模型**。

## Daemon + TUI 双进程架构

Suna 由常驻守护进程 (sunad) 和前端 TUI 客户端组成，单二进制多模式运行。核心逻辑全部在 daemon 中，与 UI 生命周期完全解耦。

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
│  │ IPC Server                                           │ │
│  │ Unix Socket (默认) / Named Pipe (Windows)             │ │
│  │ JSON-RPC 2.0                                         │ │
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
2. **感知层失效** — TUI 关闭后 Timer/Watcher/Webhook 全部停止，"持续运行"是空话
3. **状态丢失** — 会话切换、进程崩溃都会丢失未处理的任务

Daemon 解决了这些问题：感知源 24/7 运行、记忆异步批量提取、状态持久化不受 UI 影响。

### 单二进制多模式

```bash
suna                    # 自动检测: daemon 未运行 → 启动 daemon → 启动 TUI
suna                    # daemon 已运行 → 只启动 TUI
suna daemon             # 仅启动 daemon（前台，给 systemd/launchd 用）
suna stop               # 停止 daemon
suna status             # 查看 daemon 状态
```

单二进制优势不变。类似 `docker` CLI + `dockerd` 的关系，但打包在一起。

### IPC 通信

Daemon 和 TUI 之间通过 Unix Domain Socket (macOS/Linux) 或 Named Pipe (Windows) 通信，使用 JSON-RPC 2.0 协议。Transport 层抽象确保未来可扩展新的通信方式（如 WebSocket）。

详见 [01-architecture.md](01-architecture.md) 中"Daemon 架构"和"I/O 抽象层"章节。

## 三层架构 + 意图探索

| 层 | 核心创新 | vs 现有 Claw |
|---|---|---|
| **感知层 Sense** | 持续监听环境，24/7 运行，不依赖 TUI | 传统 agent 是被动唤醒 |
| **记忆层 Memory** | 仅添加式提取 + 时间推理 + 异步批量 | 传统是线性对话 + 压缩/截断 |
| **行动层 Act** | 9 工具 + Skill + MCP + Guard | 传统每次都走 LLM |

意图层 (Intent) 归档为远期探索方向，MVP 不实现。详见 [10-stateful-entity.md](10-stateful-entity.md) 中"意图层"章节。

## 文档索引

| # | 文档 | 对应层次 | 说明 |
|---|---|---|---|
| 1 | [01-architecture.md](01-architecture.md) | 全局 | Daemon 架构、Agent Loop、Main/Sub、IPC 通信 |
| 2 | [02-model-router.md](02-model-router.md) | 行动层 | 统一 Provider 接口、智能路由、缓存策略 |
| 3 | [03-tools.md](03-tools.md) | 行动层 | 固定 9 个工具、多模态输入 |
| 4 | [04-guard.md](04-guard.md) | 行动层 | 硬规则 + LLM 审查 + 渐进信任 |
| 5 | [05-capability.md](05-capability.md) | 记忆层 | 程序记忆：SKILL.md + JS + MCP + 主动学习 |
| 6 | [06-memory.md](06-memory.md) | 记忆层 | 4 层记忆、异步批量提取、实体关联、多信号检索、时间推理 |
| 7 | [07-trigger.md](07-trigger.md) | 感知层 | 4 种感知源：Timer/Watcher/Webhook/Stream，24/7 运行 |
| 8 | [08-tech-stack.md](08-tech-stack.md) | — | 技术选型、项目结构、依赖 |
| 9 | [09-competitive-review.md](09-competitive-review.md) | — | 竞品对比 |
| 10 | [10-stateful-entity.md](10-stateful-entity.md) | — | 范式转变的完整思考过程 |

## 关键差异化

```
1. 有状态实体 (vs 无状态循环)
   - Daemon 常驻，不只是被唤醒
   - 感知环境 24/7，不依赖 TUI

2. 仅添加式记忆 + 时间推理 (vs 线性对话 + 压缩)
   - 新旧事实共存，不做覆盖/删除
   - 理解时间线因果关系
   - Agent 生成的信息也是第一类的
   - 异步批量提取，不受 UI 生命周期约束

3. 主动感知 (vs 纯被动)
   - 文件变化 / 时间事件 / Webhook / 数据流 4 种感知源
   - 感知信号直接驱动行动层
   - Daemon 常驻，感知源 24/7 运行

4. Go 单二进制 + 多模型
   - 无 Node.js/Python 运行时依赖
   - 智能路由，越用越准

5. 零膨胀向量检索
   - SQLite BLOB + 暴力搜索
   - 不引入向量数据库

6. 精简交互 (vs 命令爆炸)
   - 只有 5 个 TUI 命令 + 键盘快捷键
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
| 1 | Daemon 基础 + 记忆层基础：sunad + IPC + 仅添加式记忆 + FTS5 + embedding 自动发现 + 9 工具 + Guard stub | 5 周 |
| 2 | 行动层完善：Guard 完善 + 渐进信任 + 多模型路由 + 感知源 (Timer/Watcher/Webhook/Stream) | 4 周 |
| 3 | 记忆深化 + 学习：实体关联 + 时间推理 + 程序记忆 (skill 学习) + 能力系统 (SKILL.md + JS + MCP) | 4 周 |
| 4 | 完善 + 扩展：项目配置 (SUNA.md) + 模型表现追踪 + 会话持久化 | 4 周 |
| 5 | 探索：意图层、多 I/O 渠道 (WebSocket)、能力市场、Docker sandbox | — |
