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
  github.com/sashabaranov/go-openai          # OpenAI SDK (覆盖 GLM/Qwen/Kimi/DeepSeek)
  github.com/anthropics/anthropic-sdk-go      # Anthropic SDK

能力扩展 (待引入):
  github.com/mark3labs/mcp-go                 # MCP Client (Phase 3)
  github.com/nicholasgasior/quickjs-wasm-go   # QuickJS (Phase 3, 编译为 WASM)
  github.com/tetratelabs/wazero               # 纯 Go WASM 运行时 (Phase 3)

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
2. Socket 残留文件 → daemon 启动时检测+清理 (尝试连接 → 连不上 → 删除残留)
3. NDJSON 分帧 → 每条 JSON 单行，\n 分隔，JSON 内用 \n 转义
4. TUI 重连 → daemon 推送 daemon.state 恢复显示
5. 大消息阻塞 → local conn.Send 带 context timeout
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
├── main.go                      # 入口: 检测 daemon → 启动/连接 → TUI
├── daemon_start_unix.go         # 后台启动 daemon (macOS/Linux)
├── daemon_start_windows.go      # 后台启动 daemon (Windows)
├── stop_unix.go                 # 停止 daemon (macOS/Linux)
├── stop_windows.go              # 停止 daemon (Windows)
├── go.mod
├── go.sum
├── internal/
│   ├── daemon/                  # Daemon 进程管理
│   │   ├── daemon.go            # Daemon 主循环 (启动/停止/信号处理)
│   │   └── lifecycle.go         # 自动退出策略 (无客户端 + 无感知源 → 退出)
│   ├── protocol/                # daemon 对外业务协议
│   │   ├── transport.go         # Transport 挂载接口
│   │   ├── service.go           # Service / EventSink
│   │   ├── methods.go           # method / notification 常量
│   │   ├── messages.go          # 请求/响应/事件 payload
│   │   └── multimodal.go        # MessagePart / AttachmentRef
│   ├── transport/local/         # 本机 transport：TUI/CLI
│   │   ├── jsonrpc.go           # NDJSON + JSON-RPC framing
│   │   ├── transport_unix.go    # Unix Socket (//go:build !windows)
│   │   └── transport_windows.go # Named Pipe  (//go:build windows)
│   ├── core/                    # Agent 内核
│   │   ├── agent.go             # Agent struct + Run loop + 管理 API + Guard confirm
│   │   ├── agent_management.go  # NewSession/RestoreSession/newGuardForSession
│   │   ├── agent_memory.go      # 记忆提取逻辑
│   │   ├── agent_prompt.go      # System prompt 构建 + modelRoutingSummary
│   │   ├── agent_tools.go       # 工具执行逻辑 + spawn 校验 + confirmGuard
│   ├── model/                   # 多模型抽象 + 路由
│   │   ├── provider.go          # Provider 接口 + 消息/工具定义
│   │   ├── openai.go            # go-openai 适配
│   │   ├── anthropic.go         # anthropic-sdk-go 适配
│   │   ├── router.go            # 路由工具函数 (RouteWithLLM 已移除)
│   │   └── token.go             # token 估算
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
│   │   ├── askuser.go
│   │   └── spawn.go
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
│   ├── capability/              # 能力管理
│   │   └── capability.go        # 加载/存储/解析能力目录 + LOAD_SKILL 处理
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
│   │   └── config.go            # TOML 加载/保存/验证 + 凭证管理
│   └── tui/                     # 终端 UI (只持有 UI 状态，无模型/DB 业务逻辑)
│       ├── app.go               # Bubble Tea 主程序 + Setup Wizard
│       ├── chat.go              # 对话界面 + 键盘快捷键 + 命令补全
│       ├── commands.go          # TUI 命令 (/new, /model, /compact 等)
│       └── statusbar.go         # 状态栏 (模型/tokens/速度)
├── capabilities/                # 内置示例能力
│   └── example/
│       └── SKILL.md
└── plans/                       # 设计文档
    └── *.md
