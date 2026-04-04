package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hunknownz/open-agent-sdk-go/types"
)

// CLIControlRequest is a normalized Claude CLI control-plane request.
type CLIControlRequest struct {
	RequestID             string
	Subtype               string
	ToolName              string
	Input                 map[string]interface{}
	PermissionSuggestions []map[string]interface{}
	BlockedPath           string
	DecisionReason        string
	Title                 string
	DisplayName           string
	ToolUseID             string
	AgentID               string
	Description           string
	CallbackID            string
	ServerName            string
	MCPServerName         string
	Message               string
	RequestedSchema       map[string]interface{}
	Mode                  string
	URL                   string
	ElicitationID         string
	Model                 string
	MaxThinkingTokens     *int
	Settings              map[string]interface{}
	Raw                   map[string]interface{}
}

// CLIElicitationResponse is the host response for an elicitation request.
type CLIElicitationResponse struct {
	Action  string
	Content map[string]interface{}
}

// CLIControlHandler optionally handles Claude CLI control requests.
// Return handled=false to fall through to the built-in router.
type CLIControlHandler func(ctx context.Context, request CLIControlRequest) (response interface{}, handled bool, err error)

// CLIHookCallbackHandler optionally handles Claude CLI hook_callback requests.
type CLIHookCallbackHandler func(ctx context.Context, request CLIControlRequest) (response interface{}, err error)

// CLIElicitationHandler optionally handles Claude CLI elicitation requests.
type CLIElicitationHandler func(ctx context.Context, request CLIControlRequest) (response CLIElicitationResponse, err error)

func (a *Agent) handleCLIControlRequest(ctx context.Context, session *cliSession, message *cliStreamMessage) error {
	if message == nil || message.Request == nil {
		return fmt.Errorf("received malformed claude cli control_request")
	}
	requestID := strings.TrimSpace(message.RequestID)
	if requestID == "" {
		return fmt.Errorf("received claude cli control_request without request_id")
	}

	requestCtx, cancel := context.WithCancel(ctx)
	if !session.beginControlRequest(requestID, cancel) {
		cancel()
		return nil
	}

	request := *message.Request
	go func() {
		defer session.finishControlRequest(requestID)

		response, err := a.resolveCLIControlRequest(requestCtx, requestID, request)
		switch {
		case err == nil:
			_ = session.sendControlResponseSuccess(requestID, response)
		case isControlCancellation(err):
			_ = session.sendControlCancel(requestID)
		default:
			_ = session.sendControlResponseError(requestID, err.Error())
		}
	}()

	return nil
}

func (a *Agent) resolveCLIControlRequest(ctx context.Context, requestID string, request cliControlRequestPayload) (interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	normalized := normalizeCLIControlRequest(requestID, request)
	if response, handled, err := a.routeCLIControlRequest(ctx, normalized); handled || err != nil {
		return response, err
	}

	switch normalized.Subtype {
	case "can_use_tool":
		return a.resolveCLIToolPermissionRequest(normalized)
	case "get_settings":
		return a.cliSettingsSnapshot(), nil
	case "apply_flag_settings":
		a.applyCLIFlagSettings(normalized.Settings)
		return map[string]interface{}{}, nil
	case "set_model":
		if model := strings.TrimSpace(normalized.Model); model != "" {
			a.opts.Model = model
		}
		return map[string]interface{}{}, nil
	case "set_max_thinking_tokens":
		return map[string]interface{}{}, nil
	default:
		return nil, fmt.Errorf("unsupported control request subtype: %s", normalized.Subtype)
	}
}

func (a *Agent) routeCLIControlRequest(ctx context.Context, request CLIControlRequest) (interface{}, bool, error) {
	switch request.Subtype {
	case "hook_callback":
		if a.opts.CLIHookCallbackHandler != nil {
			response, err := a.opts.CLIHookCallbackHandler(ctx, request)
			return response, true, err
		}
	case "elicitation":
		if a.opts.CLIElicitationHandler != nil {
			response, err := a.opts.CLIElicitationHandler(ctx, request)
			if err != nil {
				return nil, true, err
			}
			return response.toMap(), true, nil
		}
		return defaultCLIElicitationResponse(), true, nil
	}

	if a.opts.CLIControlHandler != nil {
		response, handled, err := a.opts.CLIControlHandler(ctx, request)
		if handled || err != nil {
			return response, handled, err
		}
	}
	return nil, false, nil
}

