# 09 — 竞品对比与设计审查

对照 Claude Code、OpenClaw、Codex Desktop 检查 Suna 设计完整性。

> **注**: 基于 Daemon + TUI 双进程三层架构编写（感知/记忆/行动）。意图层归档为远期探索。

## 架构对比

| 维度 | Claude Code | OpenClaw | Suna |
|------|-------------|----------|------|
| **架构模式** | 单进程 TUI | Gateway daemon + TUI/Web | sunad 守护进程 + TUI 前端 (单二进制) |
| **进程模型** | TUI 进程 | Gateway 常驻 + agent 无状态 | Daemon 常驻 (Agent/Memory/Perception 24/7) |
| **IPC** | 内部调用 | HTTP/WebSocket | Unix Socket + Named Pipe (JSON-RPC 2.0) |
| **状态管理** | 进程内 | Gateway 持久化 | Daemon + SQLite (进程间共享) |
| **感知** | 无 | 无 | 4 种感知源 24/7 (Timer/Watcher/Webhook/Stream) |
| **记忆** | auto memory (learnings) | memory_search + workspace files | 4 层记忆 + 仅添加式 + 异步批量 + 时间推理 |

## 功能对比矩阵

| 特性 | Claude Code | OpenClaw | Suna | 差距 |
|------|-------------|----------|------|------|
| **语言** | TypeScript (Node) | TypeScript (Node) | Go | ✅ 差异化优势 |
| **多模型** | 仅 Anthropic | 多模型 + failover | 多模型 + 智能路由 | ✅ Suna 路由更智能 |
| **核心工具** | Read/Write/Edit/Bash/Glob/Grep + MCP | exec/read/write/edit/browser/canvas + MCP | 固定 9 个 + MCP | ⚠️ 缺 glob/grep 原生工具 |
| **权限模型** | allow/deny + yolo 模式 | allow/deny + sandbox + exec approvals | LLM 审查 + 硬规则 + 渐进信任 | ✅ Suna 创新点 |
| **多渠道** | Terminal/VSCode/Desktop/Web/JetBrains | 25+ 消息平台 + macOS/iOS/Android | TUI + (远期 WebSocket) | ❌ I/O 渠道少 |
| **能力系统** | CLAUDE.md + Skills + auto memory | SKILL.md + Plugin (npm) | SKILL.md + JS (QuickJS/WASM) + MCP | ✅ 学习能力是差异化 |
| **记忆** | auto memory (learnings) | memory_search + workspace files | 4 层 + 仅添加式 + 异步批量 + 时间推理 + 多信号检索 | ✅ Suna 领先一代 |
| **有状态** | 无状态循环 | Gateway daemon 但 agent 无状态 | Daemon 常驻 + 感知 24/7 + 异步记忆 | ✅ Suna 领先 |
| **定时任务** | Routines (云端) | cron 工具 | Timer/Watcher/Webhook/Stream | ✅ 已覆盖 |
| **Sub Agent** | Lead + sub agents | subagents + multi-agent routing | Main + Sub (Spawn) | ⚠️ 基本一致 |
| **Hooks** | PreToolUse/PostToolUse/Notification | Plugin hooks | OnSignal/PreLLM/PreToolUse/PostToolUse | ✅ 已覆盖 |
| **长任务** | Goal 命令 (8h+) | cron + sessions | Daemon 常驻 + 感知驱动 | ✅ 已覆盖 |
| **人格** | CLAUDE.md | SOUL.md | capability (persona SKILL.md) | ✅ 统一到能力系统 |
| **TUI 命令** | 丰富 (~15+) | 丰富 | 精简 (5 个命令 + 快捷键) | ✅ 降低学习成本 |
| **Browser** | 无内置 (MCP) | browser 工具 (Chromium) | 无 | ⚠️ MCP 或 skill |
| **搜索** | 无内置 | web_search / web_fetch | ReadHTTP (原始) | ⚠️ skill 覆盖 |
| **Sandbox** | 无 | Docker/SSH sandbox | Guard (LLM 审查) | ⚠️ 不同路线 |

## 已识别差距

### 1. 缺少 Grep/Glob 原生工具

Claude Code 内置 `Grep` 和 `Glob` 工具用于代码搜索。Suna 设计中明确说"能用 Exec 做的事不单独加工具"，但代码搜索是 agent 高频操作。

**决策**: 维持 9 工具设计。Exec 中的只读命令通过 `isReadOnlyCommand` 自动归为 RiskLow，不经过 LLM 审查。

### 2. 缺少 Web 搜索结构化工具

**决策**: MVP 不加。通过 skill 封装搜索能力（如 `web-search/` 能力目录）。

### 3. 缺少 Browser 自动化工具

**决策**: MVP 不加。通过 MCP (Playwright/Puppeteer) 或 skill 覆盖。

### 4. I/O 渠道覆盖不足

**决策**: IPC Transport 抽象层已预留 WebSocket 扩展。Phase 5 探索。

### 5. TUI 命令精简

**决策**: Suna 只有 5 个 TUI 命令 + 键盘快捷键。相比 Claude Code (~15+) 和 OpenClaw (~20+)，学习成本最低。高级功能通过：
- 键盘快捷键 (Ctrl+T 切换工具细节, Ctrl+K 切换模型)
- 自然语言交互 (让 agent 查记忆/管能力)
- 拖拽文件到终端

### 6. 人格统一到能力系统

**决策**: 移除 SOUL.md，人格通过 `~/.suna/capabilities/persona/SKILL.md` 实现。优势：
- 不引入额外概念，统一到能力系统
- 用户可以让 agent 自己创建/调整人格
- LOAD_SKILL 机制自然适用

### 7. 缺少 AGENTS.md 项目级配置

**决策**: 已补充到 01-architecture.md。支持 SUNA.md 和 .suna/AGENTS.md。

### 8. Sandbox 隔离

**决策**: MVP 用 Guard。Phase 5 可选 Docker sandbox。

## 总结

### 设计完备度评估

| 维度 | 评分 | 说明 |
|------|------|------|
| 架构模式 | 9/10 | Daemon + TUI 双进程，IPC JSON-RPC |
| 核心循环 | 9/10 | agent loop + context + compression 完善 |
| 工具系统 | 8/10 | 9 工具合理，Exec 只读命令快速放行 |
| 安全模型 | 9/10 | LLM Guard 是创新，渐进信任有差异化 |
| 能力系统 | 9/10 | 三层能力 + 学习流程完整 |
| 记忆系统 | 9/10 | 4 层 + 仅添加式 + 异步批量 + 时间推理 |
| 感知层 | 9/10 | 4 种感知源覆盖主要异步场景 |
| 多模型路由 | 9/10 | LLM 路由 + strengths 标签 + 缓存 + 降级 |
| TUI 交互 | 8/10 | 命令精简 (5个) + 快捷键，学习成本低 |
| I/O 渠道 | 5/10 | MVP 只有 TUI，IPC Transport 预留 WebSocket |
| 生态/渠道 | 3/10 | 无消息平台、无移动端，远期规划 |

### Phase 5 探索项汇总

- WebSocket Transport (远程客户端: 微信/Telegram/手机)
- Browser 工具或 MCP 封装
- Docker sandbox (可选)
- Web UI
- 语音交互
- 能力市场
- 意图层探索
