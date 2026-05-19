package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/fsops"
	"github.com/kovaron/codesearch/internal/store"
)

const benchSystemPromptCodesearch = `You are completing a task in a Go repository at %s.

Tool selection:
- Literal pattern lookups (exact function names, error strings, fixed-format file paths): NO bash here — use search_structural with the exact name, or list_symbols on a path prefix.
- Fuzzy questions ("what depends on X", "find something similar to Y", "what does this codebase do for Z"): use search_semantic or search_hybrid.
- "Who calls X" / "what does X call": use trace_path with direction=inbound or outbound. One round-trip instead of multi-grep.
- To inspect a symbol's body: get_chunk, or set include_source=true on a search call to fold source inline.
- For multi-file edits: read_file + edit_file are per-file; expect more round-trips than a single sed would take.

Use the provided tools. When done, emit your final answer as plain text and stop. Do not explain your reasoning beyond what the task asks for. Turn cap: %d.`

const benchSystemPromptBaseline = `You are completing a task in a Go repository at %s.

Tool selection:
- Use bash with grep/find/sed/awk for almost everything. PATH includes /usr/bin and /bin only — no ripgrep, no ast-grep.
- Literal lookups: grep -rn 'pattern' --include='*.go' (or specific dirs) is fastest.
- Multi-file edits: a single sed -i (or grep -l | xargs sed) is preferred over many edit_file round-trips.
- Use read_file / edit_file when you need precise textual replacement in one file.

Use the provided tools. When done, emit your final answer as plain text and stop. Do not explain your reasoning beyond what the task asks for. Turn cap: %d.`

// systemPromptFor returns the arm-specific system prompt with tool routing
// hints baked in.
func systemPromptFor(arm ArmName, workdir string, turnCap int) string {
	switch arm {
	case ArmCodesearch:
		return fmt.Sprintf(benchSystemPromptCodesearch, workdir, turnCap)
	case ArmBaseline:
		return fmt.Sprintf(benchSystemPromptBaseline, workdir, turnCap)
	}
	return fmt.Sprintf(benchSystemPromptCodesearch, workdir, turnCap)
}

// Runner orchestrates per-task agent execution across two arms.
type Runner struct {
	Client         AnthropicClient
	Store          store.Store
	Embedder       embedder.Embedder
	Project        string
	Model          string
	SrcRepo        string
	N              int
	MaxTotalTokens int // per-task token budget; 0 defaults to 200000
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

	system := systemPromptFor(arm, workdir, t.TurnCap)
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(t.TimeoutSeconds)*time.Second)
	defer cancel()

	maxTok := r.MaxTotalTokens
	if maxTok == 0 {
		maxTok = 200000
	}
	agent := RunAgent(taskCtx, AgentInput{
		Client:         r.Client,
		Dispatcher:     disp,
		Model:          r.Model,
		Workdir:        workdir,
		System:         system,
		UserPrompt:     t.Prompt,
		Arm:            arm,
		TurnCap:        t.TurnCap,
		MaxTotalTokens: maxTok,
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
		ToolCallsByName:  agent.ToolCallsByName,
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
	if len(args) > 0 {
		if err := json.Unmarshal(args, &m); err != nil {
			return "", fmt.Errorf("parse args for %s: %w", name, err)
		}
	}
	switch name {
	case "read_file":
		return ReadFileTool(c.workdir, sArg(m, "path"))
	case "edit_file":
		return "ok", EditFileTool(c.workdir, sArg(m, "path"), sArg(m, "old"), sArg(m, "new"))
	case "replace_in_files":
		pattern := sArg(m, "pattern")
		old := sArg(m, "old")
		newStr := sArg(m, "new")
		dryRun := bArg(m, "dry_run")
		changed, total, err := fsops.ReplaceInFiles(c.workdir, pattern, old, newStr, dryRun)
		if err != nil {
			return "", err
		}
		result, merr := json.Marshal(map[string]any{
			"files_changed":      changed,
			"total_replacements": total,
			"dry_run":            dryRun,
		})
		if merr != nil {
			return "", merr
		}
		return string(result), nil
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

func bArg(m map[string]any, k string) bool {
	if v, ok := m[k].(bool); ok {
		return v
	}
	return false
}
