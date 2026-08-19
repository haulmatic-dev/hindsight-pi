package core

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// The daemon tells the worker when a project's gates are daemon-owned so the
// worker prompt can redirect the agent away from full-suite babysitting.

func TestWorkerEnvBuilder_DaemonGatesFlag(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{{
			Name: "proj", Path: "/tmp/proj", Type: "single",
			Gates: &GateSet{DaemonOwned: true},
		}},
	}
	b := NewWorkerEnvBuilder(cfg)
	env := b.Build(types.Bead{ID: "proj-e1", Project: "proj"}, "/tmp/proj", "implement", 0)
	found := false
	for _, e := range env {
		if e == "HYDRA_DAEMON_GATES=1" {
			found = true
		}
	}
	if !found {
		t.Error("daemon_owned project must set HYDRA_DAEMON_GATES=1 for its workers")
	}
}

func TestWorkerEnvBuilder_DaemonGatesFlagAbsentWhenDisabled(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{{
			Name: "proj", Path: "/tmp/proj", Type: "single",
			Gates: &GateSet{DaemonOwned: false},
		}},
	}
	b := NewWorkerEnvBuilder(cfg)
	env := b.Build(types.Bead{ID: "proj-e2", Project: "proj"}, "/tmp/proj", "implement", 0)
	for _, e := range env {
		if strings.HasPrefix(e, "HYDRA_DAEMON_GATES") {
			t.Errorf("daemon_owned=false must not set the flag, got %q", e)
		}
	}
}
