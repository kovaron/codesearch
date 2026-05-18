package bench

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Task represents a single benchmark task definition loaded from YAML.
type Task struct {
	ID             string   `yaml:"id"`
	Kind           string   `yaml:"kind"`
	Prompt         string   `yaml:"prompt"`
	Arms           []string `yaml:"arms"`
	TurnCap        int      `yaml:"turn_cap"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	Golden         Golden   `yaml:"golden"`
}

// Golden describes the expected outcome for a Task.
type Golden struct {
	Type     string   `yaml:"type"`
	Expected []string `yaml:"expected,omitempty"`
	Match    string   `yaml:"match,omitempty"`
	DiffPath string   `yaml:"diff_path,omitempty"`
}

var validKinds = map[string]bool{"search": true, "read": true, "edit": true, "analysis": true}
var validGoldenTypes = map[string]bool{"answer_match": true, "file_diff": true, "file_exists": true}
var validMatch = map[string]bool{"set_equal": true, "substring": true, "regex": true, "": true}

// LoadTask reads a single bench task definition from path and validates it.
func LoadTask(path string) (*Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var t Task
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if t.ID == "" {
		return nil, fmt.Errorf("%s: id required", path)
	}
	if !validKinds[t.Kind] {
		return nil, fmt.Errorf("%s: invalid kind %q", path, t.Kind)
	}
	if t.Prompt == "" {
		return nil, fmt.Errorf("%s: prompt required", path)
	}
	if len(t.Arms) == 0 {
		return nil, fmt.Errorf("%s: arms required (non-empty)", path)
	}
	if t.TurnCap <= 0 {
		t.TurnCap = 20
	}
	if t.TimeoutSeconds <= 0 {
		t.TimeoutSeconds = 120
	}
	if !validGoldenTypes[t.Golden.Type] {
		return nil, fmt.Errorf("%s: invalid golden.type %q", path, t.Golden.Type)
	}
	if !validMatch[t.Golden.Match] {
		return nil, fmt.Errorf("%s: invalid golden.match %q", path, t.Golden.Match)
	}
	return &t, nil
}
