//go:build integration

package bench

import (
	"context"
	"os"
	"testing"
)

func TestAgentLoop_RealAPI_Hello(t *testing.T) {
	t.Parallel()
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	client := NewSDKClient(os.Getenv("ANTHROPIC_API_KEY"))
	res := RunAgent(context.Background(), AgentInput{
		Client:     client,
		Dispatcher: stubDispatcher{response: "ok"},
		Model:      "claude-opus-4-7",
		System:     "Echo whatever the user says back to them verbatim.",
		UserPrompt: "hello",
		Arm:        ArmCodesearch,
		TurnCap:    2,
	})
	if res.Error != "" {
		t.Fatalf("Error=%s", res.Error)
	}
	if res.Answer == "" {
		t.Fatal("empty answer")
	}
	if res.InputTokens == 0 {
		t.Fatal("expected non-zero input tokens")
	}
}
