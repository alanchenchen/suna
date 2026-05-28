# 06 — 轻量主动记忆

Suna 的记忆目标不是保存所有历史，也不是构建长期知识库，而是让 Suna 越用越懂用户。

记忆只服务三个结果：

- 更理解用户的沟通方式、习惯、偏好和性格。
- 避免下次重复犯同样的错误。
- 恢复上一轮会话时知道刚才发生了什么。

Suna 只保留少量有效记忆。记忆会被刷新、合并、替换和删除。过期、不活跃、低价值的内容不应该长期存在。

## 设计原则

```
原则 1: 不使用 embedding。记忆系统不能依赖不稳定服务。
原则 2: 不永久保存完整会话。只保留最近恢复状态和待提取队列。
原则 3: 不做 append-only。记忆是 active state，会被 compact 和刷新。
原则 4: 不做项目/工作区记忆。Suna 只有一个当前会话，要么新建，要么恢复上一条。
原则 5: 主链路只读记忆。记忆提取和整理由 daemon 异步批量处理。
原则 6: 上下文必须短且缓存友好。固定提示词和历史前缀在前，动态记忆靠近当前用户消息。
```

## 记忆模型

```
┌─────────────────────────────────────────────────────────────┐
│ user_memory                                                  │
│ 长期 active memory。只记录用户画像、偏好、习惯、纠错和约束。     │
│ 数量很小，默认最多 30 条。                                    │
├─────────────────────────────────────────────────────────────┤
│ conversation_state                                           │
│ 最近会话恢复状态。只保留上一轮可恢复内容，不保存完整历史。        │
├─────────────────────────────────────────────────────────────┤
│ memory_queue                                                 │
│ 临时提取队列。主链路写入，daemon 批量处理后删除。                │
└─────────────────────────────────────────────────────────────┘
```

## user_memory

`user_memory` 是 Suna 的核心长期记忆。它不是知识库，而是用户理解层。

### 应该记住

- 用户沟通偏好：喜欢简洁、直接、详细、先给结论等。
- 用户做事习惯：偏好简单方案、重视稳定性、讨厌过度设计等。
- 用户长期约束：不要 embedding、不要复杂上下文、不要冗长解释等。
- 用户纠错记录：Suna 上次哪里做错了，下次应该避免什么。
- 用户性格/风格：效率优先、谨慎、产品直觉强、工程可控性优先等。
- 用户明确要求长期记住的信息。

### 不应该记住

- 所有会话内容。
- 临时任务细节。
- 大段历史摘要。
- 一次性讨论。
- 工具调用记录。
- 低置信推测。
- 已经过期或不再活跃的状态。

### 记忆类型

```
preference   用户偏好
habit        用户习惯
constraint   长期约束
correction   用户纠错和反复错误规避
personality  用户性格/沟通风格
fact         少量长期事实
```

### 数量限制

```
active user_memory: 30 条以内
core memory:        5 条以内
每轮注入:            5 条以内
单条长度:            120-160 字符以内
memory brief:        400 tokens 以内
```

`core memory` 是几乎每轮都应该注入的高优先级记忆，例如用户明确表达的长期偏好或反复纠正。

## conversation_state

Suna 的产品形态只有一个当前会话：用户要么新建，要么恢复上一条。因此不需要多会话管理，也不需要长期保存完整 session history。

`conversation_state` 只用于 UI 展示和恢复上一轮会话上下文。

### 保存内容

```
resume_summary       上一轮会话的简短恢复摘要，仅作辅助展示
last_messages        上一轮 working memory 中仍保留的可见 user/assistant transcript
tool_summary         上一轮会话的工具操作摘要，仅用于 UI 展示
memory_processed_at  最近一次队列处理时间
updated_at           更新时间
```

`last_messages` 不是长期历史，只是上一次会话的恢复快照。它只保存 working memory 中仍保留的可见 user/assistant 文本消息，并会剥离 assistant tool_calls/raw 结构。若当前会话发生过上下文压缩，压缩成的 system summary 不会写入 `last_messages`，因此它不是压缩前完整 transcript。新建会话时清空，下一次会话会覆盖它。

