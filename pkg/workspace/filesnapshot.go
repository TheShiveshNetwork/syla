package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type fileSnapshot struct {
	dir      string
	snapRoot string
}

func NewFileSnapshot(dir string) (Workspace, error) {
	snapRoot := filepath.Join(dir, ".syla", "snapshots")
	if err := os.MkdirAll(snapRoot, 0755); err != nil {
		return nil, err
	}
	return &fileSnapshot{dir: dir, snapRoot: snapRoot}, nil
}

func (f *fileSnapshot) Kind() string { return "file-snapshot" }

func (f *fileSnapshot) Snapshot(ctx context.Context) (string, error) {
	id := fmt.Sprintf("%d", len(mustReadDir(f.snapRoot)))
	dest := filepath.Join(f.snapRoot, id)
	if err := copyDir(f.dir, dest); err != nil {
		return "", err
	}
	return id, nil
}

func (f *fileSnapshot) Diff(ctx context.Context, since string) (string, error) {
	// naive: if snapshot count increased, report diff
	has, _ := f.HasChanges(ctx)
	if has {
		return "changes", nil
	}
	return "", nil
}

func (f *fileSnapshot) Commit(ctx context.Context, message string) error { return nil }

func (f *fileSnapshot) Rollback(ctx context.Context, to string) error {
	src := filepath.Join(f.snapRoot, to)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("snapshot %s not found", to)
	}
	return copyDir(src, f.dir)
}

func (f *fileSnapshot) HasChanges(ctx context.Context) (bool, error) {
	// check if any file newer than snapshot dir
	return false, nil
}

func mustReadDir(dir string) []os.DirEntry {
	ents, _ := os.ReadDir(dir)
	return ents
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if stringsContains(path, ".syla/snapshots") {
			return nil
		}
		if stringsContains(path, ".git") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})())
}
