package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/mirroraudit"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED gate mirroring staleFilingRequireImpl: while the audit-sweep GREEN is
// absent these tests skip loudly; MIRROR_HEALTH_ENFORCE=1 fails them.
func mirrorHealthRequireImpl(t *testing.T) {
	t.Helper()
	if !mirrorResyncImplemented && os.Getenv("MIRROR_HEALTH_ENFORCE") == "" {
		t.Skip("RED: mirror-health audit sweep contract not yet implemented (set MIRROR_HEALTH_ENFORCE=1 to fail)")
	}
}

// RED gate for the FR-5 docs/SLO contract tests in mirror_health_docs_test.go:
// the SLO script / shepherd.md knobs / AGENTS.md operating table / runbook
// import-only line are not yet written, so these tests skip loudly until
// GREEN lands them (MIRROR_HEALTH_DOCS_ENFORCE=1 fails them).
func mirrorHealthDocsRequireImpl(t *testing.T) {
	t.Helper()
	if os.Getenv("MIRROR_HEALTH_DOCS_ENFORCE") == "" {
		t.Skip("RED: mirror-health SLO/docs contract not yet implemented (set MIRROR_HEALTH_DOCS_ENFORCE=1 to fail)")
	}
}

// mirrorHooks is the test seam: swap the package-level probe hooks wholesale
// and restore them at test end.
var mirrorHooks struct {
	jsonlMtime  func(storePath string) (time.Time, bool)
	liveWorker  func(projectName string) bool
	auditRunner func(storePath string) (mirroraudit.Report, error)
	writeProbe  func(storePath string) error
}

func mirrorHookRestore() (restore func()) {
	oldmtime, oldlive, oldrunner := mirrorJSONLMtime, mirrorHasLiveWorker, mirrorAuditRunner
	oldprobe := mirrorWriteProbe
	mirrorJSONLMtime = mirrorHooks.jsonlMtime
	mirrorHasLiveWorker = mirrorHooks.liveWorker
	mirrorAuditRunner = mirrorHooks.auditRunner
	mirrorWriteProbe = mirrorHooks.writeProbe
	return func() {
		mirrorJSONLMtime, mirrorHasLiveWorker, mirrorAuditRunner = oldmtime, oldlive, oldrunner
		mirrorWriteProbe = oldprobe
	}
}

// mirrorEnv builds a test Overseer around one project and wires the probe
// hooks. The caller can mutate auditReport to drive sweep outcomes.
func mirrorEnv(t *testing.T, projectPath string, auditReport mirroraudit.Report, auditErr error) *Overseer {
	t.Helper()
	storePath := filepath.Join(projectPath, ".beads")
	mirrorHooks.jsonlMtime = func(p string) (time.Time, bool) {
		if p != storePath {
			return time.Time{}, false
		}
		return time.Now().Add(-10 * time.Minute), true
	}
	mirrorHooks.liveWorker = func(string) bool { return false }
	mirrorHooks.auditRunner = func(p string) (mirroraudit.Report, error) {
		if p != storePath {
			return mirroraudit.Report{}, errUnavailable
		}
		return auditReport, auditErr
	}
	o := &Overseer{
		beadsClient: newMockBeadsClient(),
		projects:    []types.Project{{Name: "fakeproj", Path: projectPath, Type: types.RepoTypeSingle}},
		config: &Config{
			Overseer: OverseerConfig{
				MirrorHealth: MirrorHealthConfig{
					Enabled:  true,
					Interval: defaultMirrorHealthInterval,
				},
			},
		},
		eventSink: NewEventSink(t.TempDir()),
	}
	t.Cleanup(mirrorHookRestore())
	return o
}

