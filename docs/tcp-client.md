# Suna TCP 客户端接入指南

> **目标**：让第三方桌面端、IDE 插件、本地 Web gateway、脚本或 AI 编码代理，在不 import Suna Go 包的前提下，接入与官方 TUI **同一份** Suna daemon、session、run 与 handoff。

Suna 只有一个共享 daemon：

```text
官方 TUI ── Unix socket / Named Pipe ─┐
第三方 UI ── TCP NDJSON ──────────────┼─ 同一个 Suna daemon
IDE 插件 ── TCP NDJSON ──────────────┤  ├─ sessions / handoff
本地 gateway ─ TCP NDJSON ───────────┘  ├─ memory / Skill / MCP
                                        └─ Agent / Guard / tools
```

第三方客户端**不是**启动一份私有 Agent runtime，而是连接已有 daemon。因此它可以 attach 到官方 TUI 正在使用的 session、观察 stream，并在 owner 离开且自己仍 attached 时参与 handoff。

---

## 快速接入清单

按以下顺序实现即可：

1. 执行 `suna serve --json`；
2. 从 stdout JSON 读取 `tcp_endpoint`；
3. 建立 TCP 长连接；
4. 按 NDJSON 逐行收发 JSON-RPC；
5. 第一条 request 必须是 `runtime.hello`；
6. 调用 `session.list`，再 `session.create` 或 `session.attach`；
7. 调用 `agent.sendMessage`，并持续消费 notification；
8. 离开当前会话时调用 `session.detach`；关闭客户端时关闭 TCP connection；
9. 若 TCP 连接失败，重新执行 `suna serve --json` 后再连接。

完整 schema 以 [protocol.md](protocol.md) 和 `internal/protocol` 为准；本文优先说明客户端实现路径与必须遵守的行为。

---

## 1. 启动或发现 daemon

执行：

```bash
suna serve --json
```

成功时，stdout 只输出一个 JSON 对象：

```json
{
  "status": "ready",
  "pid": 12345,
  "tcp_endpoint": "127.0.0.1:7632"
}
```

### 客户端必须遵守的启动规则

| 项目 | 规则 |
|---|---|
| 成功判断 | `exit code == 0`，且 stdout 可解析为 JSON。 |
| endpoint | 使用返回的 `tcp_endpoint`，**不要硬编码** `127.0.0.1:7632`。 |
| stderr | 仅用于人类诊断，客户端不要解析 stderr 协议。 |
| 默认端口冲突 | Suna 可回退到随机 loopback 端口，返回的 endpoint 才是权威地址。 |
| `--listen` | 可首次启动时传入 loopback 地址，例如 `suna serve --listen 127.0.0.1:9000 --json`。 |
| 已有 daemon | `suna serve --json` 返回已有 daemon 的实际 endpoint，不会重启 session 或 run。 |
| reconnect | daemon 在所有 local/TCP client 离开**约 2 秒**后会按 idle-exit 自动退出；连接失败时再次执行 `suna serve --json`。 |

`serve` 不代表永久常驻 server，也不需要第三方 UI 持有一个额外的 Suna 子进程。它只保证 daemon 当前可连接。

---

## 2. 安全边界

当前 TCP transport：

```text
仅 loopback：127.0.0.1 / ::1
无内置认证
无 TLS
```

这意味着它适用于**可信本机进程**。不要将 Suna 直接暴露到局域网或公网；当前版本会拒绝 `0.0.0.0` 等非 loopback `--listen` 地址。

若未来需要远程 Web 部署，应通过带认证的本地 gateway 或专门设计的认证 transport，而不是直接转发当前 TCP listener。

---

## 3. 传输与 JSON-RPC 规则

TCP 是一条长期连接，framing 为 **NDJSON**：

```text
一行 = 一条完整 JSON 消息
每行必须以 \n 结束
```

### 3.1 Client request

```json
{"jsonrpc":"2.0","id":1,"method":"session.list","params":{"active_only":false}}
```

| 字段 | 要求 |
|---|---|
| `jsonrpc` | 固定为 `"2.0"`。 |
| `id` | 必须是整数；response 会携带相同 ID。 |
| `method` | 例如 `session.attach`、`agent.sendMessage`。 |
| `params` | 建议总是传对象；无参数时传 `{}`。 |

当前不支持客户端 notification 和 string ID。

### 3.2 Daemon response

成功 response：

```json
{"jsonrpc":"2.0","id":1,"result":{"sessions":[]}}
```

失败 response：

```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"content is required","data":{"kind":"invalid_request"}}}
```

有 `id` 的消息是 response，应与 pending request 匹配；response 只会有 `result` 或 `error` 之一。

### 3.3 Daemon notification

```json
{"jsonrpc":"2.0","method":"agent.delta","params":{"kind":"assistant","content":"你好"}}
```

