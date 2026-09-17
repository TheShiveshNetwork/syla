package loop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Checkpoint persists run state to disk.
type Checkpoint struct {
	RunID       string    `json:"run_id"`
	RunDir      string    `json:"run_dir"`
	Provider    string    `json:"provider"`
	Iteration   int       `json:"iteration"`
	Successes   int       `json:"successes"`
	Failures    int       `json:"failures"`
	Consecutive int      `json:"consecutive"`
	TotalTokens int       `json:"total_tokens"`
	TotalUSD    float64   `json:"total_usd"`
	Status      string    `json:"status"`
	StopReason  string    `json:"stop_reason"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	TaskHash    string    `json:"task_hash"`
}

func checkpointPath(runDir string) string {
	return filepath.Join(runDir, "checkpoint.json")
}

func SaveCheckpoint(runDir string, cp *Checkpoint) error {
	cp.UpdatedAt = time.Now()
	if cp.StartedAt.IsZero() {
		cp.StartedAt = cp.UpdatedAt
	}
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	tmp := checkpointPath(runDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, checkpointPath(runDir))
}

func LoadCheckpoint(runDir string) (*Checkpoint, error) {
	data, err := os.ReadFile(checkpointPath(runDir))
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func SaveIteration(runDir string, iter int, prompt, output string, usage map[string]int) error {
	dir := filepath.Join(runDir, fmt.Sprintf("iter-%d", iter))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte(prompt), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "output.txt"), []byte(output), 0644); err != nil {
		return err
	}
	meta := map[string]any{
		"iteration": iter,
		"usage":     usage,
		"time":      time.Now().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0644)
}
