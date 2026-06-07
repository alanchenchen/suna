# 08 — 技术选型与项目结构

## 语言: Go 1.22+

| 理由 | 说明 |
|---|---|
| 单二进制部署 | 用户下载即用，无 Node/Python 运行时依赖 |
| 并发模型 | goroutine + channel 天然适合 agent 调度 |
| 交叉编译 | GOOS/GOARCH 一键构建所有平台 |
| 长期稳定 | 适合长期运行的 daemon 进程 |
| 纯 Go 生态 | 所有依赖无 CGO，Windows/macOS/Linux 一致 |

## 直接依赖

```
模型通信 (2):
  github.com/openai/openai-go/v3              # OpenAI Responses + OpenAI-compatible Chat SDK
  github.com/anthropics/anthropic-sdk-go      # Anthropic Messages SDK

能力扩展 (待引入):
  github.com/mark3labs/mcp-go                 # MCP Client

Local transport (1):
  github.com/Microsoft/go-winio               # Windows Named Pipe (Docker 同款)

TUI (4):
  charm.land/bubbletea/v2                     # TUI 框架 (v2)
  charm.land/bubbles/v2                       # TUI 组件 (textarea/viewport/spinner)
  charm.land/lipgloss/v2                      # TUI 样式
  charm.land/glamour/v2                       # Markdown 渲染

事件触发 (待引入):
  github.com/robfig/cron/v3                   # Cron 调度 (Phase 2)
  github.com/fsnotify/fsnotify                # 文件监听 (Phase 2)

基础 (4):
  github.com/BurntSushi/toml                  # 配置文件
  modernc.org/sqlite                          # 记忆存储 (纯 Go, 无 CGO)
  github.com/google/uuid                      # UUID 生成
  internal/i18n                               # 国际化 (内置实现，非外部库)
```

全部 MIT 或 Apache-2.0 许可。全部纯 Go 无 CGO。

注：
- charm.land/v2 是 Charm 生态的 v2 API，接口与 v1 不兼容。bubbletea/v2 直接使用 Model 接口（Init/Update/View），bubbles/v2 提供组件。
- i18n 为内置实现 (internal/i18n)，key-value 翻译表，支持中英文，可从外部文件扩展。

### 跨平台注意事项

```
实现方式: Go 平台后缀文件 (编译时选择，非运行时判断)

  文件命名约定:
    xxx.go              # 平台无关代码
    xxx_unix.go         # macOS + Linux  (#go:build !windows)
    xxx_windows.go      # Windows        (#go:build windows)

  不用 runtime.GOOS 运行时判断，编译时自动选择正确实现
```

#### 跨平台文件分布

```
internal/protocol/
  ├── transport.go          # protocol.Transport 接口 (平台无关)
  ├── service.go            # protocol.Service / EventSink
  ├── methods.go            # 方法和事件名
  ├── messages.go           # 请求/响应/事件 payload schema
  └── multimodal.go         # MessagePart / AttachmentRef

internal/transport/local/
  ├── jsonrpc.go            # local transport 的 NDJSON + JSON-RPC framing
  ├── transport_unix.go     # Unix Socket (//go:build !windows)
  │   func NewPlatformTransport(path) → net.Listen("unix", path)
  └── transport_windows.go  # Named Pipe  (//go:build windows)
  │   func NewPlatformTransport(path) → winio.ListenPipe(path, ...)

internal/tool/
  ├── exec.go               # Exec 工具主体 (平台无关)
  ├── shell_unix.go         # Shell 检测: 默认 bash (//go:build !windows)
  │   func defaultShell() → "bash"
  │   func findShell(cmd) → 直接执行
  ├── shell_windows.go      # Shell 检测: Git Bash → PowerShell → cmd (//go:build windows)
  │   func defaultShell() → 检测 Git Bash 优先
  │   func findShell(cmd) → 语法分析 → 选择 shell
  └── translate_windows.go  # 命令翻译层 (Phase 2, //go:build windows)
      grep → findstr, cat → type, ls → dir, ...

internal/guard/
  ├── guard.go              # Guard mode policy + risk/check rules
  ├── rules_unix.go         # Unix 硬规则: rm -rf /, mkfs, dd ... (//go:build !windows)
  ├── rules_windows.go      # Windows 硬规则: rmdir /s /q, format ... (//go:build windows)
  └── sensitive.go          # 敏感信息检测
```

