# 04 — LLM 权限守卫 (Guard)

Suna 的核心创新：权限控制不是静态规则，而是 LLM 驱动的动态审查。

## 设计哲学

```
传统 agent: 静态 allowlist/denylist + 用户弹窗确认
  问题: 规则永远不完整，复杂意图无法用规则表达

Suna: 硬规则兜底 + LLM 理解意图后动态决策
  优势: 能理解"这个删除操作是用户要求的清理" vs "这个删除可能是误操作"
```

## 审查流程

```
Action 请求 (WriteFile / EditFile / Exec / WriteHTTP)
  │
  ▼
┌──────────────────────────┐
│  Stage 1: 硬规则检查      │  确定性，零延迟
│  永远拦截的操作            │
│  - rm -rf /              │
│  - mkfs, dd if=/dev/zero │
│  - 写入系统目录 (/etc等)   │
│  - 删除整个 home 目录      │
│                          │
│  命中 → 直接拒绝          │
│  未命中 → 继续            │
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│  Stage 2: 风险评级        │  确定性，零延迟
│  基于操作类型和目标        │
│                          │
│  低风险:                  │
│  - 写入用户目录下的新文件  │
│  - 写入 /tmp              │
│  - GET 请求               │
│  - Exec: ls, cat, grep    │
│                          │
│  中风险:                  │
│  - 覆盖已有文件            │
│  - Exec: npm install      │
│  - POST 请求              │
│                          │
│  高风险:                  │
│  - 删除文件                │
│  - Exec: rm, git push     │
│  - 写入配置文件            │
│  - 修改环境变量            │
│                          │
│  低风险 → 直接放行        │
│  中风险 → Stage 3         │
│  高风险 → Stage 3 (必审)  │
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│  Stage 3: LLM 审查       │  用 fast 模型，+50-100ms
│                          │
│  输入:                    │
│  - 操作类型和参数          │
│  - 操作意图摘要            │
│  - 最近 3 轮对话上下文     │
│  - 目标文件/路径信息       │
│                          │
│  输出:                    │
│  - approve: 通过          │
│  - reject: 拒绝 + 原因    │
│  - confirm: 转用户确认    │
│  - modify: 建议修改参数   │
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│  Stage 4: 执行 + 审计    │
│                          │
│  执行操作                  │
│  记录审计日志              │
│  检查执行结果              │
│  异常 → 记录失败记忆      │
└──────────────────────────┘
```

## 硬规则配置

硬规则在 `~/.suna/config.toml` 中配置，用户可自定义。内置规则按 OS 区分：

### 内置硬规则（按 OS）

```
Unix (macOS/Linux):
  rm\s+-rf\s+/              → 禁止递归删除根目录
  rm\s+-rf\s+~              → 禁止递归删除 home
  mkfs|dd\s+if=/dev/zero    → 禁止磁盘格式化
  :\s*/etc/|:\s*/usr/|:\s*/System/  → 禁止写入系统目录
  chmod\s+-R\s+777\s+/      → 禁止递归开放权限
  >\s*/dev/sd               → 禁止直接写磁盘设备

Windows:
  rmdir\s+/s\s+/q\s+[A-Z]:\\           → 禁止递归强制删除驱动器
  rd\s+/s\s+/q\s+[A-Z]:\\              → 同上
  del\s+/s\s+/q\s+[A-Z]:\\            → 禁止递归强制删除驱动器
  Remove-Item\s+.*-Recurse.*-Force.*[A-Z]:\\  → 禁止 PowerShell 强制递归删除
  format\s+[A-Z]:                      → 禁止格式化驱动器
  cipher\s+/w:[A-Z]:\\                 → 禁止数据擦除
  :\s*C:\\Windows|:\s*C:\\Program       → 禁止写入系统目录
  :\s*C:\\Users\\[^\\]+\\ntuser         → 禁止写入用户注册表文件

通用:
  curl.*\|\s*sh|wget.*\|\s*sh           → 禁止远程脚本管道执行
  eval\s*\$\(                            → 禁止命令注入模式
```

