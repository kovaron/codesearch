package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kovaron/codesearch/internal/bench"
	"github.com/kovaron/codesearch/internal/config"
	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/store"
	"github.com/spf13/cobra"
)

// newBenchCmd returns the cobra command for the bench subcommand.
func newBenchCmd() *cobra.Command {
	var (
		tasksDir  string
		n         int
		model     string
		outDir    string
		taskID    string
		dryRun    bool
		maxTokens int
	)
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Measure codesearch vs baseline efficiency",
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := loadBenchTasks(tasksDir, taskID)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "loaded %d tasks\n", len(tasks))
			if dryRun {
				return validateBenchGoldens(cmd.OutOrStdout(), tasks)
			}
			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY required (use --dry-run to validate without API)")
			}
			cfg, err := config.Load(".")
			if err != nil {
				return err
			}
			ctx := context.Background()
			qdrantHost, qdrantPort := parseQdrantURL(cfg.QdrantURL)
			st, err := store.NewQdrant(ctx, qdrantHost, qdrantPort, cfg.Project, embedder.NomicEmbedTextDim)
			if err != nil {
				return fmt.Errorf("qdrant: %w (is the daemon running?)", err)
			}
			emb := embedder.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
			r := &bench.Runner{
				Client:         bench.NewSDKClient(apiKey),
				Store:          st,
				Embedder:       emb,
				Project:        cfg.Project,
				Model:          model,
				SrcRepo:        mustAbsPath("."),
				N:              n,
				MaxTotalTokens: maxTokens,
			}
			var allRuns []bench.RunResult
			for _, t := range tasks {
				fmt.Fprintf(cmd.ErrOrStderr(), "running task %s (%d arms × %d runs)\n", t.ID, len(t.Arms), n)
				allRuns = append(allRuns, r.RunTask(ctx, t)...)
			}
			aggs := bench.Aggregate(allRuns)
			ts := time.Now()
			outBase := filepath.Join(outDir, ts.UTC().Format("20060102T150405Z"))
			if err := os.MkdirAll(outBase, 0o755); err != nil {
				return err
			}
			meta := bench.Meta{
				Timestamp: ts, Model: model, N: n, Corpus: "self", TurnCap: tasks[0].TurnCap,
			}
			if err := os.WriteFile(filepath.Join(outBase, "results.json"),
				[]byte(bench.RenderJSON(aggs, meta)), 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(outBase, "report.md"),
				[]byte(bench.RenderMarkdown(aggs, meta)), 0o644); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), filepath.Join(outBase, "report.md"))
			return nil
		},
	}
	cmd.Flags().StringVar(&tasksDir, "tasks", "bench/tasks", "directory containing task YAML files")
	cmd.Flags().IntVar(&n, "n", 3, "runs per (task, arm)")
	cmd.Flags().StringVar(&model, "model", "claude-opus-4-7", "Anthropic model id")
	cmd.Flags().StringVar(&outDir, "out", "bench/results", "output directory")
	cmd.Flags().StringVar(&taskID, "task-id", "", "run only the named task")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate tasks + goldens, no API calls")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 200000, "per-task total token budget (input+output); 0 = unlimited")
	return cmd
}

// loadBenchTasks loads all YAML task files from dir, optionally filtering to
// the single task whose ID equals only.
func loadBenchTasks(dir, only string) ([]*bench.Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []*bench.Task
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		t, err := bench.LoadTask(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if only != "" && t.ID != only {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tasks loaded from %s", dir)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// validateBenchGoldens checks that every task's golden configuration is
// structurally complete and writes "dry-run: OK" to w on success.
func validateBenchGoldens(w io.Writer, tasks []*bench.Task) error {
	for _, t := range tasks {
		if t.Golden.Type == "file_diff" && len(t.Golden.ExpectedFiles) == 0 {
			return fmt.Errorf("%s: file_diff requires expected_files", t.ID)
		}
		if (t.Golden.Type == "answer_match" || t.Golden.Type == "file_exists") && len(t.Golden.Expected) == 0 {
			return fmt.Errorf("%s: %s requires expected[]", t.ID, t.Golden.Type)
		}
	}
	fmt.Fprintln(w, "dry-run: OK")
	return nil
}

// mustAbsPath returns the absolute form of p or panics.
func mustAbsPath(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		panic(err)
	}
	return a
}
