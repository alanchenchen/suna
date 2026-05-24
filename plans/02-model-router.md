# 02 — 多模型智能路由

Suna 的核心差异化之一：跨厂商多模型混用 + 智能路由。

## 统一 Provider 接口

所有模型厂商通过统一接口访问，上层代码不关心底层差异：

```go
type Provider interface {
    Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error)
    EstimateTokens(text string) int
    ContextWindow() int
}
```

### 三类 Provider 实现

| Provider | 协议/SDK | 适用场景 |
|---|---|---|
| `openai` | OpenAI Responses API | OpenAI 官方模型 |
| `anthropic` | `anthropic-sdk-go` Messages API | Claude |
| 自定义 provider | OpenAI Chat Completions 兼容协议 (`go-openai`) | GLM、Qwen、DeepSeek、Moonshot、Ollama、vLLM 等 |

为什么要拆开 OpenAI 官方和 OpenAI-compatible：
- OpenAI 官方优先走 Responses API，便于后续统一工具调用、usage 和多模态事件。
- OpenAI-compatible 厂商普遍兼容 Chat Completions，不一定支持 Responses API。
- Anthropic 的 API 格式与 OpenAI 差异大（tool calling 格式、thinking blocks、content blocks）
- 用官方 SDK 更稳定，减少适配 bug
- 其他厂商（智谱、通义、DeepSeek、Kimi 等）复用 Chat Completions provider，避免把各家 endpoint 内置到代码里

### 统一的消息格式

各厂商的请求/响应格式不同，但 Provider 负责转换为统一格式：

```
上层只看到:
  CompletionRequest  { Model, System, Messages, Tools, MaxTokens, Temperature }
  Chunk              { Content, ToolCalls, Done, Usage }

Provider 内部处理:
  openai             → Responses API stream → Chunk 流式输出，支持 usage/tool_calls
  openai-compatible  → ChatCompletionStream → Chunk 流式输出，支持 usage
  anthropic          → Messages.New → 一次性 Message，再转换为 content/tool_calls/done
```

### Tool Calling 的统一

这是适配层最复杂的部分。各厂商 tool calling 格式差异：

```
OpenAI:     function.name + function.arguments (JSON string)
Anthropic:  tool_use block with name + input (JSON object)
```

Provider 负责双向转换：
- `CompletionRequest.Tools[]` → 各厂商格式
- 各厂商的 tool_call 响应 → `Chunk.ToolCalls[]`

当前差异：OpenAI 官方和 OpenAI-compatible provider 已走 streaming；Anthropic provider 当前使用非 streaming API，收到完整响应后再发一个 content chunk 和 done，usage/reasoning 暂未映射。

### 多模态图片输入

Suna 内部统一使用 `model.ContentBlock` 表示图片，Provider 层按协议转换：

| Provider | 图片结构 |
|---|---|
| `openai` Responses | `input_image.image_url`，base64 图片先转为 `data:image/...;base64,...` |
| OpenAI-compatible Chat | `content[]` 中的 `image_url.url`，base64 图片同样使用 data URL |
| `anthropic` | `image.source`，URL 使用 `type=url`，base64 使用 `type=base64` + `media_type` + `data` |

当前实际支持图片输入；`ContentAudio` 只是预留类型，尚未映射到各 provider 协议。

## Provider 类型

用户添加模型时，选择 provider 类型。只有 `openai` 和 `anthropic` 是保留 provider ID，其余全部走 OpenAI-compatible Chat 协议：

```
┌─────────────────────────────────────────────────────────┐
│  provider = "openai"                                    │
│  API: https://api.openai.com/v1/responses               │
│  协议: OpenAI Responses                                 │
├─────────────────────────────────────────────────────────┤
│  provider = "anthropic"                                 │
│  API: Anthropic Messages                                │
│  SDK: anthropic-sdk-go                                  │
├─────────────────────────────────────────────────────────┤
│  provider = "openai-compatible" (或任意自定义名称)       │
│  用户必须提供 base_url                                   │
│  协议: OpenAI Chat Completions                           │
│  SDK: go-openai (改 baseURL)                             │
│  覆盖: GLM, Kimi, DeepSeek, Qwen, Ollama, vLLM, ...   │
└─────────────────────────────────────────────────────────┘
```