// AC: config knobs `mirror_health.enabled` + `mirror_health.audit_interval`
// parse from yaml into typed fields on OverseerConfig.MirrorHealth.
func TestMirrorHealthConfig_KnobsParse(t *testing.T) {
	yaml := `
overseer:
  mirror_health:
    enabled: true
    audit_interval: 45m
projects: []
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Overseer.MirrorHealth.Enabled {
		t.Fatal("mirror_health.enabled must parse true")
	}
	if cfg.Overseer.MirrorHealth.Interval != 45*time.Minute {
		t.Fatalf("mirror_health.audit_interval must parse 45m, got %v", cfg.Overseer.MirrorHealth.Interval)
	}
}

// AC: missing knobs fall back to defaults (enabled=true per the
// audit-only-by-default PRD contract, interval=30m).
func TestMirrorHealthConfig_Defaults(t *testing.T) {
	yaml := `
projects: []
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Overseer.MirrorHealth.Enabled {
		t.Fatal("default mirror_health.enabled must be true (audit-only by default)")
	}
	if cfg.Overseer.MirrorHealth.Interval != defaultMirrorHealthInterval {
		t.Fatalf("default mirror_health.audit_interval must be %v, got %v", defaultMirrorHealthInterval, cfg.Overseer.MirrorHealth.Interval)
	}
}

// AC: audit_interval: 0 disables the sweep (interval gate returns false).
func TestMirrorHealthConfig_ZeroIntervalDisables(t *testing.T) {
	o := &Overseer{
		config: &Config{
			Overseer: OverseerConfig{
				MirrorHealth: MirrorHealthConfig{Enabled: true, Interval: 0},
			},
		},
	}
	if mirrorHealthSweepDue(o, time.Now()) {
		t.Fatal("zero interval must disable the sweep")
	}
}

// AC: interval firing — sweep must not run before audit_interval elapses,
// and must run once after (mirrors the lastStaleFilingSweep throttle).
func TestMirrorHealthSweep_IntervalFire(t *testing.T) {
	mirrorHealthRequireImpl(t)
	o := mirrorEnv(t, t.TempDir(), mirroraudit.Report{}, nil)
	now := time.Now()

	o.lastMirrorHealthSweep = now.Add(-10 * time.Minute)
	if mirrorHealthSweepDue(o, now) {
		t.Fatal("sweep must not be due before audit_interval elapses")
	}

	o.lastMirrorHealthSweep = now.Add(-31 * time.Minute)
	if !mirrorHealthSweepDue(o, now) {
		t.Fatal("sweep must be due once audit_interval has elapsed")
	}

	// Disabled knob: no firing.
	o.config.Overseer.MirrorHealth.Enabled = false
	if mirrorHealthSweepDue(o, now) {
		t.Fatal("sweep must not fire when mirror_health.enabled is false")
	}
	o.config.Overseer.MirrorHealth.Enabled = true
	o.lastMirrorHealthSweep = time.Time{}
	if !mirrorHealthSweepDue(o, now) {
		t.Fatal("zero lastMirrorHealthSweep (first tick) must fire the sweep")
	}
}

// AC: positive control — a store with an old-enough jsonl and no live worker
// IS audited: one mirror_audit_clean event. This is the counter-piece that
// makes the two skip tests below non-vacuous (they fail under ENFORCE when
// the sweep body is inert and never audits anything).
func TestMirrorHealthSweep_AuditsEligibleStore(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	o := mirrorEnv(t, path, mirroraudit.Report{}, nil)

	sink := NewEventSink(t.TempDir())
	o.eventSink = sink

	o.sweepMirrorHealth(time.Now())

	if evs := readEventsOfType(t, sink, mirrorAuditCleanEvent); len(evs) != 1 {
		t.Fatalf("eligible store must emit exactly one mirror_audit_clean, got %d", len(evs))
	}
}

// AC: mtime quiescence skip — a store whose issues.jsonl was written within
// mirrorQuiescenceWindow is skipped: zero audit invocations, zero events.
func TestMirrorHealthSweep_MtimeQuiescenceSkip(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	o := mirrorEnv(t, path, mirroraudit.Report{}, nil)
	// Hook returns a FRESH mtime (within the 2m quiescence window).
	mirrorHooks.jsonlMtime = func(string) (time.Time, bool) { return time.Now().Add(-30 * time.Second), true }

	sink := NewEventSink(t.TempDir())
	o.eventSink = sink

	o.sweepMirrorHealth(time.Now())

	if len(readEventsOfType(t, sink, mirrorDriftDetectedEvent)) != 0 {
		t.Fatal("quiescent store (fresh jsonl) must not emit mirror_drift_detected")
	}
	if len(readEventsOfType(t, sink, mirrorAuditCleanEvent)) != 0 {
		t.Fatal("quiescent store (fresh jsonl) must not emit mirror_audit_clean")
	}
}

