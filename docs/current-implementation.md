# 当前实现

本文按当前代码实际行为记录 Suna 的主要实现与边界。README 面向用户上手；本文件面向维护者和需要理解当前产品状态的人。

## 进程与入口

- `suna` 默认打开 TUI。
- `suna status` 查询 daemon 状态。
- `suna stop` 请求 daemon 正常停止；daemon 不可达但 PID 文件存在时，会尝试 fallback 停止旧进程并清理 PID 文件。
- `suna runtime --transport stdio` 启动单进程 headless runtime，供第三方 UI、桌面端、IDE 插件或本地 Web 服务通过 stdio 接入。
- `SUNA_RUN_DAEMON=1` 是内部 daemon 启动入口，普通用户不需要直接使用。
- TUI 启动前会先确保 daemon 可用；如果本地 endpoint 不可达，会后台拉起同一个可执行文件作为 daemon。

## TUI 当前行为

主要页面：

- Welcome：展示版本、新会话默认模型、用量、daemon uptime、memory、Guard、Workspace，并提供新会话、恢复会话、配置、帮助入口。
- Chat：对话、工具进度、AskUser、Guard、附件、模型选择、Skill/MCP 面板。
- Config：配置模型、Guard、Workspace、UI、附件等。
- Help：展示 slash commands 与帮助说明。

Chat slash commands：

| 命令 | 当前行为 |
|---|---|
| `/new` | 清空 TUI 当前消息并请求 daemon 新建会话。 |
| `/model` | 打开模型选择器。 |
| `/model <ref>` | 切换当前 session 的模型；`<ref>` 可为完整 `provider/model`，也可在当前 session provider 下只写 model；不修改新会话默认模型。 |
| `/memory` | 拉取并展示 user profile memory。 |
| `/mcp` | 打开 MCP 面板，查看 server 状态，支持启停和 reload。 |
| `/skills` | 打开 Skill 面板，查看 Skill 并切换启用状态。 |
| `/compact` | 手动触发当前会话 compact。 |
| `/config` | 进入 Config 页面。 |
| `/help` | 进入 Help 页面。 |

未注册的 `/文本` 会作为普通用户消息发送，不会报错或执行隐藏命令。

Chat transcript 的长历史渲染采用窗口化策略：TUI 页面状态保留完整消息和全局滚动 offset，但只把当前可见区域上下各一屏 overscan 的内容交给 Bubbles viewport。这个实现属于 Chat 业务层的虚拟滚动 / windowed rendering：viewport 不持有完整 transcript，滚动时按全局 y offset 切换可见窗口；但 Suna 不替换 Bubble Tea/Bubbles 的 terminal renderer，也不使用终端原生 scrollback 双模式。该策略不改变交互语义，鼠标滚轮、触控板、PageUp/PageDown、跳到回复开头/底部和 alt screen 行为保持不变；复制终端文本通过 `Ctrl+S` 进入选择模式，临时释放鼠标给终端原生选择，`Esc` 返回 TUI 滚动。滚动仍立即应用；如果新的 offset 仍在当前 overscan window 内，只移动 viewport offset，不重新同步 transcript，只有跨出 window 时才重建可见内容。

assistant streaming 阶段使用轻量纯文本渲染，并对正在追加的消息维护增量 wrap cache。新 delta 到达时只处理追加部分和必要的换行状态，不再对完整已生成回复反复 split/wrap/join；窗口宽度变化或内容回退时才完整重算。streaming 完成后会清除 streaming 标记并使用完整 Markdown 渲染缓存，因此最终阅读体验不降级。这个优化用少量“当前 streaming 消息的已换行行缓存”换掉长回复 O(n²) 重排 CPU；缓存规模与当前消息长度同阶，不按 chunk 数增长，回复结束后会随渲染缓存生命周期回收或替换。文本流活跃时，spinner tick 不额外触发完整 transcript sync；文本流停顿或等待首 token 时，spinner 仍正常刷新等待状态。

reasoning 展开/折叠只渲染主界面实际显示需要的小窗口：运行中按内容自适应增长并限制最大高度，完成后默认最多展示 3 行内容，展开态也按内容限高而不是固定撑满。已完成 reasoning 离屏时复用 line count；tab 会先展开为空格再 wrap，避免终端 tab stop 和宽度计算不一致导致边框错位。

