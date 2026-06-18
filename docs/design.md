# 关键设计

本文解释 Suna 当前实现中的关键设计和取舍，帮助读者理解为什么系统这样分层、如何控制风险、如何管理上下文和本地状态。本文只描述当前代码实际支持的能力，不引用 `plans/` 作为当前行为依据。

## 设计理念

Suna 的设计取向是**轻量、克制、越用越懂你**。

- **轻量**：Go 单二进制、本地 daemon、TUI 客户端、SQLite/文件目录持久化，不依赖复杂服务栈。
- **克制**：不把所有能力做成命令和按钮；高级能力优先通过自然语言、少量 slash command、Skill 和工具编排表达。
- **越用越懂你**：长期记忆只保留稳定偏好、习惯、约束和纠错；会话状态负责当前任务，Skill 负责可复用工作方法。

Suna 的目标不是“功能越多越好”，而是在本地终端中保持一个可持续使用、可审查、可恢复的 Agent 工作台。

## 设计目标

Suna 的目标是在本地终端里提供一个可实际完成开发任务的通用 AI Agent：

- 能理解项目并调用本地工具。
- 能安全地读写文件、执行命令和访问 HTTP。
- 能在长对话中保留必要状态，又避免无限堆上下文。
- 能通过 Skill 和 MCP 扩展能力。
- 能让 TUI 保持轻量，把业务语义放在 daemon 和核心包。

## TUI + daemon 分层

Suna 没有把所有逻辑塞进 TUI，而是拆成：

```text
TUI
  ↓ protocol + local transport
Daemon
  ↓
Agent / Runner / Tools / Guard / Memory / Skill / MCP
```

这样做的原因：

- **UI 和业务解耦**：TUI 只处理输入、页面、浮层和渲染；模型调用、工具执行、Guard、记忆和持久化都在 daemon 侧。
- **状态集中**：配置、会话、附件、Skill、MCP server 状态统一由 daemon 协调，避免 UI 页面之间复制业务状态。
- **协议边界清晰**：TUI 与 daemon 通过 `internal/protocol` 交互，新增用户可见状态时需要显式建模。
- **便于未来扩展**：如果后续引入更多客户端或更复杂运行态，可以复用 daemon 侧能力。

当前 daemon 是按需运行的本地服务，不是长期任务调度器。最后一个客户端断开后，daemon 会短暂等待重连；如果没有新客户端，会取消当前运行并退出。

## 智能模型路由

Suna 支持配置多个模型连接，每个模型可以声明上下文窗口、能力标签和多模态能力。当前智能路由主要通过 Subtask 展示：主 Agent 在遇到适合拆分的独立任务时，可以选择更适合的模型执行子任务，例如用强推理模型做 review、用低成本模型整理信息、用多模态模型分析图片。

这个能力在当前很多 Agent 中并不常见。难点不只是“能不能调用另一个模型”，而是把模型选择、上下文裁剪、图片传递、工具授权和结果汇总放在同一个受控编排里：主 Agent 负责判断何时委派，Subtask 只负责完成明确的小任务。

这个设计没有把“所有请求都自动切模型”做成黑盒，而是让主 Agent 在明确的 `spawn` 调用中说明：

- 选择哪个模型。
- 子任务要做什么。
- 允许看到哪些上下文。
- 是否传入当前用户消息中的图片。
- 允许使用哪些工具。

因此智能模型路由和权限隔离是绑定的：模型可以按能力分工，但不会默认继承主对话的全部历史和工具权限。

## Agent / Runner 分工

Suna 把“业务编排”和“模型调用循环”分开：

- **Agent**：负责上下文组装、工具 schema、tool call 接管、Guard 编排、记忆、Skill、MCP、Subtask 等会话级语义。
- **Runner**：负责模型流式调用、reasoning/text/tool call 事件、工具调用循环、上下文压缩和基础重试。

这样可以保证安全决策不会散落在工具或 provider 中。模型可以提出 tool call，但真正是否执行由 Agent 结合 Guard、Workspace、敏感路径和用户选择统一决定。

## 工具体系设计

模型可见工具统一通过 `tools.Provider` 接入 `tools.Manager`。

当前工具来源包括：

- `internal/tools/builtin`：内置文件、命令、HTTP 工具。
- `internal/tools/agenttools`：`askuser`、`spawn` 等 Agent runtime 工具。
- `internal/tools/skilltools`：`skill_load`、`skill_start`。
- `internal/tools/mcptools`：MCP server 暴露的 tools。

