package core

import (
	"os"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED-phase tests for the stale-filing sweep: pan-filed wedge filings whose
// premise is a blocked-cohort claim must be re-verified regardless of
// open/in_progress status and closed when the premise is demonstrably
// cleared. GREEN implements sweepStalePanFilings in internal/core.
//
// Contract pinned here (all deterministic via package-level probe hooks):
//   - Probes (all three must hold):
//     (a) filing older than 48h,
//     (b) the blocked cohort the filing names has zero members still blocked
//     with that reason today,
//     (c) the project recorded at least one successful spawn in the last 24h.
//   - All three true → filing closes with reason exactly
//     "stale pan filing: premise cleared (cohort empty, spawns healthy)".
//   - Status: open AND in_progress filings are both evaluated.
//   - Guardrail: reason text matching
//     (branch protection|required approval|permissions|GH013|rules URL) is
//     NEVER auto-closed — left for human triage (pan.go campaign-anomaly
//     contract).
//   - Guardrail: at most one auto-close per project per sweep tick, with a
//     typed event "pan_filing_stale_closed" carrying
//     {bead, project, cohort_named, cohort_now, last_spawn_ok}.

func staleFilingFixture(t *testing.T, id, project, reason string, status types.BeadStatus, createdAt time.Time) types.Bead {
	t.Helper()
	return types.Bead{
		ID:          types.BeadID(id),
		Title:       "Campaign anomaly: integration wedge",
		Description: reason,
		Project:     project,
		Status:      status,
		Labels:      []string{pan.FiledLabel},
		CreatedAt:   createdAt,
	}
}

func staleFilingEnvironment(t *testing.T, path string, now time.Time) (*Overseer, *mockBeadsClient) {
	t.Helper()
	o := &Overseer{
		beadsClient: newMockBeadsClient(),
		projects:    []types.Project{{Name: "fakeproj", Path: path, Type: types.RepoTypeSingle}},
	}
	return o, o.beadsClient.(*mockBeadsClient)
}

// staleFilingRequireImpl skips loudly (or fails under STALE_FILING_ENFORCE=1)
// while the GREEN implementation is absent, mirroring the BEAD_CLAIMS RED gate.
func staleFilingRequireImpl(t *testing.T) {
	t.Helper()
	if !staleFilingSweepImplemented && os.Getenv("STALE_FILING_ENFORCE") == "" {
		t.Skip("RED: stale-filing sweep contract not yet implemented (set STALE_FILING_ENFORCE=1 to fail)")
	}
}

func staleFilingEligibleHooks() (restore func()) {
	oldIsOld, oldCohort, oldBlocked, oldSpawn := staleFilingIsOld, staleFilingCohortNamed, staleFilingCohortBlocked, staleFilingLastSpawnOK
	staleFilingIsOld = func(b types.Bead, now time.Time) bool {
		return now.Sub(b.CreatedAt) > staleFilingMinAge
	}
	staleFilingCohortNamed = func(b types.Bead) string { return "publish-wedge" }
	staleFilingCohortBlocked = func(projectPath, cohort string) bool { return false }
	staleFilingLastSpawnOK = func(projectPath string, now time.Time) bool { return true }
	return func() {
		staleFilingIsOld, staleFilingCohortNamed, staleFilingCohortBlocked, staleFilingLastSpawnOK = oldIsOld, oldCohort, oldBlocked, oldSpawn
	}
}

// AC: eligible in_progress filing must be closed with the exact cleared
// reason; a second eligible OPEN filing (next tick) must also close —
// proving the sweep covers open/in_progress alike.
func TestStaleFilingSweep_ClosesEligibleInProgressAndOpenFilings(t *testing.T) {
	staleFilingRequireImpl(t)
	defer staleFilingEligibleHooks()()
	now := time.Now()
	path := "/tmp/fakeproj-stale-ok"
	o, mock := staleFilingEnvironment(t, path, now)
	progress := staleFilingFixture(t, "proj-stale1", "fakeproj-stale-ok",
		"Publish blocked: 3 beads blocked on publish-wedge", types.StatusInProgress, now.Add(-72*time.Hour))
	open := staleFilingFixture(t, "proj-stale2", "fakeproj-stale-ok",
		"Publish blocked: 2 beads blocked on publish-wedge", types.StatusOpen, now.Add(-72*time.Hour))
	mock.beads["proj-stale1"] = progress
	mock.beads["proj-stale2"] = open

	o.sweepStalePanFilings(now)
	calls := mock.getCloseCalls()
	if len(calls) != 1 {
		t.Fatalf("first tick: exactly one auto-close (rate limit), got %d: %+v", len(calls), calls)
	}
	if calls[0].beadID != "proj-stale1" || calls[0].reason != staleFilingReasonCleared {
		t.Fatalf("in_progress filing close call mismatch: %+v", calls[0])
	}

	// Second tick: rate limit resets, the open filing is now evaluated too.
	o.sweepStalePanFilings(now.Add(time.Minute))
	calls = mock.getCloseCalls()
	if len(calls) != 2 {
		t.Fatalf("second tick must close the remaining open filing, got %d calls", len(calls))
	}
	if calls[1].beadID != "proj-stale2" || calls[1].reason != staleFilingReasonCleared {
		t.Fatalf("open filing close call mismatch: %+v", calls[1])
	}
}

// AC guardrail: a filing whose reason text names branch protection is never
// auto-closed — left for human triage.
func TestStaleFilingSweep_BranchProtectionReasonNeverClosed(t *testing.T) {
	defer staleFilingEligibleHooks()()
	now := time.Now()
	path := "/tmp/fakeproj-stale-bp"
	o, mock := staleFilingEnvironment(t, path, now)
	bead := staleFilingFixture(t, "proj-bp1", "fakeproj-stale-bp",
		"Push rejected: branch protection rules require approvals (GH013)", types.StatusInProgress, now.Add(-72*time.Hour))
	mock.beads["proj-bp1"] = bead

	o.sweepStalePanFilings(now)
	o.sweepStalePanFilings(now.Add(time.Minute))
	if calls := mock.getCloseCalls(); len(calls) != 0 {
		t.Fatalf("branch-protection filing must never auto-close, got %+v", calls)
	}
}

// AC probe (b): the cohort the filing names must have zero members still
// blocked with that reason today — a still-blocked cohort is not cleared.
func TestStaleFilingSweep_CohortStillBlockedNotClosed(t *testing.T) {
	defer staleFilingEligibleHooks()()
	staleFilingCohortBlocked = func(projectPath, cohort string) bool { return true }
	now := time.Now()
	path := "/tmp/fakeproj-stale-blocked"
	o, mock := staleFilingEnvironment(t, path, now)
	bead := staleFilingFixture(t, "proj-blocked1", "fakeproj-stale-blocked",
		"Publish blocked: 3 beads blocked on publish-wedge", types.StatusInProgress, now.Add(-72*time.Hour))
	mock.beads["proj-blocked1"] = bead

	o.sweepStalePanFilings(now)
	o.sweepStalePanFilings(now.Add(time.Minute))
	if calls := mock.getCloseCalls(); len(calls) != 0 {
		t.Fatalf("still-blocked cohort must keep the filing open, got %+v", calls)
	}
}

// AC probe (c): the project must have recorded a successful spawn within the
// last 24h — without healthy spawns the filing stays open.
func TestStaleFilingSweep_NoRecentSpawnSuccessNotClosed(t *testing.T) {
	defer staleFilingEligibleHooks()()
	staleFilingLastSpawnOK = func(projectPath string, now time.Time) bool { return false }
	now := time.Now()
	path := "/tmp/fakeproj-stale-nospawn"
	o, mock := staleFilingEnvironment(t, path, now)
	bead := staleFilingFixture(t, "proj-nospawn1", "fakeproj-stale-nospawn",
		"Publish blocked: 3 beads blocked on publish-wedge", types.StatusInProgress, now.Add(-72*time.Hour))
	mock.beads["proj-nospawn1"] = bead

	o.sweepStalePanFilings(now)
	o.sweepStalePanFilings(now.Add(time.Minute))
	if calls := mock.getCloseCalls(); len(calls) != 0 {
		t.Fatalf("no recent successful spawn must keep the filing open, got %+v", calls)
	}
}

// AC probe (a): a filing younger than 48h is not stale, whatever the cohort
// and spawn state.
func TestStaleFilingSweep_YoungFilingNotClosed(t *testing.T) {
	defer staleFilingEligibleHooks()()
	now := time.Now()
	path := "/tmp/fakeproj-stale-young"
	o, mock := staleFilingEnvironment(t, path, now)
	bead := staleFilingFixture(t, "proj-young1", "fakeproj-stale-young",
		"Publish blocked: 3 beads blocked on publish-wedge", types.StatusInProgress, now.Add(-1*time.Hour))
	mock.beads["proj-young1"] = bead

	o.sweepStalePanFilings(now)
	o.sweepStalePanFilings(now.Add(time.Minute))
	if calls := mock.getCloseCalls(); len(calls) != 0 {
		t.Fatalf("filing younger than 48h must stay open, got %+v", calls)
	}
}

// AC guardrail: rate limit + typed event shape. Two eligible filings in one
// project in one tick yield exactly one close and one
// pan_filing_stale_closed event carrying
// {bead, project, cohort_named, cohort_now, last_spawn_ok}.
func TestStaleFilingSweep_RateLimitAndTypedEvent(t *testing.T) {
	staleFilingRequireImpl(t)
	defer staleFilingEligibleHooks()()
	now := time.Now()
	path := "/tmp/fakeproj-stale-rate"
	o, mock := staleFilingEnvironment(t, path, now)
	for _, id := range []string{"proj-rate1", "proj-rate2"} {
		bead := staleFilingFixture(t, id, "fakeproj-stale-rate",
			"Publish blocked: 3 beads blocked on publish-wedge", types.StatusInProgress, now.Add(-72*time.Hour))
		mock.beads[id] = bead
	}
	sinkDir := t.TempDir()
	o.eventSink = NewEventSink(sinkDir)

	o.sweepStalePanFilings(now)
	calls := mock.getCloseCalls()
	if len(calls) != 1 {
		t.Fatalf("one auto-close per project per tick, got %d", len(calls))
	}
	evs := readEventsOfType(t, o.eventSink, "pan_filing_stale_closed")
	if len(evs) != 1 {
		t.Fatalf("expected exactly one pan_filing_stale_closed event, got %d: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Bead != string(calls[0].beadID) {
		t.Errorf("event bead must be the closed filing, got %s", ev.Bead)
	}
	if ev.Project == "" {
		t.Errorf("event must carry the project name")
	}
	for _, key := range []string{"cohort_named", "cohort_now", "last_spawn_ok"} {
		if _, ok := ev.Fields[key]; !ok {
			t.Errorf("event fields must carry %s, got %v", key, ev.Fields)
		}
	}
}
