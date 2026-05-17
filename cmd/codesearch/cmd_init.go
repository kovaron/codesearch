package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [dir]",
		Short: "Generate .codesearch.yaml and run initial full index",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			cfgPath := filepath.Join(dir, ".codesearch.yaml")
			if _, err := os.Stat(cfgPath); err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), ".codesearch.yaml already exists — skipping generation")
				return nil
			}
			base := filepath.Base(dir)
			if base == "." {
				base, _ = os.Getwd()
				base = filepath.Base(base)
			}
			content := fmt.Sprintf(`project: %s
languages: [go, typescript, java]
include: ["**/*"]
exclude: ["vendor/**", "node_modules/**", ".git/**"]
qdrant_url: http://localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
workers: 4
`, base)
			if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", cfgPath)
			fmt.Fprintln(cmd.OutOrStdout(), "Run 'codesearch daemon' to start indexing.")
			return nil
		},
	}
}