#### Local Transport 确定性坑

```
1. Windows 没有 Unix Socket → Named Pipe (go-winio, 微软官方, Docker 同款)
2. Windows Named Pipe 位于全局命名空间 → 默认 pipe 名带当前用户标识 hash，避免多用户/旧实例冲突
3. Windows Named Pipe ACL → 使用 go-winio 默认 ACL；不要使用 CO-only SDDL，否则部分环境会 `Access is denied`
4. Socket 残留文件 → daemon 启动时检测+清理 (尝试连接 → 连不上 → 删除残留)
5. NDJSON 分帧 → 每条 JSON 单行，\n 分隔，JSON 内用 \n 转义
6. 连接握手 → OnConnect 只登记 EventSink，不同步推送 full_status；TUI 通过 daemon.status/config.get 主动拉初始状态
7. 大消息阻塞 → local conn.Send 带 context timeout
```

#### Exec 工具 (os/exec)

```
Go 的 os/exec 本身跨平台无问题
风险点: LLM 生成的命令

Windows 上 LLM 高频错误:
  - 生成 grep/find/cat/rm → Windows 没有这些命令
  - 生成 PowerShell 语法 → LLM 不擅长
  - 缓解: 优先检测 Git Bash (大部分开发者有)
          System Prompt 注入 OS/Shell 信息

MVP 目标平台:
  macOS/Linux: 完全支持
  Windows:     需要 Git Bash (安装时检测并提示)
  裸 Windows:  Phase 2 (translate_windows.go 命令翻译层)

平台检测 (Exec shell=auto):
  Windows: 当前实现优先找 Git Bash，再回退 PowerShell/cmd；语法级自动转换仍是后续项
  macOS/Linux: 当前实现直接使用默认 bash/sh，不做命令语法检测
```

### 可选依赖 (Phase 2+)

```
  github.com/gorilla/websocket                # Stream 触发器 WebSocket 支持
```

### 明确不引入

```
❌ Go plugin      → 跨平台问题
❌ YAML 解析器    → 配置用 TOML，能力用 Markdown
❌ gRPC/protobuf  → local transport 用 JSON-RPC；Web transport 后续可用 HTTP/WebSocket/SSE
❌ goja           → ES5.1 限制大，LLM 生成代码需额外约束
❌ esbuild        → 需内嵌平台特定 native binary，违背单二进制原则
```

## 项目结构

