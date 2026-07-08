# Suna

> Local-first Agent Runtime：隔离 Subtask、意图感知 Guard、本地记忆、Skill、MCP，以及内置终端 TUI。

[English README](README.md) · [文档索引](docs/README.md) · [stdio runtime](docs/runtime-stdio.md) · [Subtask 设计](docs/subtask.md)

Suna 不只是“又一个终端聊天 Agent”。它更像一个运行在本地的 Agent Runtime：主 Agent 可以把任务委派给**隔离的 Subtask**，为每个子任务单独指定模型、上下文、图片和工具权限；高风险操作仍然经过**意图感知 Guard** 审查。

“Suna” 来自 śūnya，意为“空”：出厂无形，遇缘则生。这个名字也对应 Suna 的设计取向：轻量、克制，不预设复杂工作流，而是在本地 runtime、工具链路、记忆和 Skill 中逐渐贴合你的使用方式。

内置 TUI 是默认客户端。相同 runtime 也可以通过 `suna runtime --transport stdio` 和 JSON-RPC/NDJSON 接给第三方桌面端、IDE 插件、本地 Web UI 或脚本。

> Suna 目前处于快速开发状态。如果升级或使用过程中遇到功能失效，建议先升级到最新版本，并在备份必要数据后清理 Suna 数据目录中的 `.db` 文件。

## 为什么是 Suna？

### 隔离 Subtask

很多终端 Agent 是“一个模型 + 一份共享上下文 + 一套全局工具”。Suna 的主 Agent 可以在运行中创建边界清晰的子任务：

- 为某个子任务选择不同模型；
- 只传入明确的任务、上下文和图片；
- 只授权指定工具，甚至不授权任何工具；
- 默认不继承主对话完整历史和用户长期记忆；
- 子任务结束后返回状态、结果、错误和副作用披露，由主 Agent 汇总决策。

这样 Subtask 不是失控的“第二个 Agent”，而是可解释、可审查、可限制的委派单元。

### 意图感知 Guard

Suna 的 Guard 不只是简单的确认弹窗。`smart` 模式下，硬性安全规则和 Workspace 边界仍然生效；中高风险操作可以交给 LLM Review 判断是否安全、是否符合用户意图。如果审查不可用或不确定，Suna 会回退到用户确认。

它的目标不是消灭确认，而是在不放松高危边界的前提下，减少无意义打断。

### Runtime-first 架构

Suna 把 UI 和 Agent Runtime 分开：

```text
内置 TUI / 第三方 UI / 脚本
        ↓ local 或 stdio transport 上的 JSON-RPC
Daemon / Runtime
        ↓
Main Agent / Runner
   ├─ model providers
   ├─ tools / Guard / memory / Skills / MCP
   └─ isolated subtasks
        ├─ explicit model
        ├─ explicit context
        └─ explicit tool permissions
```

TUI 只是一个客户端。daemon/runtime 持有模型调用、工具执行、Guard、记忆、Skill、MCP、附件、用量和会话状态。

## Suna 适合谁？

Suna 适合希望在本地使用 AI 处理文件、文档、代码、命令、API、图片和自定义工具的人，同时保留对风险操作的审查权。

如果你关心这些，Suna 可能适合你：

- 希望终端 AI 能理解和处理本地上下文；
- 希望写文件、执行命令前有可审查的 Guard；
- 希望复杂任务可以交给隔离 Subtask，而不是全部塞进一个大上下文；
- 希望 Agent Runtime 不绑定在一个 UI 里，可以接 Web UI、IDE、桌面端或脚本；
- 希望记忆、Skill、MCP 和工具都在本地可控。

## 常见使用场景

你可以用 Suna：

- 整理笔记、会议记录和调研材料；
- 读取本地文件夹，提炼关键信息；
- 比较多个方案，并让 Subtask 先给独立意见；
- 清理文档或配置文件，并在写入前确认；
- 执行诊断、测试、构建和自动化命令；
- 查询 HTTP API，并把响应整理成可读摘要；
- 把重复工作流沉淀成可复用 Skill；
- 通过 MCP 接入外部工具，或通过 stdio runtime 接第三方客户端。

## Suna 和常见终端 Agent 有什么不同？

| 能力 | 常见终端 Agent | Suna |
|---|---|---|
| 模型选择 | 一次运行一个活跃模型 | 主 Agent 可在运行中为 Subtask 选择不同模型 |
| 子任务上下文 | 常常共享或隐式继承 | 只接收主 Agent 显式传入的上下文 |
| 工具权限 | 通常是全局工具集 | 每个 Subtask 单独设置工具白名单 |
| 用户记忆 | 容易混进整段上下文 | 轻量用户画像，靠近最新输入注入 |
| 安全审查 | 确认、自动或粗粒度 allowlist | 意图感知 Guard + Workspace / 敏感路径硬规则 |
| Skill 生命周期 | Prompt 或文件注入 | 静态检查、可选 LLM Review、用户确认后启用 |
| UI 架构 | CLI/TUI 应用本身 | 本地 daemon/runtime + protocol + TUI 客户端 |
| 第三方 UI | 通常不是稳定边界 | `suna runtime --transport stdio` + JSON-RPC/NDJSON |

