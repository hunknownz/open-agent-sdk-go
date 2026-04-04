package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hunknownz/open-agent-sdk-go/types"
)

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		runCLIHelperProcess()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestClaudeCLISessionReuse(t *testing.T) {
	agent := newHelperProcessAgent()
	defer agent.Close()

	if err := agent.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	resultOne, err := agent.Prompt(context.Background(), "first")
	if err != nil {
		t.Fatalf("Prompt(first) error = %v", err)
	}

	resultTwo, err := agent.Prompt(context.Background(), "second")
	if err != nil {
		t.Fatalf("Prompt(second) error = %v", err)
	}

	sessionOne, turnOne := parseSessionAndTurn(t, resultOne.Text)
	sessionTwo, turnTwo := parseSessionAndTurn(t, resultTwo.Text)

	if sessionOne != sessionTwo {
		t.Fatalf("expected same session, got %q and %q", sessionOne, sessionTwo)
	}
	if turnOne != 1 || turnTwo != 2 {
		t.Fatalf("expected turns 1 and 2, got %d and %d", turnOne, turnTwo)
	}
}

func TestClaudeCLIClearResetsSession(t *testing.T) {
	agent := newHelperProcessAgent()
	defer agent.Close()

	first, err := agent.Prompt(context.Background(), "before-clear")
	if err != nil {
		t.Fatalf("Prompt(before-clear) error = %v", err)
	}

	agent.Clear()

	second, err := agent.Prompt(context.Background(), "after-clear")
	if err != nil {
		t.Fatalf("Prompt(after-clear) error = %v", err)
	}

	sessionOne, turnOne := parseSessionAndTurn(t, first.Text)
	sessionTwo, turnTwo := parseSessionAndTurn(t, second.Text)

	if sessionOne == sessionTwo {
		t.Fatalf("expected Clear() to reset the session, got %q twice", sessionOne)
	}
	if turnOne != 1 || turnTwo != 1 {
		t.Fatalf("expected turn counter reset to 1 after Clear(), got %d and %d", turnOne, turnTwo)
	}
}

func TestClaudeCLIUpdateEnv(t *testing.T) {
	agent := newHelperProcessAgent()
	defer agent.Close()

	if err := agent.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := agent.UpdateEnv(map[string]string{"TEST_DYNAMIC_ENV": "refreshed"}); err != nil {
		t.Fatalf("UpdateEnv() error = %v", err)
	}

	result, err := agent.Prompt(context.Background(), "env")
	if err != nil {
		t.Fatalf("Prompt(env) error = %v", err)
	}

	if got := strings.TrimSpace(result.Text); got != "env=refreshed" {
		t.Fatalf("expected env refresh to reach child session, got %q", got)
	}
}

func TestClaudeCLIControlRequestResponse(t *testing.T) {
	agent := newHelperProcessAgent()
	defer agent.Close()

	result, err := agent.Prompt(context.Background(), "request-control")
	if err != nil {
		t.Fatalf("Prompt(request-control) error = %v", err)
	}

	if got := strings.TrimSpace(result.Text); got != "control=success behavior=deny" {
		t.Fatalf("unexpected control_request result: %q", got)
	}
}

func TestClaudeCLIHookCallbackRouting(t *testing.T) {
	agent := newHelperProcessAgentWithOptions(Options{
		CLIHookCallbackHandler: func(ctx context.Context, request CLIControlRequest) (interface{}, error) {
			return map[string]interface{}{
				"decision": "allow",
				"reason":   fmt.Sprintf("callback=%s", request.CallbackID),
			}, nil
		},
	})
	defer agent.Close()

	result, err := agent.Prompt(context.Background(), "request-hook")
	if err != nil {
		t.Fatalf("Prompt(request-hook) error = %v", err)
	}

	if got := strings.TrimSpace(result.Text); got != "hook=success callback=cb-1 decision=allow" {
		t.Fatalf("unexpected hook_callback result: %q", got)
	}
}

func TestClaudeCLIElicitationDefaultsToCancel(t *testing.T) {
	agent := newHelperProcessAgent()
	defer agent.Close()

	result, err := agent.Prompt(context.Background(), "request-elicitation")
	if err != nil {
		t.Fatalf("Prompt(request-elicitation) error = %v", err)
	}

	if got := strings.TrimSpace(result.Text); got != "elicitation=cancel" {
		t.Fatalf("unexpected default elicitation result: %q", got)
	}
}

func TestClaudeCLIElicitationCustomHandler(t *testing.T) {
	agent := newHelperProcessAgentWithOptions(Options{
		CLIElicitationHandler: func(ctx context.Context, request CLIControlRequest) (CLIElicitationResponse, error) {
			return CLIElicitationResponse{
				Action: "accept",
				Content: map[string]interface{}{
					"choice": "picked",
				},
			}, nil
		},
	})
	defer agent.Close()

	result, err := agent.Prompt(context.Background(), "request-elicitation-custom")
	if err != nil {
		t.Fatalf("Prompt(request-elicitation-custom) error = %v", err)
	}

	if got := strings.TrimSpace(result.Text); got != "elicitation=accept choice=picked" {
		t.Fatalf("unexpected custom elicitation result: %q", got)
	}
}

