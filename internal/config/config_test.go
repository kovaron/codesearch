package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovaron/codesearch/internal/config"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	content := `
project: my-api
languages: [go, typescript]
include: ["pkg/**"]
exclude: ["vendor/**"]
qdrant_url: http://localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
`
	if err := os.WriteFile(filepath.Join(dir, ".codesearch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Project != "my-api" {
		t.Errorf("Project = %q, want %q", cfg.Project, "my-api")
	}
	if len(cfg.Languages) != 2 {
		t.Errorf("Languages len = %d, want 2", len(cfg.Languages))
	}
	if cfg.QdrantURL != "http://localhost:6334" {
		t.Errorf("QdrantURL = %q", cfg.QdrantURL)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load(t.TempDir())
	if err == nil {
		t.Error("Load() expected error for missing file, got nil")
	}
}

func TestValidate_MissingProject(t *testing.T) {
	cfg := &config.Config{
		Languages: []string{"go"},
		QdrantURL: "http://localhost:6334",
		OllamaURL: "http://localhost:11434",
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() expected error for missing project, got nil")
	}
}
