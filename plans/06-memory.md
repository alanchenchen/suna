# 06 — 层次化记忆

记忆不是对话历史的附件，而是独立的层次化知识库。传统 agent 把所有信息塞进线性对话历史 → 越用越慢 → 压缩丢信息。Suna 的记忆是按需精准检索 → 越用越快。

## 设计原则

```
原则 1: 记忆不是对话历史的附件，而是独立的层次化知识库
原则 2: 新旧事实共存，不做覆盖/删除 (仅添加式提取)
原则 3: Agent 生成的信息是第一类的 (不只是记住用户说的)
原则 4: 检索是多信号的 (语义 + 关键词 + 实体)
原则 5: 时间推理是内置的 (理解"之前"和"现在"的关系)
原则 6: 不引入额外依赖，向量用 SQLite BLOB 存储
```

## 4 层记忆

```
┌─────────────────────────────────────────────────────────────┐
│ 工作记忆 (Working Memory)                                     │
│ 存储: Daemon 进程内                                           │
│ 生命周期: 当前任务                                              │
│ 内容: 对话历史 + 任务状态 + 工具调用结果                        │
│ 访问: 自动注入每轮对话                                         │
│ 归档: 异步写入提取队列，daemon worker 后台处理                 │
├─────────────────────────────────────────────────────────────┤
│ 情景记忆 (Episodic Memory)                                    │
│ 存储: SQLite + 向量 (BLOB)                                     │
│ 生命周期: 永久 (30天未使用降低检索权重)                         │
│ 内容: 每次交互的片段——谁说了什么、做了什么、结果如何             │
│ 特点: 仅添加式提取；支持时间线回溯                              │
│ 检索: 语义 + 关键词 + 实体 + 时间范围                          │
├─────────────────────────────────────────────────────────────┤
│ 语义记忆 (Semantic Memory)                                     │
│ 存储: SQLite (结构化) + 文件系统 (SKILL.md)                    │
│ 生命周期: 永久                                                  │
│ 内容: 从情景记忆中提取的结构化知识                              │
│       - 用户偏好、习惯、约束                                    │
│       - 技能定义 (SKILL.md + main.js + mcp.json)              │
│       - 项目知识 (架构、技术栈、约定)                            │
│       - 模型表现记录 (哪个模型在什么任务上表现如何)              │
│ 特点: 仅添加式提取；新旧事实共存，用时间戳区分                  │
├─────────────────────────────────────────────────────────────┤
│ 程序记忆 (Procedural Memory)                                   │
│ 存储: 文件系统 (capabilities/)                                  │
│ 生命周期: 永久                                                  │
│ 内容: agent 学会的"怎么做"——技能和操作模式                      │
│       - SKILL.md (怎么做某类事)                                 │
│       - main.js (确定性逻辑)                                   │
│       - mcp.json (外部服务集成)                                │
│ 特点: "知道什么" vs "知道怎么做"                               │
│       由学习流程自动从情景记忆中提炼                            │
└─────────────────────────────────────────────────────────────┘
```

## 各层详细设计

### Layer 1: 工作记忆

```
存储结构 (daemon 进程内):
  type WorkingMemory struct {
      Messages []Message
      TaskState map[string]any
  }

压缩策略 (见 01-architecture.md):
  当 token 估算 > 上下文窗口 × 80% 时:
    1. 先截断工具输出
    2. 再压缩早期对话为摘要
    3. 保留对话骨架

关键: 压缩只影响对话历史展示，原始信息仍在情景记忆中
  → 用户或 agent 可以通过 /memory search 精准回溯
```

### Layer 2: 情景记忆

这是和原设计差异最大的部分。不再是对话历史的简单存档，而是独立的知识库。

#### 仅添加式提取 (Append-Only Extraction)

Daemon 中的 Memory Worker 异步批量处理，不阻塞 Agent Loop，不受 TUI 生命周期影响。

