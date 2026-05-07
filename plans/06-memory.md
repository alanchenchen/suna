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
│ 存储: 进程内                                                   │
│ 生命周期: 当前任务                                              │
│ 内容: 对话历史 + 任务状态 + 工具调用结果                        │
│ 访问: 自动注入每轮对话                                         │
│ 归档: 每轮交互后自动提取记忆片段 (见下方提取管道)              │
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
存储结构 (进程内):
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

```
每次交互后，自动从对话中提取记忆片段:

输入: 最后一轮的用户消息 + agent 回复 + 工具调用结果
输出: 一组记忆片段 (facts)

提取方式: 单次 LLM 调用 (fast 模型)
  prompt = "从以下交互中提取所有值得记住的事实，包括:
    - 用户明确说的偏好/约束
    - 用户隐含的需求模式
    - Agent 完成的操作和结果
    - 决策及其原因
    - 错误和教训"

示例:
  用户: "我不用 YAML，配置都用 TOML"
  Agent: "好的，以后配置都用 TOML"
  提取:
    - { type: "preference", key: "config_format", value: "TOML",
        source: "user_stated", ts: "2026-05-06T10:00" }
    - { type: "action", key: "acknowledged", value: "config_format=TOML",
        source: "agent_confirmed", ts: "2026-05-06T10:00" }

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
  - 从情景记忆中定期提炼 (每 5 次交互后)
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

```
查询: "用户之前怎么处理部署失败的"

三个信号并行:
  1. 语义相似度: embedding(query) vs embedding(memories)
     → 找到 "部署相关的记忆"

  2. 关键词匹配: "部署" "失败" 在记忆文本中的出现
     → 精确匹配关键词

  3. 实体匹配: 查询中的实体 vs 实体索引
     → 命中 "staging" "npm" 相关的记忆

融合: 三个信号的分数加权合并 → 排名最高的 top-k 返回

Token 效率: 只返回 top-k (~7K tokens)，不塞全部上下文
参考: Mem0 在 7K tokens/query 下达到 91.6% 准确率
```

### 向量检索实现

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

不是全部塞进 system prompt，而是按相关性精准注入：

```
System Prompt 的记忆部分:

  ## 用户偏好 (语义记忆，启动时加载摘要)
  {{ semantic_facts_summary }}

  ## 相关记忆 (情景记忆，每轮多信号检索 top-k)
  {{ retrieve_memories(current_context) }}

  ## 可用能力 (程序记忆，启动时加载)
  {{ capabilities_list }}

不注入的:
  - 工作记忆 → 自动在 Messages 中
  - 全部情景记忆 → 太大，按需检索
```

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

2. 调用 fast 模型压缩压缩区:
   prompt = `将以下对话历史压缩为简洁摘要。
   保留: 用户意图、已完成操作、关键决策、当前进展
   忽略: 具体代码细节、工具返回细节、中间调试过程`

3. 替换:
   压缩区的 N 条消息 → 替换为 1 条 system 消息:
   "之前的对话摘要: {{ summary }}"

关键: 压缩前的原始消息已归档到情景记忆
  → 不会丢失，可通过 /memory search 精准回溯
```

### 缓存友好的压缩

```
正确做法:
  System Prompt (不变) + 摘要消息 (压缩后插入) + 保留区 + 最新消息

摘要消息位置:
  作为特殊的 system 消息，放在 system prompt 之后、保留区之前
  → system prompt 部分保持不变，cache 命中率高
```

## 记忆提取的完整流程

```
每次交互后的记忆处理管道:

  交互结束
    │
    ▼
  ┌──────────────────────────┐
  │ 1. 情景记忆提取            │  单次 LLM 调用 (fast)
  │    提取事实片段             │  ~50ms, ~$0.0001
  │    (仅添加式)              │
  └──────────┬───────────────┘
             │
             ▼
  ┌──────────────────────────┐
  │ 2. 实体关联               │  NLP 提取实体名
  │    提取实体名              │  + embedding API 调用
  │    生成 embedding          │
  │    更新实体索引            │
  └──────────┬───────────────┘
             │
             ▼
  ┌──────────────────────────┐
  │ 3. 语义记忆提炼 (每5轮)    │  从情景记忆提炼
  │    提取结构化知识           │  到语义记忆
  │    (仅添加式)              │
  └──────────┬───────────────┘
             │
             ▼
  ┌──────────────────────────┐
  │ 4. 失败记忆记录            │  工具失败时自动记录
  │    (独立子表)              │
  └──────────────────────────┘
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
  session_messages    — 会话消息
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

索引:
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
  | session_id | turn | role | content | tool_call | tool_result | created_at |

写入时机: 每次模型返回完整响应后 (不是每个 streaming chunk)
WAL 模式保证写入性能
```

### 恢复流程

```
Suna 启动时:
  1. 查询 sessions WHERE status='active' ORDER BY updated_at DESC
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
Suna 进程被 kill / 崩溃:
  - SQLite WAL 模式保证数据一致性
  - 重启后检查: 有 status=active 但 updated_at > 1分钟前的会话
  - 有 → 提示用户恢复

Sub agent 正在执行时中断:
  - Sub agent 随主进程销毁
  - 恢复时 main agent 看到历史 → 自行决定是否重新 Spawn
```

## 记忆的 TUI 命令

```
/memory facts          — 查看语义记忆
/memory failures       — 查看失败记录
/memory search <query> — 多信号检索情景记忆
/memory clear failures — 清除失败记录
/memory clear facts    — 清除语义记忆 (重新学习)
/memory timeline <key> — 查看某条记忆的时间线
```

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
