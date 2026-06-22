# 开发指南

本文记录 Suna 本地开发、测试和提交前检查约定。

## 本地构建

Suna 需要先安装或构建成二进制后运行；不要使用 `go run .` 启动，daemon / TUI 的本地进程管理依赖稳定的可执行文件路径。

```bash
go build -o suna .
./suna
```

项目内置 release 脚本：

```bash
./build/build-darwin.sh    # 构建 macOS arm64 / amd64
./build/build-linux.sh     # 构建 Linux arm64 / amd64
./build/build-windows.sh   # 构建 Windows arm64 / amd64
./build/build-release.sh   # 一次性构建以上 6 个平台包
```

Release 构建产物默认放在 `dist/`。压缩包名称带平台/架构后缀，包内二进制统一为 `suna`（Windows 为 `suna.exe`）。`build-release.sh` 会优先使用 `SUNA_BUILD_VERSION`，否则在 tag 上构建时使用当前 Git tag，开发构建回退到 `dev+<short-sha>`。

正式发版通过 GitHub Actions 完成：推送 `v*` tag 后，`.github/workflows/release.yml` 会运行测试、构建全平台包、生成 `checksums.txt`、创建 GitHub Release、自动生成 release notes 并上传 `dist/*`。本地只需：

```bash
git tag -a v0.3.0 -m "v0.3.0"
git push origin main
git push origin v0.3.0
```

用户可在退出 TUI 后运行 `suna update --check` 或 `suna update`。update 会先检查 daemon 是否仍在运行；如果 daemon 仍在运行，会中止并提示用户先退出 TUI / `suna stop`。下载缓存位于 `~/.suna/update/`，开始前和结束后都会清理。

## 常用运行命令

```bash
./suna              # 打开 TUI，必要时自动启动 daemon
./suna status       # 查看 daemon 状态
./suna update --check # 检查 GitHub Release 是否有新版本
./suna update       # 下载、校验并安装最新 GitHub Release
./suna stop         # 停止 daemon
```

运行数据默认位于 `~/.suna/`，排查问题时优先查看：

```text
~/.suna/config.toml
~/.suna/credentials.toml
~/.suna/memory.db
~/.suna/skills/
~/.suna/attachments/
~/.suna/logs/app.log
```

## 测试

提交前建议至少运行：

```bash
go test ./...
git diff --check
```

如果只改 TUI，可先跑局部测试：

```bash
go test ./internal/tui/...
```

涉及工具、Guard、memory、skill、daemon 时，应运行对应包测试和全量测试。

## 提交前检查

建议检查：

```bash
git status --short
git diff --stat
git diff --check
go test ./...
```

注意不要误提交：

- 本地构建产物，例如根目录 `suna` 二进制。
- 截图、临时文件、调试输出。
- `~/.suna` 下的配置、凭据或数据库。

## 代码边界

### TUI 改动

TUI 改动应聚焦 `internal/tui/**`，必要时只在 `main.go` 做启动入口胶水适配。

不应在 TUI 中直接引入：

- `internal/agent`
- `internal/runner`
- `internal/tools`
- `internal/guard`
- daemon 内部业务实现

TUI 与 daemon 的交互必须通过 protocol 和 local transport。

### daemon 和核心包改动

daemon、agent、runner、tools、guard、memory、skill 等核心包应独立维护业务语义。UI 不应为了展示方便改变这些包的默认值、超时、权限边界或持久化格式。

### 工具系统改动

模型可见工具统一由 `internal/tools.Manager` 管理。新增或调整工具时应遵守：

- 新工具优先作为 `tools.Provider` 接入，不要在 Agent/Runner 中手动拼接 tool schema。
- 具体能力实现放在对应来源包中，例如内置工具放 `internal/tools/builtin`，Skill 适配放 `internal/tools/skilltools`，Agent runtime 工具放 `internal/tools/agenttools`，MCP 工具适配放 `internal/tools/mcptools`。
- `tools.Manager` 负责稳定目录、schema 生成和执行路由；Guard 决策仍由 Agent 统一处理。
- 工具默认应走 Guard；只有 `askuser`、`spawn`、`skill_load`、`skill_start` 这类明确的运行时/说明类工具才声明跳过 Guard。
- 修改 tool name、description、parameters 或排序策略会影响模型前缀缓存；除非有明确收益，应保持同一工具集合下 schema 稳定，并同步更新稳定性测试。
- 工具 description 和参数说明应紧凑、单意；同一信息不要在工具名、描述和字段说明里重复。tool definitions 在每次请求都计入上下文，冗长描述会持续消耗 token，并稀释模型对关键参数的注意力。
- 字段默认值/上限统一写成 `Default X, max Y` 这类短语形式，不再写成 `Maximum xxx to return, default xxx, max xxx` 这类重复模板。

### MCP 改动

当前 MCP 是基础 tools-only runtime，维护时应遵守：

- MCP server 配置只放在 `config.toml [mcp.servers.<name>]`，不要写入 Skill 包。
- `internal/mcp` 负责 server 生命周期、stdio transport、JSON-RPC、tools/list、tools/call 和运行态状态。
- `internal/tools/mcptools` 只做 MCP tool 到 `tools.Provider` 的适配，公共工具名保持 `mcp__<server>__<tool>`。
- 单个 MCP server 启动、reload 或 tools/list 失败不得阻塞 daemon；错误应保留到 `/mcp` 状态展示和日志。
- 启停或 reload MCP server 后必须刷新 `tools.Manager`，让下一轮模型请求看到最新工具目录。
- MCP v1 不支持 resources、prompts、sampling、OAuth、动态 tool refresh 或 sandbox；不要把这些能力写成已完成。
- MCP server 是外部不透明进程，启用 server 表示用户信任它；Guard 不能隔离 server 内部文件、网络或进程权限。

## 注释约定

代码注释使用中文。

以下情况建议写注释：

- 并发、channel、timer、context timeout。
- 非直观状态机或错误恢复逻辑。
- 安全边界和权限判断。
- protocol 兼容或迁移 glue。
- 为性能、背压、流式渲染做的特殊处理。

不建议写无信息量注释，例如简单重复函数名或字段名。过时注释应随代码同步删除。

## 硬编码与默认值

默认值、超时、上下文窗口、路径规则等应集中维护：

- 属于模型/provider/router 的默认值放在模型或路由相关层。
- 属于 daemon 生命周期的默认值放在 daemon 或统一配置层。
- 属于 TUI 展示的默认值可放在 TUI 常量中。

避免在 runner、daemon、agent、TUI 之间层层猜测或重复维护同一个默认值。

## 文档维护

- `README.md` 面向使用者，保持简洁。
- `docs/` 记录稳定架构和开发约定。
- `plans/` 保留设计规划、调研和历史记录。
- 新增复杂模块时，优先更新 `docs/`；只有当说明必须贴近代码维护时，才考虑子包 README。
