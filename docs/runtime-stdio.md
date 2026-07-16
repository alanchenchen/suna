# Suna stdio runtime 开发者接入手册

本文面向第三方 UI、桌面端、IDE 插件、本地 Web 服务或脚本开发者。目标是让你不需要 import Go 包、不需要理解 Suna 的 local socket / Named Pipe，也能通过一个子进程接入 Suna Agent 能力。

推荐架构：

```txt
你的 UI / Web Backend / Electron Main / IDE Extension
  -> spawn `suna runtime --transport stdio`
  -> stdin/stdout JSON-RPC + NDJSON
  -> Suna Agent / Tools / Memory / Skills / MCP
```

Suna runtime 负责 Agent 能力；文件浏览、终端、HTTP/WebSocket 服务、模型列表拉取等 UI 辅助能力由你的项目自己实现。

---

## 0. 一分钟跑通

先确认 runtime 能握手：

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"runtime.hello","params":{"protocol_version":"0.2","client":{"name":"smoke","version":"0.1.0","type":"shell"}}}' \
  | suna runtime --transport stdio
```

你应该在 stdout 看到一行 JSON-RPC response，形如：

```json
{"jsonrpc":"2.0","id":1,"result":{"protocol_version":"0.2","runtime_version":"v0.8.0","transport":"stdio","capabilities":{"agent":true,"streaming":true,"tools":true,"guard":true,"ask_user":true,"session":true,"multi_session":true,"handoff":true,"config":true,"memory":true,"skills":true,"mcp":true},"content_sources":{"text":true,"image_path":true,"image_url":true},"limits":{"max_tool_result_bytes":16384}}}
```

stderr 可能有：

```txt
runtime: started (pid 12345)
```

这是人类诊断日志，不是协议消息。客户端只解析 stdout。

---

## 1. 启动 runtime

命令：

```bash
suna runtime --transport stdio
```

必须显式传入 `--transport stdio`。只运行 `suna runtime` 会打印 usage 并退出，避免未来新增 transport 后入口语义变得隐式。

Node.js 启动示例：

```js
import { spawn } from "node:child_process";

