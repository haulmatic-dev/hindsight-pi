package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// readSensorEvents reads all events from a test sink's central dir.
func readSensorEvents(t *testing.T, central string) []Event {
	t.Helper()
	entries, _ := os.ReadDir(central)
	var out []Event
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			evs, err := ReadEvents(filepath.Join(central, e.Name()))
			if err != nil {
				t.Fatalf("read events: %v", err)
			}
			out = append(out, evs...)
		}
	}
	return out
}

func sensorCount(evs []Event, typ string) int {
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// TestSensorSymmetry_VerifiedClose fires a rejection via rejectClose and
// asserts verified_close_rejected (fire) with a correlation ID; a pass-path
// emission of verified_close_passed (resolve) is verified via the same
// correlation ID.
func TestSensorSymmetry_VerifiedClose(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	central := t.TempDir()
	closer.SetEventSink(NewEventSink(central))

	// Fire: reject once (round 1) — need >1 bead state; rejectClose needs
	// bead non-terminal in the mock store.
	mock := newMockBeadsClient()
	mock.addBead(*bead)
	closer.beadsClient = mock
	err := closer.rejectClose(bead, repo, "", "tdd-validation-failed")
	if err == nil {
		t.Fatal("expected rejection error")
	}
	evs := readSensorEvents(t, central)
	if sensorCount(evs, "verified_close_rejected") != 1 {
		t.Fatalf("want 1 verified_close_rejected fire, got %d (%v)", sensorCount(evs, "verified_close_rejected"), evs)
	}
	corr := SensorCorrelation("verified-close", bead.ID.String())
	found := false
	for _, e := range evs {
		if e.Type == "verified_close_rejected" && e.Fields["correlation_id"] == corr {
			found = true
		}
	}
	if !found {
		t.Fatalf("fire missing correlation_id %q", corr)
	}

	// Resolve: emit the pass path (mirrors closeBead's success emission).
	closer.events.EmitRepo(repo, Event{Type: "verified_close_passed",
		Bead: bead.ID.String(), Reason: "completed",
		Fields: map[string]any{"correlation_id": corr}})
	evs = readSensorEvents(t, central)
	if sensorCount(evs, "verified_close_passed") != 1 {
		t.Fatalf("want 1 verified_close_passed resolve")
	}
}

// TestSensorCorrelation deterministic pairing ID.
func TestSensorCorrelation(t *testing.T) {
	if got := SensorCorrelation("aimd", "governor"); got != "aimd:governor" {
		t.Fatalf("want aimd:governor, got %q", got)
	}
}

// TestSensorSymmetry_WorktreeReconcileQuarantine fires the quarantine event.
func TestSensorSymmetry_WorktreeReconcileQuarantine(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	projectPath := gitInitRepo(t)
	m := NewWorktreeManager()
	key := "symq"
	wt, err := m.EnsureWorktree(projectPath, key)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "shared.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, projectPath, "add", "shared.txt")
	runGit(t, projectPath, "commit", "-m", "main shared")
	if err := os.WriteFile(filepath.Join(wt, "shared.txt"), []byte("wt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(wt, "bead(symq): conflict"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	backdateWorktree(t, m, projectPath, key)
	central := t.TempDir()
	sink := NewEventSink(central)
	integrated, conflicts := ReconcileOrphanedWorktrees(m, projectPath, nil, func(string) bool { return true }, sink)
	if integrated != 0 || conflicts != 1 {
		t.Fatalf("want 0 integrated / 1 conflict, got %d/%d", integrated, conflicts)
	}
	evs := readSensorEvents(t, central)
	if sensorCount(evs, "worktree_quarantined") != 1 {
		t.Fatalf("want 1 worktree_quarantined fire, got %d", sensorCount(evs, "worktree_quarantined"))
	}
	for _, e := range evs {
		if e.Type == "worktree_quarantined" && e.Fields["correlation_id"] != SensorCorrelation("worktree-reconcile", "symq") {
			t.Fatalf("quarantine fire correlation mismatch: %v", e.Fields)
		}
	}
}

// TestSensorSymmetry_QuarantineRestored — the retry-quarantine resolve pairs
// with the reconciler fire's correlation ID.
func TestSensorSymmetry_QuarantineRestored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := setupQuarantineRepo(t, "symr", false)
	central := t.TempDir()
	sink := NewEventSink(central)
	mock := newMockBeadsClient()
	id, _ := types.ParseBeadID("symr")
	mock.addBead(types.Bead{ID: id, Status: types.StatusOpen})
	if err := quarantineReattempt(repo, "symr", mock, sink, gitOut, defaultMergeTree, time.Now); err != nil {
		t.Fatalf("clean quarantine should requeue: %v", err)
	}
	evs := readSensorEvents(t, central)
	if sensorCount(evs, "quarantine_restored") != 1 {
		t.Fatalf("want 1 quarantine_restored resolve")
	}
	for _, e := range evs {
		if e.Type == "quarantine_restored" && e.Fields["correlation_id"] != SensorCorrelation("worktree-reconcile", "symr") {
			t.Fatalf("resolve correlation mismatch: %v", e.Fields)
		}
	}
}

// TestSensorSymmetry_AIMD — decrease fires aimd_scale_down, increase fires
// aimd_scale_up; both share the governor correlation ID.
func TestSensorSymmetry_AIMD(t *testing.T) {
	central := t.TempDir()
	g := NewConcurrencyGovernor(4, GovernorConfig{WindowSize: 2, CooldownSeconds: 0, RecoveryIntervalSeconds: 0})
	sink := NewEventSink(central)
	g.SetEventSink(sink)

	now := time.Unix(1000, 0)
	g.SetNow(func() time.Time { return now })
	g.RecordOutcome(false) // provider pressure stamps lastFailure
	now = now.Add(time.Second)
	g.SetNow(func() time.Time { return now })
	g.RecordOutcome(false) // full failure window -> additive decrease
	if g.Limit() != 3 {
		t.Fatalf("want limit 3 after decrease, got %d", g.Limit())
	}
	evs := readSensorEvents(t, central)
	if sensorCount(evs, "aimd_scale_down") != 1 {
		t.Fatalf("want 1 aimd_scale_down fire")
	}

	// Quiet recovery -> scale up (resolve of the scale-down fire).
	now = now.Add(2 * time.Hour)
	g.SetNow(func() time.Time { return now })
	if !g.MaybeRecover() || g.Limit() != 4 {
		t.Fatalf("want recovery to 4, got %d", g.Limit())
	}
	evs = readSensorEvents(t, central)
	if sensorCount(evs, "aimd_scale_up") != 1 {
		t.Fatalf("want 1 aimd_scale_up resolve")
	}
	for _, e := range evs {
		if (e.Type == "aimd_scale_down" || e.Type == "aimd_scale_up") && e.Fields["correlation_id"] != SensorCorrelation("aimd", "governor") {
			t.Fatalf("correlation mismatch: %v", e.Fields)
		}
	}
}

// TestSensorSymmetry_OrphanRetry: reopen fires charon_orphan_reopened with a
// correlation ID and registers the bead in orphanWatch; a later sweep over a
// terminal bead emits orphan_recovered with the same correlation ID.
func TestSensorSymmetry_OrphanRetry(t *testing.T) {
	beadID := "test-orphan-sym"
	charon, exec := setupOrphanCharon(t, beadListJSON(beadID, time.Now().Add(-10*time.Minute)), 5*time.Minute)
	central := t.TempDir()
	charon.SetEventSink(NewEventSink(central))

	if n := charon.recoverOrphanInProgressBeads(); n != 1 {
		t.Fatalf("want 1 recovered, got %d", n)
	}
	charon.orphanWatchMu.Lock()
	watched := len(charon.orphanWatch)
	charon.orphanWatchMu.Unlock()
	if watched != 1 {
		t.Fatalf("want 1 watched bead after fire, got %d", watched)
	}
	evs := readSensorEvents(t, central)
	if sensorCount(evs, "charon_orphan_reopened") != 1 {
		t.Fatalf("want 1 orphan fire")
	}

	// Bead reaches terminal status: resolve fires, watch entry cleared.
	exec.getResp = terminalBeadJSON(beadID, "completed")
	if n := charon.recoverOrphanInProgressBeads(); n != 0 {
		t.Fatalf("terminal bead must not reopen, got %d", n)
	}
	evs = readSensorEvents(t, central)
	if sensorCount(evs, "orphan_recovered") != 1 {
		t.Fatalf("want 1 orphan_recovered resolve, events: %v", evs)
	}
	corr := SensorCorrelation("orphan-retry", beadID)
	seenFire, seenResolve := false, false
	for _, e := range evs {
		if e.Fields["correlation_id"] == corr {
			if e.Type == "charon_orphan_reopened" {
				seenFire = true
			}
			if e.Type == "orphan_recovered" {
				seenResolve = true
			}
		}
	}
	if !seenFire || !seenResolve {
		t.Fatalf("fire=%v resolve=%v under correlation %q", seenFire, seenResolve, corr)
	}
	charon.orphanWatchMu.Lock()
	watched = len(charon.orphanWatch)
	charon.orphanWatchMu.Unlock()
	if watched != 0 {
		t.Fatalf("watch entry must be cleared after resolve, got %d", watched)
	}
}

// TestSensorSymmetry_PublishRetry: conflict → rebase → success emits
// publish_rebase_attempted fires and one publish_succeeded resolve sharing
// the publish-retry correlation ID; exhaustion emits the fire without resolve.
func TestSensorSymmetry_PublishRetry(t *testing.T) {
	central := t.TempDir()
	SetPublishEventSink(NewEventSink(central))
	t.Cleanup(func() { SetPublishEventSink(nil) })

	calls := 0
	publish := func() (string, error) {
		calls++
		if calls == 1 {
			return "", fmt.Errorf("gh pr create: not mergeable: conflicts between branches")
		}
		return "https://example.com/pr/1", nil
	}
	url, err := publishWithRebaseRetry(publish, func() error { return nil }, t.TempDir(), "main")
	if err != nil || url == "" {
		t.Fatalf("want success, got url=%q err=%v", url, err)
	}
	evs := readSensorEvents(t, central)
	if sensorCount(evs, "publish_rebase_attempted") != 1 || sensorCount(evs, "publish_succeeded") != 1 {
		t.Fatalf("want 1 attempted fire + 1 succeeded resolve, got events %v", evs)
	}

	// Exhaustion path: no resolve.
	central2 := t.TempDir()
	SetPublishEventSink(NewEventSink(central2))
	publish2 := func() (string, error) {
		return "", fmt.Errorf("gh pr create: not mergeable: conflicts between branches")
	}
	_, err = publishWithRebaseRetry(publish2, func() error { return nil }, t.TempDir(), "main")
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	evs2 := readSensorEvents(t, central2)
	if sensorCount(evs2, "publish_succeeded") != 0 {
		t.Fatalf("exhaustion must not emit publish_succeeded")
	}
	if sensorCount(evs2, "publish_rebase_attempted") != 2 || sensorCount(evs2, "publish_rebase_exhausted") != 1 {
		t.Fatalf("want 2 fires + 1 exhausted, got %v", evs2)
	}
}

// TestSensorSymmetry_PanRepair wraps handlers: success → repair_succeeded,
// failure → repair_failed, shared correlation, nil sink inert.
func TestSensorSymmetry_PanRepair(t *testing.T) {
	central := t.TempDir()
	sink := NewEventSink(central)
	base := map[string]func(string) error{
		"repair-worktree": func(beadID string) error { return nil },
		"clear-hard-park": func(beadID string) error { return fmt.Errorf("refused: no label") },
	}
	wrapped := wrapRepairHandlers(base, sink)
	if err := wrapped["repair-worktree"]("b-ok"); err != nil {
		t.Fatalf("success passthrough broken: %v", err)
	}
	if err := wrapped["clear-hard-park"]("b-bad"); err == nil {
		t.Fatal("error passthrough broken")
	}
	evs := readSensorEvents(t, central)
	if sensorCount(evs, "repair_succeeded") != 1 || sensorCount(evs, "repair_failed") != 1 {
		t.Fatalf("want 1+1 repair events, got %v", evs)
	}
	for _, e := range evs {
		if e.Fields["correlation_id"] == "" {
			t.Fatalf("repair event missing correlation_id: %v", e.Fields)
		}
	}
	// Nil sink: original behavior, no panic.
	nilWrapped := wrapRepairHandlers(base, nil)
	if err := nilWrapped["repair-worktree"]("b"); err != nil {
		t.Fatal("nil sink must not alter behavior")
	}
}
