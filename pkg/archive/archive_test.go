package archive_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kovaron/codesearch/pkg/archive"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "test.csi")

	m := archive.Manifest{
		Project:    "test-project",
		Version:    "1.0.0",
		ExportedAt: time.Now().UTC().Truncate(time.Second),
	}

	snapshotBytes := []byte("fake qdrant snapshot data")

	if err := archive.Write(outPath, m, snapshotBytes); err != nil {
		t.Fatalf("Write: %v", err)
	}

	gotManifest, gotSnapshot, err := archive.Read(outPath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if gotManifest.Project != m.Project {
		t.Errorf("Project = %q, want %q", gotManifest.Project, m.Project)
	}
	if string(gotSnapshot) != string(snapshotBytes) {
		t.Errorf("snapshot bytes mismatch")
	}

	_ = os.Remove(outPath) // cleanup
}

func TestWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "nested", "deep", "index.csi")

	m := archive.Manifest{Project: "p", Version: archive.Version, ExportedAt: time.Now().UTC()}
	if err := archive.Write(outPath, m, []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("archive not created at %s: %v", outPath, err)
	}
}

func TestDefaultPath(t *testing.T) {
	if archive.DefaultPath != ".codesearch/index.csi" {
		t.Errorf("DefaultPath = %q, want %q", archive.DefaultPath, ".codesearch/index.csi")
	}
}
