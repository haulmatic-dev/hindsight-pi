package core

import (
	"os"
	"path/filepath"
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
}

func mirrorHookRestore() (restore func()) {
	oldmtime, oldlive, oldrunner := mirrorJSONLMtime, mirrorHasLiveWorker, mirrorAuditRunner
	mirrorJSONLMtime = mirrorHooks.jsonlMtime
	mirrorHasLiveWorker = mirrorHooks.liveWorker
	mirrorAuditRunner = mirrorHooks.auditRunner
	return func() {
		mirrorJSONLMtime, mirrorHasLiveWorker, mirrorAuditRunner = oldmtime, oldlive, oldrunner
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
