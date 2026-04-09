package agent

import (
	"context"
	"testing"

	"github.com/hunknownz/open-agent-sdk-go/tools"
	"github.com/hunknownz/open-agent-sdk-go/types"
)

func TestStructuredOutputToolEchoesData(t *testing.T) {
	tool := newStructuredOutputTool(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string"},
		},
		"required": []string{"action"},
	})

	result, err := tool.Call(context.Background(), map[string]interface{}{
		"action": "choose_reward_card",
	}, &types.ToolUseContext{})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected tool result")
	}
	if result.Data == nil {
		t.Fatal("expected tool result data")
	}

	payload, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected structured payload map, got %T", result.Data)
	}
	if payload["action"] != "choose_reward_card" {
		t.Fatalf("expected echoed action, got %#v", payload["action"])
	}
}

func TestExtractStructuredOutputSelectsStructuredToolResult(t *testing.T) {
	responses := []tools.ToolCallResponse{
		{
			ToolName: "bash",
			Result: &types.ToolResult{
				Data: map[string]interface{}{"ignored": true},
			},
		},
		{
			ToolName: StructuredOutputToolName,
			Result: &types.ToolResult{
				Data: map[string]interface{}{"action": "end_turn"},
			},
		},
	}

	got, ok := extractStructuredOutput(responses)
	if !ok {
		t.Fatal("expected structured output to be detected")
	}
	payload, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected payload map, got %T", got)
	}
	if payload["action"] != "end_turn" {
		t.Fatalf("expected end_turn payload, got %#v", payload["action"])
	}
}
