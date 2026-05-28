# 05 — 能力系统

Suna 的核心创新：空杯出厂 + 按需学习。能力不是预装的，是使用过程中生长的。

> 当前实现状态: **Basic**
>
> 已实现的是 declarative `SKILL.md` 加载、能力摘要注入、`[LOAD_SKILL: name]` 后完整内容注入，以及能力目录的基础解析/存储。`main.js` QuickJS/WASM runner、MCP client、lifecycle hooks、skill validate/test 闭环和能力市场仍是目标设计，不能按已运行能力理解。

## 能力的本质

```
当前实现: 能力 = 知识 (SKILL.md)
目标设计: 能力 = 知识 (SKILL.md) + 可选的程序 (main.js) + 可选的外部服务 (MCP)
```

## 能力文件格式

每个能力是一个目录：

```
<data-dir>/capabilities/      # 当前默认 ~/.suna/capabilities/
├── vue-style/              # 类型 1: 纯知识
│   └── SKILL.md
├── log-parser/             # 类型 2: 知识 + 程序
│   ├── SKILL.md
│   └── main.js
├── coding-safety/          # 类型 2: 知识 + 程序 (带 lifecycle hooks)
│   ├── SKILL.md
│   └── main.js             # 含 hooks 声明 + execute 函数
├── database/               # 类型 3: 知识 + MCP
│   ├── SKILL.md
│   └── mcp.json
└── ...
```

### SKILL.md 格式

宽松 Markdown，兼容 Claude Code / OpenClaw 生态。元数据支持两种位置：

```markdown
# 浏览器自动化

自动化浏览器操作，包括打开网页、点击、提取数据

当需要抓取网页时:
1. 用 Exec 执行 playwright 脚本
2. 脚本根据任务动态生成
3. 解析 JSON 结果

注意: 等待选择器出现后再操作

---

tools: exec, readfile, writefile
type: script
```

### 解析规则

```go
func ParseSkillMD(content string) *Capability {
    cap := &Capability{}
    body := content

    // 优先检查文件开头的 frontmatter (--- 包裹的 YAML 块)
    if hasFrontmatter(content) {
        fm, body := splitFrontmatter(content)
        cap.Name = fm["name"]
        cap.Tools = fm["tools"]
        cap.Type = fm["type"]
    }

    // 其次检查文件末尾的 --- 分隔符后的元数据
    // (兼容 Claude Code / OpenClaw 格式)
    if hasFooterMeta(body) {
        meta, body := splitFooterMeta(body)
        if cap.Tools == "" { cap.Tools = meta["tools"] }
        if cap.Type == "" { cap.Type = meta["type"] }
    }

    // name 从 H1 标题提取 (# 开头的第一行)
    if cap.Name == "" { cap.Name = extractH1(body) }
    // 全部内容作为 prompt
    cap.Prompt = body

    // type 为空 → 默认 declarative
    // 有 main.js → script
    // 有 mcp.json → mcp

    return cap
}
```

## 三种能力类型

### 类型 1: declarative (纯知识)

只有 SKILL.md。覆盖 ~70% 场景。

```
例: vue-style/
  SKILL.md = "使用 Vue3 <script setup> 语法，用 composables 组织逻辑..."
  agent 读到 → 生成代码时遵循 Vue3 风格
  不需要任何新工具
```

### 类型 2: script (知识 + JS 程序)

有 `main.js`，由 QuickJS 引擎（编译为 WASM，wazero 运行）在 agent 进程内执行。覆盖 ~25% 场景。

当前状态：未接入执行链路。Suna 可以解析能力目录和 `SKILL.md`，但不会执行 `main.js`、注册 execute 函数或运行 JS hooks。

QuickJS 支持 ES2024 完整语法，LLM 生成的 JS 代码无需任何转译或约束。WASM sandbox 提供进程级隔离——内存、计算、IO 全部受控。

#### main.js 结构

