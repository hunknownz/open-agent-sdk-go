# Changelog

## v0.4.0

- Switched the module path to `github.com/hunknownz/open-agent-sdk-go`.
- Promoted this fork to the long-term maintained SDK mainline.
- Added a persistent `claude-cli` provider that keeps one Claude Code CLI child session per agent.
- Added Windows no-window process spawning for Claude CLI sessions.
- Added regression coverage for session reuse and `Clear()`-driven session reset.
- Updated documentation to describe API and Claude CLI provider modes and fork maintenance policy.