```
提取流程:

每轮对话结束 → 显著性判断 → 中/高 → 发送到 memory channel
                ↓
Memory Worker (独立 goroutine，常驻):
  - 积攒到 N 轮或空闲 M 秒后批量提取
  - 不阻塞 Agent Loop
  - TUI 关闭后 worker 继续处理
  - Daemon 重启后扫描 session_messages.memory_extracted=0 补处理

触发条件 (满足任一即触发提取):
  - 队列积攒 ≥ 5 轮未提取的交互
  - 距上次提取 ≥ 60 秒
  - 队列中存在高显著性交互 (见下方)

提取方式: 单次 LLM 调用 (active_model)，同时输出情景记忆 + 语义记忆
  prompt = "从以下交互中提取:
    1. 所有值得记住的事实片段 (情景记忆)
    2. 结构化的用户偏好/约束/习惯 (语义记忆)
    3. 关键实体名称"

  输出 JSON:
  {
    "episodes": [
      {"content": "用户偏好 TOML 作为配置格式", "type": "preference",
       "entities": ["TOML", "配置"]},
      {"content": "Agent 确认配置格式改为 TOML", "type": "action",
       "entities": ["TOML"]}
    ],
    "facts": [
      {"key": "config_format", "value": "TOML", "type": "preference"},
      {"key": "language", "value": "中文", "type": "preference"}
    ]
  }

  一次调用 → 写入 episodic_memories + semantic_facts + entities
  不需要分开两次 LLM 调用

注意: agent 确认的信息也被记录 (agent-generated facts are first-class)

不做的事情:
  ❌ 不和旧记忆做 diff/reconcile (不做 UPDATE/DELETE)
  ❌ 不覆盖旧事实
  ❌ 新事实直接追加

检索时:
  查询 "用户喜欢什么配置格式"
  → 命中两条: "YAML" (旧) 和 "TOML" (新)
  → 按时间排序，取最新的
  → 但旧的不删，因为可能需要 "之前为什么用 YAML"
```

#### 显著性过滤 (Significance Filtering)

不是每轮交互都值得提取记忆。减少无用提取，降低 LLM 调用频率。

```
Fast path (零 LLM 成本，在入队时判断):
  高显著性 (立即触发提取):
    - 用户说 "以后都这样" / "记住" / "不要" 等明确指令
    - 工具执行失败 (exit_code != 0)
    - Guard 拦截了操作
    - 用户纠正了 agent 的输出

  中显著性 (正常排队):
    - 包含工具调用的交互
    - 用户的非简单查询消息
    - Agent 做出了决策

  低显著性 (跳过提取):
    - 纯闲聊 / 简单问候
    - 用户只回复 "好" / "继续" / "OK"
    - 单轮信息查询 (如 "今天天气" "几点了")

Slow path (LLM 判断):
  - 无法确定显著性 → 默认中显著性

LLM 调用频率: 从每轮 1 次降到每 5 轮 ~0.3 次
```

#### 为什么不做 UPDATE/DELETE

```
传统记忆系统: 新事实覆盖旧事实
  问题 1: 信息丢失——"用户住在北京" 被 "用户搬到上海" 覆盖
  问题 2: 丢失时间线——无法知道"搬家"这个事件本身
  问题 3: 覆盖时可能误删关键信息

仅添加式:
  "用户住在北京" (2025-05) + "用户搬到上海" (2026-03)
  → agent 知道"旧住址是北京，现住址是上海"
  → 知道搬家发生在 2026-03
  → "你之前在北京时认识的人" 也能回答

参考: Mem0 2026.4 的 token-efficient algorithm 证明了
      仅添加式提取在 LoCoMo (+20.2) 和 LongMemEval (+25.6) 
      上大幅优于覆盖式，尤其在时间推理 (+29.6) 上
```

### Layer 3: 语义记忆

#### 结构化知识

```
SQLite 表: semantic_facts
| id | type | key | value | source | ts |
|----|------|-----|-------|--------|----|
| 1  | preference | config_format | TOML | user_stated | 2026-05-06 |
| 2  | preference | config_format | YAML | user_stated | 2025-03-10 |
| 3  | preference | language | 中文 | user_stated | 2026-05-01 |
| 4  | fact | industry | 医疗 | learned | 2026-04-20 |
| 5  | model_perf | glm-4 | coding:0.85 | observed | 2026-05-05 |

写入时机:
  - Memory Worker 每次提取时同时产出 (与情景记忆合并为一次 LLM 调用)
  - 用户主动告知
  - Agent 推断
  - 模型表现追踪

检索:
  查询时按 type + key 过滤，按 ts 降序排列取最新
  新旧共存，不删除旧的
```

