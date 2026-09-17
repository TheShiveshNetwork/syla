package workspace

import "context"

type noop struct{ dir string }

func NewNoop(dir string) Workspace { return &noop{dir: dir} }

func (n *noop) Kind() string { return "none" }
func (n *noop) Snapshot(ctx context.Context) (string, error) { return "", nil }
func (n *noop) Diff(ctx context.Context, since string) (string, error) { return "", nil }
func (n *noop) Commit(ctx context.Context, message string) error { return nil }
func (n *noop) Rollback(ctx context.Context, to string) error { return nil }
func (n *noop) HasChanges(ctx context.Context) (bool, error) { return false, nil }
