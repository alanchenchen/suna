# 07 — 感知层 (Sense)

> 当前实现状态: **预留 / 设计归档**
>
> 当前代码只保留了 SQLite `triggers` 表、protocol 方法常量/状态字段等预留结构；没有 `internal/trigger`、SenseManager、Timer/Watcher/Webhook/Stream runtime，也没有 `trigger.*` 业务路由。以下内容描述目标设计，不代表当前 daemon 已经 24/7 运行感知源。

感知层是 Suna 三层架构的第一层。传统 agent 的感知是被动的——只有当用户发消息时才"醒来"。Suna 的感知是主动的——持续监听环境变化，将信号直接传递给行动层。

目标设计中，感知源在 Daemon (sunad) 进程内 24/7 运行，与 TUI 完全解耦。当前实现尚未接入这条运行链路。

## 信号流转

```
感知源检测到信号 → 过滤器 → 行动层 (agent.Run)

感知层: "config.yaml 变了"
  → 过滤器: 是用户关注的路径?
    → 是: agent.Run("config.yaml 变了，检查并处理")
    → 否: 忽略

感知层: "早上 9 点了"
  → 有对应的定时任务?
    → 有: agent.Run(task)
    → 否: 不可能 (定时任务都是用户/agent 创建的)

感知层: ".go 文件保存了"
  → 过滤器: 用户在工作目录下?
    → 是: agent.Run("检测到文件变化，检查是否需要处理")
    → 否: 忽略
```

## 四种触发器

计算机世界所有异步事件的本质只有 4 种：

| 触发器 | 驱动类型 | 典型场景 |
|---|---|---|
| Timer | 时间驱动 | "每天早上9点汇总报告" |
| Watcher | 状态驱动 | "config.yaml 变了就重新部署" |
| Webhook | 外部驱动 | "有新 PR 就通知我" |
| Stream | 数据驱动 | "日志出现 ERROR 就告警" |

## 统一接口

```go
type PerceptionSource interface {
    ID() string
    Type() string  // "timer" / "watcher" / "webhook" / "stream"
    Start(ctx context.Context, handler func(signal Signal)) error
    Stop()
    Marshal() ([]byte, error)
}

type Signal struct {
    SourceID   string
    SourceType string
    Content    string            // 信号内容
    Metadata   map[string]any    // 额外信息 (文件路径/事件类型/HTTP body等)
    Timestamp  time.Time
    Priority   int               // 1=低, 2=中, 3=高
}

type SenseManager struct {
    sources    map[string]PerceptionSource
    agent      *Agent             // 直接驱动行动层
    store      TriggerStore       // SQLite 持久化
}

func (sm *SenseManager) LoadAll() error {
    // 启动时从 SQLite 加载所有感知源 → 逐个 Start
}

func (sm *SenseManager) Register(ps PerceptionSource) error {
    // 注册新感知源 → 持久化 → Start
}

func (sm *SenseManager) Remove(id string) error {
    // Stop → 从 SQLite 删除
}

func (sm *SenseManager) handleSignal(signal Signal) {
    // 感知过滤器 → agent.Run(signal.Content)
}
```

## Timer

### 功能

Cron 表达式驱动的定时触发。

### 配置

目标设计中，用户不直接写 TOML 配置。触发器由用户通过自然语言让 agent 创建，或通过 TUI/CLI 管理入口创建，实际数据存储在 SQLite `triggers` 表中。以下 TOML 格式仅作文档用途，展示配置字段：

```toml
# 文档用途，用户不需要手写
[[triggers]]
id = "morning-report"
type = "timer"
cron = "0 9 * * 1-5"           # 工作日每天9点
task = "汇总昨天的工作日志并发送到我的邮箱"
model = "default"
enabled = true
```

### 实现

```
库: github.com/robfig/cron/v3    (Go 生态最成熟的 cron 库)

流程:
  1. 解析 cron 表达式
  2. 注册到 cron.Scheduler
  3. 到点触发 → handler(task)
  4. handler 调 agent.Run(ctx, task)
  5. agent 执行结果 → 更新 conversation_state / memory_queue
     如果客户端在线 → 通过 protocol event 推送结果
     如果 TUI 离线 → 只保留最近恢复状态和必要 active memory

感知源在 daemon 进程内运行，不依赖 TUI
```

### 边界处理

```
- 上次任务未完成又触发 → 跳过本次 (防止堆积)
- 任务执行失败 → 记录失败记忆 → 不重试 (避免无限循环)
- Daemon 退出时 → 感知源随 daemon 停止 → 状态已持久化到 SQLite → daemon 重启后恢复
- 时区: 使用用户本地时区
```

## Watcher

### 功能

文件/目录变化监听触发。

### 配置