```
suna/
├── main.go                      # 入口: CLI 分发、daemon runtime 组装、TUI 启动
├── daemon_cmd.go                # CLI daemon 管理: start/status/stop，优先走 protocol
├── daemon_process_unix.go       # 本机进程 fallback (macOS/Linux)
├── daemon_process_windows.go    # 本机进程 fallback (Windows)
├── go.mod
├── go.sum
├── internal/
│   ├── daemon/                  # Daemon runtime，不依赖具体 transport 实现
│   │   ├── daemon.go            # Daemon 主循环 (挂载抽象 transport、信号处理、PID 写入/删除)
│   │   └── lifecycle.go         # 自动退出策略 (无客户端 + 无感知源 → 退出)
│   ├── protocol/                # daemon 对外业务协议
│   │   ├── transport.go         # Transport 挂载接口
│   │   ├── service.go           # Service / EventSink
│   │   ├── methods.go           # method / notification 常量
│   │   ├── messages.go          # 请求/响应/事件 payload
│   │   └── multimodal.go        # MessagePart / AttachmentRef
│   ├── transport/local/         # 本机 transport：TUI/CLI
│   │   ├── client.go            # local protocol client，供 TUI 和 CLI 复用
│   │   ├── jsonrpc.go           # NDJSON + JSON-RPC framing
│   │   ├── transport_unix.go    # Unix Socket (//go:build !windows)
│   │   ├── transport_windows.go # Named Pipe  (//go:build windows)
│   │   └── framing_windows.go   # Windows Named Pipe line framing helpers (//go:build windows)
│   ├── core/                    # Agent 内核
│   │   ├── agent.go             # Agent struct + Run loop + 管理 API + Guard confirm
│   │   ├── agent_management.go  # NewSession/RestoreSession/newGuardForSession
│   │   ├── agent_memory.go      # 记忆提取逻辑
│   │   ├── agent_prompt.go      # System prompt 构建 + modelRoutingSummary
│   │   ├── agent_tools.go       # 工具执行逻辑 + spawn 校验 + confirmGuard
│   ├── model/                   # 多模型抽象 + 路由
│   │   ├── provider.go          # Provider 接口 + 消息/工具定义
│   │   ├── openai_responses.go  # OpenAI Responses 协议适配
│   │   ├── openai_chat.go       # OpenAI-compatible Chat Completions 适配
│   │   ├── anthropic.go         # anthropic-sdk-go 适配
│   │   ├── router.go            # 路由工具函数 (RouteWithLLM 已移除)
│   │   └── token.go             # token 估算
│   ├── media/                   # 图片 MediaRef 校验、attachment 落盘、provider 请求前 resolve
│   │   └── store.go
│   ├── tool/                    # 7 个 registry tools；askuser/spawn 由 core 特殊处理
│   │   ├── tool.go              # Tool 接口 + 注册
│   │   ├── readfile.go
│   │   ├── listdir.go
│   │   ├── readhttp.go
│   │   ├── exec.go              # Exec 工具主体
│   │   ├── shell_unix.go        # Shell 检测: 默认 bash (//go:build !windows)
│   │   ├── shell_windows.go     # Shell 检测: Git Bash → PS → cmd (//go:build windows)
│   │   ├── writefile.go
│   │   ├── editfile.go
│   │   ├── writehttp.go
│   ├── guard/                   # LLM 权限守卫
│   │   ├── guard.go             # Guard mode policy + Check() + checkAllowed/isReadOnlyTool
│   │   ├── rules_unix.go        # Unix 硬规则 (//go:build !windows)
│   │   ├── rules_windows.go     # Windows 硬规则 (//go:build windows)
│   │   └── sensitive.go         # 敏感信息检测
	│   ├── memory/                  # 轻量 active memory
	│   │   ├── store.go             # Store: SQLite 初始化 + schema
	│   │   ├── active.go            # user_memory 存取、召回和 compact diff
	│   │   ├── conversation.go      # 最近一轮会话恢复状态
	│   │   ├── working.go           # 工作记忆 (进程内)
	│   │   ├── session.go           # 用量记录 + legacy session helpers
	│   │   ├── compress.go          # 上下文压缩
	│   │   ├── queue.go             # memory_queue 写入 + pending 恢复
	│   │   ├── worker.go            # Memory Worker (异步 full compaction)
	│   │   └── significance.go      # 显著性判断 (规则，零 LLM)
│   ├── skill/                   # Skill 管理
│   │   └── skill.go             # 扫描/check/启用/load 通用 Agent Skills
│   ├── prompt/                  # 提示词模板
│   │   ├── loader.go            # go:embed 加载 + 模板渲染
│   │   └── templates/           # Markdown 模板 (编译进二进制)
│   │       ├── system.md        # 主 agent 系统提示词 (稳定前缀→动态内容排序)
│   │       ├── subtask_system.md # subtask 隔离 prompt (task/env/tools/context/rules)
│   │       ├── guard.md         # Guard 审查提示词
│   │       ├── compress.md      # 压缩摘要提示词
	│   │       └── extract_batch.md # active memory full compaction 提示词
│   ├── i18n/                    # 国际化
│   │   └── i18n.go              # key-value 翻译表 (中英文，可扩展)
│   ├── config/                  # 配置
│   │   ├── config.go            # TOML 加载/保存/验证 + 凭证管理
│   │   └── paths.go             # 默认数据目录和派生运行路径
│   └── tui/                     # 终端 UI (只持有 UI 状态，无模型/DB 业务逻辑)
│       ├── app.go               # Bubble Tea 主程序 + Setup Wizard
│       ├── chat.go              # 对话界面 + 键盘快捷键 + 命令补全
│       ├── commands.go          # TUI 命令 (/new, /model, /compact 等)
│       └── statusbar.go         # 状态栏 (模型/tokens/速度)
├── skills/                      # 内置示例 Skills
│   └── example/
│       └── SKILL.md
└── plans/                       # 设计文档
    └── *.md
```

