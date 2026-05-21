# Suna 当前状态与阶段路线图

> 最后更新: 2026-05-21
> 目标：这份文档是 Suna 当前真实状态和下一步路线图的唯一入口。
> 原则：只把“可稳定依赖的闭环”视为完成；有代码但不闭环的模块统一标为 Prototype；只有表、模板、接口或设计的模块标为 Stub。

---

## 当前结论

Suna 目前已经跑通了最小 Agent 产品形态：daemon 启动、TUI chat、agent loop、tool calling、基础模型配置、IPC streaming、AskUser 和基本 session 流程都能工作。

但 Suna 还不是完整自主 Agent 平台。当前比较完整的只有四块：

1. **Agent Loop 主链路**：用户输入、模型流式输出、tool call、tool result 回填、继续对话。
2. **基础 Tools**：读文件、列目录、HTTP 读取、执行命令、写文件、编辑文件、HTTP 写请求、AskUser、Spawn 的入口已经存在。
3. **Chat / TUI MVP**：TUI 已经能完成基本对话、配置模型、展示工具调用、处理 AskUser options、显示 compact 结果。
4. **Daemon / IPC 基础设施**：TUI 与 daemon 分离，IPC 消息、stream、session、config 基础链路可用。

其余模块大多处于 Prototype 或 Stub：

- 智能路由已收敛为 main-agent delegated routing：main 固定 active model，spawn 时显式选择 sub-agent model/tools。
- Guard 已有最低安全闭环：支持 guard mode、真实用户确认、smart review 不确定转确认，main/sub 都受 Guard 约束。
- Memory 有表、worker、提取和 recall 的局部实现，但长期记忆不可靠。
- Context compact 有实现，但预算、摘要质量、window 使用和展示仍不完整。
- Capability 主要是 declarative SKILL.md 注入，script/MCP/runtime 没有完成。
- Trigger、QuickJS、MCP、hooks、trust、reflection、sandbox、marketplace 基本是预留或设计。

接下来不应继续扩张新能力，而应先把核心 MVP 收敛成可靠产品，再逐步闭环 Prototype 模块。

---

## 状态分级

| 状态 | 含义 | 文档规则 |
|---|---|---|
| Stable | 可作为长期基础依赖，行为明确，边界清楚 | 可以写入“已完成” |
| Usable MVP | 主链路可用，但体验、错误处理或边界仍粗糙 | 只能写“可用”，不能写“完整” |
| Prototype | 有代码，有局部链路，但不闭环或不可靠 | 必须列出缺口，不得写成完成 |
| Stub | 只有接口、表、模板、常量或设计，没有主链路 | 只能写“预留/未实现” |

---

## 模块状态仪表盘

| 模块 | 当前状态 | 真实结论 | 下一步方向 |
|---|---|---|---|
| Daemon / IPC | Usable MVP | 单 daemon + TUI IPC 可用，stream/config/session 基础链路通 | 稳定错误处理和多客户端边界 |
| TUI Chat | Usable MVP | chat、Suna 回合分组、tool 展示、AskUser、compact 展示、copy mode、注册 slash command 识别、场景式 Help 可跑通 | 补配置高级项入口和更稳的运行态展示 |
| Model Config | Usable MVP | TUI 可配置模型、API key、endpoint、context window、theme/locale、本地 config 路径、菜单式模型操作和按钮式删除确认 | 补高级配置入口和 provider test |
| Agent Loop | Usable MVP | 主对话、stream、tool loop、usage event、cancel 可用 | 稳定 context、usage、错误处理 |
| Tools | Usable MVP | 基础工具可调用，Act 工具会进入 Guard | 补并发限制、权限边界、危险操作 UX |
| Spawn | Usable MVP | 子 agent 能跑，model/tools 必填且执行层校验，sub 只看到授权 tools，禁用 askuser/spawn | 补最终 model/tools 展示、sub usage/session/memory |
| Smart Routing | Usable MVP | main agent 固定 active model；spawn 路由由 main LLM 基于模型 strengths 显式选择 model/tools | 补 model enum、路由可观察性和性能反馈 |
| Guard | Usable MVP | 支持 readonly/ask/auto/smart，真实 confirm，review 失败保守确认，main/sub 共用策略 | 补 modify 参数改写、规则编辑 UI、渐进信任 |
| Memory | Prototype | session、episodic、semantic、entity、worker 都有局部实现 | 先保证 session/tool persistence，再谈长期记忆 |
| Context / Compact | Prototype | 自动/手动 compact 存在，但预算和摘要链路不稳 | 明确 token/window 口径和压缩策略 |
| Usage / Cost | Prototype | token usage 局部可见，cost 基本没闭环 | 先统一 usage，再接 cost |
| Capability | Prototype / Stub | SKILL.md declarative 注入可用，script/MCP 只识别不执行 | 暂缓 runtime，先保持 declarative 清楚 |
| Trigger / Perception | Stub | 有表和 IPC 常量预留，没有主链路 | 暂缓 |
| Hooks / Trust / Reflection | Stub | 配置/表预留，没有执行闭环 | 暂缓 |
| QuickJS / MCP | Stub | capability 能识别类型，但无 runtime/client | 暂缓 |
| Sandbox / Marketplace | Stub | 设计层面，无实现 | 暂缓 |

