package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hunknownz/open-agent-sdk-go/types"
)

type cliStreamMessage struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype,omitempty"`
	Message          json.RawMessage `json:"message,omitempty"`
	Result           string          `json:"result,omitempty"`
	StructuredOutput interface{}     `json:"structured_output,omitempty"`
	TotalCostUSD     float64         `json:"total_cost_usd,omitempty"`
	NumTurns         int             `json:"num_turns,omitempty"`
	Usage            *types.Usage    `json:"usage,omitempty"`
	IsError          bool            `json:"is_error,omitempty"`
	Errors           []string        `json:"errors,omitempty"`
}

type cliAssistantPayload struct {
	Model   string            `json:"model,omitempty"`
	Content []cliContentBlock `json:"content,omitempty"`
}

type cliContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

func (a *Agent) runCLILoop(ctx context.Context, prompt string, eventCh chan<- types.SDKMessage) error {
	startTime := time.Now()
	a.cliMu.Lock()
	defer a.cliMu.Unlock()

	userMsg := types.Message{
		Type: types.MessageTypeUser,
		Role: "user",
		Content: []types.ContentBlock{{
			Type: types.ContentBlockText,
			Text: prompt,
		}},
		UUID:      uuid.New().String(),
		Timestamp: time.Now(),
	}
	a.messages = append(a.messages, userMsg)

	if a.cliSession == nil || a.cliSession.isClosed() {
		session, err := a.startCLISession(context.Background())
		if err != nil {
			return err
		}
		a.cliSession = session
	}
	session := a.cliSession

	if err := session.writeTurn(buildCLIUserInput(prompt)); err != nil {
		_ = session.close()
		a.cliSession = nil
		return err
	}

	var (
		lastAssistantText string
		resultSeen        bool
		resultErr         error
	)

	for session.scanner.Scan() {
		line := strings.TrimSpace(session.scanner.Text())
		if line == "" {
			continue
		}

		var message cliStreamMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}

		switch message.Type {
		case string(types.MessageTypeAssistant):
			assistantMsg, text := decodeCLIAssistant(message.Message)
			if assistantMsg == nil {
				continue
			}

			lastAssistantText = text
			a.messages = append(a.messages, *assistantMsg)
			eventCh <- types.SDKMessage{
				Type:    types.MessageTypeAssistant,
				Message: assistantMsg,
			}
		case string(types.MessageTypeResult):
			resultSeen = true
			if len(message.Errors) > 0 || message.IsError {
				text := strings.TrimSpace(strings.Join(message.Errors, "; "))
				if text == "" {
					text = strings.TrimSpace(message.Result)
				}
				if text == "" {
					text = "claude cli returned an execution error"
				}
				resultErr = fmt.Errorf("claude cli %s: %s", message.Subtype, text)
			}

			resultText := strings.TrimSpace(message.Result)
			if lastAssistantText == "" && resultText != "" {
				assistantMsg := &types.Message{
					Type: types.MessageTypeAssistant,
					Role: "assistant",
					Content: []types.ContentBlock{{
						Type: types.ContentBlockText,
						Text: resultText,
					}},
					UUID:      uuid.New().String(),
					Timestamp: time.Now(),
				}
				a.messages = append(a.messages, *assistantMsg)
				eventCh <- types.SDKMessage{
					Type:    types.MessageTypeAssistant,
					Message: assistantMsg,
				}
			}

			eventCh <- types.SDKMessage{
				Type:             types.MessageTypeResult,
				Text:             resultText,
				Usage:            message.Usage,
				NumTurns:         message.NumTurns,
				Duration:         time.Since(startTime).Milliseconds(),
				Cost:             message.TotalCostUSD,
				StructuredOutput: message.StructuredOutput,
			}

			if resultErr != nil {
				return resultErr
			}
			return nil
		}
	}

	if err := session.scanner.Err(); err != nil {
		_ = session.close()
		a.cliSession = nil
		return fmt.Errorf("read claude cli stream: %w", err)
	}

	if !resultSeen {
		stderrText := session.stderrText()
		_ = session.close()
		a.cliSession = nil
		if stderrText != "" {
			return fmt.Errorf("claude cli did not emit a result message: %s", stderrText)
		}
		return fmt.Errorf("claude cli did not emit a result message")
	}

	return nil
}

