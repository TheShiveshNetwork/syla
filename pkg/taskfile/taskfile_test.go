package taskfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTaskfile(t *testing.T) {
	content := `---
agent: codex
model: gpt-4
max_iterations: 10
stop_on: "no_diff_for(3)"
---
Hello world
`
	dir := t.TempDir()
	path := filepath.Join(dir, "task.md")
	os.WriteFile(path, []byte(content), 0644)
	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tf.Frontmatter.Agent != "codex" {
		t.Fatalf("agent %q", tf.Frontmatter.Agent)
	}
	if tf.Body != "Hello world" {
		t.Fatalf("body %q", tf.Body)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	tf, err := ParseBytes("task.md", []byte("just body"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tf.Body != "just body" {
		t.Fatalf("body %q", tf.Body)
	}
}
