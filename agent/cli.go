package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hunknownz/open-agent-sdk-go/types"
)

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
	a.cliTurnMu.Lock()
	defer a.cliTurnMu.Unlock()

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

	if err := a.ensureCLISession(ctx); err != nil {
		return err
	}

	session := a.currentCLISession()
	if session == nil {
		return fmt.Errorf("claude cli session is unavailable")
	}
	defer session.cancelAllControlRequests()

	if err := session.updateEnvironment(a.opts.Env); err != nil {
		a.resetCLISession()
		return err
	}

	if err := session.writeTurn(buildCLIUserInput(prompt)); err != nil {
		a.resetCLISession()
		return err
	}

	var (
		lastAssistantText string
	)

	for {
		message, err := session.nextMessage(ctx)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				a.resetCLISession()
				return ctx.Err()
			case errors.Is(err, io.EOF):
				stderrText := session.stderrText()
				a.resetCLISession()
				if stderrText != "" {
					return fmt.Errorf("claude cli did not emit a result message: %s", stderrText)
				}
				return fmt.Errorf("claude cli did not emit a result message")
			default:
				a.resetCLISession()
				return err
			}
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
		case string(types.MessageTypeSystem):
			systemMsg, text := decodeCLISystem(message.Message)
			if systemMsg == nil {
				continue
			}

			if text != "" {
				lastAssistantText = text
			}
			a.messages = append(a.messages, *systemMsg)
			eventCh <- types.SDKMessage{
				Type:    types.MessageTypeSystem,
				Message: systemMsg,
			}
		case "control_request":
			if err := a.handleCLIControlRequest(ctx, session, message); err != nil {
				a.resetCLISession()
				return err
			}
		case "control_response", "control_cancel_request":
			continue
		case "update_environment_variables":
			continue
		case string(types.MessageTypeResult):
			var usage *types.Usage
			if message.Usage != nil {
				decodedUsage, ok := message.Usage.(*types.Usage)
				if ok {
					usage = decodedUsage
				} else {
					bytes, marshalErr := json.Marshal(message.Usage)
					if marshalErr == nil {
						var parsedUsage types.Usage
						if unmarshalErr := json.Unmarshal(bytes, &parsedUsage); unmarshalErr == nil {
							usage = &parsedUsage
						}
					}
				}
			}

			if len(message.Errors) > 0 || message.IsError {
				text := strings.TrimSpace(strings.Join(message.Errors, "; "))
				if text == "" {
					text = strings.TrimSpace(message.Result)
				}
				if text == "" {
					text = "claude cli returned an execution error"
				}
				return fmt.Errorf("claude cli %s: %s", message.Subtype, text)
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
				Usage:            usage,
				NumTurns:         message.NumTurns,
				Duration:         time.Since(startTime).Milliseconds(),
				Cost:             message.TotalCostUSD,
				StructuredOutput: message.StructuredOutput,
			}
			return nil
		}
	}
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
	envMap := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		key := parts[0]
		if shouldScrubCLIEnv(key) {
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
		if shouldScrubCLIEnv(key) {
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

func decodeCLISystem(raw json.RawMessage) (*types.Message, string) {
	if len(raw) == 0 {
		return nil, ""
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, ""
	}

	var textParts []string
	if message, ok := payload["message"].(string); ok && strings.TrimSpace(message) != "" {
		textParts = append(textParts, strings.TrimSpace(message))
	}
	if text, ok := payload["text"].(string); ok && strings.TrimSpace(text) != "" {
		textParts = append(textParts, strings.TrimSpace(text))
	}
	if subtype, ok := payload["subtype"].(string); ok && strings.TrimSpace(subtype) != "" {
		textParts = append(textParts, "subtype="+strings.TrimSpace(subtype))
	}
	if len(textParts) == 0 {
		bytes, err := json.Marshal(payload)
		if err != nil {
			return nil, ""
		}
		textParts = append(textParts, string(bytes))
	}

	text := strings.Join(textParts, " ")
	return &types.Message{
		Type: types.MessageTypeSystem,
		Role: "system",
		Content: []types.ContentBlock{{
			Type: types.ContentBlockText,
			Text: text,
		}},
		UUID:      uuid.New().String(),
		Timestamp: time.Now(),
	}, text
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
