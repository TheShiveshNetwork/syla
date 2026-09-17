package workspace

import (
	"context"
)

// Workspace owns snapshot/commit/rollback/diff for an iteration boundary.
type Workspace interface {
	Kind() string
	Snapshot(ctx context.Context) (string, error)
	Diff(ctx context.Context, since string) (string, error)
	Commit(ctx context.Context, message string) error
	Rollback(ctx context.Context, to string) error
	HasChanges(ctx context.Context) (bool, error)
}

// Factory picks workspace by name.
func Get(kind string, dir string) (Workspace, error) {
	switch kind {
	case "", "none", "noop":
		return NewNoop(dir), nil
	case "git":
		return NewGit(dir), nil
	case "file-snapshot", "file_snapshot", "snapshot":
		return NewFileSnapshot(dir)
	default:
		return nil, errUnknownWorkspace(kind)
	}
}

func errUnknownWorkspace(k string) error {
	return &unknownError{k}
}

type unknownError struct{ k string }

func (e *unknownError) Error() string { return "unknown workspace: " + e.k }