```
[[triggers]]
id = "config-reload"
type = "watcher"
paths = ["/etc/myapp/config.yaml"]
events = ["write", "create"]    # write/create/rename/remove/chmod
task = "配置文件变了，检查并重新部署服务"
debounce = "2s"                  # 防抖，避免短时间内多次触发
```

### 实现

```
库: fsnotify (跨平台文件监听)

流程:
  1. fsnotify.Watcher 监听指定路径
  2. 事件到达 → debounce (2秒内的事件合并)
  3. 合并后触发 → handler(task)
  4. agent.Run 检查变化并执行操作

debounce 必要性:
  - 编辑器保存文件可能触发多个事件 (write + chmod)
  - npm install 可能产生数百个文件变化
  - debounce 合并为一次触发
```

### 边界处理

```
- 监听的文件被删除 → 触发一次 remove 事件 → 继续监听 (文件可能被重建)
- 监听的目录不存在 → 启动时跳过 → 定期重试 (每 60 秒检查一次)
- 权限不足 → 记录错误 → 不崩溃
- 新建文件后立即监听 (如 git clone 后监听新目录)
```

## Webhook

### 功能

HTTP 端点接收外部事件触发。

### 配置

```
[[triggers]]
id = "github-pr"
type = "webhook"
path = "/github-pr"             # 监听路径
secret = "xxx"                  # 可选, HMAC 签名验证
task_template = "GitHub 仓库 {{.repository}} 有新的 Pull Request #{{.number}}: {{.title}}"
```

### 实现

```
Daemon (sunad) 内置一个轻量 HTTP Server (net/http):
  - 默认端口: 0 (随机分配) 或用户指定
  - 路径: /webhook/{id}
  - 方法: POST

流程:
  1. 外部服务 (GitHub/GitLab/自定义) 发 POST 请求
  2. 可选: HMAC 签名验证
  3. 解析 JSON body
  4. 渲染 task_template (Go template)
  5. handler(rendered_task) → agent.Run

安全:
  - 可选 secret 字段 → 请求必须带正确签名
  - 无签名 → 任何知道 URL 的人都能触发 (仅限内网使用)
```

### 边界处理

```
- agent 正忙 → 事件排队 (channel buffer)
- 事件堆积 → 丢弃最旧的事件 + 记录警告
- 响应: 立即返回 200 (不等待 agent 执行完毕)
- 端口冲突 → 提示用户配置其他端口
```

## Stream

### 功能

持续数据流消费，按条件触发。

### 配置

```
[[triggers]]
id = "log-monitor"
type = "stream"
source = "file:/var/log/myapp.log"   # file: / ws: / exec:
pattern = "ERROR|FATAL"              # 可选, 正则过滤
task_template = "日志监控发现异常: {{.matched_line}}"
cooldown = "5m"                       # 冷却时间，避免频繁触发
```

### Source 类型

```
file:   tail -f 模式监听文件追加内容
        实现用 fsnotify + offset 追踪

ws:     WebSocket 连接，持续接收消息
        实现 gorilla/websocket 或标准库

exec:   持续执行命令并读取 stdout
        例: exec:"kubectl logs -f my-pod" → 持续读取输出
```

### 实现

```
流程:
  1. 打开 source (tail file / connect ws / exec command)
  2. 持续读取数据流
  3. 如果有 pattern → 正则匹配过滤
  4. 匹配到 → 检查 cooldown (上次触发距今 > cooldown?)
  5. 通过 cooldown → handler(task)
  6. 继续监听

cooldown 必要性:
  - 日志中可能连续出现 100 个 ERROR
  - 不冷却 → 触发 100 次 agent.Run → 资源爆炸
  - 冷却 5 分钟 → 合并为一次触发
```

### 边界处理

```
- 文件被 truncate (log rotate) → 重置 offset → 继续监听
- WebSocket 断开 → 自动重连 (指数退避: 1s, 2s, 4s, 8s, max 60s)
- 命令退出 → 记录错误 → 不重试 → 标记 trigger 为 unhealthy
```

## 感知过滤器

不是所有信号都需要处理。过滤器减少噪音，避免行动层过载。

```
信号 → 过滤器 → 决定是否触发 agent.Run

过滤规则:
  - 用户直接消息 → 总是处理
  - 文件变化:
    - .git/ → 忽略
    - node_modules/ → 忽略
    - 其他 → 按路径匹配已注册的 Watcher 触发器
  - 时间事件 → 匹配已注册的 Timer
  - Webhook/Stream → 总是处理 (用户主动配置的)

节流:
  - Debounce: 2秒内的同类信号合并
  - 优先级: 用户消息(3) > 时间事件(2) > 文件变化(1) > 流数据(1)
```

## 持久化任务 (Task / Run)

感知层最终不能直接把 signal 变成一次临时 `agent.Run` goroutine。用户从 TUI 发起的消息、Timer/Watcher/Webhook/Stream 触发的自动任务，本质上都应该先落成一条持久化任务，再由 daemon 执行。