```javascript
// 所有 host 函数通过 host.xxx 调用
// QuickJS 支持完整 ES6+，LLM 可自由使用 const/let/箭头函数/模板字符串等

// 可选: lifecycle hooks
function beforeToolUse(tool, params) {
  if (tool === "EditFile" || tool === "WriteFile") {
    const current = host.readFile(params.path);
    host.storagePut(`snapshot:${host.getCurrentTurn()}:${params.path}`, current);
  }
  return { decision: "allow" };
}

// 必需: execute 函数（被 LLM 调用时执行）
function execute(params) {
  const snapshots = host.storageList("snapshot:");
  return { snapshots };
}

module.exports = {
  hooks: {
    PreToolUse: { fn: beforeToolUse, scope: "always" }
  },
  execute
};
```

没有 module.exports.hooks = 纯知识 skill
有 module.exports.hooks = 带行为的 skill

#### Host 函数

```javascript
// 文件
host.readFile(path)              → string
host.writeFile(path, content)    → void

// 执行
host.exec(command)               → { stdout, stderr, exitCode }

// 持久存储 (skill 隔离的命名空间)
host.storageGet(key)             → string | null
host.storagePut(key, value)      → void
host.storageDelete(key)          → void
host.storageList(prefix)         → string[]

// 上下文
host.getConfig(key)              → string | null
host.getCurrentTurn()            → number
host.getSessionId()              → string
host.getWorkingDir()             → string

// 交互
host.askUser(question)           → string
host.log(message)                → void
```

所有 host 函数经过 Guard 审查（文件操作和命令执行）。

#### QuickJS + wazero

```
架构:
  QuickJS (Fabrice Bellard 的轻量 JS 引擎)
  → 编译为 WASM (~500KB)
  → 嵌入 Suna 二进制
  → wazero (纯 Go WASM 运行时) 执行

优势:
  ✅ ES2024 完整支持 — LLM 无需约束，自由生成任意 JS
  ✅ WASM sandbox 隔离 — 内存/计算/IO 全部受控
  ✅ 无转译器 — 不存在 ES5.1 兼容性地雷
  ✅ 纯 Go — wazero 无 CGO，跨平台一致
  ✅ host 函数通过 WASM import 机制注入 — 安全可控

体积:
  QuickJS WASM: ~500KB
  wazero runtime: ~2MB
  总增量: ~2.5MB

性能:
  skill 脚本都是微秒级操作 (条件判断 + 调 host 函数)
  QuickJS WASM 完全够用
```

### 类型 3: mcp (知识 + 外部服务)

有 `mcp.json`，覆盖 ~5% 场景。

当前状态：未接入 MCP client。`mcp.json` 是目标设计格式，当前不会把 MCP server tools 注册到 agent。

```json
{
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-postgres"],
    "env": {
        "DATABASE_URL": "postgresql://user:pass@localhost/mydb"
    }
}
```

加载流程：读 mcp.json → mcp-go 创建 Client → 连接 Server → 获取 tools → 注册到 agent。

## Lifecycle Hooks

skill 的 main.js 可以声明 hooks，自动拦截 agent 的操作。

当前状态：未实现。core 目前没有执行 Shell hooks 或 Skill hooks。

### 4 个 Hook 点

```
Agent Loop 中的决策点:

  1. 感知信号到达 → OnSignal(signal) → {handle, ignore, transform}
  2. 构建 LLM 请求时 → PreLLM(messages) → messages
  3. 工具执行前 → PreToolUse(tool, params) → {allow, reject, modify}
  4. 工具执行后 → PostToolUse(tool, params, result) → void
```

### Hook Scope

```
scope: "always"    → 安装即激活，不经过 LLM 判断 (安全/保护类)
scope: "matched"   → LLM 加载了该 SKILL.md 后才执行 (增强类)
```

### 执行顺序

```
同一个事件多个 hook 时:

1. Shell hooks (config.toml 里定义的) → 先执行
2. Skill hooks (main.js 里定义的)    → 按注册顺序执行

规则:
  - 任何 hook 返回 reject → 立即停止，不执行后续 hook 和工具
  - 任何 hook 返回 modify → 修改后的参数传给下一个 hook
  - 全部 allow → 执行工具
```

### Hook 示例

