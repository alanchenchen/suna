# 06 — Suna 记忆系统

Suna 的记忆目标不是保存所有历史，也不是构建长期知识库，而是让用户在连续使用时保持任务连续性，并逐渐形成对用户协作方式的长期理解。

记忆只服务四个结果：

- 当前任务或当前对话不中断，自动 compact 后仍能继续执行或讨论。
- 较早完成的任务、讨论过的话题、用户要求和关键决策不会轻易消失。
- 退出 TUI 后恢复会话，用户能看到真实对话，模型能拿到精简但高价值的上下文。
- 跨会话保留少量长期用户画像：沟通偏好、工作习惯、长期约束、纠错和少量用户事实。

## 设计原则

```text
原则 1: 单用户单当前会话。Suna 当前不做多会话管理；用户要么继续上一条，要么 /new。
原则 2: 不使用 embedding，不做向量检索或完整历史搜索。
原则 3: user_profile_memory 只保存长期用户画像，不保存项目知识、任务日志、工具结果或实现细节。
原则 4: Session State 保存当前会话状态，承接 compact 和 restore。
原则 5: TUI 展示真实可见对话；模型不必加载完整 transcript。
原则 6: Session compact 是低频高质量 LLM 请求，只在自动 80% 阈值或手动 /compact 时触发。
原则 7: compact 失败不 fallback、不伪压缩、不继续模型请求。
原则 8: 上下文结构必须缓存友好：稳定前缀在前，动态 user profile memory 靠近 latest user。
```

## 记忆模型

```text
┌─────────────────────────────────────────────────────────────┐
│ user_profile_memory                                          │
│ 长期用户画像：沟通偏好、工作习惯、长期约束、纠错和用户事实。    │
│ 数量很小，默认最多 30 条。                                    │
├─────────────────────────────────────────────────────────────┤
│ conversation_state                                           │
│ 当前会话恢复状态。保存 session_state、可见对话快照和工具摘要。 │
├─────────────────────────────────────────────────────────────┤
│ session_state                                                │
│ 当前会话内部状态账本，由 compact 生成/更新并持久化。           │
├─────────────────────────────────────────────────────────────┤
│ working_memory                                               │
│ 当前进程内模型工作上下文。compact 后只保留 recent window。     │
├─────────────────────────────────────────────────────────────┤
│ memory_queue                                                 │
│ 待合并的结构化用户画像候选；daemon 批量处理后删除。            │
└─────────────────────────────────────────────────────────────┘
```

## user_profile_memory

`user_profile_memory` 是长期用户画像。它不是知识库，不是会话历史，不是项目文档，也不是任务账本。

### 应该记住

- 用户沟通偏好：喜欢简洁、直接、详细、先给结论等。
- 用户工作习惯：偏好先讨论设计、先看代码、先全链路分析、小步修改等。
- 用户长期约束：不要硬编码、不要兼容兜底、不要泄露本机信息等。
- 用户纠错记录：Suna 上次哪里做错了，下次应该避免什么。
- 用户明确要求长期记住的信息。
- 少量稳定用户事实：用户使用环境、主要语言等，且必须由用户明确提供。

### 不应该记住

- 所有会话内容。
- 某次任务的临时细节。
- 项目实现事实、工具 schema、TUI 快捷键、测试结果。
- 工具调用日志、错误日志、文件路径或代码片段。
- 本会话中的决策 ledger、任务进度和 open thread。
- assistant 自己总结出的项目设计结论。
- 低置信推测、过期状态或敏感隐私推断。

这些内容如果对当前会话后续有价值，应进入 `Session State`；如果是项目长期知识，应进入文档/代码。

### Schema

```text
id TEXT PRIMARY KEY
user_id TEXT NOT NULL
kind TEXT NOT NULL
content TEXT NOT NULL
tags TEXT NOT NULL DEFAULT '[]'
source TEXT NOT NULL DEFAULT 'inferred'
confidence REAL NOT NULL DEFAULT 0.7
priority INTEGER NOT NULL DEFAULT 50
is_core INTEGER NOT NULL DEFAULT 0
use_count INTEGER NOT NULL DEFAULT 0
last_used_at DATETIME
evidence TEXT NOT NULL DEFAULT ''
expires_at DATETIME
created_at DATETIME NOT NULL
updated_at DATETIME NOT NULL
```

