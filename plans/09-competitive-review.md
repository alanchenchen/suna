# 09 — 竞品对比与设计审查

对照 Claude Code、OpenClaw、Codex Desktop 检查 Suna 设计完整性。

> **注**: 本文档基于旧版 7 模块设计编写，后更新为三层有状态实体架构（感知/记忆/行动）。意图层归档为远期探索。

## 对比矩阵

| 特性 | Claude Code | OpenClaw | Suna (三层架构) | 差距 |
|------|-------------|----------|------|------|
| **语言** | TypeScript (Node) | TypeScript (Node) | Go | ✅ 差异化优势 |
| **多模型** | 仅 Anthropic (Bedrock/Vertex/Fireworks 代理) | 多模型 + failover | 多模型 + 智能路由 | ✅ Suna 路由更智能 |
| **核心工具** | Read/Write/Edit/Bash/Glob/Grep + MCP | exec/read/write/edit/browser/canvas + MCP | 固定 9 个 + MCP | ⚠️ 缺 glob/grep 原生工具 |
| **权限模型** | allow/deny + yolo 模式 | allow/deny + sandbox + exec approvals | LLM 审查 + 硬规则 + 渐进信任 | ✅ Suna 创新点 |
| **多渠道** | Terminal/VSCode/Desktop/Web/JetBrains | 25+ 消息平台 + macOS/iOS/Android | TUI + (远期 Web) | ❌ I/O 渠道少 |
| **能力系统** | CLAUDE.md + Skills (SKILL.md) + auto memory | SKILL.md + Plugin (npm) | SKILL.md + JS (QuickJS/WASM) + MCP | ✅ 学习能力是差异化 |
| **记忆** | CLAUDE.md + auto memory (learnings) | memory_search/memory_get + workspace files | 4 层记忆 + 仅添加式 + 时间推理 + 多信号检索 | ✅ Suna 领先一代 |
| **意图/主动性** | 无 | 无 | 感知层主动触发 (意图层归档为远期) | ⚠️ 远期探索 |
| **有状态** | 无状态循环 | Gateway daemon 但 agent 无状态 | 持续运行的有状态实体 (感知+记忆) | ✅ Suna 领先 |
| **定时任务** | Routines (云端) + scheduled tasks | cron 工具 | Timer/Watcher/Webhook/Stream (感知层) | ✅ 已覆盖 |
| **Sub Agent** | Lead + sub agents | subagents + multi-agent routing | Main + Sub (Spawn) | ⚠️ 基本一致 |
| **Hooks** | PreToolUse/PostToolUse/Notification | Plugin hooks | OnSignal/PreLLM/PreToolUse/PostToolUse (Shell+Skill) | ✅ 已覆盖 |
| **长任务** | Goal 命令 (8h+) | cron + sessions | TUI 进程 + 感知驱动 | ✅ 已覆盖 |
| **思考深度** | thinking (extended) | /think low/medium/high | /think + 路由联动 | ✅ 已覆盖 |
| **人格** | CLAUDE.md | SOUL.md | SOUL.md + 用户认知 | ✅ 已覆盖 |
| **Daemon** | 常驻 (Desktop/Cloud) | Gateway daemon (launchd/systemd) | TUI 进程 (无 daemon) | ⚠️ 不同路线 |
| **Browser** | 无内置 (MCP 可扩展) | browser 工具 (Chromium) | 无 | ⚠️ 需依赖 MCP 或 skill |
| **搜索** | 无内置 | web_search / x_search / web_fetch | ReadHTTP (原始) | ⚠️ 缺结构化搜索 |
| **图片生成** | 无内置 | image_generate (多 provider) | 无 | ❌ 不在 MVP 范围 |
| **语音** | 无内置 | TTS + voice wake + talk mode | 无 | ❌ 不在 MVP 范围 |
| **Canvas** | 无 | canvas (A2UI) | 无 | ❌ 不在 MVP 范围 |
| **多设备节点** | 无 | macOS/iOS/Android nodes | 无 | ❌ 不在 MVP 范围 |
| **Sandbox** | 无 | Docker/SSH/OpenShell sandbox | Guard (LLM 审查) | ⚠️ 不同路线 |
| **Plugin 系统** | MCP only | Plugin API (npm 包) | JS (QuickJS/WASM) + MCP | ✅ 不同路线，可接受 |
| **配置格式** | JSON | JSON (openclaw.json) | TOML | ✅ 用户偏好 |
| **TUI** | Ink (React CLI) | WebChat | Bubble Tea | ✅ Go 生态最佳选择 |

