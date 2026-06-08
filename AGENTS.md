# AGENTS.md

## 项目理解

Suna 是本地终端 AI Agent：CLI 启动 TUI，TUI 通过 protocol/local transport 连接 daemon；daemon 持有 Agent、模型、工具、Guard、记忆、Skill、MCP 和持久化状态。TUI 只做交互与渲染，业务语义应留在 daemon/核心包。

## 技术栈

- Go CLI / daemon / 核心业务。
- Bubble Tea 体系 TUI，负责终端页面、按键和浮层。
- TOML 配置，本地 SQLite/文件目录保存记忆、会话、Skill、附件和日志。
- OpenAI / Anthropic / OpenAI-compatible 模型适配。
- JSON-RPC 风格 protocol 与本地 transport 通信。

## 主要包职责

- `main.go`、`daemon_cmd.go`：CLI 命令、daemon 进程管理入口。
- `internal/tui`：终端 UI、页面、快捷键、slash command、daemon 事件适配。
- `internal/protocol`、`internal/transport/local`：TUI 与 daemon 的请求、通知和本地连接。
- `internal/daemon`：长期运行服务，协调配置、会话、Agent、附件和状态通知。
- `internal/agent`：主 Agent 编排、上下文、工具执行入口、Guard 协调、subtask 委派。
- `internal/runner`：模型调用循环、流式输出、工具调用和上下文压缩。
- `internal/model`：模型 provider、路由、请求/响应适配和 token 估算。
- `internal/tools`：统一工具目录、Provider、schema 和执行路由。
- `internal/tools/builtin`：文件、命令、HTTP 等内置工具。
- `internal/tools/agenttools`：`askuser`、`spawn` 等 Agent runtime 工具。
- `internal/tools/skilltools`、`internal/skill`：Skill 加载、检查、启用和工具适配。
- `internal/tools/mcptools`、`internal/mcp`：MCP runtime、server 状态和 MCP tools 适配。
- `internal/guard`：风险识别、Smart Review 和工具调用安全确认。
- `internal/memory`、`internal/media`、`internal/config`：记忆、附件、配置和本地路径。
- `internal/subtask`：独立上下文的子任务执行器，由主 Agent 显式指定模型和工具。

## 架构规则

- TUI 不直接依赖 `agent`、`runner`、`tools`、`guard` 等业务包；交互必须走 protocol。
- 项目指令只从当前工作目录读取第一个非空文件，优先级为 `AGENTS.md`、`CLAUDE.md`、`GEMINI.md`、`.cursorrules`、`.windsurfrules`；不向父目录递归，不读取 `.suna/`。
- 新模型可见工具优先以 `tools.Provider` 接入，不在 Agent/Runner 中手动拼 schema。
- Guard 决策由 Agent 统一处理，工具只声明自身 Guard policy。
- Subtask 必须保持独立上下文，只能看到主 Agent 显式传入的任务、上下文、图片和授权工具。
- MCP 单个 server 失败不得阻塞 Suna/daemon 启动；错误应作为运行态状态展示。

## 开发规则

- 单个 Go 文件尽量不超过 700 行；接近上限时拆分职责清晰的小文件。
- 必要逻辑必须加中文注释，尤其是并发、状态机、错误恢复、安全边界、缓存稳定性和 protocol glue。
- 代码注释只能使用中文；提示词模板必须使用英文。
- 遵守 Go 推荐实践：小接口、清晰错误、显式依赖、避免全局隐式状态。
- 默认值、超时、路径、权限边界应放在所属层集中维护，不要跨层重复猜测。
- 不添加兼容旧逻辑的兜底代码，除非有明确迁移需求并在注释中说明。

## 测试与提交前检查

- 每次功能开发后必须运行 `gofmt` 或 `go fmt`。
- 每次功能开发后必须运行相关包测试；提交前优先运行 `go test ./...`。
- 是否运行 `go build ./...` 或 `go build -o suna .` 视改动范围决定；入口、构建、跨平台或集成改动建议运行。
- 测试风格遵循 `plans/14-test-style.md`：默认使用标准 `testing` 包，不为普通测试引入 assertion framework；测试文件放在被测包旁边，内部包优先使用同包测试。
- 测试应按行为命名，表驱动测试优先使用 `name`、`t.Run` 和 `tt := tt`；失败信息使用 `got` / `want`，默认用 `t.Fatal` / `t.Fatalf` 快速失败。
- 测试 helper 必须调用 `t.Helper()`；文件、环境和外部依赖分别使用 `t.TempDir()`、`t.Setenv()`、fake/stub 隔离。
- 普通 `go test ./...` 必须快速、离线、确定性；需要 daemon、网络、真实模型或其他外部依赖的测试必须使用 `integration` build tag。
- TUI 测试优先验证状态和关键语义文本，避免脆弱的整屏快照；检查渲染文本时应去除 ANSI。
- 提交前建议执行：`git diff --check`、`git status --short`、相关测试或全量测试。
