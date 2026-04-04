package types

// Provider selects how the agent obtains model responses.
type Provider string

const (
	ProviderAPI       Provider = "api"
	ProviderClaudeCLI Provider = "claude-cli"
)
