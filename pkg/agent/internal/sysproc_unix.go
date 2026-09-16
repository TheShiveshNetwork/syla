//go:build !windows

package internal

import "syscall"

func NewSysProcAttr(detached bool) *syscall.SysProcAttr {
	if detached {
		return &syscall.SysProcAttr{Setpgid: true}
	}
	return nil
}
