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

### 两种 Provider 实现

| Provider | SDK | 适用场景 |
|---|---|---|
| `openai` | `go-openai` | OpenAI 官方 + 所有 OpenAI 兼容 API（GLM、Qwen、DeepSeek、Moonshot 等） |
| `anthropic` | `anthropic-sdk-go` | Claude |

为什么不是全部用 `go-openai`：
- Anthropic 的 API 格式与 OpenAI 差异大（tool calling 格式、thinking blocks、content blocks）
- 用官方 SDK 更稳定，减少适配 bug
- 其他厂商（智谱、通义、DeepSeek、Kimi 等）全部兼容 OpenAI 协议，复用 `openai` provider

### 统一的消息格式

各厂商的请求/响应格式不同，但 Provider 负责转换为统一格式：

```
上层只看到:
  CompletionRequest  { Model, System, Messages, Tools, MaxTokens, Temperature }
  Chunk              { Content, ToolCalls, Done, Usage }

Provider 内部处理:
  openai    → ChatCompletionRequest → ChatCompletionResponse (改 baseURL)
  anthropic → MessageNewParams → Message
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

## Provider 类型

用户添加模型时，选择 provider 类型。只有两种内置类型，其余全部走 OpenAI 兼容：

```
┌─────────────────────────────────────────────────────────┐
│  provider = "openai"                                    │
│  内置 base_url: https://api.openai.com/v1              │
│  用户可覆盖 base_url                                    │
│  SDK: go-openai                                         │
├─────────────────────────────────────────────────────────┤
│  provider = "anthropic"                                 │
│  内置 base_url: https://api.anthropic.com               │
│  SDK: anthropic-sdk-go                                  │
├─────────────────────────────────────────────────────────┤
│  provider = "openai-compatible" (或任意自定义名称)       │
│  用户必须提供 base_url                                   │
│  SDK: go-openai (改 baseURL)                            │
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
# openai-compatible 需要提供 base_url，openai/anthropic 不需要（有内置地址）

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
- **provider 类型决定 SDK**：`openai` → go-openai，`anthropic` → anthropic-sdk-go，其余 → go-openai 改 baseURL
- **只有 openai 和 anthropic 内置 base_url**，其他 provider（如 glm/moonshot/deepseek）用户需提供 base_url
- **凭证按 provider 维度**：同一厂商的多个模型共享一个 API key，不重复配置
- **active_model**：指定主代理使用的模型，格式为 `provider/model`
- **strengths 偏好标签**：用于 LLM 路由判断，描述该模型擅长的领域
- **所有模型平级**：没有 default/fast/reasoning 的预设角色区分

## 路由策略

纯 LLM 驱动，无规则引擎。路由发生场景：sub agent 模型选择、用户在对话中指定模型。

```
┌───────────────────────────────────────────────┐
│  1. 用户显式指定                               │
│  用户说 "用 Claude 来做" → 直接用              │
│  Spawn 参数中指定 model="anthropic/xxx"       │
├───────────────────────────────────────────────┤
│  2. LLM 判断 (基于 strengths 偏好标签)        │
│  active_model 根据任务描述 + strengths 选模型  │
│  输入: 任务描述 + 各模型 strengths 标签        │
│  输出: provider/model                         │
├───────────────────────────────────────────────┤
│  Fallback: active_model                      │
│  以上都没命中 → 用主代理的 active_model       │
└───────────────────────────────────────────────┘
```

### LLM 路由

```go
func (r *Router) routeByLLM(ctx context.Context, task string) string {
    prompt := fmt.Sprintf(`
根据以下任务描述，选择最合适的模型。

可用模型:
%s

任务: %s

只回复 provider/model，不要解释。
`, r.modelStrengths(), task)

    resp := r.activeModel.Complete(ctx, &CompletionRequest{
        System:    "你是模型路由器，根据任务选择最合适的模型。",
        Messages:  []Message{{Role: "user", Content: prompt}},
        MaxTokens: 30,
    })

    return parseModelRef(resp)  // 解析 "provider/model"，找不到则 fallback 到 active_model
}
```

### strengths 偏好标签

每个模型可配置 strengths 列表，用于 LLM 路由判断：

```
模型路由的输入:
  - 任务描述 (一句话)
  - 各模型的 strengths 标签 (用户自定义，描述模型擅长领域)

LLM 据此判断:
  - "写前端页面" → 匹配 strengths 含"前端"的模型
  - "复杂推理" → 匹配 strengths 含"复杂推理"的模型
  - 未匹配到 → fallback 到 active_model

注意: strengths 是给 LLM 看的语义标签，不是程序逻辑
用户可以在对话中动态修改 strengths，agent 会更新配置
```

### 路由上下文传递

路由决策的上下文要尽量轻：

```
传入路由器的: 只有关键词或任务摘要 (几个词到一句话)
不传入的: 完整对话历史、工具调用记录、大段代码

原因:
  - 路由是为了省钱和提速，本身不能花很多钱和时间
  - 短输入 + active_model = 极低成本
```

## Sub Agent 的模型选择

Main agent 使用 active_model 运行。Sub agent 的模型通过路由决定：

```
场景 1: Main 显式指定子代理模型
  → Spawn({ task: "写前端页面", model: "moonshot/moonshot-v1-auto" })

场景 2: Main 不指定模型，内核走路由
  → Spawn({ task: "优化 SQL 查询" })  // 不指定 model
  → 内核根据 strengths 标签路由到合适的模型

场景 3: 用户在对话中指定
  用户: "用 Claude 来分析这段代码"
  → Main 理解意图，Spawn 时指定 model: "anthropic/claude-sonnet-4-20250514"

关键: Main agent 始终使用 active_model，不参与路由
      Sub agent 通过路由获得最合适的模型
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

### Anthropic prompt caching

Anthropic 支持 `cache_control` 标记，可以显式标记哪些内容应该被缓存：

```
System prompt 的能力列表部分 → 标记为 cacheable
  因为在一次会话中能力列表通常不变
  多次调用共享缓存，减少输入 token 计费
```

### 本地缓存（可选）

```
相同输入 → 相同输出的场景:
  - 模型路由决策 (相同关键词 → 相同路由)
  - Guard 审查 (相同操作模式 → 相同判断)

实现: 内存 LRU cache, key = hash(input), value = response
TTL: 5 分钟
```

## 错误处理与降级

```
模型调用失败时的策略:

1. 单次超时 → 重试 1 次 (相同模型)
2. 429 限流 → 等待 retry-after → 重试
3. 500 服务端错误 → 重试 1 次
4. 401/403 认证失败 → 不重试，通知用户检查 API key
5. 连续 3 次失败 → 降级到 active_model

Sub agent 失败:
  - 超时 → 通知 main，main 决定重试或换模型
  - 错误 → 结果中包含错误信息，main 自行处理
```
