# Suna

Suna 是一个运行在本地终端里的通用 AI Agent。它以 TUI 形式与你对话，能读取和修改本地文件、执行命令、访问 HTTP、处理图片输入、在需要时向你确认，并通过记忆和 Skill 逐步适应你的工作方式。

Suna 通过后台 daemon 处理模型、工具、配置和本地状态。当前版本不做长期常驻；最后一个客户端断开后，daemon 会短暂等待重连，然后自动收尾并退出。

> 当前版本更接近一个可用的本地 Agent MVP：对话、工具调用、模型配置、Guard 安全确认、记忆、上下文压缩、图片输入、Skill、基础 MCP tools-only runtime 和 subtask 委派已经可用；Trigger、完整 MCP 运行时、插件市场、复杂权限 UI、完整 sandbox 等能力仍不应视为已完成。

## 目录

- [三个核心亮点](#三个核心亮点)
- [主要能力](#主要能力)
- [安装与启动](#安装与启动)
- [首次使用](#首次使用)
- [TUI 使用说明](#tui-使用说明)
- [对话命令](#对话命令)
- [常用快捷键](#常用快捷键)
- [Subtask 智能模型路由](#subtask-智能模型路由)
- [模型与 Provider](#模型与-provider)
- [工具能力](#工具能力)
- [Smart Review 与安全模式](#smart-review-与安全模式)
- [图片与附件](#图片与附件)
- [记忆、会话与上下文](#记忆会话与上下文)
- [Subtask 委派](#subtask-委派)
- [Skill](#skill)
- [数据目录与配置文件](#数据目录与配置文件)
- [排查问题](#排查问题)
- [当前边界](#当前边界)

## 三个核心亮点

1. **Subtask 智能模型路由**：Suna 可以配置多个模型，并把模型的 `strengths`、上下文窗口等信息提供给主 Agent。主 Agent 在需要委派独立子任务时，会选择合适的模型执行，例如用快速模型整理资料、用强推理模型复核方案、用多模态模型分析图片。当前主对话、Guard review、Skill LLM review 等单独 LLM 请求默认仍使用 active model。
2. **Smart Review 安全审查**：Guard `smart` 模式会对中高风险工具调用做 LLM 审查，结合用户意图、工具参数、目标路径/命令和近期上下文判断是放行、拒绝、要求确认还是建议修改。
3. **Skill 检测与启用流程**：导入或创建 Skill 后，Suna 会先做静态检查，提示 scripts、网络访问、潜在风险等原因；你可以选择追加 LLM review，最后再决定是否启用。

## 主要能力

- **终端对话体验**：流式回复、Markdown 渲染、reasoning 展开/折叠、工具详情浮层、复制模式、会话恢复。
- **Subtask 智能模型路由**：可配置多个 Provider/模型；主 Agent 会参考模型 `strengths`、上下文窗口和多模态能力，把独立子任务委派给更合适的模型。
- **本地工具调用**：读取文件、列目录、读取 HTTP、执行命令、写文件、精确编辑文件、发送 HTTP 写请求。
- **Smart Review 安全审查**：Guard `smart` 模式会让 active model 审查写文件、执行命令、HTTP 写请求等风险操作；明确安全的操作可减少打扰，不确定或高风险时再让你确认。
- **AskUser 交互**：当信息不足或需要你做决定时，Suna 可以暂停并在对话中向你提问。
- **Subtask 委派**：主 Agent 可以把独立任务委派给更适合的模型；subtask 拥有独立上下文，只能使用被显式授权的工具。
- **图片输入**：支持粘贴图片路径、图片 URL 或 `data:image/...;base64,...`，作为当前消息附件发送给多模态模型。
- **轻量记忆**：保存少量用户偏好、长期事实和纠错信息；可通过 `/memory` 查看当前 active memory。
- **上下文压缩**：请求上下文接近模型窗口时可自动压缩，也可以手动 `/compact`。
- **Skill 检测流程**：Skill 导入/生成后会先静态检查，可选 LLM review，再由你确认是否启用。
- **Skill 能力目录**：支持目录式 Skill：一个目录内包含 `SKILL.md`，并可附带 `references/`、`scripts/`、`examples/`、`assets/` 等辅助文件。
- **基础 MCP tools-only runtime**：可在 `config.toml` 配置 stdio MCP server，连接后将 MCP tools 暴露给模型，并通过 `/mcp` 查看状态、启停和 reload。
- **本地 daemon 生命周期**：TUI 退出后 daemon 会短暂等待重连；若没有客户端连接，会取消当前运行、保留未处理的记忆队列并自动退出，也可通过 `suna status/stop` 查看或停止。

## 安装与启动

### 从源码构建

```bash
git clone <repo-url>
cd suna
go build -o suna .
./suna
```

也可以直接运行：

```bash
go run .
```

### 打包脚本

项目内置了常用打包脚本：

```bash
./build/build-macos-arm64.sh
./build/build-windows-amd64.sh
./build/build-release.sh
```

构建产物默认放在 `dist/`。

### CLI 命令

```bash
suna                 # 打开 TUI；如 daemon 未启动会自动启动
suna status          # 查看 daemon 状态
suna stop            # 停止 daemon
suna help            # 查看帮助
```

Windows 下如果当前目录里是 `suna.exe`，可使用：

```powershell
.\suna.exe
.\suna.exe status
```

## 首次使用

1. 启动 `suna`。
2. 如果还没有模型配置，Suna 会进入配置向导或 Config 页面。
3. 添加一个 Model Connection。
4. 选择 Provider 类型：
   - **OpenAI**：使用 OpenAI Responses 协议；TUI 会预填常见官方 Endpoint，但你可以修改。
   - **Anthropic**：使用 Anthropic Messages 协议；TUI 会预填常见官方 Endpoint，但你可以修改。
   - **OpenAI Compatible**：用于其他兼容 OpenAI API 的服务，需要填写 Provider ID、模型名和 Endpoint。
5. 填写 API Key、模型名、上下文窗口等信息。
6. 激活模型后返回 Welcome / New Conversation 开始对话。

常用配置可以在 TUI 中通过 `/config` 修改，不需要手动编辑配置文件。

## TUI 使用说明

Suna 的 TUI 主要包含几个页面和状态：

- **Welcome**：显示 daemon 状态、当前模型、上下文/记忆/会话概览，并提供进入聊天、配置、帮助等入口。
- **Chat**：主要对话页面。你可以输入自然语言、使用 slash command、查看工具调用、回复确认问题、管理附件。
- **Config**：管理模型连接、语言、主题、Guard 模式、Workspace、附件清理等。
- **Help**：查看内置帮助和快捷键说明。
- **Overlay**：工具详情、模型选择器、Skill 面板、Guard 确认、AskUser 选项等会以浮层方式出现。

典型使用方式：

```text
帮我分析这个项目结构
读取 README 并总结改进点
帮我把这个 bug 定位一下，必要时可以跑测试
把当前流程保存成一个 Skill
用另一个模型并行检查这个方案有没有漏洞
```

Suna 会根据任务自行决定是否需要调用工具。涉及写文件、执行命令、HTTP 写请求等行动类操作时，会按照 Guard 模式请求确认或自动处理。

## 对话命令

在 Chat 输入框中使用：

```text
/new              新建会话
/model            打开模型选择器
/model <ref>      切换模型，例如 /model openai/gpt-4o-mini
/memory           查看 active memory
/skills           打开 Skill 面板，查看并切换激活状态
/compact          手动压缩当前上下文
/config           打开配置界面
/help             打开帮助页
```

说明：

- 未知的 `/文本` 不会被当作命令执行，会作为普通消息发送。
- `/model <ref>` 中的 `<ref>` 通常是 `<provider>/<model>`；如果只输入模型名，Suna 会尽量使用当前 provider 补全。
- `/compact` 会让模型总结较早上下文，适合上下文接近窗口上限时使用。
- `/skills` 管理的是 Skill 激活状态，不等同于直接编辑 Skill 文件。

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

### 聊天

```text
Enter              发送消息
Shift+Enter        输入换行
Ctrl+J             输入换行
Ctrl+Y             进入复制模式
Ctrl+T             打开或关闭工具详情
Ctrl+R             展开或折叠 reasoning 详情
Esc                回复中取消当前运行；空草稿时返回 Welcome
```

### 附件

粘贴图片路径、图片 URL 或图片 data URI 时，Suna 会提示是否作为图片附件加入。

```text
Enter              加入附件 / 发送
Esc                取消附件识别
↑ / ↓              进入附件选择或移动选择
Delete/Backspace   删除选中的附件
```

### AskUser / Guard

当 Suna 需要你补充信息或确认风险操作时，会出现选项或确认面板：

- 用 `↑ / ↓` 移动选项。
- 用 `Enter` 确认。
- 如果允许自定义回答，可以直接输入文字后发送。
- Guard 确认用于写文件、执行命令、HTTP 写请求等风险操作。

## Subtask 智能模型路由

Suna 不是只能配置一个固定模型。你可以在 Config 中添加多个模型连接，并为每个模型补充能力信息。当前的智能选择主要发生在 **subtask 委派**：主 Agent 会看到可用模型列表、`strengths` 和上下文窗口，并在调用 `spawn` 时指定最适合该子任务的模型。

模型配置中最重要的信息包括：

- `provider/model`：模型引用，例如 `openai/gpt-4o-mini`。
- `context_window`：上下文窗口大小，用于判断长上下文任务和压缩策略。
- `strengths`：模型擅长项，例如快速、强推理、代码、多模态、便宜等。
- `reasoning`：思考相关配置，适合支持 reasoning 参数的模型。

典型用法：

- 主对话使用 active model；
- 资料整理、格式转换等轻任务委派给更快/更便宜的模型；
- 方案复核、bug 根因分析委派给强推理模型；
- 图片相关子任务委派给多模态模型。

注意：当前主对话、Guard Smart Review、Skill LLM Review、上下文压缩、记忆提取等单独 LLM 请求默认使用 active model；它们还不会自动改用其他模型。`strengths` 主要用于帮助主 Agent 选择 subtask 模型。

配置建议：至少准备一个主力 active model；如果经常做代码、排障或长文档处理，再增加一个强推理模型和一个低成本快速模型。这样 Suna 在需要并行检查或委派子任务时会更好用。

## 模型与 Provider

Suna 的模型引用格式是：

```text
<provider>/<model>
```

例如：

```text
openai/gpt-4o-mini
anthropic/claude-sonnet-4-20250514
zhipu/glm-5.1
```

`provider` 同时用于读取 `credentials.toml` 中对应的 API Key。同一个 provider 下的多个模型会共用同一份 API Key。

模型配置通常包含：

- `provider`：Provider ID，例如 `openai`、`anthropic`、`zhipu`。
- `model`：实际模型名。
- `base_url`：API Endpoint。
- `context_window`：上下文窗口大小，用于显示和压缩判断。
- `strengths`：模型擅长项，会提供给主 Agent 用于选择 subtask 模型。
- `reasoning`：思考相关请求字段，会透传到对应 provider 请求中。

### Provider 类型

- **OpenAI**：OpenAI Responses 协议。
- **Anthropic**：Anthropic Messages 协议。
- **OpenAI Compatible**：OpenAI-compatible Chat Completions 协议，适合第三方网关、中转站和兼容服务。

Suna 不会把 API Key 写入 `config.toml`；API Key 存放在 `credentials.toml`。

## 工具能力

Suna 的 Agent 可以调用一组本地工具。工具调用通常由模型自动决定，你也可以通过自然语言明确要求。

### 只读工具

| 工具 | 用途 |
|---|---|
| `readfile` | 读取本地文件内容。 |
| `listdir` | 列出目录内容，支持递归深度限制。 |
| `readhttp` | 发送 HTTP GET 请求并读取响应。 |

### 行动类工具

| 工具 | 用途 |
|---|---|
| `exec` | 执行 shell 命令，用于诊断、测试、构建等。 |
| `writefile` | 创建或覆盖文件。 |
| `editfile` | 用精确字符串替换编辑文件。 |
| `writehttp` | 发送 POST / PUT / DELETE / PATCH 请求。 |

行动类工具会经过 Guard。根据 Guard 模式，Suna 会请求你确认、进行 smart review、自动放行或直接拒绝。

### 内置交互与委派能力

| 能力 | 用途 |
|---|---|
| `askuser` | 当信息不足或需要你做选择时，向你提问。 |
| `spawn` | 把独立子任务委派给另一个模型。 |
| `skill_load` | 在需要时加载某个 Skill 的完整说明。 |
| `skill_start` | 导入、检查、review、启用 Skill 的内置流程。 |

## Smart Review 与安全模式

Suna 的工具分为读取类和行动类。写文件、编辑文件、执行命令、发送 HTTP 写请求等行动类工具会经过 Guard。

其中最推荐日常使用的是 **smart**：Suna 会先进行 Smart Review，对操作意图、路径、命令、HTTP 目标和潜在破坏性做一次模型审查。低风险且合理的操作可以减少确认弹窗；涉及删除、覆盖、执行未知脚本、访问敏感路径、联网写入等情况时，会继续要求你确认或直接拦截。

Guard Mode 可在 `/config` 中切换：

```text
ask       默认模式；风险操作会请求你确认
smart     Smart Review；风险操作先由模型审查，安全则减少打扰，不确定或高风险时再问你
auto      除硬性拦截规则外自动放行
readonly  只允许只读操作
```

### Workspace

Workspace 是可选的本地目录边界。

- 如果设置了 Workspace，Suna 会把本地文件和命令操作限制在该目录内。
- 如果留空，表示关闭这个目录边界。
- Suna 自有数据目录（默认 `~/.suna`）仍允许访问，方便排查配置、日志、附件和 Skill。
- credentials 等敏感文件仍会被敏感路径规则拦截。

建议：

- 日常项目开发时，把 Workspace 设置为当前项目目录。
- 不想让 Suna 写入或执行命令时，使用 `readonly`。
- 想减少确认弹窗但仍保留风险审查时，使用 `smart`。

## 图片与附件

Suna 支持把图片作为当前消息附件发送给多模态模型。

支持的输入方式：

```text
/path/to/image.png
https://example.com/image.jpg
data:image/png;base64,...
```

使用方式：

1. 在 Chat 输入框粘贴图片路径、图片 URL 或 data URI。
2. Suna 检测到图片后会提示是否加入附件。
3. 确认后，图片会出现在当前消息附件列表。
4. 发送消息时，附件和文本一起发给模型。

注意：

- 图片能力取决于当前模型是否支持多模态输入。
- 粘贴的本地图片会落盘到附件目录，便于 daemon 处理。
- 可在 Config 中清理附件状态。

## 记忆、会话与上下文

### Active Memory

Suna 会维护轻量 active memory，用于保存：

- 你的长期偏好；
- 常用工作方式；
- 长期约束；
- 对模型行为的纠错。

可通过：

```text
/memory
```

查看当前 active memory。

Active memory 不是完整知识库，也不是全文历史搜索。它只保存少量跨会话稳定信息；一次性任务细节、工具记录和本次会话里的话题账本不会写入 active memory。

### 会话恢复

- Suna 当前是单用户单当前会话形态：`/new` 会清空当前会话并开始新会话。
- Welcome 页可以恢复最近会话。
- TUI 恢复时展示真实的 user/assistant 对话，让你能回看之前聊天。
- 模型不一定加载全部历史 transcript；如果当前会话曾 compact，模型会用持久化的 Session State 加动态最近对话窗口继续。
- 原始 tool call / tool result 不会作为恢复上下文重新注入模型；重要工具事实由 Session State 承载，工具摘要只用于 TUI 展示。

### 上下文压缩

当对话变长时，Suna 可以压缩较早上下文：

```text
/compact
```

compact 的目标不是简单“生成摘要”，而是更新当前会话的内部 `Session State`：

- 保留当前任务/对话的执行状态，避免自动 compact 后任务中断；
- 把较早完成的任务、讨论过的话题、用户要求和关键决策写入会话账本；
- 把工具调用结果压成事实，避免大段日志继续占用模型上下文；
- 用动态 recent window 保留最近真实对话细节。

长对话小技巧：

- 没有触发过 compact 的短会话，恢复时基本就是最近真实对话恢复。
- 触发过 compact 的长会话，TUI 仍展示真实对话，但模型会基于 `Session State + 最近真实对话` 继续。
- 自动 compact 只在完整请求接近模型上下文窗口安全阈值时触发；手动 `/compact` 可主动整理当前上下文。
- 如果 compact 的 LLM 请求失败，Suna 不会使用伪摘要兜底，会直接提示错误并停止本轮请求，避免继续时记忆不准。

## Subtask 委派

主 Agent 可以把独立任务委派给其他模型。典型用途：

- 让便宜/快速模型做资料整理；
- 让强推理模型做方案 review；
- 让另一个模型独立检查 bug 定位；
- 让多模态模型分析当前消息中的图片。

Subtask 的特点：

- 拥有独立上下文，不继承主对话历史；
- 只能看到主 Agent 显式传入的 task/context；
- 只能使用主 Agent 显式授权的工具；
- 不能继续 spawn 子任务；
- 不能使用 askuser；
- 图片只通过 `input_images` 显式传递当前用户消息里的图片索引。

模型的 `strengths` 会帮助主 Agent 选择合适的 subtask 模型。

## Skill

Skill 用于告诉 Suna 某类任务应该如何处理。默认目录固定为：

```text
~/.suna/skills/
```

一个 Skill 是一个目录，最少包含 `SKILL.md`：

```text
~/.suna/skills/vue-style/
└── SKILL.md
```

也支持更通用的目录式 Skill 结构：`SKILL.md` 负责写给 Agent 的核心说明，目录中可以继续放参考文档、脚本、示例和素材。例如：

```text
~/.suna/skills/gpt-image2/
├── SKILL.md
├── references/
├── scripts/
├── examples/
└── assets/
```

Suna 只认通用核心字段：

```markdown
---
name: vue-style
description: Use when generating Vue code.
---

# vue-style

生成 Vue 代码时使用 Vue 3、`<script setup>` 和 composables 组织逻辑。
```

### 通过自然语言管理 Skill

你可以直接对 Suna 说：

```text
帮我导入这个 skill: https://github.com/user/skills
把 ~/Downloads/report-skill 加进来
把刚才这个流程保存成 skill
有哪些 skill 正在启用？
```

导入 Skill 时，模型只需要调用内置 `skill_start` 导入流程；Suna 会导入、静态检查、询问是否需要 LLM review，并最终询问是否激活。

静态检查会先验证 `SKILL.md` 元信息、目录结构和常见风险信号，并把 scripts、网络访问、可执行辅助文件等原因展示给你。可选 LLM review 默认使用当前 active model，对 Skill 内容做额外安全和可用性审查；review 结束后仍由你决定是否启用。

新建 Skill 时，主 Agent 会先按你的需求用普通文件工具准备目录和文件（包括可选 `references/`、`examples/`、`assets/`、`scripts/`），然后调用 `skill_start` 对已存在的 Skill 目录走同一套验收/激活流程。

### Skill 激活状态

`config.toml` 只记录轻量管理信息：

```toml
[skills.vue-style]
enabled = true

[skills.deploy-helper]
enabled = false
reasons = ["includes scripts/ helper files", "contains network access commands"]
```

启动时 daemon 只轻量扫描 `~/.suna/skills` 的目录和 `SKILL.md` 元信息：

- 手动放入的新 Skill 默认激活；
- Suna 通过对话导入或生成的 Skill 会先保持未激活；
- 完成 check、可选 LLM review 和用户确认后再激活；
- LLM 根据 Skill 的 `description` 自行判断是否需要加载，必要时通过 `skill_load(name)` 加载完整 `SKILL.md`。

### Skill scripts

`scripts/` 中的辅助脚本可由 Agent 按 `SKILL.md` 说明，在现有工具和 Guard 规则下通过 `exec` 使用。Suna 不为 Skill scripts 提供单独 sandbox。

## 数据目录与配置文件

运行数据默认保存在用户目录下：

```text
~/.suna/config.toml        # 主配置
~/.suna/credentials.toml   # API Key
~/.suna/memory.db          # 记忆、会话、用量等本地数据
~/.suna/skills/            # Skill 目录
~/.suna/attachments/       # 粘贴图片附件
~/.suna/logs/app.log       # 日志
```

Windows 示例：

```text
C:\Users\<你>\.suna\config.toml
C:\Users\<你>\.suna\credentials.toml
C:\Users\<你>\.suna\logs\app.log
```

### `config.toml`

保存模型、UI、Guard、Workspace、Skill 激活状态和 MCP server 等轻量配置。API Key 不写入这里。完整字段和复杂示例见 [配置说明](docs/configuration.md)。

### `credentials.toml`

保存 Provider 维度的 API Key。Suna 会尽量用文件权限保护它；同时 Guard 会拦截对 credentials 等敏感路径的读取。

### `memory.db`

SQLite 数据库，用于保存记忆、当前会话恢复状态、用量等本地数据。其中 `conversation_state` 保存：

- `session_state`：compact 后的当前会话内部状态，给模型恢复和后续 compact 使用；
- `last_messages`：TUI 展示真实历史对话用的 user/assistant 可见文本；
- `tool_summary`：TUI 恢复展示工具摘要用，不恢复原始 tool 输出。

### 日志

排查 daemon / transport / agent 问题时，优先查看：

```text
~/.suna/logs/app.log
```

Windows 下通常是：

```text
C:\Users\<你>\.suna\logs\app.log
```

## daemon 与 CLI

Suna 使用后台 daemon 管理本地状态。`suna` / `suna.exe` 是前台 CLI/TUI 入口；后台进程负责模型调用、工具执行、配置、记忆、Skill 和会话状态。

```bash
suna status    # 查看 daemon 是否可达、PID、uptime、连接数
suna stop      # 请求当前用户的 daemon 正常停止
```

当前生命周期实现：

- 打开 TUI 时，如果 daemon 未运行，Suna 会自动启动 daemon。
- 当前版本没有 trigger/cowork 等长期后台任务；daemon 只在有客户端连接时保持运行。
- 最后一个客户端断开后，daemon 会进入约 2 秒宽限期；如果期间没有新客户端连接，会取消当前 agent run 并自动退出。
- 未开始处理的 `memory_queue` 会保留在 SQLite 中，不会因为退出而强制触发记忆提取；下次启动后按记忆 worker 的批量策略恢复处理。
- `suna stop` 会向 daemon 发送停止请求，是推荐的手动停止方式。
- 如果 daemon 不可达但 PID 文件还在，`suna stop` 会尝试按 PID 结束旧进程并清理 PID 文件。
- 收到 `SIGTERM` / `SIGINT`、系统重启、用户注销、手动杀进程也会结束 daemon。
- daemon 的本地通信端点按当前用户隔离；Windows 使用 Named Pipe，macOS/Linux 使用 Unix socket。
- daemon 日志写入用户数据目录下的 `logs/app.log`。

升级提示：

新版 CLI/TUI 可能会先连接已经运行的旧 daemon。如果旧 daemon 来自旧版本，它可能使用旧协议、旧配置加载逻辑或旧 Windows Named Pipe 设置，导致新版前端表现异常。升级新版前建议先执行 `suna stop`；Windows 下如果新旧版本混用或连接异常，可执行 `taskkill /IM suna.exe /F` 清理旧进程后再启动新版。

## 排查问题

- **无法连接 daemon**：先运行 `suna status`，必要时 `suna stop` 后重新打开 `suna`。如果刚升级新版，优先确认旧 daemon 已停止。
- **Windows 升级后仍连接异常**：先尝试 `./suna.exe stop` 或 `suna stop`；如果旧进程仍残留，可在 PowerShell/CMD 中执行 `taskkill /IM suna.exe /F`，再启动新版。
- **模型不可用**：检查 `/config` 中 API Key、Endpoint、模型名是否正确；当前连接检查主要是本地配置检查，不等价于真实 API ping。
- **OpenAI-compatible 不工作**：确认 Base URL 是否包含正确 API 前缀，模型名是否与服务端一致，服务是否兼容 Chat Completions。
- **Anthropic/OpenAI 报认证错误**：确认 `credentials.toml` 中对应 provider 的 API Key，或在 `/config` 重新保存。
- **操作被拒绝**：检查 Guard Mode、Workspace 路径以及工具详情中的 Guard 原因。
- **Workspace 保存失败**：确认目录存在且当前用户可访问。
- **图片无法发送**：确认当前模型支持多模态输入，并检查图片路径/URL 是否可访问。
- **回复被取消**：回复中按 `Esc` 会取消当前运行。
- **查看日志**：默认位于用户数据目录下的 `logs/app.log`，例如 macOS/Linux `~/.suna/logs/app.log`，Windows `C:\Users\<你>\.suna\logs\app.log`。

## 文档

- [架构说明](docs/architecture.md)：当前 CLI、TUI、daemon、protocol 和核心包边界。
- [配置说明](docs/configuration.md)：`config.toml`、`credentials.toml` 的完整字段、示例和当前边界。
- [TUI 架构](docs/tui.md)：TUI 重构后的目录结构、Bubble Tea 约定和维护边界。
- [开发指南](docs/development.md)：本地构建、测试、提交前检查和代码约定。

`plans/` 保留规划、调研和历史设计；`docs/` 记录当前稳定事实。

## 当前边界

以下能力目前不要按完整产品能力依赖：

- Trigger / 定时任务 / 文件监听等主动感知链路。
- MCP 已支持基础 stdio tools-only runtime；远程 transport、resources、prompts、sampling、OAuth、sandbox 等完整 MCP 能力仍未完成。
- Skill sandbox、市场和复杂生命周期 hooks；Suna 只在导入/生成/更新时做 check，把风险原因展示给用户，启用后按现有工具和 Guard 使用。
- 完整历史搜索、向量记忆或知识库。
- 成本统计与价格计算。
- 复杂权限 UI 或完整 sandbox。
- TUI 断开后对正在运行任务的完整事件回放/恢复，目前仍不是完整任务系统。

## 许可证

如果仓库中包含 LICENSE 文件，请以该文件为准。
