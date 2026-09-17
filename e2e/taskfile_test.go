package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TheShiveshNetwork/syla/pkg/taskfile"
)

func TestTaskfileParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.md")
	content := `---
agent: claude
workspace: git
max_iterations: 5
max_duration: 2h
budget_usd: 10.00
stop_on: "no_diff_for(3)"
model: gpt-4
max_tokens: 1000
max_rate_limit_wait: 1h
prevent_sleep: false
---

# Task

Do something
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	tf, err := taskfile.Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tf.Frontmatter.Agent != "claude" {
		t.Fatalf("agent %q", tf.Frontmatter.Agent)
	}
	if tf.Frontmatter.Workspace != "git" {
		t.Fatalf("workspace %q", tf.Frontmatter.Workspace)
	}
	if tf.Frontmatter.MaxIterations == nil || *tf.Frontmatter.MaxIterations != 5 {
		t.Fatalf("max_iterations")
	}
	if tf.Frontmatter.Model != "gpt-4" {
		t.Fatalf("model %q", tf.Frontmatter.Model)
	}
	if tf.Frontmatter.MaxTokens == nil || *tf.Frontmatter.MaxTokens != 1000 {
		t.Fatalf("max_tokens")
	}
	if tf.Body != "# Task\n\nDo something" {
		t.Fatalf("body %q", tf.Body)
	}
	if tf.Frontmatter.GetKeepAwake() != false {
		t.Fatalf("keep_awake should be false")
	}
}

func TestTaskfileNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.md")
	content := "# Just a task\nNo frontmatter"
	os.WriteFile(path, []byte(content), 0644)
	tf, err := taskfile.Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tf.Body != content {
		t.Fatalf("body mismatch")
	}
	if tf.Frontmatter.Agent != "" {
		t.Fatalf("agent should be empty")
	}
}

func TestTaskfileStopWhenAlias(t *testing.T) {
	tf, err := taskfile.ParseBytes("task.md", []byte("---\nstop_on: \"no_diff_for(3)\"\n---\nbody"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tf.Frontmatter.StopWhen != "no_diff_for(3)" {
		t.Fatalf("stop_when alias failed %q", tf.Frontmatter.StopWhen)
	}
}
