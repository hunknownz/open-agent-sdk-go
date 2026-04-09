package agent

import (
	"testing"

	"github.com/hunknownz/open-agent-sdk-go/types"
)

func TestRepairEmptyToolUseInputs_UsesEmbeddedJSONFromAssistantText(t *testing.T) {
	msg := &types.Message{
		Role: "assistant",
		Content: []types.ContentBlock{
			{
				Type: types.ContentBlockText,
				Text: `Something seems wrong with the tool invocation. Let me try with explicit parameters.{"action":"play_card","card_index":3}`,
			},
			{
				Type: types.ContentBlockToolUse,
				ID:   "tool-1",
				Name: "act",
			},
		},
	}
	blocks := []types.ToolUseBlock{{
		ID:   "tool-1",
		Name: "act",
	}}

	repairEmptyToolUseInputs(msg, blocks)

	if got := blocks[0].Input["action"]; got != "play_card" {
		t.Fatalf("expected action play_card, got %#v", got)
	}
	cardIndex, ok := blocks[0].Input["card_index"].(float64)
	if !ok || int(cardIndex) != 3 {
		t.Fatalf("expected card_index 3, got %#v", blocks[0].Input["card_index"])
	}
	if msg.Content[1].Input["action"] != "play_card" {
		t.Fatalf("expected repaired message tool input, got %#v", msg.Content[1].Input)
	}
}

func TestRepairEmptyToolUseInputs_IgnoresAlreadyPopulatedToolInput(t *testing.T) {
	msg := &types.Message{
		Role: "assistant",
		Content: []types.ContentBlock{
			{
				Type: types.ContentBlockText,
				Text: `{"action":"skip_reward_cards"}`,
			},
			{
				Type:  types.ContentBlockToolUse,
				ID:    "tool-1",
				Name:  "act",
				Input: map[string]interface{}{"action": "choose_reward_card"},
			},
		},
	}
	blocks := []types.ToolUseBlock{{
		ID:    "tool-1",
		Name:  "act",
		Input: map[string]interface{}{"action": "choose_reward_card"},
	}}

	repairEmptyToolUseInputs(msg, blocks)

	if got := blocks[0].Input["action"]; got != "choose_reward_card" {
		t.Fatalf("expected original action to remain, got %#v", got)
	}
}