`kind` 是固定枚举：

```text
communication | workflow | preference | constraint | correction | user_fact
```

`tags` 是开放语义标签，不是固定 domain 枚举。标签只做规范化：小写、短、通用、可复用；禁止 URL、路径、文件名、项目临时名。

`source` 是固定枚举：

```text
explicit | inferred | correction
```

`confidence` 表示用户画像的置信度。低置信画像不能成为 core。

### 数量限制

```text
user_profile_memory: 30 条以内
core memory:         5 条以内
每轮注入:             5 条以内
单条长度:             120-160 字符以内
memory brief:         400 tokens 以内
```

`core memory` 是几乎跨场景都应优先考虑的高置信长期偏好或反复纠错，但每轮注入仍要限制数量，避免挤掉当前 query 相关画像。

## memory_queue 与候选提取

`memory_queue` 保存结构化用户画像候选，不保存原始长对话。

Schema：

```text
id TEXT PRIMARY KEY
user_id TEXT NOT NULL
kind TEXT NOT NULL
content TEXT NOT NULL
tags TEXT NOT NULL DEFAULT '[]'
source TEXT NOT NULL DEFAULT 'inferred'
confidence REAL NOT NULL DEFAULT 0.7
evidence TEXT NOT NULL DEFAULT ''
significance TEXT NOT NULL
created_at DATETIME NOT NULL
processed_at DATETIME
attempts INTEGER NOT NULL DEFAULT 0
next_attempt_at DATETIME
last_error TEXT
```

主链路：

```text
用户消息
  -> 写入 working memory
  -> 规则提取用户画像候选
  -> 候选进入 memory_queue
  -> 从 user_profile_memory 召回 brief
  -> 构建请求并调用主 LLM
  -> 保存 conversation_state
```

重要边界：

- 只从用户消息产生候选。
- assistant 输出和 tool result 默认不进入 user_profile_memory。
- assistant 只能在未来显式“记住刚才原则”场景中作为短证据上下文，不能主动写长期画像。
- 候选提取不调用 LLM，只用规则判断长期信号。

入队触发：

```text
记住 / 以后 / 下次 / 不要再 / 我希望 / 我偏好 / 我更喜欢
用户纠正 Suna 的协作方式
明显跨任务稳定的协作偏好
```

不入队：

```text
可行 / 改吧 / 继续
当前项目实现细节
tool schema / UI 快捷键 / 测试结果 / 文件路径 / 日志
一次性任务要求
```

## User Profile Compaction

Suna 不做 append-only 画像库。daemon 批量处理队列时，把当前全部画像和结构化候选交给 LLM：

```text
old_user_profile_memory + structured_candidates -> new_user_profile_memory
```

batch 时机保持简单：

```text
batchSize = 5
batchTimeout = 60s
high significance 尽快处理
medium 攒批，最多等待 batchTimeout 后处理
失败通过 attempts / next_attempt_at / last_error 退避重试
```

LLM 只负责合并、清理和重写画像，不负责从原始聊天里猜记忆。

规则：

- 输出完整的新 user profile memory 列表，不是 patch。
- 合并相似项，优先更新而不是新增。
- 删除项目事实、实现细节、任务历史、工具 schema、UI 快捷键、路径、日志、测试结果和 session decisions。
- 删除 stale、重复、低置信、过细或不再有用的画像。
- `core <= 5`，且只用于高置信长期偏好、强约束或反复纠错。
- `total <= 30`。

## 召回策略

主链路召回不使用 LLM，也不使用 embedding。因为 user profile memory 数量很小，规则召回足够可靠。

召回发生在用户消息进入 working memory 后、主 LLM 请求前：

```text
用户输入
  -> working_memory.Add(user)
  -> buildSystemPrompt()
  -> user_profile_memory.BuildBrief(latest_user_text)
  -> 作为 internal-context user message 注入到最新 user message 之前
  -> 调用主 LLM
```

每轮最多注入 5 条：

```text
最多 1-2 条 core
+ query/tag/content 命中的相关画像
+ 少量 correction
```

排序必须稳定：