```

## 用户数据目录

```
~/.suna/
├── config.toml                  # 用户配置
├── credentials.toml             # API Keys (权限 0600，按 provider 维度)
├── sunad.pid                    # Daemon PID 文件
├── sunad.sock                   # Unix Socket (macOS/Linux)
├── memory.db                    # SQLite (记忆 + 审计 + 触发器)
├── capabilities/                # 已安装能力 (含人格 persona/)
│   ├── persona/
│   │   └── SKILL.md             # 人格/沟通风格 (capability 实现)
│   ├── vue-style/
│   │   └── SKILL.md
│   ├── log-parser/
│   │   ├── SKILL.md
│   │   └── main.js
│   └── database/
│       ├── SKILL.md
│       └── mcp.json
└── logs/
    ├── suna.log                 # 应用日志
    └── audit.log                # 审计日志
```

## config.toml 当前支持参数

Suna 当前只读取 `~/.suna/config.toml` 和 `~/.suna/credentials.toml`。`config.toml` 保存模型、UI、Guard 和预留 hooks 配置；API key 不写入 `config.toml`，而是按 provider 维度写入 `credentials.toml`，文件权限为 `0600`。

### config.toml 完整示例

```toml
# 主代理使用的模型 (必填，格式为 "provider/model")
active_model = "glm/glm-4"

# 每个模型 ref 的默认请求限速。<= 0 时使用默认值 15。
max_model_rps = 15

# 模型列表，每个模型平级
[[models]]
provider = "glm"
model = "glm-4"
base_url = "https://open.bigmodel.cn/api/paas/v4"
context_window = 128000
cost_per_1k = 0.0
strengths = ["后端", "Go", "API 开发", "通用"]

[[models]]
provider = "glm"
model = "glm-4-flash"
base_url = "https://open.bigmodel.cn/api/paas/v4"
strengths = ["快速响应", "轻量任务"]

[[models]]
provider = "anthropic"
model = "claude-sonnet-4-20250514"
strengths = ["复杂推理", "长文写作", "代码审查"]

[[models]]
provider = "moonshot"
model = "moonshot-v1-auto"
base_url = "https://api.moonshot.cn/v1"
strengths = ["前端生成", "多模态", "图片理解"]

[guard]
mode = "ask"                      # readonly | ask | auto | smart (默认 ask)
review_model = ""                  # 预留；当前 LLM review 使用 active_model

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

