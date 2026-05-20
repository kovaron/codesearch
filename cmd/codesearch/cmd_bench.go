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
		tasksDir        string
		n               int
		model           string
		outDir          string
		taskID          string
		dryRun          bool
		maxTokens       int
		srcRepo         string
		projectOverride string
	)
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Measure codesearch vs baseline efficiency",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoDir := "."
			if srcRepo != "" {
				repoDir = srcRepo
			}
			repoAbs, err := filepath.Abs(repoDir)
			if err != nil {
				return fmt.Errorf("resolve src-repo: %w", err)
			}

			// Dry-run never needs config or daemon; resolve tasks against the
			// explicit --tasks arg or fall back to repo-local default.
			if dryRun {
				resolved := tasksDir
				if !cmd.Flags().Changed("tasks") {
					// No project known yet without config — try repo-local default.
					resolved = filepath.Join(repoAbs, "bench", "tasks")
					if _, err := os.Stat(resolved); err != nil {
						resolved = "bench/tasks"
					}
				}
				tasks, err := loadBenchTasks(resolved, taskID)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "loaded %d tasks from %s\n", len(tasks), resolved)
				return validateBenchGoldens(cmd.OutOrStdout(), tasks)
			}

			cfg, err := config.Load(repoAbs)
			if err != nil {
				return fmt.Errorf("config.Load(%s): %w", repoAbs, err)
			}
			project := cfg.Project
			if projectOverride != "" {
				project = projectOverride
			}

			resolvedTasks := tasksDir
			if !cmd.Flags().Changed("tasks") {
				resolvedTasks = resolveTasksDir(project, repoAbs)
			}

			tasks, err := loadBenchTasks(resolvedTasks, taskID)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "loaded %d tasks from %s\n", len(tasks), resolvedTasks)

			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY required (use --dry-run to validate without API)")
			}
			ctx := context.Background()
			qdrantHost, qdrantPort := parseQdrantURL(cfg.QdrantURL)
			st, err := store.NewQdrant(ctx, qdrantHost, qdrantPort, project, embedder.NomicEmbedTextDim)
			if err != nil {
				return fmt.Errorf("qdrant: %w (is the daemon running for project %q?)", err, project)
			}
			emb := embedder.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
			r := &bench.Runner{
				Client:         bench.NewSDKClient(apiKey),
				Store:          st,
				Embedder:       emb,
				Project:        project,
				Model:          model,
				SrcRepo:        repoAbs,
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
				Timestamp: ts, Model: model, N: n, Corpus: project, TurnCap: tasks[0].TurnCap,
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
	cmd.Flags().StringVar(&tasksDir, "tasks", "bench/tasks", "directory containing task YAML files (default: auto-resolved from --src-repo + project name)")
	cmd.Flags().IntVar(&n, "n", 3, "runs per (task, arm)")
	cmd.Flags().StringVar(&model, "model", "claude-opus-4-7", "Anthropic model id")
	cmd.Flags().StringVar(&outDir, "out", "bench/results", "output directory")
	cmd.Flags().StringVar(&taskID, "task-id", "", "run only the named task")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate tasks + goldens, no API calls")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 200000, "per-task total token budget (input+output); 0 = unlimited")
	cmd.Flags().StringVar(&srcRepo, "src-repo", "", "path to the source repo to sandbox (default: current directory)")
	cmd.Flags().StringVar(&projectOverride, "project", "", "override the project name from .codesearch.yaml")
	return cmd
}

// resolveTasksDir picks a tasks directory in priority order:
//  1. <repo>/bench/corpora/<project>/tasks
//  2. <cwd>/bench/corpora/<project>/tasks
//  3. <repo>/bench/tasks
//  4. <cwd>/bench/tasks (fallback default — matches the legacy single-corpus layout)
func resolveTasksDir(project, repoAbs string) string {
	candidates := []string{
		filepath.Join(repoAbs, "bench", "corpora", project, "tasks"),
		filepath.Join("bench", "corpora", project, "tasks"),
		filepath.Join(repoAbs, "bench", "tasks"),
		"bench/tasks",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "bench/tasks"
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
