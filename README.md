# Open Agent SDK (Go)

`open-agent-sdk-go` is our long-term Go SDK for embedding agent loops in local apps and services.
It supports two model backends:

- `api`: standard Anthropic-compatible Messages API
- `claude-cli`: a persistent local Claude Code CLI session using NDJSON structured I/O

This fork is maintained at `github.com/hunknownz/open-agent-sdk-go`.

## Why This Fork Exists

We are maintaining this SDK as a reusable agent framework rather than a one-off game integration.
Compared with upstream, this fork currently adds and maintains:

- A first-class `claude-cli` provider
- A persistent Claude CLI session per agent instance
- Windows no-window process spawning for CLI sessions
- Structured decision and local-runtime integrations used by `spire2mind`
- Ongoing compatibility fixes and Windows-focused developer workflow support

## Installation

```bash
go get github.com/hunknownz/open-agent-sdk-go
```

## Provider Modes

### API Provider

Use the standard Messages API backend when you have an API key and base URL.

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

The CLI is launched with the same structured I/O shape used by `research/claude-code`:

- `--print`
- `--input-format stream-json`
- `--output-format stream-json`
- `--replay-user-messages`
- `--verbose`

On Windows the SDK starts the child with `HideWindow + CREATE_NO_WINDOW`, so normal runtime use does not need an extra visible console window.

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
- Built-in tools for files, shell, search, web access, and subagents
- MCP client support for `stdio`, `http`, and `sse`
- Permission callbacks and allow/deny policies
- Hook support for pre/post tool execution and post-sampling
- Cost tracking and conversation history helpers
- Structured output via JSON schema
- Persistent Claude Code CLI backend support

## Repository Layout

```text
open-agent-sdk-go/
  agent/         agent loop, provider integration, session management
  api/           Anthropic-compatible Messages API client
  context/       system and workspace prompt context helpers
  costtracker/   token and cost accounting
  history/       JSONL conversation history helpers
  hooks/         hook registration and execution
  mcp/           MCP client and transport support
  permissions/   permission rules and path validation
  tools/         built-in tools and execution registry
  types/         shared message, tool, provider, and usage types
  examples/      runnable examples
```

## Examples

```bash
go run ./examples/01-simple-query/
go run ./examples/04-prompt-api/
go run ./examples/09-subagents/
```

## Fork Maintenance

This repository is maintained with:

- `origin = git@github.com:hunknownz/open-agent-sdk-go.git`
- `upstream = https://github.com/codeany-ai/open-agent-sdk-go`

We develop directly on `main` and selectively pull from upstream when useful.

## License

MIT. See `LICENSE`.