const proc = spawn("suna", ["runtime", "--transport", "stdio"], {
  cwd: projectRoot, // 用户当前项目目录；runtime 会把子进程 cwd 作为工作区
  stdio: ["pipe", "pipe", "pipe"],
});
```

stdio 约定：

| 流 | 用途 |
|---|---|
| stdin | 客户端写入 JSON-RPC request，一行一个 JSON。 |
| stdout | Suna 输出 JSON-RPC response / notification，一行一个 JSON。 |
| stderr | 人类可读诊断日志，不参与协议。 |

生命周期：

- stdio runtime 是前台单进程 headless runtime。
- 父进程关闭 stdin 后，runtime 会退出。
- stdio runtime 不写 `sunad.pid`；`sunad.pid` 只属于官方 TUI 使用的后台 local daemon。
- v0.2 不支持多个 Suna 进程同时写入同一个数据目录。第三方 UI 应独占启动 runtime；不要同时打开官方 TUI 和第三方 runtime 操作同一套 `~/.suna` 数据。

---

## 2. JSON-RPC / NDJSON 规则

Suna stdio runtime 使用 JSON-RPC 风格消息，外层 framing 是 NDJSON：**每一行是一条完整 JSON**。

### 2.1 Request

```json
{"jsonrpc":"2.0","id":1,"method":"config.get","params":{}}
```

字段：

| 字段 | 说明 |
|---|---|
| `jsonrpc` | 固定为 `"2.0"`。 |
| `id` | v0.2 只支持整数 id；response 会原样回传同一个整数。 |
| `method` | 方法名，例如 `agent.sendMessage`。 |
| `params` | 方法参数。建议总是传对象；无参数时传 `{}`。 |

v0.2 限制：

- 客户端 request 必须带整数 `id`。
- 暂不支持 string id。
- 暂不支持客户端 notification。

### 2.2 Response

成功：

```json
{"jsonrpc":"2.0","id":1,"result":{"models":[],"active_model":""}}
```

失败：

```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"content is required","data":{"kind":"invalid_request"}}}
```

客户端逻辑：

- 有 `id` 的消息是 response，应匹配 pending request。
- response 要么有 `result`，要么有 `error`。

### 2.3 Notification

```json
{"jsonrpc":"2.0","method":"agent.delta","params":{"kind":"assistant","content":"你好"}}
```

客户端逻辑：

- 没有 `id`、有 `method` 的消息是 notification。
- notification 是 daemon 主动推送的异步事件，不对应某个 request 的直接返回。
- 不要把 method response 当 notification，也不要等待 `agent.sendMessage` response 里出现模型回复；模型回复来自后续 notification。

---

## 3. 第一步必须握手：`runtime.hello`

stdio runtime 要求第一条 request 必须是 `runtime.hello`。

Request params：

| 字段 | 必填 | 说明 |
|---|---:|---|
| `protocol_version` | 否 | 当前公开版本是 `"0.2"`；为空时按当前默认版本处理。 |
| `client.name` | 否 | 客户端名称，用于诊断和未来能力协商。 |
| `client.version` | 否 | 客户端版本。 |
| `client.type` | 否 | 客户端类型，例如 `web`、`desktop`、`ide`、`node`。 |

Request 示例：

```json
{"jsonrpc":"2.0","id":1,"method":"runtime.hello","params":{"protocol_version":"0.2","client":{"name":"my-web-ui","version":"0.1.0","type":"web"}}}
```

Result 字段：

| 字段 | 说明 |
|---|---|
| `protocol_version` | runtime 选择的协议版本。 |
| `runtime_version` | Suna 版本。 |
| `transport` | 真实承载层，stdio runtime 下是 `"stdio"`。这个值由 Suna 注入，客户端不能通过 params 声明。 |
| `capabilities` | 能力开关，客户端应按 key 判断能力，不要从版本号推断。 |
| `content_sources` | `agent.sendMessage` 支持的内容来源。 |
| `limits` | 协议层稳定限制，例如 tool result 截断阈值。 |

Result 示例：

```json
{
  "protocol_version":"0.2",
  "runtime_version":"v0.8.0",
  "transport":"stdio",
  "capabilities":{
    "agent":true,
    "streaming":true,
    "tools":true,
    "guard":true,
    "ask_user":true,
    "session":true,
    "multi_session":true,
    "handoff":true,
    "config":true,
    "memory":true,
    "skills":true,
    "mcp":true
  },
  "content_sources":{
    "text":true,
    "image_path":true,
    "image_url":true
  },
  "limits":{
    "max_tool_result_bytes":16384
  }
}
```

未握手直接调用其它 method，会返回：

```json
{"jsonrpc":"2.0","id":2,"error":{"code":-32010,"message":"runtime.hello is required before other methods","data":{"kind":"handshake_required"}}}
```

---

## 4. 最小 Node.js 客户端

这段代码展示一个 UI 层 SDK 的最小形状：启动 runtime、发送 request、分发 notification、处理错误和退出。

```js
import { spawn } from "node:child_process";
import { createInterface } from "node:readline";

