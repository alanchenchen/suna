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

IPC 通信 (1):
  github.com/Microsoft/go-winio               # Windows Named Pipe (Docker 同款)

TUI (3):
  charm.land/bubbletea/v2                     # TUI 框架 (v2)
  charm.land/bubbles/v2                       # TUI 组件 (textarea/viewport/spinner)
  charm.land/lipgloss/v2                      # TUI 样式

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
internal/ipc/
  ├── transport.go          # Transport + Conn 接口定义 (平台无关)
  ├── socket_unix.go        # Unix Socket (//go:build !windows)
  │   func NewPlatformTransport(path) → net.Listen("unix", path)
  ├── socket_windows.go     # Named Pipe  (//go:build windows)
  │   func NewPlatformTransport(path) → winio.ListenPipe(path, ...)
  └── server.go             # JSON-RPC Server (平台无关)

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
  ├── rules.go              # 硬规则接口 (平台无关)
  ├── rules_unix.go         # Unix 硬规则: rm -rf /, mkfs, dd ... (//go:build !windows)
  ├── rules_windows.go      # Windows 硬规则: rmdir /s /q, format, cipher ... (//go:build windows)
  └── readonly_unix.go      # Unix 只读白名单: ls, grep, find, cat ...
      readonly_windows.go   # Windows 只读白名单: dir, type, findstr, Get-ChildItem ...
```

#### IPC 确定性坑

```
1. Windows 没有 Unix Socket → Named Pipe (go-winio, 微软官方, Docker 同款)
2. Socket 残留文件 → daemon 启动时检测+清理 (尝试连接 → 连不上 → 删除残留)
3. NDJSON 分帧 → 每条 JSON 单行，\n 分隔，JSON 内用 \n 转义
4. TUI 重连 → daemon 推送 daemon.state 恢复显示
5. 大消息阻塞 → Conn.Send 带 context timeout
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
  Windows: bash 风格 → Git Bash, PowerShell 风格 → powershell.exe, cmd 风格 → cmd.exe
  macOS/Linux: 默认 bash
```

### 可选依赖 (Phase 2+)

```
  github.com/gorilla/websocket                # Stream 触发器 WebSocket 支持
```

### 明确不引入

```
❌ Go plugin      → 跨平台问题
❌ YAML 解析器    → 配置用 TOML，能力用 Markdown
❌ gRPC/protobuf  → IPC 用 JSON-RPC，MCP 也用 JSON-RPC
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
│   ├── ipc/                     # IPC 通信层
│   │   ├── transport.go         # Transport + Conn 接口定义
│   │   ├── socket_unix.go       # Unix Socket (//go:build !windows)
│   │   ├── socket_windows.go    # Named Pipe  (//go:build windows)
│   │   ├── client.go            # TUI 端 IPC Client
│   │   ├── client_unix.go       # Client Unix Socket 连接
│   │   ├── client_windows.go    # Client Named Pipe 连接
│   │   ├── server.go            # Daemon 端 JSON-RPC Server (方法路由)
│   │   └── message.go           # JSON-RPC 2.0 Message + Notification 定义
│   ├── core/                    # Agent 内核
│   │   ├── agent.go             # Agent struct + Run loop + 管理 API
│   │   ├── agent_memory.go      # 记忆提取逻辑
│   │   ├── agent_tools.go       # 工具执行逻辑
│   ├── model/                   # 多模型抽象 + 路由
│   │   ├── provider.go          # Provider 接口 + 消息/工具定义
│   │   ├── openai.go            # go-openai 适配
│   │   ├── anthropic.go         # anthropic-sdk-go 适配
│   │   ├── router.go            # LLM 路由 (strengths 偏好标签)
│   │   └── token.go             # token 估算
│   ├── tool/                    # 9 个核心工具
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
│   │   ├── guard.go             # 审查流程 (硬规则 + 风险评级)
│   │   ├── rules_unix.go        # Unix 硬规则 (//go:build !windows)
│   │   ├── rules_windows.go     # Windows 硬规则 (//go:build windows)
│   │   └── sensitive.go         # 敏感信息检测
│   ├── memory/                  # 分层记忆
│   │   ├── store.go             # Store: SQLite 初始化 + schema
│   │   ├── working.go           # 工作记忆 (进程内)
│   │   ├── episodic.go          # 情景记忆 (SQLite + FTS5 + 向量)
│   │   ├── semantic.go          # 语义记忆 (SQLite)
│   │   ├── entity.go            # 实体索引
│   │   ├── session.go           # 会话持久化 + 用量记录
│   │   ├── compress.go          # 上下文压缩
│   │   ├── queue.go             # 提取 channel + 恢复 (扫描 memory_extracted=0)
│   │   ├── worker.go            # Memory Worker (异步批量提取 goroutine)
│   │   └── significance.go      # 显著性判断 (规则，零 LLM)
│   ├── capability/              # 能力管理
│   │   └── capability.go        # 加载/存储/解析能力目录 + LOAD_SKILL 处理
│   ├── prompt/                  # 提示词模板
│   │   ├── loader.go            # go:embed 加载 + 模板渲染
│   │   └── templates/           # Markdown 模板 (编译进二进制)
│   │       ├── system.md        # 系统提示词模板
│   │       ├── guard.md         # Guard 审查提示词
│   │       ├── compress.md      # 压缩摘要提示词
│   │       └── extract.md       # 记忆提取提示词
│   ├── i18n/                    # 国际化
│   │   └── i18n.go              # key-value 翻译表 (中英文，可扩展)
│   ├── config/                  # 配置
│   │   └── config.go            # TOML 加载/保存/验证 + 凭证管理
│   └── tui/                     # 终端 UI (纯前端，无业务逻辑)
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