## 用户数据目录

默认用户数据目录由 `internal/config/paths.go` 统一定义和派生。当前默认值是 `~/.suna/`；代码中不得在入口、daemon、transport、media、TUI 等模块重复手写 `$HOME/.suna`，应使用 `config.DefaultDataDir()`、`config.DefaultConfigPath()`、`config.DefaultPIDPath()`、`config.DefaultSocketPath()`、`config.DefaultAttachmentsDir()` 或 `Config` 上的路径方法。

```
~/.suna/
├── config.toml                  # 用户配置
├── credentials.toml             # API Keys (权限 0600，按 provider 维度)
├── sunad.pid                    # Daemon PID 文件
├── sunad.sock                   # Unix Socket (macOS/Linux)
├── memory.db                    # SQLite (记忆 + 审计 + 触发器)
├── tmp/                         # TUI 粘贴图片等短生命周期临时文件
├── skills/                      # 已安装/生成的通用 Agent Skills
│   ├── vue-style/
│   │   └── SKILL.md
│   └── deploy-helper/
│       ├── SKILL.md
│       └── scripts/
└── logs/
    ├── app.log                  # 标准库 log 输出 + 默认分类日志
    ├── agent.log                # Agent 运行、工具调用和生命周期日志
    ├── config.log               # 配置更新/重载日志
    ├── ipc.log                  # 本地 transport / JSON-RPC 日志
    ├── llm.log                  # Provider 请求摘要日志
    └── memory.log               # memory queue / compaction 日志
```

注：`attachments/` 当前主要保存 TUI 粘贴 `data:image/*` 后落盘的图片附件。`sunad.sock` 只对应 macOS/Linux 的 Unix Socket；Windows local transport 使用 Named Pipe，不会在默认数据目录下创建 socket 文件。若后续默认目录改为 XDG、macOS Application Support 或 Windows AppData，只修改 `internal/config/paths.go` 的默认路径策略和必要迁移逻辑。

## config.toml 当前支持参数

Suna 当前只读取默认数据目录下的 `config.toml` 和 `credentials.toml`，当前默认展开为 `~/.suna/config.toml` 和 `~/.suna/credentials.toml`。`config.toml` 保存模型、UI、Guard 和预留 hooks 配置；运行态路径 `DataDir` 不持久化。API key 不写入 `config.toml`，而是按 provider 维度写入 `credentials.toml`，文件权限为 `0600`。

### config.toml 完整示例

