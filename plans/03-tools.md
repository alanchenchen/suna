# 03 — 核心工具 (固定 9 个)

Suna 出厂只有 9 个工具，永不增长。所有更高级的能力通过 skill 系统学习获得。

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
  Spawn       委派 sub-agent (仅 main agent 可用，不经过 Guard)
```

Exec 归类为 Act 的原因：Exec 可以执行任何命令，包括删除、安装、网络操作。Guard 通过 `isReadOnlyCommand` 白名单机制，将 grep/ls/cat 等只读命令快速放行（零 LLM 审查成本），其余命令走完整审查流程。

## 逐工具详细设计

### ReadFile

```
功能: 读取文件内容，返回文本或 base64
参数: { path: string, offset?: int, limit?: int, encoding?: "text"|"base64" }
返回: { content: string, total_lines: int, truncated: bool }

边界处理:
  - 大文件: 超过 1MB 自动分页，返回前 N 行 + 提示用 offset/limit 读取更多
  - 二进制文件: encoding="base64" 返回 base64 编码
  - 不存在的文件: 返回错误 "file not found: xxx"
  - 权限不足: 返回错误 "permission denied: xxx"
  - 符号链接: 跟随链接读取实际文件

为什么是原生工具而不是 Exec("cat"):
  - 跨平台一致 (Windows 没有 cat)
  - 分页读取大文件，不爆上下文
  - 结构化返回 (行号、截断标记)
  - 自动检测编码
```

### ListDir

```
功能: 列出目录内容，返回结构化文件列表
参数: { path: string, recursive?: bool, max_depth?: int }
返回: { entries: [{ name: string, type: "file"|"dir", size: int, modified: string }] }

边界处理:
  - 空目录: 返回 { entries: [] }
  - 不存在的目录: 返回错误
  - 大目录: 超过 500 条截断，提示缩小范围
  - recursive=true 时限制 max_depth=3

为什么是原生工具而不是 Exec("ls"):
  - ls 在 Windows/macOS/Linux 格式不同
  - 结构化返回比 LLM 解析文本更可靠
  - 非程序员用户会问"桌面上有什么"，不会用 ls
```

### ReadHTTP

```
功能: 发送 HTTP GET 请求，返回响应内容
参数: { url: string, headers?: map[string]string, timeout?: int }
返回: { status: int, body: string, headers: map[string]string, truncated: bool }

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
返回: { stdout: string, stderr: string, exit_code: int, truncated: bool, shell_used: string }

经过 Guard 审查:
  Exec 是最危险的工具，所有 Exec 调用都经过 Guard
  Guard 硬规则按 OS 区分 (见 04-guard.md)

跨平台策略:
  shell 参数: "auto" (默认) | "bash" | "powershell" | "cmd"
  
  Shell = "auto" (默认):
    Windows:
      1. 检测命令语法 → bash 风格 → 找 Git Bash/WSL
      2. PowerShell 风格 → powershell.exe
      3. cmd 风格 → cmd.exe
      4. 无法判断 → 报错 "无法确定 shell，请指定 shell 参数"
    macOS/Linux:
      → bash (默认)
  
  执行时记录 shell_used 到审计日志
  Guard 基于 shell_used 做针对性审查

边界处理:
  - 超时: 默认 60 秒
  - 大输出: stdout/stderr 各截断到 50KB
  - 非零退出码: 返回 stderr 内容，不视为工具错误
  - 交互式命令: 不支持 (命令不能等待 stdin 输入)
  - 后台命令: command 末尾加 & 可后台运行，返回 pid
  - Windows 特殊处理:
    - 路径自动转换: / → \ (当 shell=cmd|powershell 时)
    - 环境变量: $VAR → %VAR% (cmd) / $env:VAR (PowerShell)

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
返回: { path: string, bytes_written: int }

经过 Guard 审查:
  - 路径检查: 不允许写入 /etc, /usr, /System 等系统目录
  - 覆盖检查: 覆盖已有文件时 Guard 会额外提示风险
  - create_dirs: true 时自动创建父目录
```

### EditFile

```
功能: 精确编辑文件的部分内容 (类似 sed 替换，但语义更清晰)
参数: { path: string, old_string: string, new_string: string, replace_all?: bool }
返回: { path: string, replacements: int }

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
返回: { status: int, body: string, headers: map }

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
功能: 创建 sub agent 执行子任务 (仅 main agent)
参数: {
  task: string,
  model?: string,
  system?: string,
  tools?: [string],
  timeout?: int,
  context?: string          // 传给 sub agent 的额外上下文
}
返回: { result: string, success: bool, error?: string }

不经过 Guard:
  sub agent 内部的 Act 操作仍然经过 Guard
  Spawn 本身只是创建了一个受限的执行环境

工具权限:
  tools 参数指定 sub agent 可用的工具列表
  如果不指定 → 默认给 ReadFile, ListDir, ReadHTTP, Exec
  Exec 在 sub agent 中仍然经过 Guard 审查（含 isReadOnlyCommand 快速放行）
  main 不能给 sub 授权 Spawn → 防止嵌套

超时:
  默认 300 秒
  sub agent 超时后自动终止，返回超时错误

并发:
  Main 可以同时发起多个 Spawn
  每个 sub agent 独立 goroutine 运行
  Main 等待所有 sub 完成后汇总
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
```

## 多模态输入

图片/音视频不是工具，而是消息内容的一部分。处理方式：

### 消息格式

```go
type ContentBlock struct {
    Type     string  // "text" | "image" | "audio"
    Text     string
    MediaURL string  // 文件路径或 URL
    MediaB64 string  // base64 编码 (小文件直接内嵌)
    MimeType string  // "image/png", "audio/mp3" 等
}

type Message struct {
    Role    string
    Content []ContentBlock  // 支持混合文本+图片+音频
}
```

### TUI 中的输入方式

```
1. 拖拽文件到终端窗口
   TUI 检测到文件拖拽事件 → 读取文件 → base64 编码
   → 作为 image/audio content block 发给模型
   → 这是主要的多模态输入方式，不需要 /file 命令

2. 剪贴板粘贴 (Ctrl+V)
   用户截图后 Ctrl+V → TUI 读取剪贴板图片
   → base64 → image content block

3. 路径引用
   用户在对话中提到文件路径 (如 "看看这张截图 ~/Desktop/screenshot.png")
   → agent 通过 ReadFile 读取 → 编码为 base64 → 注入消息
   → 纯自然语言交互，不需要额外命令

注: /file 命令已移除。所有文件输入通过拖拽、粘贴、或自然语言路径引用完成。
```

### 模型路由配合

```
用户发送图片时:
  路由层检查当前模型是否支持多模态
  - 支持 (kimi/gpt-4o/claude) → 直接使用
  - 不支持 (纯文本模型) → 自动路由到多模态模型

用户发送音频时:
  大部分模型不支持直接处理音频
  → agent 检查是否有音频转写能力 (Whisper skill)
  → 有: 先转写为文本，再处理
  → 没有: 建议用户安装音频转写能力，或路由到支持音频的模型
```

### 文件大小限制

```
内嵌 base64:
  图片: 最大 10MB (base64 后约 13MB)
  音频: 最大 20MB
  超过 → 写入临时文件 → 传文件路径 → 模型从 URL 读取 (如果支持)

注意: 大文件会大量消耗 token
  agent 应在 system prompt 中被引导:
  "如果用户发送了大图片，先确认是否需要分析全图，
   还是只需要分析某个局部。如果只需要局部，建议裁剪后发送。"
```
