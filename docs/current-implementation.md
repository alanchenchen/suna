# 当前实现

本文按当前代码实际行为记录 Suna 的主要实现与边界。README 面向用户上手；本文件面向维护者和需要理解当前产品状态的人。

## 进程与入口

- `suna` 默认打开 TUI。
- `suna status` 查询 daemon 状态。
- `suna stop` 请求 daemon 正常停止；daemon 不可达但 PID 文件存在时，会尝试 fallback 停止旧进程并清理 PID 文件。
- `SUNA_RUN_DAEMON=1` 是内部 daemon 启动入口，普通用户不需要直接使用。
- TUI 启动前会先确保 daemon 可用；如果本地 endpoint 不可达，会后台拉起同一个可执行文件作为 daemon。

## TUI 当前行为

主要页面：

- Welcome：展示版本、active model、用量、daemon uptime、memory、Guard、Workspace，并提供新会话、恢复会话、配置、帮助入口。
- Chat：对话、工具进度、AskUser、Guard、附件、模型选择、Skill/MCP 面板。
- Config：配置模型、Guard、Workspace、UI、附件等。
- Help：展示 slash commands 与帮助说明。

Chat slash commands：

| 命令 | 当前行为 |
|---|---|
| `/new` | 清空 TUI 当前消息并请求 daemon 新建会话。 |
| `/model` | 打开模型选择器。 |
| `/model <ref>` | 切换 active model；`<ref>` 可为完整 `provider/model`，也可在当前 provider 下只写 model。 |
| `/memory` | 拉取并展示 user profile memory。 |
| `/mcp` | 打开 MCP 面板，查看 server 状态，支持启停和 reload。 |
| `/skills` | 打开 Skill 面板，查看 Skill 并切换启用状态。 |
| `/compact` | 手动触发当前会话 compact。 |
| `/config` | 进入 Config 页面。 |
| `/help` | 进入 Help 页面。 |

未注册的 `/文本` 会作为普通用户消息发送，不会报错或执行隐藏命令。

## Daemon 生命周期

当前 daemon 是按需运行的本地后台服务，不是长期任务调度器：

- TUI 或 CLI status/stop 通过本地 transport 连接 daemon。
- 每个连接建立时注册 event sink，断开时注销。
- 最后一个客户端断开后进入短暂宽限期；若没有新客户端连接，则取消当前 agent run 并退出。
- 未处理的 `memory_queue` 保存在 SQLite 中，不在退出时强制 drain；下次启动后 worker 按批量策略继续处理。
- 目前没有 trigger、定时任务、文件监听、cowork/perception 等长期后台活动。

## 本地通信

- macOS/Linux 使用 Unix socket。
- Windows 使用 Named Pipe。
- protocol 为 JSON-RPC 风格 request / notification。
- TUI 只通过 protocol 与 daemon 通信，不直接调用 agent、runner、tools、guard、memory、skill、mcp 等业务包。

## 模型与请求

当前支持三类 provider 路由：

- `provider = "openai"`：OpenAI Responses 协议。
- `provider = "anthropic"`：Anthropic Messages 协议。
- 其它 provider：OpenAI-compatible Chat Completions 协议。

模型 ref 为 `<provider>/<model>`。`provider` 同时用于匹配 `credentials.toml` 中的 API Key 分组。

`models.reasoning` 是透传字段，Suna 不提供跨供应商统一 preset；是否生效由上游 API 决定。Suna 会避免该字段覆盖请求核心字段。

LLM 请求使用按场景维护的 idle timeout，而不是任务总时长 timeout。Runner 内的主对话、工具调用前后请求和 subtask 内部请求默认按普通流式响应等待 120 秒；如果实际收到 provider 归一化后的 `ReasoningContent` chunk，本次请求会升级为 30 分钟 idle timeout。这个判断基于 LLM stream 实际返回类型，不基于 `models.reasoning` 配置或模型名。compact、Guard Smart Review、Skill LLM Review、记忆整理等不走 Runner 的单独 LLM 请求使用固定 idle timeout。

