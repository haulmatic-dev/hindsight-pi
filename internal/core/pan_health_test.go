package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestProjectKey(t *testing.T) {
	cases := map[string]string{
		"/home/nipuna.j/projects/webapp":    "nipuna.j/webapp",
		"/home/ajantha/projects/webapp":     "ajantha/webapp",
		"/opt/dev-tools/components/hydra":   "hydra",
		"/test/test-project":                "test-project",
		"/home/u/projects/grand/parent/repo": "u/grand/parent/repo",
	}
	for path, want := range cases {
		if got := projectKey(path); got != want {
			t.Errorf("projectKey(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestCollectHealthDigestChainedParallel(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer

	// Two dep-chained beads, both with live workers: the c36 blast signature.
	a := types.Bead{ID: "test-project-aaa", Project: "test-project", Status: types.StatusInProgress, Type: "implement"}
	b := types.Bead{ID: "test-project-bbb", Project: "test-project", Status: types.StatusInProgress, Type: "implement",
		Dependencies: []types.BeadID{"test-project-aaa"}}
	parked := types.Bead{ID: "test-project-ccc", Project: "test-project", Status: types.StatusOpen, Type: "implement",
		Dependencies: []types.BeadID{"test-project-bbb"}}
	done := types.Bead{ID: "test-project-ddd", Project: "test-project", Status: types.StatusClosed, Type: "implement"}
	tw.mockBeads.beads["test-project-aaa"] = a
	tw.mockBeads.beads["test-project-bbb"] = b
	tw.mockBeads.beads["test-project-ccc"] = parked
	tw.mockBeads.beads["test-project-ddd"] = done

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	o.registryStore = st
	for _, id := range []string{"test-project-aaa", "test-project-bbb"} {
		if err := st.UpsertWorker(registry.WorkerRecord{
			BeadID: id, PID: 1, Project: "test-project", State: registry.StateWorking,
			StartedAt: time.Now(), LastHeartbeat: time.Now(),
		}); err != nil {
			t.Fatalf("UpsertWorker: %v", err)
		}
	}

	d := o.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	ph := d.Projects[0]
	if ph.Key != "test-project" {
		t.Errorf("key = %q", ph.Key)
	}
	if ph.LiveWorkers != 2 {
		t.Errorf("LiveWorkers = %d, want 2", ph.LiveWorkers)
	}
	if ph.ChainedParallel != 1 {
		t.Errorf("ChainedParallel = %d, want 1", ph.ChainedParallel)
	}
	if len(ph.Anomalies) == 0 || !strings.Contains(ph.Anomalies[0], "chain discipline") {
		t.Errorf("expected chain-discipline anomaly, got %v", ph.Anomalies)
	}
	// bbb (dep on in-progress aaa) and ccc (dep on in-progress bbb) both have
	// unmet deps; ddd is terminal and not counted.
	if ph.DepBlocked != 2 {
		t.Errorf("DepBlocked = %d, want 2", ph.DepBlocked)
	}
	if ph.Open != 1 || ph.InProgress != 2 {
		t.Errorf("Open=%d InProgress=%d, want 1/2", ph.Open, ph.InProgress)
	}
}

func TestCollectHealthDigestNoWorkersNoAnomaly(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	tw.mockBeads.beads["test-project-aaa"] = types.Bead{
		ID: "test-project-aaa", Project: "test-project", Status: types.StatusOpen, Type: "implement"}

	d := o.collectHealthDigest()
	if len(d.Anomalous()) != 0 {
		t.Errorf("healthy project must not be anomalous: %v", d.Anomalous())
	}
}

func TestParkCampaignRepair(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer

	open1 := types.Bead{ID: "test-project-aaa", Project: "test-project", Status: types.StatusOpen, Type: "implement"}
	open2 := types.Bead{ID: "test-project-bbb", Project: "test-project", Status: types.StatusInProgress, Type: "test"}
	epic := types.Bead{ID: "test-project-eee", Project: "test-project", Status: types.StatusOpen, Type: "epic"}
	closed := types.Bead{ID: "test-project-zzz", Project: "test-project", Status: types.StatusClosed, Type: "implement"}
	for _, b := range []types.Bead{open1, open2, epic, closed} {
		tw.mockBeads.beads[b.ID.String()] = b
	}

	target := pan.CampaignAnomalyID("test-project")
	if err := o.parkCampaignRepair(target); err != nil {
		t.Fatalf("parkCampaignRepair: %v", err)
	}

	// A hold gate was created and both non-container beads depend on it.
	var gateID string
	for _, b := range tw.mockBeads.beads {
		if b.Type == "approval-gate" && strings.Contains(b.Title, panHoldMarker) {
			gateID = b.ID.String()
		}
	}
	if gateID == "" {
		t.Fatal("no pan hold gate created")
	}
	wantDeps := map[string]bool{
		"test-project-aaa->" + gateID: true,
		"test-project-bbb->" + gateID: true,
	}
	for _, got := range tw.mockBeads.depAdds {
		if !wantDeps[got] {
			t.Errorf("unexpected dep add %s (epic/closed beads must not park)", got)
		}
		delete(wantDeps, got)
	}
	if len(wantDeps) != 0 {
		t.Errorf("missing dep adds: %v", wantDeps)
	}

	// Idempotent: a second repair reuses the gate instead of stacking one.
	beadCount := len(tw.mockBeads.beads)
	if err := o.parkCampaignRepair(target); err != nil {
		t.Fatalf("second parkCampaignRepair: %v", err)
	}
	if len(tw.mockBeads.beads) != beadCount {
		t.Errorf("second repair created another gate (%d -> %d beads)", beadCount, len(tw.mockBeads.beads))
	}
}

func TestParkCampaignRepairRejectsRealBeads(t *testing.T) {
	tw := setupTestOverseer(t)
	if err := tw.overseer.parkCampaignRepair("test-project-aaa"); err == nil {
		t.Fatal("park-campaign must refuse a non-pseudo target")
	}
}

func TestLogImprovementRepair(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	// TestMain pins HYDRA_HOME to a temp dir; use a per-test subdir to isolate.
	dir := t.TempDir()
	t.Setenv("HYDRA_HOME", dir)

	if err := o.logImprovementRepair("test-project-campaign-anomaly", "repair-worktree fails 24x; handler needs investigation"); err != nil {
		t.Fatalf("logImprovementRepair: %v", err)
	}
	if err := o.logImprovementRepair("test-project-campaign-anomaly", "second\nmultiline entry"); err != nil {
		t.Fatalf("second logImprovementRepair: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "pan-improvements.log"))
	if err != nil {
		t.Fatalf("read improvements log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 improvement lines, got %d: %q", len(lines), data)
	}
	if !strings.Contains(lines[0], "[test-project-campaign-anomaly]") || !strings.Contains(lines[0], "repair-worktree fails 24x") {
		t.Errorf("line 0 malformed: %q", lines[0])
	}
	if strings.Contains(lines[1], "\n") || !strings.Contains(lines[1], "second multiline entry") {
		t.Errorf("multiline text must be flattened: %q", lines[1])
	}
}