已完成 assistant 的 Markdown 渲染结果使用有界缓存。缓存命中使用内容长度和 hash 判断，不额外保存完整原文；缓存超过内部预算时只裁剪远离当前窗口的旧 rendered output，保留原始消息、行数元数据、当前窗口附近内容和最近消息。滚回被裁剪的旧内容时会按需重新渲染，最终显示语义不变。viewport window 有内容签名，窗口和布局完全不变时会跳过重复 `SetContentLines`，避免 spinner/tick 等无语义变化刷新带来 CPU 抖动。

TUI 仍依赖 Bubble Tea/Bubbles 负责 terminal renderer、alt screen、mouse/keyboard handling 和 viewport 基础行为；Suna 只在 Chat transcript 业务层维护 blocks、global y offset、visible range 和有界 Markdown cache，不实现自定义 terminal renderer 或 terminal scrollback 双模式。

## Daemon 生命周期

当前 daemon / runtime 是按需运行的本地服务，不是长期任务调度器：

- TUI 或 CLI status/stop 通过本地 transport 连接后台 daemon。
- `suna runtime --transport stdio` 以前台单进程运行，父进程通过 stdio 连接 runtime。
- 每个连接建立时注册 event sink，断开时注销。
- local transport 使用 `idle_exit`：最后一个客户端断开后进入短暂宽限期；若没有新客户端连接，则取消当前 agent run 并退出。
- stdio transport 使用 `client_bound`：stdio 连接结束后 runtime 退出。
- 未处理的 `memory_queue` 保存在 SQLite 中，不在退出时强制 drain；下次启动后 worker 按批量策略继续处理。
- 目前没有 trigger、定时任务、文件监听、cowork/perception 等长期后台活动。

## 本地通信

- macOS/Linux local transport 使用 Unix socket。
- Windows local transport 使用 Named Pipe。
- 第三方 runtime 使用 stdio transport，命令为 `suna runtime --transport stdio`。
- local / stdio 都使用统一 protocol 和 JSON-RPC 2.0 风格 request / response / notification；local / stdio 的 framing 是 NDJSON。
- method request 必须返回明确 result 或结构化 error；daemon 主动事件通过 notification 下发。
- 模型输出使用 `agent.delta`，run 生命周期和错误恢复使用 `agent.run`，usage/context 使用 `agent.usage`。
- TUI 和第三方客户端都只通过 protocol 与 daemon/runtime 通信，不直接调用 agent、runner、tools、guard、memory、skill、mcp 等业务包。

## 模型与请求

当前支持三类模型协议：

- `protocol = "openai_responses"`：OpenAI Responses 协议。
- `protocol = "anthropic"`：Anthropic Messages 协议。
- `protocol = "openai_chat"`：OpenAI-compatible Chat Completions 协议。

模型 ref 为 `<provider>/<model>`。`provider` 用于厂商/凭证命名空间和匹配 `credentials.toml` 中的 API Key 分组，不再决定请求协议。

`models.reasoning` 是 Suna 对各模型协议“思考/推理强度相关参数”的统一抽象入口：provider 会把它映射/注入到对应协议请求体，Suna 只检查不要覆盖已经生成的核心字段。TUI preset 只负责生成这份 map，provider 不解析 preset 的业务含义。

LLM 请求使用按场景维护的 idle timeout，而不是任务总时长 timeout。Runner 内的主对话、工具调用前后请求和 subtask 内部请求默认按普通流式响应等待 120 秒；如果实际收到 provider 归一化后的 `ReasoningContent` chunk，本次请求会升级为 30 分钟 idle timeout。这个判断基于 LLM stream 实际返回类型，不基于 `models.reasoning` 配置或模型名。compact、Guard Smart Review、Skill LLM Review、记忆整理等不走 Runner 的单独 LLM 请求使用固定 idle timeout。

Runner 对主循环中的 model request 做内置 recovery：在尚未产生 assistant/reasoning/tool call 输出前，如果遇到结构化 HTTP 408/429/5xx 或网络/timeout 错误，会最多尝试 3 次，总间隔按 8 秒等待；已产生可见输出后的中断不会自动重试，最终失败仍通过 `agent.run state=failed` 暴露并由 `agent.resumeRun` 作为人工兜底。

