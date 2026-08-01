package main

import "os/exec"

// cmdPid returns the process PID of a started command, or 0.
func cmdPid(cmd *exec.Cmd) int {
	if cmd != nil && cmd.Process != nil {
		return cmd.Process.Pid
	}
	return 0
}