#### 项目知识

```
从 SUNA.md / .suna/AGENTS.md 读取 (见 01-architecture.md)
自动提取为语义记忆中的 type=project 条目
```

### Layer 4: 程序记忆

见 [05-capability.md](05-capability.md)，能力本身就是"怎么做"的记忆。

#### 触发方式 (MVP)

```
路径 A: 规则判断 (零 LLM 成本)
  failure_records 表中同一个 pattern 出现 ≥3 次
  → agent 自省时看到 → AskUser "要不要我学习处理这个？"
  例: 3 条 pattern="npm install 失败" → 建议学习 skill

路径 B: 用户主动触发 (零 LLM 成本)
  用户说 "以后都这样做" / "你能不能记住这个" / "记住"
  → 直接触发学习流程

路径 C: LLM 判断 (远期，Phase 3)
  daemon 空闲时回顾情景记忆，检测重复模式
  MVP 不做
```

## 实体关联 (Entity Linking)

```
每个记忆片段自动提取实体:

  "用户在用 Vue3 + Vite 做前端项目"
  → 实体: Vue3, Vite, 前端

  "部署到 staging 时 npm build 失败了，因为缺少 ENV_VAR"
  → 实体: staging, npm, ENV_VAR

实体存储在独立索引:
  SQLite 表: entities
  | name | memory_ids JSON | embedding BLOB |
  |------|----------------|----------------|
  | Vue3 | [12, 45, 78]   | <vec>          |
  | Vite | [12, 56]       | <vec>          |

检索时: 实体匹配给相关记忆加权
  查询 "前端用什么框架" → 命中实体 Vue3 → 提升相关记忆的排名
```

## 多信号检索 (Multi-Signal Retrieval)

### 检索模式

```
有 embedding provider:
  三路检索: 语义 + FTS5 + 实体 → 精度最高

无 embedding provider (大多数用户的默认状态):
  两路检索: FTS5 + 实体 → 覆盖 80-90% 场景
  辅以按需查询改写弥补 FTS5 的语义弱点
```

### 按需查询改写

FTS5 不擅长语义匹配（"工作环境不好" 命中不了 "开发环境太吵"）。查询改写在 FTS5 命中不足时按需触发，不是每轮都调 LLM。

```
检索策略 (按需升级):

Level 0: 直接用用户原文做 FTS5 (零额外 LLM)
  "部署失败怎么处理" → FTS5 MATCH '部署 失败 处理'
  命中 ≥3 条 → 直接用，不调 LLM
  大部分场景 (~70%) 这个就够了

Level 1: FTS5 命中不足 → 查询改写 (一次 fast 调用)
  触发条件: FTS5 命中 < 3 条
  用户说 "工作环境太吵" → FTS5 命中 0 条
  → 调 active_model 改写: "工作环境 噪音 吵闹 办公"
  → 用改写后的关键词重新 FTS5

Level 2: 改写后仍命中不足 → 不注入记忆
  宁可不放，不塞噪音

LLM 调用频率: 仅 ~30% 的交互触发 Level 1
  每天 200 轮 → ~60 次改写 → ¥0.06/天 → ¥1.8/月 (可忽略)
```

### 检索筛选流程

记忆越来越多，筛选决定了注入 prompt 的质量。

```
用户输入 + 当前对话上下文
    │
    ▼
Step 1: 检索 (按上述 Level 0/1/2)
  FTS5 + 实体 (+ 可选语义) → 合并去重 → ~80 条候选

Step 2: 时间衰减排序
  score = 基础分 × 时间衰减因子
    7 天内:  × 1.0
    30 天内: × 0.8
    90 天内: × 0.5
    更早:   × 0.3
  优先返回最近的记忆，但不丢弃远的

Step 3: Token 预算控制
  System Prompt 分配给记忆的 token 预算: ~4K tokens
  从排序后的候选中，从高到低取:
    每条记忆 ~100-200 tokens
    预算内能放 ~20-30 条

  放不下的不注入，但 prompt 里提示:
    "以上是相关度最高的记忆。如需更多信息，可使用 /memory search"

  如果候选得分都很低:
    → 不注入任何记忆 → 宁可不放，不塞噪音
```

