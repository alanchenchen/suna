# Suna stdio runtime 接入指南

本文面向想开发第三方 UI、桌面端、IDE 插件或本地 Web 服务的开发者。第一阶段官方对外接入方式是：启动一个 headless Suna runtime，并通过 stdio 与它通信。

```bash
suna runtime --transport stdio
```

必须显式传入 `--transport stdio`。只运行 `suna runtime` 会打印 usage 并退出，避免后续新增 transport 时让入口语义变得隐式。

这个命令会在前台启动一个单进程 Suna runtime。父进程通过 JSON-RPC 2.0 + NDJSON 与它通信：

- `stdin`：客户端写入 JSON-RPC request，一行一个 JSON。
- `stdout`：Suna 输出 JSON-RPC response / notification，一行一个 JSON。
- `stderr`：人类可读诊断信息和错误提示。

> 重要：`stdout` 只会输出协议消息。第三方客户端应只解析 stdout，不要从 stderr 解析协议。

runtime 会使用进程当前工作目录作为项目工作区。第三方程序 spawn Suna 时，应把 child process 的 `cwd` 设置为用户要操作的项目目录。

> 注意：runtime v0 不支持多个 Suna 进程同时写入同一个数据目录。第三方 UI 应独占启动 `suna runtime --transport stdio`；不要同时打开官方 TUI 和第三方 runtime 使用同一套 `~/.suna` 数据。

---

## 1. 启动 runtime

Node.js 示例：

```ts
import { spawn } from "node:child_process"

const proc = spawn("suna", ["runtime", "--transport", "stdio"], {
  cwd: "/path/to/project",
  stdio: ["pipe", "pipe", "inherit"],
})
```

如果同时打开官方 TUI 和第三方 runtime 并使用同一套数据目录，当前版本不保证状态一致性；这不是 v0 支持场景。

---

## 2. 必须先握手：`runtime.hello`

stdio runtime 要求第一条 request 必须是：

```json
{"jsonrpc":"2.0","id":1,"method":"runtime.hello","params":{"protocol_version":"0.1","client":{"name":"example-ui","version":"0.1.0","type":"node"}}}
```

返回示例：

```json
{"jsonrpc":"2.0","id":1,"result":{"protocol_version":"0.1","runtime_version":"0.5.0","transport":"stdio","capabilities":{"agent":true,"streaming":true,"tools":true,"guard":true,"ask_user":true,"session":true,"config":true,"memory":true,"skills":true,"mcp":true},"content_sources":{"text":true,"image_path":true,"image_url":true}}}
```

如果未握手就调用其它 method，会返回结构化错误：

```json
{"jsonrpc":"2.0","id":2,"error":{"code":-32010,"message":"runtime.hello is required before other methods","data":{"kind":"handshake_required"}}}
```

---

## 3. 通信模型

Suna Protocol 统一遵循：

```txt
method request  = 客户端主动请求，必须返回 result 或 error
notification    = daemon 主动推送的异步事件或状态变化
```

不要把 method response 当成 notification。客户端应同时实现：

1. `request(method, params)`：通过 `id` 等待对应 response。
2. `on(method, handler)`：持续监听没有 `id` 的 notification。

---

## 4. 常用 method

### Runtime

```txt
runtime.hello
```

### Agent

```txt
agent.sendMessage
agent.cancel
agent.resumeRun
agent.askReply
agent.guardReply
```

### Session

```txt
session.new
session.restore
session.compact
session.usage
```

### Config

```txt
config.get
config.set
```

### Memory

```txt
memory.list
memory.delete
memory.clear
```

### Skill

```txt
skill.list
skill.set
```

### MCP

```txt
mcp.list
mcp.toggle
mcp.reload
```

---

## 5. 常用 notification

```txt
agent.delta
agent.run
agent.usage
agent.tool_start
agent.tool_guard
agent.tool_end
agent.ask_user
agent.guard_confirm
session.restore_message
session.restore_status
session.compact_result
config.state
memory.state
skill.load
skill.review
```

其中：

- `agent.delta`：assistant / reasoning 的流式文本增量。
- `agent.run`：run 生命周期，例如 running / retrying / done / failed / cancelled。
- `agent.usage`：token、上下文窗口、耗时和速度统计。
- `agent.tool_*`：工具开始、Guard 状态、工具结束。
- `agent.ask_user`：Agent 请求用户输入。
- `agent.guard_confirm`：高风险工具操作请求用户确认。

---

## 6. 发送文本消息

```json
{"jsonrpc":"2.0","id":2,"method":"agent.sendMessage","params":{"parts":[{"type":"text","text":"hello"}]}}
```

请求成功后，response 只表示任务已被接收：

```json
{"jsonrpc":"2.0","id":2,"result":{"status":"processing"}}
```

实际回复通过 notification 持续推送，例如：

```json
{"jsonrpc":"2.0","method":"agent.delta","params":{"kind":"assistant","content":"你好"}}
{"jsonrpc":"2.0","method":"agent.run","params":{"state":"done"}}
```

---

## 7. 发送图片

Suna runtime 不负责第三方 UI 的上传管理。第三方 UI 可以自己维护文件，然后把本地路径或 URL 传给 Suna。

### 图片路径