### 配置示例

```toml
[guard]
enabled = true
review_model = "fast"                    # 用哪个模型做 LLM 审查

# 用户自定义拦截 (追加到内置规则之上)
[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

# 用户自定义放行 (独立于内置规则，只影响 Stage 2 风险评级)
[[guard.allowed]]
pattern = "ls|cat|head|tail|grep|find|wc"
tool = "exec"
reason = "只读命令直接放行"

[[guard.allowed]]
pattern = ".*"
tool = "readfile"
reason = "读文件直接放行"
```

内置规则不可配置、不可覆盖。用户自定义规则追加在内置规则之后。

## LLM 审查的 Prompt

```
你是 Suna 的安全审查模块。判断以下操作是否应该执行。

操作: {{ tool_name }}
参数: {{ tool_params }}
意图: {{ 最近对话中的操作意图摘要 }}
目标: {{ 文件路径 / URL / 命令 }}

上下文:
{{ 最近 3 轮对话摘要 }}

判断标准:
- 用户明确要求的操作 → approve
- 操作目标与用户意图一致 → approve
- 操作可能造成不可逆损害但用户未明确要求 → confirm
- 操作明显偏离用户意图 → reject
- 操作参数可优化 → modify

回复格式 (JSON):
{ "decision": "approve|reject|confirm|modify", "reason": "原因", "suggestion": "修改建议(仅modify)" }
```

### 审查模型选择

```
用最便宜的 fast 模型做审查:
  - 不需要强推理能力
  - 只需要理解意图和操作的对应关系
  - glm-4-flash 级别足够
  - 成本 < $0.0001/次
  - 延迟 +50-100ms
```

### 为什么只传最近 3 轮

```
审查不需要完整上下文:
  - 传太多 → 审查成本高、延迟大
  - 传太少 → 缺少意图信息
  - 3 轮是经验和成本的平衡点

如果 3 轮不够判断:
  → decision="confirm"，转用户确认
  宁可多问一次，不要误放
```

## 风险评级的实现

```go
type RiskLevel int

const (
    RiskLow    RiskLevel = iota  // 直接放行
    RiskMedium                   // LLM 审查
    RiskHigh                     // LLM 审查 (更严格的 prompt)
)

func assessRisk(tool string, params ToolParams) RiskLevel {
    switch tool {
    case "exec":
        cmd := params["command"]
        // 只读命令直接放行，不经过 Stage 3 LLM 审查
        // grep/glob/find/head/tail/wc/cat/ls/stat/du/which/type 等本质是 Perceive 操作
        if isReadOnlyCommand(cmd) { return RiskLow }
        if containsAny(cmd, []string{"rm", "rmdir", "del"}) { return RiskHigh }
        if containsAny(cmd, []string{"npm", "pip", "go"}) { return RiskMedium }
        return RiskMedium
    case "writefile":
        if fileExists(params["path"]) { return RiskMedium }  // 覆盖
        return RiskLow                                        // 新建
    case "editfile":
        return RiskMedium  // 修改已有文件
    case "writehttp":
        if params["method"] == "DELETE" { return RiskHigh }
        return RiskMedium
    }
    return RiskMedium
}

// 只读命令白名单: 不修改文件系统/网络/进程的命令
// 匹配方式: 命令名 (忽略参数)，支持管道中的每个子命令独立判断
func isReadOnlyCommand(cmd string) bool {
    read_only := []string{
        "ls", "cat", "head", "tail", "wc", "stat", "du",
        "grep", "rg", "ag", "ack",              // 搜索
        "find", "glob", "locate",                // 查找
        "which", "type", "where", "command",     // 命令查找
        "echo", "printf", "date", "whoami",      // 输出
        "git status", "git log", "git diff",     // git 只读
        "git branch", "git show", "git stash list",
        "env", "printenv", "uname", "hostname",  // 环境信息
        // Windows 等效
        "dir", "type", "findstr", "where",       // Windows 只读
        "Get-ChildItem", "Get-Content",           // PowerShell 只读
    }
    // 逐个检查命令是否以 read_only 命令开头
    for _, ro := range read_only {
        if strings.HasPrefix(strings.TrimSpace(cmd), ro+" ") ||
           strings.TrimSpace(cmd) == ro {
            return true
        }
    }
    // 管道: 所有子命令都是只读 → 整体只读
    if strings.Contains(cmd, "|") {
        parts := strings.Split(cmd, "|")
        for _, p := range parts {
            if !isReadOnlyCommand(strings.TrimSpace(p)) {
                return false
            }
        }
        return true
    }
    return false
}
```