## 快速开始

### 安装

推荐优先使用 GitHub Release 的预构建二进制：

1. 打开 [Releases](https://github.com/alanchenchen/suna/releases)。
2. 下载与你的系统和架构匹配的压缩包，例如：
   - macOS Apple Silicon：`suna-darwin-arm64.zip`
   - macOS Intel：`suna-darwin-amd64.zip`
   - Linux x86_64：`suna-linux-amd64.tar.gz`
   - Linux arm64：`suna-linux-arm64.tar.gz`
   - Windows x86_64：`suna-windows-amd64.zip`
3. 解压后把 `suna`（Windows 为 `suna.exe`）放到 `PATH` 中。
4. 运行：

```bash
suna
```

如果你已经安装 Go，也可以直接安装：

```bash
go install github.com/alanchenchen/suna@latest
suna
```

如果 `suna` 命令找不到，请确认 Go bin 已加入 `PATH`：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

不要使用 `go run .` 启动 Suna。daemon / TUI 的本地进程管理依赖稳定的可执行文件路径。

### 更新 Suna

退出 TUI 后运行：

```bash
suna update
```

`update` 会检查最新 GitHub Release、展示 release notes，并在确认后下载对应平台资产、校验 checksum、替换当前 `suna` 可执行文件。

如果 daemon 仍在运行，先退出 TUI 或执行：

```bash
suna stop
```

## 首次使用

1. 启动 `suna`。
2. 如果还没有模型配置，进入 Config / Setup 页面。
3. 添加一个 Model Connection。
4. 选择 Provider 协议：
   - **OpenAI**：OpenAI Responses 协议。
   - **Anthropic**：Anthropic Messages 协议。
   - **OpenAI Compatible**：兼容 OpenAI Chat Completions 的第三方服务或网关。
5. 填写模型名、Endpoint、API Key、`context_window`、`max_output_tokens` 和可选能力标签。
6. 激活模型后回到 Welcome / New Conversation 开始对话。

常用设置都可以在 TUI 中通过 `/config` 修改。`context_window` 和 `max_output_tokens` 必须按当前模型服务的真实限制填写。`strengths` 用于告诉主 Agent 模型擅长什么；`subtask_for` 可选地控制哪些主模型能看到该模型作为 Subtask 候选。

## 试试这些 Prompt

这些例子故意写得简单一些，用来展示 Suna 如何委派任务、限制工具权限，以及在风险操作前确认。

### 1. 找一个独立视角

```text
我需要在两个方案之间做选择。你先让一个 subtask 不使用任何工具，独立给出第二意见，然后你再给我最终建议。
```

你应该能看到一次 `spawn` 调用：这个 Subtask 没有工具，只能看到主 Agent 明确传给它的上下文。

### 2. 安全地浏览本地文件

```text
帮我理解这个文件夹里的资料。你可以派一个只读 subtask 用 listdir、readfile 和 search 先看看，然后你再总结重点。
```

你应该能看到主 Agent 只给 Subtask 授权读文件和搜索类工具，最终判断仍由主对话完成。

### 3. 改文件前先确认

```text
帮我整理这份笔记，并保存成更清晰的版本。写入之前先告诉我你准备改什么，让我确认。
```

你应该能看到写文件或改文件操作经过 Suna 的 Guard，而不是静默执行。

## Suna 可以做什么？

```text
整理笔记、会议记录和调研材料
比较多个方案，并形成可执行计划
读取本地文件夹，提炼关键信息
分析截图、图片或粘贴的图片附件
查询 HTTP API，并把响应整理成可读摘要
检查文档、配置或脚本中的冲突和风险
修改文件，并说明变更影响
执行诊断、测试、构建和自动化命令
按路径、结构入口或正文搜索本地文件
使用已配置的 stdio MCP tools
创建、检查并启用 Skill
把长对话压缩成 Session State
把有边界的子任务委派给其它已配置模型
```

Suna 不是纯 coding agent，但它适合处理本地资料、代码、文档、研究、自动化和开发工作流。写文件、执行命令、文件系统操作、HTTP 写请求等行动类操作会经过 Guard。

## 内置工具

| 类型 | 工具 | 用途 |
|---|---|---|
| 感知 | `readfile` | 按行范围、tail 或 base64 读取本地文件 |
| 感知 | `listdir` | 列目录，支持递归、分页和 include/exclude 过滤 |
| 感知 | `search` | 结构化本地搜索，支持路径、结构入口和正文搜索 |
| 行动 | `exec` | 执行 shell 命令，用于诊断、测试、构建和系统操作 |
| 行动 | `writefile` | 创建、覆盖或追加文件 |
| 行动 | `editfile` | 对单个文件原子应用精确文本替换 |
| 行动 | `filesystem` | `stat` / `mkdir` / `move` / `copy` / `remove` 文件系统路径 |
| 行动 | `http` | 发送 HTTP 请求；读方法风险较低，写方法按风险审查 |

工具通过统一 Provider 暴露。Guard 决策由 Agent 统一处理，不塞进 UI 或零散工具 wrapper。

## TUI 快速上手

常用快捷键：

```text
Enter              发送 / 确认
Shift+Enter        输入换行
Ctrl+J             输入换行
Esc                取消运行、返回或关闭浮层
Ctrl+S             选择模式（拖选终端文本）
↑ / ↓             输入框为空时召回上一条 / 下一条历史输入
Ctrl+T             打开 / 关闭工具详情
Ctrl+R             展开 / 折叠 reasoning 详情
?                  打开或关闭帮助
PgUp / PgDn        滚动
Ctrl+V             尝试粘贴文本；若终端未传入文本且剪贴板含图片，则添加图片附件
Ctrl+C             退出
```

常用 Slash 命令：

```text
/new              新建会话
/model            打开模型选择器
/model <ref>      切换模型，例如 /model openai/gpt-4o-mini
/memory           查看 user profile memory
/mcp              打开 MCP 面板
/skills           打开 Skill 面板
/compact          手动压缩当前上下文
/config           打开配置页面
/help             打开帮助页
```

未注册的 `/文本` 会作为普通消息发送。

## 安全边界

Guard Mode 可在 `/config` 中切换：

```text
ask       风险操作请求确认
smart     用 LLM Review 做意图感知安全审查，必要时确认、拒绝或回退
auto      除硬性拦截规则外自动放行
readonly  只允许只读操作
```

Workspace 是可选目录边界：设置后，本地文件和命令操作会限制在该目录内。Suna 自己的数据目录仍允许用于配置、日志、附件和 Skill 管理；credentials 等敏感路径仍会被内置规则拦截。

注意：Workspace、Guard、Skill 和 MCP 都不是完整 OS sandbox。外部命令或 MCP server 启动后，仍拥有其进程本身的系统权限；启用前应确认你信任相关命令、脚本和 server。

## 数据目录

默认数据目录：

```text
~/.suna/config.toml        # 主配置
~/.suna/credentials.toml   # API Key
~/.suna/memory.db          # 记忆、会话、用量等本地数据
~/.suna/skills/            # Skill 目录
~/.suna/attachments/       # 图片和二进制附件
~/.suna/logs/app.log       # 日志
```

排查问题时优先查看 `~/.suna/logs/app.log`。

## 第三方客户端 Runtime

Suna 可以作为无头 runtime 通过 stdio 启动：

```bash
suna runtime --transport stdio
```

这个入口适合第三方 UI、桌面端、IDE 插件、本地 Web 服务或脚本。协议是 JSON-RPC 风格的 NDJSON。

建议先看：

- [stdio runtime 接入指南](docs/runtime-stdio.md)
- [Protocol](docs/protocol.md)
- [配置说明](docs/configuration.md)

## 开发者阅读入口

如果你想了解 Suna 的关键设计、架构、性能取舍和代码位置，建议从 docs 入口开始：

- [文档索引](docs/README.md)
- [Subtask 设计](docs/subtask.md)
- [关键设计](docs/design.md)
- [架构说明](docs/architecture.md)
- [代码地图](docs/code-map.md)
- [当前实现](docs/current-implementation.md)
- [配置说明](docs/configuration.md)
- [TUI 架构](docs/tui.md)
- [开发指南](docs/development.md)
- [English README](README.md)

## 当前边界

以下能力目前不要按完整产品能力依赖：

- Trigger、定时任务、文件监听等主动感知链路。
- 多会话管理 UI、完整历史搜索、向量记忆或知识库。
- 完整 MCP：远程 transport、resources、prompts、sampling、OAuth、sandbox 等尚未完成。
- Skill sandbox、市场和复杂生命周期 hooks。
- 成本统计与价格计算。
- 复杂权限 UI 或完整 OS sandbox。
- TUI 断开后对正在运行任务的完整事件回放/恢复。

## 许可证

Suna 使用 [PolyForm Noncommercial License 1.0.0](LICENSE)。

你可以在非商业目的下使用、学习、修改和分发 Suna；商业使用需要获得版权持有者的单独授权。分发原始或修改版本时，必须保留许可证条款和 required notice。
