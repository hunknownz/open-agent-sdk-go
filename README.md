# Open Agent SDK (Go)

`open-agent-sdk-go` is our long-term Go SDK for embedding agent loops in local apps and services.
This fork is maintained at `github.com/hunknownz/open-agent-sdk-go` and is developed directly on `main`.

The SDK supports two runtime model backends:

- `api`: standard Anthropic-compatible and OpenAI-compatible HTTP APIs
- `claude-cli`: a persistent local Claude Code CLI session using NDJSON structured I/O

## What This Fork Adds

Compared with upstream, this fork currently adds and maintains:

- A first-class `claude-cli` provider
- A persistent Claude CLI child session per `agent.Agent`
- Structured control-plane handling for Claude CLI `control_request`, `control_response`, env refresh, and cancellation
- Windows no-window process spawning for CLI sessions
- Local-runtime integrations used by `spire2mind`
- Ongoing compatibility, Windows, and developer workflow fixes

It also preserves the broader feature work already present on this fork's `main` branch:

- Multi-provider API support for Anthropic and OpenAI-compatible backends
- Extended thinking and effort levels
- Fallback model support
- Session management helpers
- Rate limit tracking and context usage helpers
- File checkpointing, sandboxing, plugins, and expanded built-in tools

## Installation

```bash
go get github.com/hunknownz/open-agent-sdk-go
```

## Provider Modes

### API Provider

Use the standard HTTP provider when you have an API key and base URL.
The API client auto-detects Anthropic vs OpenAI-compatible wire format when possible.

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/hunknownz/open-agent-sdk-go/agent"
    "github.com/hunknownz/open-agent-sdk-go/types"
)

func main() {
    a := agent.New(agent.Options{
        Provider: types.ProviderAPI,
        Model:    "sonnet-4-6",
        APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
        BaseURL:  os.Getenv("ANTHROPIC_BASE_URL"),
    })
    defer a.Close()

    result, err := a.Prompt(context.Background(), "Reply with OK only.")
    if err != nil {
        panic(err)
    }
    fmt.Println(result.Text)
}
```

You can also point the API provider at OpenAI-compatible services:

```go
a := agent.New(agent.Options{
    Provider:    types.ProviderAPI,
    Model:       "gpt-4o",
    APIKey:      os.Getenv("OPENAI_API_KEY"),
    BaseURL:     "https://api.openai.com",
    APIProvider: "openai",
})
```

### Claude CLI Provider

Use the local Claude Code CLI when you want to reuse Claude Code authentication and subscription-backed local usage instead of direct API billing.

```go
package main

import (
    "context"
    "fmt"

    "github.com/hunknownz/open-agent-sdk-go/agent"
    "github.com/hunknownz/open-agent-sdk-go/types"
)

func main() {
    a := agent.New(agent.Options{
        Provider:   types.ProviderClaudeCLI,
        Model:      "claude-sonnet-4-6",
        CLICommand: "claude",
    })
    defer a.Close()

    if err := a.Init(context.Background()); err != nil {
        panic(err)
    }

    result, err := a.Prompt(context.Background(), "Reply with OK only.")
    if err != nil {
        panic(err)
    }
    fmt.Println(result.Text)
}
```

## Claude CLI Session Lifecycle

The `claude-cli` provider is session-based rather than one-shot.

- Each `agent.Agent` owns one persistent Claude CLI child process.
- `Init()` prewarms the session.
- If `Init()` is skipped, the first `Query()` or `Prompt()` lazily starts the session.
- `Clear()` resets the in-memory conversation and restarts the CLI session.
- `Close()` shuts down the child process and associated goroutines.
- `UpdateEnv()` pushes runtime environment updates into the live Claude CLI session using `update_environment_variables`.

The CLI is launched with the same structured I/O shape used by `research/claude-code`:

- `--print`
- `--input-format stream-json`
- `--output-format stream-json`
- `--replay-user-messages`
- `--verbose`

On Windows the SDK starts the child with `HideWindow + CREATE_NO_WINDOW`, so normal runtime use does not need an extra visible console window.

## Claude CLI Control Plane

The Claude CLI backend now carries a lightweight control plane modeled after `research/claude-code`'s structured I/O transport.

- The child process is read by a persistent background session reader instead of a per-turn blocking scanner.
- `control_request` messages are parsed and answered without hanging the turn loop.
- `can_use_tool` requests are resolved through the SDK's existing tool registry and `CanUseTool` callback when possible.
- Unsupported control subtypes return explicit `control_response` errors instead of stalling.
- Runtime environment changes can be pushed into the live child process via `UpdateEnv()`.
- Pending control work is canceled when a turn or session is torn down, and the SDK emits `control_cancel_request` back to the child.

## Environment Behavior

The API provider reads these environment variables when fields are omitted from `agent.Options`:

- `CODEANY_API_KEY`, `ANTHROPIC_API_KEY`
- `CODEANY_BASE_URL`, `ANTHROPIC_BASE_URL`
- `CODEANY_MODEL`, `ANTHROPIC_MODEL`
- `CODEANY_CUSTOM_HEADERS`, `ANTHROPIC_CUSTOM_HEADERS`

The Claude CLI provider intentionally scrubs API-oriented variables from the child process so that the local CLI continues using Claude Code login state instead of accidentally switching to direct API billing.

By default it also preserves these Claude Code runtime toggles:

- `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
- `CLAUDE_CODE_ATTRIBUTION_HEADER=0`