export class SunaRuntime {
  constructor({ bin = "suna", cwd = process.cwd() } = {}) {
    this.nextId = 1;
    this.pending = new Map();
    this.handlers = new Map();

    this.proc = spawn(bin, ["runtime", "--transport", "stdio"], {
      cwd,
      stdio: ["pipe", "pipe", "pipe"],
    });

    createInterface({ input: this.proc.stdout }).on("line", (line) => {
      this.#handleLine(line);
    });

    // stderr 只用于日志，不要从 stderr 解析协议。
    this.proc.stderr.on("data", (chunk) => {
      process.stderr.write(`[suna] ${chunk}`);
    });

    this.proc.on("error", (err) => this.#rejectAll(err));
    this.proc.on("close", (code, signal) => {
      this.#rejectAll(new Error(`suna closed: code=${code} signal=${signal}`));
    });
  }

  hello() {
    return this.request("runtime.hello", {
      protocol_version: "0.2",
      client: { name: "example-ui", version: "0.1.0", type: "node" },
    });
  }

  request(method, params = {}, { timeoutMs = 30_000 } = {}) {
    const id = this.nextId++;
    const payload = { jsonrpc: "2.0", id, method, params };

    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`request timeout: ${method}`));
      }, timeoutMs);

      this.pending.set(id, { resolve, reject, timer });
      this.proc.stdin.write(`${JSON.stringify(payload)}\n`, (err) => {
        if (!err) return;
        clearTimeout(timer);
        this.pending.delete(id);
        reject(err);
      });
    });
  }

  on(method, handler) {
    const list = this.handlers.get(method) ?? [];
    list.push(handler);
    this.handlers.set(method, list);
  }

  close() {
    this.proc.stdin.end();
  }

  #handleLine(line) {
    if (!line.trim()) return;
    const msg = JSON.parse(line);

    if (typeof msg.id === "number") {
      const pending = this.pending.get(msg.id);
      if (!pending) return;
      clearTimeout(pending.timer);
      this.pending.delete(msg.id);

      if (msg.error) {
        const err = new Error(msg.error.message);
        err.code = msg.error.code;
        err.data = msg.error.data;
        pending.reject(err);
      } else {
        pending.resolve(msg.result);
      }
      return;
    }

    if (msg.method) {
      for (const handler of this.handlers.get(msg.method) ?? []) {
        handler(msg.params);
      }
    }
  }

  #rejectAll(err) {
    for (const [id, pending] of this.pending) {
      clearTimeout(pending.timer);
      pending.reject(err);
      this.pending.delete(id);
    }
  }
}
```

只读 smoke test：

```js
const suna = new SunaRuntime({ cwd: "/path/to/project" });
try {
  const hello = await suna.hello();
  console.log("hello", hello);

  const status = await suna.request("daemon.status", {});
  console.log("status", status);
} finally {
  suna.close();
}
```

发送一条用户消息：

```js
const suna = new SunaRuntime({ cwd: "/path/to/project" });
await suna.hello();

let assistant = "";

suna.on("agent.delta", (p) => {
  if (p.kind === "assistant") assistant += p.content;
});

suna.on("agent.run", (p) => {
  if (p.state === "retrying") {
    console.log(`retrying ${p.attempt}/${p.max_attempts}, wait ${p.delay_ms}ms`);
  }
  if (p.state === "done") {
    console.log("assistant:", assistant);
  }
  if (p.state === "failed") {
    console.error("run failed:", p.error ?? p.message);
  }
});