# Hooks 当前为预留配置，结构已支持持久化，但执行闭环尚未完成。
[[hooks]]
event = "before_tool"
tool = "exec"
command = "echo checking"
```

### 字段说明

| 字段 | 类型 | 必填 | 默认值 | 当前用途 |
|---|---|---|---|---|
| `active_model` | string | 否 | 第一个 `[[models]]` | 当前 daemon 默认模型，格式为 `provider/model`，必须能匹配某个模型配置。 |
| `max_model_rps` | int | 否 | `15` | 每个模型 ref 的请求限速，用于避免 subtask 并发打爆供应商。 |
| `[[models]]` | array | 是 | 无 | 至少需要一个模型，否则 daemon/TUI 进入配置向导。 |
| `models.provider` | string | 是 | 无 | provider 名称，也是 `credentials.toml` 里 API key 的分组名。 |
| `models.model` | string | 是 | 无 | 模型 ID。模型 ref 为 `provider/model`。 |
| `models.base_url` | string | 否 | provider 默认 | OpenAI-compatible endpoint；OpenAI 官方 provider 可留空。 |
| `models.context_window` | int | 否 | provider 默认或 TUI fallback | 上下文窗口，用于顶栏展示和 compact 判断。 |
| `models.cost_per_1k` | float | 否 | `0` | 成本字段已持久化，当前 UI/计费统计未完整使用。 |
| `models.strengths` | string[] | 否 | 空 | TUI 展示模型擅长项。 |
| `[guard].mode` | string | 否 | `ask` | `readonly` / `ask` / `auto` / `smart`。具体决策见 `plans/04-guard.md`。 |
| `[guard].review_model` | string | 否 | 空 | 预留字段；当前 LLM review 实际使用 active model。 |
| `[[guard.blocked]]` | array | 否 | 空 | 用户自定义硬拦截规则，追加到内置 blocked rules 后。 |
| `guard.blocked.pattern` | string | 是 | 无 | Go regexp，匹配命令、路径或 URL。 |
| `guard.blocked.reason` | string | 否 | 空 | 拦截原因，显示在审计/错误信息中。 |
| `[[guard.allowed]]` | array | 否 | 空 | 用户自定义允许规则，优先于 mode/risk，低于硬拦截。 |
| `guard.allowed.pattern` | string | 是 | 无 | Go regexp，匹配命令、路径或 URL。 |
| `guard.allowed.tool` | string | 否 | 空 | 限定 tool；为空表示匹配所有 guard target。 |
| `guard.allowed.reason` | string | 否 | 空 | 放行原因，当前字段可持久化；Guard 决策中暂未使用该 reason。 |
| `[ui].theme` | string | 否 | `auto` | TUI 主题：`auto` / `dark` / `light`。 |
| `[ui].locale` | string | 否 | `en` | TUI 语言；当前内置 `en` 和中文。 |
| `[[hooks]]` | array | 否 | 空 | Hook 配置结构已支持保存；执行链路仍是后续项。 |
| `hooks.event` | string | 否 | 空 | 预留 hook 事件名。 |
| `hooks.tool` | string | 否 | 空 | 预留 hook 作用 tool。 |
| `hooks.command` | string | 否 | 空 | 预留 hook shell command。 |

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

注意：`models.provider` 必须和 `credentials.toml` 的 table 名一致，否则 `ResolveAPIKey()` 会返回缺失 key。

## 当前实现状态

| 模块 | 状态 | 当前能力 | 主要缺口 |
|---|---|---|---|
| Daemon / Protocol/Transport | Usable MVP | protocol schema、local transport、stream/config/session/guard 事件 | 多客户端边界和错误恢复仍需加强 |
| Model | Usable MVP | OpenAI-compatible 与 Anthropic provider、tool calling、usage/context 透传；OpenAI-compatible 支持 streaming，Anthropic 当前非 streaming | provider ping、成本统计、Anthropic usage/reasoning 映射和高级路由策略不完整 |
| Core Agent | Usable MVP | agent loop、provider-dependent streaming、tool call 并发执行、AskUser、Spawn、session 管理 | 更细的取消/并发边界和长期任务恢复 |
| Tools | Usable MVP | read/list/readhttp/exec/write/edit/writehttp/askuser/spawn | Windows 命令翻译层仍是后续项 |
| Guard | Usable MVP | `readonly` / `ask` / `auto` / `smart`、硬拦截、风险分级、TUI confirm、LLM review | rules 编辑 UI、modify 参数改写、渐进信任未完成 |
| Memory | Usable MVP | SQLite active memory、memory_queue、conversation_state、异步 full compaction、上下文压缩 | 记忆质量评估、用户可编辑记忆 UI |
| TUI | Usable MVP | Welcome/Chat/Config/Help、模型配置、工具记录、AskUser、Guard overlay、compact、active memory list | Provider test、Config 高级项和 Help 覆盖仍不完整 |
| Logging | Usable MVP | 分类文本日志和 provider 调用日志已接入；具体文件分类以 `internal/logging` 当前实现为准 | UI 查看日志、导出诊断包 |
| Capability | Basic | SKILL.md 加载和能力目录结构 | JS/WASM runner、MCP client、能力市场未完成 |

后续路线以 `plans/00-progress.md` 为准；本文件只记录当前技术选型、目录结构和配置字段。
