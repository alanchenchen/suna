# 04 — LLM 权限守卫 (Guard)

> 当前实现状态: **Usable MVP**
> 最后更新: 2026-05-20

## 当前实现事实

Guard 已有最低安全闭环，支持 4 种 mode，真实 confirm 和 LLM review：

- **Guard Mode**: `readonly` / `ask` / `auto` / `smart`，默认 `ask`，可通过 TUI Config 切换。
- **Mode 策略**:
  - `readonly`: 只允许 low risk 且只读的操作；其他操作 reject，不弹窗。
  - `ask`: Low risk auto approve，Medium/High risk 真实暂停等待用户确认。
  - `auto`: Low/Medium/High risk auto approve；只保留硬规则 reject，不弹窗。
  - `smart`: Low risk auto approve，Medium/High risk 调 LLM review；review 失败/不确定/confirm/modify 转用户确认。
- **Confirm 机制**: `EventGuardConfirm` 独立事件类型，daemon 通过 `Reply chan string` 阻塞等待 TUI 回传 approve/reject。不复用 AskUser 事件。
- **LLM Review 修复**: review 失败、JSON parse 失败、不确定、confirm、modify 都保守转用户确认，不再静默放行。
- **Modify 处理**: 当前不做自动参数改写，作为带 suggestion 的 confirm 处理。
- **Sub-agent**: 通过 `newGuardForSession()` 继承主 Guard policy、blocked/allowed、audit DB。
- **TUI**: Guard confirm overlay 显示 tool/risk/reason/suggestion/params，支持键位操作；Config home 可切换 Guard Mode。
- **IPC**: `MethodGuardReply` / `NotifyGuardConfirm` / `GuardConfirmParams` / `GuardReplyParams`；server 用 `pendingGuards sync.Map` 管理。

未实现：
- 参数改写（modify 时自动 patch 参数）。
- Guard rules 的 TUI 编辑 UI。
- 渐进信任（行为模式学习后自动调整风险级别）。

## 当前决策表

Guard 决策顺序固定如下：

1. 命中内置或用户 blocked rule -> `reject`，不询问。
2. 命中用户 allowed rule -> `approve`，不询问。
3. 根据 tool 和参数计算 `low` / `medium` / `high`。
4. 根据 guard mode 决定自动放行、拒绝、询问或 LLM review。

### Mode 行为矩阵

| Mode | Low Risk | Medium Risk | High Risk | 硬拦截规则 |
|---|---|---|---|---|
| `readonly` | 只读操作 approve；写操作 reject | reject | reject | reject |
| `ask` | approve | confirm | confirm | reject |
| `auto` | approve | approve | approve | reject |
| `smart` | approve | LLM review | LLM review | reject |

说明：
- `confirm` 表示 TUI 显示 Guard confirmation overlay，由用户选择 approve/reject。
- `LLM review` 表示先让 active model 审查，返回 approve/reject/confirm/modify；失败或不确定时转 confirm。
- `auto` 不是“自动判断风险后询问”，而是“除硬拦截外全部自动放行”。
- 用户 allowed rule 优先级高于 mode 和 risk，但低于硬拦截。

### Risk 分级矩阵

| Tool | Low Risk | Medium Risk | High Risk |
|---|---|---|---|
| `exec` | 只读命令，如 `ls`、`cat`、`rg`、`git status` | 不是只读命令，且不含删除关键词 | 命令字符串包含 `rm`、`rmdir`、`del` |
| `writefile` | 写入当前不存在的新文件 | 覆盖当前已存在的文件 | 当前无 high 分支 |
| `editfile` | 当前无 low 分支 | 所有 editfile | 当前无 high 分支 |
| `writehttp` | 当前无 low 分支 | 非 DELETE 写请求 | method 为 `DELETE` |
| 其他工具 | 默认 low | 当前无 medium 分支 | 当前无 high 分支 |

只读命令白名单按平台区分。Unix/macOS 当前包括：

```text
ls, cat, head, tail, wc, stat, du,
grep, rg, ag, ack,
find, glob, locate,
which, type, where, command,
echo, printf, date, whoami,
git status, git log, git diff,
git branch, git show, git stash list,
env, printenv, uname, hostname
```

管道命令只有在每个子命令都属于只读命令时，整体才算 low risk。

---

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
│  - 只读 Exec: ls, cat, rg │
│  - 写入不存在的新文件      │
│  - 其他未特殊分类的工具    │
│                          │
│  中风险:                  │
│  - 覆盖已有文件            │
│  - editfile 修改文件       │
│  - Exec: npm install      │
│  - writehttp 非 DELETE    │
│                          │
│  高风险:                  │
│  - Exec 包含 rm/rmdir/del │
│  - writehttp DELETE       │
│                          │
│  后续动作由 Guard Mode 决定│
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│  Stage 3: LLM 审查       │  用 active_model，+50-100ms
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
  format\s+[A-Z]:                      → 禁止格式化驱动器
  :\s*C:\\Windows|:\s*C:\\Program       → 禁止写入系统目录

