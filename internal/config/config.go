package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Project     string   `yaml:"project"`
	Languages   []string `yaml:"languages"`
	Include     []string `yaml:"include"`
	Exclude     []string `yaml:"exclude"`
	QdrantURL   string   `yaml:"qdrant_url"`
	OllamaURL   string   `yaml:"ollama_url"`
	OllamaModel string   `yaml:"ollama_model"`
	Workers     int      `yaml:"workers"`
}

func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, ".codesearch.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no .codesearch.yaml found in %s — run codesearch init", dir)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse .codesearch.yaml: %w", err)
	}
	cfg.setDefaults()
	return &cfg, cfg.Validate()
}

func (c *Config) setDefaults() {
	if c.OllamaModel == "" {
		c.OllamaModel = "nomic-embed-text"
	}
	if c.QdrantURL == "" {
		c.QdrantURL = "http://localhost:6334"
	}
	if c.OllamaURL == "" {
		c.OllamaURL = "http://localhost:11434"
	}
	if c.Workers == 0 {
		c.Workers = 4
	}
	if len(c.Languages) == 0 {
		c.Languages = []string{"go", "typescript", "java"}
	}
}

func (c *Config) Validate() error {
	if c.Project == "" {
		return fmt.Errorf("config: 'project' is required")
	}
	return nil
}