```javascript
// coding-safety skill: 快照 + workspace 边界
function beforeToolUse(tool, params) {
  if (tool === "EditFile" || tool === "WriteFile") {
    const workspace = host.getConfig("workspace.root");
    if (workspace && !params.path.startsWith(workspace)) {
      return { decision: "reject", reason: "文件在 workspace 外" };
    }
    const current = host.readFile(params.path);
    host.storagePut(`snapshot:${host.getCurrentTurn()}:${params.path}`, current);
  }
  return { decision: "allow" };
}

// auto-format skill: 写文件后自动格式化
function afterToolUse(tool, params, result) {
  if (tool === "WriteFile") {
    if (params.path.endsWith(".go")) {
      host.exec(`gofmt -w ${params.path}`);
    }
    if (params.path.endsWith(".ts")) {
      host.exec(`npx prettier --write ${params.path}`);
    }
  }
}

// cost-guard skill: 预算警告
function beforeLLM(messages) {
  const spent = parseFloat(host.storageGet("cost:today") || "0");
  if (spent > 5.0) {
    messages.push({ role: "system", content: `警告: 今日已花费 $${spent}` });
  }
  return messages;
}

module.exports = {
  hooks: {
    PreToolUse: { fn: beforeToolUse, scope: "always" },
    PostToolUse: { fn: afterToolUse, scope: "always" },
    PreLLM: { fn: beforeLLM, scope: "always" }
  },
  execute: (params) => { /* ... */ }
};
```

## 能力加载流程

```
Agent 启动
  │
  ▼
扫描默认数据目录下的 capabilities/
  │
  ├── 每个 skill 目录:
  │   └── 读 SKILL.md → 解析 name 和 prompt 摘要
  │
  ├── 每轮对话:
  │   ├── LLM 看到所有 SKILL.md 摘要 → 自行判断加载哪个
  │   └── 加载 = 完整 SKILL.md 注入后续 system prompt
  │
  └── 用户不配置能力选择，LLM 自主决定
```

### 能力提示词注入格式

```
System Prompt 中的能力部分分两层注入:

第一层: 摘要列表 (始终注入，让 LLM 知道有哪些能力可用)

  ## 可用能力
  - vue-style: 使用 Vue3 <script setup> 语法，用 composables 组织逻辑...
  - log-parser: 解析应用日志，提取 ERROR 和关键事件...
  - database: 通过 MCP 连接 PostgreSQL 数据库...

  格式: "- {name}: {SKILL.md 前 200 字}"
  位置: System Prompt 的 "当前能力" 部分 (01-architecture.md:367)

第二层: 完整 SKILL.md (LLM 判断需要时注入)

  LLM 在回复中输出特殊标记加载能力:
    → 当 LLM 判断某个能力与当前任务相关时，在回复中包含:
       [LOAD_SKILL: skill-name]
    → 内核拦截此标记 → 将完整 SKILL.md 注入到下一条 system 消息中
    → 当前只注入 prompt，不激活 hooks
    → 标记从 LLM 输出中移除，用户不可见
  已加载的能力在后续轮次中保持注入，不需要重复加载

去重:
  同一能力只注入一次完整内容
  如果 system prompt 中已有该能力的完整内容 → 跳过
```

## Skill 验证机制

skill 写完后必须通过验证才能上线。验证由 Suna 内核执行，agent 自动闭环。

当前状态：未实现验证命令和自动修复闭环。以下是目标设计。

### 验证流程

```
1. 语法检查 → QuickJS 解析 main.js，不执行
   失败: "第 5 行语法错误" → agent 自动修复

2. 导出检查 → module.exports 必须有合法结构
   失败: "缺少 module.exports" → agent 自动修复

3. host 函数检查 → 调用的 host.xxx 是否存在
   失败: "host.readFil 不存在，请用 host.readFile" → agent 自动修复

4. 沙箱试运行 → 用模拟输入执行，host 函数返回 mock 值
   输入: { tool: "EditFile", params: { path: "/tmp/test.go" } }
   检查: PreToolUse 返回值是否是 { decision: "allow"|"reject"|"modify" }
   检查: execute 返回值是否是可序列化的对象
   失败: "PreToolUse 应返回 { decision: 'allow' } 格式" → agent 自动修复

5. 副作用隔离 → 验证阶段 host.storagePut 写临时空间，host.exec 不执行
   验证完成后清理临时数据
```

