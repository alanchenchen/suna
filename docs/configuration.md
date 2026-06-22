# 配置说明

本文记录当前代码实际支持的 Suna 配置格式。默认数据目录为用户主目录下的 `.suna`：

```text
~/.suna/config.toml        # 主配置
~/.suna/credentials.toml   # 模型 API Key，按 provider 分组
~/.suna/skills/            # Skill 目录
~/.suna/attachments/       # 图片和 MCP 二进制结果附件
~/.suna/logs/              # 分类日志
```

`config.toml` 不保存模型 API Key；模型 API Key 写入 `credentials.toml`。运行态路径 `DataDir` 不会写入配置文件。

## 最小可用配置

```toml
active_model = "openai/gpt-4o-mini"

[[models]]
provider = "openai"
model = "gpt-4o-mini"
base_url = "https://api.openai.com/v1"
context_window = 128000
max_output_tokens = 8192
strengths = ["general", "fast", "multimodal"]

[guard]
mode = "ask"
workspace = ""

[ui]
theme = "auto"
locale = "en"
```

对应 `credentials.toml`：

```toml
[openai]
api_key = "sk-..."
```

## 完整示例

```toml
# 主 Agent 使用的模型。格式为 "provider/model"，必须匹配某个 [[models]]。
active_model = "openai/gpt-4o-mini"

# 每个模型 ref 的请求限速。省略或 <=0 时使用默认值 10。
max_model_rps = 10

[[models]]
provider = "openai"                         # OpenAI Responses 协议
model = "gpt-4o-mini"
base_url = "https://api.openai.com/v1"
context_window = 128000
max_output_tokens = 8192
strengths = ["general", "fast", "multimodal"]
# OpenAI Responses 风格 reasoning 扩展字段；不需要可省略。
reasoning = { reasoning = { effort = "medium" } }

[[models]]
provider = "anthropic"                      # Anthropic Messages 协议
model = "claude-sonnet-4-20250514"
base_url = "https://api.anthropic.com"
context_window = 200000
max_output_tokens = 8192
strengths = ["reasoning", "code review", "writing"]

[[models]]
provider = "deepseek"                       # 其它 provider 走 OpenAI-compatible Chat Completions
model = "deepseek-chat"
base_url = "https://api.deepseek.com/v1"
context_window = 64000
max_output_tokens = 8192
strengths = ["code", "cheap", "fast"]
# Chat-compatible 服务若支持 reasoning_effort，可按上游协议透传。
reasoning = { reasoning_effort = "high" }

[[models]]
provider = "minimax"
model = "MiniMax-M3"
base_url = "https://api.minimax.io/v1"
context_window = 1000000
max_output_tokens = 8192
reasoning = { reasoning_split = true }
# 可选：仅当主模型 ref 匹配这些 glob 时，MiniMax-M3 才作为 subtask 候选展示。
subtask_for = ["Froghire/**", "Oio/**"]

[[models]]
provider = "dreamfield"
model = "kimi-k2.6"
base_url = "https://example.com/v1"
context_window = 256000
max_output_tokens = 8192
strengths = ["multimodal", "long context"]
# 某些 OpenAI-compatible 服务使用 thinking 字段；是否有效取决于上游。
reasoning = { thinking = { type = "enabled" } }

[guard]
mode = "smart"                              # readonly | ask | auto | smart；空或非法时按 ask 使用
workspace = "~/Documents/project"           # 空表示不启用 workspace 边界；非空必须是存在目录

[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

[[guard.blocked]]
pattern = "169\\.254\\.169\\.254|localhost|127\\.0\\.0\\.1"
reason = "禁止访问 metadata/local HTTP 服务"

[[guard.allowed]]
pattern = "^(ls|pwd|git status|git diff)(\\s|$)"
tool = "exec"
reason = "常用只读命令直接放行"

[ui]
theme = "auto"                              # auto | dark | light
locale = "zh"                               # en | zh；其它值通常回退到英文文案

# Skill 管理记录通常由 /skills 或 skill_start 写入；手写时注意 section 名。
[skills.code-review]
enabled = true

[skills.deploy-helper]
enabled = false
reasons = ["includes scripts/ helper files", "contains network access commands"]

# 名称包含点号等特殊字符时需要引用。
[skills."needs.quote"]
enabled = false

# MCP server 独立配置，不写入 Skill 包。当前只支持 stdio tools-only。
[mcp.servers.filesystem]
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/Users/me/project"]
cwd = "/Users/me/project"
timeout_seconds = 30

[mcp.servers.github]
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
timeout_seconds = 30

# 注意：Suna 当前不会展开 ${GITHUB_TOKEN}；这里会作为字面量传给子进程。
# 如需 token，当前必须写实际值，或让 MCP server 通过其它方式读取。
[mcp.servers.github.env]
GITHUB_TOKEN = "ghp_xxx"

# URL / headers 字段可以被保存，但当前 MCP runtime 不支持远程 transport。
[mcp.servers.context7]
enabled = false
transport = "streamable_http"
url = "https://mcp.context7.com/mcp"
timeout_seconds = 30

[mcp.servers.context7.headers]
Authorization = "Bearer xxx"

# Hooks 结构可保存，但当前执行链路未接入。
[[hooks]]
event = "before_tool"
tool = "exec"
command = "echo checking"
```