## 已识别差距

### 1. 缺少 Grep/Glob 原生工具

Claude Code 内置 `Grep` 和 `Glob` 工具用于代码搜索。Suna 设计中明确说"能用 Exec 做的事不单独加工具"，但代码搜索是 agent 高频操作。

**影响**: 每次 `Exec("grep -rn 'pattern' src/")` 都要经过 Guard 审查（Exec 是 Act 工具），增加延迟和成本。grep/glob 本质上是只读操作，不应过 Guard。

**建议**: 将 Grep/Glob 加入 Perceive 工具集，或把 Exec 中的只读命令自动归类为低风险跳过 LLM 审查。当前设计的 Stage 2 风险评级已部分覆盖（`ls, cat, grep` 低风险），但原生工具返回结构化结果比解析 Exec 文本输出更可靠。

**决策**: 维持 9 工具设计，但优化 Exec 的风险评级——grep/glob/find/head/tail/wc 等只读命令自动归为 RiskLow，不经过 Stage 3 LLM 审查。在 04-guard.md 的风险评级中已隐含覆盖，但需要明确。

### 2. 缺少 Web 搜索结构化工具

OpenClaw 有 `web_search`、`x_search`、`web_fetch`。Suna 只有 `ReadHTTP`（原始 HTTP 请求）。

**影响**: agent 需要 web 搜索时，只能 Exec("curl") 或 ReadHTTP 直接调用搜索 API，LLM 需要自己构造请求和解析响应。

**建议**: MVP 阶段不加入。原因：
- Web 搜索依赖外部 API（Google/Bing/SerpAPI），增加配置复杂度
- ReadHTTP + Exec 可以覆盖，skill 可以封装搜索能力
- 不属于"核心工具"范畴，符合空杯哲学

**决策**: 不改。作为 skill 提供（如 `web-search/` 能力目录包含 SKILL.md + main.js，封装 SerpAPI 调用）。

### 3. 缺少 Browser 自动化工具

OpenClaw 内置 Chromium browser 控制。Claude Code 通过 MCP 扩展。

**影响**: 网页抓取、自动化测试等场景需要浏览器。

**建议**: MVP 不加。通过 MCP (Playwright/Puppeteer MCP server) 或 skill 覆盖。05-capability.md 已设计 mcp 类型能力。

**决策**: 不改。MCP + skill 覆盖。

### 4. I/O 渠道覆盖不足

Claude Code: Terminal + VS Code + Desktop + Web + JetBrains + iOS
OpenClaw: 25+ 消息平台 + macOS + iOS + Android
Suna: TUI + (远期 Web)

**影响**: 用户无法从微信/Telegram/手机等渠道使用 Suna。

**建议**: 这是 MVP 的合理范围。01-architecture.md 已设计 I/O 抽象层 (`IO interface`)，为多渠道扩展预留了接口。Phase 5 规划了"多 I/O"探索。

**决策**: 不改。I/O 抽象层已预留扩展能力。

### 5. 缺少结构化搜索/分析工具

OpenClaw 有 `code_execution` (远程 Python sandbox)、Claude Code 有 `codebase_search`。

**影响**: 复杂数据分析、代码库语义搜索等场景受限。

**建议**: JS (QuickJS/WASM) 引擎可覆盖简单数据分析。语义搜索不在 MVP 范围。代码搜索通过 Exec + grep 覆盖。

