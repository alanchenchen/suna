# 02 — 多模型智能路由

Suna 的核心差异化之一：跨协议多模型混用 + 智能路由。

## 统一 Provider 接口

所有模型协议通过统一接口访问，上层代码不关心底层差异：

```go
type Provider interface {
    Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error)
    EstimateTokens(text string) int
    ContextWindow() int
}
```

### 三类 Provider 实现

这里的 `provider` 是协议适配器语义，不是官方厂商 endpoint。实际请求地址必须由 `models.base_url` 显式给出。

| Provider | 协议/SDK | 适用场景 |
|---|---|---|
| `openai` | OpenAI Responses API (`openai-go/v3`) | OpenAI Responses 协议；可指向官方或中转站 base URL |
| `anthropic` | Anthropic Messages API (`anthropic-sdk-go`) | Anthropic Messages 协议；可指向官方或中转站 base URL |
| 自定义 provider | OpenAI-compatible Chat Completions (`openai-go/v3`) | GLM、Qwen、DeepSeek、Moonshot、Ollama、vLLM 等兼容 endpoint |

为什么要拆开 OpenAI Responses 和 OpenAI-compatible Chat：
- `provider = "openai"` 明确表示 Responses 协议，便于统一工具调用、usage 和多模态事件。
- OpenAI-compatible 厂商普遍兼容 Chat Completions，不一定支持 Responses API。
- Anthropic 的 API 格式与 OpenAI 差异大（tool calling 格式、thinking blocks、content blocks）
- 用官方 SDK 更稳定，减少适配 bug
- daemon/core 不内置任何官方 endpoint；TUI 只负责新建模型时预填常见官方 URL，用户可改为中转站

### 统一的消息格式

各厂商的请求/响应格式不同，但 Provider 负责转换为统一格式：

```
上层只看到:
  CompletionRequest  { Model, System, Messages, Tools, MaxTokens, Temperature, Reasoning }
  Chunk              { Content, ToolCalls, Done, Usage }

Provider 内部处理:
  openai             → Responses API stream → Chunk 流式输出，支持 usage/tool_calls
  openai-compatible  → ChatCompletionStream → Chunk 流式输出，支持 usage/reasoning_content
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

当前差异：OpenAI Responses 和 OpenAI-compatible provider 已走 streaming；Anthropic provider 当前使用非 streaming API，收到完整响应后再发 content/tool_calls/done，并已支持 Claude thinking 参数。

OpenAI-compatible streaming 兼容策略：
- OpenAI Chat Completions 和 OpenAI Responses 都使用 `openai-go` 的 SSE stream。
- Suna 注册兼容 `text/event-stream` decoder，跳过 heartbeat/comment-only/empty SSE event，避免中转空事件触发 `unexpected end of JSON input`。
- OpenAI-compatible Chat 保留 `stream_options.include_usage=true`，同时继续解析服务端自行返回的 usage chunk。
- compatible header normalizer 不覆盖 `Accept`；仅保留 `User-Agent` 归一化和 Stainless 追踪头清理。

### 多模态图片输入

Suna 内部统一使用 `model.ContentBlock` + `MediaRef` 表示图片。agent、runner、subtask 只传轻量引用，Provider 层在请求构造时通过 media resolver 临时转换：

| Provider | 图片结构 |
|---|---|
| `openai` Responses | `input_image.image_url`；URL 直传，本地 path/attachment 临时转 data URL |
| OpenAI-compatible Chat | `content[]` 中的 `image_url.url`；URL 直传，本地 path/attachment 临时转 data URL |
| `anthropic` | `image.source`；URL 使用 `type=url`，本地 path/attachment 临时转 `type=base64` |

当前实际支持图片输入；audio/video/document 不在当前实现范围。

## Provider 类型

用户添加模型时，选择 provider 类型。`openai` 和 `anthropic` 是保留协议 ID，其余全部走 OpenAI-compatible Chat 协议。所有 provider 都必须显式配置 `base_url`：

```
┌─────────────────────────────────────────────────────────┐
│  provider = "openai"                                    │
│  协议: OpenAI Responses                                 │
│  base_url: 必填，可为 https://api.openai.com/v1 或中转站 │
├─────────────────────────────────────────────────────────┤
│  provider = "anthropic"                                 │
│  API: Anthropic Messages                                │
│  SDK: anthropic-sdk-go                                  │
│  base_url: 必填，可为 https://api.anthropic.com 或中转站 │
├─────────────────────────────────────────────────────────┤
│  provider = "openai-compatible" (或任意自定义名称)       │
│  用户必须提供 base_url                                   │
│  协议: OpenAI Chat Completions                           │
│  SDK: openai-go/v3 (改 baseURL)                          │
│  覆盖: GLM, Kimi, DeepSeek, Qwen, Ollama, vLLM, ...   │
└─────────────────────────────────────────────────────────┘
```

为什么 daemon/core 不内置 base_url：
- provider 是协议适配器，不是厂商 endpoint。
- 同一协议可以指向官方地址或中转站。
- 完整配置显式、可复现，也避免 SDK 默认 URL 造成隐式请求。
- TUI 层可以预填官方 URL，但 daemon 层只接受显式配置。

## 模型配置

配置以模型列表为主，`provider + model` 组合作为唯一标识。凭证与配置分离存储。

```toml
# 主代理使用的模型 (必填，格式为 "provider/model")
active_model = "glm/glm-4"