## Core Features

- Streaming agent loop with tool execution and multi-turn conversations
- Built-in tools for files, shell, search, web access, subagents, plan/todo/team utilities, MCP resources, cron, config, notebook editing, and more
- MCP client support for `stdio`, `http`, and `sse`, plus in-process SDK server support
- Permission callbacks, allow/deny policies, and directory validation
- Hook support for pre/post tool execution, notifications, permission requests, and post-sampling
- Extended thinking with explicit configuration and effort levels
- Fallback model support for API-mode retries
- Session management helpers
- Rate limit tracking and context usage accounting
- File checkpointing and sandbox utilities
- Plugin loading support
- Persistent Claude Code CLI backend support

## Additional Capabilities

### Extended Thinking and Effort

```go
a := agent.New(agent.Options{
    Thinking: &agent.ThinkingConfig{
        Type:         agent.ThinkingEnabled,
        BudgetTokens: 10000,
    },
})

a = agent.New(agent.Options{
    Effort: agent.EffortHigh,
})
```

### Fallback Model

```go
a := agent.New(agent.Options{
    Model:         "opus-4-6",
    FallbackModel: "sonnet-4-6",
})
```

### Subagents

```go
a := agent.New(agent.Options{
    Agents: map[string]agent.AgentDefinition{
        "researcher": {
            Description:  "Research agent for deep analysis",
            Instructions: "You are a research specialist...",
            Model:        "opus-4-6",
            Tools:        []string{"Read", "Glob", "Grep", "WebSearch"},
            MaxTurns:     20,
            Effort:       agent.EffortHigh,
        },
    },
})
```

### Session Management

```go
import "github.com/hunknownz/open-agent-sdk-go/session"

mgr := session.NewManager("")
sessions, _ := mgr.ListSessions("my-project")
messages, _ := mgr.GetSessionMessages(sessions[0].SessionID)
```

### Hooks

```go
a := agent.New(agent.Options{
    Hooks: hooks.HookConfig{
        PreToolUse: []hooks.HookRule{{
            Matcher: "Bash",
            Hooks: []hooks.HookFn{
                func(ctx context.Context, tool string, input map[string]interface{}) (string, error) {
                    return "", nil
                },
            },
        }},
    },
})
```

### Permissions

```go
config := &permissions.Config{
    Mode: types.PermissionModeDefault,
    AllowRules: []permissions.Rule{{ToolName: "Read"}},
}
```

### In-Process MCP SDK Server

```go
server := mcp.NewSdkServer("my-tools", "1.0.0")
```

## Repository Layout

```text
open-agent-sdk-go/
  agent/         agent loop, provider integration, session management
  api/           Anthropic-compatible and OpenAI-compatible HTTP clients
  checkpoint/    file checkpointing and rewind helpers
  context/       system and workspace prompt context helpers
  contextusage/  context window accounting
  costtracker/   token and cost accounting
  history/       JSONL conversation history helpers
  hooks/         hook registration and execution
  mcp/           MCP client, SDK server, and transport support
  permissions/   permission rules and path validation
  plugins/       plugin discovery and loading
  ratelimit/     API rate limit tracking
  sandbox/       sandbox policy helpers
  session/       session inspection and management
  tools/         built-in tools and execution registry
  types/         shared message, tool, provider, and usage types
  examples/      runnable examples
```

## Examples

```bash
go run ./examples/01-simple-query/
go run ./examples/04-prompt-api/
go run ./examples/09-subagents/
go run ./examples/web/
```

## Fork Maintenance

This repository is maintained with:

- `origin = hunknownz/open-agent-sdk-go` via SSH or HTTPS
- `upstream = https://github.com/codeany-ai/open-agent-sdk-go`

We develop directly on `main` and selectively pull from upstream when useful.

## Links

- Upstream reference: [github.com/codeany-ai/open-agent-sdk-go](https://github.com/codeany-ai/open-agent-sdk-go)
- TypeScript SDK: [github.com/codeany-ai/open-agent-sdk-typescript](https://github.com/codeany-ai/open-agent-sdk-typescript)
- Fork issues: [github.com/hunknownz/open-agent-sdk-go/issues](https://github.com/hunknownz/open-agent-sdk-go/issues)

## License

MIT. See `LICENSE`.
