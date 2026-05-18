package bench

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".codesearch":  true,
}

// CloneSandbox copies src into a fresh tmpdir minus heavy skip dirs.
// Returns the new dir and a cleanup func that removes it.
func CloneSandbox(src string) (string, func(), error) {
	dst, err := os.MkdirTemp("", "codesearch-bench-*")
	if err != nil {
		return "", nil, fmt.Errorf("mkdir tmp: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dst) }

	err = filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() && skipDirs[info.Name()] {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("walk %s: %w", src, err)
	}
	return dst, cleanup, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