这样可以统一处理：

- TUI 断开后 agent 仍在后台运行。
- 下次进入 TUI 时恢复正在运行、已完成或失败的任务状态。
- transport 写入成功但客户端未收到响应时，避免重复提交同一条用户输入。
- Trigger 触发的任务在没有 TUI 在线时仍可执行，并在之后被用户查看。
- AskUser / GuardConfirm 等等待用户输入的中间状态可以被新 TUI 连接重新接管。

### 统一任务来源

```
用户输入 (TUI/local/Web)   -> tasks
Timer / Watcher / Webhook -> tasks
Stream                   -> tasks
远期其他入口              -> tasks
```

任务表不是长期聊天历史库。它保存的是 daemon 需要恢复和调度的执行状态，而不是完整对话 transcript。长期用户偏好仍进入 `user_memory`，最近对话恢复仍由 `conversation_state` 提供轻量快照。

### 目标表结构

```text
tasks
| id | source_type | source_id | client_msg_id | input | status |
| created_at | started_at | finished_at | last_error | result_summary |

task_events
| id | task_id | seq | type | payload_json | created_at |
```

`task_events` 可以先只保存关键事件，不必保存所有 streaming chunk：

```text
user_message
assistant_final
ask_user
guard_confirm
error
done
```

如果后续需要更完整的重连体验，再扩展为 `events_since(last_seen_event_id)`。

### 状态机

```text
queued      已创建，等待 daemon 调度
running     agent loop 正在执行
waiting     等待 AskUser / GuardConfirm 等用户输入
completed   正常完成
failed      执行失败，记录 last_error
cancelled   用户或 daemon 明确取消
```

### 幂等提交

TUI 提交用户输入时应带 `client_msg_id`。daemon 接收后创建或复用对应 task，并返回 `task_id`。

```text
TUI -> daemon: agent.sendMessage(content, client_msg_id)
daemon:
  - 如果 client_msg_id 已存在，返回已有 task_id/status
  - 如果不存在，创建 task(status=queued/running) 并返回 task_id
```

这样 transport 断开时客户端不需要猜测“上一条输入有没有被接收”，也不应该自动重发未知状态的输入。

### 与当前 TUI 断连边界的关系

当前实现中，`agent.sendMessage` 被 daemon 接收后会异步运行；TUI 断开不会天然取消 agent。但因为没有持久化 task/run 状态，也没有事件回放，新 TUI 连接只能恢复 `conversation_state` 的最近可见消息，无法可靠判断后台任务是否仍在运行、是否失败、是否正在等待 AskUser/GuardConfirm。

这个问题不要在 TUI 层用本地草稿、自动重发或特殊补丁解决。应在实现持久化任务时一起解决：daemon 是任务状态 owner，TUI 只订阅和展示任务状态。

### 调度边界

```text
- 同一用户当前只允许一个 main task running；新任务 queued 或按策略拒绝。
- Trigger 重复触发时，根据 trigger 策略 skip / coalesce / enqueue。
- Daemon 重启时，queued 继续执行；running/waiting 标记为 interrupted 或恢复为 waiting，不能静默丢失。
- completed/failed task 可按 TTL 清理，只保留摘要和必要事件。
```

## 感知源的持久化

```
SQLite 表: triggers (表名不变，兼容旧数据)
| id | type | config_json | signal_template | enabled | last_fire | created_at |

Daemon 启动时:
  1. 查询 triggers WHERE enabled=true
  2. 按 type 反序列化为具体 PerceptionSource
  3. 逐个 Start
  4. 某个 Start 失败 → 记录错误 → 继续

Daemon 退出时:
  1. 逐个 Stop
  2. 状态已经持久化
```

## 感知源的管理

### 感知源的管理

感知源不通过 TUI 命令管理。用户通过自然语言交互:

```
用户: "每天早上9点帮我汇总昨天的工作"
Agent: 理解意图 → 创建 Timer 感知源 → 保存 → 启动
Agent: "好的，已设置每天早上9点汇总。"

用户: "监听 config.yaml，变了就重新部署"
Agent: 创建 Watcher 感知源 → 保存 → 启动

用户: "日志里出现 ERROR 就告诉我"
Agent: 创建 Stream 感知源 → 保存 → 启动

用户: "帮我看看设置了哪些定时任务"
Agent: 查询 triggers 表 → 自然语言回复

用户: "不需要那个文件监听了"
Agent: 理解意图 → 删除对应感知源
```

注：/trigger 命令已移除。所有感知源管理通过自然语言完成，降低命令学习成本。

## 新增依赖

```
Timer:    github.com/robfig/cron/v3    (Cron 调度)
Watcher:  fsnotify                     (文件监听，跨平台)
Stream:   标准库 (tail file / exec) + gorilla/websocket (可选)
Webhook:  标准库 net/http
```