### 向量检索实现 (可选，需要 embedding provider)

```
不引入向量数据库。单用户 agent 的记忆量级是几千到几万条，
暴力余弦相似度计算足够快。

存储:
  每条情景记忆有一个 embedding BLOB 字段
  SQLite 表: episodic_memories
  | id | content | entities JSON | embedding BLOB | ts | source | type |

  embedding 维度由 provider 自动决定（见下文），用户无需配置
  每条 ~4-8KB (取决于维度)，1 万条约 40-80MB，完全可接受

检索流程:
  1. 调 embedding API 得到 query 向量 (~50ms)
  2. 从 SQLite 读所有 embedding BLOB
  3. Go 内存中暴力余弦相似度 (几万条 <10ms)
  4. 取 top-k

  总延迟: ~60ms，零新依赖

如果以后记忆膨胀到十万级以上:
  → 考虑 sqlite-vec 扩展 (纯 Go 编译进二进制)
  → 但这是远期的事
```

### Embedding 在线检索

#### 自动发现

用户配置 provider 时，Suna 自动检测 embedding 端点：

```
用户配置新 provider:
  → 检测 /v1/chat/completions → "模型连接成功"
  → 检测 /v1/embeddings     → "已启用语义记忆检索"
  → 或: "该服务不支持 embedding，记忆检索使用全文搜索模式"

支持 embedding 的常见 provider:
  智谱 (open.bigmodel.cn)       → embedding-3
  OpenAI (api.openai.com)        → text-embedding-3-small
  通义千问 (dashscope.aliyuncs.com) → text-embedding-v3
  其他 OpenAI 兼容服务            → 尝试 /v1/embeddings，失败则跳过
```

#### 检索模式

```
有 embedding provider:
  三路检索: 语义 + FTS5 + 实体 → 精度最高

无 embedding provider:
  两路检索: FTS5 + 实体 → 覆盖 80% 场景
  /memory status 提示: "检索模式: 全文搜索 (未检测到 embedding 服务)"
```

#### 用户反馈

```
/memory status:
┌──────────────────────────────────────────┐
│ 记忆状态                                  │
│                                          │
│ 检索模式: 语义检索 (智谱 embedding-3)      │
│ 活跃记忆: 1,247 条                        │
│ 实体索引: 389 个                          │
│ 向量维度: 2048                            │
│ 存储大小: 12.3 MB                         │
└──────────────────────────────────────────┘

或:
┌──────────────────────────────────────────┐
│ 记忆状态                                  │
│                                          │
│ 检索模式: 全文搜索                         │
│ 活跃记忆: 1,247 条                        │
│ 实体索引: 389 个                          │
│                                          │
│ 提示: 配置支持 embedding 的 provider       │
│ 可启用语义检索，提升记忆检索精度            │
└──────────────────────────────────────────┘
```

#### 向量存储

```
embedding 维度由 provider 返回决定，用户无需配置

存储: SQLite BLOB 字段 (episodic_memories.embedding)
  每条 ~4-8KB (取决于维度)
  1 万条约 40-80MB
  暴力余弦相似度 <10ms / 万条

config.toml 不需要 [embedding] 段
embedding provider = 从已配置的 provider 自动发现
```

#### 降级策略

```
运行时 embedding API 不可用 (网络故障/额度用尽):
  → 自动降级到 FTS5 + 实体检索
  → 日志记录降级事件
  → API 恢复后自动恢复语义检索
  → 降级期间新增的记忆不生成向量
  → 下次 embedding 可用时补算

不影响记忆写入，只影响检索精度
```

## 时间推理 (Temporal Reasoning)

这是现有记忆系统最弱的地方，但也是最有价值的。

```
记忆库中有:
  - "用户住在北京" (2025-05)
  - "用户搬到上海" (2026-03)
  - "用户说老邻居很吵" (2025-12)

查询: "用户说邻居吵是什么时候的事？还住在北京吗？"

时间推理:
  "邻居吵" 发生在 2025-12
  当时住在北京 (2025-05 的记录)
  2026-03 搬到了上海
  → 回答: "当时住在北京，现在已经搬到了上海"

这不是简单的"取最新"，而是理解时间线的因果关系。
```

### 实现