没有 `id`、有 `method` 的消息是 notification。它由 daemon 主动发送，客户端必须与 response **独立分发**。

特别注意：`agent.sendMessage` 的 response 只表示消息已接收；模型文本、工具状态与 run 结果全部来自后续 notification。

---

## 4. 第一条 request：`runtime.hello`

TCP client 连接后，必须先发送：

```json
{"jsonrpc":"2.0","id":1,"method":"runtime.hello","params":{"protocol_version":"0.2","client":{"name":"my-ui","version":"1.0.0","type":"desktop"}}}
```

建议字段：

| 字段 | 是否必填 | 说明 |
|---|---:|---|
| `protocol_version` | 否 | 当前公开版本为 `"0.2"`；推荐始终传入。 |
| `client.name` | 否 | 客户端名称，用于诊断。 |
| `client.version` | 否 | 客户端版本。 |
| `client.type` | 否 | 例如 `desktop`、`ide`、`web_gateway`、`script`。 |

成功后可得到：

```json
{
  "protocol_version":"0.2",
  "runtime_version":"v0.x.x",
  "transport":"tcp",
  "capabilities":{"agent":true,"session":true,"handoff":true},
  "content_sources":{"text":true,"image_path":true,"image_url":true},
  "limits":{"max_tool_result_bytes":16384}
}
```

未握手就调用其他 method 会收到 `handshake_required` 错误。服务端也会关闭长期未完成握手的连接。

---

## 5. 最小会话流程

### 5.1 列出 session

```json
{"jsonrpc":"2.0","id":2,"method":"session.list","params":{"active_only":false}}
```

### 5.2 创建 session

`cwd` 必填，它决定 session 的默认工作区与相对路径边界：

```json
{"jsonrpc":"2.0","id":3,"method":"session.create","params":{"cwd":"/absolute/project/path","title":"Project work"}}
```

创建成功后，当前 TCP connection 会自动 attach 到新 session，并返回 `SessionSnapshot`。

### 5.3 Attach 已有 session

```json
{"jsonrpc":"2.0","id":4,"method":"session.attach","params":{"session_id":"SESSION_ID","require_active":false}}
```

如果你的 UI 是“加入正在进行的 run”，传入：

```json
{"session_id":"SESSION_ID","require_active":true}
```

Attach response 中的 `current_run`、`assistant_buffer` 与 `reasoning_buffer` 用于恢复正在进行的展示。

### 5.4 发送消息

```json
{"jsonrpc":"2.0","id":5,"method":"agent.sendMessage","params":{"parts":[{"type":"text","text":"Inspect this repository and explain the architecture."}]}}
```

随后重点消费：

| Notification | 客户端应做什么 |
|---|---|
| `agent.run` | 更新 running / retrying / done / failed / cancelled 状态。 |
| `agent.delta` | 追加 assistant 或 reasoning 流式文本。 |
| `agent.usage` | 更新 token、context、耗时统计。 |
| `agent.tool_start` / `agent.tool_guard` / `agent.tool_end` | 展示工具生命周期和结果。 |
| `agent.ask_user` | 展示提问，并由允许回复的 client 回答。 |
| `agent.guard_confirm` | 展示安全确认，并由允许回复的 client 决策。 |
| `agent.interaction_resolved` | 移除对应 AskUser / Guard UI。 |
| `session.updated` | 更新 session 元数据与 active 状态。 |

### 5.5 Detach

不再使用当前 session 时：

```json
{"jsonrpc":"2.0","id":6,"method":"session.detach","params":{}}
```

持久化 history 不会被删除。

当最后一个 attached client 离开正在 running/waiting 的 session 时，daemon 会取消 run、清理 pending interaction，并在 runtime 变为 idle 后卸载其内存状态。后续 attach 仍可从持久化数据恢复 session。

---

## 6. 多客户端与 handoff

多个客户端可以 attach 到同一个 session：

```text
Client A（官方 TUI）发起 run
Client B（IDE）attach 同一 session
→ B 可观察 stream、tool、run state
→ A 离开，但 B 仍 attached
→ B 可接手 AskUser 或 Guard confirmation
```

规则：

- 每个 session 同一时间只允许一个 active run；
- 当前 run 有一个 owner；其他 attached client 可观察；
- owner 离开且还有其他 attached client 时，等待中的 interaction 可 handoff；
- 回复 AskUser/Guard 的 client 必须仍 attach 到该 session，且必须被 daemon 允许。

回复 AskUser：

```json
{"jsonrpc":"2.0","id":7,"method":"agent.askReply","params":{"id":"ASK_ID","answer":"Continue"}}
```

回复 Guard：

```json
{"jsonrpc":"2.0","id":8,"method":"agent.guardReply","params":{"id":"GUARD_ID","decision":"approve"}}
```

