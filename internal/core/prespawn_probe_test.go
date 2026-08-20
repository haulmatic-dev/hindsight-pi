package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Pre-spawn gate probe (2026-08-20): 65wa0 burned 7.9M tokens across 3
// empty-diff reject rounds because its suite was ALREADY green on main —
// nothing for the worker to do. If the bead's scoped gate passes before
// spawning, the bead closes immediately at zero token cost.
//
// Probe rules:
//   - only scoped gates (cmd contains <scope>) — full-suite probes are too
//     expensive pre-spawn
//   - only when the bead declares target files (the scope source)
//   - fail-OPEN: probe errors never block a spawn (it is an optimization)

func TestPrespawnProbe_AlreadyGreen(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	gates := &GateSet{
		DaemonOwned: true,
		Test:        GateSpec{Cmd: "true # <scope>", Parser: "exit-code"},
	}
	bead := &types.Bead{ID: "proj-p1", Project: "proj",
		Metadata: map[string]string{"files": "src/x/a.spec.ts"}}
	if !PrespawnProbePasses(r, gates, bead, t.TempDir()) {
		t.Error("passing scoped gate must report already-green")
	}
}

func TestPrespawnProbe_RedSpawns(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	gates := &GateSet{
		DaemonOwned: true,
		Test:        GateSpec{Cmd: "false # <scope>", Parser: "exit-code"},
	}
	bead := &types.Bead{ID: "proj-p2", Project: "proj",
		Metadata: map[string]string{"files": "src/x/a.spec.ts"}}
	if PrespawnProbePasses(r, gates, bead, t.TempDir()) {
		t.Error("failing scoped gate must spawn a worker")
	}
}

func TestPrespawnProbe_SkipsUnscopedGates(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	// Full-suite gate (no <scope>): probing it pre-spawn is too expensive.
	gates := &GateSet{
		DaemonOwned: true,
		Test:        GateSpec{Cmd: "true", Parser: "exit-code"},
	}
	bead := &types.Bead{ID: "proj-p3", Project: "proj",
		Metadata: map[string]string{"files": "src/x/a.spec.ts"}}
	if PrespawnProbePasses(r, gates, bead, t.TempDir()) {
		t.Error("unscoped gates must not be probed pre-spawn")
	}
}

func TestPrespawnProbe_NoTargetsSpawns(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	gates := &GateSet{
		DaemonOwned: true,
		Test:        GateSpec{Cmd: "true # <scope>", Parser: "exit-code"},
	}
	bead := &types.Bead{ID: "proj-p4", Project: "proj"}
	if PrespawnProbePasses(r, gates, bead, t.TempDir()) {
		t.Error("no target files = no scope = no probe")
	}
}

func TestPrespawnProbe_DisabledSpawns(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	gates := &GateSet{
		DaemonOwned: false,
		Test:        GateSpec{Cmd: "true # <scope>", Parser: "exit-code"},
	}
	bead := &types.Bead{ID: "proj-p5", Project: "proj",
		Metadata: map[string]string{"files": "src/x/a.spec.ts"}}
	if PrespawnProbePasses(r, gates, bead, t.TempDir()) {
		t.Error("daemon_owned=false must never probe")
	}
}

func TestPrespawnProbe_RunnerFaultFailsOpen(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	gates := &GateSet{
		DaemonOwned: true,
		Test:        GateSpec{Cmd: "true # <scope>", Parser: "bogus-parser"},
	}
	bead := &types.Bead{ID: "proj-p6", Project: "proj",
		Metadata: map[string]string{"files": "src/x/a.spec.ts"}}
	if PrespawnProbePasses(r, gates, bead, t.TempDir()) {
		t.Error("probe fault must fail OPEN (spawn normally), never block work")
	}
}
