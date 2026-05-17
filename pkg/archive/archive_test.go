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
