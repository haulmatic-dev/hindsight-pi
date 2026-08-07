package core

import "testing"

// A worker process dying by signal (negative exit code) is NEVER a bead code
// fault: the worker's own code deliberately exits 1/2/3/5/6/7, while a
// negative code means an external kill — Charon's hung-worker SIGKILL, the OOM
// killer, or an operator. All of those are infrastructure. Misclassifying them
// as bead faults was the dominant churn driver on 2026-08-07: 99 exit -1
// failures in 2h, each spawning fix beads that died the same way (see
// hydra-code-9p7: park-wake resume wedge → Charon hung-kill → exit -1 →
// isInfra=false).
func TestIsInfrastructureExitCode_SignalDeathsAreInfra(t *testing.T) {
	for _, code := range []int{-1, -2, -9, -15} {
		if !isInfrastructureExitCode(code) {
			t.Errorf("isInfrastructureExitCode(%d) = false, want true (signal death is external/infra)", code)
		}
	}
	// The worker's own deliberate failure codes stay bead faults.
	for _, code := range []int{1, 5} {
		if isInfrastructureExitCode(code) {
			t.Errorf("isInfrastructureExitCode(%d) = true, want false (deliberate worker exit = bead fault)", code)
		}
	}
}