对应 `credentials.toml`：

```toml
[openai]
api_key = "sk-..."

[anthropic]
api_key = "sk-ant-..."

[deepseek]
api_key = "..."

[dreamfield]
api_key = "..."
```

`models.provider` 必须和 `credentials.toml` 的 table 名一致。同一个 provider 下的多个模型共享一份 API key。

## 字段说明

| 字段 | 类型 | 必填 | 默认值 | 当前用途 |
|---|---|---:|---|---|
| `active_model` | string | 否 | 第一个 `[[models]]` | 主 Agent 默认模型，格式为 `provider/model`，必须匹配某个模型配置。 |
| `max_model_rps` | int | 否 | `10` | 每个模型 ref 的请求限速，避免 subtask 并发打爆供应商。保存时值为 0 会被省略。 |
| `[[models]]` | array | 是 | 无 | 至少一个模型，否则配置不可用。 |
| `models.provider` | string | 是 | 无 | provider 协议名，也是 credentials 分组名。`openai` 走 OpenAI Responses，`anthropic` 走 Anthropic Messages，其它名称走 OpenAI-compatible Chat Completions。 |
| `models.model` | string | 是 | 无 | 上游模型 ID。模型 ref 为 `provider/model`。 |
| `models.base_url` | string | 是 | 无 | API endpoint。当前所有 provider 都要求显式配置，Suna 不依赖 SDK 默认地址。 |
| `models.context_window` | int | 是 | 无 | 模型服务声明的总上下文窗口，按 `input + output` 理解；用于 status、usage 展示和 compact 预算。 |
| `models.max_output_tokens` | int | 是 | 无 | 模型服务允许的最大单次输出；所有 LLM 请求默认使用该值作为输出预算，且必须小于 `context_window`。 |
| `models.strengths` | string[] | 否 | 空 | 模型能力描述，会给主 Agent 参考，用于选择 subtask 模型。 |
| `models.subtask_for` | string[] | 否 | 空 | 子任务候选可见性过滤器；留空表示所有主模型可用，非空时 active model ref 匹配任一 glob 才展示，模型始终可作为自己的子任务模型。`*` 不跨 `/`，`**` 可跨 `/`。 |
| `models.reasoning` | object | 否 | 空 | 透传到 provider 请求体的额外 reasoning/thinking 字段；Suna 不理解 preset，是否有效取决于上游。 |
| `[guard].mode` | string | 否 | `ask` | `readonly` / `ask` / `auto` / `smart`。空或非法值按 `ask` 使用；`smart` 会对中高风险调用进行安全审查，而不是做普通 tool-call 优化。 |
| `[guard].workspace` | string | 否 | 空 | 本地文件和 exec 的目录硬边界；非空时必须是存在目录，会展开 `~/` 并规范化为绝对路径。 |
| `[[guard.blocked]]` | array | 否 | 空 | 用户自定义硬拦截规则，追加到内置 blocked rules 后。 |
| `guard.blocked.pattern` | string | 是 | 无 | Go regexp，匹配命令、路径或 URL。 |
| `guard.blocked.reason` | string | 否 | 空 | 拦截原因。 |
| `[[guard.allowed]]` | array | 否 | 空 | 用户自定义允许规则，优先于 mode/risk，低于 workspace、内置 blocked 和用户 blocked。 |
| `guard.allowed.pattern` | string | 是 | 无 | Go regexp，匹配命令、路径或 URL。 |
| `guard.allowed.tool` | string | 否 | 空 | 限定工具名；为空表示匹配所有 guard target。建议显式填写。 |
| `guard.allowed.reason` | string | 否 | 空 | 放行原因，当前主要用于配置可读性和持久化。 |
| `[ui].theme` | string | 否 | `auto` | TUI 主题：`auto` / `dark` / `light`。 |
| `[ui].locale` | string | 否 | `en` | TUI 文案语言：当前内置 `en` 和 `zh`。 |
| `[skills.<name>].enabled` | bool | 否 | `false` | 是否允许加载该 Skill。只有 enabled=true 且 SKILL.md 有效时才进入 active skill index。 |
| `[skills.<name>].reasons` | string[] | 否 | 空 | 最近一次 check/review 发现的原因或风险提示。 |
| `[mcp.servers.<name>]` | object | 否 | 空 | MCP server 配置，独立于 Skill。 |
| `mcp.servers.<name>.enabled` | bool | 否 | `false` | daemon 启动时是否尝试启动该 server；`/mcp` 面板也可运行态启停。 |
| `mcp.servers.<name>.transport` | string | 否 | `stdio` | 当前只支持 `stdio`；其它值可保存但启动会报 unsupported transport。 |
| `mcp.servers.<name>.command` | string | stdio 必填 | 空 | stdio server 启动命令，不经过 shell。 |
| `mcp.servers.<name>.args` | string[] | 否 | 空 | stdio server 参数。 |
| `mcp.servers.<name>.env` | table | 否 | 空 | 额外传给子进程的环境变量；当前不展开 `${VAR}`。 |
| `mcp.servers.<name>.cwd` | string | 否 | 空 | 子进程工作目录。 |
| `mcp.servers.<name>.timeout_seconds` | int | 否 | `30` | initialize、tools/list、tools/call 的超时。 |
| `mcp.servers.<name>.url` | string | 否 | 空 | 远程 transport 预留字段；当前不用于实际连接。 |
| `mcp.servers.<name>.headers` | table | 否 | 空 | 远程 transport 预留字段；当前不用于实际连接。 |
| `[[hooks]]` | array | 否 | 空 | 预留 hook 配置，可保存但当前不会执行。 |
| `hooks.event` | string | 否 | 空 | 预留 hook 事件名。 |
| `hooks.tool` | string | 否 | 空 | 预留 hook 作用工具。 |
| `hooks.command` | string | 否 | 空 | 预留 hook shell command。 |

