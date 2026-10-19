//go:build linux

package main

import "syscall"

// piSysProcAttr launches Pi in its own process group (so a stuck-session
// group-kill cannot hit the worker or daemon) with PR_SET_PDEATHSIG: when the
// worker process dies (crash, SIGKILL, OOM) the kernel delivers SIGKILL to
// the Pi child instead of letting it reparent to PID 1 and leak its node/jest
// grandchildren (observed live: 20+ reparented node processes, ~9G RSS).
// Pi's own grandchildren are covered by Charon's reparented-worktree sweep.
func piSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