当前 OpenAI Responses、OpenAI-compatible Chat 和 Anthropic provider 都会把协议返回的 reasoning/thinking delta 归一为 `ReasoningContent`。Chat-compatible provider 支持常见的 `reasoning_content` 字符串，以及 MiniMax M3 在 `reasoning_split=true` 时返回的 `reasoning_details[].text`；这些兼容逻辑只在 provider 层读取可选字段，字段不存在或格式不匹配时会忽略。Anthropic provider 使用 Messages streaming，把 Claude thinking block / delta 归一为 `ReasoningContent`，因此可以触发 Runner 的 30 分钟动态 reasoning idle timeout。

当前多模型智能选择主要用于 subtask：主 Agent 可查看经过 `subtask_for` 可见性过滤后的可用模型、上下文窗口、strengths 和多模态能力，然后在 `spawn` 时选择模型。每个 session 在创建时从 `active_model` 取得初始 `model_ref`，后续主对话、Guard Smart Review、Skill LLM Review、上下文压缩和记忆候选提取都使用该 session 的显式模型绑定；memory worker 则使用候选队列持久化的 `model_ref`。运行期不会回退到 `active_model`。

`llm.log` 中的 `request_prepare` 由 Runner 记录，用于对比 Suna 请求前的 `estimated_context_tokens`、`estimator_safety_tokens`、`compact_context_tokens` 和 `input_limit`；canonical `request` 由 Router 统一记录，覆盖 chat、subtask、Guard、Skill、compact 和 memory 等所有经 Router 的单次物理 LLM 请求，并包含 `provider`、`protocol`、`model_ref`、`model` 以及 provider 返回的 usage。Runner recovery 语义另由 `llm/recovery` 记录，避免把 retry attempt 等 runner 状态塞进 Router 或 `CompletionRequest`。

## Agent / Runner / Tool

- Agent 负责任务决策、上下文组装、工具执行入口、Guard 编排、Skill/MCP/subtask runtime 适配。
- Runner 执行模型流式调用和工具调用循环。
- `tools.Manager` 维护工具目录、schema 和执行路由，不做会话级安全决策。
- 模型可见工具应通过 `tools.Provider` 注册，避免在 Agent / Runner 中手工拼 schema。

内置工具：

| 工具 | 类型 | 当前用途 |
|---|---|---|
| `readfile` | 感知 | 按行范围、tail 或 base64 读取本地文件。 |
| `listdir` | 感知 | 列目录，支持递归、分页、include/exclude 和隐藏文件开关；`max_depth` 上限 3。 |
| `search` | 感知 | 通用本地搜索工具。`path` 可指向文件或目录；`mode=auto` 同时返回路径、轻量结构入口和正文分组，也可指定 `content` / `path` / `symbol`；`symbol` 表示文档标题、配置段/key、常见定义/声明等轻量结构入口，不限于代码。支持 `context`(默认 1，最大 5)、`limit`(默认 100，最大 1000)、`depth`(默认 8，最大 20)、include/exclude、`match=literal/regex/glob`、`case=smart/insensitive/sensitive`、`scope=workspace/deps/all` 和 `word`。默认排除常见依赖/构建/缓存/VCS 目录和凭据文件，并通过扫描文件数、文件大小、输出大小限制保持有界；空结果或截断时只在正文追加诊断提示，不改变 TUI 依赖的 metadata contract。 |
| `exec` | 行动 | 执行 shell 命令；Guard 会把可证明只读的命令归为 low risk。 |
| `writefile` | 行动 | 创建、覆盖或追加文件，支持 `create_dirs=true` 自动创建父目录和写前 SHA-256 校验；创建新文件场景应优先使用本工具而不是先 `filesystem mkdir` 再写。 |
| `editfile` | 行动 | 对单个文件原子应用一个或多个精确文本替换；默认要求 `old_string` 唯一匹配，`target="all"` 替换全部，`target="2"` 按 1-based 序号替换第 2 个匹配。 |
| `filesystem` | 行动 | `stat` / `mkdir` / `move` / `copy` / `remove` 文件系统路径；`stat` 为只读低风险调用。创建新文件优先使用 `writefile create_dirs=true`，避免无意义的预先 `mkdir`。 |
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
- `smart`：中高风险操作由当前 session 绑定的模型做 Smart Review。Review 只判断安全、用户意图和权限边界；安全且合理的调用会放行，不确定时请求确认，明确危险时拒绝，只有当前调用不安全或明显过宽且有具体等价替代时才建议修改。

