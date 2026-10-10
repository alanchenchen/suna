# Suna

[![Go](https://img.shields.io/badge/Go-1.26-blue)](https://go.dev) [![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE) [![Release](https://img.shields.io/github/v/release/alanchenchen/suna)](https://github.com/alanchenchen/suna/releases)

> 本地优先的通用 AI Agent：自由选择模型，按任务分工，在终端或官方 Web 客户端中处理文件、代码、资料与自动化。

Suna 在本机运行 Agent、工具和会话管理。你可以配置 OpenAI、Anthropic 或兼容协议的模型服务，让主 Agent 把边界清晰的工作委派给不同模型，并明确每个子任务获得的上下文和工具。

使用内置 TUI，或通过 **[Suna App](https://github.com/alanchenchen/suna-app)** 官方 Web 客户端连接同一个 Runtime。

[快速开始](#快速开始) · [官方 Web 客户端](#官方-web-客户端) · [文档](docs/README.md) · [English README](README.md)

本地优先指 Runtime 和持久化状态位于本机；模型请求仍会把上下文发送到你配置的模型服务。Suna 使用 [MIT 许可证](LICENSE)，允许商业使用。

<p align="center">
  <img src="docs/assets/tui-overview.png" alt="Suna 终端 UI：Agent 对话、工具活动与用量状态" width="74%" />
</p>

> 真实 Suna 会话：流式输出、工具活动、Guard 决策和用量状态。

## 为什么选择 Suna？

### 自己选择模型，按任务分工

每个会话保持自己选择的模型。主 Agent 可以为子任务选择另一已配置模型：让一个模型探索资料，另一个实现修改，再由另一个独立审查。`strengths` 描述模型适合的工作，`subtask_for` 控制哪些会话模型能看到它作为子任务候选。

这是任务级委派，不是悄悄替换主对话模型的黑盒路由。

### 每次委派，边界明确

每次 `spawn` 都必须指定模型、自包含的任务和工具白名单。只传入子任务需要的上下文和图片，授权指定工具，或不给任何工具、只做分析。子任务不默认继承主对话完整历史和用户记忆。

结果连同状态和副作用披露回到主 Agent。例如，让只读子任务浏览文件，再让无工具子任务评估方案，最后由主 Agent 汇总并决定下一步。

这里的隔离是模型上下文和工具授权隔离，不是独立文件系统沙箱。执行边界见 [Subtask](docs/subtask.md)。

### 终端与 Web，共用一个本地 Runtime

内置 TUI 和官方 Suna App 连接同一个 Runtime，由 Runtime 管理会话、Agent 运行、工具、Guard 和持久化。你也可以通过[公开协议](docs/tcp-client.md)开发自己的客户端。

Runtime 提供 macOS、Linux 和 Windows 的 Go 二进制；使用预构建版本无需额外安装 Node.js 或桌面运行环境。Suna App 独立安装和发版。

TUI 使用增量渲染和窗口化布局，减少长会话中的重复计算；可复现基准和已知限制见[性能文档](docs/performance.md)。

### 工具活动与审查决定可查看

TUI 展示工具调用、结果、子任务活动和 Guard 决策。Guard 结合硬检查、只读分析和可配置模式；`smart` 模式让 LLM 审查非只读 `exec` 调用。Skill 用于复用工作方式，MCP 用于连接外部工具。

Guard 是应用层防护，不是 OS sandbox。MCP server 属于需要用户信任的外部进程，当前 MCP 工具不经过 Guard 检查。启用前请阅读[安全边界](#安全边界)。

## 快速开始

### 安装

从 [GitHub Releases](https://github.com/alanchenchen/suna/releases) 下载预构建二进制：

- macOS Apple Silicon：`suna-darwin-arm64.zip`
- macOS Intel：`suna-darwin-amd64.zip`
- Linux x86_64：`suna-linux-amd64.tar.gz`
- Linux arm64：`suna-linux-arm64.tar.gz`
- Windows x86_64：`suna-windows-amd64.zip`

解压后把 `suna`（Windows 为 `suna.exe`）放到 `PATH` 中，然后运行：

```bash
suna
```

如果已经安装 Go，也可以从源码安装：

```bash
go install github.com/alanchenchen/suna@latest
suna
```

请确认 Go bin 已加入 `PATH`：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

不要使用 `go run .` 启动 Suna。daemon / TUI 的进程管理依赖稳定的可执行文件路径。

### 配置模型

1. 启动 `suna`，没有模型配置时进入 Config / Setup。
2. 添加模型连接，选择协议：
   - **OpenAI**：OpenAI Responses。
   - **Anthropic**：Anthropic Messages。
   - **OpenAI Compatible**：兼容 OpenAI Chat Completions 的模型服务。
3. 按服务的真实限制填写模型名、Endpoint、API Key、`context_window` 和 `max_output_tokens`。
4. 设置新会话默认模型，然后开始对话。

通过 `/config` 修改设置，通过 `/model` 只切换当前会话模型。修改默认模型不会改变已有会话。`strengths`、`subtask_for` 和推理参数见[配置说明](docs/configuration.md)。

主题可在 **配置 → 通用 → 主题** 中切换。内置 `default` 主题按终端背景自动适配，自定义主题从 `~/.suna/themes/*.toml` 读取。详见[主题](docs/themes.md)。

### 更新

退出正在使用 Runtime 的客户端，停止仍在运行的 daemon，再更新：

```bash
suna stop
suna update
```

更新器展示发布说明，并在确认后下载、校验 checksum 和替换二进制。

Suna 仍在积极开发中。升级或排障前请备份重要本地数据，不要把删除数据库作为常规恢复步骤。诊断可从 `~/.suna/logs/` 下的日志开始。

## 官方 Web 客户端

[Suna App](https://github.com/alanchenchen/suna-app) 是连接已安装 Suna Runtime 的官方浏览器客户端。Go Gateway 通过公开本地协议连接 Runtime；浏览器只与 Gateway 通信，不直接访问 Runtime 的 TCP socket。它不包含第二套 Agent 实现。

安装、构建和兼容要求见 [Suna App README](https://github.com/alanchenchen/suna-app#readme)。App 与 Runtime 独立发版，兼容性取决于 Runtime 协议和公开能力，不要求版本号相同。不要默认所有 TUI 功能都已经在 Web 客户端中提供。

## 试试这些任务

Suna 可以处理本地文档、代码、调研、诊断、API 和自动化，不只用于修改代码仓库。下面的例子展示如何明确分工与审查。

### 1. 找一个独立视角

```text
我需要在两个方案之间做选择。让一个子任务不使用工具，独立给出第二意见，然后你再给我建议。只传给它两个方案和评价标准。
```

你可以查看工具白名单为空的 `spawn` 调用；子任务只收到明确提供的上下文。

### 2. 探索资料，再审查结论

```text
帮我理解这个文件夹。把探索交给适合调研的模型，只授权 listdir、readfile 和 search。汇总发现后，再请另一个可用模型不使用工具，独立审查这份总结。
```

配置多个模型后可体验跨模型委派。主 Agent 从可见候选中选择模型，并整合结果。

<p align="center">
  <img src="docs/assets/tui-subtasks.png" alt="Suna 终端 UI：隔离 Subtask 时间线" width="66%" />
</p>

> 在主会话查看子任务的工具活动和结果。

### 3. 改文件前确认

要在工具层确认非只读操作，请在 `/config` 选择 Guard 的 `ask` 模式，并确认没有匹配的 allowed 规则绕过确认。默认 `smart` 模式不会逐次确认普通文件写入。

```text
帮我整理这份笔记，保存成更清晰的版本。先解释准备改什么，等待我确认后再写入。
```

这条 Prompt 要求 Agent 先讨论变更；`ask` 模式则在工具执行时另行要求确认。

## TUI 快速参考

```text
Enter              发送 / 确认
Shift+Enter        输入换行
Ctrl+J             输入换行
Esc                取消运行、返回或关闭浮层
↑ / ↓              输入框为空时召回上一条 / 下一条历史输入
Ctrl+T             展开 / 折叠视窗内的工具块
Ctrl+R             展开 / 折叠 reasoning 详情
?                  打开或关闭帮助
PgUp / PgDn        滚动；手动滚动后暂停跟随
Home               输入框为空时查看当前长回复的本轮开头
End                回到最新内容并恢复跟随
Ctrl+V             粘贴文本；终端未传入文本时可添加剪贴板图片
Ctrl+C             退出
```

常用 Slash 命令：

```text
/new              以新会话替换当前独占且空闲的会话
/sessions         加入其他位置的活跃会话或删除空闲会话
/model            打开当前会话的模型选择器
/model <ref>      切换当前模型，例如 /model openai/gpt-4o-mini
/memory           查看用户画像记忆
/mcp              打开 MCP 面板
/skills           打开 Skill 面板
/compact          手动压缩当前上下文
/config           打开配置页面
/help             打开帮助页
```

未注册的 `/文本` 会作为普通消息发送。

## 工具与扩展

| 工具 | 用途 |
|---|---|
| `readfile`、`listdir`、`search` | 按行范围、路径、结构或正文读取和探索本地文件 |
| `read_image` | 从本地路径、URL 或历史附件引用加载图片 |
| `writefile`、`editfile` | 写文件，或对单个文件原子应用精确替换 |
| `filesystem` | 查看、创建、移动、复制或删除文件系统路径 |
| `exec` | 执行命令；显式启动、查询或停止受管后台任务 |
| `http` | 发送 HTTP 请求并查看响应 |

[Skill](docs/configuration.md) 提供可复用指令和工作流；全局 Skill 导入经过静态检查、可选 LLM 审查和用户确认后启用。[MCP 配置](docs/configuration.md)用于接入可信 stdio server 提供的工具。

## 安全边界

对经过 Guard 的调用，硬检查先于 allowed 规则和模式策略；匹配的 allowed 规则可以在模式决策前批准调用。未匹配时：

```text
smart     默认。只读调用放行；非只读 exec 调用由 LLM 审核；其他写操作放行
ask       只读调用放行；非只读调用要求确认
auto      除硬性拦截外自动放行
readonly  只允许静态判定为只读的调用
```

Workspace 是应用层的可选路径边界，覆盖已接入的本地工具和命令中可识别的路径。Suna 默认数据目录仍可访问，但受敏感路径检查约束。当前 `read_image` 的本地来源未接入 Guard 的 workspace 检查。

Guard 不是 OS sandbox：命令分析不能限制脚本或程序运行时访问的全部路径。MCP 工具当前跳过 Guard，server 拥有其进程权限，只应启用可信 server。子任务工具白名单限制的是可调用工具，不是已授权命令或外部工具的全部副作用。

其他当前限制：

- 客户端在运行中断开后，没有完整事件回放；恢复会话不等于完整工具和思考输出归档。
- TUI 尚无完整历史搜索、向量知识库或完整多会话管理界面。
- 尚无完整定时任务、Trigger、文件监听或主动执行能力。
- MCP 仅支持 tools-only stdio 接入，不是完整 resources / prompts / sampling / OAuth 支持。
- 尚无 Skill sandbox、市场、完整 OS sandbox 或完整模型服务价格核算。

## 本地数据

```text
~/.suna/config.toml        # 模型与 Runtime 配置
~/.suna/credentials.toml   # API Key
~/.suna/memory.db          # 记忆、会话状态、用量和审计记录
~/.suna/skills/            # 全局 Skill
~/.suna/attachments/       # 图片和二进制附件
~/.suna/mcp-workdirs/      # stdio MCP server 默认工作目录
~/.suna/logs/              # 分类日志
```

持久化会话保留到用户明确删除，但不代表完整正文事件回放。Guard 审计明细和逐请求用量保留 30 天。SQLite 复用释放页面、不自动执行 `VACUUM`，因此清理后数据库文件不一定立即缩小。

项目 Skill 在会话 runtime 创建时，从其 cwd 到 Git worktree 根之间的兼容目录发现；由项目管理，不能在 Suna 中 toggle。

## 开发客户端或阅读代码

执行 `suna serve --json` 启动或发现 Runtime，并获取其权威 TCP endpoint。本地协议在 `runtime.hello` 后使用 JSON-RPC 风格 NDJSON，供可信本地客户端接入，不用于未经认证的远程暴露。

- [文档索引](docs/README.md)
- [客户端接入指南](docs/tcp-client.md)与 [Protocol](docs/protocol.md)
- [配置说明](docs/configuration.md)
- [Subtask](docs/subtask.md)
- [架构说明](docs/architecture.md)与[关键设计](docs/design.md)
- [性能](docs/performance.md)
- [代码地图](docs/code-map.md)与[当前实现](docs/current-implementation.md)
- [开发指南](docs/development.md)

## 许可证

Suna 使用 [MIT 许可证](LICENSE)，允许按其条款进行商业与非商业使用、修改和再分发。软件副本或实质性部分中须保留版权与许可声明。
