package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/kovaron/codesearch/internal/config"
	"github.com/kovaron/codesearch/internal/embedder"
	"github.com/kovaron/codesearch/pkg/archive"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [input.csi]",
		Short: "Restore index from a .csi archive",
		Long: "Restore index from a .csi archive.\n\n" +
			"With no argument, reads from " + archive.DefaultPath + " (the conventional\n" +
			"repo-local path for a committed index snapshot).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inPath := archive.DefaultPath
			if len(args) > 0 {
				inPath = args[0]
			}
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

			if err := ensureCollection(restURL, cfg.Project); err != nil {
				return fmt.Errorf("ensure collection: %w", err)
			}

			body := &bytes.Buffer{}
			mw := multipart.NewWriter(body)
			part, err := mw.CreateFormFile("snapshot", "snapshot.bin")
			if err != nil {
				return err
			}
			if _, err := part.Write(snapBytes); err != nil {
				return err
			}
			if err := mw.Close(); err != nil {
				return err
			}

			uploadURL := restURL + "/collections/" + cfg.Project + "/snapshots/upload?priority=snapshot"
			req, err := http.NewRequest(http.MethodPost, uploadURL, body)
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", mw.FormDataContentType())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("upload snapshot: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				respBody, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("qdrant upload returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Import complete. Run 'codesearch daemon' to catch up on changes.")
			return nil
		},
	}
}

// ensureCollection creates the target collection with the default vector dim
// if it does not already exist. Qdrant's snapshot-upload endpoint requires
// the collection to exist; the snapshot's own config + data replaces whatever
// is created here. Idempotent — a 4xx from "already exists" is ignored.
func ensureCollection(restURL, project string) error {
	getResp, err := http.Get(restURL + "/collections/" + project)
	if err != nil {
		return err
	}
	getResp.Body.Close()
	if getResp.StatusCode == http.StatusOK {
		return nil
	}

	body := fmt.Sprintf(`{"vectors":{"size":%d,"distance":"Cosine"}}`, embedder.NomicEmbedTextDim)
	req, err := http.NewRequest(http.MethodPut, restURL+"/collections/"+project, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create collection: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