设计原则：

- 工具目录、schema 和执行路由由 `tools.Manager` 管理。
- 工具声明自身 Guard policy，但不做会话级安全决策。
- 新工具优先作为 Provider 接入，不在 Agent 或 Runner 中手动拼 schema。
- 工具 schema 顺序和内容应尽量稳定，减少模型前缀缓存失效。

## Guard 和 Workspace

Suna 的工具能力比较强，因此 Guard 是核心设计。它不是单一的“弹窗确认”，而是多层安全边界组合：

- **硬编码高危拦截**：敏感路径、内置 blocked rule、Workspace 越界等硬边界优先于普通 allowed rule。
- **工具风险分级**：区分只读工具、行动类工具、HTTP 写请求、文件写入、命令执行等不同风险。
- **命令和路径分析**：对 shell 命令和文件路径做静态识别，可证明只读的命令可以降低风险等级。
- **用户可选模式**：`readonly`、`ask`、`auto`、`smart` 适配不同使用场景。
- **LLM Smart Review**：`smart` 模式下由 active model 结合工具名、参数和上下文审查 tool call 是否安全、是否符合用户意图、是否越过权限或 Workspace 边界。Smart Review 不是通用 tool-call optimizer，不负责修正普通参数风格、代码风格或“还能更窄一点”的常规优化；只有当前调用不安全或明显过宽，并且存在明确等价的更安全调用时才建议修改。

这套设计的目标是在“完全 auto 的风险”和“所有操作都打断用户”之间取得平衡：硬规则兜住高危边界，LLM review 负责理解更细的工具意图。相较于只提供 `auto` / `ask` 两档的 Agent，`smart` mode 的价值在于让安全层理解“这个工具调用为什么发生”，同时把职责限制在安全、用户意图和权限边界上，而不是代替模型优化每一次 tool call。

当前 Guard mode：

```text
ask       风险操作请求确认
smart     先由 active model 做安全审查，安全且符合意图则减少打扰，不确定或高风险时再确认/拒绝/建议更安全调用
auto      除硬性拦截规则外自动放行
readonly  只允许只读操作
```

Guard 会结合：

- 工具类型和参数。
- 只读 / 行动类判断。
- 文件路径风险。
- 敏感路径规则。
- Workspace 边界。
- 用户 blocked / allowed rule。
- Smart Review 结果。

Workspace 是路径边界，不是 OS sandbox：

- 它能限制 Suna 的文件工具和明显 exec 路径。
- 它不能限制外部程序启动后自行访问的文件、网络或进程权限。
- MCP server 和 Skill 脚本也没有额外 sandbox，启用前需要信任来源。

## 上下文和性能设计

Suna 的上下文设计兼顾可恢复性、长对话和模型前缀缓存。daemon、TUI、工具和流式传输的完整性能优化清单见 [性能优化](performance.md)。

模型请求倾向于保持这样的结构：

```text
稳定前缀：system / project instructions / skill index / tool schema
低频变化：Session State
近期内容：recent messages
靠近最新用户输入：user profile memory
```

关键取舍：

- **稳定前缀**：system prompt、项目指令、Skill 索引和工具 schema 尽量稳定，减少不必要的前缀缓存失效。
- **Session State 不塞进 system prompt**：compact 生成的当前会话状态作为独立上下文字段参与请求，避免 system prompt 频繁变化。
- **recent window 而不是完整历史**：恢复和后续请求使用 Session State + 最近对话窗口，而不是无上限重放所有原始 tool call/result。
- **主动 compact 而不是极限塞满上下文**：Suna 用本地估算、完整输出预留和 estimator safety 在安全边界前触发 compact，优先保证长工具任务连续性，不把 provider context overflow fallback 作为常规路径。
- **compact 失败不硬继续**：上下文压缩失败时，Suna 会停止本轮请求并提示错误，不使用不可靠摘要或强行裁剪继续。
- **工具 schema 稳定性**：工具名称、描述、参数和排序变化会影响模型缓存和行为，修改时需要谨慎。

## 记忆设计

Suna 的 user profile memory 是轻量长期记忆，不是知识库。

它只保存少量跨会话稳定信息，例如：

- 用户长期偏好。
- 工作方式。
- 明确约束。
- 反复纠正过的事实。