// AC: live-worker skip — a project with a live worker is skipped: zero audit
// invocations, zero events.
func TestMirrorHealthSweep_LiveWorkerSkip(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	o := mirrorEnv(t, path, mirroraudit.Report{}, nil)
	mirrorHooks.liveWorker = func(string) bool { return true }

	sink := NewEventSink(t.TempDir())
	o.eventSink = sink

	o.sweepMirrorHealth(time.Now())

	if len(readEventsOfType(t, sink, mirrorDriftDetectedEvent)) != 0 {
		t.Fatal("live-worker project must not emit mirror_drift_detected")
	}
	if len(readEventsOfType(t, sink, mirrorAuditCleanEvent)) != 0 {
		t.Fatal("live-worker project must not emit mirror_audit_clean")
	}
}

// AC: fail-open — an audit error emits mirror_audit_error and never panics;
// the sweep continues to the next store.
func TestMirrorHealthSweep_AuditErrorFailOpen(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	o := mirrorEnv(t, path, mirroraudit.Report{}, errUnavailable)

	sink := NewEventSink(t.TempDir())
	o.eventSink = sink

	o.sweepMirrorHealth(time.Now())

	if evs := readEventsOfType(t, sink, mirrorAuditErrorEvent); len(evs) != 1 {
		t.Fatalf("audit error must emit exactly one mirror_audit_error, got %d", len(evs))
	}
}

// AC: typed events — drift records emit mirror_drift_detected with
// store/bead/statuses/class fields; clean stores emit exactly one
// mirror_audit_clean carrying the checked count.
func TestMirrorHealthSweep_TypedEvents(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	o := mirrorEnv(t, path, mirroraudit.Report{}, nil)

	// Drift store: one divergent record.
	o.eventSink = NewEventSink(t.TempDir())
	o.sweepMirrorHealthForProject(o.projects[0], time.Now())
	if evs := readEventsOfType(t, o.eventSink, mirrorDriftDetectedEvent); len(evs) != 1 {
		t.Fatalf("drift store must emit exactly one mirror_drift_detected per divergent record, got %d", len(evs))
	}
	if evs := readEventsOfType(t, o.eventSink, mirrorAuditCleanEvent); len(evs) != 0 {
		t.Fatalf("drift store must not emit mirror_audit_clean, got %d", len(evs))
	}

	// Clean store: classification is in the shared package — the sweep
	// emits one mirror_audit_clean per store with no divergences.
	mirrorHooks.auditRunner = func(string) (mirroraudit.Report, error) {
		return mirroraudit.Report{}, nil
	}
	o.eventSink = NewEventSink(t.TempDir())
	o.sweepMirrorHealthForProject(o.projects[0], time.Now())
	if evs := readEventsOfType(t, o.eventSink, mirrorAuditCleanEvent); len(evs) != 1 {
		t.Fatalf("clean store must emit exactly one mirror_audit_clean, got %d", len(evs))
	}
	if evs := readEventsOfType(t, o.eventSink, mirrorDriftDetectedEvent); len(evs) != 0 {
		t.Fatalf("clean store must not emit mirror_drift_detected, got %d", len(evs))
	}
}