func normalizeCLIControlRequest(requestID string, payload cliControlRequestPayload) CLIControlRequest {
	return CLIControlRequest{
		RequestID:             requestID,
		Subtype:               strings.TrimSpace(payload.Subtype),
		ToolName:              strings.TrimSpace(payload.ToolName),
		Input:                 payload.Input,
		PermissionSuggestions: payload.PermissionSuggestions,
		BlockedPath:           strings.TrimSpace(payload.BlockedPath),
		DecisionReason:        strings.TrimSpace(payload.DecisionReason),
		Title:                 strings.TrimSpace(payload.Title),
		DisplayName:           strings.TrimSpace(payload.DisplayName),
		ToolUseID:             strings.TrimSpace(payload.ToolUseID),
		AgentID:               strings.TrimSpace(payload.AgentID),
		Description:           strings.TrimSpace(payload.Description),
		CallbackID:            strings.TrimSpace(payload.CallbackID),
		ServerName:            strings.TrimSpace(payload.ServerName),
		MCPServerName:         strings.TrimSpace(payload.MCPServerName),
		Message:               strings.TrimSpace(payload.Message),
		RequestedSchema:       payload.RequestedSchema,
		Mode:                  strings.TrimSpace(payload.Mode),
		URL:                   strings.TrimSpace(payload.URL),
		ElicitationID:         strings.TrimSpace(payload.ElicitationID),
		Model:                 strings.TrimSpace(payload.Model),
		MaxThinkingTokens:     payload.MaxThinkingTokens,
		Settings:              payload.Settings,
		Raw:                   payload.Raw,
	}
}

func isControlCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (a *Agent) resolveCLIToolPermissionRequest(request CLIControlRequest) (interface{}, error) {
	tool := a.toolRegistry.Get(request.ToolName)
	if tool == nil {
		return map[string]interface{}{
			"behavior":  string(types.PermissionDeny),
			"message":   fmt.Sprintf("tool %q is not registered in open-agent-sdk-go", request.ToolName),
			"toolUseID": request.ToolUseID,
		}, nil
	}

	decision, err := a.canUseTool(tool, request.Input)
	if err != nil {
		return nil, err
	}
	if decision == nil {
		decision = &types.PermissionDecision{Behavior: types.PermissionAllow}
	}

	response := map[string]interface{}{
		"behavior":  string(decision.Behavior),
		"toolUseID": request.ToolUseID,
	}
	if len(decision.UpdatedInput) > 0 {
		response["updatedInput"] = decision.UpdatedInput
	}
	if strings.TrimSpace(decision.Reason) != "" {
		response["message"] = decision.Reason
	}
	if decision.Interrupt {
		response["interrupt"] = true
	}

	return response, nil
}

func defaultCLIElicitationResponse() map[string]interface{} {
	return map[string]interface{}{
		"action": "cancel",
	}
}

func (r CLIElicitationResponse) toMap() map[string]interface{} {
	action := strings.TrimSpace(r.Action)
	if action == "" {
		action = "cancel"
	}

	response := map[string]interface{}{
		"action": action,
	}
	if len(r.Content) > 0 {
		response["content"] = r.Content
	}
	return response
}

func (a *Agent) cliSettingsSnapshot() map[string]interface{} {
	effective := map[string]interface{}{
		"model":          a.opts.Model,
		"permissionMode": a.opts.PermissionMode,
	}

	applied := map[string]interface{}{
		"model": a.opts.Model,
	}
	if a.opts.Effort != "" {
		applied["effort"] = string(a.opts.Effort)
	}

	sourceSettings := map[string]interface{}{
		"model": a.opts.Model,
	}
	if a.opts.PermissionMode != "" {
		sourceSettings["permissionMode"] = a.opts.PermissionMode
	}

	return map[string]interface{}{
		"effective": effective,
		"sources": []map[string]interface{}{
			{
				"source":   "flagSettings",
				"settings": sourceSettings,
			},
		},
		"applied": applied,
	}
}

func (a *Agent) applyCLIFlagSettings(settings map[string]interface{}) {
	if len(settings) == 0 {
		return
	}

	if model, ok := settings["model"].(string); ok && strings.TrimSpace(model) != "" {
		a.opts.Model = model
	}
}