```text
is_core
kind weight
tag/query/content token match
priority
confidence
last_used_at
id
```

不要纯 priority top5，避免单一领域记忆长期挤占其他画像。

## conversation_state

Suna 当前只有一个当前会话。`conversation_state` 是这个会话的持久化恢复状态，不是完整历史库。

保存内容：

```text
user_id              当前固定为默认用户
session_state        当前会话的内部状态账本，compact 后生成/更新
last_messages        TUI 恢复展示用的真实可见 user/assistant transcript
tool_summary         工具操作摘要，仅用于 TUI 恢复展示
memory_processed_at  最近一次 user_profile_memory 队列处理时间
updated_at           更新时间
```

`last_messages` 保存真实可见对话，用于 TUI 恢复时展示。它只保存 user/assistant 纯文本消息，会剥离 assistant tool_calls/raw 结构，不保存 system Session State，不保存原始 tool result。

`session_state` 给模型恢复和 compact 使用。它保存当前会话的高价值状态：当前任务、已完成任务/话题账本、用户要求、关键决策、工具事实和未完成事项。

`tool_summary` 只保存轻量工具摘要，例如“exec [success]: go test ./... 通过”。恢复时可以通过 TUI 展示给用户，但不作为原始 tool 上下文放回模型。

## Session State

Session State 是当前会话状态，不是用户画像。

固定结构：

```markdown
# Session State

## Active context
当前正在做什么/聊什么，任务阶段，下一步。

## Completed work / topic ledger
本会话已完成任务、讨论过的话题、较早内容的可回忆索引。

## User requirements and decisions
用户明确要求、纠正、偏好、赞同/拒绝过的方案和已定决策。

## Tool facts
工具事实：读过什么、改过什么、跑过什么、失败过什么、验证结果是什么。

## Open threads
未完成、暂停、用户可能后续会继续的问题。

## Recovery note
未来恢复会话或 compact 后，agent 应如何接上。
```

设计要求：

- 当前任务不中断：coding/tool 任务要保留执行 checkpoint；纯对话要保留当前讨论焦点。
- 较早内容不消失：完成任务和旧话题至少保留模糊 ledger。
- 不 append-only：每次 compact 是 `旧 Session State + 新历史 -> 新 Session State`，不是不断追加摘要。
- bounded rewrite：Session State 有 token 预算，旧条目会合并为更短 ledger。
- 不进入 user_profile_memory：Session State 是当前会话状态，不是长期用户画像。

## 恢复行为

### 没有 compact 过

```text
TUI:
  展示 last_messages 中的真实可见对话

模型:
  last_messages + user_profile_memory + latest user
```

### compact 过

```text
TUI:
  仍展示 last_messages 中的真实可见对话

模型:
  Session State + dynamic recent messages + user_profile_memory + latest user
```

### 新建会话

`/new` 会清空 working memory、session_state、last_messages、tool_summary，并生成新的 session id。长期 `user_profile_memory` 保留。

## Working Memory Compact

Working memory 是当前 agent 会话内发送给模型的短期上下文。完整 LLM 请求还包括 system prompt、tool schemas、user_profile_memory 注入和 max output reserve。

Compact 的目标：

- 降低上下文 token 占用。
- 保障当前任务或当前对话不中断。
- 通过 Session State 记录较早任务/话题/决策。
- 避免大 tool result 持续占用模型上下文。

自动 compact 在每次 LLM 请求发出前执行 preflight 检测：

```text
estimated_request_tokens =
  system prompt
  + Session State
  + working messages
  + user_profile_memory / internal context 注入
  + tool schemas
  + max output reserve

触发条件:
  estimated_request_tokens > context_window * 0.8
```

触发后流程：

```text
1. 通知 TUI: session.compact_result {running:true}。
2. 按真实请求预算计算 recent window 可用 token。
3. 用代码确定性选择 dynamic recent messages。
4. 调用压缩 LLM：旧 Session State + 待折叠历史 -> 新 Session State。
5. 更新独立 Session State，WorkingMemory = dynamic recent messages。
6. 重新构造完整请求并再次估算。
7. 成功则通知 TUI compact_done，模型自动继续。
8. 如果 compact 失败或 compact 后仍超限，通知 TUI compact_error 并停止本轮模型请求。
```

