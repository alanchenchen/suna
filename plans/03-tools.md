# 03 — 核心工具

Suna 当前对 main agent 暴露 10 个基础工具定义：8 个 builtin registry tools（readfile/listdir/search/exec/writefile/editfile/filesystem/http）+ 2 个 agent built-ins（askuser/spawn）。`askuser` 和 `spawn` 依赖 main agent 事件流和动态 schema，由 `internal/agent` 特殊处理，不注册到通用 builtin provider。更高级的任务流程通过 Skill 学习/导入，外部工具通过 MCP 接入。

工具返回在实现上统一为 `tool.Result{Content string, IsError bool, Truncated bool, Metadata map[string]any}`。下面的“返回”描述以当前 LLM 实际看到的 `Content` 文本为准；`truncated` 和 `metadata` 是内部结构化标记，不代表每个工具都会返回 JSON 对象，也不会额外进入 LLM 上下文。

## 设计原则

```
1. Perceive 工具不需要确认，可以直接使用
2. Act 工具必须经过 Guard 审查
3. 能用 Exec 做的事，不单独加工具
4. 不加的工具: grep/glob/find → Exec, 进程查看 → Exec("ps"), 压缩 → Exec("tar")
5. 图片/音视频理解 → 模型 multimodal content，不是工具
```

## 工具结果展示

```
Content:
  - 给 LLM 消费，必须短、稳定、语义明确
  - 不放大段 diff，不放完整文件内容，避免污染上下文
  - 文件修改类工具使用单行事实摘要，例如:
    file updated: config.toml (+3 -1, 1 replacement, 84B -> 126B)

Metadata:
  - 给 TUI/API 客户端消费，不进入 LLM 上下文
  - 用于在主工具块中展示更直观的结果条，避免 TUI 解析自然语言文本
  - 当前已定义 kind="file_change"，字段包含 path/operation/added_lines/removed_lines/size_before/size_after/replacements
```

## 工具分类

```
Perceive (感知) — 只读调用可被 Guard 归为低风险
  ReadFile    读文件内容
  ListDir     列目录内容
  Search      搜索文件名或文件内容

Act (行动) — 必须经过 Guard 审查；其中部分 action 可被静态判定为低风险只读
  Exec        执行命令 (万能逃逸口，最危险的工具；可证明只读命令为 low risk)
  WriteFile   创建/覆盖/追加文件
  EditFile    精确编辑文件部分内容
  FileSystem  stat/mkdir/move/copy/remove 文件系统路径；stat 为只读 low risk
  HTTP        统一 HTTP 请求；GET/HEAD 为只读 low risk，写方法按风险审查

Agent built-ins (特殊处理，不在 builtin provider 中)
  AskUser     向用户提问/确认 (不经过 Guard)
  Spawn       委派 subtask (仅 main agent 可用，不经过 Guard)
```

Exec 归类为 Act 的原因：Exec 可以执行任何命令，包括删除、安装、网络操作。Guard 通过轻量 shell analyzer 将可证明只读的 grep/ls/cat/git status 等命令归为 low risk（零 LLM 审查成本）；复杂/动态/未知命令至少归为 medium risk，高危命令归为 high risk 或命中硬拦截。

## 逐工具详细设计

### ReadFile

```
功能: 读取文件内容，返回文本或 base64
参数: { path: string, start_line?: int, line_count?: int, tail_lines?: int, encoding?: "text"|"base64" }
返回: 带行号的文本内容；内部标记 truncated

边界处理:
  - 文本大文件: 读取上限 100KB，超过后截断，并提示用 start_line/line_count 读取更多
  - tail_lines > 0 时读取末尾 N 行，并忽略 start_line/line_count
  - 二进制/base64: encoding="base64" 返回 base64 编码，文件大小上限 10MB
  - 不存在的文件: 返回错误 "file not found: xxx"
  - 权限不足: 返回错误 "permission denied: xxx"
  - 符号链接: 跟随链接读取实际文件

为什么是原生工具而不是 Exec("cat"):
  - 跨平台一致 (Windows 没有 cat)
  - 分页读取大文件，不爆上下文
  - 稳定文本格式 (行号、截断标记)
  - 自动检测编码
```

