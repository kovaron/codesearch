package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/store"
)

const benchSystemPrompt = `You are completing a task in a Go repository at %s.
Use the provided tools. When done, emit your final answer as plain text and stop.
Do not explain your reasoning beyond what the task asks for.
Turn cap: %d.`

// Runner orchestrates per-task agent execution across two arms.
type Runner struct {
	Client   AnthropicClient
	Store    store.Store
	Embedder embedder.Embedder
	Project  string
	Model    string
	SrcRepo  string
	N        int
}

// RunTask executes one task across every arm in t.Arms, N times each.
func (r *Runner) RunTask(ctx context.Context, t *Task) []RunResult {
	var out []RunResult
	for _, arm := range t.Arms {
		for i := 0; i < r.N; i++ {
			out = append(out, r.runOne(ctx, t, ArmName(arm), i))
		}
	}
	return out
}

func (r *Runner) runOne(ctx context.Context, t *Task, arm ArmName, idx int) RunResult {
	workdir, cleanup, err := CloneSandbox(r.SrcRepo)
	if err != nil {
		return RunResult{TaskID: t.ID, Arm: string(arm), RunIdx: idx, Error: err.Error()}
	}
	defer cleanup()

	disp := r.dispatcherFor(arm, workdir, t.TimeoutSeconds)

	system := fmt.Sprintf(benchSystemPrompt, workdir, t.TurnCap)
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(t.TimeoutSeconds)*time.Second)
	defer cancel()

	agent := RunAgent(taskCtx, AgentInput{
		Client:     r.Client,
		Dispatcher: disp,
		Model:      r.Model,
		Workdir:    workdir,
		System:     system,
		UserPrompt: t.Prompt,
		Arm:        arm,
		TurnCap:    t.TurnCap,
	})

	correct, reason := EvaluateGolden(t.Golden, agent.Answer, workdir)
	rr := RunResult{
		TaskID:           t.ID,
		Arm:              string(arm),
		RunIdx:           idx,
		InputTokens:      agent.InputTokens,
		OutputTokens:     agent.OutputTokens,
		CacheReadTokens:  agent.CacheReadTokens,
		CacheWriteTokens: agent.CacheWriteTokens,
		TotalTokens:      agent.InputTokens + agent.OutputTokens,
		LatencyMs:        agent.LatencyMs,
		ToolCalls:        agent.ToolCalls,
		Turns:            agent.Turns,
		Truncated:        agent.Truncated,
		Correct:          correct,
		Error:            agent.Error,
	}
	if !correct && rr.Error == "" {
		rr.Error = "golden: " + reason
	}
	return rr
}

func (r *Runner) dispatcherFor(arm ArmName, workdir string, timeoutSec int) Dispatcher {
	timeout := time.Duration(timeoutSec) * time.Second
	switch arm {
	case ArmCodesearch:
		return &compositeDispatcher{
			mcp:         NewMCPDispatcher(r.Project, r.Store, r.Embedder),
			workdir:     workdir,
			bashTimeout: timeout,
		}
	case ArmBaseline:
		return &compositeDispatcher{
			workdir:     workdir,
			bashTimeout: timeout,
			allowBash:   true,
		}
	}
	return nil
}

type compositeDispatcher struct {
	mcp         *MCPDispatcher
	workdir     string
	bashTimeout time.Duration
	allowBash   bool
}

func (c *compositeDispatcher) Call(ctx context.Context, name string, args json.RawMessage) (string, error) {
	var m map[string]any
	_ = json.Unmarshal(args, &m)
	switch name {
	case "read_file":
		return ReadFileTool(c.workdir, sArg(m, "path"))
	case "edit_file":
		return "ok", EditFileTool(c.workdir, sArg(m, "path"), sArg(m, "old"), sArg(m, "new"))
	case "bash":
		if !c.allowBash {
			return "", fmt.Errorf("bash not available on this arm")
		}
		timeout := c.bashTimeout
		if ms := iArg(m, "timeout_ms", 0); ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
		return RunBash(c.workdir, sArg(m, "command"), timeout)
	}
	if c.mcp != nil {
		return c.mcp.Call(ctx, name, args)
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownTool, name)
}

func sArg(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func iArg(m map[string]any, k string, def int) int {
	if v, ok := m[k].(float64); ok {
		return int(v)
	}
	return def
}
