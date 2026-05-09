# Suna 开发进度清单

> 最后更新: 2026-05-08
> 对应设计文档: plans/01~11
> 当前阶段: **Phase 1 收尾**

## Phase 状态总览

| Phase | 内容 | 状态 | 完成度 |
|---|---|---|---|
| Phase 1 | Daemon + 记忆 + 9工具 + Guard stub | 收尾中 | ~85% |
| Phase 2 | Guard LLM review + 感知源 + 渐进信任 | 未开始 | 0% |
| Phase 3 | QuickJS + MCP + Skill 学习 | 未开始 | 0% |
| Phase 4 | 模型表现追踪 + 完善 | 未开始 | 0% |
| Phase 5 | 意图层 + WebSocket + Docker | 未开始 | 0% |

---

## Phase 1 已实现清单

### Daemon + TUI 双进程 (01-architecture)

- [x] Daemon 常驻进程 (`internal/daemon/daemon.go`)
- [x] PID 文件 + Socket 文件管理
- [x] 30 分钟空闲自动退出 (`lifecycle.go`)
- [x] 信号处理 (SIGTERM/SIGINT)
- [x] 单二进制多模式: `suna`, `suna daemon`, `suna stop`, `suna status` (`main.go`)
- [x] Setup Wizard (provider/model/apikey 引导) (`tui/app.go`)
- [x] TUI 纯前端，无业务逻辑

### IPC 通信 (01-architecture)

- [x] JSON-RPC 2.0 over NDJSON (`internal/ipc/`)
- [x] Transport 抽象接口 (Unix Socket / Named Pipe)
- [x] 跨平台 Socket: `_unix.go` + `_windows.go`
- [x] TUI IPC Client 在 `tui/` 包内 (包独立性)
- [x] Daemon State 初始推送 (provider/model 名称)
- [x] 流式通知: stream, reasoning, tool_start, tool_end, ask_user
- [x] 通知式结果: compact_result, memory_search_result
- [x] AskUser 跨请求协调 (pending asks map)

### Agent Loop (01-architecture)

- [x] 完整循环: route → system prompt → LLM → output → memory extraction
- [x] Main/Sub agent (Spawn, 嵌套深度限制)
- [x] System prompt 模板渲染 (`prompt/loader.go`, `templates/system.md`)
- [x] 环境信息注入 (OS/Arch/WorkDir/User/Time)
- [x] SUNA.md / .suna/AGENTS.md 项目配置加载
- [x] Capability 注入 (summary list + [LOAD_SKILL] 动态加载)
- [x] 上下文压缩 (80% 阈值, 10 轮保留, LLM 摘要)
- [x] 流式超时保护 (120s)
- [x] 可取消 Run (CancelCurrentRun)
- [x] SOUL.md 已移除 (人格统一到 capability)

### 多模型路由 (02-model-router)

- [x] Provider 接口: Complete, EstimateTokens, ContextWindow, SupportsEmbedding, Embed
- [x] OpenAI Provider (覆盖所有 OpenAI 兼容 API)
- [x] Anthropic Provider
- [x] 统一消息格式: CompletionRequest, Chunk, Message, ToolCall
- [x] Tool calling 跨 Provider 转换
- [x] LLM 路由 (RouteWithLLM, 基于 strengths 偏好标签选择模型)
- [x] Embedding 自动发现 (HTTP probe `/v1/embeddings`)
- [x] 已知 Provider embedding model 映射 (Zhipu/OpenAI/DashScope)
- [x] Router.EmbeddingProvider() 方法
- [x] Token 估算 (CJK 支持)

### 9 工具 (03-tools)

- [x] ReadFile (敏感文件保护)
- [x] ListDir
- [x] ReadHTTP
- [x] Exec (跨平台 shell: `_unix.go` / `_windows.go`)
- [x] WriteFile
- [x] EditFile
- [x] WriteHTTP
- [x] AskUser (动态追加, 不在 registry)
- [x] Spawn (动态追加, 子 agent 有超时/工具集限制)
- [x] 工具分类: Perceive (无 Guard) / Act (过 Guard) / Communicate
- [x] API Key 脱敏 (`sensitive.go:MaskSensitiveContent`)

### Guard (04-guard)

- [x] 四阶段 Stub: 硬规则 → 风险评估 → auto-approve → 审计日志
- [x] 跨平台硬规则 (`rules_unix.go`, `rules_windows.go`)
- [x] 通用规则 (curl|sh, wget|sh, eval$())
- [x] 风险等级: Low / Medium / High
- [x] isReadOnlyCommand 白名单 (ls, cat, grep, find, git read-only...)
- [x] Pipe 检测 (所有子命令必须 read-only)
- [x] 审计日志 (SQLite audit_log)
- [x] 用户自定义 blocked/allowed 规则 (NewGuardWithConfig)
- [x] 敏感文件保护 (`sensitive.go:IsSensitivePath`)