`tool_summary` 只保存轻量摘要，例如“exec [success]: go test ./... 通过”。恢复时通过 TUI-only `restore_summary` role 展示给用户，不放回 LLM working memory。

### 恢复行为

```
恢复上一轮:
  加载 user_memory
  把 conversation_state.last_messages 放回 TUI 展示和 agent working memory
  把 conversation_state.tool_summary 展示给 TUI，但不放回 agent working memory
  resume_summary 仅作辅助展示，不作为唯一上下文来源

新建会话:
  只注入 user_memory
  清空上一轮 last_messages/tool_summary
```

如果 memory_queue 尚未被 daemon 处理，恢复上一轮时 `last_messages` 是兜底上下文。它能恢复最近可见对话，但不保证包含已被 compact 到 system summary 的旧上下文，也不恢复 raw tool call/result。

## memory_queue

`memory_queue` 是临时队列。主链路只负责写入，daemon 负责批量消费。

## Working Memory Compact

Working memory 是当前 agent 会话内发送给模型的短期上下文。它包含用户消息、assistant 消息、tool call/result、运行时 system summary 等，但不等同于完整 LLM 请求。完整请求还包括 system prompt、active memory 注入、tool schemas 和 max output reserve。

Compact 的目标是避免当前会话上下文超过模型窗口，不是保存长期历史，也不是更新 `user_memory`。

### 自动 compact

自动 compact 在每次 LLM 请求发出前执行 preflight 检测：

```
estimated_request_tokens =
  system prompt
  + active memory / internal context 注入
  + working memory messages
  + tool schemas
  + max output reserve

触发条件:
  estimated_request_tokens > context_window * 0.8
```

触发后只压缩 working memory：

```
1. 计算不可压缩开销：system prompt、tool schemas、max output reserve、非 working 注入消息。
2. 用剩余预算选择 recent working messages suffix。
   - 最多保留最近 10 条原始消息。
   - 预算不足时保留更少。
   - 至少保留最新 1 条，避免当前请求被压掉。
3. 将 suffix 之前的 prefix 一次性压成一条 system summary。
4. 重建完整请求并重新估算。
5. 如果仍超过安全阈值，返回明确错误，提示新建会话或减少当前输入。
```

自动 compact 最多只额外发起一次 summary 请求，不做多轮重试压缩，避免延迟、成本和摘要漂移。

### 手动 compact

手动 `/compact` 是用户显式整理当前上下文，策略固定且可预测：

```
working memory > 10 条消息:
  prefix -> summary
  recent -> 保留最近最多 10 条原始消息

working memory <= 10 条消息:
  no-op，不报错，并在 TUI 中说明暂无可压缩内容
```

手动 compact 不需要根据完整请求预算动态计算 recent 数量，因为它不是为了立即避免某次请求超限，而是用户主动瘦身上下文。

### compact 后的状态

Compact 成功后，agent 的 working memory 会立即变为：

```
system: Conversation summary: <压缩摘要>
recent working messages...
```

TUI 的聊天 transcript 不会被替换或删除。旧聊天仍保留在界面中供用户回看，但模型后续只基于 compact 后的 summary + recent messages 继续。

`conversation_state.last_messages` 只保存 compact 后 working memory 中仍保留的可见 user/assistant 纯文本消息。compact 生成的 system summary、tool call/result 和 raw 结构不会写入恢复快照。因此恢复会话不是完整历史回放，而是最近可见对话的轻量恢复。

### 写入时机

主链路按显著性写入：

- 用户消息：如果命中 medium/high significance，写入队列。
- 助手最终回复：如果命中 medium/high significance，写入队列。
- 必要的失败摘要或用户纠错事件：high significance。

不写入普通低价值对话，不写入 streaming chunk。

### 处理时机

daemon 满足任一条件时处理 pending queue：

```
pending event >= 5 条
存在 high significance event
worker timer 60 秒到期且满足以上任一条件
```

60 秒 timer 只是 wake-up 机制。少量 medium event 不会因为 timer 到期就单独触发 LLM，会继续等待合批，避免每轮对话都额外产生一次记忆整理请求。