当前 OpenAI Responses 和 OpenAI-compatible Chat provider 会把 reasoning delta 归一为 `ReasoningContent`。Anthropic provider 目前使用非 streaming 的 Messages 调用，只在完整响应后返回文本和 tool call，尚未把 Claude thinking block / delta 归一为 `ReasoningContent`；因此 Anthropic thinking 暂不能触发 Runner 的 30 分钟动态 reasoning idle timeout。若要支持，需要先把 Anthropic provider 改为 streaming，并在 provider 层输出 `Chunk{ReasoningContent: ...}`。

当前多模型智能选择主要用于 subtask：主 Agent 可查看可用模型、上下文窗口、strengths 和多模态能力，然后在 `spawn` 时选择模型。主对话、Guard Smart Review、Skill LLM Review、上下文压缩、记忆提取等单独 LLM 请求默认仍使用 active model。

## Agent / Runner / Tool

- Agent 负责任务决策、上下文组装、工具执行入口、Guard 编排、Skill/MCP/subtask runtime 适配。
- Runner 执行模型流式调用和工具调用循环。
- `tools.Manager` 维护工具目录、schema 和执行路由，不做会话级安全决策。
- 模型可见工具应通过 `tools.Provider` 注册，避免在 Agent / Runner 中手工拼 schema。

内置工具：

| 工具 | 类型 | 当前用途 |
|---|---|---|
| `readfile` | 感知 | 按行范围、tail 或 base64 读取本地文件。 |
| `listdir` | 感知 | 列目录，支持递归、分页、include/exclude 和隐藏文件开关。 |
| `search` | 感知 | 按文件名或内容搜索目录，默认排除常见依赖/构建目录和凭据文件；空结果或截断时只在正文追加诊断提示，不改变 TUI 依赖的 metadata contract。 |
| `exec` | 行动 | 执行 shell 命令；Guard 会把可证明只读的命令归为 low risk。 |
| `writefile` | 行动 | 创建、覆盖或追加文件，支持父目录创建和写前 SHA-256 校验。 |
| `editfile` | 行动 | 对单个文件原子应用一个或多个精确文本替换；替换范围使用 `mode=unique|nth|all` 表达，避免互斥布尔参数组合。 |
| `filesystem` | 行动 | `stat` / `mkdir` / `move` / `copy` / `remove` 文件系统路径；`stat` 为只读低风险调用。 |
| `http` | 行动 | 统一 HTTP 请求工具；`GET` / `HEAD` 为只读低风险调用，写方法按风险审查。 |
| `askuser` | runtime | 向用户提问。 |
| `spawn` | runtime | 委派独立 subtask。 |
| `skill_load` | runtime | 加载某个 Skill 的完整说明。 |
| `skill_start` | runtime | 导入或检查 Skill，并进入 review/enable 工作流。 |

MCP tools 会以 `mcp__<server>__<tool>` 的形式注册到工具目录。

## Guard 与 Workspace

Guard 由 Agent 统一处理，工具只声明自身 Guard policy。

当前 Guard mode：

- `readonly`：只允许只读操作。
- `ask`：风险操作请求用户确认。
- `auto`：除硬性拦截规则外自动放行。
- `smart`：中高风险操作由 active model 做 Smart Review，再决定放行、拒绝、确认或建议修改。

Workspace 是本地文件和明显 exec 路径的目录硬边界，不能被用户 allowed rule 绕过。它不是 OS sandbox，无法限制外部程序启动后自行访问的文件、网络或进程权限。

敏感路径、内置 blocked rule、用户 blocked rule 优先级高于普通 allowed rule。

## 记忆与会话状态

默认 SQLite 数据库为 `~/.suna/memory.db`。

主要数据：