通用:
  curl.*\|\s*sh                         → 禁止远程脚本管道执行
  wget.*\|\s*sh                         → 禁止远程脚本管道执行
  eval\s*\$\(                            → 禁止命令注入模式
```

### 配置示例

```toml
[guard]
review_model = "fast"                    # 用哪个模型做 LLM 审查

# 用户自定义拦截 (追加到内置规则之上)
[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

# 用户自定义放行 (优先于 mode/risk，低于硬拦截)
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
用 active_model 做审查:
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

以下代码片段描述当前实现语义，应与 `internal/guard/guard.go` 保持一致。

```go
type RiskLevel int

const (
    RiskLow RiskLevel = iota
    RiskMedium
    RiskHigh
)

func assessRisk(tool string, params ToolParams) RiskLevel {
    switch tool {
    case "exec":
        cmd := params["command"]
        // 只读命令为 low risk；是否直接放行由 Guard Mode 决定。
        if isReadOnlyCommand(cmd) { return RiskLow }
        if containsAny(cmd, []string{"rm", "rmdir", "del"}) { return RiskHigh }
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
    return RiskLow
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

用户可以通过自然语言查询审计记录:
  "帮我看看最近做了哪些操作"
  "有哪些高风险操作"
  或通过 /memory search "audit" 查询

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
   3. LLM 审查    — 中高风险操作 → active_model 审查
  4. 用户确认    — 审查不确定或高风险 → 转用户

远期: 意图信任 — 用户确认的意图下确定性操作直接执行 (见 10-stateful-entity.md)
```

## Guard Mode 实现

当前 Guard 以 full mode 运行，不再是 stub：

```go
type Mode string

const (
    ModeReadOnly Mode = "readonly"
    ModeAsk      Mode = "ask"
    ModeAuto     Mode = "auto"
    ModeSmart    Mode = "smart"
)

func (g *Guard) Check(ctx context.Context, tool string, params map[string]any) (GuardResult, error) {
    risk := assessRisk(tool, params)

    if hit, reason := g.checkBlocked(tool, params); hit {
        return GuardResult{Decision: "reject", Reason: reason}, nil
    }
    if hit, reason := g.checkAllowed(tool, params); hit {
        return GuardResult{Decision: "approve", Reason: reason}, nil
    }

    if g.Mode == ModeReadOnly {
        if risk == RiskLow && isReadOnlyTool(tool) {
            return GuardResult{Decision: "approve", Reason: "readonly low risk"}, nil
        }
        return GuardResult{Decision: "reject", Reason: "readonly mode blocks this operation"}, nil
    }

    if risk == RiskLow {
        return GuardResult{Decision: "approve", Reason: "low risk"}, nil
    }
    if g.Mode == ModeAuto {
        return GuardResult{Decision: "approve", Reason: "auto mode"}, nil
    }
    if g.Mode == ModeAsk {
        return GuardResult{Decision: "confirm", Reason: "confirm risky operation"}, nil
    }

    // smart mode: run LLM review for medium/high risk.
    return g.llmReviewOrConfirm(ctx, tool, params, risk)
}
```

### Mode 行为矩阵

| Mode | Low Risk | Medium Risk | High Risk | 硬拦截规则 |
|---|---|---|---|---|
| `readonly` | 只读操作 approve；写操作 reject | reject | reject | reject |
| `ask` | approve | **confirm** | **confirm** | reject |
| `auto` | approve | approve | approve | reject |
| `smart` | approve | **LLM review** | **LLM review** | reject |

LLM review 的输出处理：
- `approve` → 放行
- `reject` → 拒绝
- `confirm` → 转用户确认
- `modify` → 当前转用户确认（附带 suggestion），未实现参数改写
- 解析失败 / LLM 调用失败 → 保守转用户确认

### Guard Confirm 流程

```
1. Guard.Check() 返回 "confirm"
2. Core 通过 EventGuardConfirm 暂停 tool 执行
3. IPC 发送 NotifyGuardConfirm 到 TUI
4. TUI 渲染 Guard confirm overlay
5. 用户选择 approve/reject
6. TUI 通过 MethodGuardReply 回传决策
7. Core 通过 Reply chan 收到决策
8. 执行或拒绝 tool
```

### Sub-agent Guard

```go
// newGuardForSession() 是统一创建入口
// main/sub/NewSession/RestoreSession 都通过它
func (a *Agent) newGuardForSession() (*Guard, error) {
    return &Guard{
        Mode:     a.config.Guard.ModeOrDefault(),
        // 共享 blocked/allowed/audit DB
    }, nil
}
```

### 配置

```toml
[guard]
mode = "ask"  # readonly | ask | auto | smart
```

`ModeOrDefault()` 返回配置的 mode，默认 `ask`。
