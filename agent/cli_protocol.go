package agent

import (
	"encoding/json"
	"strings"
)

var cliScrubbedEnvKeys = map[string]struct{}{
	"ANTHROPIC_API_KEY":        {},
	"ANTHROPIC_AUTH_TOKEN":     {},
	"ANTHROPIC_BASE_URL":       {},
	"ANTHROPIC_CUSTOM_HEADERS": {},
	"ANTHROPIC_MODEL":          {},
	"CODEANY_API_KEY":          {},
	"CODEANY_AUTH_TOKEN":       {},
	"CODEANY_BASE_URL":         {},
	"CODEANY_CUSTOM_HEADERS":   {},
	"CODEANY_MODEL":            {},
	"SPIRE2MIND_API_KEY":       {},
	"SPIRE2MIND_API_BASE_URL":  {},
	"SPIRE2MIND_MODEL":         {},
}

type cliStreamMessage struct {
	Type             string                    `json:"type"`
	Subtype          string                    `json:"subtype,omitempty"`
	RequestID        string                    `json:"request_id,omitempty"`
	Request          *cliControlRequestPayload `json:"request,omitempty"`
	Message          json.RawMessage           `json:"message,omitempty"`
	Result           string                    `json:"result,omitempty"`
	StructuredOutput interface{}               `json:"structured_output,omitempty"`
	TotalCostUSD     float64                   `json:"total_cost_usd,omitempty"`
	NumTurns         int                       `json:"num_turns,omitempty"`
	Usage            interface{}               `json:"usage,omitempty"`
	IsError          bool                      `json:"is_error,omitempty"`
	Errors           []string                  `json:"errors,omitempty"`
	Variables        map[string]string         `json:"variables,omitempty"`
}

type cliControlRequestPayload struct {
	Subtype               string                   `json:"subtype,omitempty"`
	ToolName              string                   `json:"tool_name,omitempty"`
	Input                 map[string]interface{}   `json:"input,omitempty"`
	PermissionSuggestions []map[string]interface{} `json:"permission_suggestions,omitempty"`
	BlockedPath           string                   `json:"blocked_path,omitempty"`
	DecisionReason        string                   `json:"decision_reason,omitempty"`
	Title                 string                   `json:"title,omitempty"`
	DisplayName           string                   `json:"display_name,omitempty"`
	ToolUseID             string                   `json:"tool_use_id,omitempty"`
	AgentID               string                   `json:"agent_id,omitempty"`
	Description           string                   `json:"description,omitempty"`
	CallbackID            string                   `json:"callback_id,omitempty"`
	ServerName            string                   `json:"server_name,omitempty"`
	MCPServerName         string                   `json:"mcp_server_name,omitempty"`
	Message               string                   `json:"message,omitempty"`
	RequestedSchema       map[string]interface{}   `json:"requested_schema,omitempty"`
	Mode                  string                   `json:"mode,omitempty"`
	URL                   string                   `json:"url,omitempty"`
	ElicitationID         string                   `json:"elicitation_id,omitempty"`
	Model                 string                   `json:"model,omitempty"`
	MaxThinkingTokens     *int                     `json:"max_thinking_tokens,omitempty"`
	Settings              map[string]interface{}   `json:"settings,omitempty"`
	Raw                   map[string]interface{}   `json:"-"`
}

func (p *cliControlRequestPayload) UnmarshalJSON(data []byte) error {
	type payloadAlias cliControlRequestPayload

	var decoded payloadAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		decoded.Raw = raw
	}

	*p = cliControlRequestPayload(decoded)
	return nil
}

type cliControlResponseEnvelope struct {
	Type     string                    `json:"type"`
	Response cliControlResponsePayload `json:"response"`
}

type cliControlResponsePayload struct {
	Subtype   string      `json:"subtype"`
	RequestID string      `json:"request_id"`
	Response  interface{} `json:"response,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type cliControlCancelRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

type cliUpdateEnvironmentMessage struct {
	Type      string            `json:"type"`
	Variables map[string]string `json:"variables"`
}

func shouldScrubCLIEnv(key string) bool {
	_, blocked := cliScrubbedEnvKeys[strings.ToUpper(strings.TrimSpace(key))]
	return blocked
}

func filterCLIEnvUpdates(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	filtered := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(key) == "" || shouldScrubCLIEnv(key) {
			continue
		}
		filtered[key] = value
	}

	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