high significance 由零 LLM 规则判断，包括：

- 用户说“记住”“以后都这样”“不要再这样”。
- 用户纠正 Suna。
- Suna 重复犯错。
- 工具失败且 agent 需要改变策略。
- 用户明确表达长期偏好或边界。

当前关键词包括：

```
记住 / 帮我记住 / 以后都 / 以后都这样 / 以后不要再 / 以后别再
always / never / remember / from now on
keep in mind
```

medium significance 包括较弱但可能长期有效的偏好、习惯或边界表达：

```
我希望 / 我不希望 / 我更 / 我比较 / 我倾向
我的习惯 / 我的性格 / 下次 / 以后 / 别再 / 不要再
更喜欢 / 不喜欢 / i want / i don't want / i tend to / next time / avoid / prefer
```

low significance 不进入 memory_queue，例如简单问候、短确认、普通一次性查询。

处理完成后，已处理队列可以删除，不需要长期保存。

## Daemon 记忆整理

记忆提取使用 LLM，但不在主请求链路中执行。

主链路：

```
用户消息
  -> 写入 working memory
  -> 按显著性决定是否写 memory_queue
  -> 从 user_memory 规则召回 active memory brief
  -> 构建短上下文
  -> 调用主 LLM
  -> 更新 conversation_state
```

判断 significance 不依赖召回，也不调用 LLM。召回只影响主 LLM prompt。

daemon 链路：

```
读取未处理 memory_queue
读取当前 user_memory
调用 LLM 做 full compaction
得到新的 user_memory 列表
代码 diff 后更新数据库
删除已处理 queue
```

## Full Compaction

Suna 不做复杂局部 patch，也不做 append-only。

每次 daemon 处理队列时，把当前全部 active memory 和新事件一起交给 LLM：

```
old_user_memory + new_queue_events -> new_user_memory
```

LLM 输出新的 active memory 列表，最多 30 条。系统代码再做 diff：

- 内容相同或语义延续：保留原 id，刷新字段。
- 内容变化：更新原 id。
- 新的重要信息：新增。
- 未返回的旧记忆：删除或标记 inactive。

这样天然支持合并、替换、删除和纠错，不需要复杂的 update one 逻辑。

### Compaction 规则

LLM 必须遵守：

- 优先更新已有记忆，而不是新增。
- 删除临时、过期、低价值、不活跃的记忆。
- 新记忆必须对未来交互有帮助。
- 用户当前明确表达的偏好优先于旧记忆。
- 冲突记忆只保留当前有效版本。
- 不保存完整会话事实。
- 不保存隐私推测。
- 总数不得超过 30 条。

## 召回策略

主链路召回不使用 LLM，也不使用 embedding。

因为 active memory 数量很小，规则足够可靠。

召回发生在用户消息进入 working memory 后、主 LLM 请求前：

```
用户输入
  -> working memory.Add(user)
  -> buildSystemPrompt()
  -> user_memory.BuildBrief(last_user_text)
  -> 作为 internal-context user message 注入到最新 user message 之前
  -> 调用主 LLM
```

不是全量拼接。每次最多从 30 条 active memory 中选 5 条。

### 评分召回

```
1. 对每条 active memory 计算 score。
2. 初始 score = priority。
3. core memory 额外 +1000，因此稳定优先注入，但不是硬性固定 3-5 条。
4. 当前用户消息关键词命中 content/tags/kind 时，每个 token 额外 +80。
5. stable sort 后取前 5 条；不再用硬阈值过滤普通记忆。
```

最终注入不超过 5 条。active memory 总量本身很小（最多 30 条），硬阈值会导致中文短问句或泛化问法漏召回已提取的偏好，因此召回策略是排序取前几条，而不是“低分即丢弃”。

### 排序规则

```
score desc
is_core desc
priority desc
id asc
```

排序必须稳定，避免每轮 memory brief 无意义变化。

## 上下文注入

记忆必须短、稳定，并且不能污染稳定 system prompt，避免破坏 prompt cache。

