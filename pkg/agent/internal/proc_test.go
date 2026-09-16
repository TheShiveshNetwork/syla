package internal

import (
	"os/exec"
	"testing"
	"time"
)

func TestSignalChildProcessNoProcess(t *testing.T) {
	cmd := &exec.Cmd{}
	if err := SignalChildProcess(cmd, true, nil); err != nil {
		// should return nil when no process
		t.Fatalf("should be nil when no process, got %v", err)
	}
}

func TestShutdownChildProcessNoProcess(t *testing.T) {
	cmd := &exec.Cmd{}
	if err := ShutdownChildProcess(cmd, false, 0); err != nil {
		t.Fatalf("should be nil, got %v", err)
	}
}

func TestShutdownChildProcessWithTimeout(t *testing.T) {
	// Use sleep as a real child to test signal handling
	cmd := exec.Command("sleep", "0.1")
	if err := cmd.Start(); err != nil {
		t.Skipf("skip: cannot start sleep: %v", err)
	}
	start := time.Now()
	err := ShutdownChildProcess(cmd, false, 500*time.Millisecond)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("shutdown err %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("took too long %v", elapsed)
	}
	// Process should be exited
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		// Wait a bit more
		_ = cmd.Wait()
	}
}

func TestNewSysProcAttr(t *testing.T) {
	attr := NewSysProcAttr(true)
	if attr == nil {
		t.Fatalf("should not be nil for detached")
	}
	// non-detached may return nil or not, but should not panic
	_ = NewSysProcAttr(false)
}
