# 11 — 设计审查：问题清单

逐文件审查发现的矛盾和缺口。已修复项标记 ✅。

> 历史审查记录：本文件保留当时发现的问题和修复记录，不作为当前快捷键、功能状态或实现细节的唯一事实来源。当前行为以对应主题文档和 `internal/*` 实现为准。

## Critical

### ✅ Exec 分类矛盾
- 03-tools.md: Exec 归类为 Perceive 工具
- 04-guard.md: "Exec 是最危险的工具，所有调用都经过 Guard"
- **已修复**: Exec 改为 Act 工具，Guard 通过 isReadOnlyCommand 快速放行只读命令

## High

### ✅ 压缩保留口径不一致
- 01-architecture.md: "超过 20 轮的部分压缩"
- 06-memory.md: "保留区: 最近 10 轮"
- **已修复**: 当前口径为 working memory 消息数；手动 compact 保留最近最多 10 条消息，自动 compact 按完整请求预算决定 recent suffix。

### ✅ Guard TOML 格式不一致
- 04-guard.md: `[[guard.blocked]]` (数组表)
- 08-tech-stack.md: `[guard.blocked]` + `patterns = [...]`
- **已修复**: 统一为 `[[guard.blocked]]` 格式

### ✅ 失败记忆存储矛盾
- 06-memory.md 正文: 独立 `failure_records` 表
- 06-memory.md 底部"与原设计关系": "合并到情景记忆"
- **已修复**: 底部改为"独立表 failure_records"

## Medium

### ✅ Layer 计数混乱
- index.md: "三层架构"
- 10-stateful-entity.md: 四层架构图
- **已修复**: 全部统一为三层，07/09/index 文件已更新

### ✅ Shell Hooks vs Skill Hooks 未关联
- 01-architecture.md: shell command hooks
- 05-capability.md: JS function hooks
- **已修复**: 01 中明确两套 hooks 的关系和执行顺序

### ✅ trust_rules 表缺失
- 04-guard.md 定义了 `trust_rules` 表
- 06-memory.md SQLite schema 未包含
- **已修复**: trust_rules 已在 06 schema 中

### ✅ 触发器配置来源不清
- **已修复**: 07-trigger.md 明确"用户不直接写 TOML，自然语言创建，存 SQLite"

## Low

### ✅ `/file` 命令未列入 TUI 命令
- **已修复**: 已补充到 01-architecture.md TUI 命令列表

### ✅ Embedding 维度
- **已修复**: 06-memory.md 改为"维度由 provider 自动决定"，新增 Embedding 自动配置节

### ✅ 项目结构 vs 记忆设计
- 08-tech-stack.md: facts.go/failures.go（旧设计）
- **已修复**: 改为 episodic.go/semantic.go/entity.go/embed.go

## 已知未解决项（远期）

- 记忆膨胀管理：30 天未使用降低权重为当前策略，远期需设计重要性衰减和合并
- 能力版本管理
- 后台进程（Exec `&`）监控
- Webhook 端口发现：设计为随机分配或用户指定 (见 07-trigger.md)，用户通过自然语言让 agent 查看已分配端口
- 感知层资源消耗预算

## 第三轮审查修复 (2026-05-06)

### ✅ 09-competitive-review.md 过时信息
- 多渠道行: "TUI + Daemon" → "TUI + (远期 Web)"
- 权限模型: 删除 "意图信任"
- Hooks 行: 更新为 OnSignal/PreLLM/PreToolUse/PostToolUse
- 注释: "四层" → "三层"

### ✅ 10-stateful-entity.md Phase 不一致
- Phase 1/3/4 描述统一为与 index.md 一致
- 开放问题: 向量索引标记为已解决
- "3 层记忆" → "4 层记忆"

### ✅ 05-capability.md Skill 加载机制
- 使用 `skill_load(name)` 内部工具加载已启用且有效的 Skill
- ParseSkillMD 支持 frontmatter 的 name/description 字段
- 补充 H1 提取 name 逻辑

### ✅ 06-memory.md 压缩阈值
- 当前口径已更新为：自动 compact 检测完整 LLM 请求的 80% 安全阈值；手动 compact 保留最近最多 10 条 working messages。

### ✅ 06-memory.md embedding 大小
- 统一为 ~4-8KB (取决于维度)

