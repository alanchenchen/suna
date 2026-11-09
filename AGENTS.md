# AGENTS.md

## 项目理解

Suna 是本地优先的通用 AI Agent：CLI 启动 TUI，TUI 与第三方客户端通过 protocol 连接同一个 daemon；daemon 持有 Agent、模型、工具、Guard、记忆、Skill、MCP 和持久化状态。TUI 只做交互与渲染，业务语义应留在 daemon/核心包。

## 技术栈

- Go 写 CLI / daemon / 核心业务。
- Bubble Tea TUI，负责终端页面、按键、浮层、i18n 和主题。
- TOML 配置，本地 SQLite/文件保存记忆、会话、Skill、附件和日志；默认 `~/.suna`，日志在其 `logs/`；时间戳统一 UTC。
- 模型适配 OpenAI Responses、Anthropic Messages、OpenAI Chat 兼容。
- JSON-RPC 风格 protocol；local 走 Unix socket / Named Pipe，TCP 走 loopback 且需 `runtime.hello`，两者共用 `transport/jsonrpc`。

## 主要包职责

- 入口：`main.go`、`*_cmd.go`；`daemon_cmd.go` 管进程，`serve_cmd.go` 供第三方 TCP。
- 通信：`internal/protocol` 定义方法、参数、结果和通知；`internal/transport` 承载 local 与 TCP 两种连接。事件语义见架构规则。
- daemon：`internal/daemon` 单实例多 session 的长期运行服务，协调配置、会话、Agent、附件和状态通知。
- 核心：`internal/agent` 编排、上下文、工具入口、Guard 协调、subtask 委派；`internal/runner` 模型循环、流式、工具调用、上下文压缩、请求自动恢复；`internal/model` provider（只实现协议，router 只做路由）、请求响应适配、结构化错误、token 估算。
- 能力：`internal/tools` 是工具目录、Provider、schema 与执行路由；子包 `internal/tools/builtin` 放文件/命令/HTTP，`agenttools` 放 askuser/spawn，`skilltools` 适配 Skill，`mcptools` 适配 MCP。
- 运行时：`internal/skill` 负责加载、检查、启用；`internal/mcp` 是 stdio runtime 并维护 server 状态；`internal/guard` 做风险识别、Smart Review 和工具调用安全确认；`internal/subtask` 是独立上下文子任务执行器，由主 Agent 显式指定模型和工具。
- 状态：`internal/memory` 记忆、`internal/media` 附件、`internal/config` 配置与本地路径、`internal/prompt` 提示词模板加载、`internal/logging` 结构化日志、`internal/update` 自更新与 GitHub Release 版本检查、`internal/version` 构建版本来源。
- TUI：`internal/tui` 终端 UI、页面、快捷键、slash command、剪贴板输入；其子包 `pages/` 放页面状态，`components/` 依赖由调用方注入，`theme/` 是纯子系统，`events/` 把 notification 转强类型消息，`internal/tui/transport` 只做连接适配。

## 架构规则

- TUI 不直接依赖 `agent`、`runner`、`tools`、`guard` 等业务包，交互必须走 protocol。
- protocol 语义不混用：`agent.delta` 只承载 assistant/reasoning 增量，`agent.run` 承载 running/retrying/cancelling/done/failed/cancelled，`agent.usage` 承载 token/context/duration。
- 模型调用必须显式：Router 不拥有“当前模型”，按 `sessions.model_ref` 产出不可变 `ModelBinding`，主 run、Guard、Skill、compact 复用同一 binding，缺失必须显式失败，禁止回退 `active_model`；retry 依赖 `ModelError` 的 status code / kind，不靠错误字符串匹配。
- daemon 生命周期由 transport retention 决定：persistent > 活跃连接 > 运行中 run > client_bound > idle_exit，run 未结束不得退出；每 session 至多一个 active run；detach 不取消 run，只有无人可回复的 ask/guard 等待会被取消。
- 项目指令只读当前工作目录第一个非空文件，优先级 `AGENTS.md`、`CLAUDE.md`、`GEMINI.md`、`.cursorrules`、`.windsurfrules`；不递归父目录，不读 `.suna/`。
- 扩展点固定：新工具走 `tools.Provider`，新模型协议走 `model.Adapter`（router 只选择 adapter，协议文件只做请求响应适配，公共 reasoning fields 不 import 具体 SDK），不在 Agent/Runner 手动拼 schema；Guard 决策只由 Agent 处理，工具只声明自身 Guard policy。
- Subtask 保持独立上下文，只能看到主 Agent 显式传入的任务、上下文、图片和授权工具，返回状态、文本和副作用披露；可见模型由 `subtask_for` 过滤并在 spawn 时二次校验。
- MCP 单个 server 失败不阻塞启动，错误作为运行态状态展示；剪贴板与粘贴图片识别属 TUI 输入，daemon 只收 attachment refs。

## 开发规则