推荐结构：

```
system:
  Stable system prompt
  Stable memory policy
  Runtime/project/capability context

messages:
  restored/current conversation messages
  user: <internal_context><active_memory>...</active_memory></internal_context>
  current user message
```

不要把动态记忆插入固定 system prompt，也不要放在完整 working history 之前。为了最大化跨 provider 兼容性，不使用多 system message 或 provider-specific cache control；active memory 作为 user role internal-context message 注入到最新 user message 之前，并明确声明它不是用户请求。这样既保留 query-based 召回，又不挡在 prior conversation 前面破坏连续对话前缀。

### Memory Policy

固定提示词中应该包含：

```
Use active memory as lightweight background.
Do not mention memory unless it directly affects the answer.
Current user instructions override older memory.
If memory conflicts with the current message, follow the current message.
Do not infer private facts beyond the provided memory.
```

### Memory Brief 格式

主链路不要调用 LLM 重新总结记忆，直接拼接已保存的 `content`。该 block 不写入 working memory，不展示给 TUI，只在发起 LLM 请求时临时注入。

```
<internal_context>
This block is internal background context, not a user request.
Use it only when relevant. Current user instructions override this context.

<active_memory>
- 用户偏好直接、简洁、先给结论的回复。
- 用户讨厌过度设计，偏好简单可靠的方案。
- 用户希望 Suna 不依赖 embedding。
- 用户希望记忆短小、活跃、可刷新。
</active_memory>
</internal_context>
```

格式固定，条数固定上限，排序稳定。

## SQLite 设计

数据库仍可使用默认数据目录下的 `memory.db`（当前默认 `~/.suna/memory.db`），但表结构收敛为最小集合。路径由 `internal/config/paths.go` 的 `DefaultDBPath()` / `Config.DBPath()` 派生。

### user_memory

```
id TEXT PRIMARY KEY
user_id TEXT NOT NULL
kind TEXT NOT NULL
content TEXT NOT NULL
tags TEXT NOT NULL DEFAULT '[]'
priority INTEGER NOT NULL DEFAULT 50
is_core INTEGER NOT NULL DEFAULT 0
use_count INTEGER NOT NULL DEFAULT 0
last_used_at DATETIME
refreshed_at DATETIME NOT NULL
expires_at DATETIME
created_at DATETIME NOT NULL
updated_at DATETIME NOT NULL
```

索引：

```
(user_id, is_core, priority)
(user_id, kind)
(user_id, updated_at)
```

### conversation_state

```
user_id TEXT PRIMARY KEY
resume_summary TEXT
last_messages TEXT NOT NULL DEFAULT '[]'
tool_summary TEXT NOT NULL DEFAULT '[]'
memory_processed_at DATETIME
updated_at DATETIME NOT NULL
```

`last_messages` 为 JSON，保存上一次 working memory 中仍保留的可见 user/assistant 纯文本 transcript。它不保存 tool call 原始参数、tool result 原始输出、system compression summary 或 streaming chunk。新建会话时清空，下一次会话会覆盖。

`tool_summary` 为 JSON，只保存工具操作摘要，用于 TUI 恢复展示，不注入 LLM 上下文。

### Tool Call 与恢复

当前会话内，tool call/result 会进入 working memory，并传给下一次 LLM 请求：

```
assistant message + tool_calls
tool result message
```

这保证同一会话里的工具调用链对 LLM 可见。

但 `conversation_state.last_messages` 不保存原始 tool call/result，恢复会话时默认不把 tool call/result 放回 LLM 上下文。

原因：原始工具参数和输出通常包含大量 stdout/stderr、临时路径、敏感信息或过期状态，直接恢复会污染上下文并降低 prompt cache 命中。

`tool_summary` 用于 UI 恢复展示，例如“运行 go test ./... 通过”。`tool_summary` 不进入长期 `user_memory`，也不以 raw output 形式注入 LLM。

### memory_queue

```
id TEXT PRIMARY KEY
user_id TEXT NOT NULL
role TEXT NOT NULL
content TEXT NOT NULL
significance TEXT
created_at DATETIME NOT NULL
processed_at DATETIME
```

