# 04 — LLM 权限守卫 (Guard)

> 当前实现状态: **Usable MVP（Guard 已加固）**
> 最后更新: 2026-05-29

## 当前实现事实

Guard 已有最低安全闭环，支持 4 种 mode、workspace 硬边界、真实 confirm 和 LLM review：

- **Guard Mode**: `readonly` / `ask` / `auto` / `smart`，默认 `ask`，可通过 TUI Config 切换。
- **Guard 覆盖范围**: 除 `askuser` 和 `spawn` 外，所有 tool call 都会在实际执行前进入 Guard；`spawn` 本身跳过，但 subtask 内部工具继续继承同一个 Guard。
- **Workspace 硬边界**: `[guard].workspace` 默认为空，不启用限制；非空时必须是存在目录，Guard 会优先拦截 workspace 外的本地文件路径和 `exec` 明显路径访问，返回明确 reject 原因。
- **Mode 策略**:
  - `readonly`: 只允许 low risk 且只读的操作；其他操作 reject，不弹窗。
  - `ask`: Low risk auto approve，Medium/High risk 真实暂停等待用户确认。
  - `auto`: Low/Medium/High risk auto approve；只保留硬规则 reject，不弹窗。
  - `smart`: Low risk auto approve，Medium/High risk 调 LLM review；review 可直接 approve/reject/confirm/modify，失败/不确定转用户确认。
- **Confirm 机制**: `EventGuardConfirm` 独立事件类型，daemon 通过 `Reply chan string` 阻塞等待 TUI 回传 approve/reject。不复用 AskUser 事件。
- **LLM Review**: smart mode 的 review 会接收轻量结构化意图上下文（当前用户请求、tool intent、assistant context、最近消息摘要），不再依赖 Guard 内部全局 recent context；review 失败、JSON parse 失败、不确定或 confirm 都保守转用户确认。
- **Modify 处理**: `modify` 不执行原 tool call，也不弹用户确认；Guard 将 reason/suggestion 作为 tool error 返回给主 agent，由主 agent 重新发起更安全/更窄的工具调用并再次经过 Guard。
- **Sub-agent**: 通过 `newGuardForSession()` 继承主 Guard policy、blocked/allowed、audit DB 和 LLM reviewer。
- **审计**: 当前记录 Guard 决策本身；tool 执行后的最终 result/error 暂未回写到 audit_log。审计参数会脱敏/摘要化，当前暂未提供读取 UI 或查询工具。
- **TUI**: Guard confirm overlay 显示 tool/risk/reason/suggestion/params，支持 `Y/A` approve、`N/R/Esc` reject、方向键选择、`Enter` 确认所选；工具块通过 `agent.tool_guard` 显示 Guard 决策、来源和风险；Config home 可切换 Guard Mode 并编辑 Workspace。
- **IPC**: `MethodGuardReply` / `NotifyGuardConfirm` / `GuardConfirmParams` / `GuardReplyParams`；server 用 `pendingGuards sync.Map` 管理。

未实现：
- Guard 自动 patch 参数（当前由主 agent 根据 modify suggestion 重新发起工具调用）。
- Guard rules 的 TUI 编辑 UI。
- 渐进信任（行为模式学习后自动调整风险级别）。

## 当前决策表

Guard 决策顺序固定如下：

1. `[guard].workspace` 非空且操作解析到 workspace 外 -> `reject`，不询问。
2. 命中内置 blocked rule 或用户 `[[guard.blocked]]` -> `reject`，不询问。
3. 命中用户 `[[guard.allowed]]` -> `approve`，不询问。
4. 根据 tool 和参数计算 `low` / `medium` / `high`。
5. 根据 guard mode 决定自动放行、拒绝、询问或 LLM review。

### Mode 行为矩阵

| Mode | Low Risk | Medium Risk | High Risk | 硬拦截规则 |
|---|---|---|---|---|
| `readonly` | 只读操作 approve；写操作 reject | reject | reject | reject |
| `ask` | approve | confirm | confirm | reject |
| `auto` | approve | approve | approve | reject |
| `smart` | approve | LLM review | LLM review | reject |