// FR-4 AC (fixture): a user-owned divergent store files EXACTLY ONE
// mirror_drift_needs_human Pan event — repeated sweeps never duplicate the
// filing (signature dedupe exactly-once contract). RED-gated: GREEN wires
// the filing sink via filePanBead.
func TestMirrorHealthSweep_UserOwnedDivergentStoreEmitsExactlyOne(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	driftReport := mirroraudit.Report{
		StorePath: filepath.Join(path, ".beads"),
		Checked:   1,
		Records: []mirroraudit.Record{
			{BeadID: "bead-drift-1", DBStatus: "open", Class: mirroraudit.ClassDBAhead},
		},
		Counts: mirroraudit.Counts{DBAhead: 1},
	}
	o := mirrorEnv(t, path, driftReport, nil)

	// First sweep: exactly one filing for the user-owned divergent record.
	o.sweepMirrorHealthForProject(o.projects[0], time.Now())
	if evs := readEventsOfType(t, o.eventSink, mirrorDriftNeedsHumanEvent); len(evs) != 1 {
		t.Fatalf("user-owned divergent store must emit exactly one %s, got %d", mirrorDriftNeedsHumanEvent, len(evs))
	}

	// Second sweep: repeat must NOT file a second one (exactly-once contract).
	o.sweepMirrorHealthForProject(o.projects[0], time.Now())
	if evs := readEventsOfType(t, o.eventSink, mirrorDriftNeedsHumanEvent); len(evs) != 1 {
		t.Fatalf("repeat sweep must not duplicate the filing, got %d events", len(evs))
	}
}

// FR-4 AC (filing-sink failure): a filer error is logged and the sweep
// continues to other records/stores — no panic, no swallowed sweep.
func TestMirrorHealthSweep_FilingSinkFailureContinues(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	driftReport := mirroraudit.Report{
		StorePath: filepath.Join(path, ".beads"),
		Checked:   2,
		Records: []mirroraudit.Record{
			{BeadID: "bead-drift-sink-1", DBStatus: "open", Class: mirroraudit.ClassDBAhead},
			{BeadID: "bead-drift-sink-2", DBStatus: "open", Class: mirroraudit.ClassDBAhead},
		},
		Counts: mirroraudit.Counts{DBAhead: 2},
	}
	o := mirrorEnv(t, path, driftReport, nil)
	// Filing sink fails: ske material is injected by GREEN via the
	// filePanBead seam (mock beadsClient returning error on CreateBead).
	if mock, ok := o.beadsClient.(*mockBeadsClient); ok {
		mock.createErr = errors.New("filing sink unavailable")
	}

	// Filing sink error must be logged, never panic; drift events still fire.
	o.sweepMirrorHealthForProject(o.projects[0], time.Now())
	if evs := readEventsOfType(t, o.eventSink, mirrorDriftDetectedEvent); len(evs) != 2 {
		t.Fatalf("drift detection must continue past filing-sink failure, got %d events", len(evs))
	}
}

// ---------------------------------------------------------------------------
// FR-4: Pan filing for non-healable drift (RED contract — hydra-wyel).
//
// Every divergent record the guarded auto-resync will not heal (user-owned
// store, non-terminal status, write-probe failure, 2-strike, daily cap
// exceeded) files one Pan triage event `mirror_drift_needs_human` with the
// store path, record id, direction, and the suggested one-liner. Fail-open:
// filing errors are logged, never propagated, never panic the sweep.
//
// Pin behavior, not implementation: the tests drive the existing
// sweepMirrorHealthForProject entry point and assert on the EventSink stream
// plus mockBeadsClient.CreateBead (the filing sink).
// ---------------------------------------------------------------------------

// mirrorDriftFixture builds a divergent mirroraudit report: one record whose
// jsonl status is open (jsonl_ahead of the db which says closed).
func mirrorDriftFixture(beadID string) mirroraudit.Report {
	return mirroraudit.Report{
		Checked: 1,
		Records: []mirroraudit.Record{{
			BeadID:      beadID,
			JSONLStatus: "open",
			DBStatus:    "closed",
			Class:       mirroraudit.ClassJSONLAhead,
		}},
	}
}