await suna.request("agent.sendMessage", {
  parts: [{ type: "text", text: "hello" }],
});
```

---

## 5. 接入流程建议

一个完整 UI 通常按这个顺序接入：

1. spawn `suna runtime --transport stdio`，设置 `cwd` 为项目目录。
2. 建立 JSON-RPC pending map 和 notification dispatcher。
3. 发送 `runtime.hello`。
4. 调用 `config.get`，检查是否已有可用模型。
5. 调用 `session.list`，再按 UI 选择调用 `session.create` 或 `session.attach`。
6. 用户发送消息时调用 `agent.sendMessage`。
7. 持续监听：
   - `agent.delta`：流式文本。
   - `agent.run`：running / retrying / done / failed / cancelled。
   - `agent.tool_start` / `agent.tool_guard` / `agent.tool_end`：工具展示。
   - `agent.ask_user`：弹出用户输入。
   - `agent.guard_confirm`：弹出高风险操作确认。
   - `agent.usage`：用量统计。
8. UI 离开当前 session 时调用 `session.detach`；进程退出时 `stdin.end()`。

---

## 6. Method 列表

本节的 Request 示例是完整 JSON-RPC request；Result 示例默认只展示 `result` 对象。真实 stdout response 外层仍然是：`{"jsonrpc":"2.0","id":请求ID,"result":...}`。

### 6.1 Runtime

#### `runtime.hello`

用途：stdio runtime 握手和能力发现。必须作为第一条 request。

Params：见 [第 3 节](#3-第一步必须握手runtimehello)。

Result：见 [第 3 节](#3-第一步必须握手runtimehello)。

---

### 6.2 Agent

#### `agent.sendMessage`

用途：发送用户消息。response 只表示任务已接收；实际模型输出来自 notification。

Params：

| 字段 | 必填 | 说明 |
|---|---:|---|
| `client_msg_id` | 否 | UI 自己生成的消息 ID，便于未来做去重或关联。 |
| `parts` | 是 | 消息内容数组。纯文本也必须放在 text part 中。 |

`parts` 支持：

| 类型 | 示例 |
|---|---|
| 文本 | `{"type":"text","text":"hello"}` |
| 图片路径 | `{"type":"image","path":"/absolute/path/a.png","mime_type":"image/png"}` |
| 图片 URL | `{"type":"image","url":"https://example.com/a.png","mime_type":"image/png"}` |

Request 示例：

```json
{"jsonrpc":"2.0","id":10,"method":"agent.sendMessage","params":{"parts":[{"type":"text","text":"hello"}]}}
```

Result 示例：

```json
{"status":"accepted"}
```

随后可能收到：

```json
{"jsonrpc":"2.0","method":"agent.run","params":{"state":"running","phase":"model"}}
{"jsonrpc":"2.0","method":"agent.delta","params":{"kind":"assistant","content":"你好"}}
{"jsonrpc":"2.0","method":"agent.run","params":{"state":"done"}}
```

#### `agent.cancel`

用途：取消当前 run。

Params：`{}`

Result：

```json
{"status":"cancelled"}
```

#### `agent.resumeRun`

用途：当前 run 因模型错误失败且 `agent.run.resume_available=true` 时，不新增用户消息，继续未完成 turn。

Params：`{}`

Result：

```json
{"status":"accepted"}
```

#### `agent.askReply`

用途：回复 `agent.ask_user` notification。

Params：

| 字段 | 必填 | 说明 |
|---|---:|---|
| `id` | 是 | `agent.ask_user.params.id`。 |
| `answer` | 是 | 用户回答。 |

Request 示例：

```json
{"jsonrpc":"2.0","id":11,"method":"agent.askReply","params":{"id":"opaque-ask-id","answer":"A"}}
```

Result：

```json
{"status":"ok"}
```

#### `agent.guardReply`

用途：回复 `agent.guard_confirm` notification。

Params：

| 字段 | 必填 | 说明 |
|---|---:|---|
| `id` | 是 | `agent.guard_confirm.params.id`。 |
| `decision` | 是 | `"approve"` 或 `"reject"`。 |

Request 示例：

```json
{"jsonrpc":"2.0","id":12,"method":"agent.guardReply","params":{"id":"opaque-guard-id","decision":"approve"}}
```

Result：

```json
{"status":"ok"}
```

---

### 6.3 Session

#### `session.list`

用途：列出 sessions。

Params：

```json
{"active_only":false}
```

Result：

```json
{"sessions":[{"id":"session_id","title":"","cwd":"/repo","status":"idle","client_count":0,"message_count":3}]}
```

`active` 不作为字段返回，客户端按 `client_count > 0 || status != "idle"` 派生。

#### `session.create`

用途：创建新 session 并 attach 当前连接。`cwd` 必填，影响 prompt、exec 和文件类工具的默认相对路径。

Params：

```json
{"cwd":"/absolute/project/path","title":""}
```

Result：`SessionSnapshot`，包含 `session`、最近可见 `messages`、`compacted`、`tool_summary` 和可选 `current_run`。

#### `session.attach`

用途：attach 到已有 session。Resume 和 Join Active 都使用该方法。

Params：

```json
{"session_id":"session_id","require_active":false}
```

Join Active 时应传 `require_active:true`，用于防止陈旧 UI 把已经 idle 的 session 误当成 active join。

Result：`SessionSnapshot`。

#### `session.detach`

用途：当前连接离开当前 session，但保持 stdio 连接。UI 回到 session 选择页时应调用。

Params：`{}`

Result：

```json
{"status":"detached"}
```

#### `session.update`

用途：更新当前 attached session 的 title 或模型选择。只更新 title 时可在运行中执行；更新 `model_ref` 时必须处于 idle。

Params：

```json
{"session_id":"session_id","title":"new title","model_ref":"openai/gpt-4o-mini"}
```

`title` 与 `model_ref` 均为可选，但请求至少应包含其一。更新 `model_ref` 成功后，该 session 后续主对话、Guard、Skill review 和 compact 都使用新模型；不会修改 `config.active_model`。

Result：`SessionSnapshot`。

#### `session.delete`

用途：删除非当前、非 active、无人 attached 的 idle session。

Params：

```json
{"session_id":"session_id"}
```

Result：

```json
{"deleted":true}
```

#### `session.compact`

用途：手动压缩当前 session 上下文。compact 会独占 session；如果 session 正在 running/waiting/compacting，会返回 busy error。

Params：`{}`

Result：

```json
{"status":"ok"}
```

压缩详情通过 `session.compact_result` 推送。

#### `session.usage`

用途：查询用量摘要。

Params：`{}`

Result 示例：

```json
{
  "today":{"input_tokens":1000,"output_tokens":200,"requests":3},
  "week":{"input_tokens":5000,"output_tokens":1200,"requests":12},
  "month":{"input_tokens":12000,"output_tokens":3000,"requests":30}
}
```

#### `SessionSnapshot`

```json
{
  "session":{"id":"session_id","title":"","cwd":"/repo","model_ref":"openai/gpt-4o-mini","status":"running","client_count":2,"message_count":3},
  "messages":[{"role":"user","content":"hello"}],
  "compacted":false,
  "tool_summary":null,
  "current_run":{"status":"running","phase":"model","assistant_buffer":"partial","reasoning_buffer":"","waiting_type":"","can_control":false}
}
```

snapshot 是轻量恢复视图，不保证完整 tool timeline / event replay。

---

### 6.4 Config

#### `config.get`

用途：读取当前配置。

Params：`{}`

Result 示例：

```json
{
  "models":[
    {
      "provider":"openai",
      "protocol":"openai_chat",
      "model":"gpt-4o-mini",
      "base_url":"https://api.openai.com/v1",
      "context_window":128000,
      "max_output_tokens":8192,
      "has_api_key":true
    }
  ],
  "active_model":"openai/gpt-4o-mini",
  "locale":"zh",
  "theme":"dark",
  "guard_mode":"ask",
  "workspace":"/path/to/project"
}
```

说明：`has_api_key` 只表示已保存 key；不会返回真实 API key。

#### `config.set`

用途：更新模型配置、激活模型、通用设置。

公共 Params：

| 字段 | 说明 |
|---|---|
| `action` | 必填。取值：`upsert_model`、`delete_model`、`activate_model`、`update_general`。 |
| `model` | `upsert_model` 使用。 |
| `model_ref` | 更新/删除已有模型时使用，例如 `openai/gpt-4o-mini`。 |
| `active_model` | 新建 session 的默认模型。修改它不会改变已有 session；删除当前默认模型时，daemon 会选择一个剩余模型作为新默认值，或在没有模型时清空。 |
| `api_key` | 新增/更新 provider API key。Suna 会自动 trim 首尾空白。 |
| `delete_api_key` | 删除模型时，如果该 provider 不再被其它模型使用，可同时删除 key。 |
| `locale` / `theme` / `guard_mode` / `workspace` | `update_general` 使用。 |

新增或更新模型：

```json
{
  "action":"upsert_model",
  "model":{
    "provider":"openai",
    "protocol":"openai_chat",
    "model":"gpt-4o-mini",
    "base_url":"https://api.openai.com/v1",
    "context_window":128000,
    "max_output_tokens":8192,
    "strengths":["chat","coding"],
    "subtask_for":["analysis"]
  },
  "api_key":"sk-..."
}
```

激活模型：

```json
{"action":"activate_model","active_model":"openai/gpt-4o-mini"}
```

删除模型：

```json
{"action":"delete_model","model_ref":"openai/gpt-4o-mini","delete_api_key":true}
```

更新通用设置：

```json
{"action":"update_general","locale":"zh","theme":"dark","guard_mode":"ask","workspace":"/path/to/project"}
```

Result：返回更新后的 `ConfigParams`，结构同 `config.get`。

注意：Suna runtime 不提供 `config.list_models`。模型列表拉取本质是 UI 对 provider 的 HTTP 请求，第三方 UI 应自行实现。

---

### 6.5 Memory

#### `memory.list`

用途：查询 user profile memory。

Params：`{}`

Result 示例：

```json
{
  "memories":[
    {"id":"mem_1","content":"用户偏好中文回答","kind":"communication","tags":["preference"],"priority":80,"is_core":true}
  ]
}
```

#### `memory.delete`

Params：

```json
{"id":"mem_1"}
```

Result：

```json
{"deleted":true}
```

成功后会推送 `memory.state`。

#### `memory.clear`

Params：`{}`

Result：

```json
{"deleted_count":3}
```

成功后会推送 `memory.state`。

---

### 6.6 Skill

#### `skill.list`

用途：查询 Skill 状态。

Params：`{}`

Result 示例：

```json
{
  "skills":[
    {"name":"demo","description":"示例技能","enabled":true,"valid":true,"path":"/Users/me/.suna/skills/demo"}
  ]
}
```

#### `skill.set`

用途：启用或禁用 Skill。

Params：

```json
{"name":"demo","enabled":true}
```

Result：

```json
{"status":"ok"}
```

---

### 6.7 MCP

#### `mcp.list`

用途：查询 MCP server 状态。

Params：`{}`

Result 示例：

```json
{
  "servers":[
    {"id":"filesystem","name":"filesystem","transport":"stdio","command":"npx ...","active":true,"configured":true,"tool_count":5}
  ]
}
```

#### `mcp.toggle`

用途：启用或禁用 MCP server。

Params：

```json
{"name":"filesystem","active":false}
```

Result：

```json
{"status":"ok"}
```

#### `mcp.reload`

用途：重载 MCP server。

Params：

```json
{"name":"filesystem"}
```

Result：

```json
{"status":"ok"}
```

---

### 6.8 诊断和 TUI 内部方法

这些 method 当前可用，但第三方 UI v0.2 通常不需要依赖：

| Method | 说明 |
|---|---|
| `daemon.status` | 只读诊断状态，适合 smoke test 或状态面板。 |
| `daemon.stop` | local daemon 管理语义；stdio runtime 下通常关闭 stdin 即可退出，不建议第三方 UI 使用。 |
| `attachment.status` / `attachment.clear` | 官方 TUI 附件缓存管理。第三方 UI 应自行管理上传和缓存，然后向 `agent.sendMessage` 传 image path/url。 |

`daemon.status` Result 示例：

```json
{
  "pid":12345,
  "uptime":"10s",
  "connections":1,
  "triggers":0,
  "agent_status":"idle",
  "provider":"openai",
  "model":"gpt-4o-mini",
  "context_tokens":1200,
  "context_window":128000
}
```

---

## 7. Notification 列表

### 7.1 `agent.delta`

用途：assistant / reasoning 的流式文本增量。

Params：

| 字段 | 说明 |
|---|---|
| `run_id` | 可选，预留给未来持久 Run。 |
| `kind` | `assistant` 或 `reasoning`。 |
| `content` | 本次文本增量。 |

示例：

```json
{"kind":"assistant","content":"你好"}
```

### 7.2 `agent.run`

用途：run 生命周期、retry、失败、取消和恢复能力。

Params：

| 字段 | 说明 |
|---|---|
| `state` | `running`、`retrying`、`done`、`failed`、`cancelled`。 |
| `phase` | 可选：`model`、`tool`、`compact`、`guard`、`ask`、`skill`。 |
| `message` | 可选人类可读说明。 |
| `attempt` / `max_attempts` / `delay_ms` | `retrying` 使用。 |
| `error` | 失败时的结构化 `ModelError`。 |
| `run_error` | 失败前置条件的结构化错误。`no_model_configured` 表示尚未配置模型；`session_model_unavailable` 表示当前 session 的 `model_ref` 已不可用，UI 应引导用户通过 `session.update` 选择模型。 |
| `resume_available` | 失败后是否可调用 `agent.resumeRun`。 |

示例：

```json
{"state":"running","phase":"model"}
{"state":"retrying","phase":"model","attempt":2,"max_attempts":4,"delay_ms":8000}
{"state":"done"}
```

失败示例：

```json
{
  "state":"failed",
  "phase":"model",
  "run_error":{"kind":"session_model_unavailable","model_ref":"openai/gpt-4o-mini"}
}
```

说明：`retrying` 不是终态；不要把它当最终错误显示。只有 `failed` / `cancelled` / `done` 表示当前 run 结束。

### 7.3 `agent.usage`

用途：token、上下文、耗时和速度统计。

示例：

```json
{
  "input_tokens":1000,
  "output_tokens":120,
  "cached_tokens":0,
  "context_tokens":3000,
  "estimated_context_tokens":2900,
  "context_window":128000,
  "duration_ms":2500,
  "tokens_per_sec":48
}
```

### 7.4 `agent.tool_start`

用途：工具开始执行。

Params：

| 字段 | 说明 |
|---|---|
| `id` | tool call id。 |
| `tool` | 工具名。 |
| `params` | 工具参数。 |
| `intent` | 可选，模型声明的调用意图。 |

示例：

```json
{"id":"tool_1","tool":"readfile","params":{"path":"README.md"},"intent":"查看项目说明"}
```

### 7.5 `agent.tool_guard`

用途：工具执行前 Guard 决策状态。

示例：

```json
{"tool_call_id":"tool_1","tool":"exec","risk":"medium","decision":"ask","source":"guard","reason":"命令可能修改文件"}
```

### 7.6 `agent.tool_end`

用途：工具执行结束。`result` 是给 UI 展示的结果，可能被截断；不是模型内部完整 tool result。

示例：

```json
{"id":"tool_1","tool":"readfile","result":"# README\n...","error":false,"result_truncated":false,"result_bytes":1280}
```

### 7.7 `agent.ask_user`

用途：Agent 请求用户输入。UI 应弹窗或表单收集答案，然后调用 `agent.askReply`。

示例：

```json
{"id":"ask_1","question":"请选择目标环境","options":["dev","prod"],"allow_custom":true}
```

回复：

```json
{"jsonrpc":"2.0","id":20,"method":"agent.askReply","params":{"id":"ask_1","answer":"dev"}}
```

### 7.8 `agent.guard_confirm`

用途：高风险工具操作请求用户确认。UI 应展示工具名、参数、风险和原因，然后调用 `agent.guardReply`。

示例：

```json
{"id":"guard_1","tool_call_id":"tool_2","tool":"exec","params":{"command":"rm -rf build"},"risk":"high","reason":"即将删除目录"}
```

允许：

```json
{"jsonrpc":"2.0","id":21,"method":"agent.guardReply","params":{"id":"guard_1","decision":"approve"}}
```

拒绝：

```json
{"jsonrpc":"2.0","id":22,"method":"agent.guardReply","params":{"id":"guard_1","decision":"reject"}}
```

### 7.9 Session notifications

#### `session.user_message`

同一 session 中其他 client 新增 user turn。发送者通常已经本地插入该 turn，因此 daemon 不回发给发送者。

```json
{"session_id":"session_id","parts":[{"type":"text","text":"hello from another window"}]}
```

#### `session.updated`

session metadata/status/client_count 更新，用于刷新 session list、Welcome 和 Handoff 状态。

```json
{"session":{"id":"session_id","cwd":"/repo","model_ref":"openai/gpt-4o-mini","status":"running","client_count":2,"message_count":3}}
```

#### `session.compact_result`

手动 compact 结果或运行状态。

```json
{"running":true}
{"before_tokens":120000,"after_tokens":30000,"context_window":128000,"turns_compressed":8,"summary_tokens":45000,"truncated_outputs":2}
```

### 7.10 State notifications

#### `config.state`

配置变更后的主动状态通知。params 结构同 `config.get` result。

#### `memory.state`

memory 变更后的主动状态通知。params 结构同 `memory.list` result。

#### `skill.load`

Skill load 生命周期通知。

```json
{"name":"demo","status":"loaded"}
```

#### `skill.review`

Skill review 生命周期通知。

```json
{"name":"demo","status":"done","review":"Looks good"}
```

#### `daemon.full_status`

daemon 聚合快照，主要供官方 TUI 使用。第三方 UI 可以用于状态面板，但不应依赖它完成聊天主流程。

---

## 8. 错误结构

JSON-RPC error 使用统一外层：

```json
{
  "jsonrpc":"2.0",
  "id":10,
  "error":{
    "code":-32602,
    "message":"content is required",
    "data":{"kind":"invalid_request"}
  }
}
```

常见 code：

| code | 含义 |
|---:|---|
| `-32700` | parse error，stdin 某一行不是合法 JSON。 |
| `-32600` | invalid request，JSON-RPC 外层结构不合法。 |
| `-32601` | method not found。 |
| `-32602` | invalid params。 |
| `-32603` | internal error。 |
| `-32010` | stdio runtime 未先握手。 |

`error.data`：

| 字段 | 说明 |
|---|---|
| `kind` | 稳定错误分类。UI/SDK 应优先根据它做分支，不要解析 message。 |
| `reason` | 可选机器可读补充原因。 |
| `retryable` | 可选，表示同一请求是否值得重试。 |
| `status_code` | 可选，上游 HTTP/模型错误状态码。 |

常见 `data.kind`：

| kind | 含义 |
|---|---|
| `parse_error` | 输入行不是合法 JSON。 |
| `invalid_request` | 请求或参数无效。 |
| `unsupported_method` | method 不存在。 |
| `unsupported_capability` | 当前 runtime 或协议版本不支持。 |
| `handshake_required` | 未先调用 `runtime.hello`。 |
| `internal_error` | daemon 内部错误。 |

模型请求失败不会作为 `agent.sendMessage` response error 返回，因为 `agent.sendMessage` 只表示“已接收”。已经发起的模型请求失败通过 `agent.run state=failed` 的 `error`（`ModelError`）字段通知；请求前无法满足模型条件时通过 `run_error` 字段通知。客户端必须按这些结构化 kind 分支，不能解析自由文本。

```json
{
  "state":"failed",
  "phase":"model",
  "resume_available":true,
  "error":{
    "kind":"http",
    "message":"Service Unavailable",
    "status_code":503,
    "code":"overloaded",
    "type":"server_error",
    "provider":"anthropic",
    "model":"claude-..."
  }
}
```

---

## 9. UI 层不应该放进 Suna runtime 的能力

为了保持 Suna runtime 边界清晰，下面能力由第三方 UI 自己实现：

| 能力 | 建议 |
|---|---|
| WebSocket / HTTP server | UI 后端自己做，再桥接到 stdio runtime。 |
| 文件浏览 | UI 后端自己读文件系统。 |
| 终端 / PTY | UI 后端自己管理。 |
| 上传缓存 | UI 自己管理文件，然后向 Suna 传 image path/url。 |
| provider 模型列表拉取 | UI 自己请求 provider `/models` 或对应 API。Suna 只保存最终模型配置。 |
| 完整 event replay | v0.2 只提供轻量 current_run snapshot；完整历史事件回放未来单独设计。 |

---

## 10. 最小实现清单

第三方 UI 的最小闭环：

- [ ] spawn `suna runtime --transport stdio`。
- [ ] 设置 child process `cwd` 为用户项目目录。
- [ ] stdout 按行 `JSON.parse`。
- [ ] stderr 只做日志。
- [ ] request pending map：`id -> resolve/reject`。
- [ ] notification dispatcher：`method -> handlers`。
- [ ] 第一条 request 发送 `runtime.hello`。
- [ ] 调用 `config.get` 判断配置状态。
- [ ] 调用 `session.list`。
- [ ] 调用 `session.create` 或 `session.attach`；Join Active 使用 `require_active=true`。
- [ ] 调用 `agent.sendMessage` 发送用户消息。
- [ ] 监听 `agent.delta` / `agent.run` / `agent.tool_*` / `agent.ask_user` / `agent.guard_confirm`。
- [ ] UI 离开当前 session 时调用 `session.detach`；进程退出时 `stdin.end()`。