说明：
- `confirm` 表示 TUI 显示 Guard confirmation overlay，由用户选择 approve/reject。
- `LLM review` 表示先让 active model 审查，返回 approve/reject/confirm/modify；失败、不确定或 confirm 时转用户确认；modify 时返回 suggestion 给主 agent 重试。
- `auto` 不是“自动判断风险后询问”，而是“除硬拦截外全部自动放行”。
- workspace 硬边界优先级最高；用户 allowed rule、`auto`、用户确认和 LLM review 都不能绕过 workspace reject。
- 用户 allowed rule 优先级高于 mode 和 risk，但低于 workspace 与其他硬拦截。

### Risk 分级矩阵

| Tool | Low Risk | Medium Risk | High Risk |
|---|---|---|---|
| `exec` | 轻量 shell analyzer 能证明所有命令片段都是只读 | 有副作用、复杂语法、动态执行、嵌套 shell、无法证明只读 | 删除/格式化/权限/系统配置/远程脚本执行等高危模式 |
| `writefile` | 当前无 low 分支 | 普通文件写入（含新建和覆盖） | 敏感路径、系统路径、profile、启动项、CI、git hooks、高影响配置 |
| `editfile` | 当前无 low 分支 | 普通文件修改 | 敏感路径、系统路径、profile、启动项、CI、git hooks、高影响配置 |
| `writehttp` | 当前无 low 分支 | 非 DELETE 写请求 | method 为 `DELETE` |
| `readfile` / `listdir` / `readhttp` | 只读感知工具 | 当前无 medium 分支 | 当前无 high 分支 |
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

```text
传统 agent: 静态 allowlist/denylist + 用户弹窗确认
  问题: 规则永远不完整，复杂意图无法用规则表达

Suna: 硬规则兜底 + LLM 理解意图后动态决策
  优势: 能理解“这个删除操作是用户要求的清理” vs “这个删除可能是误操作”
```

## 审查流程

```text
Tool 请求 (ReadFile / ListDir / ReadHTTP / WriteFile / EditFile / Exec / WriteHTTP)
  │
  ▼
Stage 0: Workspace 检查
  - guard.workspace 为空时跳过
  - 文件类 path、exec.cwd、exec 明显命令路径解析到 workspace 外 → reject
  │
  ▼
Stage 1: 硬规则检查
  - 递归删除根/home、磁盘格式化、系统目录破坏、远程脚本 pipe 等 → reject
  │
  ▼
Stage 2: 风险评级
  - low: 可证明只读
  - medium: 普通写入、动态/未知 exec、非 DELETE 写 HTTP
  - high: 删除/格式化/权限/系统改动、敏感/启动路径、DELETE 等
  │
  ▼
Stage 3: LLM 审查（仅 smart 的 medium/high）
  输入: tool/risk/target/脱敏参数 + UserRequest + ToolIntent + AssistantContext + RecentContext
  输出: approve / reject / confirm / modify
  │
  ▼
Stage 4: 审计 + 执行或返回
  - approve: 执行原 tool call
  - reject: tool error
  - confirm: 等用户确认
  - modify: 不执行，返回 reason/suggestion 给主 agent 重试
```

## 硬规则配置

硬规则在默认数据目录下的 `config.toml` 中配置，当前默认路径为 `~/.suna/config.toml`，用户可自定义。配置路径由 `internal/config/paths.go` 的 `DefaultConfigPath()` / `Config.ConfigPath()` 派生。内置规则按 OS 区分。

### 配置示例