为什么不内置 GLM/Kimi 等厂商的 base_url：
- 同一厂商有 API 调用和 Plan 调用等不同接入方式，地址各不相同
- 厂商经常调整 API 地址，内置反而维护负担
- 用户对自己的 provider 地址是了解的，自行填写更准确

## 模型配置

配置以模型列表为主，`provider + model` 组合作为唯一标识。凭证与配置分离存储。

```toml
# 主代理使用的模型 (必填，格式为 "provider/model")
active_model = "glm/glm-4"

# 模型列表，每个模型平级
# openai-compatible 需要提供 base_url，openai/anthropic 不需要。

[[models]]
provider = "glm"
base_url = "https://open.bigmodel.cn/api/paas/v4"
model = "glm-4"
strengths = ["后端", "Go", "API 开发", "通用"]

[[models]]
provider = "glm"
model = "glm-4-flash"
base_url = "https://open.bigmodel.cn/api/paas/v4"
strengths = ["快速响应", "轻量任务", "节省成本"]

[[models]]
provider = "anthropic"
model = "claude-sonnet-4-20250514"
strengths = ["复杂推理", "长文写作", "代码审查"]

[[models]]
provider = "moonshot"
model = "moonshot-v1-auto"
base_url = "https://api.moonshot.cn/v1"
strengths = ["前端生成", "多模态", "图片理解"]

[[models]]
provider = "openai"
model = "gpt-4o"
strengths = ["通用", "多模态"]
```

### 凭证存储

API key 不在 config.toml 中配置，统一存储在凭证文件中：

```
~/.suna/credentials.toml    # 权限 0600，与 config 分离
```

```toml
# 按 provider 维度存 key，同一 provider 下的多个模型共享一个 key
[glm]
api_key = "sk-xxx..."

[anthropic]
api_key = "sk-ant-xxx..."

[moonshot]
api_key = "sk-xxx..."

[openai]
api_key = "sk-xxx..."
```

查找逻辑：
```
模型配置: provider="glm", model="glm-4"
  → 查找 credentials.toml 中 [glm].api_key
  → 用配置中的 base_url 调用（provider 不是内置的 openai/anthropic 时必须有 base_url）
```

### 设计说明

- **唯一标识：`provider/model`** — 如 `glm/glm-4`、`anthropic/claude-sonnet-4-20250514`，全局唯一
- **provider 类型决定协议**：`openai` → Responses API，`anthropic` → Anthropic Messages，其余 → OpenAI Chat Completions 兼容协议
- **只有 openai 和 anthropic 不需要 base_url**，其他 provider（如 glm/moonshot/deepseek）用户需提供 base_url
- **凭证按 provider 维度**：同一厂商的多个模型共享一个 API key，不重复配置
- **active_model**：指定主代理使用的模型，格式为 `provider/model`
- **strengths 偏好标签**：用于 LLM 路由判断，描述该模型擅长的领域
- **所有模型平级**：没有 default/fast/reasoning 的预设角色区分

## 路由策略

当前实现采用 **main-agent delegated routing**。main agent 始终使用用户手动选择的 `active_model`，不自动切换自身。智能路由只发生在 main agent 调用 `spawn` 时：main LLM 基于完整任务上下文、可用模型 strengths 和 `spawn` tool schema，显式选择 subtask 的 `model` 和 `tools`。

daemon 不再为 spawn 额外调用路由 LLM。`RouteWithLLM` / `routeByLLM` / `route.md` 已从主实现移除。

```
┌───────────────────────────────────────────────┐
│  1. 用户显式指定                               │
│  用户说 "用 Claude 来做" → 直接用              │
│  Spawn 参数中指定 model="anthropic/xxx"       │
├───────────────────────────────────────────────┤
│  2. Main LLM 判断 (基于完整上下文 + strengths) │
│  main 在 spawn tool call 中显式填写 model/tools │
│  输入: 对话上下文 + 可用模型 strengths + schema │
│  输出: spawn.model + spawn.tools              │
├───────────────────────────────────────────────┤
│  校验失败: 返回 tool error                    │
│  model/tools 缺失或非法 → main LLM 重新选择    │
└───────────────────────────────────────────────┘
```

### Main-Agent Delegated Routing

main system prompt 中动态注入可用于 spawned subtasks 的 models：

```text
Available models for spawned subtasks:
- glm/GLM-5.1: coding, reasoning; ctx 128k
- anthropic/claude-sonnet-4: architecture, review; ctx 200k
```

