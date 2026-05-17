package indexer

import (
	"context"
	"io/fs"
	"path/filepath"
)

// noiseDirs lists directory names that are always skipped during walks.
// These contain build artifacts, dependencies, version-control metadata, or
// tool state — none of which should be indexed.
var noiseDirs = map[string]struct{}{
	".git":         {},
	"vendor":       {},
	"node_modules": {},
	".claude":      {},
	".codesearch":  {},
	".idea":        {},
	".vscode":      {},
	"dist":         {},
	"build":        {},
	"target":       {},
}

// IsNoiseDir reports whether name should be skipped during recursive walks.
func IsNoiseDir(name string) bool {
	_, ok := noiseDirs[name]
	return ok
}

// WalkFiles invokes fn for every file under root with a known language
// extension. Directories matching IsNoiseDir are skipped wholesale. Unreadable
// entries are silently ignored so a transient permission error doesn't abort
// the walk.
func WalkFiles(root string, fn func(path, lang string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && IsNoiseDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		lang := LanguageFor(path)
		if lang == "unknown" {
			return nil
		}
		return fn(path, lang)
	})
}

// WalkAndIndex walks root and calls IndexFile for every file with a known
// language extension, skipping noise directories. Errors from IndexFile are
// returned, aborting the walk on the first failure.
func (idx *Indexer) WalkAndIndex(ctx context.Context, root string) error {
	return WalkFiles(root, func(path, lang string) error {
		return idx.IndexFile(ctx, path, lang)
	})
}
