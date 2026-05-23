# 04 — LLM 权限守卫 (Guard)

> 当前实现状态: **Usable MVP（Guard 已加固）**
> 最后更新: 2026-05-23

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
- **审计**: 当前记录 Guard 决策本身；tool 执行后的最终 result/error 暂未回写到 audit_log。审计参数会脱敏/摘要化，当前暂未提供读取 UI 或查询工具。
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
| `exec` | 轻量 shell analyzer 能证明所有命令片段都是只读 | 有副作用、复杂语法、动态执行、嵌套 shell、无法证明只读 | 删除/格式化/权限/系统配置/远程脚本执行等高危模式 |
| `writefile` | 当前无 low 分支 | 普通文件写入（含新建和覆盖） | 敏感路径、系统路径、profile、启动项、CI、git hooks、高影响配置 |
| `editfile` | 当前无 low 分支 | 普通文件修改 | 敏感路径、系统路径、profile、启动项、CI、git hooks、高影响配置 |
| `writehttp` | 当前无 low 分支 | 非 DELETE 写请求 | method 为 `DELETE` |
| 其他 Act 工具 | 当前无 low 分支 | 未显式分类的 Act 工具默认 medium | 当前无 high 分支 |

`RiskLow` 被刻意收窄，只代表“可证明只读”。除 `readonly` mode 外，low risk 会自动放行，因此不能把“不确定但看起来还好”的操作归为 low。轻量 shell analyzer 的原则是：

- 能证明所有片段都是只读 -> `RiskLow`
- 明确高危 -> `RiskHigh`
- 有副作用、复杂/动态语法、嵌套 shell、解释器动态执行、无法解析 -> `RiskMedium`

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

Windows 当前包括：

```text
dir, type, findstr, where,
Get-ChildItem, Get-Content, Get-Location,
gci, gc, pwd,
echo, date, whoami,
git status, git log, git diff,
git branch, git show, git stash list,
set, ver, hostname
```

多命令组合只有在每个片段都属于只读命令时，整体才算 low risk。支持常见分隔符/管道拆分，例如 `|`、`&&`、`||`、`;`、`&`、换行。重定向、命令替换、PowerShell encoded command、嵌套 shell、解释器 `-c/-e` 等动态语法不会被归为 low。

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
│  - 可证明只读 Exec         │
│    ls, cat, rg, git status│
│                          │
│  中风险:                  │
│  - 普通 writefile          │
│  - editfile 修改文件       │
│  - Exec: npm install      │
│  - 动态/嵌套/未知命令       │
│  - writehttp 非 DELETE    │
│                          │
│  高风险:                  │
│  - 删除/格式化/权限/系统改动│
│  - 远程脚本 pipe 执行       │
│  - 敏感/系统/启动路径写入   │
│  - writehttp DELETE       │
│                          │
│  后续动作由 Guard Mode 决定│
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│  Stage 3: LLM 审查       │  用 active_model
│                          │
│  输入:                    │
│  - 操作类型和参数          │
│  - 操作意图摘要            │
│  - recent context 字段      │
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
│  Stage 4: 审计 + 执行    │
│                          │
│  执行操作                  │
│  先记录 Guard 决策          │
│  再执行操作                 │
│  执行结果进入 tool result   │
└──────────────────────────┘
```

## 硬规则配置

硬规则在 `~/.suna/config.toml` 中配置，用户可自定义。内置规则按 OS 区分：

### 内置硬规则（按 OS）

```
Unix (macOS/Linux):
  (?i)rm ... -r ... -f /     → 禁止递归删除根目录
  (?i)rm ... -r ... -f ~     → 禁止递归删除 home
  (?i)mkfs|dd ... /dev       → 禁止磁盘格式化/设备写入
  :\s*/etc/|:\s*/usr/|:\s*/System/  → 禁止写入系统目录
  (?i)chmod ... -r ... 777 / → 禁止递归开放权限
  (?i)curl|wget ... | sh     → 禁止远程脚本管道执行
  (?i)eval $()               → 禁止命令注入模式
  >\s*/dev/sd               → 禁止直接写磁盘设备

