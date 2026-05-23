# 03 — 核心工具 (固定 9 个)

Suna 当前对模型暴露 9 个固定工具定义：7 个 registry tools（readfile/listdir/readhttp/exec/writefile/editfile/writehttp）+ 2 个 agent built-ins（askuser/spawn）。`askuser` 和 `spawn` 由 `internal/agent` 特殊处理，不注册到通用 tool registry。所有更高级的能力通过 skill 系统学习获得。

工具返回在实现上统一为 `tool.Result{Content string, IsError bool, Truncated bool}`。下面的“返回”描述以当前 LLM 实际看到的文本内容为准；`truncated` 是内部结构化标记，不代表每个工具都会返回 JSON 对象。

## 设计原则

```
1. Perceive 工具不需要确认，可以直接使用
2. Act 工具必须经过 Guard 审查
3. 能用 Exec 做的事，不单独加工具
4. 不加的工具: grep/glob/find → Exec, 进程查看 → Exec("ps"), 压缩 → Exec("tar")
5. 图片/音视频理解 → 模型 multimodal content，不是工具
```

## 工具分类

```
Perceive (感知) — 不需要 Guard 审查
  ReadFile    读文件内容
  ListDir     列目录内容
  ReadHTTP    HTTP GET 请求

Act (行动) — 必须经过 Guard 审查
  Exec        执行命令 (万能逃逸口，最危险的工具)
  WriteFile   创建/覆盖文件
  EditFile    精确编辑文件部分内容
  WriteHTTP   HTTP POST/PUT/DELETE 请求

Communicate (协作) — 特殊处理
  AskUser     向用户提问/确认 (不经过 Guard)
  Spawn       委派 subtask (仅 main agent 可用，不经过 Guard)
```

Exec 归类为 Act 的原因：Exec 可以执行任何命令，包括删除、安装、网络操作。Guard 通过轻量 shell analyzer 将可证明只读的 grep/ls/cat/git status 等命令归为 low risk（零 LLM 审查成本）；复杂/动态/未知命令至少归为 medium risk，高危命令归为 high risk 或命中硬拦截。

## 逐工具详细设计

### ReadFile

```
功能: 读取文件内容，返回文本或 base64
参数: { path: string, offset?: int, limit?: int, encoding?: "text"|"base64" }
返回: 带行号的文本内容；内部标记 truncated

边界处理:
  - 文本大文件: 读取上限 100KB，超过后截断，并提示用 offset/limit 读取更多
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
参数: { path: string, recursive?: bool, max_depth?: int }
返回: 每行一个 entry，包含类型、路径、大小和修改时间；内部标记 truncated

边界处理:
  - 空目录: 返回空列表文本
  - 不存在的目录: 返回错误
  - 大目录: 超过 500 条截断，提示缩小范围
  - recursive=true 时限制 max_depth=3

为什么是原生工具而不是 Exec("ls"):
  - ls 在 Windows/macOS/Linux 格式不同
  - 稳定格式比各平台 ls 输出更可靠
  - 非程序员用户会问"桌面上有什么"，不会用 ls
```

### ReadHTTP

```
功能: 发送 HTTP GET 请求，返回响应内容
参数: { url: string, headers?: map[string]string, timeout?: int }
返回: 文本格式响应，包含 status/body 等信息；内部标记 truncated

边界处理:
  - 超时: 默认 30 秒
  - 大响应: 超过 100KB 截断
  - JSON 响应: 直接返回
  - HTML 响应: 返回原始 HTML（LLM 自行解析）
  - 重定向: 自动跟随 (最多 5 次)
  - 4xx/5xx: 返回状态码和错误信息，不视为工具错误

不经过 Guard:
  GET 是幂等读取，不会修改任何东西
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
功能: 创建或覆盖文件
参数: { path: string, content: string, create_dirs?: bool }
返回: 文本结果，说明写入路径和字节数

经过 Guard 审查:
  - 路径检查: 不允许写入 /etc, /usr, /System 等系统目录
  - 覆盖检查: 覆盖已有文件时 Guard 会额外提示风险
  - create_dirs: true 时自动创建父目录
```

### EditFile

```
功能: 精确编辑文件的部分内容 (类似 sed 替换，但语义更清晰)
参数: { path: string, old_string: string, new_string: string, replace_all?: bool }
返回: 文本结果，说明修改路径和替换次数

经过 Guard 审查:
  同 WriteFile 的路径检查

匹配逻辑:
  - old_string 必须在文件中精确匹配 (区分大小写、空白)
  - 多次匹配时: replace_all=false 报错，replace_all=true 全部替换
  - 不匹配: 返回错误 "old_string not found in file"

为什么需要 EditFile 而不全用 WriteFile:
  - 精确修改几行，不需要重写整个文件
  - LLM 可以只看需要改的部分，减少上下文占用
  - 减少误覆盖风险 (只改该改的)
```

### WriteHTTP

```
功能: 发送 HTTP POST/PUT/DELETE/PATCH 请求
参数: { method: string, url: string, headers?: map, body?: string, timeout?: int }
返回: 文本格式响应，包含 status/body 等信息；内部标记 truncated

经过 Guard 审查:
  写操作可能修改外部服务 (发邮件、下订单、删数据)
  Guard 会检查 URL 和 body 内容

边界处理:
  - 同 ReadHTTP 的超时、截断策略
  - body 支持任意 Content-Type
```

