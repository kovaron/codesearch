package archive

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Version is the manifest schema version stamped into every .csi archive.
const Version = "1.0.0"

// DefaultPath is the conventional repo-local path for a committed index
// snapshot. The `codesearch export` and `import` commands default to this
// path when invoked without an argument.
const DefaultPath = ".codesearch/index.csi"

type Manifest struct {
	Project    string    `json:"project"`
	Version    string    `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
}

// Write creates a .csi archive at path with the given manifest and Qdrant
// snapshot bytes. Parent directories are created if missing.
func Write(path string, m Manifest, snapshot []byte) (err error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create archive dir: %w", err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	manifestBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := writeEntry(tw, "manifest.json", manifestBytes); err != nil {
		return err
	}
	if err := writeEntry(tw, "qdrant-snapshot.bin", snapshot); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	return nil
}

func writeEntry(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name: name,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// Read extracts the manifest and snapshot bytes from a .csi archive.
func Read(path string) (Manifest, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return Manifest{}, nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var m Manifest
	var snapshot []byte

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, nil, fmt.Errorf("read archive: %w", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return Manifest{}, nil, err
		}
		switch hdr.Name {
		case "manifest.json":
			if err := json.Unmarshal(data, &m); err != nil {
				return Manifest{}, nil, err
			}
		case "qdrant-snapshot.bin":
			snapshot = data
		}
	}
	return m, snapshot, nil
}
