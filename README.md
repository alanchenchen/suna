# Suna

Suna 是一个运行在终端里的通用 AI Agent。它以 TUI 形式与你对话，可以读取和修改本地文件、执行命令、访问 HTTP、在需要时向你确认，并通过轻量记忆和 Skill 逐步适应你的使用方式。

> 当前版本更接近一个可用的本地 Agent MVP：对话、工具调用、模型配置、Guard 安全确认、记忆、上下文压缩、图片输入和 subtask 委派已经可用；Trigger、完整 MCP 运行时、插件市场等能力仍不应视为已完成。

## 主要能力

- **终端对话体验**：流式回复、Markdown 渲染、思考/工具详情浮层、复制模式、会话恢复。
- **多模型配置**：支持 OpenAI、Anthropic 以及 OpenAI-compatible Provider；可配置 API Key、Endpoint、上下文窗口、模型擅长项和 reasoning 参数。
- **本地工具调用**：读取文件、列目录、读取 HTTP、执行命令、写文件、精确编辑文件、发送 HTTP 写请求。
- **安全确认（Guard）**：支持 `ask`、`smart`、`auto`、`readonly` 模式；可配置 Workspace，把本地文件和命令操作限制在一个目录内。
- **Subtask 委派**：主 Agent 可以把独立任务委派给指定模型；subtask 拥有独立上下文，只能使用被显式授权的工具。
- **AskUser 交互**：当信息不足或需要你做决定时，Suna 可以在对话中暂停并向你提问。
- **图片输入**：支持粘贴图片路径、图片 URL 或 `data:image/...;base64,...`，作为当前消息附件发送给多模态模型。
- **轻量记忆**：保存少量用户偏好、长期事实和纠错信息；可通过 `/memory` 查看当前 active memory。
- **上下文压缩**：长对话可自动压缩，也可以手动 `/compact`。
- **Skill 能力目录**：支持主流目录式 Skill：一个目录内包含 `SKILL.md`，并可附带 `references/`、`scripts/`、`agents/` 等辅助文件。Suna 会扫描并识别这些能力，以 `SKILL.md` 作为模型可加载的能力说明入口；辅助脚本可由 Agent 按说明通过命令工具运行。

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

项目内置了打包脚本：

```bash
./build/build-macos-arm64.sh
./build/build-release.sh
```

### CLI

```bash
suna                 # 打开 TUI；如 daemon 未启动会自动启动
suna start           # 后台启动 daemon
suna status          # 查看 daemon 状态
suna stop            # 停止 daemon
suna help            # 查看帮助
```

运行数据默认保存在：

```text
~/.suna/config.toml        # 主配置
~/.suna/credentials.toml   # API Key
~/.suna/memory.db          # 记忆、会话、用量等本地数据
~/.suna/skills/            # Skill 目录
~/.suna/attachments/       # 粘贴图片附件
~/.suna/logs/app.log       # 日志
```

## 首次使用

1. 启动 `suna`。
2. 如果还没有模型配置，进入 Config 添加一个 Model Connection。
3. 选择 Provider 类型：
   - **OpenAI**：使用 OpenAI 默认 Endpoint。
   - **Anthropic**：使用 Anthropic 默认 Endpoint。
   - **OpenAI Compatible**：用于其他兼容 OpenAI API 的服务，需要填写 Provider ID、模型名和 Endpoint。
4. 填写 API Key、模型名、上下文窗口等信息。
5. 激活模型后返回 New Conversation 开始对话。

常用配置也可以在 TUI 中通过 `/config` 修改，不需要手动编辑配置文件。

## 对话命令

在聊天输入框中使用：

```text
/new              新建会话
/model            打开模型选择器
/model <ref>      切换模型，例如 /model openai/gpt-4o-mini
/memory           查看 active memory
/compact          手动压缩当前上下文
/config           打开配置界面
/help             打开帮助页
```

