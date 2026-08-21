package core

import (
	"os"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Close-path wiring (FR-002 slice 2): when a project declares
// gates.daemon_owned, a successful worker completion must run the declared
// gates daemon-side before the bead closes. The close path contains zero LLM
// calls and zero agent-quoted evidence: pass/fail comes from the persisted
// artifact. Gate failure reopens the bead (rejectClose machinery), labels it,
// and hands the retry worker the parsed summary + raw log path via the bead
// description — never license to re-run the suite.

func gateTestConfig(daemonOwned bool, testCmd string) *Config {
	return &Config{
		Projects: []ProjectConfig{{
			Name: "proj",
			Path: "/tmp/proj",
			Type: "single",
			Gates: &GateSet{
				DaemonOwned: daemonOwned,
				Test:        GateSpec{Cmd: testCmd, Parser: "exit-code"},
			},
		}},
	}
}

func gateTestCloser(t *testing.T, cfg *Config, projectPath string) (*BeadCloser, *mockBeadsClient) {
	t.Helper()
	mock := newMockBeadsClient()
	closer := NewBeadCloser(mock, nil, nil, nil, cfg)
	closer.SetGateRunner(NewGateRunner(t.TempDir()))
	return closer, mock
}

func TestDaemonGates_DisabledSkips(t *testing.T) {
	closer, _ := gateTestCloser(t, gateTestConfig(false, "exit 1"), t.TempDir())
	bead := &types.Bead{ID: "proj-g1", Project: "proj", Status: types.StatusInProgress}
	if err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", ""); err != nil {
		t.Fatalf("daemon_owned=false must skip gates even when they would fail: %v", err)
	}
}

func TestDaemonGates_Pass(t *testing.T) {
	wt := t.TempDir()
	closer, _ := gateTestCloser(t, gateTestConfig(true, "true"), wt)
	bead := &types.Bead{ID: "proj-g2", Project: "proj", Status: types.StatusInProgress}
	if err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", wt); err != nil {
		t.Fatalf("passing gate must not block close: %v", err)
	}
}

func TestDaemonGates_FailBlocksAndAnnotates(t *testing.T) {
	wt := t.TempDir()
	closer, mock := gateTestCloser(t, gateTestConfig(true, "echo 'gate exploded' && exit 1"), wt)
	bead := &types.Bead{ID: "proj-g3", Project: "proj", Status: types.StatusInProgress, Description: "orig"}
	mock.beads["proj-g3"] = *bead

	err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", wt)
	if err == nil {
		t.Fatal("failing gate must block close")
	}
	if !strings.Contains(err.Error(), "gate") {
		t.Errorf("error must name the gate path, got %v", err)
	}
	b := mock.beads["proj-g3"]
	if b.Status != types.StatusOpen {
		t.Errorf("bead must be reopened, status = %s", b.Status)
	}
	joined := strings.Join(b.Labels, ",")
	if !strings.Contains(joined, "gate-failed") {
		t.Errorf("bead must carry gate-failed label, got %v", b.Labels)
	}
	// The retry worker reads the description: it must carry the parsed
	// summary and the raw log path, not license to re-run the suite.
	if !strings.Contains(b.Description, "gate exploded") {
		t.Errorf("description must contain gate summary, got %q", b.Description)
	}
	if !strings.Contains(b.Description, "gates/proj/proj-g3/test.log") {
		t.Errorf("description must reference raw log path, got %q", b.Description)
	}
}

func TestDaemonGates_NoWorktreeFailsClosed(t *testing.T) {
	closer, _ := gateTestCloser(t, gateTestConfig(true, "true"), t.TempDir())
	bead := &types.Bead{ID: "proj-g4", Project: "proj", Status: types.StatusInProgress}
	// empty worktree path + daemon_owned: no evidence, no close.
	if err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", ""); err == nil {
		t.Fatal("daemon_owned without a worktree must fail closed")
	}
}

func TestDaemonGates_RunnerFaultFailsClosed(t *testing.T) {
	wt := t.TempDir()
	closer, _ := gateTestCloser(t, gateTestConfig(true, "true"), wt)
	// daemon_owned but no runner attached = misconfiguration; never silently pass.
	closer.gateRunner = nil
	bead := &types.Bead{ID: "proj-g5", Project: "proj", Status: types.StatusInProgress}
	if err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", wt); err == nil {
		t.Fatal("daemon_owned without a gate runner must fail closed")
	}
}

func TestDaemonGates_UnknownParserBlocks(t *testing.T) {
	wt := t.TempDir()
	cfg := gateTestConfig(true, "true")
	cfg.Projects[0].Gates.Test.Parser = "bogus"
	closer, _ := gateTestCloser(t, cfg, wt)
	bead := &types.Bead{ID: "proj-g6", Project: "proj", Status: types.StatusInProgress}
	if err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", wt); err == nil {
		t.Fatal("unknown parser in close path must fail closed")
	}
}

func TestDaemonGates_ScopeFromTargetFiles(t *testing.T) {
	wt := t.TempDir()
	cfg := gateTestConfig(true, `echo "scoped: <scope>"`)
	closer, mock := gateTestCloser(t, cfg, wt)
	bead := &types.Bead{ID: "proj-g7", Project: "proj", Status: types.StatusInProgress,
		Metadata: map[string]string{"files": "src/x/a.spec.ts"}}
	mock.beads["proj-g7"] = *bead
	if err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", wt); err != nil {
		t.Fatalf("passing scoped gate must not block: %v", err)
	}
	a, err := LoadGateArtifact(closer.gateRunner.root, "proj", "proj-g7", "test")
	if err != nil {
		t.Fatalf("artifact must persist: %v", err)
	}
	data, _ := os.ReadFile(a.RawLog)
	if !strings.Contains(string(data), "scoped: 'src/x/a.spec.ts'") {
		t.Errorf("scope must come from bead target files, log: %q", data)
	}
}

func TestDaemonOwnedGatesLookup(t *testing.T) {
	cfg := &Config{Projects: []ProjectConfig{
		{Name: "on", Gates: &GateSet{DaemonOwned: true}},
		{Name: "off", Gates: &GateSet{DaemonOwned: false}},
		{Name: "none"},
	}}
	if !daemonOwnedGates(cfg, "on") {
		t.Error("daemon_owned=true must report true")
	}
	if daemonOwnedGates(cfg, "off") {
		t.Error("daemon_owned=false must report false")
	}
	if daemonOwnedGates(cfg, "none") {
		t.Error("missing gates block must report false")
	}
	if daemonOwnedGates(nil, "on") {
		t.Error("nil config must report false")
	}
}