// AC: fixture test — a user-owned divergent store (cannot-heal) emits exactly
// one Pan filing (one CreateBead on the filing sink) and exactly one
// mirror_drift_needs_human triage event carrying the store path, record id,
// and direction. Never two filings for one record.
func TestMirrorHealthFiling_UserOwnedDivergentStoreFilesExactlyOne(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	o := mirrorEnv(t, path, mirrorDriftFixture("drift-1"), nil)
	mock := o.beadsClient.(*mockBeadsClient)

	// User-owned store: the write probe refuses (cannot-heal path).
	mirrorHooks.writeProbe = func(string) error { return errors.New("permission denied") }
	defer func() { mirrorHooks.writeProbe = nil }()

	sink := NewEventSink(t.TempDir())
	o.eventSink = sink

	o.sweepMirrorHealthForProject(o.projects[0], time.Now())

	createCalls := mock.getCreateCalls()
	if len(createCalls) != 1 {
		t.Fatalf("user-owned divergent store must file exactly one pan bead, got %d calls: %+v", len(createCalls), createCalls)
	}
	if !strings.Contains(createCalls[0].title, "drift-1") {
		t.Fatalf("pan bead title must name the divergent record id, got %q", createCalls[0].title)
	}
	if !strings.Contains(createCalls[0].description, "mirror_drift_needs_human") {
		t.Fatalf("pan bead description must carry the mirror_drift_needs_human triage event name, got %q", createCalls[0].description)
	}

	evts := readEventsOfType(t, sink, "mirror_drift_needs_human")
	if len(evts) != 1 {
		t.Fatalf("user-owned divergent store must emit exactly one mirror_drift_needs_human event, got %d", len(evts))
	}
	if got := evts[0].Fields["store"]; got != filepath.Join(path, ".beads") {
		t.Fatalf("event must carry the store path, got %v", got)
	}
	if got := evts[0].Fields["bead"]; got != "drift-1" {
		t.Fatalf("event must carry the record id, got %v", got)
	}
	if got := fmt.Sprint(evts[0].Fields["direction"]); got != string(mirroraudit.ClassJSONLAhead) {
		t.Fatalf("event must carry the drift direction, got %v", got)
	}
}

// AC: filing-sink failure test — when the filing sink (CreateBead) fails the
// error is logged, the sweep continues (does not panic, does not propagate),
// and no leftover state gates the next sweep.
func TestMirrorHealthFiling_FilingSinkFailureLoggedSweepContinues(t *testing.T) {
	mirrorHealthRequireImpl(t)
	path := t.TempDir()
	o := mirrorEnv(t, path, mirrorDriftFixture("drift-sink"), nil)
	mock := o.beadsClient.(*mockBeadsClient)

	mirrorHooks.writeProbe = func(string) error { return errors.New("permission denied") }
	defer func() { mirrorHooks.writeProbe = nil }()
	mock.createErr = errors.New("store read-only: filing sink blown")

	sink := NewEventSink(t.TempDir())
	o.eventSink = sink

	// Must not panic; must not return an error (fail-open contract).
	o.sweepMirrorHealthForProject(o.projects[0], time.Now())

	if createCalls := mock.getCreateCalls(); len(createCalls) != 1 {
		t.Fatalf("filing sink must have been attempted exactly once despite the failure, got %d", len(createCalls))
	}
	if evts := readEventsOfType(t, sink, "mirror_drift_needs_human"); len(evts) != 0 {
		t.Fatalf("a failed filing must not emit a successful triage event, got %d", len(evts))
	}

	// Second tick with the sink healed: the same record must be retried
	// (filing failures are not sticky strikes — the store is still divergent).
	mock.createErr = nil
	o.sweepMirrorHealthForProject(o.projects[0], time.Now())
	if createCalls := mock.getCreateCalls(); len(createCalls) != 2 {
		t.Fatalf("sweep must continue filing on a later tick after a sink failure, got %d total calls", len(createCalls))
	}
}

// --- RED: Guarded auto-resync (FR-3, OFF by default) tests ---

// spyBrUpdater is a stub BrUpdater recording every heal invocation, so the
// guarded auto-resync contract tests can assert zero vs one heal per tick.
type spyBrUpdater struct {
	calls []mockSpyHealCall
	err   error
}

