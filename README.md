# Suna

Suna 是一个运行在本地终端里的通用 AI Agent。它通过 TUI 与你对话，由本地 daemon 负责模型调用、工具执行、配置、记忆、Skill、MCP 和会话状态。你可以让它理解项目、读写文件、执行命令、访问 HTTP、处理图片输入，在关键风险操作前请求确认或进行 Smart Review。

> 当前 README 面向用户上手与功能说明；`docs/` 记录当前代码实现；`plans/` 保留规划、调研和历史设计，不一定代表当前行为。

## 当前可用能力

- **终端对话**：流式回复、Markdown 渲染、reasoning 展开/折叠、工具详情浮层、复制模式、会话恢复。
- **多模型配置**：支持 OpenAI、Anthropic、OpenAI-compatible Provider；可配置多个模型和能力标签。
- **本地工具**：读文件、列目录、HTTP GET、执行命令、写文件、精确编辑文件、HTTP 写请求。
- **Smart Review / Guard**：风险工具调用可确认、自动审查、自动放行或只读拦截。
- **Workspace 边界**：可限制本地文件与命令操作在指定目录内。
- **图片附件**：支持本地图片路径、图片 URL、`data:image/...;base64,...`。
- **轻量记忆**：保存少量跨会话稳定偏好、约束和纠错，可在 TUI 中查看。
- **上下文压缩**：长对话接近模型窗口时自动压缩，也可手动触发。
- **Subtask 委派**：主 Agent 可把独立任务交给另一个配置好的模型，并限定其可见上下文和工具。
- **Skill**：支持目录式 Skill，导入或创建后经过静态检查、可选 LLM review，再由用户决定是否启用。
- **MCP tools-only runtime**：支持 stdio MCP server，把 MCP tools 暴露给模型，并可在 TUI 中查看、启停、reload。
- **本地 daemon**：TUI 自动拉起 daemon；最后一个客户端断开后 daemon 短暂等待重连，然后自动退出。

## 安装与启动

### 从源码运行

```bash
git clone <repo-url>
cd suna
go run .
```

或构建二进制：

```bash
go build -o suna .
./suna
```

Windows 下通常是：

```powershell
.\suna.exe
```

### 打包脚本

```bash
./build/build-macos-arm64.sh
./build/build-windows-amd64.sh
./build/build-release.sh
```

构建产物默认放在 `dist/`。

### CLI 命令

```bash
suna                 # 打开 TUI；daemon 未运行时自动启动
suna status          # 查看 daemon 状态
suna stop            # 停止 daemon
suna help            # 查看帮助
```

升级新版前建议先执行 `suna stop`，避免新版 TUI 连接到旧 daemon。

## 首次使用

1. 启动 `suna`。
2. 如果还没有模型配置，进入 Config / Setup 页面。
3. 添加一个 Model Connection。
4. 选择 Provider 类型：
   - **OpenAI**：OpenAI Responses 协议。
   - **Anthropic**：Anthropic Messages 协议。
   - **OpenAI Compatible**：兼容 OpenAI Chat Completions 的第三方服务或网关。
5. 填写模型名、Endpoint、API Key、上下文窗口和能力标签。
6. 激活模型后回到 Welcome / New Conversation 开始对话。

常用设置都可以在 TUI 里通过 `/config` 修改，不必手动编辑配置文件。

## TUI 快速上手

Suna 主要有四类页面/浮层：

- **Welcome**：显示版本、当前模型、用量、记忆、Guard、Workspace，并进入新会话、恢复会话、配置或帮助。
- **Chat**：输入自然语言、管理附件、查看回复、工具调用、Guard 确认和 AskUser 问题。
- **Config**：管理模型、主题、语言、Guard、Workspace、附件状态等。
- **Overlay**：模型选择器、工具详情、Skill 面板、MCP 面板、Guard 确认等临时浮层。

可以直接用自然语言描述任务，例如：

```text
帮我分析这个项目结构，并指出主要入口
读取 README，整理一版更适合用户上手的文档
定位这个测试失败的原因，必要时可以运行测试
把刚才的代码审查流程保存成一个 Skill
用另一个模型独立 review 这个方案
```

Suna 会自行决定是否需要调用工具。写文件、执行命令、HTTP 写请求等行动类操作会经过 Guard。

## Slash 命令

在 Chat 输入框中使用：

```text
/new              新建会话
/model            打开模型选择器
/model <ref>      切换模型，例如 /model openai/gpt-4o-mini
/memory           查看 active memory
/mcp              打开 MCP 面板，查看、启停、reload MCP server
/skills           打开 Skill 面板，查看并切换启用状态
/compact          手动压缩当前上下文
/config           打开配置页面
/help             打开帮助页
```

说明：

- 未注册的 `/文本` 会作为普通消息发送。
- `/model <ref>` 的 `<ref>` 通常是 `<provider>/<model>`；如果只输入模型名，Suna 会尽量用当前 provider 补全。
- `/compact` 会请求模型更新当前会话的 Session State，不只是生成一段摘要。
- Skill 的导入、生成、检查和启用流程也可以通过自然语言触发，不限于 `/skills` 面板。

