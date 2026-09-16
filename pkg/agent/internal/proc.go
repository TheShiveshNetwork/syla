package internal

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// SignalChildProcess sends a signal to a child process, handling detached process groups.
func SignalChildProcess(cmd *exec.Cmd, detached bool, sig os.Signal) error {
	if detached && cmd.Process != nil {
		if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
			if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
				if err := syscall.Kill(-pgid, sig.(syscall.Signal)); err == nil {
					return nil
				}
			}
		}
	}
	if cmd.Process != nil {
		return cmd.Process.Signal(sig)
	}
	return nil
}

// ShutdownChildProcess attempts graceful shutdown with SIGTERM then SIGKILL.
func ShutdownChildProcess(cmd *exec.Cmd, detached bool, timeout time.Duration) error {
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil
	}
	if cmd.Process == nil {
		return nil
	}
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	_ = SignalChildProcess(cmd, detached, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() {
		_, err := cmd.Process.Wait()
		done <- err
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		_ = SignalChildProcess(cmd, detached, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
		return nil
	}
}