- 单个 Go 文件尽量不超过 700 行；接近上限时拆分职责清晰的小文件。
- 必要逻辑必须加中文注释，尤其是并发、状态机、错误恢复、安全边界、缓存稳定性和 protocol glue。
- 代码注释只能使用中文；提示词模板必须使用英文。
- 遵守 Go 推荐实践：小接口、清晰错误、显式依赖、避免全局隐式状态。
- 默认值、超时、路径、权限边界应放在所属层集中维护，不要跨层重复猜测。
- 不添加兼容旧逻辑的兜底代码，除非有明确迁移需求并在注释中说明。
- 公开仓库隐私：代码、文档、示例、提示词、用户可见文案和测试夹具不得写入真实或仿真的 API key、token、Cookie、Authorization 值、私钥、带凭据 URL 或其他敏感凭据；使用 `<API_KEY>`、`<TOKEN>`、`test-api-key` 等无真实格式的占位符。敏感信息识别/脱敏功能的测试可保留格式模式，但测试输入和断言不得含可用凭据。
- 公开仓库隐私：不得在代码、文档、示例、提示词、用户可见文案和测试夹具中固化私有、中转或内部模型提供商名称、域名、模型 ref 或配置；文档和用户可见内容优先使用 OpenAI、Anthropic 等官方提供商示例，通用兼容场景使用 `custom`、`example-provider`、`provider-a` 与 `https://api.example.com/v1`。新增或修改后应检索确认无残留。

## 测试与提交前检查

- 每次功能开发后必须运行 `gofmt` 或 `go fmt`。
- 每次功能开发后必须运行相关包测试；提交前优先运行 `go test ./...`。
- 是否运行 `go build ./...` 或 `go build -o suna .` 视改动范围决定；入口、构建、跨平台或集成改动建议运行。
- Suna 是跨平台应用；涉及操作系统差异的代码或测试必须保证支持平台行为正确，不得只依赖当前开发机语义。路径、换行符、Shell、进程、信号、权限、可执行文件和 build tag 等平台相关行为应使用可移植实现与断言，并按影响范围执行相应平台的测试或交叉构建。
- 测试风格遵循 `docs/development.md`：默认使用标准 `testing` 包，不为普通测试引入 assertion framework；测试文件放在被测包旁边，内部包优先使用同包测试。
- 测试应按行为命名，表驱动测试优先使用 `name`、`t.Run`；Go 1.22+ 循环变量语义已修正，无需 `tt := tt`；失败信息使用 `got` / `want`，默认用 `t.Fatal` / `t.Fatalf` 快速失败。
- 测试文件不写文件头注释（`// 以下测试覆盖...` 之类的清单）；解释性注释只写在测试函数内部，解释“为什么”而不是复述“测了什么”。
- 测试 helper 必须调用 `t.Helper()`；文件、环境和外部依赖分别使用 `t.TempDir()`、`t.Setenv()`、fake/stub 隔离。
- 普通 `go test ./...` 必须快速、离线、确定性；需要 daemon、网络、真实模型或其他外部依赖的测试必须使用 `integration` build tag。
- TUI 测试优先验证状态和关键语义文本，避免脆弱的整屏快照；检查渲染文本时应去除 ANSI。
- 提交前建议执行：`git diff --check`、`git status --short`、相关测试或全量测试。

## Git 提交规范

- 提交信息必须使用英文 Conventional Commits：`type(scope): imperative summary`。
- `type` 优先使用 `feat`、`fix`、`refactor`、`perf`、`docs`、`test`、`chore`；`scope` 使用稳定模块名，例如 `session`、`model`、`daemon`、`agent`、`tui`、`memory`、`protocol`、`config`。跨模块且没有清晰归属时可省略 scope。
- summary 使用简洁英文动词短语，首字母小写，不加句号；避免中文、泛泛的 `update` 和无意义的实现细节。
- 重要 feature、跨模块 fix、refactor、协议变更或兼容迁移必须附英文 body，说明设计意图、关键行为、兼容/迁移影响与用户可见变化；微小且局部的修改可只写 subject。
- 不兼容变更使用 `!`，并在 body 末尾使用英文 `BREAKING CHANGE:` footer 明确迁移要求，例如 `feat(protocol)!: replace legacy stream events`。

## 发版规则

- 版本号使用 SemVer tag，不再使用日期版本；示例：`v0.3.0`、`v0.3.1`、`v0.4.0`。
- 版本号来源以 Git tag 为准，不在代码或脚本里手动维护固定版本号。
- 让 Suna 代发版时，先根据变更范围建议版本号；用户确认后再继续创建 tag 和推送。
- 发版前必须确认工作区和测试状态，建议执行：`git status --short`、`git diff --check`、`go test ./...`。
- 发版必须使用 annotated tag，并在 tag message 中写中文 release notes；GitHub Actions 会把 tag message 作为 GitHub Release body。不要使用 lightweight tag。
- 推荐交互式创建 tag：`git tag -a v0.5.0`，在编辑器里写本次更新内容；也可用多段 `-m`：`git tag -a v0.5.0 -m "v0.5.0" -m "- 改进模型请求自动恢复"`。
- 推送顺序建议先推主分支再推 tag：`git push origin main`，然后 `git push origin v0.5.0`。也可以合并为 `git push origin main v0.5.0`。
- 推送 `v*` tag 会触发 GitHub Actions 自动构建 release assets、生成 `checksums.txt`、创建 GitHub Release 并上传产物。
- 发版后本地只需用 `git ls-remote --tags origin v0.5.0` 确认远端 tag 存在；不要默认使用 `gh` CLI 检查 workflow 或 release 状态，除非当前环境明确安装并已授权 `gh`。