未知的 `/文本` 不会被当作命令执行，会作为普通消息发送。

## 常用快捷键

### 通用

```text
↑ / ↓              导航
Enter              确认 / 发送
Esc                返回、取消；回复中按 Esc 会取消当前运行
Ctrl+C             退出
?                  打开帮助
PgUp / PgDn        滚动
```

### 聊天

```text
Enter              发送消息
Shift+Enter        输入换行
Ctrl+Y             复制模式
Ctrl+T             打开工具详情
Ctrl+R             展开或折叠 reasoning 详情
```

### 附件

粘贴图片路径、图片 URL 或图片 data URI 时，Suna 会提示是否作为图片附件加入。

```text
Enter              加入附件 / 发送
Esc                取消附件识别
↑ / ↓              进入附件选择
Delete/Backspace   删除选中的附件
```

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

模型的 `strengths` 会提供给主 Agent，用于它在创建 subtask 时选择更适合的模型。

## 安全模式与 Workspace

Suna 的工具分为读取类和行动类。写文件、编辑文件、执行命令、发送 HTTP 写请求等行动类工具会经过 Guard。

Guard Mode 可在 `/config` 中切换：

```text
ask       默认模式；风险操作会请求你确认
smart     中高风险操作先由模型审查，不确定时再问你
auto      除硬性拦截规则外自动放行
readonly  只允许只读操作
```

Workspace 可选。如果设置了 Workspace，Suna 会把本地文件和命令操作限制在该目录内；留空表示关闭这个边界。

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

也支持目前更通用的目录式 Skill 结构：`SKILL.md` 负责写给 Agent 的核心说明，目录中可以继续放参考文档、脚本、示例和素材。例如：

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

Skill 主要通过自然语言导入、生成和管理：

```text
帮我导入这个 skill: https://github.com/user/skills
把 ~/Downloads/report-skill 加进来
把刚才这个流程保存成 skill
有哪些 skill 正在启用？
```

Suna 在导入、生成或内容变化后会执行 check，解释发现的风险原因，并询问是否启用。信任结果记录在 `config.toml`：

```toml
[skills.vue-style]
enabled = true
hash = "sha256:..."

[skills.deploy-helper]
enabled = false
hash = "sha256:..."
reasons = ["包含脚本", "脚本访问网络"]
```

启动时 daemon 扫描 `~/.suna/skills`，只有 `enabled=true` 且 hash 匹配的 Skill 才会进入 active skill index。LLM 根据 Skill 的 `description` 自行判断是否需要加载，必要时通过 `skill.load(name)` 加载完整 `SKILL.md`。

`scripts/` 中的辅助脚本可由 Agent 按 `SKILL.md` 说明，在现有工具和 Guard 规则下通过 `exec` 使用；Suna 不为 Skill scripts 提供单独 sandbox。MCP server 独立配置在 `config.toml`。

## 当前边界

以下能力目前不要按完整产品能力依赖：

- Trigger / 定时任务 / 文件监听等主动感知链路。
- MCP client/runtime 仍按 `config.toml` 独立接入，完整实现进度以代码为准。
- Skill sandbox、市场和复杂生命周期 hooks；Suna 只在导入/生成/更新时做 check，把风险原因展示给用户，启用后按现有工具和 Guard 使用。
- 完整历史搜索、向量记忆或知识库。
- 成本统计与价格计算。
- 复杂权限 UI 或完整 sandbox。

## 排查问题

- **无法连接 daemon**：先运行 `suna status`，必要时 `suna stop` 后重新打开 `suna`。
- **模型不可用**：检查 `/config` 中 API Key、Endpoint、模型名是否正确；当前连接检查主要是本地配置检查，不等价于真实 API ping。
- **操作被拒绝**：检查 Guard Mode、Workspace 路径以及工具详情中的 Guard 原因。
- **Workspace 保存失败**：确认目录存在且当前用户可访问。
- **查看日志**：`~/.suna/logs/app.log`。
