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
原则 6: 上下文必须短且缓存友好。固定提示词在前，动态记忆靠后。
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

`conversation_state` 只用于 UI 展示和恢复上一轮上下文。

### 保存内容

```
resume_summary       上一轮会话的简短恢复摘要
last_messages        最近一轮完整消息，通常是 user + assistant
memory_processed_at  最近一次队列处理时间
updated_at           更新时间
```

`last_messages` 不是长期历史，只是恢复快照。默认保留最近 2 条消息；必要时可保留最近 4-6 条，但不能无限增长。

### 恢复行为

```
恢复上一轮:
  注入 user_memory + conversation_state.resume_summary + last_messages

新建会话:
  只注入 user_memory
  不注入上一轮 last_messages
```

如果 memory_queue 尚未被 daemon 处理，恢复上一轮时 `last_messages` 是兜底上下文，保证 Suna 至少知道用户刚才说过什么。

## memory_queue

`memory_queue` 是临时队列。主链路只负责写入，daemon 负责批量消费。

### 写入时机

每轮交互完成后写入：

- 用户消息。
- 助手最终回复。
- 必要的失败摘要或用户纠错事件。

不写入 streaming chunk。

### 处理时机

daemon 满足任一条件时处理：

```
队列积攒 >= 5 轮
距离上次处理 >= 60 秒
存在高显著性事件
daemon 空闲
```

高显著性事件包括：

- 用户说“记住”“以后都这样”“不要再这样”。
- 用户纠正 Suna。
- Suna 重复犯错。
- 工具失败且 agent 需要改变策略。
- 用户明确表达长期偏好或边界。

处理完成后，已处理队列可以删除，不需要长期保存。

## Daemon 记忆整理

记忆提取使用 LLM，但不在主请求链路中执行。

主链路：

```
用户消息
  -> 写 memory_queue
  -> 读取 user_memory
  -> 读取 conversation_state
  -> 构建短上下文
  -> 调用主 LLM
  -> 更新 conversation_state
```

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

### 两层召回

```
1. Core memory
   每轮固定注入 3-5 条高优先级记忆。

2. Matched memory
   根据当前用户消息做关键词/tag/kind 匹配，最多补充 0-2 条。
```

最终注入不超过 5 条。

### 排序规则

```
is_core desc
priority desc
last_used_at desc
updated_at desc
id asc
```

排序必须稳定，避免每轮 memory brief 无意义变化。

## 上下文注入

记忆必须短、稳定、靠后，避免破坏 prompt cache。

推荐结构：

```
Stable system prompt
Stable memory policy
Dynamic active memory brief
Dynamic conversation resume state
Current user message
```

不要把动态记忆插入固定 system prompt 前部。

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

主链路不要调用 LLM 重新总结记忆，直接拼接已保存的 `content`。

```
<active_memory>
- 用户偏好直接、简洁、先给结论的回复。
- 用户讨厌过度设计，偏好简单可靠的方案。
- 用户希望 Suna 不依赖 embedding。
- 用户希望记忆短小、活跃、可刷新。
</active_memory>
```

格式固定，条数固定上限，排序稳定。

## SQLite 设计

数据库仍可使用 `~/.suna/memory.db`，但表结构收敛为最小集合。

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
memory_processed_at DATETIME
updated_at DATETIME NOT NULL
```

`last_messages` 为 JSON，最多保存最近 2-6 条消息。

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

已处理队列可以立即删除。如果需要 debug，可保留短期日志，但必须有 TTL。

## 会话持久化与恢复

### 每轮结束

```
1. 保存 user/assistant 到 memory_queue。
2. 更新 conversation_state.last_messages。
3. 更新 conversation_state.resume_summary。
4. 返回用户，不等待记忆提取。
```

### TUI 启动

```
1. 读取 conversation_state。
2. 如果存在 last_messages，展示“继续上一轮 / 新建会话”。
3. 继续上一轮: 加载 last_messages + resume_summary。
4. 新建会话: 清空 last_messages，但保留 user_memory。
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
