// Package fsops provides filesystem utilities shared by the bench harness and
// the MCP server.
package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skipDirs mirrors the set in internal/bench/sandbox.go. Files inside these
// directories are never matched or modified.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".codesearch":  true,
}

// ReplaceInFiles walks workdir, finds files matching pattern, and replaces
// every occurrence of old with new in each file's content. It returns the
// list of changed file paths (relative to workdir) and the total replacement
// count. When dryRun is true no files are written, but the list of files that
// would have been changed and the replacement count are still returned.
//
// Pattern matching supports two forms:
//   - "**/TAIL" — match any file whose base name matches TAIL (filepath.Match
//     semantics) regardless of directory depth.
//   - Anything else — match against the path relative to workdir using
//     filepath.Match directly (e.g. "*.go" matches only top-level .go files;
//     "internal/*.go" matches .go files one level deep inside internal/).
//
// The empty string is not valid for pattern or old; new may be empty (deletion).
func ReplaceInFiles(workdir, pattern, old, newStr string, dryRun bool) (changed []string, total int, err error) {
	if pattern == "" {
		return nil, 0, errors.New("replace_in_files: pattern must not be empty")
	}
	if old == "" {
		return nil, 0, errors.New("replace_in_files: old must not be empty")
	}

	recursive, tail := parsePattern(pattern)

	walkErr := filepath.WalkDir(workdir, func(path string, d os.DirEntry, we error) error {
		if we != nil {
			return we
		}
		rel, relErr := filepath.Rel(workdir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		// Skip heavy dirs.
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Match pattern.
		matched, matchErr := matchPattern(recursive, tail, pattern, rel)
		if matchErr != nil {
			return fmt.Errorf("replace_in_files: match %q: %w", rel, matchErr)
		}
		if !matched {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("replace_in_files: read %s: %w", rel, readErr)
		}
		content := string(data)
		n := strings.Count(content, old)
		if n == 0 {
			return nil
		}
		replaced := strings.ReplaceAll(content, old, newStr)
		if !dryRun {
			info, stErr := d.Info()
			if stErr != nil {
				return fmt.Errorf("replace_in_files: stat %s: %w", rel, stErr)
			}
			if writeErr := os.WriteFile(path, []byte(replaced), info.Mode()); writeErr != nil {
				return fmt.Errorf("replace_in_files: write %s: %w", rel, writeErr)
			}
		}
		changed = append(changed, rel)
		total += n
		return nil
	})
	if walkErr != nil {
		return nil, 0, walkErr
	}
	return changed, total, nil
}

// parsePattern splits a pattern into (recursive, tail).
// If the pattern starts with "**/" the recursive flag is true and tail is the
// remainder. Otherwise recursive is false and the full pattern is the tail.
func parsePattern(pattern string) (recursive bool, tail string) {
	if strings.HasPrefix(pattern, "**/") {
		return true, pattern[3:]
	}
	return false, pattern
}

// matchPattern returns true when the relative path rel matches the compiled pattern.
func matchPattern(recursive bool, tail, fullPattern, rel string) (bool, error) {
	if recursive {
		// Match tail against the base name of rel so **/*.go matches a/b/c/x.go.
		return filepath.Match(tail, filepath.Base(rel))
	}
	return filepath.Match(fullPattern, rel)
}
