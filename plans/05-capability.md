# 05 — Skills 与 MCP

Suna 使用两个独立机制：

```text
Skill = 用户信任后的 Agent Skill 包，用来教 Suna 如何完成某类任务
MCP   = 外部工具 / 资源 / 服务接入层，用 config.toml 配置和启动
```

二者分离：Skill 不内嵌 MCP server 配置；MCP 不承担 Skill 的任务说明职责。

当前实现上，所有模型可见工具都通过统一工具系统注册：

```text
internal/tools.Manager
  ├── internal/tools/builtin     # editfile / exec / filesystem / http / listdir / readfile / search / writefile
  ├── internal/tools/skilltools  # skill_load / skill_start
  ├── internal/tools/agenttools  # askuser / spawn
  └── internal/tools/mcptools    # mcp__<server>__<tool>
```

MCP 已有基础 tools-only runtime：读取 `config.toml [mcp.servers.*]`，启动 enabled 的 stdio server，执行 initialize、tools/list 和 tools/call，并通过 `/mcp` 面板展示状态、启停和 reload。

## 设计原则

```text
1. Skill 兼容主流目录式 Skill 包；Suna 只要求 SKILL.md 存在。
2. Skill 是 prompt / 指令包，不会自动把 scripts/ 注册成新工具。
3. Skill 操作以自然语言对话为主；TUI `/skills` 只做简单管理入口。
4. Suna 在导入或验收 Skill 时做 static check，并可询问用户是否运行 LLM review。
5. 用户 enabled 后，Skill 可被 LLM 通过 skill_load 加载。
6. 不做复杂 Skill sandbox，不单独设计 script 权限系统；脚本若被执行，仍通过现有工具和 Guard。
7. MCP 是独立 runtime 能力，由 config.toml 配置；enabled MCP server 代表用户信任该外部 server。
8. MCP server 是不透明外部进程 / 服务；Suna 不承诺理解或限制其内部行为。
9. MCP 的连接和生命周期归 MCP runtime；MCP tools 通过 tools.Manager 暴露给模型。
```

## Skill 目录

默认 Skill 目录为：

```text
~/.suna/skills/<skill-name>/SKILL.md
```

实际路径由 Suna data dir 派生，即 `config.Config.SkillsDir()`。默认 data dir 下对应 `~/.suna/skills`。

兼容目录式 Skill：

```text
~/.suna/skills/code-review/
├── SKILL.md
├── references/     # 可选，参考文档
├── examples/       # 可选，示例
├── assets/         # 可选，模板/素材
└── scripts/        # 可选，辅助脚本
```

Suna 只要求 `SKILL.md` 存在。其他目录只是辅助资源，不会自动注册为新工具。

## SKILL.md 字段

Suna 当前只解析少量通用信息：

```markdown
---
name: code-review
description: Use when reviewing code, diffs, pull requests, bugs, security risks, or maintainability concerns.
---

# Code Review

Review correctness first, then security, then maintainability...
```

字段策略：

```text
name         可选；优先从 frontmatter 读取，其次从 H1 提取，最后使用目录名
              名称必须由字母、数字、-、_、. 组成，长度不超过 80

description  可选；优先从 frontmatter 读取，其次从正文首段提取

其他字段     不作为 Suna 行为依据；可以忽略或仅展示
```

未知字段不报错，但不赋予任何权限。

## config.toml 中的 Skill 记录

`config.toml` 保存轻量 Skill 管理信息：

```toml
[skills.code-review]
enabled = true

[skills.deploy-helper]
enabled = false
reasons = [
  "includes scripts/ helper files",
  "contains network access commands",
  "mentions sensitive environment variables or tokens"
]
```

字段含义：

```text
enabled  是否允许加载该 Skill
reasons  最近一次 workflow check / review 发现的风险原因；无明显风险时可省略
```

当前不设计：

```text
state / risk / script_policy / blocked / project_trusted / content hash / per-script permission
```

运行时状态来自文件系统和记录合并：