## 常用快捷键

### 通用

```text
↑ / ↓              导航 / 移动选项
Enter              确认 / 发送
Esc                返回、取消；回复中按 Esc 会取消当前运行
Ctrl+C             退出
?                  打开或关闭帮助
PgUp / PgDn        滚动
```

### Chat

```text
Enter              发送消息
Shift+Enter        输入换行
Ctrl+J             输入换行
Ctrl+Y             进入 / 退出复制模式
Ctrl+T             打开 / 关闭工具详情
Ctrl+R             展开 / 折叠 reasoning 详情
Esc                取消运行；无草稿时返回 Welcome；有草稿时提示丢弃
```

### 附件、AskUser、Guard

- 粘贴图片路径、图片 URL 或图片 data URI 后，Suna 会提示是否加入附件。
- 附件选择中可用 `↑ / ↓` 移动，`Delete/Backspace` 删除，`Esc` 取消识别。
- AskUser / Guard 选项中可用 `↑ / ↓` 选择，`Enter` 确认；允许自定义回答时可以直接输入文本。

## 模型与 Provider

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

模型配置通常包括：

- `provider`：Provider ID，也是 `credentials.toml` 中 API Key 的分组名。
- `model`：上游模型名。
- `base_url`：API Endpoint。
- `context_window`：上下文窗口，用于显示、压缩和路由参考。
- `strengths`：模型能力标签，会提供给主 Agent 选择 subtask 模型。
- `reasoning`：透传给上游的 reasoning / thinking 扩展字段。

Provider 行为：

- `provider = "openai"` 使用 OpenAI Responses 协议。
- `provider = "anthropic"` 使用 Anthropic Messages 协议。
- 其它 provider 默认使用 OpenAI-compatible Chat Completions 协议。

API Key 不写入 `config.toml`，而是保存在 `credentials.toml`。完整配置字段见 [配置说明](docs/configuration.md)。

## 工具、安全与 Workspace

Suna 内置工具分为两类：

| 类型 | 工具 | 用途 |
|---|---|---|
| 只读 | `readfile` | 读取本地文件 |
| 只读 | `listdir` | 列目录 |
| 只读 | `readhttp` | HTTP GET |
| 行动 | `exec` | 执行 shell 命令 |
| 行动 | `writefile` | 创建或覆盖文件 |
| 行动 | `editfile` | 精确字符串替换编辑文件 |
| 行动 | `writehttp` | POST / PUT / DELETE / PATCH |

Guard Mode 可在 `/config` 中切换：

```text
ask       风险操作请求确认
smart     先由 active model 审查，安全则减少打扰，不确定或高风险时再问你
auto      除硬性拦截规则外自动放行
readonly  只允许只读操作
```

Workspace 是可选目录边界：

- 设置后，本地文件和命令操作会限制在该目录内。
- 留空表示关闭 Workspace 边界。
- `~/.suna` 数据目录仍允许用于配置、日志、附件和 Skill 管理。
- credentials 等敏感路径仍会被内置规则拦截。

建议日常项目开发时把 Workspace 设置为项目根目录；不希望 Suna 写入或执行命令时使用 `readonly`；想减少确认但保留审查时使用 `smart`。

## 图片与附件

支持把图片作为当前消息附件发送给多模态模型：

```text
/path/to/image.png
https://example.com/image.jpg
data:image/png;base64,...
```

注意：

- 图片能力取决于当前模型是否支持多模态输入。
- 本地图片会复制到附件目录，便于 daemon 处理。
- 可在 Config 中查看或清理附件状态。

## 记忆、会话与上下文

### Active Memory

Suna 的 active memory 只保存少量跨会话稳定信息，例如长期偏好、工作方式、约束和纠错。它不是知识库，也不是完整历史搜索。

```text
/memory
```

### 会话恢复

- 当前是单用户、单当前会话形态。
- `/new` 会清空当前会话并开始新会话。
- Welcome 页可以恢复最近会话。
- TUI 恢复时展示真实 user / assistant 可见对话。
- 模型恢复上下文由 Session State + 最近对话窗口组成，不会重新注入原始 tool call / tool result。

### 上下文压缩

```text
/compact
```

compact 会更新当前会话内部 Session State，保留任务状态、关键决策、用户要求和工具事实，并释放较早上下文占用。自动 compact 只在请求接近模型上下文窗口安全阈值时触发；如果 compact 失败，Suna 会停止本轮请求并提示错误，避免基于不可靠摘要继续。

## Subtask 委派

主 Agent 可以把独立子任务委派给另一个模型。常见用途：

- 用快速/低成本模型整理资料或转换格式。
- 用强推理模型独立 review 方案或 bug 定位。
- 用多模态模型分析图片。

Subtask 边界：