---

## 关键设计边界

### Main Agent 与模型路由

main agent **必须使用 active model**。它不做自动模型选择，这是当前设计，不是缺陷。

智能路由只用于 spawn 子任务，但不再由 daemon 额外发起 `RouteWithLLM` 请求。当前实现是 **main-agent delegated routing**：

- main agent 接收用户请求，用 active model 规划和执行。
- main agent 决定是否调用 spawn。
- main agent 在 system prompt 中看到可用 sub-agent models 及 strengths。
- main agent 调用 spawn 时必须显式传入 `model` 和 `tools`。
- daemon 只校验 model/tools 是否有效，不做后台隐式路由。
- `RouteWithLLM`、`routeByLLM` 和 `route.md` 已从主实现移除。

因此后续不应把“main agent 未使用智能路由”列为问题。真正需要观察的是 main LLM 选择的 sub-agent model/tools 是否合理，以及这些选择是否可见、可调试。

### Spawn 与权限边界

当前 spawn 已经是可用 MVP，但还不是完整 delegation runtime。

当前事实：

- sub-agent 可以运行。
- sub-agent 禁止嵌套 spawn。
- sub-agent model 必须由 main agent 在 `spawn.model` 中显式指定。
- sub-agent tools 必须由 main agent 在 `spawn.tools` 中显式指定。
- daemon 校验 model ref、tool name，并拒绝缺失或非法参数。
- sub-agent 只收到授权 tools 的 schema，不能调用未授权工具。
- sub-agent 禁止 `askuser` 和 `spawn`。
- sub-agent 使用独立 `spawn_system.md`，不继承 main `system.md`。
- sub-agent 不保存独立 session/usage/memory。
- sub-agent 继承主 Guard policy、blocked/allowed、audit DB，并在 smart mode 下可使用同一 LLM reviewer。

短期设计边界：

- sub-agent 的第一层安全边界来自“启动前授予的工具集”。
- Guard 是第二层安全策略，对 main/sub 都生效。
- 不存在默认工具集；缺少 `tools` 会返回错误让 main LLM 重选。
- 高危工具必须显式授予，且仍会经过 Guard。
- TUI/日志应展示 sub-agent 最终使用的 model 和 tools。

### Guard 安全边界

当前 Guard 已有最低安全闭环，但不是完整权限系统。

当前事实：

- Act 工具会进入 Guard。
- Guard 支持 `readonly` / `ask` / `auto` / `smart` 四种 mode。
- Low risk 自动 approve。
- blocked rule 命中会 reject。
- `ask` mode 下 Medium/High risk 会真实暂停 tool 执行并等待用户确认。
- `smart` mode 下 Medium/High risk 会调用 LLM review。
- LLM review 失败、JSON parse 失败、不确定、confirm、modify 都会转用户确认，不再静默放行。
- `modify` 当前不做自动参数改写，会作为带 suggestion 的 confirm 处理。
- sub-agent 使用同一套 Guard policy。

短期设计边界：