### ListDir

```
功能: 列出目录内容，返回稳定文本文件列表
参数: { path: string, recursive?: bool, max_depth?: int, offset?: int, limit?: int, include?: [string], exclude?: [string], include_hidden?: bool }
返回: 每行一个 entry，包含类型、路径、大小和修改时间；内部标记 truncated

边界处理:
  - 空目录或过滤后无可见条目: 返回 empty directory
  - 不存在的目录: 返回错误
  - 大目录: 默认最多 500 条，最大 1000 条，截断后提示 offset 继续读取
  - recursive=true 时限制 max_depth 最大为 3
  - include/exclude 使用 glob 模式过滤；include_hidden=false 时隐藏点号开头条目

为什么是原生工具而不是 Exec("ls"):
  - ls 在 Windows/macOS/Linux 格式不同
  - 稳定格式比各平台 ls 输出更可靠
  - 非程序员用户会问"桌面上有什么"，不会用 ls
```

### Search

```
功能: 在目录下搜索文件名或文件内容
参数: { path: string, query: string, mode?: "content"|"name", regex?: bool, case_sensitive?: bool, recursive?: bool, max_depth?: int, include?: [string], exclude?: [string], use_default_exclude?: bool, max_matches?: int }
返回: content 模式返回 path:line: text；name 模式返回匹配路径；内部标记 truncated

边界处理:
  - 默认 recursive=true，max_depth 默认 8，最大 20
  - 默认最多返回 200 个匹配，最大 1000
  - 单文件超过 2MB 跳过；二进制文件跳过
  - 默认排除 .git、node_modules、vendor、dist、build、target、.cache、coverage、tmp 等目录
  - 默认排除常见凭据文件，例如 .env、*.pem、*.key、.ssh/**、.gnupg/**、.netrc、.npmrc 等
```

### HTTP

```
功能: 发送 HTTP 请求，返回响应内容
参数: { url: string, method?: "GET"|"HEAD"|"POST"|"PUT"|"PATCH"|"DELETE", headers?: map[string]string, body?: string, timeout?: int, max_body_bytes?: int }
返回: 文本格式响应，包含 status/headers/body；内部标记 truncated

边界处理:
  - method 默认 GET
  - 超时: 默认 30 秒
  - 大响应: 默认超过 100KB 截断，可通过 max_body_bytes 调低或调高
  - JSON/HTML/文本响应均返回原始 body
  - 重定向: 自动跟随，最多 5 次
  - 4xx/5xx: 返回状态码和响应内容，不视为工具错误

Guard:
  - GET/HEAD 是只读调用，通常 low risk；访问 localhost、私网或 link-local URL 会升为 medium
  - POST/PUT/PATCH 为 medium risk
  - DELETE 为 high risk
```

### Exec

```
功能: 执行系统命令
参数: { command: string, cwd?: string, timeout?: int, env?: map[string]string, shell?: string }
返回: stdout/stderr 拼接后的文本；非零退出码会追加 `[exit code: N]` 并标记为 tool error；内部标记 truncated

经过 Guard 审查:
  Exec 是最危险的工具，所有 Exec 调用都经过 Guard
  Guard 硬规则按 OS 区分 (见 04-guard.md)

跨平台策略:
  shell 参数: "auto" (默认) | "bash" | "powershell" | "cmd"
  
  Shell = "auto" (默认):
    Windows: 当前实现优先找 Git Bash，再回退 PowerShell/cmd
    macOS/Linux: 当前实现直接使用默认 bash/sh
  
  当前 tool result 和审计日志不记录 shell_used 字段。

边界处理:
  - 超时: 默认 60 秒
  - 大输出: stdout/stderr 各截断到 50KB
  - 非零退出码: 返回输出内容，并视为 tool error
  - 交互式命令: 不支持 (命令不能等待 stdin 输入)
  - 后台命令: 当前没有专门 pid 返回或后台任务管理语义
  - Windows 特殊处理: 命令翻译层仍是后续项

这是万能逃逸口:
  grep → Exec("grep -rn 'pattern' src/")
  find → Exec("find . -name '*.go'")
  ps   → Exec("ps aux")
  tar  → Exec("tar -czf out.tar.gz dir/")
  curl → Exec("curl -X POST ...")
  git  → Exec("git status")
  npm  → Exec("npm install")
  ...
```

