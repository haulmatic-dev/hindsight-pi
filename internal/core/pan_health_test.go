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
		"/home/nipuna.j/projects/webapp":     "nipuna.j/webapp",
		"/home/ajantha/projects/webapp":      "ajantha/webapp",
		"/opt/dev-tools/components/hydra":    "hydra",
		"/test/test-project":                 "test-project",
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

func TestCollectHealthDigestDiskWatermark(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer

	// Real filesystem is well below 100% on a healthy box: watermark 1 must
	// fire, default watermark must not.
	o.SetDiskWatermark(1, "/")
	d := o.collectHealthDigest()
	if d.DiskUsedPct <= 0 || d.DiskUsedPct > 100 {
		t.Fatalf("DiskUsedPct = %d, want 1..100", d.DiskUsedPct)
	}
	if len(d.HostAnomalies) == 0 || !strings.Contains(d.HostAnomalies[0], "disk usage") {
		t.Errorf("expected disk anomaly at watermark 1, got %v", d.HostAnomalies)
	}
	if !strings.Contains(d.Text(), "host: disk=") {
		t.Errorf("digest text missing host disk line: %q", d.Text())
	}

	o.SetDiskWatermark(101, "/")
	d2 := o.collectHealthDigest()
	if len(d2.HostAnomalies) != 0 {
		t.Errorf("watermark 101 must not fire, got %v", d2.HostAnomalies)
	}
	if d2.DiskUsedPct == 0 {
		t.Errorf("DiskUsedPct telemetry missing below watermark")
	}
	// Text must stay silent when no host anomalies: stable digests (PR #236).
	if strings.Contains(d2.Text(), "host:") {
		t.Errorf("host line emitted without anomalies: %q", d2.Text())
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

func TestCollectHealthDigestPanFiledNotCountedInProgress(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	// Pan-filed meta beads sit in_progress forever (no worker ever claims
	// them); they must not inflate inprog — Pan infers "orphaned claims"
	// from inprog > workers and files another twin about the phantom.
	tw.mockBeads.beads["test-project-aaa"] = types.Bead{
		ID: "test-project-aaa", Project: "test-project", Status: types.StatusInProgress,
		Type: "task", Labels: []string{pan.FiledLabel}}
	tw.mockBeads.beads["test-project-bbb"] = types.Bead{
		ID: "test-project-bbb", Project: "test-project", Status: types.StatusInProgress, Type: "implement"}

	d := o.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	if got := d.Projects[0].InProgress; got != 1 {
		t.Errorf("InProgress = %d, want 1 (pan-filed meta bead excluded)", got)
	}
}

func TestParkCampaignRepair(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	o.registryStore = st

	target := pan.CampaignAnomalyID("test-project")
	if err := o.parkCampaignRepair(target); err != nil {
		t.Fatalf("parkCampaignRepair: %v", err)
	}

	info, ok, err := st.ProjectParkInfo("test-project")
	if err != nil {
		t.Fatalf("ProjectParkInfo: %v", err)
	}
	if !ok {
		t.Fatal("expected project to be parked")
	}
	if !info.Parked {
		t.Errorf("Parked = false, want true")
	}
	if info.Reason == "" {
		t.Errorf("expected non-empty park reason")
	}

	// Idempotent: a second repair updates the timestamp but keeps it parked.
	firstAt := info.ParkedAt
	if err := o.parkCampaignRepair(target); err != nil {
		t.Fatalf("second parkCampaignRepair: %v", err)
	}
	info, ok, err = st.ProjectParkInfo("test-project")
	if err != nil {
		t.Fatalf("ProjectParkInfo after second park: %v", err)
	}
	if !ok || !info.Parked {
		t.Fatal("expected project to still be parked after idempotent repair")
	}
	if !info.ParkedAt.After(firstAt) && !info.ParkedAt.Equal(firstAt) {
		t.Errorf("parked_at should not regress: %v vs %v", info.ParkedAt, firstAt)
	}
}

func TestUnparkCampaignRepair(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	o.registryStore = st

	target := pan.CampaignAnomalyID("test-project")
	if err := o.parkCampaignRepair(target); err != nil {
		t.Fatalf("parkCampaignRepair: %v", err)
	}
	if err := o.unparkCampaignRepair(target); err != nil {
		t.Fatalf("unparkCampaignRepair: %v", err)
	}

	info, ok, err := st.ProjectParkInfo("test-project")
	if err != nil {
		t.Fatalf("ProjectParkInfo: %v", err)
	}
	if !ok {
		t.Fatal("expected park row to remain after unpark")
	}
	if info.Parked {
		t.Errorf("Parked = true after unpark, want false")
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

// TestCollectHealthDigestOrphanClaims guards the fms P0 anomaly (2026-10-01):
// in_progress beads with no live registry worker are orphaned claims that
// wedge the whole DAG ladder while looking silent — Pan must see them.
func TestCollectHealthDigestFreshHeartbeatCompletedRowNotOrphan(t *testing.T) {
	// Regression (2026-10-02 hydra-l867): a worker that is still running after
	// its registry row was prematurely marked completed heartbeats every 30s —
	// the bead has a live worker, not an orphan claim. The digest previously
	// listed only working/parked rows, so this state counted as orphaned and
	// Pan re-filed a twin P0 report bead every judgment round.
	tw := setupTestOverseer(t)
	tw.overseer.registryStore = nil

	bead := types.Bead{ID: "test-project-fin", Project: "test-project", Status: types.StatusInProgress, Type: "bug"}
	tw.mockBeads.beads["test-project-fin"] = bead

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	tw.overseer.registryStore = st
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID: "test-project-fin", PID: 3, Project: "test-project", State: registry.StateCompleted,
		StartedAt: time.Now().Add(-30 * time.Minute), LastHeartbeat: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	d := tw.overseer.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	ph := d.Projects[0]
	if ph.OrphanClaims != 0 {
		t.Errorf("OrphanClaims = %d, want 0 (heartbeat-fresh completed row is live)", ph.OrphanClaims)
	}
	for _, a := range ph.Anomalies {
		if strings.Contains(a, "P0 campaign anomaly") {
			t.Errorf("unexpected orphan anomaly: %v", ph.Anomalies)
		}
	}
}

func TestCollectHealthDigestEscalatedBeadNotOrphan(t *testing.T) {
	// Regression (2026-10-02 07:5x): human-escalated beads (verified-close
	// rejected 3/3 → escalated, left in_progress by design) never get a
	// worker, so the digest counted them as orphaned claims and re-fired the
	// P0 anomaly for exactly the beads the escalation gate already owns
	// (bxzr/1h4v/wiu6/cxpf — tdd-validation-failed escalations, workers had
	// completed normally). Escalated beads are excluded; escalation_ttl owns
	// the aging.
	tw := setupTestOverseer(t)
	tw.overseer.registryStore = nil

	bead := types.Bead{ID: "test-project-esc", Project: "test-project", Status: types.StatusInProgress, Type: "bug", UpdatedAt: time.Now().Add(-10 * time.Minute), Labels: []string{"escalated", "needs-human"}}
	tw.mockBeads.beads["test-project-esc"] = bead

	d := tw.overseer.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	ph := d.Projects[0]
	if ph.OrphanClaims != 0 {
		t.Errorf("OrphanClaims = %d, want 0 (escalated bead is human-gated, not orphaned)", ph.OrphanClaims)
	}
	for _, a := range ph.Anomalies {
		if strings.Contains(a, "P0 campaign anomaly") {
			t.Errorf("unexpected orphan anomaly: %v", ph.Anomalies)
		}
	}
}

// TestCollectHealthDigestCloseInProgressNotOrphan guards the hydra
// 2026-10-06 persisting orphanclaims=6 anomaly: beads whose close gates were
// mid-run (workers exited 0 minutes earlier, Charon correctly left them via
// the close-in-progress probe) were still counted as orphaned claims because
// the digest lacked the gate-artifact probe — every judgment round re-filed
// a P0 campaign-anomaly twin. A bead with a live running gate artifact must
// NOT count as an orphaned claim.
func TestCollectHealthDigestCloseInProgressNotOrphan(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	o.registryStore = nil

	bead := types.Bead{ID: "test-project-midclose", Project: "test-project", Status: types.StatusInProgress, Type: "bug", UpdatedAt: time.Now().Add(-10 * time.Minute)}
	tw.mockBeads.beads["test-project-midclose"] = bead

	root := t.TempDir()
	o.gateRoot = root
	a := GateArtifact{Project: "test-project", BeadID: "test-project-midclose", Gate: "test", Status: GateStatusRunning, PID: os.Getpid(), StartedAt: time.Now()}
	if err := WriteGateArtifact(root, a); err != nil {
		t.Fatalf("WriteGateArtifact: %v", err)
	}

	d := o.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	ph := d.Projects[0]
	if ph.OrphanClaims != 0 {
		t.Errorf("OrphanClaims = %d, want 0 (bead is mid-close, not orphaned)", ph.OrphanClaims)
	}
	for _, an := range ph.Anomalies {
		if strings.Contains(an, "P0 campaign anomaly") {
			t.Errorf("unexpected orphan anomaly: %v", ph.Anomalies)
		}
	}

	// Without the gate root the same bead still counts (probe disabled).
	o.gateRoot = ""
	d = o.collectHealthDigest()
	if d.Projects[0].OrphanClaims != 1 {
		t.Errorf("OrphanClaims = %d with probe disabled, want 1", d.Projects[0].OrphanClaims)
	}
}

func TestCollectHealthDigestOrphanClaims(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer

	orphan := types.Bead{ID: "test-project-orph", Project: "test-project", Status: types.StatusInProgress, Type: "bug", UpdatedAt: time.Now().Add(-10 * time.Minute)}
	worked := types.Bead{ID: "test-project-live", Project: "test-project", Status: types.StatusInProgress, Type: "bug", UpdatedAt: time.Now().Add(-10 * time.Minute)}
	tw.mockBeads.beads["test-project-orph"] = orphan
	tw.mockBeads.beads["test-project-live"] = worked

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	o.registryStore = st
	if err := st.UpsertWorker(registry.WorkerRecord{
		BeadID: "test-project-live", PID: 2, Project: "test-project", State: registry.StateWorking,
		StartedAt: time.Now(), LastHeartbeat: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	d := o.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	ph := d.Projects[0]
	if ph.OrphanClaims != 1 {
		t.Errorf("OrphanClaims = %d, want 1", ph.OrphanClaims)
	}
	found := false
	for _, a := range ph.Anomalies {
		if strings.Contains(a, "P0 campaign anomaly") && strings.Contains(a, "no live registry worker") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected P0 orphan-claim anomaly, got %v", ph.Anomalies)
	}
	if !strings.Contains(d.Text(), "orphanclaims=1") {
		t.Errorf("digest text missing orphanclaims field: %q", d.Text())
	}
}

// TestCollectHealthDigestOrphanClaims_FreshClaimGrace guards the hydra
// 2026-10-02 filing loop: a bead claimed seconds ago has no registry worker
// yet (spawn → status update → registry upsert is not atomic), so the digest
// counted it orphaned and Pan filed a campaign-anomaly bead for it — which
// itself became a new fresh claim, re-firing the anomaly (4 false filings in
// 13 min). A claim within the grace window must NOT count as orphaned.
func TestCollectHealthDigestOrphanClaims_FreshClaimGrace(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer

	fresh := types.Bead{ID: "test-project-fresh", Project: "test-project", Status: types.StatusInProgress, Type: "bug", UpdatedAt: time.Now()}
	old := types.Bead{ID: "test-project-old", Project: "test-project", Status: types.StatusInProgress, Type: "bug", UpdatedAt: time.Now().Add(-10 * time.Minute)}
	tw.mockBeads.beads["test-project-fresh"] = fresh
	tw.mockBeads.beads["test-project-old"] = old

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	o.registryStore = st

	d := o.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	ph := d.Projects[0]
	if ph.OrphanClaims != 1 {
		t.Errorf("OrphanClaims = %d, want 1 (fresh claim must be skipped, stale must count)", ph.OrphanClaims)
	}
	for _, a := range ph.Anomalies {
		if strings.Contains(a, "1 in_progress") {
			continue
		}
	}
	found := false
	for _, a := range ph.Anomalies {
		if strings.Contains(a, "P0 campaign anomaly") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected P0 orphan-claim anomaly for the stale claim, got %v", ph.Anomalies)
	}
}
