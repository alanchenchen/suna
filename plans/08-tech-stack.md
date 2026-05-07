# 08 — 技术选型与项目结构

## 语言: Go 1.22+

| 理由 | 说明 |
|---|---|
| 单二进制部署 | 用户下载即用，无 Node/Python 运行时依赖 |
| 并发模型 | goroutine + channel 天然适合 agent 调度 |
| 交叉编译 | GOOS/GOARCH 一键构建所有平台 |
| 长期稳定 | 适合长期运行的 TUI 进程 |
| 纯 Go 生态 | 所有依赖无 CGO，Windows/macOS/Linux 一致 |

## 直接依赖

```
模型通信 (2):
  github.com/sashabaranov/go-openai          # OpenAI SDK (覆盖 GLM/Qwen/Kimi/DeepSeek)
  github.com/anthropics/anthropic-sdk-go      # Anthropic SDK

能力扩展 (3):
  github.com/mark3labs/mcp-go                 # MCP Client
  github.com/nicholasgasior/quickjs-wasm-go   # QuickJS (编译为 WASM，ES2024 完整支持)
  github.com/tetratelabs/wazero               # 纯 Go WASM 运行时 (执行 QuickJS WASM)

TUI (2):
  charm.land/bubbletea/v2                     # TUI 框架
  github.com/charmbracelet/lipgloss           # TUI 样式

事件触发 (2):
  github.com/robfig/cron/v3                   # Cron 调度 (Timer)
  github.com/fsnotify/fsnotify                # 文件监听 (Watcher)

基础 (2):
  github.com/BurntSushi/toml                  # 配置文件
  modernc.org/sqlite                          # 记忆存储 (纯 Go, 无 CGO)
```

全部 MIT 或 Apache-2.0 许可。全部纯 Go 无 CGO。

### 可选依赖 (Phase 2+)

```
  github.com/gorilla/websocket                # Stream 触发器 WebSocket 支持
```

### 明确不引入

```
❌ Go plugin      → 跨平台问题
❌ YAML 解析器    → 配置用 TOML，能力用 Markdown
❌ CLI 框架       → 只有 TUI
❌ gRPC/protobuf  → MCP 用 JSON-RPC
❌ goja           → ES5.1 限制大，LLM 生成代码需额外约束
❌ esbuild        → 需内嵌平台特定 native binary，违背单二进制原则
```

## 项目结构

```
suna/
├── cmd/
│   └── suna/
│       └── main.go              # 入口: 初始化 → TUI
├── internal/
│   ├── core/                    # Agent 内核
│   │   ├── agent.go             # Agent struct + Run loop
│   │   ├── sub.go               # Sub agent 创建/管理
│   │   ├── context.go           # 上下文管理 + 压缩
│   │   └── prompt.go            # System prompt 组装
│   ├── model/                   # 多模型抽象 + 路由
│   │   ├── provider.go          # Provider 接口
│   │   ├── openai.go            # go-openai 适配
│   │   ├── anthropic.go         # anthropic-sdk-go 适配
│   │   ├── router.go            # 三层路由
│   │   └── token.go             # token 估算
│   ├── tool/                    # 9 个核心工具
│   │   ├── tool.go              # Tool 接口 + 注册
│   │   ├── readfile.go
│   │   ├── listdir.go
│   │   ├── readhttp.go
│   │   ├── exec.go
│   │   ├── writefile.go
│   │   ├── editfile.go
│   │   ├── writehttp.go
│   │   ├── askuser.go
│   │   └── spawn.go
│   ├── guard/                   # LLM 权限守卫
│   │   ├── guard.go             # 审查流程
│   │   ├── rules.go             # 硬规则
│   │   ├── risk.go              # 风险评级
│   │   └── audit.go             # 审计日志
│   ├── memory/                  # 分层记忆
│   │   ├── store.go             # MemoryStore 接口
│   │   ├── working.go           # 工作记忆 (进程内)
│   │   ├── episodic.go          # 情景记忆 (SQLite + 向量)
│   │   ├── semantic.go          # 语义记忆 (SQLite)
│   │   ├── failures.go          # 失败记忆 (SQLite)
│   │   ├── entity.go            # 实体索引
│   │   ├── embed.go             # Embedding (复用 provider)
│   │   └── compress.go          # 上下文压缩
│   ├── capability/              # 能力管理
│   │   ├── store.go             # 加载/存储能力目录
│   │   ├── parse.go             # 解析 SKILL.md
│   │   ├── inject.go            # 注入 system prompt
│   │   └── learn.go             # 能力学习
│   ├── runner/                  # JS 脚本引擎 (QuickJS + wazero)
│   │   ├── runner.go            # QuickJS WASM 封装
│   │   └── host.go              # host 函数 (→ agent tools，通过 WASM import)
│   ├── mcp/                     # MCP client
│   │   ├── client.go            # MCP 连接管理
│   │   └── tools.go             # MCP tools → Suna tools 适配
│   ├── trigger/                 # 事件触发层
│   │   ├── manager.go           # TriggerManager
│   │   ├── timer.go             # Cron 定时
│   │   ├── watcher.go           # 文件监听
│   │   ├── webhook.go           # HTTP 端点
│   │   └── stream.go            # 数据流消费
│   ├── config/                  # 配置
│   │   └── config.go            # TOML 加载/保存
│   └── tui/                     # 终端 UI
│       ├── app.go               # Bubble Tea 主程序
│       ├── chat.go              # 对话界面
│       ├── commands.go          # TUI 内命令 (/model, /skill 等)
│       └── theme.go             # 样式定义
├── capabilities/                # 内置示例能力
│   └── example/
│       └── SKILL.md
└── go.mod
```

