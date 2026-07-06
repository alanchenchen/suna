# Suna

> Local-first agent runtime with isolated subtasks, intent-aware Guard, memory, Skills, MCP, and a terminal UI.

[中文 README](README.zh-CN.md) · [Documentation](docs/README.md) · [stdio runtime](docs/runtime-stdio.md) · [Subtasks](docs/subtask.md)

Suna is not just another terminal chat agent. It is a local agent runtime where a main agent can delegate work to **isolated subtasks** with different models, explicit context, selected images, and a per-task tool whitelist — while risky actions still go through an **intent-aware Guard**.

The built-in TUI is the default client. The same runtime can also be used by third-party desktop apps, IDE extensions, local web UIs, or scripts through `suna runtime --transport stdio` and JSON-RPC/NDJSON.

> Suna is under active development. If an upgrade breaks local state, update to the latest release first and back up important data before removing `.db` files under the Suna data directory.

## Why Suna?

### Isolated Subtasks

Most terminal agents run one model with one shared context and one shared toolset. Suna lets the main agent create bounded workers at runtime:

- choose a different model for a specific subtask;
- pass only explicit task/context/images;
- grant only selected tools, or no tools at all;
- keep user memory and main conversation history out of the subtask by default;
- return structured status, result text, error, and side-effect disclosure to the main agent.

This makes delegation explainable and auditable instead of becoming an uncontrolled second agent.

### Intent-aware Guard

Suna's Guard is not just an approve/deny popup. In `smart` mode, hard safety rules and workspace boundaries stay enforced, while an LLM review can judge whether a medium/high-risk action is safe and aligned with the user's intent. If the review is unavailable or uncertain, Suna falls back to user confirmation.

### Runtime-first architecture

Suna separates UI from agent runtime:

```text
Built-in TUI / third-party UI / script
        ↓ JSON-RPC over local or stdio transport
Daemon / runtime
        ↓
Main Agent / Runner
   ├─ model providers
   ├─ tools / Guard / memory / Skills / MCP
   └─ isolated subtasks
        ├─ explicit model
        ├─ explicit context
        └─ explicit tool permissions
```

The TUI is only one client. The daemon/runtime owns model calls, tools, Guard, memory, Skills, MCP, attachments, usage, and session state.

## Who is Suna for?

Suna is for people who want a local AI workbench that can work with files, documents, code, commands, APIs, images, and custom tools — while keeping risky actions reviewable.

It may fit you if you want:

- a terminal AI assistant that understands local context;
- safer file and command operations with Guard;
- isolated subtasks instead of one giant shared context;
- a runtime that can be reused by Web UIs, IDEs, desktop apps, or scripts;
- memory, Skills, MCP, and tools under your own control.

## Common workflows

Use Suna to:

- organize notes, meeting records, and research material;
- read a local folder and summarize what matters;
- compare options and ask a subtask for an independent second opinion;
- clean up documents or config files with confirmation before writing;
- run local commands for diagnostics, tests, builds, and automation;
- call HTTP APIs and turn responses into readable summaries;
- create reusable Skills for repeated workflows;
- connect external tools through MCP or third-party clients through stdio runtime.

## What makes Suna different?

| Capability | Typical terminal agent | Suna |
|---|---|---|
| Model choice | One active model per run | Main agent can spawn subtasks on different models |
| Subagent context | Shared or implicit | Explicit, isolated context only |
| Tool permissions | Usually global | Per-subtask tool whitelist |
| User memory | Often mixed into the whole context | Lightweight profile memory near the latest user input |
| Safety | Confirm, auto, or coarse allowlist | Intent-aware Guard plus hard workspace/sensitive-path rules |
| Skill lifecycle | Prompt/file injection | Static check, optional LLM review, then user confirmation |
| UI architecture | CLI/TUI app | Local daemon/runtime plus protocol plus TUI client |
| Third-party UI | Usually not a stable boundary | `suna runtime --transport stdio` with JSON-RPC/NDJSON |

## Install

