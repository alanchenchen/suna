# 05 — Skills 与 MCP

Suna 使用两个独立机制：

```text
Skill = 用户信任后的通用 Agent Skill 包，用来教 Suna 如何完成某类任务
MCP   = 外部工具 / 资源 / 服务接入层，用 config.toml 配置
```

二者分离：Skill 不内嵌 MCP server 配置；MCP 不承担 Skill 的任务说明职责。

## 设计原则

```text
1. 兼容主流 Agent Skill 目录结构，不发明 Suna 专属格式。
2. Skill 操作以自然语言对话为主，TUI `/skills` 只做简单管理入口。
3. Suna 在导入 / 生成 / 更新 Skill 时做 check，并把风险原因解释给用户。
4. 用户 enabled 后，Skill 作为用户信任的能力包可被 LLM 使用。
5. 不做复杂 Skill sandbox，不单独设计 script 权限系统；运行时仍走现有工具与 Guard。
6. config.toml 只记录用户对某个 Skill 内容版本的信任结果。
7. MCP 独立放在 config.toml，作为 daemon runtime 的工具接入能力。
```

## Skill 目录

全局 Skill 目录固定为：

```text
~/.suna/skills/<skill-name>/SKILL.md
```

不提供用户可配置的 Skill directory，避免增加普通用户心智负担。

兼容通用目录式 Skill：

```text
~/.suna/skills/code-review/
├── SKILL.md
├── references/     # 可选，参考文档
├── examples/       # 可选，示例
├── assets/         # 可选，模板/素材
└── scripts/        # 可选，辅助脚本
```

Suna 只要求 `SKILL.md` 存在。其他目录只是辅助资源，不会自动注册为新工具。

## SKILL.md 字段

Suna 只认主流通用字段，核心是：

```markdown
---
name: code-review
description: Use when reviewing code, diffs, pull requests, bugs, security risks, or maintainability concerns.
---

# Code Review

Review correctness first, then security, then maintainability...
```

字段策略：

```text
name         必需或可从目录名推导
description 供 LLM 判断何时使用 Skill，强烈建议存在
其他字段     不作为 Suna 行为依据；可以忽略或仅展示
```

未知字段不报错，但不赋予任何权限。

## config.toml

`config.toml` 只保存用户信任结果：

```toml
[skills.code-review]
enabled = true
hash = "sha256:abc123"

[skills.deploy-helper]
enabled = false
hash = "sha256:def456"
reasons = [
  "包含脚本",
  "脚本访问网络",
  "引用 GITHUB_TOKEN"
]
```

字段含义：

```text
enabled  是否允许加载该 Skill
hash     用户 check 过的 Skill 内容版本
reasons  check 发现的风险原因；无明显风险时可省略
```

不再设计：

```text
state / risk / script_policy / blocked / project_trusted / skill directory
```

运行时状态由 daemon 扫描后推导：

```text
active        enabled=true 且 hash 匹配且 SKILL.md 有效
inactive      enabled=false 且 hash 匹配
unchecked     目录存在但 config 没记录
needs_review  config 有记录但当前 hash 不匹配
invalid       SKILL.md 缺失或格式无效
missing       config 有记录但目录不存在
```

## Skill check

重型 check 不在每次启动运行，只在这些场景触发：

```text
1. 用户通过对话导入远程 repo
2. 用户通过对话导入本地目录或 zip
3. Suna 根据用户需求生成 Skill
4. 已记录 Skill 的内容 hash 变化后，用户要求重新检查
```

check 流程：

```text
validate 标准格式
  ↓
扫描 SKILL.md、references、scripts
  ↓
检测明显风险：危险命令、敏感路径、网络访问、prompt injection、混淆/二进制等
  ↓
LLM 辅助阅读理解 Skill 和脚本意图
  ↓
生成 reasons
  ↓
向用户解释并询问是否 enabled
  ↓
写入 config.toml
```

check 的目标不是证明安全，而是帮助用户理解风险。空 `reasons` 只表示“未发现明显风险”，不表示绝对安全。

## Skill 加载与使用

### 启动时

Daemon 启动时只做轻量工作：

```text
扫描 ~/.suna/skills
读取 SKILL.md 的 name / description
计算内容 hash
读取 config.toml
生成 Skill registry
```

不会每次启动都跑 LLM review，也不会要求用户重复确认。

### Prompt 组装

每轮用户消息前，Suna 只把 active Skill 的索引放入系统上下文：

