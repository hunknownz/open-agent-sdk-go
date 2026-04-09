package api

import (
	"testing"

	"github.com/hunknownz/open-agent-sdk-go/types"
)

func TestConvertMessageToOpenAIAssistantToolCallUsesEmptyStringContent(t *testing.T) {
	msgs := convertMessageToOpenAI(APIMessage{
		Role: "assistant",
		Content: []types.ContentBlock{
			{
				Type:  types.ContentBlockToolUse,
				ID:    "call_1",
				Name:  "act",
				Input: map[string]interface{}{"action": "play_card", "card_index": 0},
			},
		},
	})

	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", msgs[0].Role)
	}
	content, ok := msgs[0].Content.(string)
	if !ok {
		t.Fatalf("expected assistant content to be a string, got %T", msgs[0].Content)
	}
	if content != "" {
		t.Fatalf("expected empty string content for tool-only assistant message, got %q", content)
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(msgs[0].ToolCalls))
	}
}

func TestApplyLocalOpenAIOptionsAddsNumCtxForLoopback(t *testing.T) {
	req := openAIRequest{Model: "qwen3:8b"}
	cfg := ClientConfig{
		BaseURL:       "http://127.0.0.1:11434",
		ContextWindow: 8192,
	}

	applyLocalOpenAIOptions(&req, cfg)

	if req.Options == nil {
		t.Fatal("expected options to be initialized")
	}
	if got, ok := req.Options["num_ctx"].(int); !ok || got != 8192 {
		t.Fatalf("num_ctx = %#v, want 8192", req.Options["num_ctx"])
	}
}

func TestApplyLocalOpenAIOptionsAddsNumCtxForPrivateLAN(t *testing.T) {
	req := openAIRequest{Model: "qwen3.5:35b-a3b"}
	cfg := ClientConfig{
		BaseURL:       "http://192.168.3.23:11434",
		ContextWindow: 8192,
	}

	applyLocalOpenAIOptions(&req, cfg)

	if req.Options == nil {
		t.Fatal("expected options to be initialized")
	}
	if got, ok := req.Options["num_ctx"].(int); !ok || got != 8192 {
		t.Fatalf("num_ctx = %#v, want 8192", req.Options["num_ctx"])
	}
}

func TestApplyLocalOpenAIOptionsSkipsRemoteBackends(t *testing.T) {
	req := openAIRequest{Model: "gpt-4.1"}
	cfg := ClientConfig{
		BaseURL:       "https://api.openai.com",
		ContextWindow: 8192,
	}

	applyLocalOpenAIOptions(&req, cfg)

	if req.Options != nil {
		t.Fatalf("expected no local options for remote backend, got %#v", req.Options)
	}
}
