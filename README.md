# Suna

> Suna (सून्य / śūnya)：梵文“空”。出厂无形，遇缘则生。

Suna 是一个运行在本地终端里的通用 AI Agent。它用 TUI 和你对话，由本地 daemon 负责模型调用、工具执行、Guard、记忆、Skill、MCP、附件和会话状态，让你可以在终端里直接让 AI 理解项目、修改文件、执行命令、访问 HTTP、处理图片，并在高风险操作前获得确认或 Smart Review。

Suna 的设计取向是**轻量、克制、越用越懂你**：不追求把所有能力堆成复杂面板，而是把关键能力收敛在本地 daemon、少量自然语言入口和可审查的工具链路里。随着使用增加，Suna 会通过轻量记忆、会话状态、Skill 和可配置模型逐渐贴合你的工作方式。

> Suna 目前处于快速开发状态。如果升级或使用过程中遇到功能失效，建议先升级到最新版本，并在备份必要数据后清理 Suna 数据目录中的 `.db` 文件。

## 亮点

- **智能模型路由，用 Subtask 发挥不同模型优势**：这不是常见的“启动前手动选一个模型”，而是主 Agent 可以在运行中根据任务性质、模型能力、上下文窗口和多模态能力，显式选择某个模型执行独立子任务；每个 Subtask 都是独立上下文，并由主 Agent 动态分配可见信息、图片和工具权限，完成后只把结果交回主 Agent 汇总决策。
- **Smart Mode：让安全审核理解工具意图**：很多 Agent 只有 `auto` 或手动确认两档；Suna 的 `smart` Guard 会在硬规则、Workspace 和风险分级之外，用 LLM Review 判断工具调用是否安全且符合用户意图。Smart Review 是安全审查器，不做普通 tool-call 优化，因此在不牺牲高危拦截的前提下降低无意义打断。
- **Skill 预检查与可选 LLM Check**：目录式 Skill 导入或创建后先做静态检查，再可选 LLM review，最后由用户确认是否启用，避免把不合格或不可信 Skill 直接暴露给 Agent。
- **越用越懂你的轻量记忆**：Suna 不把完整聊天历史当作长期记忆，而是提取稳定偏好、习惯、约束和纠错信息；记忆是轻量背景，不喧宾夺主。
- **终端里的高性能 Agent 工作台**：TUI 支持流式回复、Markdown 渲染、reasoning 展开/折叠、工具详情、复制模式、会话恢复和配置页面；Chat transcript 使用窗口化渲染和流式增量渲染缓存，在不降低流式刷新体验的前提下降低长回复和滚动时的 CPU 压力。
- **本地 daemon 架构**：TUI 专注交互，daemon 持有 Agent、模型、工具、安全策略、记忆和持久化状态，避免把业务逻辑塞进 UI。
- **真实可用的本地工具**：内置读文件、列目录、搜索、执行命令、写文件、精确编辑、文件系统操作和 HTTP 请求。
- **图片附件和 MCP 扩展**：支持多模态图片附件、剪贴板图片粘贴、会话恢复、上下文压缩，以及 stdio MCP tools-only runtime。

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

源码构建和 release 打包脚本见 [开发指南](docs/development.md)。不要使用 `go run .` 启动 Suna，daemon / TUI 的本地进程管理依赖稳定的可执行文件路径。

### 更新 Suna

退出 TUI 后运行：

```bash
suna update
```

`update` 会检查最新 GitHub Release、展示 release notes，并在有新版本时询问是否安装。确认后会下载对应平台资产、校验 checksum，并替换当前 `suna` 可执行文件。

如果 daemon 仍在运行，update 会中止并提示先退出 TUI 或执行：

```bash
suna stop
```

### 常用命令

```bash
suna                 # 打开 TUI；daemon 未运行时自动启动
suna status          # 查看 daemon 状态
suna update          # 检查新版本、展示 release notes，并在确认后安装
suna stop            # 停止 daemon
suna help            # 查看帮助
```

## 首次使用

1. 启动 `suna`。
2. 如果还没有模型配置，进入 Config / Setup 页面。
3. 添加一个 Model Connection。
4. 选择初始 Provider 类型，表单会预设对应的模型协议；后续也可以在“模型协议”字段中切换：
   - **OpenAI**：OpenAI Responses 协议。
   - **Anthropic**：Anthropic Messages 协议。
   - **OpenAI Compatible**：兼容 OpenAI Chat Completions 的第三方服务或网关。