**决策**: 不改。QuickJS + Exec 覆盖。

### 6. Sandbox 隔离

OpenClaw 提供 Docker/SSH sandbox 隔离非 main session。Suna 用 LLM Guard 做安全。

**影响**: Suna 不提供进程级隔离，sub agent 的操作在主进程权限下运行。

**建议**: Guard 是 Suna 的差异化创新，不需要传统 sandbox。但在 Phase 5 可考虑为 sub agent 添加可选的 Docker 隔离。

**决策**: MVP 不加。记录为 Phase 5 探索项。

## 新增发现

### 7. 缺少 AGENTS.md 等级的项目级配置

Claude Code 和 OpenClaw 都支持项目级配置文件（`CLAUDE.md` / `AGENTS.md`）。Suna 目前只有全局 `~/.suna/config.toml`。

**建议**: 增加 `.suna/AGENTS.md` 或 `SUNA.md` 项目级配置文件支持，放在工作目录中自动加载。内容为项目特定的 agent 指令、能力偏好、工具约束等。

**决策**: 需要补充到 01-architecture.md。

### 8. 缺少 `/compact` 触发压缩后的反馈

Claude Code 的 `/compact` 有明确的用户反馈。Suna 设计了 `/compact` 命令但没描述反馈机制。

**建议**: 补充压缩后的 TUI 反馈（压缩了多少 token、保留了多少轮）。

**决策**: 小改动，补充到 06-memory.md。

### 9. 缺少 Thinking/推理预算的控制细节

Claude Code 和 OpenClaw 都有 reasoning_effort/thinking 控制。Suna 有 `/think` 命令但缺少具体实现细节。

**建议**: 已在 01-architecture.md 的 Thinking 控制部分覆盖。无需额外改动。

**决策**: 已覆盖，不改。

### 10. 缺少 Session 共享/协作

OpenClaw 有 `sessions_list/sessions_history/sessions_send` 实现 session 间通信。Claude Code 有 agent teams。

**建议**: Suna 的 Spawn 已覆盖 sub agent 调度。跨 session 通信不在 MVP 范围。

**决策**: 不改。Phase 5 探索项。

## 总结

### 设计完备度评估

| 维度 | 评分 | 说明 |
|------|------|------|
| 核心循环 | 9/10 | agent loop + context + compression 完善 |
| 工具系统 | 8/10 | 9 工具合理，Exec 风险评级需更精确 |
| 安全模型 | 9/10 | LLM Guard 是创新，渐进信任有差异化 |
| 能力系统 | 9/10 | 三层能力 + 学习流程完整 |
| 记忆系统 | 9/10 | 4 层 + 压缩 + 持久化 |
| 触发器 | 9/10 | 4 种触发器覆盖主要异步场景 |
| 多模型路由 | 9/10 | 三层路由 + 缓存 + 降级 |
| I/O 渠道 | 5/10 | MVP 只有 TUI，抽象层预留了扩展 |
| 生态/渠道 | 3/10 | 无消息平台、无移动端，远期规划 |

### 需要补充的文档改动

1. **01-architecture.md**: 增加项目级配置文件支持（`.suna/AGENTS.md`）
2. **04-guard.md**: 明确 Exec 中只读命令（grep/glob/find/head/tail/wc/cat）自动 RiskLow，不经过 Stage 3
3. **06-memory.md**: 补充 `/compact` 命令的 TUI 反馈格式

### MVP 阶段不需要改动的项

- Web 搜索：skill 覆盖
- Browser：MCP 覆盖
- 语音/Canvas/图片生成：Phase 5
- 多渠道 I/O：Phase 5
- Sandbox 隔离：Guard 替代，Phase 5 可选
- 跨 session 通信：Phase 5

### Phase 5 探索项汇总

- 多 I/O 渠道（微信/Telegram/手机）
- Browser 工具或 MCP 封装
- Docker sandbox（可选）
- Web UI
- 语音交互
- 能力市场
