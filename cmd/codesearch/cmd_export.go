package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/kovaron/codesearch/internal/config"
	"github.com/kovaron/codesearch/pkg/archive"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <output.csi>",
		Short: "Export index snapshot to a portable .csi archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outPath := args[0]
			cfg, err := config.Load(".")
			if err != nil {
				return err
			}
			restURL := restURLFor(cfg.QdrantURL)

			// Create snapshot
			resp, err := http.Post(restURL+"/collections/"+cfg.Project+"/snapshots", "application/json", nil)
			if err != nil {
				return fmt.Errorf("create snapshot: %w", err)
			}
			defer resp.Body.Close()
			var snapResult struct {
				Result struct {
					Name string `json:"name"`
				} `json:"result"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&snapResult); err != nil {
				return fmt.Errorf("parse snapshot response: %w", err)
			}

			// Download snapshot
			dlResp, err := http.Get(restURL + "/collections/" + cfg.Project + "/snapshots/" + snapResult.Result.Name)
			if err != nil {
				return fmt.Errorf("download snapshot: %w", err)
			}
			defer dlResp.Body.Close()
			snapBytes, err := io.ReadAll(dlResp.Body)
			if err != nil {
				return err
			}

			m := archive.Manifest{
				Project:    cfg.Project,
				Version:    archive.Version,
				ExportedAt: time.Now().UTC(),
			}
			if err := archive.Write(outPath, m, snapBytes); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Exported %d bytes to %s\n", len(snapBytes), outPath)
			return nil
		},
	}
}

// restURLFor converts a gRPC Qdrant URL (port 6334) to its REST equivalent (port 6333).
func restURLFor(grpcURL string) string {
	u, err := url.Parse(grpcURL)
	if err != nil || u.Host == "" {
		return "http://localhost:6333"
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host
	}
	return fmt.Sprintf("%s://%s:6333", u.Scheme, host)
}