- Guard confirm 是 daemon/core 事件，不是 TUI 业务逻辑。
- TUI 只展示 `agent.guard_confirm` 并通过 `agent.guardReply` 回传 approve/reject。
- confirm 时 tool 尚未执行，当前 tool 调用暂停等待确认。
- review 失败不能默认放行高风险操作。
- 没有实现参数改写前，`modify` 保守转用户确认。
- sub-agent 同时受 `spawn.tools` 和 Guard mode 约束。

### Memory 与 Context

当前 Memory 不能被描述为完整长期记忆。

当前事实：

- session persistence 可用，但 tool call/result persistence 不完整。
- episodic/semantic/entity 表和 store 存在。
- extraction worker 存在。
- FTS recall 存在。
- embedding 接口和部分存储存在，但主链路没闭环。
- entity recall 没接入主 prompt。
- session summary 没有完整写入链路。
- cost 没接入 usage 闭环。
- compact 有实现，但 context budget 和 token 口径仍不稳定。

短期设计边界：

- 先把 session 和 tool persistence 做可靠。
- 再做 memory recall 质量。
- 最后再做 embedding/entity/semantic 合并。
- 文档中不要再把 memory 写成完整四层记忆系统已完成。

---

## Phase 路线图

### Phase 0：文档止血与边界冻结

目标：让文档不再误导开发判断。

状态：当前应立即完成。

需要做：

- 将 `00-progress.md` 作为唯一真实状态入口。
- 所有设计文档顶部增加当前状态块。
- 区分 Current Implementation 和 Target Design。
- 明确哪些模块是 Usable MVP、Prototype、Stub。
- 明确 main agent 固定 active model。
- 明确 spawn/guard/memory/capability 都不是完整闭环。

完成标准：

- 任何人看 `00-progress.md` 能知道当前可依赖什么。
- 任何人看模块设计文档，能分清目标设计和当前实现。
- 不再因为“有代码/有表/有模板”把模块误判为完成。

不做：

- 不重写全部业务逻辑设计。
- 不追求文档漂亮。
- 不继续扩展新功能。

---

### Phase 1：核心 MVP 稳定化

目标：把已经能跑的 Agent Loop + TUI + Tool 基础链路变成可信 MVP。

需要做：

- 稳定 daemon / IPC / TUI 基础体验。
- 稳定 main agent loop。
- 稳定 tool call 和 tool result 顺序。
- 稳定 cancel、error、timeout 展示。
- 保证基础模型配置流程顺畅。
- 保证 AskUser options 可用。
- 保证 session restore 不破坏对话状态。
- TUI 明确展示真实状态，不隐藏失败或占位能力。

完成标准：

- 用户可以完成连续多轮对话。
- 用户可以配置模型并切换 active model。
- 用户可以看到 stream、tool、error、AskUser、compact 的真实状态。
- 基础读写/执行工具行为可预测。
- 没有明显“看起来成功但实际没生效”的 UI。

不做：

- 不做完整 Memory。
- 不做 Trigger。
- 不做 MCP/QuickJS。
- 不做完整权限系统。
- 不做能力市场。

---

### Phase 2：Spawn 与权限收敛

状态：大部分已完成，剩余可观察性和 usage/session/memory 收敛。

目标：让 sub-agent 成为边界清楚、权限可控的辅助执行单元。

需要做：

- 明确 spawn 的工具授权规则。
- `model` 和 `tools` 必填，不再有 router 推荐 tools 或 fallback 默认 tools。
- main LLM 基于 system prompt 中的模型 strengths 和 spawn schema 直接选择 model/tools。
- daemon 校验 model ref 和 tool name。
- 高危工具必须显式授予或通过明确策略授予。
- TUI/日志展示 sub-agent 最终 model 和 tools。
- 保持 main agent 固定 active model。
- 保持 sub-agent 禁止嵌套 spawn。

完成标准：

- spawn 缺少 model/tools 时返回可解释错误，让 main LLM 重选。
- spawn 传 tools 时，sub-agent 只能使用授予工具。
- main 选择的 model 生效并可被 daemon 校验。
- sub-agent 默认不会拿到任何隐式工具能力。

不做：

- 不做 sub-agent 独立长期记忆。
- 不做多层嵌套 agent。
- 不做复杂权限 UI。