5. 填写模型名、Endpoint、API Key、`context_window`、`max_output_tokens` 和能力标签。
6. 激活模型后回到 Welcome / New Conversation 开始对话。

常用设置都可以在 TUI 中通过 `/config` 修改，不必手动编辑配置文件。`context_window` 和 `max_output_tokens` 是模型能力参数，必须按当前模型服务实际限制填写。`strengths` 用于告诉主 Agent 模型擅长什么；`subtask_for` 是可选的子任务模型可见性过滤器，留空表示所有主模型都可使用，支持 `provider/model` glob（例如 `openai/*`、`anthropic/claude-*`），且模型始终可作为自己的子任务模型。

## 可以直接让 Suna 做什么

```text
整理这份会议记录，列出决策、待办和风险
帮我比较两个方案的优缺点，并给出推荐路径
读取一个文档目录，生成适合新用户的上手说明
分析一张截图或图片，提取关键信息
查询一个 HTTP API，并把结果整理成表格
检查一组配置文件是否存在冲突或安全风险
帮我理解这个项目结构，并定位相关实现入口
修改一个脚本或配置文件，并说明变更影响
把一套重复工作流程沉淀成 Skill
打开 MCP 面板，看看有哪些 server 报错
```

Suna 会自行决定是否需要调用工具。写文件、执行命令、HTTP 写请求等行动类操作会经过 Guard。

## TUI 快速上手

Suna 主要有四类页面/浮层：

- **Welcome**：显示版本、当前模型、用量、记忆、Guard、Workspace，并进入新会话、恢复会话、配置或帮助。
- **Chat**：输入自然语言、管理附件、查看回复、工具调用、Guard 确认和 AskUser 问题。
- **Config**：管理模型、主题、语言、Guard、Workspace、附件状态等。
- **Overlay**：模型选择器、工具详情、Skill 面板、MCP 面板、Guard 确认等临时浮层。

常用快捷键：

```text
Enter              发送 / 确认
Shift+Enter        输入换行
Ctrl+J             输入换行
Esc                取消运行、返回或关闭浮层
Ctrl+Y             进入 / 退出复制模式
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
/mcp              打开 MCP 面板，查看、启停、reload MCP server
/skills           打开 Skill 面板，查看并切换启用状态
/compact          手动压缩当前上下文
/config           打开配置页面
/help             打开帮助页
```

未注册的 `/文本` 会作为普通消息发送。`/model <ref>` 的 `<ref>` 通常是 `<provider>/<model>`；如果只输入模型名，Suna 会尽量用当前 provider 补全。

## 核心设计一眼看懂

```text
CLI / TUI
   ↓ protocol + local transport
Daemon
   ↓
Agent / Runner
   ↓
Model Provider / Tools / Guard / Memory / Skill / MCP / Subtask
```

关键原则：

- **TUI 不承载业务语义**：TUI 只负责交互和渲染，模型调用、工具执行、安全策略和持久化都在 daemon 侧。
- **Agent 统一编排安全边界**：工具只声明自身风险策略，Guard 决策由 Agent 结合当前会话、Workspace 和用户选择统一处理。
- **工具通过 Provider 暴露**：模型可见工具统一注册到 `tools.Manager`，避免在 Agent 或 Runner 中手工拼 schema。
- **上下文面向缓存和可恢复设计**：稳定 system/project/skill/tool schema 前缀 + Session State + recent messages + 靠近最新用户输入的 memory，降低长对话成本和上下文失控风险。
- **Subtask 不是普通子对话**：它不继承主对话历史、记忆、恢复会话或完整工具箱；主 Agent 必须为每次委派明确指定模型、任务、上下文、图片索引和工具白名单。Subtask 的结果会返回执行状态、结果文本和副作用披露，供主 Agent 汇总判断。
- **MCP 失败不阻塞启动**：单个 MCP server 失败会显示为运行态错误，不影响 Suna/daemon 启动。

更多设计细节见 [docs/README.md](docs/README.md)。

## 模型与工具

模型引用格式为：

```text
<provider>/<model>
```

例如：

```text
openai/gpt-4o-mini
anthropic/claude-sonnet-4-20250514
deepseek/deepseek-chat
```