```text
enabled      config 中 enabled=true，且 SKILL.md 有效时可被 skill_load
inactive     config 中 enabled=false，或用户停用
invalid      SKILL.md 缺失、不可读、名称无效或重复，仅作为错误提示
missing      config 中有记录，但 skills 目录中找不到对应 Skill
```

当前实现还有一个重要规则：

```text
手动放入 skills 目录的新有效 Skill，如果 config 中没有记录，会在 Runtime reload/sync 时默认写入 enabled=true。
```

这表示“用户手动放入本地 skills 目录”被视为已信任行为。通过 `skill_start import/check` 进入的 Skill 则会经过 workflow 并询问是否启用。

## Skill check

check 只在这些场景触发：

```text
1. 用户通过 skill_start import 导入远程 repo / 本地目录 / zip
2. 用户通过普通文件工具创建或修改 Skill 后，调用 skill_start check
3. 用户明确要求重新验收已有 Skill 时，调用 skill_start check
```

TUI `/skills` 的启用 / 禁用只是切换 `enabled`，不会触发 check。

当前 static check 目标是发现明显风险和给用户解释，不是证明安全。检查内容包括：

```text
validate 标准结构和名称
扫描 SKILL.md 及辅助目录
跳过 .git / node_modules / vendor / dist / build / .cache 等生成或依赖目录
限制扫描文件数、单文件大小和总大小
检测明显风险：scripts/、危险命令、sudo、网络访问、敏感 token、敏感路径、prompt injection、二进制/混淆内容等
生成 reasons
```

`skill_start` workflow 在 static check 后会询问用户是否运行 LLM review；LLM review 是辅助阅读和解释，不是强安全证明。

典型 workflow：

```text
import 或 check existing directory
  ↓
static check
  ↓
询问是否运行 LLM review
  ↓
可选 LLM review
  ↓
询问是否启用
  ↓
写入 config.toml 的 enabled / reasons
```

空 `reasons` 只表示“未发现明显风险”，不表示绝对安全。

## Skill 加载与使用

### 启动 / reload 时

Skill runtime 做轻量工作：

```text
扫描 skills 目录
读取 SKILL.md 的 name / description / content
读取 config.toml 中的 skills 记录
生成 Skill registry / summary
同步手动放入的新有效 Skill 记录
```

不会每次启动都跑 LLM review，也不会要求用户重复确认。

### Prompt 组装

每轮用户消息前，Suna 只把 enabled 且 valid 的 Skill 索引放入系统上下文：

```text
Available Skills:
- code-review: Use when reviewing code, diffs, pull requests...
- weekly-report: Use when writing weekly reports in the user's format...
```

LLM 根据 `description` 自己判断是否需要某个 Skill。

### skill_load

`skill_load` 是统一工具系统中的 Skill 工具：

```text
skill_load(name)
```

只允许加载：

```text
enabled=true
SKILL.md valid
```

加载后，完整 `SKILL.md` 进入后续上下文。未启用、无效或缺失的 Skill 不进入上下文。

加载时 TUI 会显示 Skill loading / loaded 事件。

### skill_start

`skill_start` 是内置 Skill 验收 workflow 工具：

```text
skill_start(action, name, source)
```

支持：

```text
action = import  从本地目录 / zip / git/http/ssh URL 导入 Skill source
action = check   对已准备在 skills 目录下的 Skill 执行验收流程
```

`skill_start` 会走 static check、可选 LLM review、询问是否启用，并返回简化 summary JSON 给模型。

## Skill scripts

Suna 不为 Skill scripts 设计单独 sandbox 或 script policy。

规则：

```text
1. enabled=true 表示用户信任整个 Skill 包，包括 scripts/ 中的辅助脚本和 references。
2. scripts/ 不会自动注册为新工具。
3. LLM 如需运行脚本，必须通过现有工具，例如 exec；因此运行时仍经过现有工具系统和 Guard。
4. Suna 不承诺完全隔离或证明脚本安全。
5. 风险解释前置到导入 / check / 启用阶段。
```

