# Suna 文档

这里存放 Suna 当前代码实际支持的设计、架构、配置和维护文档。根目录 `README.md` 是英文项目门面，突出 isolated subtasks、intent-aware Guard、runtime-first architecture、快速开始和常用功能；`README.zh-CN.md` 是中文项目门面；`docs/` 面向希望深入理解 Suna 的读者，目前以中文为主。

`plans/` 保留规划、调研和历史设计，可能已经滞后，也可能包含未实现能力；理解当前行为时不要以 `plans/` 为准。

## 推荐阅读路径

### 第三方 UI / 客户端开发

1. [stdio runtime 接入指南](runtime-stdio.md)：从第三方 UI、桌面端、IDE 插件或本地 Web 服务接入 Suna runtime。先看这个。
2. [Protocol](protocol.md)：了解 TUI 与 daemon 的统一 method / notification / schema 语义。
3. [配置说明](configuration.md)：如果 UI 需要做模型、Guard、MCP、Skill 配置页，再看这里。
4. [架构说明](architecture.md)：需要理解 daemon、transport、TUI 和核心包边界时再看。

### 快速了解代码和设计

1. [关键设计](design.md)：先理解 Suna 为什么采用 TUI + daemon、Guard、上下文压缩、Subtask、Skill、MCP 等设计。
2. [配置说明](configuration.md)：查看模型、`subtask_for`、Guard、MCP、Skill 等配置字段。
3. [性能优化](performance.md)：集中了解 daemon、模型流、上下文、工具和 TUI 的性能边界。
4. [Subtask 设计](subtask.md)：重点了解 Suna 如何由主 Agent 动态分配模型、上下文、图片和工具权限。
5. [架构说明](architecture.md)：再看整体分层、daemon 生命周期和核心模块边界。
6. [代码地图](code-map.md)：需要定位代码时，看功能到包、核心流程和常见入口。
7. [当前实现](current-implementation.md)：确认当前实际支持的行为和不要依赖的边界。

### 配置和使用细节

- [配置说明](configuration.md)：`config.toml`、`credentials.toml` 的字段、示例和限制。
- [TUI 架构](tui.md)：`internal/tui` 目录结构、Bubble Tea 约定和维护边界。

### 本地开发维护

- [开发指南](development.md)：本地构建、测试、提交前检查和代码约定。

## 文档分工

- 根目录 `README.md`：英文项目介绍、核心差异、快速开始、常用操作、安全提醒和 docs 入口。
- `README.zh-CN.md`：中文项目介绍、核心差异、快速开始、常用操作、安全提醒和 docs 入口。
- `docs/runtime-stdio.md`：第三方 UI / 客户端通过 `suna runtime --transport stdio` 接入 Suna runtime 的官方入口文档。
- `docs/protocol.md`：统一 protocol 语义，说明 method、notification、事件和错误边界。
- `docs/design.md`：关键设计和取舍，解释“为什么这样做”。
- `docs/performance.md`：daemon、模型流、上下文、工具和 TUI 的当前性能优化。
- `docs/subtask.md`：Subtask、独立上下文、动态模型分配和动态工具分配。
- `docs/architecture.md`：稳定架构和模块边界，解释“系统怎么分层”。
- `docs/code-map.md`：功能到代码位置和核心流程，解释“代码从哪里看”。
- `docs/current-implementation.md`：当前功能事实和未完成边界，解释“现在到底支持什么”。
- `docs/configuration.md`：配置字段和示例。
- `docs/tui.md`：TUI 内部结构。
- `docs/development.md`：构建、测试和维护约定。
- `plans/`：规划、调研、历史设计和阶段性记录，不作为当前实现文档。

## 维护原则

- README 保持吸引力和可读性，不展开过多实现细节；英文 README 和中文 README 需要同步核心定位。
- docs 记录当前事实，不把未来规划写成已完成能力；深度 docs 暂时以中文为主，优先保证准确性。
- 同一内容只保留一个主位置，其它文档用链接引用，避免重复维护。
- 涉及用户可见行为、配置字段、安全边界或模块职责变化时，应同步更新相关 docs。
