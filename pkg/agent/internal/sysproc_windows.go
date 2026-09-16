//go:build windows

package internal

import "syscall"

func NewSysProcAttr(detached bool) *syscall.SysProcAttr {
	return nil
}