这避免半吊子的运行时权限系统。Suna 的职责是 check、解释、记录用户选择，而不是伪装成完整 sandbox。

## Tool 与 Guard 边界

当前所有模型可见工具由 `tools.Manager` 管理，工具来源通过 `tools.Spec.Source` 标识。

Guard 策略由 `tools.Spec.Guard` 声明：

```text
builtin tools     默认走 Guard
askuser           GuardNever
spawn             GuardNever
skill_load        GuardNever
skill_start       GuardNever
future MCP tools  由 MCP 产品策略明确声明
```

当前 Guard 的精细风险识别主要覆盖 Suna 内置 7 个工具：

```text
exec       -> command
readfile   -> path
writefile  -> path
editfile   -> path
listdir    -> path
search     -> path
filesystem -> action path 或 action path -> destination
http       -> METHOD url
```

未知工具若走 Guard，会被视为 opaque medium risk，无法做内置工具级别的 path / command / url 精细判断。

## 对话式 Skill 操作

Skill 的主入口是自然语言，不是复杂 CLI。

典型用户表达：

```text
帮我导入这个 skill: https://github.com/user/skills
把 ~/Downloads/report-skill 加进来
把刚才这个流程保存成 skill
以后写周报都按这个格式
有哪些 skill 正在启用？
```

Suna 识别后进入内置 workflow：

```text
import existing source
  ↓
static check
  ↓
询问是否运行 LLM review
  ↓
可选 LLM review
  ↓
询问是否启用
  ↓
写 config.toml enabled/reasons
```

新建 Skill 时，main agent 先使用普通文件工具在 `~/.suna/skills/<name>/` 下准备 `SKILL.md` 和可选 `references/`、`examples/`、`assets/`、`scripts/`，然后调用 `skill_start check` 对已存在目录执行同一套验收 / 激活流程。

TUI 保留简单入口：

```text
/skills
```

用于查看 active / inactive / invalid / missing 与 issues，并支持启用、停用。TUI 的启用 / 停用只是切换 `enabled`，不会触发 check；重新验收通过对话或 `skill_start check` 完成。

## System Workflows vs User Skills

Suna 内部保留 system workflows，但它们不是普通 Skill：

```text
skill import flow
skill authoring flow
skill check flow
mcp setup flow
```

这些 workflow 内置在 Suna 中，用来识别用户意图、生成 Skill、执行检查并引导用户启用。它们不放在 `~/.suna/skills`，不走普通 `skill_load`，也不受 `[skills.<name>]` 配置管理。

普通 User Skills 才是：

```text
~/.suna/skills/<name>/SKILL.md
```

## MCP

MCP 是外部工具 / 资源 / 服务接入层，独立于 Skill。

MCP server 是不透明外部主体：

```text
本地 stdio server 会作为外部进程运行
远程 HTTP/SSE server 会作为外部服务被调用
Suna 可以控制是否启用/连接 server，以及是否把 tools 暴露给模型
Suna 不能可靠理解或限制 server 内部到底读写了什么、访问了哪里、执行了什么逻辑
```

因此 MCP v1 不设计复杂 per-tool 权限系统。推荐语义是：

```text
enabled=true 表示用户信任并启用该 MCP server
server 连接成功后，Suna 获取 tools/list 并注册为 Suna tools
模型可按工具 schema 调用这些 MCP tools
```

MCP 配置负责连接和生命周期；如果未来需要高级调用级安全策略，应放到统一 Guard rules，而不是塞进 MCP server 配置。

### 配置形态

建议放在 `~/.suna/config.toml`：

```toml
[mcp.servers.github]
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
# 当前不会展开 ${GITHUB_TOKEN}，需要写入实际值或由 server 自行读取环境。
env = { GITHUB_TOKEN = "<GITHUB_TOKEN>" }

# 远程 transport 配置字段可保存，但当前基础 MCP runtime 只支持 stdio。
[mcp.servers.context7]
enabled = false
transport = "streamable_http"
url = "https://mcp.context7.com/mcp"
```

字段职责：

