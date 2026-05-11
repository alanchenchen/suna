# Suna 开发进度清单

> 最后更新: 2026-05-09
> 对应设计文档: plans/01~12
> 当前阶段: **Phase 1 收尾 + Phase 2 启动**

## Phase 状态总览

| Phase | 内容 | 状态 | 完成度 |
|---|---|---|---|
| Phase 1 | Daemon + 记忆 + 9工具 + Guard stub + TUI 重构 | 收尾中 | ~90% |
| Phase 2 | Guard LLM review + sub-agent 智能路由 + 并发控制 + 提示词优化 | 进行中 | ~10% |
| Phase 3 | QuickJS + MCP + Skill 学习 | 未开始 | 0% |
| Phase 4 | 模型表现追踪 + 完善 | 未开始 | 0% |
| Phase 5 | 意图层 + WebSocket + Docker | 未开始 | 0% |

---

## 4 个核心点 — 当前状态

### 1. 多模型 + 子任务智能调度

| 子功能 | 状态 | 说明 |
|---|---|---|
| 多模型配置 (active_model + [[models]]) | ✅ 完成 | `config.toml` 新版结构 |
| Provider 统一接口 | ✅ 完成 | OpenAI + Anthropic + 兼容 API |
| LLM 路由 `RouteWithLLM` | ⚠️ 实现未接入 | `router.go` 已实现，但 spawn 不调用 |
| Spawn 子 agent | ✅ 基础完成 | 串行执行，工具集受限，不嵌套 |
| **Spawn 自动选择模型** | ❌ 缺失 | `executeSpawn` 始终用 active model |
| **Spawn 提示词模板** | ❌ 缺失 | 无独立的 sub-agent system prompt 模板 |
| **Sub-agent 智能描述和权限** | ❌ 缺失 | main 不自动生成 sub 的描述/权限 |

### 2. 并行 + 并发限制

| 子功能 | 状态 | 说明 |
|---|---|---|
| Tool 并行执行 | ✅ 完成 | 多 tool call 同时 goroutine |
| **Tool 并发上限** | ❌ 缺失 | config 无 `max_parallel_tools`，无上限 |
| **Sub-agent 并行** | ❌ 缺失 | spawn 是串行的，不能同时跑多个 |
| **Sub-agent 并发上限** | ❌ 缺失 | config 无 `max_parallel_subagents` |

### 3. Guard 权限系统

| 子功能 | 状态 | 说明 |
|---|---|---|
| Stage 1 硬规则 | ✅ 完成 | 跨平台 blocked rules |
| Stage 2 风险评估 | ✅ 完成 | Low/Medium/High |
| 敏感文件保护 | ✅ 完成 | `IsSensitivePath` + `MaskSensitiveContent` |
| 审计日志 | ✅ 完成 | SQLite audit_log |
| 用户自定义规则 | ✅ 完成 | blocked/allowed in config.toml |
| **Stage 3 LLM 审核** | ❌ 缺失 | `guard.md` 模板存在但未接入 |
| **confirm/modify 决策** | ❌ 缺失 | 只支持 approve/reject |
| 渐进信任 | ❌ 缺失 | trust_rules 表存在但无逻辑 |

### 4. 自主学习能力

| 子功能 | 状态 | 说明 |
|---|---|---|
| 记忆提取 (extract) | ✅ 完成 | 异步 Worker + 4 层存储 |
| 语义记忆 (semantic facts) | ✅ 完成 | preference/action/decision/error/fact |
| 情景记忆 (episodic) | ✅ 完成 | FTS5 + 向量搜索 + 时间衰减 |
| 实体关联 | ✅ 基础 | entity store 存在，图谱关系浅 |
| **主动学习循环** | ❌ 缺失 | 没有从记忆中主动学习和调整行为 |
| **习惯学习** | ❌ 缺失 | 不检测用户模式 |

---

## 缺失点详情

### AskUser 选项选择

- **现状**: `options` 参数从 LLM → IPC → TUI 全链路传递，TUI 收到 `AskUserParams.Options`
- **问题**: TUI 只把 options 当文本展示（`"❓ " + p.Question`），不渲染为可选列表
- **需要**: TUI 渲染为数字标记的可选项，用户输入数字或选择后回传，agent 侧验证

### Spawn 提示词模板

- **现状**: spawn 的 system prompt 由 LLM 在 `params["system"]` 中传入，无独立模板
- **需要**: 新增 `spawn_system.md` 模板，自动注入任务上下文、可用工具说明、约束条件

### Project Config

- **不是问题**: `SUNA.md` 或 `.suna/AGENTS.md` 的内容注入 system prompt，是用户自定义项目指令的机制
- 设计文档中定义的 `[Project Configuration]` section 就是这个用途

---

## Phase 1 已实现清单