Workspace 是本地文件和明显 exec 路径的目录硬边界，不能被用户 allowed rule 绕过。它不是 OS sandbox，无法限制外部程序启动后自行访问的文件、网络或进程权限。

敏感路径、内置 blocked rule、用户 blocked rule 优先级高于普通 allowed rule。

## 记忆与会话状态

默认 SQLite 数据库为 `~/.suna/memory.db`。

主要数据：

- `user_profile_memory`：长期 user profile memory，只保存少量跨会话稳定偏好、习惯、约束和纠错。
- `sessions`：多 session 元数据，包含 title、cwd、`model_ref`、message_count、created/updated/last_attached 时间。旧 session 首次 attach 时会一次性固化当时默认模型；已有 session 不会因默认模型变化而切换。
- `session_state`：每个 session 的 compacted state、最近可见 user/assistant 消息和 TUI-only 有界工具摘要。attach/create snapshot 会返回这些展示状态；不保存完整 tool timeline。
- `memory_queue`：user profile memory 临时提取队列，持久化候选产生时的 `model_ref`，由单 worker 按该引用批量处理。

自动 compact 在完整请求接近上下文窗口安全阈值时触发。阈值按 `context_window - max_output_tokens - margin` 计算，并在判断时使用 `compact_context_tokens = estimated_context_tokens + estimator_safety_tokens`；其中 `estimated_context_tokens` 是 Suna 请求前本地估算的输入上下文，`estimator_safety_tokens = max(8192, estimated_context_tokens / 16)`。compact 成功后，Session State 作为独立请求字段由 provider 注入模型上下文，working memory 只保留 budget-aware recent window，并在写回时复制消息 slice 以避免旧历史 backing array 被继续持有。compact 失败时不会使用伪摘要、overflow fallback retry 或硬裁剪继续，而是提示错误并停止本轮请求。

## 图片与附件

- TUI 识别粘贴的本地图片路径、图片 URL、图片 data URI；如果收到 `ctrl+v` 且终端没有传入 PasteMsg，会在 TUI 层读取系统剪贴板图片作为 fallback。
- 本地图片和图片 bytes 会保存到附件目录，发送消息时作为当前用户消息附件提交；data URI 和剪贴板图片会经过 MIME/大小校验，并按 SHA-256 内容 hash 去重落盘。
- 图片是否可被模型理解取决于当前 session 模型或被委派 subtask 模型的多模态能力。
- 非图片二进制不会作为图片粘贴附件加入；MCP 二进制结果也会保存到附件目录，并以文本引用返回给模型/TUI。

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
~/.suna/attachments/<session_id>/
~/.suna/logs/app.log
```

`config.toml` 保存模型、UI、Guard、Workspace、Skill 启用状态、MCP server 等轻量配置。API Key 写入 `credentials.toml`。

完整字段见 [configuration.md](configuration.md)。

## 当前未完成或不应依赖的能力

- Trigger、定时任务、文件监听、主动感知链路。
- Handoff 只提供多窗口 attach、观察和接力；不做完整事件回放或多人协作编辑。
- 已存在于 DB 的历史 session 不会自动常驻内存；但本次 daemon 生命周期内 attach 过的非空 session runtime 当前会作为热缓存保留，尚未实现 idle runtime LRU / unload。长期打开大量 session 时需要关注内存增长。
- 当前没有全局 active run 限流；不同 session 可以并行调用模型、工具、subtask 和 MCP server，资源消耗随并发 run 数增长。
- 完整 MCP runtime：remote transport、resources、prompts、sampling、OAuth、sandbox。
- Skill sandbox、市场、复杂生命周期 hooks。
- Hooks 执行链路；当前配置结构可保存但不会执行。
- 成本统计和价格计算。
- 完整 OS sandbox 或复杂权限 UI。
- TUI 断开后正在运行任务的完整事件回放/恢复。
