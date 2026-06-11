# Subtask：主 Agent 驱动的动态模型与工具分配

Subtask 是 Suna 当前智能模型路由的核心能力。它不是简单的“开一个子对话”，而是由主 Agent 在运行时显式创建的隔离执行单元：主 Agent 选择模型、定义任务、裁剪上下文、决定是否传入图片，并按最小权限原则授予工具。

这让 Suna 可以在一次任务中组合不同模型的优势，同时避免子任务天然继承主会话的历史、记忆和完整工具箱。

## 核心价值

很多 Agent 的模型选择停留在“启动前选一个模型”或“全局 router 自动切换模型”。Suna 的 Subtask 更强调运行时编排。这个能力并不是因为单点实现多难，而是因为 Suna 已经把它接入了真实 Agent 循环：模型选择、上下文隔离、工具授权、Guard 审查和结果汇总是一套完整链路。

- **动态模型分配**：主 Agent 可根据当前子问题选择不同模型，而不是让整轮对话只能使用 active model。
- **独立上下文**：子任务不继承主对话历史、恢复会话、记忆或项目长上下文，只接收主 Agent 显式传入的信息。
- **动态工具分配**：每个 Subtask 有自己的工具白名单。不给工具时就是纯模型任务；只给读文件工具时就无法写文件；只给 MCP 某个工具时就只能调用该能力。
- **主 Agent 保持控制权**：Subtask 只返回结果。是否采纳、如何合并、是否继续追问用户、是否再调用工具，都由主 Agent 决定。
- **权限和路由绑定**：模型路由不是黑盒自动发生，而是和上下文裁剪、图片传递、工具授权一起在 `spawn` 调用中显式表达。

## 什么时候使用 Subtask

主 Agent 可以在这些场景中使用 Subtask：

- **独立 review**：让另一个模型独立审查方案、代码或安全风险，减少主模型自我确认偏差。
- **模型能力分工**：让强推理模型处理复杂判断，让低成本模型整理材料，让多模态模型分析图片。
- **上下文隔离**：只把必要片段交给子任务，避免子任务受到主对话中无关信息干扰。
- **权限隔离**：把子任务限制成只读分析、无工具推理，或只允许访问某几个 MCP / builtin 工具。
- **任务拆分**：主 Agent 把大任务拆成明确的小任务，最后自己综合结果。

Subtask 适合“边界清晰、输入明确、输出可汇总”的任务；不适合需要持续与用户交互或依赖完整主会话隐含状态的任务。

## `spawn` 工具模型

Subtask 通过 Agent runtime 工具 `spawn` 暴露给主 Agent。

主要参数：

| 参数 | 说明 |
|---|---|
| `model` | 必填。要使用的精确模型引用，例如 `<provider>/<model>`。必须来自当前可用模型列表。 |
| `task` | 必填。自包含的子任务描述。应说明目标、输出要求和判断标准。 |
| `context` | 可选。主 Agent 选择性传入的额外上下文。不会自动包含主对话历史。 |
| `tools` | 必填。允许子任务使用的工具名列表。`[]` 表示纯模型任务。 |
| `input_images` | 可选。当前用户消息中图片附件的索引列表，例如 `[0]`。不传则子任务看不到图片。 |
| `system` | 可选。备用 system prompt；正常情况下 Suna 会使用内置 Subtask prompt 模板。 |

设计上要求 `model` 和 `tools` 必须显式提供，是为了让模型选择和工具权限成为主 Agent 的有意识决策，而不是默认继承。

## 独立上下文边界

Subtask 的隔离规则是硬设计：

- 不继承主 Agent 的完整 system prompt。
- 不继承主对话历史。
- 不继承 restored conversation。
- 不继承 user profile memory。
- 不继承主 Agent 当前看到的所有附件。
- 不继承主 Agent 的完整工具目录。
- 不允许继续 `spawn` 子任务。
- 不允许调用 `askuser`。

Subtask 的初始 working memory 是新建的，只包含本次 `task` 和显式传入的图片 block。`context` 会进入 Subtask system prompt，作为主 Agent 给出的额外材料，而不是主会话的自动透传。

## 动态模型分配

Suna 支持配置多个模型连接。主 Agent 的 system prompt 会看到可用于 Subtask 的模型列表，包括模型引用、能力信息和上下文窗口等摘要。运行时，主 Agent 可以根据任务选择模型，例如：

```text
主对话模型：低成本快速模型
  ↓
遇到复杂架构判断
  ↓
spawn 到强推理模型，tools=[]，只做独立分析
  ↓
主 Agent 汇总结论并决定下一步
```

或者：

```text
用户上传图片并要求分析
  ↓
主 Agent 发现另一个模型具备多模态能力
  ↓
spawn(model=多模态模型, input_images=[0], tools=[])
  ↓
子任务只看这张图片和明确任务，不看主会话其它附件
```

