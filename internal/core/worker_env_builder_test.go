package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestWorkerEnv_SetsPiOffline verifies workers run Pi with PI_OFFLINE=1 so Pi
// skips its startup provider-list fetch (which flakes under concurrent worker
// launches) and uses the local models.json cache instead.
func TestWorkerEnv_SetsPiOffline(t *testing.T) {
	b := NewWorkerEnvBuilder(DefaultConfig())
	env := b.Build(types.Bead{ID: "x", Project: "p"}, "/tmp/p", "orchestrator", 0)

	found := false
	for _, e := range env {
		if e == "PI_OFFLINE=1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("worker env must set PI_OFFLINE=1")
	}
}