func (a *Agent) newCLICommand(ctx context.Context) (*exec.Cmd, error) {
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--replay-user-messages",
		"--verbose",
		"--tools", "",
		"--no-session-persistence",
	}

	if model := strings.TrimSpace(a.opts.Model); model != "" {
		args = append(args, "--model", model)
	}

	systemPrompt := buildCLISystemPrompt(a.opts)
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	if a.opts.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(a.opts.MaxTurns))
	}

	if a.opts.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(a.opts.MaxBudgetUSD, 'f', -1, 64))
	}

	if a.opts.JSONSchema != nil {
		bytes, err := json.Marshal(a.opts.JSONSchema)
		if err != nil {
			return nil, fmt.Errorf("marshal claude cli json schema: %w", err)
		}
		args = append(args, "--json-schema", string(bytes))
	}

	if len(a.opts.CLIArgs) > 0 {
		args = append(append([]string{}, a.opts.CLIArgs...), args...)
	}

	cmd := exec.CommandContext(ctx, a.opts.CLICommand, args...)
	cmd.Dir = a.opts.CWD
	cmd.Env = buildCLIEnv(a.opts.Env)
	configureCLIProcess(cmd)
	return cmd, nil
}

func buildCLISystemPrompt(opts Options) string {
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	if opts.AppendSystemPrompt != "" {
		systemPrompt += "\n\n" + opts.AppendSystemPrompt
	}
	return strings.TrimSpace(systemPrompt)
}

func buildCLIEnv(overrides map[string]string) []string {
	scrubbed := map[string]struct{}{
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

	envMap := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		key := parts[0]
		if _, blocked := scrubbed[strings.ToUpper(key)]; blocked {
			continue
		}
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		envMap[key] = value
	}

	if _, ok := envMap["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"]; !ok {
		envMap["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
	}
	if _, ok := envMap["CLAUDE_CODE_ATTRIBUTION_HEADER"]; !ok {
		envMap["CLAUDE_CODE_ATTRIBUTION_HEADER"] = "0"
	}

	for key, value := range overrides {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, blocked := scrubbed[strings.ToUpper(key)]; blocked {
			continue
		}
		envMap[key] = value
	}

	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	return env
}

func decodeCLIAssistant(raw json.RawMessage) (*types.Message, string) {
	if len(raw) == 0 {
		return nil, ""
	}

	var payload cliAssistantPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, ""
	}

	content := make([]types.ContentBlock, 0, len(payload.Content))
	var parts []string
	for _, block := range payload.Content {
		switch block.Type {
		case "text":
			content = append(content, types.ContentBlock{
				Type: types.ContentBlockText,
				Text: block.Text,
			})
			if strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		case "thinking":
			content = append(content, types.ContentBlock{
				Type:     types.ContentBlockThinking,
				Thinking: block.Thinking,
			})
		}
	}

	if len(content) == 0 {
		return nil, ""
	}

	return &types.Message{
		Type:      types.MessageTypeAssistant,
		Role:      "assistant",
		Content:   content,
		Model:     payload.Model,
		UUID:      uuid.New().String(),
		Timestamp: time.Now(),
	}, strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildCLIUserInput(prompt string) string {
	payload := map[string]interface{}{
		"type":       "user",
		"session_id": "",
		"message": map[string]interface{}{
			"role":    "user",
			"content": prompt,
		},
		"parent_tool_use_id": nil,
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return `{"type":"user","session_id":"","message":{"role":"user","content":"` + escapeCLIJSONString(prompt) + `"},"parent_tool_use_id":null}` + "\n"
	}

	return string(bytes) + "\n"
}

func escapeCLIJSONString(value string) string {
	replacer := strings.NewReplacer(
		`\\`, `\\\\`,
		`"`, `\"`,
		"\r", `\r`,
		"\n", `\n`,
		"\t", `\t`,
	)
	return replacer.Replace(value)
}