Windows:
  (?i)rmdir|rd|del|erase ... /s|/q drive → 禁止递归强制删除驱动器
  (?i)format drive                       → 禁止格式化驱动器
  (?i)C:\\Windows|C:\\Program             → 禁止写入系统目录
  (?i)Remove-Item ... recurse/force      → 禁止 PowerShell 强制递归删除驱动器
  (?i)iwr|irm ... | iex                  → 禁止远程 PowerShell 执行
  (?i)iex|Invoke-Expression              → 禁止 PowerShell 动态执行
  (?i)Set-ExecutionPolicy                → 禁止修改执行策略
  (?i)Start-Process ... -Verb RunAs      → 禁止提权启动
  (?i)reg/sc/schtasks/vssadmin/bcdedit   → 禁止系统配置修改
  (?i)diskpart/takeown/icacls            → 禁止磁盘/权限破坏操作
  (?i)robocopy ... /mir                  → 禁止破坏性镜像同步

通用:
  (?i)curl|wget|iwr|irm ... | interpreter → 禁止远程脚本管道执行
  (?i)eval $()                            → 禁止命令注入模式
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

注意：`guard.allowed.reason` 当前可以持久化到 config，但 `newGuardForSession()` 创建 Guard 时只传递 pattern/tool，放行原因暂未进入 Guard 决策或审计输出。

## LLM 审查的 Prompt

```
你是 Suna 的安全审查模块。判断以下操作是否应该执行。

操作: {{ tool_name }}
参数: {{ tool_params }}
意图: {{ 最近对话中的操作意图摘要 }}
目标: {{ 文件路径 / URL / 命令 }}

上下文:
{{ recent context，如已设置 }}

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

### Recent Context 当前状态

```
Guard 结构中保留 recent context 字段，LLM review prompt 也支持注入该字段。

当前 core 执行路径尚未在每次工具调用前填充最近 3 轮对话或 subtask task/context，因此多数 LLM review 的 recent context 为空。subtask 不继承 main conversation 或 active memory；如果需要 Guard review 上下文，应使用显式 delegated task/context。若 review 不确定，smart mode 会保守转用户确认。
```

## 风险评级的实现

以下代码片段描述当前实现语义，应与 `internal/guard/guard.go`、`internal/guard/shell_analyzer.go`、`internal/guard/file_risk.go` 保持一致。

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
        shell := params["shell"]
        return analyzeExecCommand(cmd, shell, platformReadOnlyCommands())
    case "writefile":
        if isHighRiskFilePath(params["path"]) { return RiskHigh }
        return RiskMedium
    case "editfile":
        if isHighRiskFilePath(params["path"]) { return RiskHigh }
        return RiskMedium
    case "writehttp":
        if params["method"] == "DELETE" { return RiskHigh }
        return RiskMedium
    }
    return RiskMedium
}

// 轻量 shell analyzer：不是完整 shell parser，只做保守分类。
func analyzeExecCommand(cmd string, shell string, readOnly []string) RiskLevel {
    if isHighRiskCommand(cmd) { return RiskHigh }
    if hasDynamicShellSyntax(cmd, shell) { return RiskMedium }

    segments, ok := splitShellSegments(cmd, shell)
    if !ok { return RiskMedium }
    for _, segment := range segments {
        if !isReadOnlySegment(segment, readOnly) {
            return RiskMedium
        }
    }
    return RiskLow
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
| result | TEXT | 预留字段；当前未回写执行结果 |
| error | TEXT | 预留字段；当前未回写执行错误 |
```

当前审计写入发生在 Guard 决策阶段，记录 session/tool/params/risk/decision/reason。tool 执行后的 stdout/stderr、成功/失败和 error 不会再更新到同一条 audit_log。

`params` 会以 JSON 写入，但不会原样保存高敏/大字段：

- `content` / `body` / `old_string` / `new_string` / `system` 只保存长度和 sha256 摘要。
- `env` 的值不保存明文，只保存 redacted summary。
- 其他字符串字段会经过 `MaskSensitiveContent()` 脱敏。

当前没有 audit 查询 UI 或自然语言查询工具；审计先作为内部安全日志保留，也为后续渐进信任提供数据基础。

审计日志不进入 user_memory，也不通过 `/memory search` 暴露，避免污染用户画像。

## Subtask 的 Guard

```
Subtask 的 Guard 和 main 共享同一套规则
但 subtask 的操作上下文更少：不继承 main conversation、active memory 或 main working memory

当前处理方式:
  - sub 通过 newGuardForSession() 继承同一套 mode、blocked/allowed、audit DB 和 LLM reviewer
  - sub 的 Guard recent context 当前未自动注入 delegated task 描述
  - 如果审查不确定 → confirm 事件回到发起连接，由 TUI 展示给用户
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