---

### Phase 3：Guard 最低安全闭环

状态：最低安全闭环已完成，剩余渐进信任、参数改写和规则 UI。

目标：让安全系统不再“看起来审查了但实际放行”。

需要做：

- 明确哪些工具进入 Guard。
- 明确 low/medium/high 风险行为。
- 修正 LLM review 失败后的保守策略。
- 修正 `confirm` 不真实确认的问题。
- 修正 `modify` 不真实修改的问题（当前转 confirm，未做参数改写）。
- 保证 Guard reviewer 在 new/restore/spawn session 后不丢失。
- TUI 提供 Guard Mode 配置和 Guard confirm overlay。
- 保证 reject reason 能传到 TUI。
- 保证占位能力不会被文档写成完成。

完成标准：

- 高风险操作不会因为 review 失败而静默放行。
- confirm 不会假装已经处理；modify 暂时转用户确认。
- 用户能看到 Guard 拒绝原因。
- 主 agent 和 sub-agent 的 Act 工具安全行为可预测。
- sub-agent 的安全边界由 tools 授权 + Guard mode 控制，并在文档中明确。

不做：

- 不做完整渐进信任。
- 不做复杂策略语言。
- 不做完整 sub-agent guard 嵌套。
- 不做 sandbox。

---

### Phase 4：Session、Context 与 Usage 收敛

目标：让对话状态、上下文压缩和用量展示可信。

需要做：

- 保存完整 session 对话结构。
- 明确 tool call/result 是否持久化以及如何恢复。
- 修正 compact 的 context window 来源和 token 口径。
- 区分真实 provider usage 和估算值。
- 统一 TUI 中 token、context、compact 展示语义。
- 明确 usage cost 当前是否可用。
- 让 session restore 后的上下文可预测。

完成标准：

- restore 后用户能理解恢复了什么、没恢复什么。
- compact 后用户能看到真实或明确标注的估算状态。
- token/context 展示不伪造来源。
- tool call/result 不再导致 session 历史断裂。

不做：

- 不做完整长期记忆。
- 不做复杂 cost analytics。
- 不做模型表现评分。

---

### Phase 5：Memory 最小闭环

目标：把 Memory 从“有很多存储和 worker”收敛为一个可解释、可验证的记忆能力。

需要做：

- 明确 memory 当前要解决的问题。
- 明确哪些内容会被保存。
- 明确哪些内容会被 recall。
- 保证 extraction 不重复、不串 session、不误标完成。
- 决定先做 FTS 还是 embedding，不同时扩张。
- 如果启用 embedding，必须保证写入和 recall 都闭环。
- 如果启用 entity，必须保证 entity recall 接入 prompt 或明确只是存储。
- 明确 semantic facts 的追加、合并和展示策略。

完成标准：

- 用户能解释“为什么这条记忆被用到了”。
- recall 来源可观察。
- extraction 失败不会产生误导性完成状态。
- memory 不会被文档描述为超过实际能力。

不做：

- 不做主动习惯学习。
- 不做复杂知识图谱。
- 不做长期人格系统。
- 不做自动策略调整。

---

### Phase 6：Capability 收敛

目标：先把 declarative capability 做清楚，再决定是否进入 runtime。

需要做：

- 明确 SKILL.md declarative capability 的加载、展示、注入规则。
- 明确 `[LOAD_SKILL]` 的触发和生效时机。
- 明确 script 和 MCP 当前只是识别，不执行。
- 如果后续实现 script，先定义 runtime 边界。
- 如果后续实现 MCP，先定义 tool 注册和安全边界。

完成标准：

- declarative capability 行为可预测。
- 文档不再暗示 script/MCP 已可用。
- runtime 能力在实现前保持 Stub 状态。

不做：

- 不直接上 marketplace。
- 不直接上 MCP 全量生态。
- 不直接上 QuickJS 自动执行。

---

### Phase 7：扩展能力

目标：在核心 MVP、Spawn、Guard、Memory 都稳定后，再考虑平台扩展。

候选方向：

- Trigger / Perception。
- Hooks。
- Trust rules。
- Reflection / retry。
- QuickJS runtime。
- MCP client。
- WebSocket 多 I/O。
- Docker sandbox。
- Capability marketplace。