```toml
[guard]
mode = "ask"
workspace = ""                            # 空表示不限制；非空时必须是存在目录

# 用户自定义拦截 (追加到内置规则之上)
[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

[[guard.blocked]]
pattern = "(^|/)private-notes(/|$)"
reason = "禁止读取私人笔记目录"

[[guard.blocked]]
pattern = "169\\.254\\.169\\.254|localhost|127\\.0\\.0\\.1"
reason = "禁止访问 metadata/local HTTP 服务"

# 用户自定义放行 (优先于 mode/risk，低于 workspace 和其他硬拦截)
[[guard.allowed]]
pattern = "^(ls|pwd|git status|git diff)(\\s|$)"
tool = "exec"
reason = "只读命令直接放行"

[[guard.allowed]]
pattern = ".*"
tool = "readfile"
reason = "读文件直接放行"
```

内置规则不可配置、不可覆盖。用户自定义规则追加在内置规则之后。`guard.workspace` 是最高优先级硬边界，启用后会在 blocked/allowed/mode/LLM review 之前执行。

### Guard Rule Pattern

`guard.blocked.pattern` 和 `guard.allowed.pattern` 都是 Go regexp，匹配当前 tool 的 guard target。匹配的是 tool 参数里的原始字符串，不是 workspace 解析后的真实路径。

当前 target 映射：

| Tool | Pattern 匹配对象 | 示例 target |
|---|---|---|
| `exec` | `command` | `git status --short` |
| `readfile` | `path` | `docs/plan.md` |
| `listdir` | `path` | `/Users/me/project/private-notes` |
| `writefile` | `path` | `src/generated.ts` |
| `editfile` | `path` | `internal/guard/guard.go` |
| `readhttp` | `url` | `http://169.254.169.254/latest/meta-data` |
| `writehttp` | `url` | `https://api.example.com/items/1` |

常用写法：

```toml
# 禁止发布 npm 包。\s+ 表示一个或多个空白字符。
[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

# 禁止读取或列出 private-notes 目录；同时覆盖相对路径和绝对路径里的路径段。
[[guard.blocked]]
pattern = "(^|/)private-notes(/|$)"
reason = "禁止读取私人笔记目录"

# 禁止访问云 metadata 和本机 HTTP 服务，适用于 readhttp/writehttp 的 url。
[[guard.blocked]]
pattern = "169\\.254\\.169\\.254|localhost|127\\.0\\.0\\.1"
reason = "禁止访问 metadata/local HTTP 服务"

# 只允许 exec 中几类只读命令绕过确认。
[[guard.allowed]]
pattern = "^(ls|pwd|git status|git diff)(\\s|$)"
tool = "exec"
reason = "常用只读命令直接放行"

# 只允许读取 docs 目录下的 markdown 文件。
[[guard.allowed]]
pattern = "^docs/.*\\.md$"
tool = "readfile"
reason = "允许读取文档"
```

注意事项：
- TOML 字符串里的反斜杠需要转义，例如 regexp 的 `\s` 要写成 `\\s`，`.` 字面量要写成 `\\.`。
- `tool` 为空表示匹配所有 guard target；建议给 `allowed` rule 显式写 `tool`，避免误放行其它工具。
- `blocked` 优先于 `allowed`。如果同一个 target 同时命中 blocked 和 allowed，最终是 reject。
- `allowed` 不能绕过 workspace、内置 blocked rule、用户 blocked rule。
- `readfile`、`listdir`、`readhttp` 通过 workspace 和 blocked 后通常是 low risk，会自动放行；需要额外限制时使用 `[[guard.blocked]]`。

### Workspace 边界

`guard.workspace` 为空时不启用限制。非空时，配置加载会将其展开为绝对路径并要求目录存在；配置非法会导致加载失败，避免安全配置写错后静默降级。

当前检查范围：
- `readfile.path`、`listdir.path`、`writefile.path`、`editfile.path` 必须解析到 workspace 内。
- `exec.cwd` 必须解析到 workspace 内；启用 workspace 后，未传 `cwd` 的 `exec` 会默认使用 workspace 根目录。
- `exec.command` 中明显的绝对路径、相对路径和重定向目标会按 `cwd` 解析，解析到 workspace 外则直接 reject。
- `exec.command` 中出现 shell 变量展开（如 `$HOME`、`${VAR}`）时无法可靠证明目标路径，会保守 reject。
- `readhttp`/`writehttp` 仍会经过 Guard，但 workspace 对 URL 不适用。