# 模型列表，每个模型平级
# 每个模型都需要提供 base_url；TUI 可预填官方 URL，但 daemon 不兜底。

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
base_url = "https://api.anthropic.com"
strengths = ["复杂推理", "长文写作", "代码审查"]

[[models]]
provider = "moonshot"
model = "moonshot-v1-auto"
base_url = "https://api.moonshot.cn/v1"
strengths = ["前端生成", "多模态", "图片理解"]

[[models]]
provider = "openai"
model = "gpt-4o"
base_url = "https://api.openai.com/v1"
strengths = ["通用", "多模态"]

[[models]]
provider = "deepseek"
model = "deepseek-v4-pro"
base_url = "https://api.deepseek.com/v1"
strengths = ["推理", "代码"]

[models.reasoning]
reasoning_effort = "max"

[models.reasoning.thinking]
type = "enabled"
```

### 凭证存储

API key 不在 config.toml 中配置，统一存储在凭证文件中：

```
<data-dir>/credentials.toml    # 当前默认 ~/.suna/credentials.toml，权限 0600，与 config 分离
```

凭证路径由 `internal/config/paths.go` 的 `DefaultCredentialsPath()` / `Config.CredentialsPath()` 派生，不在模型路由或 TUI 中手写 `$HOME/.suna`。

```toml
# 按 provider 维度存 key，同一 provider 下的多个模型共享一个 key
[glm]
api_key = "<API_KEY>"

[anthropic]
api_key = "<API_KEY>"

[moonshot]
api_key = "<API_KEY>"

[openai]
api_key = "<API_KEY>"
```

查找逻辑：
```
模型配置: provider="glm", model="glm-4"
  → 查找 credentials.toml 中 [glm].api_key
  → 用配置中的 base_url 调用（所有 provider 都必须有 base_url）
```

### OAuth / 登录型凭证（未来规划）

当前不实现 OAuth。Suna 的默认 provider 接入仍以 API key、显式 `base_url` 和模型配置为主，避免把登录流程、浏览器回调和 token 生命周期过早引入 daemon。

未来如需支持官方 OAuth、device code 或第三方授权平台，应作为统一 credential manager 能力设计，而不是放进 runner 或 provider adapter：

```
TUI / CLI:
  只负责发起登录、展示授权 URL / device code、展示成功或失败

Daemon / core credential manager:
  负责 OAuth flow 状态、token 持久化、refresh、logout 和权限边界

Provider adapter:
  只消费统一 credential / token source，不关心凭证来自 API key 还是 OAuth
```

设计约束：
- daemon 仍保持 headless 可用；TUI 不是唯一登录入口，CLI 可提供 `suna auth login <provider>` 一类命令。
- runner 不直接处理 OAuth、refresh token 或交互流程。
- provider adapter 不保存 token，只通过 credential manager 获取当前可用凭证。
- token 存储必须沿用凭证目录和权限边界，不写入 `config.toml`。
- 不支持非官方消费端账号复用或网页会话抓取；只考虑官方、稳定、可维护的授权机制。
- OAuth 能力等待明确用户需求后再排期，不作为 Gemini 等新 provider 的前置条件。

### 设计说明

- **唯一标识：`provider/model`** — 如 `glm/glm-4`、`anthropic/claude-sonnet-4-20250514`，全局唯一
- **provider 类型决定协议**：`openai` → Responses API，`anthropic` → Anthropic Messages，其余 → OpenAI Chat Completions 兼容协议
- **base_url 全显式**：所有 provider 都必须配置 `base_url`；TUI 只在新建 `openai`/`anthropic` 时预填官方 URL，用户可改为中转站
- **reasoning 透传**：`models.reasoning` 是思考相关请求字段组，由 TUI preset 或用户 custom 生成；daemon/core 不理解 preset，provider 注入请求时禁止覆盖已生成字段
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
    "tools": {"type": "array", "items": {"type": "string", "enum": ["readfile", "listdir", "exec"]}},
    "input_images": {"type": "array", "items": {"type": "integer"}}
  }
}
```

daemon 只做校验和执行：

- `model` 为空或不是已配置 ref → tool error。
- `tools` 缺失或包含不存在工具 → tool error；`tools=[]` 表示纯模型 subtask。
- `spawn` 和 `askuser` 不能授予 subtask。
- subtask 只看到授权 tools 的 tool schema。
- `input_images` 只引用当前用户消息图片索引，daemon 不接受 base64/path/url 作为 spawn 参数。

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

### API 缓存（prefix / KV cache）

Suna 不绑定具体厂商的缓存协议。当前策略是保持自然前缀稳定，让支持 automatic prefix cache、KV pool 或 prompt cache 的服务自行命中：

```
原则: 不变的内容放前面，变化的内容放后面

构建 CompletionRequest 时的固定顺序:
  1. System prompt (几乎不变)
  2. 工具定义 (很少变)
  3. Session State + recent messages (compact 后低频变化，普通轮次追加)
  4. Active memory (query-based，靠近当前用户消息)
  5. 最新消息 (每轮变)

不要做的:
  ❌ 每次重排 system prompt 中的段落
  ❌ 在固定段落中间插入动态内容
  ❌ 频繁变更工具定义
  ❌ 默认注入 provider-specific cache_control / prompt_cache_key
```

显式缓存断点（如 Anthropic `cache_control` 或 OpenAI `prompt_cache_key`）不是默认行为。若未来需要支持，应作为 provider/model 配置项引入，避免为了单个服务破坏 OpenAI-compatible 通用请求结构。

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
