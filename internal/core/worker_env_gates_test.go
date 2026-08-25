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

func TestWorkerEnvBuilder_GateCmdsJSON(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{{
			Name: "proj", Path: "/tmp/proj", Type: "single",
			Gates: &GateSet{
				DaemonOwned: true,
				Test:        GateSpec{Cmd: "npm run test:agent -- <scope>"},
				Lint:        GateSpec{Cmd: "npm run lint"},
				Build:       GateSpec{Cmd: "npm run build"},
			},
		}},
	}
	b := NewWorkerEnvBuilder(cfg)
	env := b.Build(types.Bead{ID: "proj-e3", Project: "proj"}, "/tmp/proj", "implement", 0)
	want := `HYDRA_GATE_CMDS=["npm run test:agent -- <scope>","npm run lint","npm run build"]`
	found := false
	for _, e := range env {
		if e == want {
			found = true
		}
		if strings.HasPrefix(e, "HYDRA_GATE_CMDS") {
			t.Logf("HYDRA_GATE_CMDS value: %s", e)
		}
	}
	if !found {
		t.Errorf("daemon_owned project must set HYDRA_GATE_CMDS=%q", want)
	}
}

func TestWorkerEnvBuilder_GateCmdsAbsentWhenNotDaemonOwned(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{{
			Name: "proj", Path: "/tmp/proj", Type: "single",
			Gates: &GateSet{
				DaemonOwned: false,
				Test:        GateSpec{Cmd: "npm run test:agent -- <scope>"},
			},
		}},
	}
	b := NewWorkerEnvBuilder(cfg)
	env := b.Build(types.Bead{ID: "proj-e4", Project: "proj"}, "/tmp/proj", "implement", 0)
	for _, e := range env {
		if strings.HasPrefix(e, "HYDRA_GATE_CMDS") {
			t.Errorf("non-daemon-owned project must not set HYDRA_GATE_CMDS, got %q", e)
		}
	}
}