### WriteFile

```
功能: 创建、覆盖或追加文件
参数: { path: string, content: string, mode?: "overwrite"|"create_new"|"append", create_dirs?: bool, expected_sha256?: string }
返回: 单行文件变更摘要，例如 file created: notes/todo.md (+24 -0, 612B)

metadata:
  kind: "file_change"
  path: 文件路径
  operation: "created" | "updated" | "appended" | "unchanged"
  added_lines / removed_lines: 行数变动统计
  size_before / size_after: 修改前后字节数；新文件没有 size_before

经过 Guard 审查:
  - 路径检查: 不允许写入 /etc, /usr, /System 等系统目录
  - 覆盖检查: 覆盖已有文件时 Guard 会额外提示风险
  - create_dirs: true 时自动创建父目录
  - expected_sha256: 写入前校验已有文件 SHA-256，避免基于过期内容覆盖

性能策略:
  - 覆盖或追加已有文件前会读取旧内容，用于判断 created/updated/appended/unchanged 和计算变更摘要
  - 写入采用同目录临时文件 + rename，避免目标文件半写入
  - 不生成 diff；行数统计对大变化片段有 LCS 上限，超过后退化为前后缀差异统计
```

### EditFile

```
功能: 对单个文件应用一个或多个精确文本替换，所有替换在内存中验证并原子写入
参数: { path: string, edits: [{ old_string: string, new_string: string, occurrence?: int, replace_all?: bool, expected_replacements?: int }] }
返回: 单行文件变更摘要，例如 file updated: config.toml (+3 -1, 1 replacement, 84B -> 126B)

metadata:
  kind: "file_change"
  path: 文件路径
  operation: "updated" | "unchanged"
  added_lines / removed_lines: 行数变动统计
  size_before / size_after: 修改前后字节数
  replacements: 实际替换次数

经过 Guard 审查:
  同 WriteFile 的路径检查

匹配逻辑:
  - 每个 edit 的 old_string 必须在当前内容中精确匹配 (区分大小写、空白)
  - 未指定 occurrence 或 replace_all 时，如果 old_string 匹配多次则报错，要求显式指定 occurrence 或 replace_all=true
  - occurrence 为 1-based，只替换指定第 N 次出现
  - replace_all=true 替换全部匹配；不能同时指定 occurrence
  - expected_replacements 可用于断言实际替换次数，不符合则失败
  - 任一 edit 失败时不会写入文件

为什么需要 EditFile 而不全用 WriteFile:
  - 精确修改几行，不需要重写整个文件
  - LLM 可以只看需要改的部分，减少上下文占用
  - 减少误覆盖风险 (只改该改的)
```

### FileSystem

```
功能: 管理文件系统路径，包括 stat、mkdir、move、copy、remove
参数: { action: "stat"|"mkdir"|"move"|"copy"|"remove", path: string, destination?: string, parents?: bool, overwrite?: bool, recursive?: bool, allow_missing?: bool, expected_kind?: "any"|"file"|"dir"|"symlink" }
返回: stat 返回路径 kind/size；变更操作返回 filesystem 摘要；metadata kind="fs_change"

边界处理:
  - stat 使用 os.Lstat，不跟随 symlink 判断目标 kind
  - mkdir 支持 parents=true 创建父目录
  - move/copy 需要 destination；parents=true 可创建目标父目录
  - copy 目录必须 recursive=true；copy symlink 会保留 symlink
  - remove 目录默认只删除空目录；递归删除必须 recursive=true
  - overwrite=true 允许覆盖同类型 destination；覆盖目录还必须 recursive=true
  - expected_kind 可在操作前断言路径类型

Guard:
  - stat 是只读 low risk
  - mkdir/move/copy/remove 默认为 medium risk
  - recursive remove、敏感/系统/启动/profile/CI/git hook 等路径为 high risk
```

### AskUser