路径解析会处理 `~`、相对路径、绝对路径和 symlink；新建文件会解析最近存在的父目录，避免通过 workspace 内 symlink 写到外部。该机制是 Guard 级预执行检查，不是 OS sandbox，不能完全阻止程序运行后内部访问 workspace 外文件。

## LLM 审查 Prompt

当前 `smart` mode 的 LLM review 使用短 prompt，目标是让模型理解“工具调用是否服务当前任务”，而不是要求用户逐字指定命令。实际模板见 `internal/prompt/templates/guard_review.md`。

核心输入：

- `tool` / `risk` / `target` / 脱敏后的 `params`。
- 当前用户请求 `UserRequest`。
- 工具调用携带的 `ToolIntent`。
- 工具调用前 assistant 输出的简短 `AssistantContext`。
- 最近少量消息摘要 `RecentContext`。

输出 JSON：

```json
{"decision":"approve|reject|confirm|modify","reason":"short reason","suggestion":"optional safer alternative"}
```

决策语义：

- `approve`: 操作合理服务当前任务，且风险可接受。
- `reject`: 明确危险、恶意、偏离用户意图、访问/外传 secrets、提权、破坏系统或具有不安全外部副作用。
- `confirm`: 可能合理但上下文不足、范围过宽、影响不可逆或模型不确定。
- `modify`: 当前调用不应执行，但可以换成更安全/更窄的工具调用；suggestion 返回主 agent，让它重新规划。

LLM review 使用当前 active model，`Temperature=0`，`MaxTokens=180`。prompt 不注入完整对话和完整工具结果，只传短意图上下文，降低 token 占用和误判概率。

Guard 不再保存全局 `recentCtx`。每次 tool call 会由 runner/agent 构造不可变 `ReviewContext` 并传入 `Guard.Check()`，因此并发工具调用不会串上下文。`runner.ToolExecution` 会携带当前 runner 的 working messages 快照；main 使用 main working，subtask 使用自己的独立 working，让 smart review 判断当前执行单元的用户请求、tool intent、assistant context 和最近摘要。

## 审计日志

每次 Act 操作都记录审计日志，存入 SQLite：

```text
表: audit_log
| 字段 | 类型 | 说明 |
|---|---|---|
| id | TEXT | UUID |
| timestamp | DATETIME | 操作时间 |
| session_id | TEXT | 会话 ID |
| tool | TEXT | 工具名 |
| params | JSON | 操作参数 |
| risk_level | TEXT | low/medium/high |
| guard_decision | TEXT | approve/reject/confirm/modify |
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

## Subtask 的 Guard

Subtask 的 Guard 和 main 共享同一套全局安全策略，但 review 上下文使用 subtask 自己的 runner working：

- sub 继承同一套 mode、blocked/allowed、workspace、audit DB 和 LLM reviewer。
- sub 的 Guard review 使用每次 tool call 的 `ReviewContext`，包括 subtask 自己的 delegated task、最近 subtask 消息摘要、tool intent 和 assistant context；不会串用 main working memory。
- subtask 的 `tool_call` / `tool_guard` / `guard_confirm` / `tool_result` 使用一致的 namespaced tool id：`spawn:<parentToolCallID>:<subToolCallID>`，因此 TUI 可以把 Guard 决策、风险、reason 和用户确认结果挂到对应子工具行。
- 如果审查不确定，confirm 事件回到发起连接，由 TUI 展示给用户；用户回复通过 main 事件流恢复对应 subtask tool 执行。
- `modify` 不执行原调用，reason/suggestion 作为 tool error 返回 subtask LLM，由 subtask 自己决定是否按建议重新发起更安全/更窄的工具调用。

## 渐进信任

渐进信任尚未实现。未来可基于审计日志学习用户常规批准/拒绝模式，但不能绕过 workspace 和 blocked rules。