### 记忆层 (06-memory)

- [x] 4 层框架: Working / Episodic / Semantic / Procedural(filesystem)
- [x] 仅添加式提取 (INSERT only, 无 UPDATE/DELETE)
- [x] 显著性过滤 (rule-based, 零 LLM 成本) (`significance.go`)
- [x] Memory Worker 独立 goroutine 批量处理 (`worker.go`)
- [x] 单次 LLM 调用产出 episodes + facts + entities
- [x] FTS5 全文搜索 + CJK LIKE 降级 (`episodic.go:SearchFTS`)
- [x] 向量搜索 (brute-force cosine similarity) (`episodic.go:SearchByEmbedding`)
- [x] 时间衰减评分 (7d=1.0, 30d=0.8, 90d=0.5, >90d=0.3)
- [x] 4K token budget (`episodic.go:Recall`)
- [x] Query Rewrite 基础设施 (`episodic.go:SearchWithRewrite`, <3 结果时触发)
- [x] 上下文压缩 (compress.go, 80%阈值 + 10轮保留 + LLM摘要)
- [x] 工具输出截断 (50KB / 500 行)
- [x] 会话持久化 + 恢复 (session.go)
- [x] 会话切换零延迟 handoff (NewSession 注入未提取上下文)
- [x] ExtractQueue.EnqueueSession (旧会话推入 Worker)
- [x] 冷恢复 (RecoverUnextracted, daemon 重启后补处理)
- [x] 实体关联 (entity.go: Store, StoreBatch, Search, TopEntities)
- [x] Embedding auto-discovery (probe /v1/embeddings)
- [x] SQLite schema 全部表: sessions, session_messages, episodic_memories, episodic_fts, semantic_facts, entities, usage_log, audit_log, failure_records, trust_rules, triggers
- [x] /compact 反馈面板 (bordered panel, before/after tokens, context window %)

### Capability (05-capability)

- [x] 声明式 SKILL.md 解析 (frontmatter + footer meta + H1)
- [x] 两层注入: summary list (常驻) + full content ([LOAD_SKILL] 触发)
- [x] 能力目录扫描 (`~/.suna/capabilities/`)
- [x] 类型检测 (declarative/script/mcp 基于文件存在)
- [x] 热重载 (Reload)

### TUI (01-architecture, 06-memory)

- [x] 5 个命令: /new, /model, /memory search, /compact, /help
- [x] 键盘快捷键: Ctrl+N, Ctrl+K, Ctrl+T, Ctrl+U/D, Esc, Enter, Alt+Enter
- [x] 命令自动补全建议
- [x] 状态栏: provider/model + token 用量 + 速度
- [x] i18n 完整覆盖 (中/英, 40+ keys)
- [x] /compact 反馈面板 (bordered panel)
- [x] Memory search 结果渲染

### 技术栈 (08-tech-stack)

- [x] Go + pure Go SQLite (modernc.org/sqlite)
- [x] 跨平台 build tags (`_unix.go` / `_windows.go`)
- [x] Bubble Tea v2 + Bubbles v2 + Lipgloss v2
- [x] go-openai + anthropic-sdk-go
- [x] go:embed 模板
- [x] 数据目录 `~/.suna/` 完整布局

---

## Phase 1 遗留问题 (需修复)

### 关键 (影响核心功能)

| # | 问题 | 文件 | 设计文档依据 | 说明 |
|---|---|---|---|---|
| 1 | **Anthropic 非流式** | `model/anthropic.go:59` | 01-architecture | 使用 `Messages.New()` 阻塞调用，应改为 streaming API。用户等很久才看到一次性全部输出 |
| 2 | **FTS rowid 映射错误** | `memory/episodic.go:57` | 06-memory | `episodic_memories` 用 TEXT 主键 (`time.Now().UnixNano()`)，FTS insert 用 `LastInsertId()`，两者不匹配导致 FTS JOIN 失败 |
| 3 | **Embedding 未接入 Worker** | `memory/worker.go:154-171` | 06-memory | `generateEmbedding()` 存在但 Worker `storeEpisodicSummary()` 不调用，向量搜索永远空 |
| 4 | **Worker 提取 prompt 中文** | `memory/worker.go:194-210` | 06-memory | "从以下交互中提取"、"用户"、"助手" 等中文 prompt 应改为英文 |
| 5 | **Query Rewrite 未接入** | `core/agent.go:643` | 06-memory | `buildSystemPrompt` 调用 `SearchFTS` 而非 `SearchWithRewrite`，改写逻辑永远不会触发 |
| 6 | **LLM 路由未激活** | `core/agent.go:261` | 02-model-router | `Route()` 不调用 `RouteWithLLM()`，多模型场景下 LLM 路由不生效 |

### 中等优先