### AskUser

```
功能: 向用户提问并等待回复
参数: { question: string, options?: [string] }
返回: { answer: string }

不经过 Guard:
  这是纯交互工具，不涉及任何系统操作

使用场景:
  - 需要用户确认: "确定要删除 xxx 吗？"
  - 需要额外信息: "请提供数据库连接地址"
  - 选择分支: "有 3 种方案，你倾向哪种？"
  - 能力学习: "我发现我缺少 XXX 能力，你希望我学习吗？"

options 参数:
  提供 options 时，TUI 渲染为选择列表
  不提供时，TUI 渲染为开放式输入框
```

### Spawn

```
功能: 创建 subtask 执行子任务 (仅 main agent)
参数: {
  task: string,              // 必填
  model: string,             // 必填: subtask 使用的模型 ref (provider/model)
  tools: [string],           // 必填: subtask 可用工具列表
  timeout?: int,             // 默认 300 秒
  context?: string,          // 传给 subtask 的额外上下文
  system?: string            // 可选 fallback；正常由 subtask_system.md 模板生成
}
返回: JSON 文本 { result: string, success: bool, status: string }

不经过 Guard:
  subtask 内部的 Act 操作仍然经过 Guard
  Spawn 本身只是创建了一个受限的执行环境

工具权限:
  tools 参数必填，指定 subtask 可用的工具列表
  没有"默认工具集" — 缺少 tools 会返回错误，让 main LLM 重选
  Exec 在 subtask 中仍然经过 Guard 审查（含轻量 shell analyzer 的只读快速放行）
  subtask 禁止授予 askuser 和 spawn — 防止交互逃逸和嵌套
  daemon 校验每个 tool name: 空/不存在/spawn/askuser 都返回 tool error

模型选择:
  model 必填，指定 subtask 使用的模型 ref
  daemon 校验 model ref 是否为已配置模型
  model 为空或非法 → 返回 tool error，main LLM 重新选择

系统提示词:
  subtask 使用独立 subtask_system.md，不继承 main system.md、active memory、main working memory 或 conversation history
  subtask_system.md 只含 task/env/tools/context/rules，并明确 one-way data flow
  通过 runner request 的 System 字段注入

上下文隔离:
  spawn request 只把 task/context/tools/model 显式传入 subtask
  subtask 的 tool events 会转发给 TUI 用于可观察性，但不会成为 main 对话继承给 subtask 的共享上下文
  subtask 返回 final result/status 给 main，不保存独立长期记忆

超时:
  默认 300 秒
  subtask 超时后自动终止，返回超时错误

并发:
  Main 可以同时发起多个 Spawn
  每个 subtask 独立 goroutine 运行
  Main 等待所有 sub 完成后汇总

Guard 策略:
  subtask 使用全局 Guard policy、blocked/allowed、audit DB
  需要用户确认时通过 main agent 事件流暂停并等待确认
  smart mode 下可使用同一 LLM reviewer

事件与 usage:
  subtask 不对外发送 stream/reasoning
  subtask 的 tool call/tool result 通过 main agent 事件流转发给 TUI
  subtask 的 usage 计入 main session
```

## 工具定义的 JSON Schema

每个工具提供标准 JSON Schema，供模型 tool calling 使用。格式遵循 OpenAI function calling 规范（Suna 内部统一格式，Provider 层负责转换为各厂商格式）。

## 工具结果的大小控制

```
所有工具返回的内容都有上限:
  ReadFile:   100KB
  ListDir:    500 条
  ReadHTTP:   100KB
  Exec:       stdout/stderr 各 50KB
  WriteHTTP:  100KB

超过上限:
  截断 + truncated=true 标记
  LLM 看到 truncated 后可以决定:
    - 用 offset/limit 分页读取
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
  - params/result 默认不在聊天主线展开，只进入详情面板
  - tool error 需要在主线显示短错误摘要
  - Guard reject 视为该 tool 失败，UI 应把对应 tool 标为 error
```

## 多模态输入

当前主链路支持图片作为结构化 `ContentBlock` 进入 provider request。TUI MVP 只支持图片 path/url；音频、视频、PDF 不在当前范围。

### 消息格式

```go
type ContentBlock struct {
    Type     string  // "text" | "image" | "audio"
    Text     string
    MediaURL string  // 远程 URL
    MediaB64 string  // daemon 内部读取 path 后生成，不能进入 protocol 或持久化存储
    MimeType string  // "image/png", "audio/mp3" 等
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
   TUI 询问是否加入附件 → 确认后保存到 ~/.suna/tmp/paste-*.png
   → 再作为 path attachment 发送；base64 不进入 protocol

注: 不支持 /attach 命令、不识别裸 base64、不自动扫描普通提示词里的图片路径。
```

### 模型路由配合 — 目标设计

```
用户发送图片时:
  daemon 将 path/url 规范化为 ContentImage
  provider 将 ContentImage 转成各家模型的 image block
  如果当前模型不支持图片，应在 daemon/provider 层报错或后续路由到支持多模态的模型
```

### 文件大小和存储限制

```
protocol:
  只接受 path/url，不接受 base64/blob

daemon:
  path 图片最大 10MB
  path -> 读取文件 -> 短生命周期 base64 -> provider request

持久化:
  working memory / conversation_state / user_memory 不保存 raw media
  只保存用户文本和附件 metadata
```