### Daemon + TUI 双进程 (01-architecture)

- [x] Daemon 常驻进程 (`internal/daemon/daemon.go`)
- [x] PID 文件 + Socket 文件管理
- [x] 30 分钟空闲自动退出 (`lifecycle.go`)
- [x] 信号处理 (SIGTERM/SIGINT)
- [x] 单二进制多模式: `suna`, `suna daemon`, `suna stop`, `suna status` (`main.go`)
- [x] TUI 纯前端，无业务逻辑
- [x] TUI 页面模型: Welcome / Chat / Config / Help

### IPC 通信 (01-architecture)

- [x] JSON-RPC 2.0 over NDJSON (`internal/ipc/`)
- [x] Transport 抽象接口 (Unix Socket / Named Pipe)
- [x] 跨平台 Socket: `_unix.go` + `_windows.go`
- [x] TUI IPC Client 在 `tui/` 包内 (包独立性)
- [x] Daemon State 初始推送 (provider/model 名称)
- [x] 流式通知: stream, reasoning, tool_start, tool_end, ask_user
- [x] 通知式结果: compact_result, memory_search_result
- [x] AskUser 跨请求协调 (pending asks map)
- [x] Config CRUD IPC (config.get / config.set)

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

### 多模型路由 (02-model-router)

- [x] Provider 接口: Complete, EstimateTokens, ContextWindow, SupportsEmbedding, Embed
- [x] OpenAI Provider (覆盖所有 OpenAI 兼容 API)
- [x] Anthropic Provider
- [x] 统一消息格式: CompletionRequest, Chunk, Message, ToolCall
- [x] Tool calling 跨 Provider 转换
- [x] LLM 路由 (RouteWithLLM, 基于 strengths 偏好标签选择模型)
- [x] Embedding 自动发现 (HTTP probe `/v1/embeddings`)
- [x] 已知 Provider embedding model 映射 (Zhipu/OpenAI/DashScope)
- [x] Token 估算 (CJK 支持)
- [x] 新版配置: active_model + [[models]] + credentials.toml

### 9 工具 (03-tools)

- [x] ReadFile (敏感文件保护)
- [x] ListDir
- [x] ReadHTTP
- [x] Exec (跨平台 shell: `_unix.go` / `_windows.go`)
- [x] WriteFile
- [x] EditFile
- [x] WriteHTTP
- [x] AskUser (动态追加, options 参数全链路传递)
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
- [x] 上下文压缩 (compress.go, 80%阈值 + 10轮保留 + LLM摘要)
- [x] 工具输出截断 (50KB / 500 行)
- [x] 会话持久化 + 恢复 (session.go)
- [x] 会话切换零延迟 handoff (NewSession 注入未提取上下文)
- [x] 实体关联 (entity.go: Store, StoreBatch, Search, TopEntities)
- [x] Embedding auto-discovery (probe /v1/embeddings)

### Capability (05-capability)

- [x] 声明式 SKILL.md 解析 (frontmatter + footer meta + H1)
- [x] 两层注入: summary list (常驻) + full content ([LOAD_SKILL] 触发)
- [x] 能力目录扫描 (`~/.suna/capabilities/`)
- [x] 热重载 (Reload)

### TUI (12-tui-design)

- [x] 页面模型: Welcome / Chat / Config / Help
- [x] Welcome: pet logo + 状态概览 + 菜单导航
- [x] Chat: viewport + textarea + spinner 底栏 + 命令联想 + help overlay
- [x] Config: 纯 IPC 交互，provider 表单
- [x] Markdown 渲染 (glamour v2)
- [x] i18n (中/英, 40+ keys, 收敛到 internal/tui)
- [x] 5 个命令: /new, /model, /memory search, /compact, /help
- [x] 键盘快捷键: Ctrl+N, Ctrl+K, Ctrl+T, Ctrl+U/D, Esc, Enter, Alt+Enter
- [x] 状态栏: provider/model + token 用量 + 速度
- [x] /compact 反馈面板

### 技术栈 (08-tech-stack)

- [x] Go + pure Go SQLite (modernc.org/sqlite)
- [x] 跨平台 build tags (`_unix.go` / `_windows.go`)
- [x] Bubble Tea v2 + Bubbles v2 + Lipgloss v2 + Glamour v2
- [x] go-openai + anthropic-sdk-go
- [x] go:embed 模板
- [x] 数据目录 `~/.suna/` 完整布局

---

## Phase 1 遗留问题

### 关键 (影响核心功能)

