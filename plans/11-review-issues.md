# 11 — 设计审查：问题清单

逐文件审查发现的矛盾和缺口。已修复项标记 ✅。

## Critical

### ✅ Exec 分类矛盾
- 03-tools.md: Exec 归类为 Perceive 工具
- 04-guard.md: "Exec 是最危险的工具，所有调用都经过 Guard"
- **已修复**: Exec 改为 Act 工具，Guard 通过 isReadOnlyCommand 快速放行只读命令

## High

### ✅ 压缩轮数不一致
- 01-architecture.md: "超过 20 轮的部分压缩"
- 06-memory.md: "保留区: 最近 10 轮"
- **已修复**: 统一为 10 轮

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
- Webhook 端口发现：设计为随机分配或用户指定 (见 07-trigger.md)，用户通过 /trigger list 查看已分配端口
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

### ✅ 05-capability.md 能力加载机制
- 删除 "AskUser 工具" 引用，明确为 [LOAD_SKILL: name] 文本标记
- ParseSkillMD 支持 frontmatter + footer meta 双格式
- 补充 H1 提取 name 逻辑

### ✅ 06-memory.md 压缩阈值
- 压缩区/保留区统一为 10 轮

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
