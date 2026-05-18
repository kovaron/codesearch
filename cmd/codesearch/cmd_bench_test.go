package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBenchDryRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.yaml")
	body := `
id: x
kind: search
prompt: x
arms: [codesearch, baseline]
turn_cap: 5
timeout_seconds: 10
golden:
  type: answer_match
  expected: [foo]
  match: substring
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newBenchCmd()
	cmd.SetArgs([]string{"--tasks", dir, "--dry-run"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v out=%s", err, buf.String())
	}
}