索引：

```
(processed_at, created_at)
(user_id, processed_at)
```

当前实现成功处理后直接删除队列行；`processed_at` 主要作为 pending filter/兼容字段，不作为成功历史。失败时通过 `attempts`、`next_attempt_at`、`last_error` 做退避重试。

### 失败重试

memory compaction 失败时不能每 60 秒无限重试，否则会反复消耗 LLM 调用。

`memory_queue` 记录：

```
attempts
next_attempt_at
last_error
```

失败策略：

```
第 1 次失败: 5 分钟后重试
第 2 次失败: 15 分钟后重试
第 3 次失败: 丢弃该批 queue event，并写日志
```

timer 到期时只处理 `next_attempt_at <= now` 且 `attempts < 3` 的 pending event。这样 provider 返回非法 JSON、网络失败或模型输出异常时，不会形成 daemon LLM 请求循环。

compaction prompt 必须要求模型返回严格合法 JSON。字符串内部双引号必须转义，或改用中文引号。

## 最近会话恢复

### 每轮结束

```
1. 用户消息进入 working memory 后，立即同步更新 conversation_state.last_messages。
2. 按显著性把 user 写入 memory_queue。
3. assistant 最终回复完成后，再次同步更新 conversation_state.last_messages。
4. 按显著性把 assistant 写入 memory_queue。
5. 同步更新 conversation_state.tool_summary 为当前会话工具操作摘要。
6. 更新 conversation_state.resume_summary。
7. 返回用户，不等待记忆提取。
```

如果 LLM 失败、超时、取消或进程退出，`conversation_state.last_messages` 至少已经包含用户刚输入的消息，恢复会话不会丢失未完成输入。空 assistant 消息不会写入 `last_messages`。

### TUI 启动

```
1. 读取 conversation_state。
2. 如果存在 last_messages，展示“继续上一轮 / 新建会话”。
3. 继续上一轮: 加载 last_messages 到 TUI 和 agent working memory。
4. 继续上一轮: 通过 `restore_summary` role 展示 tool_summary 给 TUI，但不放入 agent working memory。
5. 新建会话: 清空 last_messages/tool_summary，但保留 user_memory。
```

### Daemon 崩溃恢复

```
1. 读取 memory_queue 中 processed_at IS NULL 的事件。
2. 批量执行 full compaction。
3. 更新 user_memory。
4. 删除已处理 queue。
```

## 与 Capability 的关系

记忆记录“用户是什么样的人、Suna 应该避免什么”。

Capability 记录“Suna 学会了怎么做某类事”。

重复失败或稳定操作模式不应该无限塞进 user_memory：

```
同类 correction/failure 出现多次
  -> user_memory 记录简短偏好或禁忌
  -> capability 系统判断是否需要学习新能力
```

例如：

```
user_memory:
- 用户不希望 Suna 在不确认环境的情况下直接执行破坏性命令。

capability:
- 执行数据库迁移前先检查当前环境、备份状态和 dry-run 结果。
```

## 不做的事情

MVP 明确不做：

- embedding。
- 向量检索。
- 实体索引。
- 长期情景记忆。
- 完整会话历史检索。
- append-only 事实库。
- 多会话管理。
- workspace/project 记忆层。
- `/memory search` 精准历史回溯。
- 20-30 条大记忆注入。

## 成功标准

记忆系统成功不是因为存得多，而是因为 Suna 行为更贴合用户。

验收标准：

- 用户明确纠正后，Suna 下次不再重复同样错误。
- 用户偏好简洁，Suna 后续回答明显更短更直接。
- 用户偏好简单方案，Suna 后续不会默认过度设计。
- 退出后恢复上一轮，Suna 能接上刚才的事情。
- 新建会话后，Suna 不带入上一轮临时上下文，但仍保留用户长期偏好。
- 数据库不会随使用时间持续膨胀。
- prompt cache 命中率不会因为记忆大幅下降。

最终目标：Suna 足够轻量，足够聪明，越用越懂用户。