## 用户数据目录

```
~/.suna/
├── config.toml                  # 用户配置
├── memory.db                    # SQLite (记忆 + 审计 + 触发器)
├── capabilities/                # 已安装能力
│   ├── vue-style/
│   │   └── SKILL.md
│   ├── log-parser/
│   │   ├── SKILL.md
│   │   └── main.js
│   └── database/
│       ├── SKILL.md
│       └── mcp.json
└── logs/
    └── audit.log                # 审计日志
```

## config.toml 完整示例

```toml
[models.default]
provider = "openai"
model = "glm-4"
base_url = "https://open.bigmodel.cn/api/paas/v4"
api_key_env = "GLM_API_KEY"
context_window = 128000

[models.fast]
provider = "openai"
model = "glm-4-flash"
base_url = "https://open.bigmodel.cn/api/paas/v4"
api_key_env = "GLM_API_KEY"

[models.reasoning]
provider = "anthropic"
model = "claude-sonnet-4-20250514"
api_key_env = "ANTHROPIC_API_KEY"

[models.kimi]
provider = "openai"
model = "moonshot-v1-auto"
base_url = "https://api.moonshot.cn/v1"
api_key_env = "MOONSHOT_API_KEY"
strengths = ["多模态", "前端生成", "图片理解"]

[router]
default = "default"
rules = [
  { pattern = "前端|页面|样式|CSS", model = "kimi" },
  { pattern = "逻辑|算法|后端", model = "default" },
  { pattern = "复杂推理|长文|写作", model = "reasoning" },
]

[guard]
enabled = true
review_model = "fast"

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
```

## MVP 开发阶段

与 index.md 保持一致。以下为详细拆解。

| Phase | 内容 | 周期 (1人) |
|---|---|---|
| **1** | 感知层 + 记忆层基础：TUI 进程 + 仅添加式记忆 + FTS5 + embedding 自动发现 + 9 工具 + Guard stub | 4 周 |
| **2** | 行动层完善：Guard 完善 + 渐进信任 + 多模型路由 + 感知源 (Timer/Watcher/Webhook/Stream) | 4 周 |
| **3** | 记忆深化 + 学习：实体关联 + 时间推理 + 程序记忆 (skill 学习) + 能力系统 (SKILL.md + JS + MCP) | 4 周 |
| **4** | 完善 + 扩展：项目配置 (SUNA.md) + 模型表现追踪 + 会话持久化 | 4 周 |
| **5** | 探索: 意图层、多 I/O 渠道、能力市场、Docker sandbox | — |

### Phase 1 详细拆解

```
Week 1: model/ + tool/ + memory/
  - Provider 接口 + OpenAIProvider (go-openai)
  - Tool 接口 + ReadFile, ListDir, Exec, WriteFile, EditFile
  - MemoryStore + SQLite 初始化 + FTS5
  - streaming completion + tool calling
  - embedding 自动发现 (检测 /v1/embeddings)

Week 2: core/ + tool/ + guard/
  - Agent.Run loop + Agent struct 组装
  - ReadHTTP, WriteHTTP, AskUser, Spawn
  - Guard stub: 硬规则 + isReadOnlyCommand + 全部放行 + 审计日志
  - 记忆提取: 每轮对话后调 fast 模型提取 → SQLite
  - 记忆检索: FTS5 (+ embedding 如果有)

Week 3: tui/ + memory/
  - Bubble Tea 对话界面 + 流式输出渲染
  - TUI 命令解析 (/new, /model, /usage, /memory 等)
  - 上下文压缩 (10 轮阈值)
  - /compact 命令 + 反馈

Week 4: 集成 + 测试
  - config/ TOML 加载
  - I/O 抽象层 (TUI 实现)
  - 端到端测试
  - Bug fix
```