### 与文档设计的差异

```
实现 vs 设计的差异:

1. 入口: main.go 在根目录，不在 cmd/suna/
   → 单二进制直接 go build ./... 即可，不需要 cmd 子目录

2. prompt/ 目录: 新增
   → 模板通过 go:embed 编译进二进制，用户不可覆盖
   → 系统提示词是内核行为规范，改坏会失控

3. i18n/ 目录: 新增
   → 内置 key-value 翻译表，支持中英文
   → 从 config.toml [tui] locale 读取语言偏好

4. core/ 目录: 拆分为 agent.go + agent_memory.go + agent_tools.go
   → 按功能拆文件，单文件不过大

5. guard/ 目录: 新增 sensitive.go
   → 敏感信息检测（API Key 泄漏等）

6. memory/ 目录: 新增 session.go
   → 会话持久化 + 用量记录合并到 memory 包

7. 不存在的目录 (远期):
   runner/    → QuickJS WASM (Phase 3)
   mcp/       → MCP Client (Phase 3)
   trigger/   → 感知层 (Phase 2)
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

## config.toml 完整示例

```toml
# 主代理使用的模型 (必填，格式为 "provider/model")
active_model = "glm/glm-4"

# 模型列表，每个模型平级
[[models]]
provider = "glm"
model = "glm-4"
base_url = "https://open.bigmodel.cn/api/paas/v4"
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
enabled = true

# 内置规则已覆盖危险操作 (rm -rf /, rmdir /s /q 等，按 OS 区分)
# 以下为用户自定义规则，追加到内置规则之上
[[guard.blocked]]
pattern = "npm\\s+publish"
reason = "禁止发布 npm 包"

[[guard.allowed]]
pattern = "ls|cat|head|tail|grep|find|wc|dir|type"
tool = "exec"
reason = "只读命令直接放行"

[tui]
theme = "dark"
locale = "en"                    # "en" | "zh" | "zh-CN"
```

## MVP 开发阶段

与 index.md 保持一致。以下为详细拆解。

| Phase | 内容 | 周期 (1人) |
|---|---|---|
| **1** | Daemon 基础 + 记忆层基础：sunad + IPC (Unix Socket + JSON-RPC) + 仅添加式记忆 + FTS5 + embedding 自动发现 + 9 工具 + Guard stub | 5 周 |
| **2** | 行动层完善：Guard 完善 + 渐进信任 + 多模型路由 + 感知源 (Timer/Watcher/Webhook/Stream) | 4 周 |
| **3** | 记忆深化 + 学习：实体关联 + 时间推理 + 程序记忆 (skill 学习) + 能力系统 (SKILL.md + JS + MCP) | 4 周 |
| **4** | 完善 + 扩展：项目配置 (SUNA.md) + 模型表现追踪 + 会话持久化 | 4 周 |
| **5** | 探索: 意图层、WebSocket Transport、多 I/O 渠道、能力市场、Docker sandbox | — |

### Phase 1 详细拆解

```
Week 1: daemon/ + ipc/ + model/
  - Daemon 主循环: 启动/停止/PID 文件/socket 残留清理
  - Transport 接口 + Unix Socket 实现 (macOS/Linux)
  - JSON-RPC 2.0 Message 定义 + Server 方法路由
  - Provider 接口 + OpenAIProvider (go-openai)
  - Windows Named Pipe (go-winio) 基础实现

Week 2: core/ + tool/ + memory/
  - Agent.Run loop + Agent struct 组装
  - Tool 接口 + ReadFile, ListDir, Exec, WriteFile, EditFile
  - ReadHTTP, WriteHTTP, AskUser, Spawn
  - MemoryStore + SQLite 初始化 + FTS5
  - 提取队列 (memory channel + session_messages.memory_extracted) + Memory Worker goroutine
  - streaming completion + tool calling
  - embedding 自动发现 (检测 /v1/embeddings)

Week 3: guard/ + tui/
  - Guard stub: 硬规则 + isReadOnlyCommand + 全部放行 + 审计日志
  - 记忆检索: FTS5 (+ embedding 如果有)
  - 显著性过滤 (规则判断，零 LLM)
  - Bubble Tea TUI: IPC Client → 连接 daemon → 对话界面
  - 流式输出渲染 (接收 JSON-RPC notification)
  - TUI 命令解析 (/new, /model, /usage, /memory 等 → IPC)

Week 4: memory/ + 集成
  - 上下文压缩 (10 轮阈值)
  - /compact 命令 + 反馈
  - config/ TOML 加载
  - Daemon 自动退出策略 (无客户端 + 无感知源)

Week 5: 跨平台 + 测试
  - Windows Named Pipe 完整实现 + 测试
  - Daemon 重启后提取队列恢复
  - 端到端测试
  - Bug fix
```
