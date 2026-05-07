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

### 三种 Provider 实现

| Provider | 覆盖厂商 | 实现方式 |
|---|---|---|
| `OpenAIProvider` | OpenAI, GLM, Qwen, DeepSeek, Moonshot(Kimi) | `go-openai` 改 baseURL |
| `AnthropicProvider` | Claude | `anthropic-sdk-go` |
| `GenericProvider` | 任意 OpenAI 兼容 API | `net/http` + 手动解析 |

为什么不是全部用 `go-openai`：
- Anthropic 的 API 格式与 OpenAI 差异大（tool calling 格式、thinking blocks、content blocks）
- 用官方 SDK 更稳定，减少适配 bug
- OpenAI 兼容格式的厂商（智谱、通义、DeepSeek、Kimi）全部复用 `OpenAIProvider`

### 统一的消息格式

各厂商的请求/响应格式不同，但 Provider 负责转换为统一格式：

```
上层只看到:
  CompletionRequest  { Model, System, Messages, Tools, MaxTokens, Temperature }
  Chunk              { Content, ToolCalls, Done, Usage }

Provider 内部处理:
  OpenAI    → ChatCompletionRequest → ChatCompletionResponse
  Anthropic → MessageNewParams → Message
  Generic   → HTTP JSON → 手动解析
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

## 模型配置

用户在 `~/.suna/config.toml` 中配置模型预设：

```toml
# 必须有一个 default
[models.default]
provider = "openai"                       # openai | anthropic | generic
model = "glm-4"
base_url = "https://open.bigmodel.cn/api/paas/v4"
api_key_env = "GLM_API_KEY"               # 从环境变量读取 key
context_window = 128000                    # 可选, 不填则用默认值

[models.fast]
provider = "openai"
model = "glm-4-flash"
base_url = "https://open.bigmodel.cn/api/paas/v4"
api_key_env = "GLM_API_KEY"
cost_per_1k = 0.001                       # 可选, 用于成本估算

[models.reasoning]
provider = "anthropic"
model = "claude-sonnet-4-20250514"
api_key_env = "ANTHROPIC_API_KEY"

[models.kimi]
provider = "openai"
model = "moonshot-v1-auto"
base_url = "https://api.moonshot.cn/v1"
api_key_env = "MOONSHOT_API_KEY"
strengths = ["多模态", "前端生成", "图片理解"]  # 可选, 用户自定义标签
```

## 路由策略

三层路由，逐层 fallback：

```
┌───────────────────────────────────────────────┐
│  Layer 1: 显式指定                             │
│  Spawn 参数中指定 model="kimi" → 直接用        │
│  用户说 "用 GLM 来做" → 匹配模型名             │
│  命中率: ~10%                                  │
├───────────────────────────────────────────────┤
│  Layer 2: 规则匹配                             │
│  config.toml 中的 router.rules                 │
│  正则匹配用户消息中的关键词                     │
│  命中率: ~60%                                  │
├───────────────────────────────────────────────┤
│  Layer 3: LLM 判断                             │
│  规则未命中 → 用 fast 模型判断任务适合哪个模型   │
│  输入: 任务描述 + 各模型 strengths 标签         │
│  输出: 模型名                                  │
│  命中率: ~20%                                  │
├───────────────────────────────────────────────┤
│  Fallback: default 模型                        │
│  以上都没命中 → 用 default                      │
└───────────────────────────────────────────────┘
```

### 规则路由

```toml
[router]
default = "default"
rules = [
  { pattern = "前端|页面|样式|CSS|组件", model = "kimi" },
  { pattern = "逻辑|算法|后端|API|数据库", model = "default" },
  { pattern = "复杂推理|长文|写作|分析报告", model = "reasoning" },
]
```

匹配逻辑：遍历 rules，第一个 pattern 命中即返回。无命中进入 Layer 3。

### LLM 路由

```go
// 用最便宜的模型做路由决策
func (r *Router) routeByLLM(ctx context.Context, task string) string {
    prompt := fmt.Sprintf(`
根据以下任务描述，选择最合适的模型。

可用模型:
%s

任务: %s

只回复模型名称，不要解释。
`, r.modelDescriptions(), task)

    resp := r.fastModel.Complete(ctx, &CompletionRequest{
        System:    "你是模型路由器，根据任务选择最合适的模型。",
        Messages:  []Message{{Role: "user", Content: prompt}},
        MaxTokens: 20,
    })

    return parseModelName(resp)  // 解析模型名，找不到则 fallback
}
```

### 路由上下文传递

路由决策的上下文要尽量轻：

```
传入路由器的: 只有关键词或任务摘要 (几个词到一句话)
不传入的: 完整对话历史、工具调用记录、大段代码

原因:
  - 路由是为了省钱和提速，本身不能花很多钱和时间
  - fast 模型 + 短输入 = 极低成本 (< $0.0001)
```

## Sub Agent 的模型选择

Main agent 通过 Spawn 创建 sub agent 时指定模型：

```
场景 1: Main 知道子任务类型
  → Spawn({ task: "写前端页面", model: "kimi" })  // 显式指定

场景 2: Main 不知道用什么模型
  → Spawn({ task: "优化 SQL 查询" })  // 不指定 model
  → 内核走路由流程自动选择

场景 3: 用户要求特定模型
  用户: "用 Claude 来分析这段代码"
  → Main 理解意图，Spawn 时指定 model: "reasoning"
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
5. 连续 3 次失败 → 降级到 default 模型

Sub agent 失败:
  - 超时 → 通知 main，main 决定重试或换模型
  - 错误 → 结果中包含错误信息，main 自行处理
```