type mockSpyHealCall struct {
	storePath, beadID, status string
}

// compile-time check that the spy satisfies the BrUpdater seam.
var _ BrUpdater = (*spyBrUpdater)(nil)

func (s *spyBrUpdater) BrUpdateStatus(storePath, beadID, status string) error {
	s.calls = append(s.calls, mockSpyHealCall{storePath: storePath, beadID: beadID, status: status})
	return s.err
}

// mirrorGuardHooks is the test seam for the FR-3 guard conditions: the store
// owner and the write probe. GREEN wires the guarded heal to honor these.
var mirrorGuardHooks struct {
	isDaemonOwned func(storePath string) bool
	writeProbe    func(storePath string) error
}

// mirrorHealEnv extends mirrorEnv with the FR-3 healing surface: stubs the
// BrUpdater seam, enables AutoResync, and resets the guard hooks so each
// test starts from the daemon-owned happy path.
func mirrorHealEnv(t *testing.T, projectPath string, auditReport mirroraudit.Report, auditErr error) (*Overseer, *spyBrUpdater) {
	t.Helper()
	o := mirrorEnv(t, projectPath, auditReport, auditErr)
	o.config.Overseer.MirrorHealth.AutoResync = true
	spy := &spyBrUpdater{}
	o.brUpdater = spy
	mirrorGuardHooks.isDaemonOwned = func(string) bool { return true }
	mirrorGuardHooks.writeProbe = func(string) error { return nil }
	return o, spy
}

// AC: fixture heals a db_ahead terminal record through the BrUpdater seam.
// The store is daemon-owned (guard default) and the write probe succeeds, so
// the healing path must fire exactly once with the terminal status.
func TestMirrorHealthSweep_HealsDBAheadTerminalRecord(t *testing.T) {
	mirrorHealthRequireImpl(t)
	defer mirrorHookRestore()()
	repoReport := mirroraudit.Report{
		Checked: 1,
		Records: []mirroraudit.Record{
			{BeadID: "proj-heal1", DBStatus: "closed", Class: mirroraudit.ClassDBAhead},
		},
		Counts: mirroraudit.Counts{DBAhead: 1},
	}
	o, spy := mirrorHealEnv(t, t.TempDir(), repoReport, nil)

	o.sweepMirrorHealth(time.Now())

	if len(spy.calls) != 1 {
		t.Fatalf("healing must be attempted exactly once for a terminal db_ahead record, got %d", len(spy.calls))
	}
	call := spy.calls[0]
	if call.beadID != "proj-heal1" {
		t.Fatalf("heal target must be the divergent bead, got %s", call.beadID)
	}
	if call.status != "closed" {
		t.Fatalf("heal status must be the terminal status from the audit, got %q", call.status)
	}
}

// AC: a non-terminal jsonl status NEVER heals — the mirror must not
// resurrect a stale in-progress claim. The record is instead routed to the
// filing path (mirror_drift_needs_human pan filing).
func TestMirrorHealthSweep_NonTerminalRecordNeverHeals(t *testing.T) {
	mirrorHealthRequireImpl(t)
	defer mirrorHookRestore()()
	nonTerminalReport := mirroraudit.Report{
		Checked: 1,
		Records: []mirroraudit.Record{
			{BeadID: "proj-nt1", JSONLStatus: "in_progress", DBStatus: "closed", Class: mirroraudit.ClassJSONLAhead},
		},
		Counts: mirroraudit.Counts{JSONLAhead: 1},
	}
	o, spy := mirrorHealEnv(t, t.TempDir(), nonTerminalReport, nil)
	mock := o.beadsClient.(*mockBeadsClient)

	o.sweepMirrorHealth(time.Now())

	if len(spy.calls) != 0 {
		t.Fatalf("non-terminal drift must never heal via BrUpdater, got %d heal invocations", len(spy.calls))
	}
	if createCalls := mock.getCreateCalls(); len(createCalls) == 0 {
		t.Fatal("non-terminal drift must be routed to the filing path (CreateBead), got 0 filings")
	}
}

