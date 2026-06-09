# Suna 文档

这里存放 Suna 当前代码实际支持的实现、配置和架构文档。规划、调研和历史设计仍保留在 `plans/`，不一定代表当前行为。

## 文档索引

- [当前实现](current-implementation.md)：按模块记录当前功能、运行行为和未完成边界。
- [配置说明](configuration.md)：`config.toml`、`credentials.toml` 的完整字段、示例和当前边界。
- [架构说明](architecture.md)：当前 CLI、TUI、daemon、protocol 和核心包边界。
- [TUI 架构](tui.md)：`internal/tui` 重构后的目录结构、Bubble Tea 约定和维护边界。
- [开发指南](development.md)：本地构建、测试、提交前检查和代码约定。

## 文档分工

- 根目录 `README.md`：用户入口、功能说明、安装、常用操作和注意事项。
- `docs/`：当前实现事实、配置、架构和维护说明。
- `plans/`：规划、调研、历史设计和阶段性记录。
- 子包 README：仅当某个包足够复杂且必须贴近代码维护时再新增。