```text
Available Skills:
- code-review: Use when reviewing code, diffs, pull requests...
- weekly-report: Use when writing weekly reports in the user's format...
```

LLM 根据 `description` 自己判断是否需要某个 Skill。

### skill.load

Daemon 提供内部工具：

```text
skill.load(name)
```

只允许加载：

```text
enabled=true
hash match
SKILL.md valid
```

加载后，完整 `SKILL.md` 进入后续上下文。未启用、未检查、hash 变化或无效的 Skill 不进入上下文。

## Skill scripts

Suna 不为 Skill scripts 设计单独 sandbox 或 script policy。

规则：

```text
1. enabled=true 表示用户信任整个 Skill 包，包括 scripts/ 中的辅助脚本。
2. LLM 可以按 SKILL.md 说明，通过现有工具读取 references 或用 exec 运行 scripts。
3. 运行时仍走现有工具系统和 Guard；Suna 不承诺完全隔离或证明脚本安全。
4. 风险解释前置到导入 / check / 启用阶段。
```

这避免半吊子的运行时权限系统。Suna 的职责是 check、解释、记录用户选择，而不是伪装成完整 sandbox。

## 对话式 Skill 操作

Skill 的主入口是自然语言，不是复杂 CLI。

典型用户表达：

```text
帮我导入这个 skill: https://github.com/user/skills
把 ~/Downloads/report-skill 加进来
把刚才这个流程保存成 skill
以后写周报都按这个格式
有哪些 skill 正在启用？
```

Suna 识别后进入内置 workflow：

```text
import/create
  ↓
validate/check
  ↓
展示 reasons
  ↓
询问是否启用
  ↓
写 config.toml
```

TUI 只保留简单入口：

```text
/skills
```

用于查看 active / inactive / unchecked / needs_review，并支持启用、停用、重新检查。复杂 CLI 和 marketplace 后置。

## System Workflows vs User Skills

Suna 内部保留 system workflows，但它们不是普通 Skill：

```text
skill import flow
skill authoring flow
skill check flow
mcp setup flow
```

这些 workflow 内置在 Suna 中，用来识别用户意图、生成 Skill、执行检查并引导用户启用。它们不放在 `~/.suna/skills`，不走普通 `skill.load`，也不受 `[skills.<name>]` 配置管理。

普通 User Skills 才是：

```text
~/.suna/skills/<name>/SKILL.md
```

需要 check、记录 hash、enabled 后才能被加载。

## MCP

MCP 是外部工具 / 资源 / 服务接入层，独立于 Skill。

MCP server 配置放在 `~/.suna/config.toml`：

```toml
[mcp.servers.github]
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]

[mcp.servers.github.env]
GITHUB_TOKEN = "${GITHUB_TOKEN}"

[mcp.servers.context7]
enabled = true
transport = "http"
url = "https://mcp.context7.com/mcp"
```

v1 优先支持：

```text
stdio
```

HTTP/SSE 可后续支持。

Daemon 启动流程：

```text
读取 config.toml
启动 enabled MCP servers
获取 tools / resources / prompts
注册到 Suna tool registry
在 chat 顶部展示 MCP 状态
```

MCP 启动失败不阻塞 Suna，只展示状态，例如：

```text
MCP: github ✓ filesystem ✕
```

Skill 可以在说明中提到需要某类外部能力，但不能内嵌 MCP server 配置。是否启用 MCP、如何提供 token、连接哪个 server，全部由用户的 `config.toml` 决定。

## Chat 状态展示

TUI 聊天顶部展示轻量状态：

```text
Skills: 3 active, 1 needs review
MCP: 2 enabled, 1 failed
```

正常情况不弹窗、不打断。异常只提示，例如：

```text
go-tui skill changed since last check and will not be loaded until reviewed.
```

用户可通过 `/skills` 查看详情。

## 最小实现清单

```text
SkillManager:
  - scan ~/.suna/skills
  - parse SKILL.md name/description
  - compute hash
  - validate/check
  - import from git/local/zip
  - create generated Skill
  - enable/disable
  - load

Agent/Runner:
  - prompt 注入 active skill index
  - 支持 skill.load(name)
  - load 后注入完整 SKILL.md

TUI:
  - chat 顶部 Skills/MCP 状态
  - /skills 简单管理页

MCPManager:
  - 读取 config.toml [mcp.servers.*]
  - v1 stdio client
  - 注册 MCP tools/resources/prompts
```