| # | 问题 | 文件 | 说明 |
|---|---|---|---|
| 1 | **AskUser 不渲染选项** | `tui/app.go:428` | TUI 只展示问题文本，不渲染可选项 |
| 2 | **Spawn 不走路由** | `core/agent_tools.go:119` | sub-agent 始终用 active model，不调 RouteWithLLM |
| 3 | **无 Spawn 提示词模板** | `core/agent_tools.go:131-135` | system prompt 全靠 LLM 传入，无结构化模板 |
| 4 | **无并发限制** | `core/agent.go:394-401` | tool 并行无上限，config 无限制字段 |
| 5 | **Guard LLM 未接入** | `guard/guard.go:110` | guard.md 模板存在但 Check() 不调用 LLM |
| 6 | **FTS rowid 映射错误** | `memory/episodic.go:57` | TEXT 主键 vs FTS LastInsertId() 不匹配 |
| 7 | **Embedding 未接入 Worker** | `memory/worker.go:154-171` | storeEpisodicSummary 不调用 generateEmbedding |
| 8 | **Query Rewrite 未接入** | `core/agent_prompt.go:45` | 调 SearchFTS 而非 SearchWithRewrite |

### 中等优先

| # | 问题 | 文件 | 说明 |
|---|---|---|---|
| 9 | Sub agent Guard 无规则 | `core/agent_tools.go:123` | NewGuard(nil, sessionID) 不共享用户规则 |
| 10 | Context window 预算未分配 | `core/agent.go` | 无 ~4K system + ~100K memory + ~20K tools 预算 |
| 11 | Anthropic 非流式 | `model/anthropic.go:59` | 阻塞 API 调用 |

### 低优先 (可延后到 Phase 2+)

| # | 问题 | 说明 |
|---|---|---|
| 12 | Self-reflection 未实现 | 工具执行后的自检 |
| 13 | Retry 策略未实现 | 失败后最多 3 次重试 |
| 14 | Hooks 系统未实现 | HookConfig 存在但无执行逻辑 |
| 15 | `suna "单命令"` 模式 | 非交互单次执行 |
| 16 | 渐进信任未实现 | trust_rules 表存在但无读写逻辑 |
| 17 | Guard confirm/modify 决策 | 只支持 approve/reject |
| 18 | 语义记忆定期合并 | facts 只追加不合并 |

---

## Phase 2 进行中

### 当前任务

- [ ] **AskUser 选项选择**: TUI 渲染为可选列表，支持数字/点击选择
- [ ] **并发限制 config**: 新增 `max_parallel_tools` (默认 5) + `max_parallel_subagents` (默认 4)
- [ ] **Spawn 提示词模板**: 新增 `spawn_system.md`，自动注入任务/工具/约束
- [ ] **Spawn 接入路由**: executeSpawn 调用 RouteWithLLM 选择最佳模型
- [ ] **提示词优化**: 优化 system.md (sub-agent 分配指导) + guard.md + extract.md

### Guard 完善 (04-guard)

- [ ] Stage 3: LLM review (用 active_model 判断高风险操作)
- [ ] `confirm` 决策 (路由到用户确认)
- [ ] `modify` 决策 (建议参数修改)
- [ ] 渐进信任 (trust_rules 读写, 行为学习)
- [ ] Sub agent Guard 上下文注入

### 感知源 (07-trigger)

- [ ] `PerceptionSource` 接口 (ID, Type, Start, Stop)
- [ ] `SenseManager` (注册/信号处理)
- [ ] Timer / Watcher / Webhook / Stream
- [ ] 信号过滤 + 防抖
- [ ] 持久化到 triggers 表

---

## Phase 3 待实现

### 能力系统 (05-capability)

- [ ] Script 类型 (main.js, QuickJS runtime)
- [ ] MCP 类型 (mcp.json, MCP client)
- [ ] Lifecycle hooks
- [ ] Skill 验证
- [ ] 能力学习流

### 记忆深化 (06-memory)

- [ ] 实体关联完整实现 (实体图谱)
- [ ] 时间推理注入
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

## 提示词模板清单

| 模板 | 文件 | 用途 | 状态 |
|---|---|---|---|
| system | `templates/system.md` | 主 agent 系统提示词 | ✅ 需优化 |
| guard | `templates/guard.md` | LLM 安全审查 | ⚠️ 模板存在未接入 |
| compress | `templates/compress.md` | 上下文压缩 | ✅ 完成 |
| extract | `templates/extract.md` | 记忆提取 | ✅ 需优化 |
| **spawn_system** | `templates/spawn_system.md` | 子 agent 系统提示词 | ❌ 缺失 |
| **route** | 路由提示词 (router.go 内联) | 模型路由选择 | ✅ 完成 |

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
| 配置热重载 | 未实现 | 激活/新增模型需重启 daemon |
| Project Config | SUNA.md / .suna/AGENTS.md | 用户自定义项目指令，注入 system prompt |
| 并发限制 | 待实现 | config 无上限，默认应限制 4-5 |
| Sub-agent 路由 | 待接入 | RouteWithLLM 已实现但未调用 |
