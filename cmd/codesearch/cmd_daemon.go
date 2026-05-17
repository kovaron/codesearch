package main

import (
	"context"
	"log"
	"net"
	"net/url"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/kovaron/codesearch/internal/config"
	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/internal/indexer"
	"github.com/kovaron/codesearch/internal/parser"
	"github.com/kovaron/codesearch/internal/store"
	"github.com/kovaron/codesearch/internal/watcher"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon [dir]",
		Short: "Watch files and keep the index up to date",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			qdrantHost, qdrantPort := parseQdrantURL(cfg.QdrantURL)
			st, err := store.NewQdrant(ctx, qdrantHost, qdrantPort, cfg.Project, 768)
			if err != nil {
				return err
			}

			emb := embedder.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
			reg := parser.NewRegistry()
			idx := indexer.New(reg, emb, st)

			// Heartbeat goroutine
			go func() {
				t := time.NewTicker(15 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						_ = st.WriteHeartbeat(ctx)
					}
				}
			}()
			_ = st.WriteHeartbeat(ctx)

			handler := func(path string, deleted bool) {
				rel, err := filepath.Rel(dir, path)
				if err != nil {
					rel = path
				}
				if deleted {
					log.Printf("delete %s", rel)
					if err := idx.DeleteFile(ctx, path); err != nil {
						log.Printf("delete error: %v", err)
					}
					return
				}
				log.Printf("index %s", rel)
				if err := idx.IndexFile(ctx, path, indexer.LanguageFor(path)); err != nil {
					log.Printf("index error: %v", err)
				}
			}

			w, err := watcher.New([]string{dir}, handler)
			if err != nil {
				return err
			}

			log.Printf("daemon: watching %s for project %q", dir, cfg.Project)
			w.Run(ctx)
			log.Println("daemon: stopped")
			return nil
		},
	}
}

// parseQdrantURL extracts host and gRPC port from a URL like http://localhost:6334
func parseQdrantURL(rawURL string) (string, int) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "localhost", 6334
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return u.Host, 6334
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		port = 6334
	}
	return host, port
}