```json
{"jsonrpc":"2.0","id":3,"method":"agent.sendMessage","params":{"parts":[{"type":"text","text":"分析这张图片"},{"type":"image","source":{"kind":"path","path":"/absolute/path/image.png","mime_type":"image/png"}}]}}
```

### 图片 URL

```json
{"jsonrpc":"2.0","id":4,"method":"agent.sendMessage","params":{"parts":[{"type":"text","text":"分析这张图片"},{"type":"image","source":{"kind":"url","url":"https://example.com/image.png","mime_type":"image/png"}}]}}
```

---

## 8. 处理 askuser

收到：

```json
{"jsonrpc":"2.0","method":"agent.ask_user","params":{"id":"ask_1","question":"请选择目标","options":["A","B"],"allow_custom":true}}
```

回复：

```json
{"jsonrpc":"2.0","id":5,"method":"agent.askReply","params":{"id":"ask_1","answer":"A"}}
```

---

## 9. 处理 Guard 确认

收到：

```json
{"jsonrpc":"2.0","method":"agent.guard_confirm","params":{"id":"guard_1","tool":"exec","risk":"high","reason":"即将执行命令","params":{"command":"..."}}}
```

允许：

```json
{"jsonrpc":"2.0","id":6,"method":"agent.guardReply","params":{"id":"guard_1","decision":"approve"}}
```

拒绝：

```json
{"jsonrpc":"2.0","id":7,"method":"agent.guardReply","params":{"id":"guard_1","decision":"deny"}}
```

---

## 10. 错误结构

JSON-RPC error 会包含结构化 `data`：

```json
{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"content is required","data":{"kind":"invalid_request"}}}
```

常见 `data.kind`：

```txt
handshake_required
invalid_request
unsupported_method
unsupported_capability
internal_error
```

---

## 11. 最小客户端实现要点

第三方客户端至少需要实现：

1. 启动 `suna runtime --transport stdio`。
2. 按行读取 stdout，并 `JSON.parse`。
3. 如果消息有 `id`，把它匹配到 pending request。
4. 如果消息没有 `id` 但有 `method`，当成 notification 分发。
5. 第一条 request 发送 `runtime.hello`。
6. 调用 `agent.sendMessage` 后持续监听 `agent.delta`、`agent.run`、`agent.tool_*`、`agent.ask_user`、`agent.guard_confirm`。

这就是开发第三方 UI 的最小闭环。

---

## 12. Node.js 最小 SDK Demo

下面是一段可直接运行的最小客户端。它只做只读 smoke test：启动 runtime、发送 `runtime.hello`、再调用 `daemon.status`。不会发送用户消息、不会触发模型请求、不会执行工具。

保存为 `suna-stdio-smoke.mjs`，然后运行：

```bash
node suna-stdio-smoke.mjs /path/to/project
```

```js
import { spawn } from "node:child_process";
import { createInterface } from "node:readline";

class SunaRuntime {
  constructor({ bin = "suna", cwd = process.cwd() } = {}) {
    this.nextID = 1;
    this.pending = new Map();
    this.handlers = new Map();
    this.proc = spawn(bin, ["runtime", "--transport", "stdio"], {
      cwd,
      stdio: ["pipe", "pipe", "pipe"],
    });

    const stdout = createInterface({ input: this.proc.stdout });
    stdout.on("line", (line) => this.#handleLine(line));

    // stderr 只用于人类诊断和日志，不要从这里解析协议消息。
    this.proc.stderr.on("data", (chunk) => {
      process.stderr.write(`[suna] ${chunk}`);
    });

    this.proc.on("error", (err) => this.#rejectAll(err));
    this.proc.on("close", (code, signal) => {
      this.#rejectAll(new Error(`suna runtime closed: code=${code} signal=${signal}`));
    });
  }

  async hello() {
    return this.request("runtime.hello", {
      protocol_version: "0.1",
      client: { name: "node-smoke", version: "0.1.0", type: "node" },
    });
  }

  request(method, params = {}, { timeoutMs = 30_000 } = {}) {
    const id = this.nextID++;
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

    let msg;
    try {
      msg = JSON.parse(line);
    } catch (err) {
      console.error("invalid JSON from Suna stdout:", line);
      return;
    }

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

const cwd = process.argv[2] ?? process.cwd();
const suna = new SunaRuntime({ cwd });

try {
  const hello = await suna.hello();
  console.log("hello:", hello);

  const status = await suna.request("daemon.status", {});
  console.log("status:", status);
} finally {
  suna.close();
}
```

如果要发送真实用户消息，最小流程是在 `hello()` 之后注册 notification handler，再调用 `agent.sendMessage`：

```js
let assistant = "";

suna.on("agent.delta", (params) => {
  if (params.kind === "assistant") assistant += params.content;
});

suna.on("agent.run", (params) => {
  if (params.state === "done") console.log("assistant:", assistant);
  if (params.state === "failed") console.error("run failed:", params.error ?? params.message);
});

await suna.request("agent.sendMessage", {
  parts: [{ type: "text", text: "hello" }],
});
```

UI 层应把 `agent.sendMessage` 的 response 只当成“已接收”，真正的文本、工具状态、askuser、guard 和终态都来自后续 notification。