内置工具：

| 类型 | 工具 | 用途 |
|---|---|---|
| 感知 | `readfile` | 按行范围、tail 或 base64 读取本地文件 |
| 感知 | `listdir` | 列目录，支持递归、分页和 include/exclude 过滤 |
| 感知 | `search` | 结构化本地搜索，可替代常见 `find` / `grep` / `glob` / `rg` 用法；支持路径、正文和结构入口搜索，使用 `mode`、`pattern`/`terms`、`match`、`scope`、include/exclude、上下文行和安全默认排除 |
| 行动 | `exec` | 执行 shell 命令；可证明只读的命令会被 Guard 归为低风险 |
| 行动 | `writefile` | 创建、覆盖或追加文件 |
| 行动 | `editfile` | 对单个文件原子应用一个或多个精确文本替换；默认唯一匹配，`target="all"` 替换全部，`target="2"` 替换第 2 个匹配 |
| 行动 | `filesystem` | `stat` / `mkdir` / `move` / `copy` / `remove` 文件系统路径 |
| 行动 | `http` | 统一 HTTP 请求；`GET` / `HEAD` 为只读，写方法按风险审查 |

完整配置字段见 [配置说明](docs/configuration.md)。

## 安全边界

Guard Mode 可在 `/config` 中切换：

```text
ask       风险操作请求确认
smart     先由 active model 做安全审查，安全且符合意图则减少打扰，不确定或高风险时再确认/拒绝/建议更安全调用
auto      除硬性拦截规则外自动放行
readonly  只允许只读操作
```

Workspace 是可选目录边界：

- 设置后，本地文件和命令操作会限制在该目录内。
- 留空表示关闭 Workspace 边界。
- `~/.suna` 数据目录仍允许用于配置、日志、附件和 Skill 管理。
- credentials 等敏感路径仍会被内置规则拦截。

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

Windows 示例：

```text
C:\Users\<你>\.suna\config.toml
C:\Users\<你>\.suna\credentials.toml
C:\Users\<你>\.suna\logs\app.log
```

`~/.suna/attachments/` 保存图片和二进制附件。TUI 粘贴 data URI 或剪贴板图片时，会先经过图片 MIME/大小校验，再按内容 hash 落盘去重；非图片二进制不会作为图片附件加入。

排查问题时优先查看 `~/.suna/logs/app.log`。

## 开发者阅读入口

如果你想了解 Suna 的关键设计、架构、性能取舍和代码位置，建议从 docs 入口开始：

- [文档索引](docs/README.md)：各文档分工和推荐阅读路径。
- [Subtask 设计](docs/subtask.md)：主 Agent 如何动态分配模型、上下文、图片和工具权限。
- [关键设计](docs/design.md)：架构、安全、上下文、性能、记忆、Subtask、Skill、MCP 等设计取舍。
- [架构说明](docs/architecture.md)：CLI、TUI、daemon、protocol 和核心包边界。
- [代码地图](docs/code-map.md)：功能到包、核心流程和常见代码入口。
- [当前实现](docs/current-implementation.md)：当前实际行为和未完成边界。
- [配置说明](docs/configuration.md)：`config.toml`、`credentials.toml` 字段和示例。
- [TUI 架构](docs/tui.md)：TUI 目录结构、Bubble Tea 约定和维护边界。
- [开发指南](docs/development.md)：构建、测试、提交前检查和代码约定。

## 当前边界

以下能力目前不要按完整产品能力依赖：

- Trigger、定时任务、文件监听等主动感知链路。
- 多会话管理 UI、完整历史搜索、向量记忆或知识库。
- 完整 MCP：远程 transport、resources、prompts、sampling、OAuth、sandbox 等尚未完成。
- Skill sandbox、市场和复杂生命周期 hooks。
- Hooks 执行链路。
- 成本统计与价格计算。
- 复杂权限 UI 或完整 OS sandbox。
- TUI 断开后对正在运行任务的完整事件回放/恢复。

## 许可证

Suna 使用 [PolyForm Noncommercial License 1.0.0](LICENSE)。

你可以在非商业目的下使用、学习、修改和分发 Suna；商业使用需要获得版权持有者的单独授权。分发原始或修改版本时，必须保留许可证条款和 required notice。
