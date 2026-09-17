package keepawake

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Keeper holds a sleep-prevention lock.
type Keeper struct {
	cmd     *exec.Cmd
	release func() error
	Held    bool
	Err     error
}

// Acquire tries to prevent sleep. It never returns an error that should abort a run; caller should log and continue.
func Acquire(reason string) *Keeper {
	k := &Keeper{}
	switch runtime.GOOS {
	case "darwin":
		k.cmd = exec.Command("caffeinate", "-dims", "-w", fmt.Sprintf("%d", 999999))
		// Actually for syla we just hold caffeinate without -w, as a background process
		k.cmd = exec.Command("caffeinate", "-dims")
		if err := k.cmd.Start(); err != nil {
			k.Err = fmt.Errorf("caffeinate failed: %w", err)
			return k
		}
		k.Held = true
		k.release = func() error {
			if k.cmd.Process != nil {
				_ = k.cmd.Process.Kill()
				_, _ = k.cmd.Process.Wait()
			}
			return nil
		}
	case "linux":
		// Try systemd-inhibit. We spawn it as a sleep that holds the lock.
		// If not available, fall back to warning.
		if _, err := exec.LookPath("systemd-inhibit"); err == nil {
			k.cmd = exec.Command("systemd-inhibit", "--what=sleep:idle", "--who=syla", "--why="+reason, "--mode=block", "sleep", "infinity")
			if err := k.cmd.Start(); err != nil {
				k.Err = fmt.Errorf("systemd-inhibit failed: %w", err)
				return k
			}
			k.Held = true
			k.release = func() error {
				if k.cmd.Process != nil {
					_ = k.cmd.Process.Kill()
					_, _ = k.cmd.Process.Wait()
				}
				return nil
			}
		} else {
			k.Err = fmt.Errorf("keep-awake not available (no systemd-inhibit, no D-Bus fallback)")
		}
	case "windows":
		// Use SetThreadExecutionState via syscall - implemented in keepawake_windows.go
		if err := acquireWindows(); err != nil {
			k.Err = err
		} else {
			k.Held = true
			k.release = releaseWindows
		}
	default:
		k.Err = fmt.Errorf("keep-awake not supported on %s", runtime.GOOS)
	}
	return k
}

func (k *Keeper) Release() error {
	if k.release != nil {
		err := k.release()
		k.Held = false
		return err
	}
	return nil
}

// stubs for non-windows
func acquireWindows() error { return fmt.Errorf("windows keepawake not implemented on this OS") }
func releaseWindows() error { return nil }