```toml
# 主代理使用的模型 (必填，格式为 "provider/model")
active_model = "glm/glm-4"

# 每个模型 ref 的默认请求限速。<= 0 时使用默认值 10。
max_model_rps = 10

# 模型列表，每个模型平级
[[models]]
provider = "glm"
model = "glm-4"
base_url = "https://open.bigmodel.cn/api/paas/v4"
context_window = 128000
strengths = ["后端", "Go", "API 开发", "通用"]

[[models]]
provider = "glm"
model = "glm-4-flash"
base_url = "https://open.bigmodel.cn/api/paas/v4"
strengths = ["快速响应", "轻量任务"]

[[models]]
provider = "anthropic"
model = "claude-sonnet-4-20250514"
base_url = "https://api.anthropic.com"
strengths = ["复杂推理", "长文写作", "代码审查"]

[[models]]
provider = "moonshot"
model = "moonshot-v1-auto"
base_url = "https://api.moonshot.cn/v1"
strengths = ["前端生成", "多模态", "图片理解"]

[[models]]
provider = "openai"
model = "gpt-4o"
base_url = "https://api.openai.com/v1"
strengths = ["通用", "多模态"]

[[models]]
provider = "deepseek"
model = "deepseek-v4-pro"
base_url = "https://api.deepseek.com/v1"
strengths = ["推理", "代码"]

[models.reasoning]
reasoning_effort = "max"

[models.reasoning.thinking]
type = "enabled"

[guard]
mode = "ask"                      # readonly | ask | auto | smart (默认 ask)
workspace = ""                     # 空表示不限制；非空时拦截 workspace 外本地文件/exec 操作

# 内置规则已覆盖危险操作 (rm -rf /, rmdir /s /q 等，按 OS 区分)
# 以下为用户自定义规则，追加到内置规则之上
[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

[[guard.allowed]]
pattern = "ls|cat|head|tail|grep|find|wc|dir|type"
tool = "exec"
reason = "只读命令直接放行"

[ui]
theme = "auto"                    # auto | dark | light
locale = "en"                    # "en" | "zh" | "zh-CN"

# Skill 管理记录：只记录用户是否启用，以及最近一次 check 的提示原因。
[skills.code-review]
enabled = true

[skills.deploy-helper]
enabled = false
reasons = ["includes scripts/ helper files", "contains network access commands"]

# MCP server 独立配置，不写入 Skill 包。
[mcp.servers.github]
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]

[mcp.servers.github.env]
GITHUB_TOKEN = "${GITHUB_TOKEN}"

# Hooks 当前为预留配置，结构已支持持久化，但执行闭环尚未完成。
[[hooks]]
event = "before_tool"
tool = "exec"
command = "echo checking"
```

上面示例覆盖 `config.toml` 的主要设计字段：`active_model`、`max_model_rps`、`[[models]]`、`[guard]`、`[ui]`、`[skills.<name>]`、`[mcp.servers.<name>]` 和预留 `[[hooks]]`。`DataDir` 与模型 `APIKey` 为运行态/凭证字段，不会写入 `config.toml`。

### 字段说明

