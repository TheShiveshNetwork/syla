package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type git struct {
	dir string
}

func NewGit(dir string) Workspace { return &git{dir: dir} }

func (g *git) Kind() string { return "git" }

func (g *git) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.dir
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, errBuf.String())
	}
	return strings.TrimSpace(out.String()), nil
}

func (g *git) Snapshot(ctx context.Context) (string, error) {
	return g.run(ctx, "rev-parse", "HEAD")
}

func (g *git) HasChanges(ctx context.Context) (bool, error) {
	out, err := g.run(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (g *git) Diff(ctx context.Context, since string) (string, error) {
	if since == "" {
		return g.run(ctx, "diff", "HEAD")
	}
	return g.run(ctx, "diff", since+"..HEAD")
}

func (g *git) Commit(ctx context.Context, message string) error {
	has, err := g.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !has {
		return nil
	}
	if _, err := g.run(ctx, "add", "-A"); err != nil {
		return err
	}
	_, err = g.run(ctx, "commit", "-m", message)
	return err
}

func (g *git) Rollback(ctx context.Context, to string) error {
	if to == "" {
		return nil
	}
	_, err := g.run(ctx, "reset", "--hard", to)
	if err != nil {
		return err
	}
	_, err = g.run(ctx, "clean", "-fd")
	return err
}