- `user_profile_memory`：长期 user profile memory，只保存少量跨会话稳定偏好、习惯、约束和纠错。
- `conversation_state.session_state`：compact 后的当前会话内部状态，用于模型恢复和后续 compact。
- `conversation_state.last_messages`：TUI 恢复展示用的真实可见 user/assistant 对话。
- `conversation_state.tool_summary`：TUI-only 工具摘要，恢复时展示，不作为原始工具结果注入模型。
- `memory_queue`：user profile memory 临时提取队列，由 worker 批量处理。

自动 compact 在完整请求接近上下文窗口安全阈值时触发。compact 成功后，Session State 作为独立请求字段由 provider 注入模型上下文，working memory 只保留最近对话窗口。compact 失败时不会使用伪摘要或硬裁剪继续，而是提示错误并停止本轮请求。

## 图片与附件

- TUI 识别粘贴的本地图片路径、图片 URL、图片 data URI。
- 本地图片会保存到附件目录，发送消息时作为当前用户消息附件提交。
- 图片是否可被模型理解取决于 active model 或被委派 subtask 模型的多模态能力。
- MCP 二进制结果也会保存到附件目录，并以文本引用返回给模型/TUI。

## Skill 当前实现

Skill 默认目录为 `~/.suna/skills/`。每个 Skill 是一个目录，至少包含 `SKILL.md`。

Suna 识别通用 front matter：

```markdown
---
name: skill-name
description: When to use this skill.
---
```

当前流程：

- daemon 启动时轻量扫描 Skill 目录和 `SKILL.md` 元信息。
- 手动放入目录的有效 Skill 默认可用。
- 通过对话导入或生成的 Skill 先保持未启用。
- `skill_start` 会执行静态检查，提示是否进行 LLM review，最后询问是否启用。
- LLM 根据 active skill index 中的 description 判断是否需要 `skill_load(name)`。
- `scripts/` 辅助脚本没有额外 sandbox；执行仍走普通 `exec` 工具和 Guard。

## MCP 当前实现

当前 MCP 是基础 stdio tools-only runtime：

- daemon 启动时尝试启动 enabled 的 stdio server。
- 支持 initialize、tools/list、tools/call。
- 支持在 `/mcp` 面板运行态启停和 reload server。
- 单个 server 失败不会阻塞 daemon 启动，错误通过状态和 TUI 展示。
- 二进制结果保存为附件引用。

当前不支持：

- remote transport 的实际连接；`url`、`headers` 字段只是可持久化预留。
- resources、prompts、sampling。
- OAuth。
- MCP server sandbox。

环境变量边界：

- 默认只继承少量基础环境变量，如 `PATH`、`HOME`、`LANG`、`LC_*`、`TMPDIR`、`TEMP`、`TMP`。
- `[mcp.servers.<name>.env]` 传入字面量值。
- 当前不会展开 `${ENV_NAME}`。

## 配置与数据目录

默认数据目录：

```text
~/.suna/config.toml
~/.suna/credentials.toml
~/.suna/memory.db
~/.suna/skills/
~/.suna/attachments/
~/.suna/logs/app.log
```

`config.toml` 保存模型、UI、Guard、Workspace、Skill 启用状态、MCP server 等轻量配置。API Key 写入 `credentials.toml`。

完整字段见 [configuration.md](configuration.md)。

## 当前未完成或不应依赖的能力

- Trigger、定时任务、文件监听、主动感知链路。
- 多会话管理 UI、完整历史搜索、向量记忆或知识库。
- 完整 MCP runtime：remote transport、resources、prompts、sampling、OAuth、sandbox。
- Skill sandbox、市场、复杂生命周期 hooks。
- Hooks 执行链路；当前配置结构可保存但不会执行。
- 成本统计和价格计算。
- 完整 OS sandbox 或复杂权限 UI。
- TUI 断开后正在运行任务的完整事件回放/恢复。