## 模型配置细节

### provider 与协议

- `provider = "openai"`：使用 OpenAI Responses 协议。
- `provider = "anthropic"`：使用 Anthropic Messages 协议。
- 其它 provider 值：使用 OpenAI-compatible Chat Completions 协议。

`provider` 不是厂商名的固定枚举。你可以写 `deepseek`、`glm`、`dreamfield` 等，只要：

1. `base_url` 指向兼容服务；
2. `credentials.toml` 中存在同名 table；
3. `active_model` 使用同样的 `provider/model` ref。

### context_window 与 max_output_tokens

`context_window` 和 `max_output_tokens` 都是必填的模型能力参数。Suna 不维护内置模型能力库，也不再为这两个字段提供运行时默认值；缺失或非法时配置加载/保存会失败。

语义约定：

```text
context_window = input tokens + output tokens 的总窗口
max_output_tokens = 单次请求可用的最大输出预算，也是输入预算里的完整输出预留
usable_input_budget ≈ context_window - max_output_tokens - margin
compact_context_tokens = estimated_context_tokens + estimator_safety_tokens
```

其中 `margin` 是小的 context 边界余量：`max(2048, context_window / 200)`。自动 compact 判断不会直接使用 provider 返回的 usage，而是使用 Suna 请求前估算的输入上下文：

```text
estimated_context_tokens = Suna 对本次请求输入上下文的本地估算
estimator_safety_tokens = max(8192, estimated_context_tokens / 16)
compact_context_tokens = estimated_context_tokens + estimator_safety_tokens
```

当 `compact_context_tokens > usable_input_budget` 时，Suna 会主动 compact。TUI 的 `ctx` 展示使用 raw `estimated_context_tokens`；provider 返回的 `input_tokens` / `context_tokens` 保留用于 usage 统计、计费对账和诊断。Suna 不再使用 `context_window * 0.8` 这类大比例保守阈值，也不依赖 provider context overflow 后的 fallback retry 作为常规策略。

所有 LLM 请求都会默认使用当前模型配置的 `max_output_tokens`，包括主 chat、subtask、Guard smart review、Skill review、Session State compact 和用户画像 memory compact。prompt/schema 负责约束内部请求输出格式；`max_output_tokens` 表示硬输出上限，同时也是上下文预算里的完整输出预留。这个策略更安全但会更早触发 compact；Suna 当前不提供单独的 `reserved_output_tokens` / `default_output_tokens` 字段。

