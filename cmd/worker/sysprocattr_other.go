//go:build !linux

package main

import "syscall"

// piSysProcAttr: Pdeathsig is Linux-only; other platforms keep the existing
// process-group isolation and rely on explicit group kills.
func piSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