`spawn` tool schema 要求：

```json
{
  "required": ["task", "model", "tools"],
  "properties": {
    "model": {"type": "string", "description": "Exact model ref"},
    "tools": {"type": "array", "items": {"type": "string", "enum": ["readfile", "listdir", "exec"]}}
  }
}
```

daemon 只做校验和执行：

- `model` 为空或不是已配置 ref → tool error。
- `tools` 为空或包含不存在工具 → tool error。
- `spawn` 和 `askuser` 不能授予 subtask。
- subtask 只看到授权 tools 的 tool schema。

### strengths 偏好标签

每个模型可配置 strengths 列表，用于 main LLM 在 spawn 时判断：

```
模型选择输入:
  - 当前完整对话上下文
  - 子任务目标
  - 各模型的 strengths 标签 (用户自定义，描述模型擅长领域)
  - context window
  - spawn.tools schema

LLM 据此判断:
  - "写前端页面" → 匹配 strengths 含"前端"的模型
  - "复杂推理" → 匹配 strengths 含"复杂推理"的模型
  - 未匹配到 → main 应选择最小可用模型或直接不 spawn

注意: strengths 是给 LLM 看的语义标签，不是程序逻辑
用户可以在对话中动态修改 strengths，agent 会更新配置
```

### 路由上下文传递

不再有独立路由请求，因此不需要把任务摘要传给 router。main agent 已经拥有完整上下文，可以在同一个 LLM 回合中同时决定：

- 是否需要 spawn。
- subtask task。
- subtask model。
- subtask tools 权限。
- 传给 subtask 的简短 context。

这减少一次额外 LLM 请求，也避免 router 只看到 task 摘要而丢失用户意图。只有这些显式 spawn 字段会传入 subtask；subtask 不继承 main conversation、active memory 或 main working memory。

## Subtask 的模型选择

Main agent 使用 active_model 运行。Subtask 的模型由 main agent 在 `spawn.model` 中显式指定：

```
场景 1: Main 根据任务选择子代理模型
  → Spawn({ task: "写前端页面", model: "moonshot/moonshot-v1-auto", tools: ["readfile", "writefile"] })

场景 2: Main 缺少 model/tools
  → Spawn({ task: "优化 SQL 查询" })
  → daemon 返回 tool error: spawn requires explicit model/tools
  → main LLM 重新选择

场景 3: 用户在对话中指定
  用户: "用 Claude 来分析这段代码"
  → Main 理解意图，Spawn 时指定 model: "anthropic/claude-sonnet-4-20250514"

关键: Main agent 始终使用 active_model，不自动切换自身
      Subtask 只能使用 main 显式指定且 daemon 校验通过的模型
```

## 缓存策略

### API 缓存（prefix cache）

利用各厂商的 prefix cache 机制（OpenAI automatic prefix caching、Anthropic prompt caching）：

```
原则: 不变的内容放前面，变化的内容放后面

构建 CompletionRequest 时的固定顺序:
  1. System prompt (几乎不变)
  2. 工具定义 (很少变)
  3. 对话历史 (逐步追加)
  4. 最新消息 (每轮变)

不要做的:
  ❌ 每次重排 system prompt 中的段落
  ❌ 在固定段落中间插入动态内容
  ❌ 频繁变更工具定义
```

### Anthropic prompt caching — 目标设计

Anthropic 支持 `cache_control` 标记，可以显式标记哪些内容应该被缓存；当前 provider 适配层尚未写入 cache_control：

```
System prompt 的能力列表部分 → 标记为 cacheable
  因为在一次会话中能力列表通常不变
  多次调用共享缓存，减少输入 token 计费
```

### 本地缓存（可选，未实现）

```
相同输入 → 相同输出的场景:
  - Guard 审查 (相同操作模式 → 相同判断)

实现: 内存 LRU cache, key = hash(input), value = response
TTL: 5 分钟
```

## 错误处理与降级

```
当前行为:

1. Router 只负责选择 active provider/model 并做每模型 ref 的简单 rate limit。
2. provider 调用失败时，agent 立即把错误返回给 TUI/LLM 流程。
3. 当前没有统一 retry、retry-after 处理或跨模型 fallback。

Sub agent 失败:
  - 超时或错误 → spawn tool result 中返回失败状态，main LLM 自行决定是否重试、换模型或向用户说明
```