| 字段 | 类型 | 必填 | 默认值 | 当前用途 |
|---|---|---|---|---|
| `active_model` | string | 否 | 第一个 `[[models]]` | 当前 daemon 默认模型，格式为 `provider/model`，必须能匹配某个模型配置。 |
| `max_model_rps` | int | 否 | `10` | 每个模型 ref 的请求限速，用于避免 subtask 并发打爆供应商。 |
| `[[models]]` | array | 是 | 无 | 至少需要一个模型，否则 daemon/TUI 进入配置向导。 |
| `models.provider` | string | 是 | 无 | provider 协议名，也是 `credentials.toml` 里 API key 的分组名。`openai` 表示 OpenAI Responses 协议，`anthropic` 表示 Anthropic Messages 协议，其它名称表示 OpenAI-compatible Chat Completions 协议。 |
| `models.model` | string | 是 | 无 | 模型 ID。模型 ref 为 `provider/model`。 |
| `models.base_url` | string | 是 | 无 | 该 provider 协议实际请求的 endpoint。daemon/core 不内置官方 URL，不读取 SDK 默认 endpoint；TUI 只在新建 `openai`/`anthropic` 时预填官方 URL，用户可改为中转站。 |
| `models.context_window` | int | 否 | `200000` | 上下文窗口，用于 daemon status、usage 展示和 compact 判断；运行时默认值由 `model.DefaultContextWindow` 统一维护，provider 的 `ContextWindow()` 是权威来源。TUI 会按 provider 显示默认值/placeholder，但未填写时不会自动写入 `config.toml`。 |
| `models.strengths` | string[] | 否 | 空 | TUI 展示模型擅长项。 |
| `models.reasoning` | object | 否 | 空 | 思考相关请求字段组。daemon/core 不理解 preset；provider 请求时将顶层字段注入最终 request body，并禁止覆盖已生成字段。TUI preset 负责生成该对象。 |
| `[guard].mode` | string | 否 | `ask` | `readonly` / `ask` / `auto` / `smart`。具体决策见 `plans/04-guard.md`。 |
| `[guard].workspace` | string | 否 | 空 | workspace 硬边界；非空时必须是存在目录。除 `askuser`/`spawn` 外所有 tool 都先过 Guard，文件类路径和 `exec.cwd`/明显命令路径解析到 workspace 外会直接 reject；默认数据目录（当前默认 `~/.suna`，由 config path 派生）允许访问以便排查配置/日志/Skill，但敏感文件规则仍会拦截 credentials；`exec` shell 变量展开无法安全检查时也会 reject。优先级高于 allowed、auto、LLM review 和用户确认。 |
| `[[guard.blocked]]` | array | 否 | 空 | 用户自定义硬拦截规则，追加到内置 blocked rules 后。 |
| `guard.blocked.pattern` | string | 是 | 无 | Go regexp，匹配命令、路径或 URL。 |
| `guard.blocked.reason` | string | 否 | 空 | 拦截原因，显示在 Guard 决策错误信息中。 |
| `[[guard.allowed]]` | array | 否 | 空 | 用户自定义允许规则，优先于 mode/risk，低于硬拦截。 |
| `guard.allowed.pattern` | string | 是 | 无 | Go regexp，匹配命令、路径或 URL。 |
| `guard.allowed.tool` | string | 否 | 空 | 限定 tool；为空表示匹配所有 guard target。 |
| `guard.allowed.reason` | string | 否 | 空 | 放行原因，当前字段可持久化；Guard 决策中暂未使用该 reason。 |
| `[ui].theme` | string | 否 | `auto` | TUI 主题：`auto` / `dark` / `light`。 |
| `[ui].locale` | string | 否 | `en` | TUI 语言；当前内置 `en` 和中文。 |
| `[skills.<name>].enabled` | bool | 否 | `false` | 是否允许加载该 Skill。只有 enabled=true 且 SKILL.md 有效时才进入 active skill index。 |
| `[skills.<name>].reasons` | string[] | 否 | 空 | check 发现的风险原因；无明显风险时可省略。 |
| `[mcp.servers.<name>]` | object | 否 | 空 | MCP server 配置，独立于 Skill；v1 优先支持 stdio。 |
| `mcp.servers.<name>.enabled` | bool | 否 | `false` | 是否启动该 MCP server。 |
| `mcp.servers.<name>.transport` | string | 否 | `stdio` | MCP transport，v1 先支持 `stdio`，HTTP/SSE 后续。 |
| `mcp.servers.<name>.command` | string | stdio 必填 | 空 | stdio server 启动命令。 |
| `mcp.servers.<name>.args` | string[] | 否 | 空 | stdio server 参数。 |
| `mcp.servers.<name>.env` | table | 否 | 空 | server 环境变量，可引用 `${ENV_NAME}`。 |
| `[[hooks]]` | array | 否 | 空 | Hook 配置结构已支持保存；执行链路仍是后续项。 |
| `hooks.event` | string | 否 | 空 | 预留 hook 事件名。 |
| `hooks.tool` | string | 否 | 空 | 预留 hook 作用 tool。 |
| `hooks.command` | string | 否 | 空 | 预留 hook shell command。 |

