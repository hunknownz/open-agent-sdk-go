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

func runCLIHelperProcess() {
	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	scanner := bufio.NewScanner(os.Stdin)
	for turn := 1; scanner.Scan(); turn++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		text := fmt.Sprintf("session=%s turn=%d", sessionID, turn)
		assistant := map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"model": "fake-claude",
				"content": []map[string]any{
					{
						"type": "text",
						"text": text,
					},
				},
			},
		}
		result := map[string]any{
			"type":           "result",
			"subtype":        "success",
			"result":         text,
			"num_turns":      turn,
			"total_cost_usd": 0,
			"usage": map[string]any{
				"input_tokens":  1,
				"output_tokens": 1,
			},
		}

		_ = json.NewEncoder(os.Stdout).Encode(assistant)
		_ = json.NewEncoder(os.Stdout).Encode(result)
	}
}

func newHelperProcessAgent() *Agent {
	return New(Options{
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
	})
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