```
检索时额外返回时间上下文:

1. 命中的记忆条目及其 ts
2. 同一 key 下更早/更晚的记忆 (时间线)
3. 关联实体的时间线

LLM 推理阶段:
  将时间线信息一并注入 prompt:
  "相关记忆 (按时间排列):
   2025-05: 用户住在北京
   2025-12: 用户说老邻居很吵
   2026-03: 用户搬到上海"

  LLM 自然能理解时间关系，不需要特殊算法
```

## 记忆的注入策略

不是全部塞进 system prompt，也不是分成多个独立区块。只有两部分：固定指令 + 相关记忆。

```
System Prompt:

  ## 固定指令
  身份 + 工作方式 + 工具原则 + 环境信息

  ## 项目配置 (如有 SUNA.md)
  {{ project_config }}

  ## 相关记忆 (统一区块，所有动态内容都在这里)
  {{ 用户偏好 (语义记忆摘要) }}
  {{ 多信号检索 top-k (情景记忆) }}
  {{ 压缩后的对话摘要 (如有，也以记忆片段形式呈现) }}

  ## 当前能力
  {{ capabilities_list }}
```

关键：对话历史不作为独立区块。压缩后的对话摘要归入"相关记忆"，格式与其他记忆片段一致。LLM 不需要知道哪些是检索到的记忆、哪些是压缩后的对话 — 对它来说都是"相关记忆"。

这样做的优势：
- 提示词结构最简，只有固定 + 动态两部分
- Cache 命中率最高（固定部分不变）
- LLM 不需要区分不同来源的信息

## 上下文压缩

### 何时压缩

```
每次模型返回 usage 信息后:
  if estimated_tokens(messages) > model.ContextWindow() * 80%:
      compress(messages)
```

### 压缩优先级

```
优先压缩 (占空间大，信息密度低):
  1. 工具输出 (Exec 返回的大段文本 → 截断或摘要)
  2. 早期对话 (超过 10 轮的部分 → 摘要)

不压缩:
  - 最近 10 轮对话
  - System Prompt
  - 当前正在处理的文件内容
```

### 压缩方式

#### 工具输出截断 (第一道防线)

```
ReadFile:   超过 2000 行 → 保留前 100 行 + "...(truncated, N lines total)"
Exec:       stdout/stderr 各超过 500 行 → 保留前 200 行
ReadHTTP:   body 超过 50KB → 保留前 20KB

截断时追加: "内容已截断。如需完整内容，使用 offset/limit 参数分页读取。"
```

#### 历史消息摘要 (第二道防线)

```
1. 将对话分为两部分:
   - 保留区: 最近 10 轮，完整保留
   - 压缩区: 更早的对话

2. 调用 active_model 压缩压缩区:
   prompt = `将以下对话历史压缩为简洁摘要。
   保留: 用户意图、已完成操作、关键决策、当前进展
   忽略: 具体代码细节、工具返回细节、中间调试过程`

3. 替换:
    压缩区的 N 条消息 → 从 working memory 移除
    压缩摘要注入 System Prompt 的"相关记忆"区块
    格式: "对话摘要: {{ summary }}"

关键: 压缩后的原始消息已归档到情景记忆
  → 不会丢失，可通过 /memory search 精准回溯
  → 摘要只是 working memory 的瘦身，不新建独立区块
```

### 缓存友好的压缩

```
正确做法:
  System Prompt (固定指令，不变) + 相关记忆 (统一区块，含偏好+检索+摘要)

  相关记忆区块内部:
    1. 用户偏好 (很少变)
    2. 检索到的情景记忆 (每轮变化)
    3. 压缩后的对话摘要 (偶尔追加)
  
  所有动态内容都在一个区块内，LLM 不区分来源。
  System Prompt 部分保持不变，cache 命中率高。
```

## 记忆提取的完整流程

Daemon 中的异步处理管道。Agent Loop 不等待提取完成，直接进入下一轮。