`decision` 只能是 `approve` 或 `reject`。

---

## 7. 可直接复用的 Node.js 最小客户端

下面的示例负责：启动/发现 daemon、TCP 连接、NDJSON 分帧、request/response 匹配、notification 分发与握手。

```js
import { execFile } from "node:child_process";
import { createConnection } from "node:net";
import { createInterface } from "node:readline";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export class SunaClient {
  static async connect({ bin = "suna" } = {}) {
    const { stdout } = await execFileAsync(bin, ["serve", "--json"]);
    const { tcp_endpoint } = JSON.parse(stdout);
    const [host, portText] = tcp_endpoint.split(":");
    const socket = createConnection({ host, port: Number(portText) });
    await new Promise((resolve, reject) => {
      socket.once("connect", resolve);
      socket.once("error", reject);
    });

    const client = new SunaClient(socket);
    await client.request("runtime.hello", {
      protocol_version: "0.2",
      client: { name: "example-ui", version: "1.0.0", type: "node" },
    });
    return client;
  }

  constructor(socket) {
    this.socket = socket;
    this.nextID = 1;
    this.pending = new Map();
    this.handlers = new Map();

    createInterface({ input: socket }).on("line", (line) => {
      if (!line.trim()) return;
      this.#handle(JSON.parse(line));
    });
    socket.on("error", (err) => this.#rejectAll(err));
    socket.on("close", () => this.#rejectAll(new Error("Suna TCP connection closed")));
  }

  request(method, params = {}, timeoutMs = 30_000) {
    const id = this.nextID++;
    const payload = { jsonrpc: "2.0", id, method, params };
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`request timeout: ${method}`));
      }, timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      this.socket.write(`${JSON.stringify(payload)}\n`, (err) => {
        if (!err) return;
        clearTimeout(timer);
        this.pending.delete(id);
        reject(err);
      });
    });
  }

  on(method, handler) {
    const handlers = this.handlers.get(method) ?? [];
    handlers.push(handler);
    this.handlers.set(method, handlers);
  }

  close() {
    this.socket.end();
  }

  #handle(message) {
    if (Number.isInteger(message.id)) {
      const pending = this.pending.get(message.id);
      if (!pending) return;
      clearTimeout(pending.timer);
      this.pending.delete(message.id);
      if (message.error) {
        const err = new Error(message.error.message);
        err.code = message.error.code;
        err.data = message.error.data;
        pending.reject(err);
      } else {
        pending.resolve(message.result);
      }
      return;
    }
    if (message.method) {
      for (const handler of this.handlers.get(message.method) ?? []) {
        handler(message.params);
      }
    }
  }

  #rejectAll(error) {
    for (const [id, pending] of this.pending) {
      clearTimeout(pending.timer);
      pending.reject(error);
      this.pending.delete(id);
    }
  }
}
```

使用示例：

```js
const suna = await SunaClient.connect();
try {
  const sessions = await suna.request("session.list", { active_only: false });
  const snapshot = await suna.request("session.create", {
    cwd: process.cwd(),
    title: "Example",
  });

  let answer = "";
  suna.on("agent.delta", ({ kind, content }) => {
    if (kind === "assistant") answer += content;
  });
  suna.on("agent.run", ({ state, error, message }) => {
    if (state === "done") console.log(answer);
    if (state === "failed") console.error(error ?? message);
  });

  await suna.request("agent.sendMessage", {
    parts: [{ type: "text", text: "Explain this project." }],
  });
} finally {
  await suna.request("session.detach", {}).catch(() => {});
  suna.close();
}
```

> 上例假设 endpoint 是 IPv4。若你的客户端要支持 `[::1]:port`，请使用语言标准库的 host/port parser，而不是手动 `split(":")`。

---

## 8. 交给 AI 编码代理的最小任务描述

如果要让 AI agent 实现一个 Suna client，可直接提供以下约束：

```text
实现一个 Suna TCP JSON-RPC client：
1. 执行 `suna serve --json`，解析 stdout 的 tcp_endpoint；
2. 使用长期 TCP connection 和 NDJSON，一行一条 JSON；
3. 第一条 request 必须是 runtime.hello，protocol_version 为 0.2；
4. 用整数 request ID 和 pending map 匹配 response；
5. 独立分发无 ID 的 daemon notification；
6. 先 session.list，再 session.create 或 session.attach；
7. agent.sendMessage 的 response 只表示 accepted；真实输出来自 agent.delta / agent.run；
8. UI 离开 session 时调用 session.detach；断线后重新 serve 并重连；
9. 不要访问 Suna 内部文件、SQLite、Agent 或 Go 包；所有业务交互走 protocol。
```

协议字段、完整方法与错误语义请见 [protocol.md](protocol.md)。