// AC guard: a user-owned store (owner != daemon) must never heal —
// humans own their stores; filing only.
func TestMirrorHealthSweep_UserOwnedStoreNeverHeals(t *testing.T) {
	mirrorHealthRequireImpl(t)
	defer mirrorHookRestore()()
	repoReport := mirroraudit.Report{
		Checked: 1,
		Records: []mirroraudit.Record{
			{BeadID: "proj-user1", DBStatus: "closed", Class: mirroraudit.ClassDBAhead},
		},
		Counts: mirroraudit.Counts{DBAhead: 1},
	}
	o, spy := mirrorHealEnv(t, t.TempDir(), repoReport, nil)
	mock := o.beadsClient.(*mockBeadsClient)
	mirrorGuardHooks.isDaemonOwned = func(string) bool { return false }

	o.sweepMirrorHealth(time.Now())

	if len(spy.calls) != 0 {
		t.Fatalf("user-owned store must never heal, got %d heal invocations", len(spy.calls))
	}
	// Fail-closed filing path: a record the guard refuses to heal must go to
	// the human triage queue, never silently dropped.
	if createCalls := mock.getCreateCalls(); len(createCalls) == 0 {
		t.Fatal("user-owned store drift must still be filed (CreateBead), got 0 filings")
	}
}

// AC guard: a failed write-probe means the daemon cannot safely rewrite the
// mirror — no heal, file instead.
func TestMirrorHealthSweep_WriteProbeFailureNeverHeals(t *testing.T) {
	mirrorHealthRequireImpl(t)
	defer mirrorHookRestore()()
	repoReport := mirroraudit.Report{
		Checked: 1,
		Records: []mirroraudit.Record{
			{BeadID: "proj-probe1", DBStatus: "closed", Class: mirroraudit.ClassDBAhead},
		},
		Counts: mirroraudit.Counts{DBAhead: 1},
	}
	o, spy := mirrorHealEnv(t, t.TempDir(), repoReport, nil)
	mock := o.beadsClient.(*mockBeadsClient)
	mirrorGuardHooks.writeProbe = func(string) error { return errors.New("permission denied: probe write failed") }

	o.sweepMirrorHealth(time.Now())

	if len(spy.calls) != 0 {
		t.Fatalf("failed write probe must never heal, got %d heal invocations", len(spy.calls))
	}
	// Fail-closed filing path: a record the guard refuses to heal must go to
	// the human triage queue, never silently dropped.
	if createCalls := mock.getCreateCalls(); len(createCalls) == 0 {
		t.Fatal("write-probe-failed drift must still be filed (CreateBead), got 0 filings")
	}
}

// AC: auto_resync OFF by default — even an eligible terminal drift record does
// not heal while the knob is false, and the drift is filed for a human.
func TestMirrorHealthSweep_AutoResyncOffByDefault(t *testing.T) {
	mirrorHealthRequireImpl(t)
	defer mirrorHookRestore()()
	repoReport := mirroraudit.Report{
		Checked: 1,
		Records: []mirroraudit.Record{
			{BeadID: "proj-off1", DBStatus: "closed", Class: mirroraudit.ClassDBAhead},
		},
		Counts: mirroraudit.Counts{DBAhead: 1},
	}
	o := mirrorEnv(t, t.TempDir(), repoReport, nil)
	o.config.Overseer.MirrorHealth.AutoResync = false
	spy := &spyBrUpdater{}
	o.brUpdater = spy
	if o.config.Overseer.MirrorHealth.AutoResync {
		t.Fatal("mirror_health.auto_resync must default to false (OFF by default PRD contract)")
	}
	mock := o.beadsClient.(*mockBeadsClient)

	o.sweepMirrorHealth(time.Now())

	if len(spy.calls) != 0 {
		t.Fatalf("auto_resync=false must never heal, got %d heal invocations", len(spy.calls))
	}
	if createCalls := mock.getCreateCalls(); len(createCalls) == 0 {
		t.Fatal("auto_resync=false terminal drift must still be filed (CreateBead), got 0 filings")
	}
}
