package main

import (
	"syscall"
	"testing"
)

// Process-hygiene (c): Pi must launch in its own process group with
// PDEATHSIG so a dead worker's Pi child is kernel-killed instead of
// reparenting to PID 1 with its node/jest grandchildren.
func TestPiSysProcAttr(t *testing.T) {
	a := piSysProcAttr()
	if a == nil || !a.Setpgid {
		t.Fatalf("Setpgid required, got %+v", a)
	}
	if a.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("Pdeathsig = %v, want SIGKILL", a.Pdeathsig)
	}
}
