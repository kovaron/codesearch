package main

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/kovaron/codesearch/internal/config"
	"github.com/kovaron/codesearch/pkg/archive"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <input.csi>",
		Short: "Restore index from a .csi archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inPath := args[0]
			cfg, err := config.Load(".")
			if err != nil {
				return err
			}
			restURL := restURLFor(cfg.QdrantURL)

			m, snapBytes, err := archive.Read(inPath)
			if err != nil {
				return fmt.Errorf("read archive: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Importing snapshot for project %q (exported at %s)\n",
				m.Project, m.ExportedAt.Format("2006-01-02 15:04:05 UTC"))

			uploadURL := restURL + "/collections/" + cfg.Project + "/snapshots/upload?collection_name=" + cfg.Project
			req, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(snapBytes))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/octet-stream")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("upload snapshot: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("qdrant returned status %d during upload", resp.StatusCode)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Import complete. Run 'codesearch daemon' to catch up on changes.")
			return nil
		},
	}
}
