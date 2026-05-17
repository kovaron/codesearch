package archive

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

const Version = "1.0.0"

type Manifest struct {
	Project    string    `json:"project"`
	Version    string    `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
}

// Write creates a .csi archive at path with the given manifest and Qdrant snapshot bytes.
func Write(path string, m Manifest, snapshot []byte) (err error) {
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