func TestClaudeCLIControlHandlerFallback(t *testing.T) {
	agent := newHelperProcessAgentWithOptions(Options{
		CLIControlHandler: func(ctx context.Context, request CLIControlRequest) (interface{}, bool, error) {
			if request.Subtype != "hook_callback" {
				return nil, false, nil
			}

			return map[string]interface{}{
				"decision": "allow",
				"reason":   "fallback",
			}, true, nil
		},
	})
	defer agent.Close()

	result, err := agent.Prompt(context.Background(), "request-hook")
	if err != nil {
		t.Fatalf("Prompt(request-hook) error = %v", err)
	}

	if got := strings.TrimSpace(result.Text); got != "hook=success fallback decision=allow" {
		t.Fatalf("unexpected fallback hook_callback result: %q", got)
	}
}

func TestClaudeCLISessionCancelRequest(t *testing.T) {
	agent := newHelperProcessAgent()
	defer agent.Close()

	if err := agent.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	session := agent.currentCLISession()
	if session == nil {
		t.Fatal("expected CLI session to be initialized")
	}

	if err := session.writeTurn(buildCLIUserInput("request-cancel")); err != nil {
		t.Fatalf("writeTurn() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	message, err := session.nextMessage(ctx)
	if err != nil {
		t.Fatalf("nextMessage(control_request) error = %v", err)
	}
	if message.Type != "control_request" {
		t.Fatalf("expected control_request, got %q", message.Type)
	}

	if err := session.sendControlCancel(message.RequestID); err != nil {
		t.Fatalf("sendControlCancel() error = %v", err)
	}

	assistant, err := session.nextMessage(ctx)
	if err != nil {
		t.Fatalf("nextMessage(assistant) error = %v", err)
	}
	if assistant.Type != string(types.MessageTypeAssistant) {
		t.Fatalf("expected assistant after cancel, got %q", assistant.Type)
	}

	result, err := session.nextMessage(ctx)
	if err != nil {
		t.Fatalf("nextMessage(result) error = %v", err)
	}
	if result.Type != string(types.MessageTypeResult) {
		t.Fatalf("expected result after cancel, got %q", result.Type)
	}
	if got := strings.TrimSpace(result.Result); got != "control=cancel" {
		t.Fatalf("unexpected cancel result text: %q", got)
	}
}

type helperCLIState struct {
	sessionID        string
	env              map[string]string
	turn             int
	pendingRequestID string
}

func runCLIHelperProcess() {
	state := &helperCLIState{
		sessionID: fmt.Sprintf("session-%d", time.Now().UnixNano()),
		env:       make(map[string]string),
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}

		switch payload["type"] {
		case "update_environment_variables":
			mergeHelperEnv(state, payload["variables"])
		case "control_response":
			handleHelperControlResponse(state, payload)
		case "control_cancel_request":
			handleHelperControlCancel(state, payload)
		case "user":
			handleHelperUserMessage(state, payload)
		}
	}
}

func handleHelperUserMessage(state *helperCLIState, payload map[string]interface{}) {
	state.turn++
	prompt := helperPromptText(payload)

	switch prompt {
	case "request-control", "request-cancel":
		requestID := fmt.Sprintf("req-%d", state.turn)
		state.pendingRequestID = requestID
		_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"type":       "control_request",
			"request_id": requestID,
			"request": map[string]interface{}{
				"subtype":     "can_use_tool",
				"tool_name":   "UnknownTool",
				"input":       map[string]interface{}{"path": "example.txt"},
				"tool_use_id": fmt.Sprintf("tool-%d", state.turn),
			},
		})
	case "request-hook":
		requestID := fmt.Sprintf("req-hook-%d", state.turn)
		state.pendingRequestID = requestID
		_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"type":       "control_request",
			"request_id": requestID,
			"request": map[string]interface{}{
				"subtype":     "hook_callback",
				"callback_id": "cb-1",
				"input":       map[string]interface{}{"value": "ok"},
				"tool_use_id": fmt.Sprintf("tool-hook-%d", state.turn),
			},
		})
	case "request-elicitation", "request-elicitation-custom":
		requestID := fmt.Sprintf("req-elicitation-%d", state.turn)
		state.pendingRequestID = requestID
		_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"type":       "control_request",
			"request_id": requestID,
			"request": map[string]interface{}{
				"subtype":          "elicitation",
				"mcp_server_name":  "test-server",
				"message":          "Pick one",
				"mode":             "form",
				"elicitation_id":   "elic-1",
				"requested_schema": map[string]interface{}{"choice": "string"},
			},
		})
	case "env":
		helperWriteAssistantAndResult(state.turn, fmt.Sprintf("env=%s", state.env["TEST_DYNAMIC_ENV"]))
	default:
		helperWriteAssistantAndResult(state.turn, fmt.Sprintf("session=%s turn=%d", state.sessionID, state.turn))
	}
}