### ✅ 04-guard.md 用户规则描述
- "覆盖内置规则" → "独立于内置规则"

### ✅ 跨平台补充
- 01: Source 字段删除 "intent"
- 04: isReadOnlyCommand 加 Windows 命令
- 08: config.toml 示例补 guard.allowed + guard 规则说明
- 08: 数据目录补 logs/

### ✅ 07-trigger.md
- Timer 行为: 删除 "TUI 关闭时写入日志"，改为 "TUI 关闭时进程退出"

## 第四轮审查修复 (2026-05-08) — Daemon 架构升级

### ✅ 架构从 TUI 单进程改为 Daemon + TUI 双进程
- **根因**: 记忆提取与 TUI 生命周期耦合导致三个问题：(1) 提取被迫同步 (2) 感知层随 TUI 生死 (3) 状态丢失
- **变更范围**: 01/06/07/08/09/10/index 共 8 个文件
- **新架构**: sunad 守护进程 (Agent/Memory/Perception) + TUI 前端 (纯 UI)，IPC 通信 (Unix Socket + JSON-RPC 2.0)

### ✅ 01-architecture.md Daemon 架构
- 删除 "无 daemon 模式"
- 新增: Daemon 架构节 (架构图、生命周期、自动退出策略)
- 新增: IPC 协议节 (Transport 接口、JSON-RPC 方法、Streaming、连接管理)
- I/O 抽象层: 从直接 IO 接口改为 IPC Client/Conn
- 运行模式: 单二进制多模式 (suna / suna daemon / suna stop)
- 数据目录: 新增 sunad.pid, sunad.sock
- TUI 命令: 新增 /daemon status, /daemon stop, /daemon restart
- Agent Loop 步骤 7: "自动提取" → "异步: 写入提取队列"

### ✅ 06-memory.md 异步批量提取
- 仅添加式提取: 从 "每轮同步提取" 改为 "异步队列 + 批量"
- 新增: 显著性过滤 (高/中/低，规则判断零 LLM 成本)
- 新增: Memory Worker goroutine (独立于 Agent Loop)
- 提取队列: memory channel (热路径) + session_messages.memory_extracted (冷路径恢复)
- 不新增 extraction_queue 表，复用 session_messages 加 memory_extracted BOOL 字段
- 异常中断: 区分 TUI 崩溃 (daemon 继续) vs Daemon 崩溃 (扫描 memory_extracted=0 恢复)

### ✅ 07-trigger.md 感知 24/7
- 删除 "感知源随 TUI 进程停止"
- 感知源在 daemon 进程内 24/7 运行
- Timer: 执行结果存好，TUI 在线则推送，离线则下次展示
- Daemon 启动/退出: 感知源恢复/停止
- Webhook: 从 "Suna 内置" 改为 "Daemon (sunad) 内置"

### ✅ 08-tech-stack.md 技术选型
- 新增依赖: go-winio (Windows Named Pipe, 微软官方)
- 删除: "CLI 框架" 从明确不引入列表
- 项目结构: 新增 daemon/, ipc/ 目录
- 项目结构: memory/ 新增 queue.go, worker.go, significance.go
- 项目结构: tui/ 注释改为 "纯前端，无业务逻辑"
- 数据目录: 新增 sunad.pid, sunad.sock
- Phase 1: 4周 → 5周，新增 daemon/ipc 开发和跨平台测试
- Phase 5: "多 I/O 渠道" → "WebSocket Transport"

### ✅ 09-competitive-review.md 竞品对比
- Daemon 行: "TUI 进程 (无 daemon) ⚠️" → "sunad 守护进程 + TUI 前端 ✅"
- 有状态行: 更新为 "Daemon 常驻 + 感知 24/7 + 异步记忆"
- 记忆行: 新增 "异步批量提取"
- 长任务行: "TUI 进程" → "Daemon 常驻"
- I/O 渠道: 评分描述更新为 "IPC 抽象层预留了扩展"

### ✅ 10-stateful-entity.md
- 架构图: "Entity" → "sunad"，"通信层 IO" → "IPC Server"
- Phase 1: 4周 → 5周，新增 daemon + IPC
- Phase 5: 新增 WebSocket Transport