- 拥有独立上下文，不继承主对话历史。
- 只能看到主 Agent 显式传入的 task / context。
- 只能使用主 Agent 显式授权的工具。
- 不能继续 spawn 子任务。
- 不能使用 askuser。
- 图片只通过 `input_images` 显式传递当前用户消息中的附件。

当前智能路由主要发生在 subtask 委派；主对话、Guard Smart Review、Skill LLM Review、上下文压缩和记忆提取默认仍使用 active model。

## Skill

Skill 用于告诉 Suna 某类任务应该如何处理。默认目录：

```text
~/.suna/skills/
```

最小结构：

```text
~/.suna/skills/vue-style/
└── SKILL.md
```

也可以包含辅助目录：

```text
~/.suna/skills/report-helper/
├── SKILL.md
├── references/
├── scripts/
├── examples/
└── assets/
```

`SKILL.md` 示例：

```markdown
---
name: vue-style
description: Use when generating Vue code.
---

# vue-style

生成 Vue 代码时使用 Vue 3、`<script setup>` 和 composables 组织逻辑。
```

可以用自然语言管理 Skill：

```text
帮我导入这个 skill: https://github.com/user/skills
把 ~/Downloads/report-skill 加进来
把刚才这个流程保存成 skill
有哪些 skill 正在启用？
```

导入或生成 Skill 后，Suna 会走统一流程：导入/写入文件 → 静态检查 → 询问是否 LLM review → 询问是否启用。`scripts/` 中的辅助脚本没有单独 sandbox，使用时仍依赖现有工具和 Guard。

## MCP

Suna 当前支持基础 MCP tools-only runtime：

- 支持 stdio MCP server。
- 支持 initialize、tools/list、tools/call。
- MCP tools 会注册为 `mcp__<server>__<tool>`。
- 单个 server 启动失败不会阻塞 Suna。
- `/mcp` 面板可查看状态、启停 server、reload tools。

当前不支持 MCP resources、prompts、sampling、OAuth、远程 transport 或 MCP 级 sandbox。MCP server 是外部进程；启用前应确认你信任它。

## 数据目录与配置文件

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

排查问题时优先查看 `~/.suna/logs/app.log`。

## daemon 生命周期

Suna 使用本地 daemon 管理核心状态：

- 打开 TUI 时，如果 daemon 未运行，会自动后台启动。
- 当前没有 trigger / cowork 等长期后台任务；daemon 主要随客户端连接存在。
- 最后一个客户端断开后，daemon 会等待约 2 秒；如果没有新连接，会取消当前运行并退出。
- 未开始处理的记忆队列会保留在 SQLite 中，下次启动后继续按 worker 策略处理。
- `suna stop` 是推荐的手动停止方式。
- macOS / Linux 使用 Unix socket，Windows 使用 Named Pipe。

## 常见问题

- **无法连接 daemon**：运行 `suna status`，必要时 `suna stop` 后重新打开。升级新版前也建议先停止旧 daemon。
- **Windows 升级后连接异常**：先尝试 `suna stop`；如果旧进程残留，可执行 `taskkill /IM suna.exe /F` 后再启动。
- **模型不可用**：检查 `/config` 中 API Key、Endpoint、模型名和 active model。
- **OpenAI-compatible 不工作**：确认 Base URL 是否包含正确 API 前缀，模型名是否与服务端一致，服务是否兼容 Chat Completions。
- **操作被拒绝**：检查 Guard Mode、Workspace、敏感路径规则，以及工具详情中的 Guard 原因。
- **图片无法发送**：确认当前模型支持多模态，并检查图片路径或 URL 是否可访问。
- **回复被取消**：回复中按 `Esc` 会取消当前运行。
- **Skill 没生效**：确认 `/skills` 中已启用，且 `SKILL.md` 的 `description` 足够明确。
- **MCP server 无工具**：打开 `/mcp` 查看错误，确认命令、参数、cwd、环境变量和 server 本身可运行。

## 更多文档

- [当前实现](docs/current-implementation.md)：按模块记录当前代码实际行为和边界。
- [配置说明](docs/configuration.md)：`config.toml`、`credentials.toml` 的字段、示例和限制。
- [架构说明](docs/architecture.md)：CLI、TUI、daemon、protocol 和核心包边界。
- [TUI 架构](docs/tui.md)：TUI 目录结构、Bubble Tea 约定和维护边界。
- [开发指南](docs/development.md)：本地构建、测试、提交前检查和代码约定。

## 当前边界

以下能力目前不要按完整产品能力依赖：

- Trigger、定时任务、文件监听等主动感知链路。
- 完整 MCP：远程 transport、resources、prompts、sampling、OAuth、sandbox 等尚未完成。
- Skill sandbox、市场和复杂生命周期 hooks。
- 完整历史搜索、向量记忆或知识库。
- 成本统计与价格计算。
- 复杂权限 UI 或完整 OS sandbox。
- TUI 断开后对正在运行任务的完整事件回放/恢复。

## 许可证

如果仓库中包含 LICENSE 文件，请以该文件为准。