Guard rule 的 `pattern` 是 Go regexp，匹配当前 tool 的 guard target：`exec.command`、文件类工具的 `path`、HTTP 工具的 `url`。TOML 字符串中反斜杠需要转义，例如 `\s` 要写成 `\\s`，字面量 `.` 要写成 `\\.`。

常用示例：

```toml
[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

[[guard.blocked]]
pattern = "(^|/)private-notes(/|$)"
reason = "禁止读取私人笔记目录"

[[guard.blocked]]
pattern = "169\\.254\\.169\\.254|localhost|127\\.0\\.0\\.1"
reason = "禁止访问 metadata/local HTTP 服务"

[[guard.allowed]]
pattern = "^(ls|pwd|git status|git diff)(\\s|$)"
tool = "exec"
reason = "常用只读命令直接放行"
```

更完整的 rule target 映射和示例见 `plans/04-guard.md`。

### credentials.toml

`credentials.toml` 按 provider 保存 API key，同一 provider 下多个模型共享一个 key。该文件由 TUI 通过 protocol/config.set 写入，权限为 `0600`。

```toml
[glm]
api_key = "..."

[anthropic]
api_key = "..."

[moonshot]
api_key = "..."
```

注意：`models.provider` 必须和 `credentials.toml` 的 table 名一致，否则 `ResolveAPIKey()` 会返回缺失 key。`provider` 是协议适配器语义，不是官方厂商 endpoint：`openai` 走 OpenAI Responses 协议，`anthropic` 走 Anthropic Messages 协议，其它 provider 走 OpenAI-compatible Chat Completions 协议。所有模型都必须显式配置 `base_url`。

## 当前实现状态

| 模块 | 状态 | 当前能力 | 主要缺口 |
|---|---|---|---|
| Daemon / Protocol/Transport | Usable MVP | protocol schema、local transport、stream/config/session/guard 事件 | 多客户端边界和错误恢复仍需加强 |
| Model | Usable MVP | OpenAI Responses、OpenAI-compatible Chat 与 Anthropic provider；图片输入、tool calling、usage/context 透传；OpenAI/OpenAI-compatible 支持 streaming，并注册兼容 SSE decoder 跳过中转 heartbeat/empty event；`models.reasoning` 支持 TUI preset 与自定义注入；Anthropic 当前非 streaming | provider ping 和高级路由策略不完整 |
| Core Agent | Usable MVP | agent loop、provider-dependent streaming、tool call 并发执行、AskUser、Spawn、session 管理 | 更细的取消/并发边界和长期任务恢复 |
| Tools | Usable MVP | read/list/readhttp/exec/write/edit/writehttp/askuser/spawn | Windows 命令翻译层仍是后续项 |
| Guard | Usable MVP | `readonly` / `ask` / `auto` / `smart`、硬拦截、风险分级、TUI confirm、LLM review | rules 编辑 UI、modify 参数改写、渐进信任未完成 |
| Memory | Usable MVP | SQLite active memory、memory_queue、conversation_state、异步 full compaction、上下文压缩 | 记忆质量评估、用户可编辑记忆 UI |
| TUI | Usable MVP | Welcome/Chat/Config/Help、模型配置、Workspace 配置、工具记录、AskUser、Guard overlay、compact、active memory list、context-aware help | Provider test、Config 高级项（guard rules/hooks/限速）仍不完整 |
| Logging | Usable MVP | 分类文本日志和 provider 调用日志已接入；具体文件分类以 `internal/logging` 当前实现为准 | UI 查看日志、导出诊断包 |
| Skill / MCP | Skill Usable MVP / MCP Design | Skill runtime 已闭环：`~/.suna/skills`、enabled/reasons、`skill_load`、`skill_start import/check` 固定验收流程、`/skills` overlay；MCP 独立配置方案已定稿 | MCP stdio runtime 待实现 |

后续路线以 `plans/00-progress.md` 为准；本文件只记录当前技术选型、目录结构和配置字段。