### ✅ 跨平台注意事项补充 (08-tech-stack.md)
- 新增"跨平台注意事项"节，记录 IPC 和 Exec 两方面的坑
- IPC: Windows Named Pipe (go-winio), Socket 残留清理, NDJSON 分帧, 重连状态, 写阻塞
- Exec: LLM 生成 Windows 命令错误, Git Bash 优先策略, MVP 目标平台定义
- 与前面讨论的 5 个确定性坑完全一致

### ✅ extraction_queue 表改为复用 session_messages
- 删除独立的 extraction_queue 表
- session_messages 新增 memory_extracted BOOL + significance TEXT 字段
- 热路径: memory channel (内存), 冷路径: 扫描 memory_extracted=0 (daemon 恢复)
- 01/06/08/11 四个文件同步修正

### ✅ index.md
- 架构图: 全面替换为 Daemon + TUI 双进程
- 新增 "Daemon + TUI 双进程架构" 节 (含为什么需要 Daemon)
- 新增 "单二进制多模式" 说明
- 新增 "IPC 通信" 说明
- 关键差异化: 感知 24/7、异步批量提取
- MVP Phase 1: 4周 → 5周

## 第五轮审查修复 (2026-05-08) — 记忆系统优化

### ✅ 合并情景+语义提取为一次 LLM 调用
- Memory Worker 每次 LLM 调用同时输出 episodes + facts + entities
- 删除 "语义记忆每 5 轮单独提炼" 的独立步骤
- 语义记忆的写入时机改为 "Memory Worker 每次提取时同时产出"
- 减少 LLM 调用次数，不丢失任何信息

### ✅ 按需查询改写
- 新增三级检索策略: Level 0 (直接 FTS5) → Level 1 (FTS5 不足时改写) → Level 2 (放弃)
- 仅 ~30% 交互触发 Level 1 改写 → 每天 ~60 次额外 LLM 调用 → ¥1.8/月
- 弥补 FTS5 在无 embedding 时的语义弱点

### ✅ 会话切换零延迟记忆传递
- /new 切换会话时不等待 flush (LLM 响应慢)
- 直接从旧 session 的 session_messages 取最近 5 轮未提取原文注入新 session
- 同时推给 Memory Worker 异步处理，完成后自然替代临时注入

### ✅ 检索筛选流程细化
- 新增时间衰减排序 (7天内×1.0, 30天×0.8, 90天×0.5, 更早×0.3)
- 新增 Token 预算控制 (~4K tokens → 放 ~20-30 条记忆)
- 候选得分都低时不注入 → 宁可不放，不塞噪音

### ✅ 程序记忆触发方式明确
- MVP 只做路径 A (failure_records pattern 聚合 ≥3 次) + 路径 B (用户主动触发)
- 路径 C (LLM 判断重复模式) 标记为 Phase 3

### ✅ LLM 成本预算
- 新增成本预算节: 重度用户 ~¥303/月，记忆相关 LLM 调用 < 1%

### ✅ 跨平台文件分布更新
- 08-tech-stack.md 项目结构改为 _unix.go / _windows.go 后缀
- 新增完整跨平台文件分布图 (ipc/tool/guard 三个包)

## 第六轮审查修复 (2026-05-08) — 交互精简 + 提示词简化 + 项目结构对齐

### ✅ TUI 命令精简
- 合并 /new + /reset → 只有 /new (新建会话 = 清空记忆 + 新 session)
- 移除命令: /verbose, /session, /audit, /file, /think, /intent, /trigger, /daemon, /skill
- 保留 5 个命令: /new, /model, /compact, /memory search, /help
- 新增键盘快捷键: Ctrl+N (新建), Ctrl+T (工具细节 toggle), Ctrl+K (切换模型)
- 拖拽文件到终端直接读取，替代 /file 命令
- 01-architecture.md, 06-memory.md, 03-tools.md 同步更新

### ✅ 提示词上下文简化
- 对话历史不再作为独立区块
- 压缩后的对话摘要归入"相关记忆"区块
- 提示词只有两部分: 固定指令 + 相关记忆
- 用户偏好 + 检索记忆 + 压缩摘要 → 统一为"相关记忆"区块
- 01-architecture.md 上下文窗口分配 + 缓存友好策略更新
- 06-memory.md 注入策略 + 压缩策略更新

### ✅ 移除 SOUL.md
- 人格定义改为普通 Skill: 默认数据目录下的 skills/persona/SKILL.md
- 不引入额外概念，统一到 Skill 系统
- 用户可以让 agent 自己创建/调整人格
- 01-architecture.md 人格定义节重写
- 08-tech-stack.md 用户数据目录更新