```text
<mcp server id> 来自 [mcp.servers.<id>]，例如 github / context7
enabled          是否启动/连接该 server
transport        当前仅支持 stdio；sse / streamable_http 字段可保存但不会连接
command          stdio server 命令
args             stdio server 参数
env              显式传给 stdio server 的环境变量，当前按字面量传入，不展开 ${ENV_NAME}
url              SSE / Streamable HTTP endpoint，预留字段
headers          远程 transport 请求头，预留字段
```

不在 MCP server 配置中设计：

```text
per-tool allow / deny
per-call confirm
read/write 自动识别
server 内部 capability 伪权限
```

### MCP tools 命名与注册

MCP tools 通过 `internal/tools/mcptools.Provider` 接入：

```text
internal/mcp              # MCP 协议、transport、server lifecycle
internal/tools/mcptools   # MCP Runtime -> tools.Provider 适配
```

public tool name 需要稳定，建议：

```text
mcp__<server>__<tool>
```

例如：

```text
mcp__github__create_issue
mcp__context7__resolve_library_id
```

对应 `tools.Spec`：

```text
Name        = mcp__github__create_issue
Description = MCP tool description
Parameters  = MCP inputSchema
Category    = Act
Source      = {Kind: mcp, ID: github}
Metadata    = {mcp_tool: create_issue}
```

同一配置下 tool name、schema 和排序必须稳定，避免影响模型前缀缓存命中。

### MCP Guard 策略

MCP v1 的产品策略保持简单：

```text
启用 server = 信任 server
MCP tools 标记 GuardNever，启用 server 后不再逐次询问
不做 per-tool / per-call Guard 配置
不尝试把 MCP tool 自动归类为 read/write
```

原因：MCP server 是不透明外部主体。尤其是本地 stdio server，一旦启动就已经作为外部进程运行；逐次询问 tool call 并不能真正限制 server 内部行为。因此 v1 把信任边界放在 server 启用/启动阶段，而不是每次调用阶段。

UI / 文档必须明确说明：

```text
Suna 不会 sandbox MCP server，也无法检查其内部行为。
启用 MCP server 表示信任该 server 及其提供的工具。
```

若未来要做精细控制，应扩展统一 Guard rules 支持 `tool/source_kind/source_id`，例如：

```toml
[[guard.rules]]
decision = "block"
source_kind = "mcp"
source_id = "github"
tool = "delete_repository"

[[guard.rules]]
decision = "confirm"
source_kind = "mcp"
```

这属于高级安全策略，不放进 MCP 基础配置。

## MCP 阶段计划

### Phase 0 — 工具架构准备（已完成）

```text
统一 tools.Manager
所有模型可见工具通过 Provider 注册
builtin / skilltools / agenttools 已接入
Tool schema 按 name 稳定排序，保持缓存友好
```

### Phase 1 — stdio tools-only MVP（已完成基础闭环）

目标：能连接常见本地 stdio MCP server，并把 MCP tools 暴露给模型。当前已完成基础闭环：

```text
internal/mcp:
  - Runtime 管理多个 server
  - stdio process 启动 / stdin/stdout 通信 / stderr 日志
  - JSON-RPC request/response
  - initialize
  - notifications/initialized
  - tools/list
  - tools/call
  - Close / process cleanup

internal/tools/mcptools:
  - 将 MCP tool 转成 tools.Spec
  - 将 tools.Call 转成 MCP tools/call
  - 将 MCP result 转成 tools.Result
  - public tool name 稳定编码和解析

Agent:
  - 读取 config.toml MCP 配置
  - 创建 mcp.Runtime
  - 注册 mcptools.Provider
  - Agent Close 时关闭 MCP runtime
```

暂不支持：

```text
SSE / Streamable HTTP
resources
prompts
sampling
OAuth
dynamic tool refresh
sandbox
per-tool permissions
```

### Phase 2 — 生命周期与体验完善（部分完成）

