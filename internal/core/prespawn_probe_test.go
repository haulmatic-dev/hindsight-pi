package core

import (
	"strings"
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
	pass_, _ := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if !pass_ {
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
	pass_, _ := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if pass_ {
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
	pass_, _ := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if pass_ {
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
	pass_, _ := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if pass_ {
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
	pass_, _ := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if pass_ {
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
	pass_, _ := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if pass_ {
		t.Error("probe fault must fail OPEN (spawn normally), never block work")
	}
}

// Scheduler-path beads carry an empty Project field — the project lives in
// the bead ID prefix (the codebase pattern: worker.BeadID.Project()). The
// probe used bead.Project directly and silently never fired (zero Prespawn
// log lines while 65wa0 burned 20.7M tokens, 2026-08-21).
func TestBeadProject_DerivesFromID(t *testing.T) {
	b := &types.Bead{ID: "srv-smart-engine-65wa0"} // Project field empty
	if got := beadProject(b); got != "srv-smart-engine" {
		t.Errorf("beadProject = %q, want srv-smart-engine", got)
	}
	b2 := &types.Bead{ID: "srv-smart-engine-x", Project: "explicit"}
	if got := beadProject(b2); got != "explicit" {
		t.Errorf("explicit Project field wins, got %q", got)
	}
}

// Option A (context priming): a RED probe knows exactly why the suite fails —
// the worker currently burns 3-5 discovery turns (and ~100K context each)
// re-deriving it. The probe must hand the evidence over: artifact with
// summary + raw log path on red.
func TestPrespawnProbe_RedReturnsEvidence(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	gates := &GateSet{
		DaemonOwned: true,
		Test:        GateSpec{Cmd: "echo 'FAIL src/x/a.spec.ts: expected 1 got 2' >&2; false # <scope>", Parser: "exit-code"},
	}
	bead := &types.Bead{ID: "proj-p9", Project: "proj",
		Metadata: map[string]string{"files": "src/x/a.spec.ts"}}
	pass, ev := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if pass {
		t.Fatal("red gate must not pass")
	}
	if ev == nil {
		t.Fatal("red probe must return the evidence artifact")
	}
	if !strings.Contains(ev.Summary, "expected 1 got 2") {
		t.Errorf("evidence summary must carry the failure, got %q", ev.Summary)
	}
	if ev.RawLog == "" {
		t.Error("evidence must carry the raw log path")
	}
	if ev.Gate != "prespawn-test" {
		t.Errorf("evidence gate = %q, want prespawn-test", ev.Gate)
	}
}

// Probe evidence lands in the bead description marker-delimited: repeated
// probes REPLACE the block instead of growing the description forever.
func TestAppendProbeEvidence_ReplacesPriorBlock(t *testing.T) {
	d1 := appendProbeEvidence("orig desc", "prespawn-test", GateArtifact{Summary: "first failure", RawLog: "/l1"})
	if !strings.Contains(d1, "orig desc") || !strings.Contains(d1, "first failure") || !strings.Contains(d1, "/l1") {
		t.Fatalf("evidence must append to description, got %q", d1)
	}
	d2 := appendProbeEvidence(d1, "prespawn-test", GateArtifact{Summary: "second failure", RawLog: "/l2"})
	if strings.Contains(d2, "first failure") {
		t.Error("second probe must replace the prior evidence block")
	}
	if !strings.Contains(d2, "second failure") {
		t.Error("second probe evidence must be present")
	}
	if strings.Count(d2, "prespawn-evidence") != 2 { // open + close markers
		t.Errorf("markers must appear exactly once each, got %q", d2)
	}
}