它不用于保存完整聊天历史、项目知识库或向量检索结果。当前会话的任务状态主要由 Session State 和 recent messages 承担。

记忆提取通过队列和 worker 批量处理。daemon 退出时不会强制 drain 未处理队列，pending item 会保留在 SQLite 中，下次启动后继续处理。

## Skill 设计

Skill 用于告诉 Suna 某类任务应该如何处理。

当前 Skill 是目录式结构：

```text
~/.suna/skills/<skill-name>/
└── SKILL.md
```

可选包含：

```text
references/
scripts/
examples/
assets/
```

设计取舍：

- daemon 启动时轻量扫描 Skill 元信息。
- Agent 先看到 active skill index，再根据 description 决定是否 `skill_load` 完整内容。
- 导入或生成 Skill 后不会直接静默启用，而是统一走静态预检查、可选 LLM check、用户确认启用流程。
- 静态预检查负责发现结构、元信息和基本格式问题；可选 LLM check 负责从语义和安全角度 review Skill 内容。
- `scripts/` 没有独立 sandbox，执行仍依赖普通工具和 Guard。

## MCP 设计

当前 MCP 是基础 stdio tools-only runtime。

支持：

- stdio server。
- initialize。
- tools/list。
- tools/call。
- `/mcp` 面板查看状态、启停和 reload。

不支持：

- remote transport 实际连接。
- resources。
- prompts。
- sampling。
- OAuth。
- MCP server sandbox。

单个 MCP server 启动或 reload 失败不会阻塞 Suna/daemon 启动。错误会进入运行态状态和日志，由 `/mcp` 面板展示。

## Subtask 设计

Subtask 是 Suna 当前智能模型路由的主要体现。它不是普通子对话，而是由主 Agent 在运行时创建的隔离执行单元：主 Agent 明确选择模型、任务、上下文、图片和工具白名单，Subtask 只负责完成这一个子任务并把结果返回主 Agent。

关键点：

- **完全由主 Agent 驱动**：是否委派、委派给哪个模型、传入什么上下文、开放哪些工具，都由主 Agent 在 `spawn` 调用中决定。
- **动态模型分配**：主 Agent 可以根据模型配置中的上下文窗口、能力标签和多模态能力，选择更适合的模型处理子任务。
- **动态工具分配**：每个 Subtask 都有独立工具白名单。`tools: []` 表示纯模型任务；只授权 `readfile` / `search` 时就是只读分析；也可以只授权某个 MCP tool。
- **独立上下文**：Subtask 不继承主对话历史、恢复会话、记忆、完整附件或完整工具目录。
- **主 Agent 汇总决策**：Subtask 不能直接接管用户交互，最终如何采纳、合并和继续执行仍由主 Agent 决定。

隔离规则：

- 不继承主对话历史。
- 只能看到主 Agent 显式传入的 task / context。
- 只能使用主 Agent 显式授权的工具。
- 不能继续 spawn 子任务。
- 不能使用 askuser。
- 图片只通过 `input_images` 显式传递当前用户消息中的附件。

即使工具被授权给 Subtask，行动类调用仍会经过 Guard、Workspace 和敏感路径规则。因此 Subtask 是“按任务最小授权的受控执行器”，不是另一个拥有完整权限的 Agent。

更完整的说明见 [Subtask 设计](subtask.md)。

## 本地数据设计

默认数据目录是 `~/.suna/`：

```text
config.toml        # 主配置
credentials.toml   # API Key
memory.db          # 记忆、会话、用量等本地数据
skills/            # Skill 目录
attachments/       # 图片和二进制附件
logs/app.log       # 日志
```

API Key 不写入 `config.toml`，而是保存在 `credentials.toml`。排查问题时优先查看 `logs/app.log`。

Suna 目前处于快速开发状态，数据库 schema 和运行状态可能变化。升级后如果遇到异常，建议先 `suna stop`，备份必要数据，再清理数据目录中的 `.db` 文件。

## 当前边界

以下能力当前不要按完整产品能力依赖：

- Trigger、定时任务、文件监听等主动感知链路。
- 多会话管理 UI、完整历史搜索、向量记忆或知识库。
- 完整 MCP runtime。
- Skill sandbox、市场和复杂生命周期 hooks。
- Hooks 执行链路。
- 成本统计和价格计算。
- 完整 OS sandbox 或复杂权限 UI。
- TUI 断开后对正在运行任务的完整事件回放/恢复。