进入条件：

- Core MVP 已稳定。
- Spawn 权限边界明确。
- Guard 最低安全闭环完成。
- Session/context/usage 可信。
- Memory 不再误导。

当前建议：全部暂缓。

---

## 当前最高优先级缺口

### P0：必须先澄清或修正

| 问题 | 当前影响 | 期望状态 |
|---|---|---|
| 文档把 Prototype 写得像完成 | 开发判断混乱 | 设计目标和当前实现分离 |
| spawn 最终 model/tools 未展示 | 调试 delegation 困难 | TUI/日志展示最终执行边界 |
| sub-agent usage/session/memory 不完整 | 成本和历史不可追踪 | 补 usage/session 或明确不持久化 |
| Guard modify 未做参数改写 | smart review 的 modify 只能转 confirm | 后续实现参数 patch 或继续明确降级 |
| Guard rules 无 TUI 编辑 | 用户只能手写配置 | 后续高级配置页支持 blocked/allowed 编辑 |
| TUI 不展示最终 spawn model/tools | 调试困难 | 用户能看到最终执行边界 |

### P1：核心体验稳定

| 问题 | 当前影响 | 期望状态 |
|---|---|---|
| session restore 不完整 | 历史和上下文可能断裂 | 恢复语义明确 |
| tool call/result 未完整持久化 | restore 后 agent 上下文不完整 | tool 历史可恢复或明确不恢复 |
| compact token/window 口径不稳 | 用户误解上下文状态 | 真实值和估算值分开 |
| usage cost 不闭环 | cost 展示不可信 | 未完成前不暗示可用 |
| provider test 是占位 | 配置体验误导 | 明确 not implemented 或实现真实测试 |

### P2：暂缓但保留方向

| 问题 | 当前影响 | 期望状态 |
|---|---|---|
| embedding 未闭环 | 向量记忆不可依赖 | 写入和 recall 同时闭环 |
| entity recall 未接入 | entity store 只是存储 | 接入 prompt 或明确降级 |
| capability script/MCP 未执行 | 能力系统容易被误解 | 保持 Stub 标记 |
| trigger/hooks/trust 只有预留 | 容易扩大范围 | 暂缓到 Phase 7 |

---

## 文档维护规则

1. `00-progress.md` 是状态入口，只写真实状态和阶段方向。
2. 模块设计文档可以保留目标设计，但顶部必须写当前实现状态。
3. 有代码不等于完成；必须闭环、可观察、可解释才算完成。
4. Prototype 模块必须列出缺口。
5. Stub 模块不得写成已实现。
6. 每完成一个 Phase，先更新 `00-progress.md`，再更新对应模块设计文档。
7. 不在同一段落混写 Current Implementation 和 Target Design。

---

## 近期建议执行顺序

1. ~~完成 Phase 0：文档止血与边界冻结~~ ✅ 已完成
2. ~~执行 Phase 2：Spawn 与权限收敛~~ ✅ 已完成（model/tools 必填、执行层校验、sub 隔离）
3. ~~执行 Phase 3：Guard 最低安全闭环~~ ✅ 已完成（4 mode、真实 confirm、TUI overlay、Guard Mode 配置）
4. 执行 Phase 4：Session、Context 与 Usage 收敛
5. 之后再评估是否进入 Phase 5 Memory。

当前不建议推进 Phase 6/7。

### Phase 2/3 已完成工作清单

- 模型路由改造：删除 RouteWithLLM/routeByLLM/RouteResult/route.md；spawn schema required=["task","model","tools"]
- 系统提示词优化：重排 system.md 为 cache-friendly 顺序；删除 Current Time/User 字段
- Sub-agent 隔离：systemPromptOverride、spawn_system.md、禁止 askuser/spawn
- Guard mode：4 个 mode (readonly/ask/auto/smart)，真实 confirm，LLM review 修复
- IPC guard 协议：EventGuardConfirm、pendingGuards、handleGuardReply
- TUI guard confirm overlay：独立面板、键位操作、i18n 文案
- TUI Guard Mode 配置：Config home 切换、config.set 持久化
- 全部 go test/build 通过
