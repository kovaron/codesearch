package bench

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// stubDispatcher returns a fixed canned response for any tool call.
type stubDispatcher struct{ response string }

func (s stubDispatcher) Call(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return s.response, nil
}

// fakeAnthropic cycles through pre-scripted replies; on exhaustion it repeats the last.
type fakeAnthropic struct {
	replies []*anthropic.Message
	idx     int
}

func (f *fakeAnthropic) CreateMessage(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
	if f.idx >= len(f.replies) {
		return f.replies[len(f.replies)-1], nil
	}
	r := f.replies[f.idx]
	f.idx++
	return r, nil
}

// makeTextMsg constructs a *anthropic.Message with a text content block.
func makeTextMsg(text string, inputTok, outputTok int64) *anthropic.Message {
	return &anthropic.Message{
		StopReason: anthropic.StopReasonEndTurn,
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: text},
		},
		Usage: anthropic.Usage{
			InputTokens:  inputTok,
			OutputTokens: outputTok,
		},
	}
}

// makeToolUseMsg constructs a *anthropic.Message with a single tool_use block.
func makeToolUseMsg(toolName, toolID string, inputArgs map[string]any, inputTok, outputTok int64) *anthropic.Message {
	raw, _ := json.Marshal(inputArgs)
	return &anthropic.Message{
		StopReason: anthropic.StopReasonToolUse,
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", Name: toolName, ID: toolID, Input: raw},
		},
		Usage: anthropic.Usage{
			InputTokens:  inputTok,
			OutputTokens: outputTok,
		},
	}
}

func TestAgentLoop_EndsOnEndTurn(t *testing.T) {
	t.Parallel()
	client := &fakeAnthropic{
		replies: []*anthropic.Message{
			makeTextMsg("done: found it", 100, 50),
		},
	}
	res := RunAgent(context.Background(), AgentInput{
		Client:     client,
		Dispatcher: stubDispatcher{},
		Model:      anthropic.ModelClaudeSonnet4_5,
		UserPrompt: "find something",
		Arm:        ArmCodesearch,
		TurnCap:    5,
	})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Turns != 1 {
		t.Errorf("turns=%d want 1", res.Turns)
	}
	if res.ToolCalls != 0 {
		t.Errorf("tool_calls=%d want 0", res.ToolCalls)
	}
	if res.Answer != "done: found it" {
		t.Errorf("answer=%q", res.Answer)
	}
	if res.InputTokens != 100 {
		t.Errorf("input_tokens=%d want 100", res.InputTokens)
	}
	if res.OutputTokens != 50 {
		t.Errorf("output_tokens=%d want 50", res.OutputTokens)
	}
}

func TestAgentLoop_DispatchesToolCallsAndStops(t *testing.T) {
	t.Parallel()
	client := &fakeAnthropic{
		replies: []*anthropic.Message{
			makeToolUseMsg("search_structural", "u1", map[string]any{"query": "NewIndexer"}, 80, 30),
			makeTextMsg("found NewIndexer", 120, 60),
		},
	}
	disp := stubDispatcher{response: `[{"name":"NewIndexer"}]`}
	res := RunAgent(context.Background(), AgentInput{
		Client:     client,
		Dispatcher: disp,
		Model:      anthropic.ModelClaudeSonnet4_5,
		UserPrompt: "find NewIndexer",
		Arm:        ArmCodesearch,
		TurnCap:    5,
	})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Turns != 2 {
		t.Errorf("turns=%d want 2", res.Turns)
	}
	if res.ToolCalls != 1 {
		t.Errorf("tool_calls=%d want 1", res.ToolCalls)
	}
	if res.InputTokens != 80+120 {
		t.Errorf("input_tokens=%d want 200", res.InputTokens)
	}
	if res.OutputTokens != 30+60 {
		t.Errorf("output_tokens=%d want 90", res.OutputTokens)
	}
}

func toolUseBlock(id, name string, argsJSON string) anthropic.ContentBlockUnion {
	return anthropic.ContentBlockUnion{
		Type:  "tool_use",
		ID:    id,
		Name:  name,
		Input: json.RawMessage(argsJSON),
	}
}

func TestAgentLoop_RespectsTokenBudget(t *testing.T) {
	t.Parallel()
	// Each tool_use reply burns 50k tokens. Budget of 60k must trip after 1 turn.
	loop := &anthropic.Message{
		StopReason: anthropic.StopReasonToolUse,
		Content:    []anthropic.ContentBlockUnion{toolUseBlock("u", "search_structural", `{"query":"x"}`)},
		Usage:      anthropic.Usage{InputTokens: 50000, OutputTokens: 0},
	}
	client := &fakeAnthropic{replies: []*anthropic.Message{loop}}
	res := RunAgent(context.Background(), AgentInput{
		Client:         client,
		Dispatcher:     stubDispatcher{response: "[]"},
		Model:          "x",
		Workdir:        t.TempDir(),
		System:         "sys",
		UserPrompt:     "p",
		Arm:            ArmCodesearch,
		TurnCap:        20,
		MaxTotalTokens: 60000,
	})
	if !res.Truncated {
		t.Error("expected Truncated=true on token budget exhaustion")
	}
	if res.Turns > 2 {
		t.Errorf("expected ≤2 turns, got %d", res.Turns)
	}
}

func TestAgentLoop_SurfacesCacheTokens(t *testing.T) {
	t.Parallel()
	reply := &anthropic.Message{
		StopReason: anthropic.StopReasonEndTurn,
		Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "done"}},
		Usage: anthropic.Usage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheReadInputTokens:     42,
			CacheCreationInputTokens: 7,
		},
	}
	client := &fakeAnthropic{replies: []*anthropic.Message{reply}}
	res := RunAgent(context.Background(), AgentInput{
		Client:     client,
		Dispatcher: stubDispatcher{response: "[]"},
		Model:      "x",
		UserPrompt: "q",
		System:     "sys",
		Arm:        ArmCodesearch,
		TurnCap:    5,
	})
	if res.CacheReadTokens != 42 {
		t.Errorf("CacheReadTokens=%d want 42", res.CacheReadTokens)
	}
	if res.CacheWriteTokens != 7 {
		t.Errorf("CacheWriteTokens=%d want 7", res.CacheWriteTokens)
	}
}

func TestAgentLoop_TruncatesAtCap(t *testing.T) {
	t.Parallel()
	// Always returns tool_use, so the loop must hit the TurnCap.
	client := &fakeAnthropic{
		replies: []*anthropic.Message{
			makeToolUseMsg("bash", "t1", map[string]any{"command": "ls"}, 10, 10),
		},
	}
	res := RunAgent(context.Background(), AgentInput{
		Client:     client,
		Dispatcher: stubDispatcher{response: "ok"},
		Model:      anthropic.ModelClaudeSonnet4_5,
		UserPrompt: "do something",
		Arm:        ArmBaseline,
		TurnCap:    3,
	})
	if !res.Truncated {
		t.Error("want Truncated=true")
	}
	if res.Turns != 3 {
		t.Errorf("turns=%d want 3", res.Turns)
	}
}