| # | 问题 | 文件 | 说明 |
|---|---|---|---|
| 7 | Sub agent Guard 无规则 | `core/agent.go` Spawn | 子 agent Guard 用 `NewGuard(nil, sessionID)` 传入 nil DB，不共享用户规则 |
| 8 | Context window 预算未分配 | `core/agent.go` | 设计要求 ~4K system + ~100K memory + ~20K tools + ~4K output，实际无预算控制 |
| 9 | 自动压缩中 summaryTokens 被丢弃 | `core/agent.go:599` | `Compact()` 中 `len(summary)/4` 被赋值给 `_`，未返回给调用方 |

### 低优先 (可延后到 Phase 2)

| # | 问题 | 文件 | 说明 |
|---|---|---|---|
| 10 | Self-reflection 未实现 | — | 工具执行后的自检 (exit code + LLM deep check) |
| 11 | Retry 策略未实现 | — | 失败后最多 3 次重试 |
| 12 | Hooks 系统未实现 | `config/config.go` | HookConfig 存在但无执行逻辑 |
| 13 | `suna "单命令"` 模式 | `main.go` | 非交互单次执行 |
| 14 | 渐进信任未实现 | — | trust_rules 表存在但无读写逻辑 |
| 15 | Guard confirm/modify 决策 | — | 只支持 approve/reject |
| 16 | 语义记忆定期合并 | — | facts 只追加不合并，可能膨胀 |

---

## Phase 2 待实现

### Guard 完善 (04-guard)

- [ ] Stage 3: LLM review (用 review_model 判断高风险操作)
- [ ] `confirm` 决策 (路由到用户确认)
- [ ] `modify` 决策 (建议参数修改)
- [ ] 渐进信任 (trust_rules 读写, 行为学习)
- [ ] Sub agent Guard 上下文注入

### 感知源 (07-trigger)

- [ ] `PerceptionSource` 接口 (ID, Type, Start, Stop)
- [ ] `SenseManager` (注册/信号处理)
- [ ] Timer (cron, `robfig/cron/v3`)
- [ ] Watcher (fsnotify 文件变化监听)
- [ ] Webhook (HTTP server)
- [ ] Stream (file/ws/exec 数据流)
- [ ] 信号过滤 + 防抖
- [ ] `perception.event` IPC 广播
- [ ] 自然语言创建/管理感知源
- [ ] 持久化到 triggers 表

---

## Phase 3 待实现

### 能力系统 (05-capability)

- [ ] Script 类型 (main.js, QuickJS runtime)
- [ ] MCP 类型 (mcp.json, MCP client)
- [ ] Lifecycle hooks (OnSignal, PreLLM, PreToolUse, PostToolUse)
- [ ] JS Host functions (file, exec, storage, context, interaction)
- [ ] Skill 验证 (ValidateSkill)
- [ ] 能力学习流 (detect → generate → validate → confirm)

### 记忆深化 (06-memory)

- [ ] 实体关联完整实现 (实体图谱)
- [ ] 时间推理注入 (时间相关查询的 timeline context)
- [ ] 语义记忆定期合并
- [ ] `/memory status` 命令

---

## Phase 4 待实现

- [ ] 模型表现追踪 (准确率/延迟/成本)
- [ ] 项目配置完善 (SUNA.md 高级功能)
- [ ] 会话持久化增强 (跨 daemon 重启)

## Phase 5 待实现

- [ ] 意图层 (主动预测、习惯学习)
- [ ] WebSocket 多 I/O 渠道
- [ ] 能力市场
- [ ] Docker sandbox

---

## 已知 Bug / 技术债

1. **FTS rowid 映射**: episodic_memories TEXT 主键 vs FTS LastInsertId() 不匹配，可能导致 FTS 搜索无结果
2. **Anthropic 非流式**: 阻塞 API 调用，影响用户体验
3. **Embedding 空转**: Worker 不生成 embedding，向量搜索形同虚设
4. **extract prompt 中文**: Worker 提取 prompt 应统一英文
5. **Compact summaryTokens 丢弃**: `Compact()` 返回值 `_ = len(summary)/4` 被忽略

---

## 架构决策记录

| 决策 | 选择 | 原因 |
|---|---|---|
| IPC Client 位置 | `tui/` 包 | 包独立性，TUI 不依赖 `ipc/` |
| Config 归属 | Daemon 持有 | TUI 通过 IPC 获取 provider/model 名 |
| Guard 构造器 | 双构造器 | NewGuard (子agent) / NewGuardWithConfig (主agent) |
| 人格机制 | Capability | 移除 SOUL.md，统一到 persona/SKILL.md |
| 向量搜索 | 暴力 cosine | 不引入向量数据库，SQLite BLOB |
| Embedding 发现 | HTTP probe | 不消耗 token，检测 endpoint 是否存在 |
| 记忆提取 | 异步批量 | 不阻塞 Agent Loop，Worker 独立 goroutine |
| 会话切换 | 零延迟 handoff | 注入未提取原文 + 后台推送 Worker |