func handleHelperControlResponse(state *helperCLIState, payload map[string]interface{}) {
	response, _ := payload["response"].(map[string]interface{})
	requestID, _ := response["request_id"].(string)
	if state.pendingRequestID == "" || requestID != state.pendingRequestID {
		return
	}

	subtype, _ := response["subtype"].(string)
	text := "control=error"
	switch subtype {
	case "success":
		responsePayload, _ := response["response"].(map[string]interface{})
		switch {
		case strings.HasPrefix(requestID, "req-hook-"):
			decision, _ := responsePayload["decision"].(string)
			callback, _ := responsePayload["reason"].(string)
			if decision == "" {
				decision = "unknown"
			}
			text = fmt.Sprintf("hook=success %s decision=%s", callback, decision)
		case strings.HasPrefix(requestID, "req-elicitation-"):
			action, _ := responsePayload["action"].(string)
			if action == "" {
				action = "unknown"
			}
			text = fmt.Sprintf("elicitation=%s", action)
			if content, ok := responsePayload["content"].(map[string]interface{}); ok {
				if choice, ok := content["choice"].(string); ok && strings.TrimSpace(choice) != "" {
					text += " choice=" + choice
				}
			}
		default:
			behavior, _ := responsePayload["behavior"].(string)
			if behavior == "" {
				behavior = "unknown"
			}
			text = fmt.Sprintf("control=success behavior=%s", behavior)
		}
	case "error":
		errorText, _ := response["error"].(string)
		if strings.TrimSpace(errorText) == "" {
			errorText = "unknown"
		}
		text = fmt.Sprintf("control=error message=%s", errorText)
	}

	state.pendingRequestID = ""
	helperWriteAssistantAndResult(state.turn, text)
}

func handleHelperControlCancel(state *helperCLIState, payload map[string]interface{}) {
	requestID, _ := payload["request_id"].(string)
	if state.pendingRequestID == "" || requestID != state.pendingRequestID {
		return
	}

	state.pendingRequestID = ""
	helperWriteAssistantAndResult(state.turn, "control=cancel")
}

func helperWriteAssistantAndResult(turn int, text string) {
	assistant := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"model": "fake-claude",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": text,
				},
			},
		},
	}
	result := map[string]interface{}{
		"type":           "result",
		"subtype":        "success",
		"result":         text,
		"num_turns":      turn,
		"total_cost_usd": 0,
		"usage": map[string]interface{}{
			"input_tokens":  1,
			"output_tokens": 1,
		},
	}

	_ = json.NewEncoder(os.Stdout).Encode(assistant)
	_ = json.NewEncoder(os.Stdout).Encode(result)
}

func mergeHelperEnv(state *helperCLIState, raw interface{}) {
	variables, _ := raw.(map[string]interface{})
	for key, value := range variables {
		if text, ok := value.(string); ok {
			state.env[key] = text
		}
	}
}

func helperPromptText(payload map[string]interface{}) string {
	message, _ := payload["message"].(map[string]interface{})
	content := message["content"]

	switch typed := content.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []interface{}:
		for _, block := range typed {
			item, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if item["type"] == "text" {
				if text, ok := item["text"].(string); ok {
					return strings.TrimSpace(text)
				}
			}
		}
	}

	return ""
}

func newHelperProcessAgent() *Agent {
	return newHelperProcessAgentWithOptions(Options{})
}

func newHelperProcessAgentWithOptions(overrides Options) *Agent {
	opts := Options{
		Provider:   types.ProviderClaudeCLI,
		Model:      "claude-sonnet-4-6",
		CLICommand: os.Args[0],
		CLIArgs: []string{
			"-test.run=^$",
			"--",
		},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
		},
	}

	if overrides.CLIControlHandler != nil {
		opts.CLIControlHandler = overrides.CLIControlHandler
	}
	if overrides.CLIHookCallbackHandler != nil {
		opts.CLIHookCallbackHandler = overrides.CLIHookCallbackHandler
	}
	if overrides.CLIElicitationHandler != nil {
		opts.CLIElicitationHandler = overrides.CLIElicitationHandler
	}
	for key, value := range overrides.Env {
		opts.Env[key] = value
	}

	return New(opts)
}

func parseSessionAndTurn(t *testing.T, text string) (string, int) {
	t.Helper()

	fields := strings.Fields(strings.TrimSpace(text))
	session := ""
	turn := 0
	for _, field := range fields {
		switch {
		case strings.HasPrefix(field, "session="):
			session = strings.TrimPrefix(field, "session=")
		case strings.HasPrefix(field, "turn="):
			if _, err := fmt.Sscanf(field, "turn=%d", &turn); err != nil {
				t.Fatalf("parse turn from %q: %v", field, err)
			}
		}
	}

	if session == "" || turn == 0 {
		t.Fatalf("unexpected CLI result text: %q", text)
	}

	return session, turn
}
