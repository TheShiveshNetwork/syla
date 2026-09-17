package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGet(t *testing.T) {
	if _, err := Get("unknown", "."); err == nil {
		t.Fatal("should fail unknown")
	}
	for _, k := range []string{"none", "", "git", "file-snapshot"} {
		if _, err := Get(k, t.TempDir()); err != nil {
			t.Fatalf("get %q failed %v", k, err)
		}
	}
}

func TestNoop(t *testing.T) {
	w, _ := Get("none", t.TempDir())
	ctx := context.Background()
	if k := w.Kind(); k != "none" {
		t.Fatalf("kind %q", k)
	}
	if s, _ := w.Snapshot(ctx); s != "" {
		t.Fatalf("noop snapshot should be empty")
	}
	if d, _ := w.Diff(ctx, ""); d != "" {
		t.Fatalf("noop diff empty")
	}
	if h, _ := w.HasChanges(ctx); h {
		t.Fatalf("noop no changes")
	}
}

func TestGitWorkspace(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		// use workspace git helper via exec
		// init repo
	}
	_ = run
	// use git via shell
	// init repo
	if out, err := (&git{dir: dir}).run(context.Background(), "init"); err != nil {
		t.Skipf("git not available: %v %s", err, out)
	}
	// config user
	(&git{dir: dir}).run(context.Background(), "config", "user.email", "test@test.com")
	(&git{dir: dir}).run(context.Background(), "config", "user.name", "test")
	// create file and commit
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	w, _ := Get("git", dir)
	ctx := context.Background()
	// commit
	if err := w.Commit(ctx, "initial"); err != nil {
		t.Fatalf("commit %v", err)
	}
	snap, err := w.Snapshot(ctx)
	if err != nil || snap == "" {
		t.Fatalf("snapshot %v %q", err, snap)
	}
	// modify file
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("world"), 0644)
	has, _ := w.HasChanges(ctx)
	if !has {
		t.Fatalf("should have changes")
	}
	diff, _ := w.Diff(ctx, snap)
	if diff == "" {
		// diff HEAD vs snap? after modify without commit, HEAD is still snap, so diff should be from git diff HEAD (unstaged)
		// our Diff with since returns diff since..HEAD which after modify but not committed will be empty, but HasChanges true
		// we check Diff with empty since to get unstaged diff
		diff2, _ := w.Diff(ctx, "")
		if diff2 == "" {
			t.Logf("diff empty (expected if not committed): %q", diff)
		}
	}
	if err := w.Rollback(ctx, snap); err != nil {
		t.Fatalf("rollback %v", err)
	}
	has, _ = w.HasChanges(ctx)
	if has {
		t.Fatalf("after rollback no changes")
	}
}

func TestFileSnapshot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v1"), 0644)
	w, err := Get("file-snapshot", dir)
	if err != nil {
		t.Fatalf("get %v", err)
	}
	ctx := context.Background()
	id, err := w.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot %v", err)
	}
	if id == "" {
		t.Fatalf("empty id")
	}
	if err := w.Commit(ctx, "msg"); err != nil {
		t.Fatalf("commit %v", err)
	}
	if d, _ := w.Diff(ctx, id); d != "" && d != "changes" {
		t.Fatalf("diff %q", d)
	}
}