填写建议：

- 优先使用模型服务或中转控制台实际生效的限制，而不是只看模型官方宣传值。
- 如果服务文档写的是 `400k context / 128k max output`，则填写 `context_window = 400000`、`max_output_tokens = 128000`。Suna 会先完整预留 128k 输出空间，再扣除 margin 和 estimator safety 规划 compact。
- 如果某个服务只给出一个总上下文窗口，没有明确 max output，需要先查服务层默认/上限；不要随意留空。

### reasoning 写法

`models.reasoning` 会作为额外 JSON 字段透传给上游请求。Suna 只检查不要覆盖它已经生成的字段，例如 `model`、`messages`、`tools`、`temperature` 等。

推荐写成 inline table，便于和对应 `[[models]]` 绑定：

```toml
[[models]]
provider = "openai"
model = "gpt-5"
base_url = "https://api.openai.com/v1"
context_window = 400000
max_output_tokens = 128000
reasoning = { reasoning = { effort = "high" } }

[[models]]
provider = "deepseek"
model = "deepseek-reasoner"
base_url = "https://api.deepseek.com/v1"
context_window = 64000
max_output_tokens = 8192
reasoning = { reasoning_effort = "high" }

[[models]]
provider = "dreamfield"
model = "kimi-k2.6"
base_url = "https://example.com/v1"
context_window = 256000
max_output_tokens = 8192
reasoning = { thinking = { type = "disabled" } }
```

不要把 reasoning 写成会覆盖请求核心字段的形式，例如：

```toml
# 错误：model 是 Suna 生成的请求字段，会冲突。
reasoning = { model = "other-model" }
```

## Guard 配置细节

`pattern` 是 Go regexp，匹配当前 tool 的 guard target：

| Tool | Pattern 匹配对象 |
|---|---|
| `exec` | `command` |
| `readfile` / `listdir` / `writefile` / `editfile` / `search` | `path` |
| `filesystem` | `action path`；`move` / `copy` 带 destination 时为 `action path -> destination` |
| `http` | `METHOD url`，未传 method 时默认为 `GET` |

例如 `http` 的 target 可能是 `GET https://example.com` 或 `DELETE https://api.example.com/items/1`。旧的 `readhttp` / `writehttp` 已合并为 `http`，如果规则依赖 URL 开头匹配，需要考虑 method 前缀。

TOML 字符串中的反斜杠需要转义，例如 regexp 的 `\s` 要写成 `\\s`，字面量 `.` 要写成 `\\.`。

Workspace 启用后是最高优先级硬边界：workspace 外本地文件路径和明显 exec 路径会被拒绝，不能被 `allowed` 规则绕过。它不是 OS sandbox，不能限制外部程序运行后自己访问什么。

`smart` mode 的 LLM Review 只负责安全、用户意图和权限边界判断：安全且合理的中高风险调用可以直接放行；不确定或影响较大时请求确认；明确危险时拒绝；只有当前调用不安全或明显过宽，并且有具体等价的更安全调用时才返回修改建议。它不用于修正普通参数风格、代码风格或常规 tool-call 优化。

## MCP 配置细节

当前 MCP 是基础 tools-only runtime：

- 支持 stdio server；
- 支持 initialize、tools/list、tools/call；
- MCP tools 会注册为 `mcp__<server>__<tool>`；
- 单个 server 启动失败不会阻塞 Suna；错误在 `/mcp` 面板显示；
- 不支持 resources、prompts、sampling、OAuth、远程 transport 或 sandbox。

MCP 子进程环境变量边界：

- Suna 默认只继承少量环境变量：`PATH`、`HOME`、`LANG`、`LC_*`、`LC_ALL`、`TMPDIR`、`TEMP`、`TMP`。
- `[mcp.servers.<name>.env]` 会额外传入字面量值。
- 当前不会展开 `${ENV_NAME}`。如果写 `GITHUB_TOKEN = "${GITHUB_TOKEN}"`，子进程看到的就是这串字面量，除非 MCP server 自己再解释。

MCP server 是外部不透明进程。启用 server 表示用户信任它；Guard 只能控制 Suna 是否调用暴露出来的工具，不能隔离 server 进程内部的文件、网络或进程权限。

## Hooks 配置

`[[hooks]]` 当前只是可持久化结构，执行链路尚未接入。可以保留配置，但不要依赖它运行。
