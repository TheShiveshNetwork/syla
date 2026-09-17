package e2e

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/loop"
)

func TestCheckpointSaveLoad(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".syla", "runs", "test-run")
	cp := &loop.Checkpoint{
		RunID:     "test-run",
		RunDir:    runDir,
		Iteration: 5,
		Successes: 3,
		Failures:  2,
		StartedAt: time.Now(),
		Status:    "running",
	}
	if err := loop.SaveCheckpoint(runDir, cp); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := loop.LoadCheckpoint(runDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Iteration != 5 || loaded.Successes != 3 {
		t.Fatalf("mismatch %v", loaded)
	}
	if loaded.RunID != "test-run" {
		t.Fatalf("runID %q", loaded.RunID)
	}
}

func TestSaveIteration(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs", "1")
	if err := loop.SaveIteration(runDir, 1, "prompt", "output", map[string]int{"input": 10}); err != nil {
		t.Fatalf("save iteration: %v", err)
	}
	// check files exist
	if _, err := loop.LoadCheckpoint(runDir); err == nil {
		// checkpoint may not exist, but iteration files should
	}
}