Download a prebuilt binary from [GitHub Releases](https://github.com/alanchenchen/suna/releases):

- macOS Apple Silicon: `suna-darwin-arm64.zip`
- macOS Intel: `suna-darwin-amd64.zip`
- Linux x86_64: `suna-linux-amd64.tar.gz`
- Linux arm64: `suna-linux-arm64.tar.gz`
- Windows x86_64: `suna-windows-amd64.zip`

Put `suna` (or `suna.exe` on Windows) on your `PATH`, then run:

```bash
suna
```

If you have Go installed, you can also install from source:

```bash
go install github.com/alanchenchen/suna@latest
suna
```

Make sure your Go bin directory is on `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Do not use `go run .` to launch Suna. The daemon/TUI process manager depends on a stable executable path.

## Update

Quit the TUI, then run:

```bash
suna update
```

`update` checks the latest release, shows release notes, downloads the matching asset, verifies checksums, and replaces the current binary after confirmation.

If the daemon is still running, stop it first:

```bash
suna stop
```

## First run

1. Start `suna`.
2. Open Config / Setup if no model is configured.
3. Add a model connection.
4. Choose a provider protocol:
   - **OpenAI**: OpenAI Responses API.
   - **Anthropic**: Anthropic Messages API.
   - **OpenAI Compatible**: OpenAI Chat Completions compatible providers or gateways.
5. Fill in model name, endpoint, API key, `context_window`, `max_output_tokens`, and optional capability labels.
6. Activate the model and start a new conversation.

You can change most settings from the TUI with `/config`. `context_window` and `max_output_tokens` must match the real limits of your model provider. `strengths` tell the main agent what a model is good at. `subtask_for` optionally controls which main models may see a model as a subtask candidate.

## Try these prompts

These prompts are intentionally simple. They are meant to show how Suna delegates work, limits tool access, and asks before risky actions.

### 1. Get a second opinion

```text
I need to decide between two options. First, ask a subtask to give an independent second opinion without using any tools. Then give me your final recommendation.
```

You should see a `spawn` tool call where the subtask has no tools and only receives the context the main agent explicitly passes to it.

### 2. Explore local files safely

```text
Help me understand the documents in this folder. You can ask a read-only subtask to look around with listdir, readfile, and search, then summarize the important points for me.
```

You should see the main agent grant only read/search tools to the subtask and keep final judgment in the main conversation.

### 3. Make a small change with confirmation

```text
Please clean up this note and save the improved version. Before writing anything, tell me what you plan to change and let me confirm it.
```

You should see file edits go through Suna's Guard instead of being silently executed.

## What can Suna do?

```text
Organize notes, meeting records, and research material
Compare options and turn rough ideas into a practical plan
Read a local folder and summarize what matters
Analyze screenshots or pasted images
Call HTTP APIs and turn responses into readable summaries
Check documents, configs, or scripts for conflicts and risks
Modify files with exact edits and explain the impact
Run commands for diagnostics, tests, builds, and automation
Search local files by path, symbol-like entries, or content
Use MCP tools from configured stdio servers
Create, review, and enable Skills
Compact long sessions into Session State
Delegate bounded subtasks to other configured models
```

Action tools such as writing files, running commands, filesystem operations, and HTTP write requests go through Guard.

## Built-in tools

| Category | Tool | Purpose |
|---|---|---|
| Perception | `readfile` | Read files by line range, tail, or base64 |
| Perception | `listdir` | List directories with recursion, pagination, include/exclude filters |
| Perception | `search` | Structured local search across paths, headings/symbol-like entries, and content |
| Action | `exec` | Run shell commands for diagnostics, tests, builds, and system operations |
| Action | `writefile` | Create, overwrite, or append files |
| Action | `editfile` | Atomically apply exact text replacements to a single file |
| Action | `filesystem` | `stat`, `mkdir`, `move`, `copy`, or `remove` paths |
| Action | `http` | Send HTTP requests; read methods are lower risk than write methods |

Tools are exposed through a stable provider system. Guard decisions are handled by the Agent, not by UI code or ad-hoc tool wrappers.

## TUI quick reference

Common keys:

```text
Enter              Send / confirm
Shift+Enter        Newline
Ctrl+J             Newline
Esc                Cancel run, go back, or close overlay
Ctrl+Y             Toggle copy mode
Ctrl+T             Toggle tool detail
Ctrl+R             Toggle reasoning detail
?                  Toggle help
PgUp / PgDn        Scroll
Ctrl+V             Paste text; if no terminal text is provided and clipboard has an image, attach image
Ctrl+C             Quit
```

Common slash commands:

```text
/new              New conversation
/model            Open model picker
/model <ref>      Switch model, e.g. /model openai/gpt-4o-mini
/memory           View user profile memory
/mcp              Open MCP panel
/skills           Open Skill panel
/compact          Manually compact current context
/config           Open configuration
/help             Open help
```

Unknown `/text` input is sent as a normal message.

## Safety boundary

Guard modes:

```text
ask       Ask for confirmation on risky actions
smart     Use LLM review for intent-aware safety, then confirm/reject/fallback when needed
auto      Allow actions except hard-blocked ones
readonly  Only allow read-only actions
```

Workspace is an optional directory boundary. When set, local file and command operations are limited to that workspace. Suna's own data directory remains accessible for configuration, logs, attachments, and Skills, while credentials and other sensitive paths are still blocked by built-in rules.

Workspace, Guard, Skills, and MCP are not an OS sandbox. External commands and MCP servers still run with their process permissions. Only enable tools and servers you trust.

## Data directory

Default data directory:

```text
~/.suna/config.toml        # main config
~/.suna/credentials.toml   # API keys
~/.suna/memory.db          # memory, session state, usage
~/.suna/skills/            # Skills
~/.suna/attachments/       # images and binary attachments
~/.suna/logs/app.log       # logs
```

For troubleshooting, check `~/.suna/logs/app.log` first.

## Runtime for third-party clients

Suna can run as a headless runtime over stdio:

```bash
suna runtime --transport stdio
```

This is intended for third-party UIs, desktop apps, IDE plugins, local web services, and scripts. The protocol is JSON-RPC-style NDJSON.

Start here:

- [stdio runtime guide](docs/runtime-stdio.md)
- [Protocol reference](docs/protocol.md)
- [Configuration reference](docs/configuration.md)

## Documentation

- [Documentation index](docs/README.md)
- [Subtask design](docs/subtask.md)
- [Architecture](docs/architecture.md)
- [Design notes](docs/design.md)
- [Performance](docs/performance.md)
- [Code map](docs/code-map.md)
- [Current implementation](docs/current-implementation.md)
- [Development guide](docs/development.md)
- [Chinese README](README.zh-CN.md)

## Current boundaries

Do not rely on these as complete product features yet:

- triggers, scheduled jobs, file watching, or proactive perception;
- multi-session management UI, full history search, vector memory, or knowledge base;
- full MCP support beyond tools-only stdio runtime;
- Skill sandbox, Skill marketplace, or complex lifecycle hooks;
- complete cost accounting and provider price calculation;
- full OS sandbox;
- complete event replay after TUI disconnects during a running task.

## License

Suna is released under the [PolyForm Noncommercial License 1.0.0](LICENSE).

You may use, study, modify, and distribute Suna for noncommercial purposes. Commercial use requires separate permission from the copyright holder. When distributing original or modified versions, keep the license terms and required notice.