```
功能: 向用户提问并等待回复
参数: { question: string, options?: [string], allow_custom?: bool }
返回: { answer: string }

不经过 Guard:
  这是纯交互工具，不涉及任何系统操作

使用场景:
  - 需要用户确认: "确定要删除 xxx 吗？"
  - 需要额外信息: "请提供数据库连接地址"
  - 选择分支: "有 3 种方案，你倾向哪种？"
  - Skill authoring: "这像是可复用流程，要我保存成 Skill 吗？"

options 参数:
  提供 options 时，TUI 渲染为选择列表
  不提供时，TUI 渲染为开放式输入框

allow_custom 参数:
  默认 true，普通问题应省略或保持 true，让用户可以自由输入
  false 表示 choice-only，只能选择 options 中的一个答案
  仅用于严格系统/workflow 确认，例如 Skill workflow 的“是否运行 LLM review / 是否启用”
```

### Spawn

```
功能: 创建 subtask 执行子任务 (仅 main agent)
参数: {
  task: string,              // 必填
  model: string,             // 必填: subtask 使用的模型 ref (provider/model)
  tools: [string],           // 必填: subtask 可用工具列表；[] 表示纯模型任务
  input_images?: [int],      // 当前用户消息图片索引，例如 [0]
  context?: string,          // 传给 subtask 的额外上下文
  system?: string            // 可选 fallback；正常由 subtask_system.md 模板生成
}
返回: JSON 文本 { result: string, success: bool, status: string }

不经过 Guard:
  subtask 内部的 Act 操作仍然经过 Guard
  Spawn 本身只是创建了一个受限的执行环境

工具权限:
  tools 参数必填，指定 subtask 可用的工具列表
  tools=[] 表示纯模型任务，例如识图、总结、改写
  没有"默认工具集" — 缺少 tools 会返回错误，让 main LLM 重选
  Exec 在 subtask 中仍然经过 Guard 审查（含轻量 shell analyzer 的只读快速放行）
  subtask 禁止授予 askuser 和 spawn — 防止交互逃逸和嵌套
  daemon 校验每个非空 tool name: 不存在/spawn/askuser 都返回 tool error

模型选择:
  model 必填，指定 subtask 使用的模型 ref
  daemon 校验 model ref 是否为已配置模型
  model 为空或非法 → 返回 tool error，main LLM 重新选择

系统提示词:
  subtask 使用独立 subtask_system.md，不继承 main system.md、active memory、main working memory 或 conversation history
  subtask_system.md 只含 task/env/tools/context/rules，并明确 one-way data flow
  通过 runner request 的 System 字段注入

上下文隔离:
  spawn request 只把 task/context/tools/model/input_images 显式传入 subtask
  subtask 的 tool events 会转发给 TUI 用于可观察性，但不会成为 main 对话继承给 subtask 的共享上下文
  subtask 返回 final result/status 给 main，不保存独立长期记忆

超时:
  spawn 不限制 subtask 总运行时长，长任务可以自然运行到完成
  主 LLM 流式无响应由 runner stream timeout 处理
  工具卡住由各工具自己的 timeout 处理
  smart Guard review 有独立短超时，失败时回退到人工确认

并发:
  Main 可以同时发起多个 Spawn
  每个 subtask 独立 goroutine 运行
  Main 等待所有 sub 完成后汇总

Guard 策略:
  subtask 使用全局 Guard policy、workspace、blocked/allowed、audit DB
  需要用户确认时通过 main agent 事件流暂停并等待确认
  smart mode 下可使用同一 LLM reviewer
  Guard review 上下文来自 subtask 自己的 runner working：delegated task、最近 subtask 消息摘要、tool intent、assistant context
  LLM review 返回 modify 时不执行原调用，reason/suggestion 作为 tool error 返回 subtask LLM，由 subtask 决定是否重试

事件与 usage:
  subtask 不对外发送 stream/reasoning
  subtask 的 tool call/tool guard/guard confirm/tool result 通过 main agent 事件流转发给 TUI
  subtask tool 相关事件使用统一 namespaced id: spawn:<parentToolCallID>:<subToolCallID>
  TUI 依靠该 id 把 Guard 决策、风险、确认结果和文件变更挂到对应子工具行
  subtask 的 usage 计入 main session
```

## 工具定义的 JSON Schema

每个工具提供标准 JSON Schema，供模型 tool calling 使用。格式遵循 OpenAI function calling 规范（Suna 内部统一格式，Provider 层负责转换为各厂商格式）。

