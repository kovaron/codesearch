package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeJoin returns the absolute path of rel resolved against root after
// rejecting any traversal that escapes root via `..` or absolute paths.
// It is the only sandbox-confinement check the read/edit tools rely on.
func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.Join(root, rel))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanAbs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(cleanAbs, rootAbs+string(filepath.Separator)) && cleanAbs != rootAbs {
		return "", fmt.Errorf("path %q escapes sandbox", rel)
	}
	return cleanAbs, nil
}

// ReadFileTool reads rel relative to root, rejecting any escape via
// `..`. Returns the file contents as a string.
func ReadFileTool(root, rel string) (string, error) {
	p, err := safeJoin(root, rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EditFileTool replaces the first occurrence of oldStr with newStr in
// the file at rel (relative to root). Errors if oldStr is not present.
func EditFileTool(root, rel, oldStr, newStr string) error {
	p, err := safeJoin(root, rel)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), oldStr) {
		return fmt.Errorf("old_string not found in %s", rel)
	}
	updated := strings.Replace(string(b), oldStr, newStr, 1)
	return os.WriteFile(p, []byte(updated), 0o644)
}
