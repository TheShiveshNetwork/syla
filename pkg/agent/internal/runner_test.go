package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.sh")
	script := "#!/bin/sh\n" + content + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake %v", err)
	}
	return path
}

func TestRunJSONLBasic(t *testing.T) {
	script := `cat <<'EOF'
{"a":1}
{"b":2}
EOF`
	bin := writeFakeScript(t, script)
	var events []map[string]any
	stdoutTail, stderrStr, exitCode, err := RunJSONL(context.Background(), RunnerConfig{
		Bin:      bin,
		Args:     []string{},
		Cwd:      t.TempDir(),
		Detached: false,
	}, func(m map[string]any) {
		events = append(events, m)
	})
	if err != nil {
		t.Fatalf("RunJSONL err %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode %d", exitCode)
	}
	if stderrStr != "" {
		t.Fatalf("stderr %q", stderrStr)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events got %d", len(events))
	}
	if !strings.Contains(stdoutTail, `"a":1`) {
		t.Fatalf("stdoutTail missing a")
	}
}

func TestRunJSONLWithStdin(t *testing.T) {
	// script that cat stdin to stdout as json
	script := `cat`
	bin := writeFakeScript(t, script)
	var events []map[string]any
	_, _, exitCode, err := RunJSONL(context.Background(), RunnerConfig{
		Bin:          bin,
		Args:         []string{},
		Cwd:          t.TempDir(),
		StdinContent: `{"stdin":true}`,
		Detached:     false,
	}, func(m map[string]any) {
		events = append(events, m)
	})
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	if len(events) != 1 || events[0]["stdin"] != true {
		t.Fatalf("events %v", events)
	}
}

func TestRunJSONLAbort(t *testing.T) {
	script := `sleep 1; echo '{"a":1}'`
	bin := writeFakeScript(t, script)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := RunJSONL(ctx, RunnerConfig{
		Bin:  bin,
		Args: []string{},
		Cwd:  t.TempDir(),
	}, func(m map[string]any) {})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}

func TestRunJSONLNonZeroExit(t *testing.T) {
	script := `echo "boom" >&2; echo '{"type":"error","error":{"message":"fail"}}'; exit 2`
	bin := writeFakeScript(t, script)
	stdoutTail, stderrStr, exitCode, err := RunJSONL(context.Background(), RunnerConfig{
		Bin:  bin,
		Args: []string{},
		Cwd:  t.TempDir(),
	}, func(m map[string]any) {})
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if exitCode != 2 {
		t.Fatalf("exitCode %d", exitCode)
	}
	if !strings.Contains(stderrStr, "boom") {
		t.Fatalf("stderr %q", stderrStr)
	}
	if !strings.Contains(stdoutTail, "fail") {
		t.Fatalf("stdoutTail %q", stdoutTail)
	}
}

func TestRunJSONLWithLogPath(t *testing.T) {
	script := `echo '{"a":1}'`
	bin := writeFakeScript(t, script)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.jsonl")
	_, _, _, err := RunJSONL(context.Background(), RunnerConfig{
		Bin:     bin,
		Args:    []string{},
		Cwd:     dir,
		LogPath: logPath,
	}, func(m map[string]any) {})
	if err != nil {
		t.Fatalf("err %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log read %v", err)
	}
	if !strings.Contains(string(data), `"a":1`) {
		t.Fatalf("log content %q", string(data))
	}
}

func TestRunJSONLInvalidBin(t *testing.T) {
	_, _, _, err := RunJSONL(context.Background(), RunnerConfig{
		Bin:  "/nonexistent/bin12345",
		Args: []string{},
		Cwd:  t.TempDir(),
	}, func(m map[string]any) {})
	if err == nil {
		t.Fatalf("should fail for invalid bin")
	}
	if !strings.Contains(err.Error(), "Failed to spawn") {
		t.Fatalf("wrong err %v", err)
	}
}