失败时不修改 working memory，不写坏 Session State，不继续主模型请求。

### Token 估算校准与动态安全垫

本地 token 估算公式按通用 tokenizer 调参，不同 provider 会有系统性偏差：Claude 等模型的真实 tokenizer 对中文和代码切分更细，本地估算会明显低估，导致 compact 触发过晚、接近窗口才压缩甚至撞窗口报错。为此引入按模型的 token 估算校准（`model.TokenCalibrator`）：

```text
校准系数 coef = 真实 input token / 当轮原始本地估算
```

- 数据来源：每次 LLM 请求返回后，用真实 `usage.InputTokens`（已含 cache 创建/读取）与该轮未校准的原始估算回喂校准器。
- 异常防护（防止中转站 usage 回传错误带偏系数）：硬区间过滤（ratio 落在 [0.25,4.0] 外直接丢弃）+ 相对离群过滤（已有稳定系数后单次大幅偏离视为抖动跳过）+ EMA 平滑（单次观测只挪动系数一小步）。
- 归属：校准器由 Agent 持有一份，注入主 Agent 与 subtask 的 runner，按 modelRef 共享。它是模型 tokenizer 的校准常数，不是会话上下文，因此 subtask 复用不违反上下文隔离，反而加快收敛。
- 应用：compact 触发判断（`estimated_request_tokens`）在物理尺度乘上 coef 再与窗口比；传给压缩器的 recent 预算除回估算尺度（压缩器内部仍用未校准估算填预算）。无校准数据或校准器未注入时 coef=1.0，行为与未校准完全一致。

安全垫（`estimator_safety_tokens`）随校准状态自适应：

- 未校准或校准刚回退（中转站异常）：维持 1/16（约 6.25%，最低 8192）厚垫兜底。
- 已有稳定校准数据：收到 1/40（约 2.5%，最低 2048），释放出更多可用上下文。

安全垫只补偿校准器管不到的瞬时偏差（本轮新增消息、单次波动、系数回退），不能归零；手动 `/compact` 不依赖校准状态，保守使用未校准口径。

## 缓存友好上下文结构

每次主模型请求尽量保持稳定前缀：

```text
System prompt / project instructions / skills / tool schemas  稳定
Session State                                                 compact 后稳定
Recent messages                                                普通轮次 append-only
User Profile Memory internal block                            靠近 latest user
Latest user                                                    每轮变化
```

约束：

- Session State 不拼进 system prompt，避免 compact 后污染稳定 system 前缀。
- 不做每轮 rolling Session State update；只在自动 compact、手动 compact 或 restore 加载时变化。
- User Profile Memory 按 latest user query 召回，插在最新 user message 之前。
- Recent messages 普通轮次只追加，不每轮重排；compact 时才按预算裁剪。

## Subtask 关系

Subtask 保持独立上下文：

- 不继承 main conversation。
- 不继承 main working memory。
- 不继承 main Session State。
- 不继承 user_profile_memory，除非 main 显式放进 task/context。
- 子任务内部可 auto compact，但只影响子任务临时 working memory，不持久化、不污染主会话。

## 不做的事情

当前明确不做：

- embedding。
- 向量检索。
- 完整历史搜索。
- append-only 事实库。
- 多会话管理。
- workspace/project 记忆层。
- `/memory search` 精准历史回溯。
- subtask 独立长期记忆。
- 退出 TUI 时额外发起 LLM compact。
- 从 assistant 输出或 tool result 自动提取长期用户画像。

## 成功标准

- 用户一直对话时，自动 compact 不打断当前任务。
- 用户问起较早完成的任务或讨论过的话题，Suna 能通过 Session State 模糊回忆。
- 退出后恢复上一轮，TUI 展示真实对话，模型能用 Session State + recent 接上。
- 新建会话后，临时上下文清空，但长期 user_profile_memory 保留。
- user_profile_memory 越用越懂用户，但不会积累项目实现细节和任务日志。
- compact 后 token 占用明显下降，且失败时明确报错、不伪压缩。
- prompt cache 命中率不会因为 user_profile_memory 或 Session State 设计大幅下降。