## 审计日志

每次 Act 操作都记录审计日志，存入 SQLite：

```
表: audit_log
| 字段 | 类型 | 说明 |
|---|---|---|
| id | TEXT | UUID |
| timestamp | DATETIME | 操作时间 |
| session_id | TEXT | 会话 ID |
| tool | TEXT | 工具名 |
| params | JSON | 操作参数 |
| risk_level | TEXT | low/medium/high |
| guard_decision | TEXT | approve/reject/confirm |
| guard_reason | TEXT | 审查原因 |
| result | TEXT | success/failure |
| error | TEXT | 错误信息 (如有) |
```

用户可以查看审计日志：
```
TUI 命令: /audit          # 查看最近操作
TUI 命令: /audit --risk   # 只看高风险操作
```

## Sub Agent 的 Guard

```
Sub agent 的 Guard 和 main 共享同一套规则
但 sub agent 的操作上下文更少 (sub 没有完整对话历史)

处理方式:
  - sub 的 Guard 审查 prompt 中额外注入:
    "这是 main agent 委派的子任务: {task_description}"
  - 让 Guard 知道操作的大背景
  - 如果审查不确定 → confirm 转给 main agent → main 转给用户
```

## 渐进信任

Guard 会学习用户的行为模式。

```
用户连续 10 次 approve 了 "npm install"
  → Guard 自动将该操作降级为低风险
  → 下次不再经过 LLM 审查

用户连续 3 次 reject 了某类操作
  → Guard 自动升级风险级别
  → 下次强制转用户确认

存储: SQLite trust_rules
  | pattern | tool | risk_adjustment | reason | learned_from |
```

### 信任层级

```
信任从高到低:
  1. 行为信任    — 连续 10 次 approve 的操作 → 跳过 LLM 审查
  2. 低风险放行  — 只读命令等 → 跳过 LLM 审查
  3. LLM 审查    — 中高风险操作 → fast 模型审查
  4. 用户确认    — 审查不确定或高风险 → 转用户

远期: 意图信任 — 用户确认的意图下确定性操作直接执行 (见 10-stateful-entity.md)
```

## Phase 1 Guard Stub

Phase 1 不实现 LLM 审查（Stage 3）。Guard 以 stub 模式运行：

```go
type GuardStub struct {
    db *sql.DB  // 审计日志
}

func (g *GuardStub) Check(ctx context.Context, tool string, params map[string]any) error {
    // Stage 1: 硬规则检查 (完整实现)
    if isBlocked(tool, params) {
        g.audit(ctx, tool, params, "blocked", "hard_rule")
        return fmt.Errorf("blocked: %s", blockReason)
    }

    // Stage 2: 风险评级 (完整实现)
    risk := assessRisk(tool, params)

    // Phase 1: 跳过 Stage 3 LLM 审查，全部放行
    // 记录审计日志，方便后续分析
    g.audit(ctx, tool, params, "auto_approve", fmt.Sprintf("risk=%s phase1_stub", risk))

    return nil
}
```

```
Phase 1 行为:
  - 硬规则拦截: 正常工作 (rm -rf / 等仍然被拦截)
  - 只读命令: isReadOnlyCommand 快速放行
  - 其余所有操作: 自动放行 + 审计日志
  - 无 LLM 审查成本

Phase 2 升级:
  - GuardStub 替换为完整 Guard
  - 审计日志中的 "auto_approve" 记录可用于校准初始信任规则
```