当前实现不是“全局自动路由所有请求”。主对话仍有 active model；Subtask 是主 Agent 在任务中主动发起的动态模型分配。这种方式更可解释，也更容易和权限边界结合。

## 动态工具分配

Subtask 的工具权限由 `tools` 参数决定。

示例策略：

| 场景 | 推荐工具授权 |
|---|---|
| 独立方案评审 | `[]` |
| 只读代码审查 | `readfile`, `listdir`, `search` |
| 运行测试定位问题 | `readfile`, `listdir`, `search`, `exec` |
| 调用外部能力 | 指定的 `mcp__<server>__<tool>` |

只有可授权给 Subtask 的工具才会出现在 `spawn` 的 `tools` 枚举中。当前可授权来源主要是：

- builtin 工具，例如文件、搜索、命令、HTTP 等。
- MCP tools。

不会授权给 Subtask 的工具包括：

- `askuser`：用户交互必须由主 Agent 管理。
- `spawn`：避免子任务继续递归委派。
- `skill_load` / `skill_start`：Skill 加载和生命周期由主 Agent 控制。

即使某个工具被授权给 Subtask，它仍然不是无条件执行。行动类工具仍会经过 Guard、Workspace、敏感路径规则和必要的用户确认。

## 安全设计

Subtask 同时受两层边界保护：

1. **授予前边界**：主 Agent 只能从可授权工具中选择，并显式给出工具白名单。
2. **执行时边界**：Subtask 调用工具时仍走主 Agent 的 Guard 流程。

执行时还有这些约束：

- 未在白名单中的工具会被拒绝。
- 不可授权工具即使出现在参数中也会被拒绝。
- 敏感文件读取仍会被硬编码规则拦截。
- Workspace 启用时仍限制路径和命令工作目录。
- `smart` Guard review 会使用子任务自己的 working context，而不是误用主对话上下文。
- 子任务工具事件会带有 `spawn:<id>:` 命名空间，避免和主 Agent 工具调用混淆。

因此，Subtask 的能力不是“另一个拥有同等权限的 Agent”，而是一个由主 Agent 临时创建、按任务最小授权的受控执行器。

## 执行流程

```text
主 Agent 判断需要委派
  ↓
选择模型 model
  ↓
编写自包含 task
  ↓
裁剪 context，选择 input_images
  ↓
选择 tools 白名单
  ↓
调用 spawn
  ↓
internal/agent 校验模型和工具
  ↓
internal/subtask 创建新的 working memory
  ↓
runner 使用指定 model 运行子任务
  ↓
子任务工具调用经 subtaskExecutor 校验和 Guard
  ↓
返回 JSON 结果给主 Agent
  ↓
主 Agent 汇总、判断、继续执行或回复用户
```

关键代码位置：

- `internal/tools/agenttools/provider.go`：`spawn` schema、可授权工具枚举。
- `internal/agent/tools.go`：`ExecuteSpawnTool`、模型校验、工具白名单、图片传递、subtask executor。
- `internal/subtask/subtask.go`：独立 working memory 和 runner 调用。
- `internal/prompt/templates/system.md`：主 Agent 如何理解 delegation。
- `internal/prompt/templates/subtask_system.md`：Subtask 的隔离规则和输出要求。
- `internal/tools/types.go`：`CanGrantToSubtask` 工具授权边界。

## 和普通工具调用的区别

普通工具调用是“主 Agent 请求某个工具执行一次操作”。Subtask 则是“主 Agent 创建一个小型独立 Agent 运行一次模型循环”。

区别包括：

| 维度 | 普通工具调用 | Subtask |
|---|---|---|
| 是否调用模型 | 否，直接执行工具 | 是，子任务有自己的模型循环 |
| 模型选择 | 使用主 Agent 当前上下文 | 每次 `spawn` 显式指定模型 |
| 上下文 | 主 Agent 当前 working memory | 新 working memory，只含显式输入 |
| 工具 | 主 Agent 可见工具 | 每次 `spawn` 指定白名单 |
| 用户交互 | 主 Agent 可 `askuser` | 子任务不能 `askuser` |
| 再委派 | 主 Agent 可 `spawn` | 子任务不能再 `spawn` |
| 结果处理 | 工具结果进入主模型循环 | 子任务最终文本作为 `spawn` 结果进入主模型循环 |

## 当前边界

当前 Subtask 仍有边界：

- 不做自动并行调度；是否委派由主 Agent 决定。
- 不做跨 Subtask 的长期状态保存。
- 不支持子任务与用户直接交互。
- 不支持子任务递归 spawn。
- Subtask 的 stream / reasoning 不直接进入主聊天文本，只把工具事件和最终结果返回主 Agent。
- 它不是 sandbox；授权的外部命令或 MCP server 仍拥有其进程本身的系统权限，需依赖 Guard、Workspace 和用户信任边界。

这些限制是有意设计：Subtask 负责把“动态模型与工具分配”变成可控、可解释、可审查的主 Agent 编排能力，而不是让多个 Agent 不受控地互相调用。