```text
已完成：
启动失败不阻塞 Suna，只记录状态
TUI / chat 顶部展示 MCP server 状态
支持手动 activate/deactivate 与 reload MCP servers
reload 后刷新 tools.Manager

仍待完善：
日志记录 server stderr / crash / reconnect reason
配置变更后自动启动新增 enabled server
更清晰的错误恢复和诊断展示
```

状态示例：

```text
MCP: github ✓ filesystem ✕ context7 ✓
```

### Phase 3 — 远程 transport

完整 MCP transport 不建议全部手写到最终形态。建议：

```text
stdio MVP 可以自写
SSE / Streamable HTTP / OAuth 优先评估官方或主流 Go SDK
内部保留 Client/Transport 抽象，避免 SDK 选择影响 tools/mcptools 和 Agent
```

抽象方向：

```text
type Client interface {
  Start(ctx) error
  ListTools(ctx) ([]Tool, error)
  CallTool(ctx, name, args) (CallResult, error)
  Close(ctx) error
}
```

transport 支持顺序：

```text
1. stdio
2. streamable_http
3. sse
4. auth headers / bearer token
5. OAuth / token refresh（如 SDK 成熟再做）
```

### Phase 4 — MCP resources / prompts

MCP 不只有 tools。resources 和 prompts 需要单独设计，不应草率塞入现有 Skill 或文件工具语义。

后续设计问题：

```text
resources/list/read 是否暴露为 mcp resource read tools？
prompts/list/get 是否展示给模型，还是映射为 Suna workflow/skill-like prompt？
sampling 是否允许 MCP server 反向请求 Suna 模型？默认不建议开启。
roots / logging / progress notifications 如何展示？
```

Phase 4 前，Suna 只承诺 MCP tools。

### Phase 5 — 高级安全与 sandbox（可选）

没有一个轻量、靠谱、跨端、能跑任意 MCP server 的通用 sandbox。

可选方向：

```text
light hardening:
  - 不继承完整环境变量
  - 显式 env 白名单
  - cwd 限制到 workspace
  - no shell by default
  - process tree cleanup

native/container/wasm:
  - macOS sandbox-exec（experimental）
  - Linux bubblewrap / Landlock
  - Docker/Podman secure mode
  - WASM/WASI 用于未来安全插件，而不是通用 MCP 生态兼容
```

安全文案必须明确：

```text
Suna Guard 只能控制是否调用某个工具，不能隔离 MCP server 进程本身。
如果没有 sandbox，MCP server 拥有其运行环境允许的文件、网络和进程权限。
```

## Chat 状态展示

TUI 使用 `/skills` 打开 overlay，展示：

```text
Skills: 3 active / 5 total · 1 issue
● code-review      active
○ deploy-helper    inactive   2 reasons
```

用户可通过方向键选择，并用 Enter/Space 切换激活状态。正常情况不弹窗、不打断。LLM 调用 `skill_load` 时，TUI 显示 Skill loaded 消息。

MCP 状态后续可在 chat 顶部或状态区展示：

```text
MCP: github ✓ filesystem ✕
```

## 最小实现清单

```text
Skill Runtime / Manager:
  - scan skills dir
  - parse SKILL.md name/description/content
  - validate name and structure
  - static check / reasons
  - optional LLM review
  - import from git/local/zip
  - enable/disable
  - load enabled Skill content
  - sync manual local Skill records

Tools:
  - builtin.Provider
  - skilltools.Provider: skill_load / skill_start
  - agenttools.Provider: askuser / spawn
  - mcptools.Provider: mcp__<server>__<tool>

Agent/Runner:
  - prompt 注入 enabled skill index
  - 通过 tools.Manager 生成稳定 tool schema
  - 通过 tools.Manager 执行工具
  - Skill workflow 上下文仍由 Agent 提供

TUI:
  - /skills 简单管理页
  - skill_load loading / loaded 展示
  - /mcp 状态面板、activate/deactivate、reload

MCP Runtime:
  - Phase 1 stdio tools-only
  - Phase 2 lifecycle/status/reload
  - Phase 3 streamable_http / sse / auth
  - Phase 4 resources / prompts
  - Phase 5 optional guard rules / sandbox
```