### ✅ 09-competitive-review.md 全面重写
- 新增"架构对比"节 (进程模型/IPC/状态管理/感知/记忆)
- 功能矩阵更新: 人格 → Skill, TUI 命令 → 精简 (5 个)
- 移除已过时的建议/决策分离格式，直接记录决策
- 设计完备度评估新增"架构模式"和"TUI 交互"维度

### ✅ 08-tech-stack.md 项目结构对齐实际实现
- 移除 cmd/ 目录 (main.go 在根目录)
- 新增 prompt/ 目录 (go:embed 模板)
- 新增 i18n/ 目录 (内置国际化)
- tech stack 更新: bubbles/v2, uuid, 移除未引入的依赖
- 新增"与文档设计的差异"节
- 用户数据目录: 新增 credentials.toml, persona/SKILL.md, suna.log

### ✅ 10-stateful-entity.md 架构图修正
- 移除图中"意图层"框，改为感知→记忆→行动三层
- 与 index.md 和 01-architecture.md 保持一致

## 第七轮审查修复 (2026-05-20) — 模型路由改造 + Guard mode + Spawn 收敛

### ✅ 模型路由改造
- 删除 `RouteWithLLM()` / `routeByLLM()` / `RouteResult` / `route.md`
- spawn schema required=["task","model","tools"]
- 执行层校验 model/tool 名称
- spawn.tools 用 enum 限定可选工具
- 02-model-router.md 已更新为 main-agent delegated routing

### ✅ 系统提示词优化
- 删除 Current Time 和 User 字段
- 重排 system.md 为稳定策略→低频动态→高频动态
- subtask 不继承 system.md，使用独立 subtask_system.md
- subtask_system.md 只含 task/env/tools/context/rules，并明确隔离上下文和单向数据流

### ✅ Subtask 隔离
- 新增 `systemPromptOverride` 字段
- subtask 不暴露 askuser/spawn tool schema
- 删除 SpawnToolGuide/SpawnTools 模板变量

### ✅ Guard mode 实现
- 4 个 mode: readonly / ask / auto / smart，默认 ask
- Check() 按 mode 分策略
- confirm/modify/review-fail 不再假 approve
- 新增 checkAllowed() / guardTarget() / isReadOnlyTool() / RiskString()

### ✅ Core guard confirm
- 独立 EventGuardConfirm 事件类型（不复用 AskUser）
- confirmGuard() 暂停 tool 执行等待用户确认
- newGuardForSession() 统一创建带 mode 的 Guard

### ✅ IPC guard 协议
- MethodGuardReply / NotifyGuardConfirm / GuardConfirmParams / GuardReplyParams
- server 新增 pendingGuards sync.Map 和 handleGuardReply()

### ✅ TUI guard confirm UI
- 独立 overlay 面板显示 tool/risk/reason/suggestion/params
- 支持 ←→/j/k/Enter/Esc/Y/N 键位，默认选 Reject
- 所有文案走 i18n

### ✅ TUI Guard Mode 配置
- Config home 新增 Guard Mode 行
- 可切换 ask→smart→auto→readonly→ask
- 通过 protocol config.set 持久化

### ✅ 04-guard.md 更新
- 新增"当前实现事实"状态块
- Phase 1 Guard Stub 替换为 Guard Mode 实现
- 新增 Mode 行为矩阵和 Confirm 流程

### ✅ 03-tools.md 更新
- Spawn 参数 model 和 tools 改为必填
- 删除"默认工具集"
- 新增 daemon 校验说明和 subtask 限制

### ✅ 12-tui-design.md 更新
- 新增 Guard confirm overlay 章节
- 新增 Guard Mode 配置
- IPC 数据新增 guard_confirm / guardReply
- 文件结构更新

### ✅ 01-architecture.md 更新
- Guard 描述更新为 4 mode
- Subtask 属性更新（必填 model/tools、禁用 askuser、独立 subtask_system.md）

### ✅ 08-tech-stack.md 更新
- 项目结构新增 agent_management.go / agent_prompt.go
- guard.go 描述更新
- 新增 subtask_system.md 模板
- IPC server/message 描述更新
- config.toml 新增 guard.mode