## 工具结果的大小控制

```
所有工具返回的内容都有上限:
  ReadFile:   文本结果 100KB；base64 文件最大 10MB
  ListDir:    默认 500 条，最大 1000 条
  Search:     默认 200 个匹配，最大 1000；输出 100KB；单文件扫描 2MB；最多扫描 20000 文件
  HTTP:       默认响应 body 100KB，可通过 max_body_bytes 调整
  Exec:       stdout/stderr 各 50KB

超过上限:
  截断 + truncated=true 标记
  LLM 看到 truncated 后可以决定:
    - 用 start_line/line_count 分页读取 readfile
    - 用 offset/limit 分页读取 listdir
    - 用 search 的 include/exclude/max_matches 缩小范围
    - 用 Exec + 管道处理 (如 grep 过滤)
    - 忽略多余内容

这是上下文管理的第一道防线。

IPC 展示层额外限制:
  - agent/runner/LLM 内部保留完整 tool.Result.Content
  - daemon 发送 agent.tool_end 给 UI 时，只对 result 做 16KB 展示级限制
  - 超过 16KB 时，result 只包含前 16KB 的 UTF-8 安全文本
  - agent.tool_end 同时携带 result_truncated 和 result_bytes
  - UI 根据字段自行决定如何展示截断状态

Tool intent 展示:
  - tool_start.intent 是 UI 主线展示的首要文本，用来让用户第一眼知道 agent 正在做什么
  - tool_guard 是工具执行前的安全来源事件，展示 Guard 决策、风险、来源和原因，不进入 tool.Result.Metadata
  - params/result 默认不在聊天主线展开，只进入详情面板
  - tool error 需要在主线显示短错误摘要
  - Guard reject 视为该 tool 失败，UI 应把对应 tool 标为 error
```

## 多模态输入

当前主链路支持图片作为结构化 `ContentBlock` 进入 provider request。TUI 支持图片 path/url，以及粘贴 data:image base64 后落盘为 attachment。音频、视频、PDF 不在当前范围。

### 消息格式

```go
type ContentBlock struct {
    Type  string    // "text" | "image"
    Text  string
    Media *MediaRef // 图片等大媒体只保存轻量引用
}

type MediaRef struct {
    Kind     string // "path" | "url" | "attachment"
    Path     string
    URL      string
    MimeType string
    Name     string
    Size     int64
}

type Message struct {
    Role    string
    Content []ContentBlock  // 当前主链路支持混合文本+图片
}
```

### TUI 中的输入方式

```
1. Ctrl+V 粘贴图片 path
   TUI 检测到 .png/.jpg/.jpeg/.webp/.gif → 询问是否加入附件
   → 确认后作为 path attachment 发送，不插入输入框

2. Ctrl+V 粘贴远程图片 URL
   TUI 检测明显图片后缀 URL → 询问是否加入附件
   → 确认后作为 url attachment 发送

3. Ctrl+V 粘贴 data:image base64
   TUI 询问是否加入附件 → 确认后保存到默认数据目录的 attachments/sha256-*.png
   → 再作为 attachment ref 发送；base64 不进入 protocol/daemon/agent memory

注: 不支持 /attach 命令、不识别裸 base64、不自动扫描普通提示词里的图片路径。
```

### 模型路由配合 — 目标设计

```
用户发送图片时:
  daemon 将 path/url/attachment 规范化为 ContentImage(MediaRef)
  main 可通过 spawn.input_images 显式把当前用户图片传给多模态 subtask
  provider 请求阶段通过 media resolver 将 MediaRef 临时转成 URL/base64，再映射到各家模型协议
```

### 文件大小和存储限制

```
protocol:
  只接受 path/url/attachment，不接受 base64/blob

media resolver:
  path 图片最大 10MB
  url 保留 URL；path/attachment 只在 provider request 阶段临时读取并编码 base64

持久化:
   working memory / conversation_state / user_memory 不保存 raw media
   只保存用户文本和附件 metadata
```

附件根目录由 `internal/config/paths.go` 的 `DefaultAttachmentsDir()` 派生，当前默认展开为 `~/.suna/attachments`。