```
Agent Loop 完成一轮交互
    │
    ▼
  ┌──────────────────────────┐
  │ 0. 显著性判断             │  零 LLM 成本，规则判断
  │    高/中 → 入队           │  低 → 跳过
  │    发送到 memory channel  │
  └──────────┬───────────────┘
             │
             ▼  (异步，不阻塞 Agent Loop)
  ┌──────────────────────────┐
  │ 1. 合并提取              │  Memory Worker (独立 goroutine)
  │    情景记忆 + 语义记忆    │  单次 LLM 调用 (fast)
  │    + 实体关联             │  同时输出 episodes + facts
  │    (仅添加式)             │  ~50ms, ~$0.0001
  │    标记 session_messages  │
  │    为 memory_extracted=1  │
  └──────────┬───────────────┘
             │
             ▼
  ┌──────────────────────────┐
  │ 2. 实体 + Embedding       │  实体写入 entities 表
  │    (可选)                  │  embedding 有 provider 时才生成
  └──────────┬───────────────┘
             │
             ▼
  ┌──────────────────────────┐
  │ 3. 失败记忆记录           │  工具失败时自动记录
  │    (独立子表)              │
  └──────────────────────────┘

热路径/冷路径:
  热路径 (daemon 运行中):
    Agent Loop → memory channel (内存) → Worker 消费 → 标记 memory_extracted
  冷路径 (daemon 启动恢复):
    扫描 session_messages WHERE memory_extracted=0 → 补处理
    用于 daemon 崩溃后恢复未处理的交互
```

### 会话切换时的记忆传递

用户 `/new` 切换会话时，旧会话的记忆可能还没提取完。不做 flush 等待（LLM 响应慢），直接零延迟兜底。

```
用户 /new 切换会话
  │
  ▼
Daemon 处理:
  1. 从旧 session 的 session_messages 取最近 5 轮 memory_extracted=0 的原文
  2. 注入新 session 的 System Prompt "相关记忆" 部分 (与正常检索格式一致):
     "## 相关记忆
      - 用户偏好: 配置格式用 TOML (来源: 上一个会话)
      - Agent 确认: 配置格式改为 TOML (来源: 上一个会话)"
  3. 同时把旧 session 积攒的轮次推给 Memory Worker (不等完成)
  4. 立即创建新 session，零延迟

格式一致性:
  LLM 看到的 "相关记忆" 段落格式永远一致，不区分来源:
    - 临时注入的原文 → 格式化为记忆片段
    - 正常检索的 episodic_memories → 同样格式
  Suna 内部处理替换，LLM 无感知

临时上下文的自动清除:
  daemon 维护 pendingContext map: map[sessionID]bool
  注入时: pendingContext["session_abc"] = true
  Worker 完成提取后: 删除 pendingContext["session_abc"]
  后续构建 System Prompt 时:
    - map 中存在的 session → 继续注入临时记忆
    - map 中已删除 → 正常从 episodic_memories 检索
  对 LLM 来说，"相关记忆" 段落始终存在，只是内容从临时变为正式
```

### LLM 调用成本预算

```
重度用户 (每天 200 轮，LLM 调用 ~¥0.001/次):

1. Main LLM (生成回复):     200 次 × ¥0.05 = ¥10/天 = ¥300/月
2. Memory 合并提取:          40 次 × ¥0.001 = ¥0.04/天 = ¥1.2/月
3. 查询改写 (按需, ~30%):    60 次 × ¥0.001 = ¥0.06/天 = ¥1.8/月
4. 上下文压缩 (偶尔):         5 次 × ¥0.001 = ¥0.005/天 = ¥0.15/月

总计: ~¥303/月
其中记忆相关的 LLM 调用: ~¥3/月 (< 1%)
```

## SQLite 存储设计

```
数据库: ~/.suna/memory.db

表:
  episodic_memories   — 情景记忆 (核心表)
  entities            — 实体索引
  semantic_facts      — 语义记忆 (结构化知识)
  failure_records     — 失败记忆
  sessions            — 会话元数据
  session_messages    — 会话消息 (含提取状态)
  usage_log           — 用量记录
  audit_log           — 审计日志
  trust_rules         — 渐进信任 (见 04-guard.md)
  triggers            — 感知源 (见 07-trigger.md)

核心表结构:

episodic_memories:
  | id | content TEXT | type TEXT | source TEXT |
  | entities JSON | embedding BLOB | ts DATETIME |
  | session_id TEXT | metadata JSON |

entities:
  | name TEXT | memory_ids JSON | embedding BLOB | updated_at DATETIME |

semantic_facts:
  | id | type TEXT | key TEXT | value TEXT |
  | source TEXT | ts DATETIME |

failure_records:
  | id | pattern TEXT | operation TEXT | reason TEXT |
  | context TEXT | created_at DATETIME |

session_messages:
  | session_id | turn | role | content | tool_call | tool_result |
  | significance TEXT | memory_extracted BOOL DEFAULT 0 | created_at |

  memory_extracted: 0=待提取, 1=已提取
  significance: high/medium/low/null (null=未判断)

索引:
  session_messages: (memory_extracted), (session_id, turn)
  episodic_memories: (ts), (type), (source)
  entities: (name) UNIQUE
  semantic_facts: (type, key, ts)
  failure_records: (pattern), (created_at)
```

