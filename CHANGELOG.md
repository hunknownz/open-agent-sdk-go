# Changelog

## v0.4.1

- Added a background Claude CLI session reader so persistent sessions no longer depend on a per-turn blocking scanner.
- Added structured control-plane support for `control_request`, `control_response`, `control_cancel_request`, and runtime `update_environment_variables`.
- Added `Agent.UpdateEnv()` to refresh a live Claude CLI child process without restarting the session.
- Added regression coverage for env refresh, control request handling, and control cancellation.

## v0.4.0

- Switched the module path to `github.com/hunknownz/open-agent-sdk-go`.
- Promoted this fork to the long-term maintained SDK mainline.
- Added a persistent `claude-cli` provider that keeps one Claude Code CLI child session per agent.
- Added Windows no-window process spawning for Claude CLI sessions.
- Added regression coverage for session reuse and `Clear()`-driven session reset.
- Updated documentation to describe API and Claude CLI provider modes and fork maintenance policy.