### 验证闭环

```
Agent 生成 main.js
  │
  ▼
Suna 内核 ValidateSkill()
  │
  ├── 有错误 → 返回错误信息给 agent → agent 修复 → 重新验证
  │   最多 5 轮，超过则: "这个能力我学不会，请帮我看看"
  │
  └── 通过 → "验证通过，能力已就绪" → 用户确认 → 保存
```

### 用户手动验证

```
/skill validate coding-safety              → 重新验证某个 skill
/skill test coding-safety --input '...'    → 用指定输入测试
/skill list                                 → 显示所有 skill + 验证状态

/skill test coding-safety --input '{"tool":"EditFile","params":{"path":"/etc/passwd"}}'
→ { decision: "reject", reason: "outside workspace" }
```

## 能力学习流程

### 触发条件

```
agent 检测到以下信号之一:
  1. 同类任务连续 3 次以上，且用户每次都在纠正
  2. 某类任务反复出现，但没有对应能力
  3. 用户主动说 "你能不能记住这个" / "以后都这样做"
```

### 学习流程

```
Step 1: Agent 判断是否需要学习
  "我注意到你经常让我做 XXX，要我把这个总结成能力吗？"
  AskUser → 用户确认/拒绝

Step 2: Agent 生成能力
  a) 总结对话中的知识 → 生成 SKILL.md
  b) 判断是否需要程序:
     - 纯知识型 → 只生成 SKILL.md
     - 需要确定性逻辑 → 同时生成 main.js
  c) LLM 直接输出 Markdown/JS

Step 3: 验证 (有 main.js 时自动执行)
  Suna 内核 ValidateSkill() → 有错误 → agent 修复 → 重新验证
  最多 5 轮自动修复，超过则请求用户帮助

Step 4: 用户确认
  TUI 显示能力摘要 → [确认保存] [修改] [取消]
  确认 → 保存到默认数据目录下的 capabilities/xxx/

Step 5: 后续优化
  使用中发现不够好 → 自省检测 → 建议更新 → 用户确认后覆盖
```

### 三条学习路径

```
路径 A: Agent 自学
  Agent 从对话中提取知识/程序 → 自动生成 → 验证 → 用户确认

路径 B: 用户教学
  用户一步步演示: "你先看日志，然后找 ERROR，然后告诉我"
  agent 观察操作 → 提取模式 → 生成 SKILL.md + main.js
  验证 → 用户确认
  非编程用户通过自然语言教 agent

路径 C: 社区获取
  下载 skill 目录 → 验证 → 安装
  (远期: 能力市场)
```

### 用户教学示例

```
用户: "帮我盯着 app.log，出现 ERROR 就通知我"

Agent: "好的，我需要学一个能力。你能教我怎么做吗？"

用户: "你先看 app.log 最后 10 行"
Agent: [Exec: tail -n 10 app.log] → 看到日志内容

用户: "找到包含 ERROR 的行"
Agent: [Exec: grep ERROR app.log] → 找到 3 行

用户: "以后每隔 30 秒检查一次，有 ERROR 就通知我"
Agent: "我学会了:
  监控 app.log，每 30 秒检查，发现 ERROR 通知你。
  对吗？"

用户: "对"
→ Agent 生成 SKILL.md + main.js
→ Suna 内核验证通过
→ 保存到 capabilities/log-monitor/
→ 用户无感，全程没写代码
```

## 能力的共享

```
能力是一个目录，天然可分享:
  - 压缩成 .zip/.tar.gz → 发给别人
  - Git 仓库管理 → 版本控制
  - 能力市场 (远期)

安装方式:
  - 解压到默认数据目录下的 capabilities/xxx/
  - TUI: /skill load ./xxx/
  - 市场安装后自动验证
```

能力根目录由 `internal/config/paths.go` 的 `DefaultCapabilitiesDir()` / `Config.CapabilitiesDir()` 派生，当前默认展开为 `~/.suna/capabilities`。