## 会话持久化与恢复

### 持久化

```
每轮对话完成后自动保存到 SQLite:

sessions:
  | id | created_at | updated_at | summary | status |

session_messages:
  | session_id | turn | role | content | tool_call | tool_result |
  | significance | memory_extracted | created_at |

写入时机: 每次模型返回完整响应后 (不是每个 streaming chunk)
WAL 模式保证写入性能
```

### 恢复流程

```
TUI 连接 daemon 时:
  1. Daemon 查询 sessions WHERE status='active' ORDER BY updated_at DESC
  2. 如果有未完成会话:
     TUI: "上次你在做「重构认证模块」，已完成 60%。要继续吗？"
     [继续] [开始新会话]
  3. 继续 → 从 session_messages 加载历史 + 从情景记忆检索相关上下文
  4. 新会话 → 标记旧会话为 paused

加载策略:
  - 最近 10 轮: 完整加载到工作记忆
  - 更早的: 只加载摘要
  - 额外: 多信号检索和当前任务相关的情景记忆
```

### 会话过期

```
- active 会话超过 7 天未更新 → 自动标记为 completed
- completed 会话保留 30 天后删除 (只删消息，关键信息已在情景记忆中)
```

### 异常中断处理

```
TUI 崩溃/关闭:
  - Daemon 继续运行
  - 提取队列中的任务继续处理
  - Agent loop 如果正在执行，继续完成
  - 结果存入 session_messages，下次 TUI 连接时可查看

Daemon 崩溃:
  - SQLite WAL 模式保证数据一致性
  - 重启后 Memory Worker 扫描 session_messages WHERE memory_extracted=0 → 补处理
  - 检查: 有 status=active 但 updated_at > 1分钟前的会话
  - 有 → 下次 TUI 连接时提示用户恢复

Sub agent 正在执行时 Daemon 重启:
  - Sub agent 随 Daemon 进程销毁
  - 恢复时 main agent 看到历史 → 自行决定是否重新 Spawn
```

## 记忆的 TUI 命令

```
/memory search <query> — 多信号检索情景记忆
```

注：/memory facts, /memory failures, /memory clear, /memory timeline 等命令已精简。
- 用户可通过自然语言让 agent 查询记忆 (如"你还记得我之前用什么配置格式吗？")
- 记忆管理（清除等）通过 agent 对话完成，不暴露为命令
- 减少命令数量，降低学习成本

## /compact 命令反馈

```
┌──────────────────────────────────────────┐
│ 上下文压缩完成                             │
│                                          │
│ 压缩前: 98,400 tokens (77% 窗口)          │
│ 压缩后: 41,200 tokens (32% 窗口)          │
│                                          │
│ 保留: 最近 10 轮完整对话                    │
│ 摘要: 45 轮历史 → 1 段摘要 (~800 tokens)   │
│ 截断: 3 个工具输出                         │
│                                          │
│ 原始消息仍可通过 /memory search 回溯        │
└──────────────────────────────────────────┘
```

## 与原 4 层记忆的关系

```
原 Layer 1: 工作记忆       → 新 Layer 1: 工作记忆 (基本不变)
原 Layer 2: 用户认知       → 新 Layer 3: 语义记忆 (仅添加式 + 实体关联)
原 Layer 3: 能力记忆       → 新 Layer 4: 程序记忆 (概念重新定位)
原 Layer 4: 失败记忆       → 独立表 failure_records (不走向量检索，查询更高效)
新增:      情景记忆        → 新 Layer 2 (核心创新: 仅添加式 + 多信号检索)
```
