package bench

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	Type          string          `yaml:"type"`
	Expected      []string        `yaml:"expected,omitempty"`
	Match         string          `yaml:"match,omitempty"`
	DiffPath      string          `yaml:"diff_path,omitempty"`
	ExpectedFiles []FileAssertion `yaml:"expected_files,omitempty"`
}

// FileAssertion describes per-file contains/not_contains checks for file_diff goldens.
type FileAssertion struct {
	Path        string   `yaml:"path"`
	Contains    []string `yaml:"contains,omitempty"`
	NotContains []string `yaml:"not_contains,omitempty"`
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

// EvaluateGolden returns (passed, reason). answer is the agent's final
// text output; workdir is the sandbox root for file-based checks.
func EvaluateGolden(g Golden, answer, workdir string) (bool, string) {
	switch g.Type {
	case "answer_match":
		return evalAnswerMatch(g, answer)
	case "file_exists":
		for _, rel := range g.Expected {
			if _, err := os.Stat(filepath.Join(workdir, rel)); err != nil {
				return false, fmt.Sprintf("missing %s", rel)
			}
		}
		return true, ""
	case "file_diff":
		for _, fa := range g.ExpectedFiles {
			p, err := safeJoin(workdir, fa.Path)
			if err != nil {
				return false, "path: " + err.Error()
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return false, "read " + fa.Path + ": " + err.Error()
			}
			s := string(b)
			for _, want := range fa.Contains {
				if !strings.Contains(s, want) {
					return false, fa.Path + " missing " + want
				}
			}
			for _, bad := range fa.NotContains {
				if strings.Contains(s, bad) {
					return false, fa.Path + " still has " + bad
				}
			}
		}
		return true, ""
	}
	return false, "unknown golden.type"
}

func evalAnswerMatch(g Golden, answer string) (bool, string) {
	match := g.Match
	if match == "" {
		match = "substring"
	}
	switch match {
	case "set_equal":
		got := strings.Fields(strings.ReplaceAll(answer, "\n", " "))
		gotSet := make(map[string]struct{}, len(got))
		for _, s := range got {
			gotSet[s] = struct{}{}
		}
		wantSet := make(map[string]struct{}, len(g.Expected))
		for _, w := range g.Expected {
			wantSet[w] = struct{}{}
		}
		for w := range wantSet {
			if _, ok := gotSet[w]; !ok {
				return false, "missing: " + w
			}
		}
		if len(gotSet) != len(wantSet) {
			return false, "size mismatch"
		}
		return true, ""
	case "substring":
		for _, want := range g.Expected {
			if !strings.Contains(answer, want) {
				return false, "missing: " + want
			}
		}
		return true, ""
	case "regex":
		for _, want := range g.Expected {
			re, err := regexp.Compile(want)
			if err != nil {
				return false, "bad regex: " + want
			}
			if !re.MatchString(answer) {
				return false, "no match: " + want
			}
		}
		return true, ""
	}
	return false, "unknown match mode"
}
