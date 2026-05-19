package bench

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Dispatcher abstracts the tool router so both arms share one loop.
type Dispatcher interface {
	Call(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// AgentInput carries all configuration needed to run one agent turn loop.
type AgentInput struct {
	Client         AnthropicClient
	Dispatcher     Dispatcher
	Model          string
	Workdir        string
	System         string
	UserPrompt     string
	Arm            ArmName
	TurnCap        int
	MaxTokens      int
	MaxTotalTokens int // hard cap on total (input+output) tokens; 0 = unlimited
}

// AgentOutput holds the result of a completed agent run.
type AgentOutput struct {
	Answer           string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	ToolCalls        int
	Turns            int
	Truncated        bool
	Error            string
	LatencyMs        int64
}

// RunAgent drives a turn loop until end_turn, TurnCap is reached, or error.
func RunAgent(ctx context.Context, in AgentInput) AgentOutput {
	start := time.Now()
	maxTok := in.MaxTokens
	if maxTok == 0 {
		maxTok = 4096
	}
	turnCap := in.TurnCap
	if turnCap == 0 {
		turnCap = 20
	}

	msgs := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(in.UserPrompt)),
	}

	var systemBlocks []anthropic.TextBlockParam
	if in.System != "" {
		systemBlocks = []anthropic.TextBlockParam{{
			Text:         in.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}

	out := AgentOutput{}

	for turn := 0; turn < turnCap; turn++ {
		req := anthropic.MessageNewParams{
			Model:     in.Model,
			MaxTokens: int64(maxTok),
			System:    systemBlocks,
			Messages:  msgs,
			Tools:     toolDefsWithCache(in.Arm),
		}

		resp, err := in.Client.CreateMessage(ctx, req)
		if err != nil {
			out.Error = err.Error()
			out.LatencyMs = time.Since(start).Milliseconds()
			return out
		}
		out.Turns++
		out.InputTokens += int(resp.Usage.InputTokens)
		out.OutputTokens += int(resp.Usage.OutputTokens)
		out.CacheReadTokens += int(resp.Usage.CacheReadInputTokens)
		out.CacheWriteTokens += int(resp.Usage.CacheCreationInputTokens)

		if in.MaxTotalTokens > 0 && out.InputTokens+out.OutputTokens >= in.MaxTotalTokens {
			out.Truncated = true
			out.Answer = collectText(resp.Content)
			out.LatencyMs = time.Since(start).Milliseconds()
			return out
		}

		switch resp.StopReason {
		case anthropic.StopReasonEndTurn:
			out.Answer = collectText(resp.Content)
			out.LatencyMs = time.Since(start).Milliseconds()
			return out

		case anthropic.StopReasonToolUse:
			toolBlocks := []anthropic.ContentBlockParamUnion{}
			for _, blk := range resp.Content {
				name, id, args, ok := asToolUse(blk)
				if !ok {
					continue
				}
				out.ToolCalls++
				result, derr := in.Dispatcher.Call(ctx, name, args)
				if derr != nil {
					result = "ERROR: " + derr.Error()
				}
				toolBlocks = append(toolBlocks, anthropic.NewToolResultBlock(id, result, false))
			}
			if len(toolBlocks) == 0 {
				// tool_use stop reason but no parsable tool_use blocks — break to
				// avoid sending the API an empty user turn.
				out.Truncated = true
				out.Answer = collectText(resp.Content)
				out.LatencyMs = time.Since(start).Milliseconds()
				return out
			}
			// Append assistant turn then tool results as user turn.
			msgs = append(msgs, asAssistantMessage(resp))
			msgs = append(msgs, anthropic.NewUserMessage(toolBlocks...))

		default:
			// Unexpected stop reason — treat as truncated.
			out.Truncated = true
			out.Answer = collectText(resp.Content)
			out.LatencyMs = time.Since(start).Milliseconds()
			return out
		}
	}

	// TurnCap exhausted.
	out.Truncated = true
	out.LatencyMs = time.Since(start).Milliseconds()
	return out
}

// collectText concatenates the text from all text-type content blocks.
func collectText(content []anthropic.ContentBlockUnion) string {
	var sb strings.Builder
	for _, blk := range content {
		if blk.Type == "text" {
			sb.WriteString(blk.Text)
		}
	}
	return sb.String()
}

// asToolUse extracts tool-use fields from a ContentBlockUnion.
// Returns ok=false when the block is not a tool_use.
func asToolUse(c anthropic.ContentBlockUnion) (name, id string, args json.RawMessage, ok bool) {
	if c.Type != "tool_use" {
		return "", "", nil, false
	}
	return c.Name, c.ID, c.Input, true
}

// toolDefsWithCache returns ToolDefs for arm with a cache-control breakpoint on
// the last tool definition. This tells the Anthropic API to cache everything up
// to and including that block, covering the full tool list on subsequent turns.
func toolDefsWithCache(arm ArmName) []anthropic.ToolUnionParam {
	tools := ToolDefs(arm)
	if len(tools) == 0 {
		return tools
	}
	last := &tools[len(tools)-1]
	if last.OfTool != nil {
		last.OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return tools
}

// asAssistantMessage converts an API response into an assistant MessageParam
// suitable for the next conversation turn.
func asAssistantMessage(resp *anthropic.Message) anthropic.MessageParam {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(resp.Content))
	for _, blk := range resp.Content {
		switch blk.Type {
		case "text":
			blocks = append(blocks, anthropic.NewTextBlock(blk.Text))
		case "tool_use":
			blocks = append(blocks, anthropic.NewToolUseBlock(blk.ID, blk.Input, blk.Name))
		}
	}
	return anthropic.NewAssistantMessage(blocks...)
}
