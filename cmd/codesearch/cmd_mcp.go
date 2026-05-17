package main

import (
	"context"

	"github.com/kovaron/codesearch/internal/config"
	"github.com/kovaron/codesearch/internal/embedder"
	internalmcp "github.com/kovaron/codesearch/internal/mcp"
	"github.com/kovaron/codesearch/internal/store"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start stdio MCP server for AI agent integration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(".")
			if err != nil {
				return err
			}
			ctx := context.Background()
			qdrantHost, qdrantPort := parseQdrantURL(cfg.QdrantURL)
			st, err := store.NewQdrant(ctx, qdrantHost, qdrantPort, cfg.Project, 768)
			if err != nil {
				return err
			}
			emb := embedder.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
			return internalmcp.Serve(st, emb)
		},
	}
}
