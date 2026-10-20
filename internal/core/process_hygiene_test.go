package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/internal/registry"
)

// newHygieneTestCharon builds a Charon with the hygiene sensors armed and no
// live infrastructure beyond what each test injects.
func newHygieneTestCharon(t *testing.T) *Charon {
	t.Helper()
	wt, err := NewWorkerTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkerTracker: %v", err)
	}
	return NewCharon(nil, wt, time.Minute)
}

func TestZeroTokenStallDetector(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	j := newHygieneTestCharon(t)
	j.SetRegistry(st)

	old := time.Now().Add(-20 * time.Minute)
	// Working, tokens=0, age>15m → flagged.
	if err := st.UpsertWorker(registry.WorkerRecord{BeadID: "stalled", PID: 111, State: registry.StateWorking, StartedAt: old, LastHeartbeat: time.Now()}); err != nil {
		t.Fatalf("UpsertWorker stalled: %v", err)
	}
	// Working, tokens>0, age>15m → NOT flagged (long tool call, earlier turns).
	if err := st.UpsertWorker(registry.WorkerRecord{BeadID: "productive", PID: 222, State: registry.StateWorking, StartedAt: old, LastHeartbeat: time.Now(), TokensSoFar: 5000}); err != nil {
		t.Fatalf("UpsertWorker productive: %v", err)
	}
	// Working, tokens=0, young → NOT flagged (spawn window).
	if err := st.UpsertWorker(registry.WorkerRecord{BeadID: "young", PID: 333, State: registry.StateWorking, StartedAt: time.Now().Add(-2 * time.Minute), LastHeartbeat: time.Now()}); err != nil {
		t.Fatalf("UpsertWorker young: %v", err)
	}
	// Completed with tokens=0 → NOT flagged (terminal state).
	if err := st.UpsertWorker(registry.WorkerRecord{BeadID: "done", PID: 444, State: registry.StateCompleted, StartedAt: old, LastHeartbeat: time.Now()}); err != nil {
		t.Fatalf("UpsertWorker done: %v", err)
	}

	stalls := j.detectZeroTokenStalls()
	if len(stalls) != 1 || stalls[0].BeadID != "stalled" {
		t.Fatalf("expected exactly [stalled], got %+v", stalls)
	}

	// No registry → nil, no panic.
	nilJ := newHygieneTestCharon(t)
	if got := nilJ.detectZeroTokenStalls(); got != nil {
		t.Fatalf("nil registry should return nil, got %+v", got)
	}
}

func TestKillStalledWorker_ExplicitPIDOnly(t *testing.T) {
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	j := newHygieneTestCharon(t)
	j.SetRegistry(st)
	var killedPIDs []int
	j.killFn = func(pid int) error {
		killedPIDs = append(killedPIDs, pid)
		return nil
	}
	if err := st.UpsertWorker(registry.WorkerRecord{BeadID: "stall-1", PID: 777, State: registry.StateWorking, StartedAt: time.Now().Add(-time.Hour), LastHeartbeat: time.Now()}); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}
	if err := j.KillStalledWorker("stall-1"); err != nil {
		t.Fatalf("KillStalledWorker: %v", err)
	}
	if len(killedPIDs) != 1 || killedPIDs[0] != 777 {
		t.Fatalf("expected explicit PID 777 killed, got %v", killedPIDs)
	}
	rec, ok, err := st.GetWorker("stall-1")
	if err != nil || !ok {
		t.Fatalf("GetWorker: %v ok=%v", err, ok)
	}
	if rec.State != registry.StateCompleted {
		t.Fatalf("row not marked dead: state=%q err_message=%q", rec.State, rec.ErrMessage)
	}
	if rec.ErrMessage != "zero-token-stall-killed" {
		t.Fatalf("expected zero-token-stall-killed provenance, got %q", rec.ErrMessage)
	}
	// Unknown bead → loud error.
	if err := j.KillStalledWorker("no-such-bead"); err == nil {
		t.Fatal("expected error for unknown bead")
	}
}

func TestRoguePIScanner(t *testing.T) {
	j := newHygieneTestCharon(t)
	proc := t.TempDir()
	// Fake boot time 3 hours ago so a starttime of ~0 ticks is >2h old.
	btime := time.Now().Add(-3 * time.Hour).Unix()
	if err := os.WriteFile(filepath.Join(proc, "stat"), []byte(fmt.Sprintf("btime %d\n", btime)), 0o644); err != nil {
		t.Fatal(err)
	}
	mkProc := func(pid int, argv0, cgroup string, startTicks string) {
		t.Helper()
		dir := filepath.Join(proc, fmt.Sprint(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(name, content string) {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("cmdline", argv0+"\x00")
		write("cgroup", cgroup+"\n")
		// Pad to field 22: after ')' come fields 3..22 → 20 entries; starttime
		// is the 20th (index 19).
		// After ')': fields 3..21 (19 tokens: state, ppid, pgrp, session, tty,
		// tpgid, flags, minflt..itrealvalue), then field 22 = starttime.
		stat := "1 (x) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 0 " + startTicks + " 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
		write("stat", stat)
	}
	// The live fixture shape: bare pi, session scope (outside hydra.service), old.
	mkProc(3002553, "/usr/local/bin/pi", "0::/user.slice/user-0.slice/session-13176.scope", "0")
	// Daemon-owned pi inside hydra.service → not rogue.
	mkProc(4000001, "/usr/local/bin/pi", "0::/system.slice/hydra.service", "0")
	// Tracked worker pi → not rogue.
	mkProc(4000002, "/usr/local/bin/pi", "0::/user.slice/other.scope", "0")
	// Young rogue → not rogue (age gate).
	mkProc(4000003, "/usr/local/bin/pi", "0::/user.slice/young.scope", "720000") // +2h over btime(now-3h) = started 1h ago → under the 2h gate
	// Non-pi process outside hydra.service → not rogue.
	mkProc(4000004, "/usr/bin/vim", "0::/user.slice/editor.scope", "0")

	tracked := map[int]bool{4000002: true}
	rogues := j.scanRoguePisIn(proc, tracked)
	if len(rogues) != 1 || rogues[0].PID != 3002553 {
		t.Fatalf("expected only fixture PID 3002553, got %+v", rogues)
	}
	if isHydraServiceCgroup(rogues[0].Cgroup) {
		t.Fatal("fixture cgroup must not classify as hydra.service")
	}
	if rogues[0].Cmdline == "" || !strings.Contains(rogues[0].Cmdline, "pi") {
		t.Fatalf("cmdline not captured: %q", rogues[0].Cmdline)
	}

	// Scan again: no duplicate events (dedupe per PID).
	if again := j.scanRoguePisIn(proc, tracked); len(again) != 1 {
		t.Fatalf("rescan snapshot lost: %+v", again)
	}

	// Digest snapshot present.
	if got := j.RoguePiReport(); len(got) != 1 || got[0].PID != 3002553 {
		t.Fatalf("RoguePiReport: %+v", got)
	}
}

func TestKillRoguePis_ExplicitPIDAndClearsSnapshot(t *testing.T) {
	j := newHygieneTestCharon(t)
	j.hygiene.rogues = []RoguePi{{PID: 3002553, Cmdline: "pi", Cgroup: "session", Age: 3 * time.Hour}}
	var killed []int
	j.killFn = func(pid int) error {
		killed = append(killed, pid)
		return nil
	}
	n, err := j.KillRoguePis()
	if err != nil {
		t.Fatalf("KillRoguePis: %v", err)
	}
	if n != 1 || len(killed) != 1 || killed[0] != 3002553 {
		t.Fatalf("expected explicit kill of 3002553, got n=%d killed=%v", n, killed)
	}
	if got := j.RoguePiReport(); len(got) != 0 {
		t.Fatalf("snapshot not cleared: %+v", got)
	}
}

func TestRepairVocabulary_IncludesProcessHygiene(t *testing.T) {
	for _, name := range []string{"kill-stalled-worker", "kill-rogue-pi"} {
		if !pan.IsAllowedRepair("repair:" + name) {
			t.Fatalf("repair:%s not whitelisted", name)
		}
	}
}

func TestDigest_SurfacesProcessHygiene(t *testing.T) {
	j := newHygieneTestCharon(t)
	j.events = NewEventSink(t.TempDir())
	j.hygiene.stalls = map[string]registry.WorkerRecord{
		"stalled-bead": {BeadID: "stalled-bead", PID: 555, State: registry.StateWorking, StartedAt: time.Now().Add(-time.Hour)},
	}
	j.hygiene.rogues = []RoguePi{{PID: 3002553, Cmdline: "pi", Cgroup: "session", Age: 3 * time.Hour}}
	o := &Overseer{orphanJanitor: j}
	digest := o.collectHealthDigest()
	if len(digest.ZeroTokenStalls) != 1 || digest.ZeroTokenStalls[0] != "stalled-bead@pid555" {
		t.Fatalf("ZeroTokenStalls: %+v", digest.ZeroTokenStalls)
	}
	if len(digest.RoguePis) != 1 {
		t.Fatalf("RoguePis: %+v", digest.RoguePis)
	}
	joined := strings.Join(digest.HostAnomalies, "\n")
	if !strings.Contains(joined, "repair:kill-stalled-worker stalled-bead") {
		t.Fatalf("digest missing stalled-worker anomaly: %s", joined)
	}
	if !strings.Contains(joined, "rogue pi PID 3002553") || !strings.Contains(joined, "repair:kill-rogue-pi") {
		t.Fatalf("digest missing rogue-pi anomaly: %s", joined)
	}
	if !strings.Contains(digest.Text(), "process-hygiene") {
		t.Fatalf("Text() missing process-hygiene line:\n%s", digest.Text())
	}
}

func TestNilHygieneSafe(t *testing.T) {
	j := &Charon{} // no hygiene state — legacy construction paths
	if got := j.ZeroTokenStallReport(); len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
	if got := j.RoguePiReport(); len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
	if n, err := j.KillRoguePis(); n != 0 || err != nil {
		t.Fatalf("nil hygiene kill should no-op, got %d %v", n, err)
	}
	if j.detectZeroTokenStalls() != nil {
		t.Fatal("nil registry detect should be nil")
	}
}

// TestRoguePIScanner_LiveFixture is the dogfood live proof: the deliberately
// preserved rogue bare-pi process (root session scope, outside
// hydra.service) must be detected by the scanner on this box. Skips silently
// once an operator/repair has killed it.
func TestRoguePIScanner_LiveFixture(t *testing.T) {
	const fixturePID = 3002553
	if _, err := os.Stat(fmt.Sprintf("/proc/%d/cmdline", fixturePID)); err != nil {
		t.Skipf("live rogue-pi fixture PID %d not present (already killed): %v", fixturePID, err)
	}
	j := newHygieneTestCharon(t)
	rogues := j.scanRoguePis()
	found := false
	for _, r := range rogues {
		if r.PID == fixturePID {
			found = true
			t.Logf("LIVE FIXTURE CAUGHT: PID %d cmdline=%q cgroup=%q age=%v", r.PID, r.Cmdline, r.Cgroup, r.Age.Round(time.Minute))
			if isHydraServiceCgroup(r.Cgroup) {
				t.Fatal("fixture classified as hydra.service")
			}
		}
	}
	if !found {
		t.Fatalf("live rogue-pi fixture PID %d NOT detected; rogues=%+v", fixturePID, rogues)
	}
}

// TestKillRoguePis_LiveFixture is the live dispatch proof for the Pan
// repair: it runs the REAL kill path (no killFn stub) against the preserved
// rogue fixture PID 3002553. When the daemon account can signal it, the
// repair kills it; when the fixture is root-owned (this box: daemon uid
// hydra, fixture owner root) the repair fails closed with the typed
// rogue_pi_kill_denied escalation and the exact root remedy. Skips when the
// fixture is already gone.
func TestKillRoguePis_LiveFixture(t *testing.T) {
	const fixturePID = 3002553
	if _, err := os.Stat(fmt.Sprintf("/proc/%d/cmdline", fixturePID)); err != nil {
		t.Skipf("live rogue-pi fixture PID %d not present (already killed): %v", fixturePID, err)
	}
	j := newHygieneTestCharon(t)
	sink := NewEventSink(t.TempDir())
	j.events = sink
	procIsOurs := func(pid int) bool {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				fields := strings.Fields(line)
				return len(fields) > 1 && fields[1] == fmt.Sprint(os.Getuid())
			}
		}
		return false
	}
	rogues := j.scanRoguePis()
	var target *RoguePi
	for i := range rogues {
		if rogues[i].PID == fixturePID {
			target = &rogues[i]
		}
	}
	if target == nil {
		t.Fatalf("scanner did not report live fixture PID %d; rogues=%+v", fixturePID, rogues)
	}
	n, err := j.KillRoguePis() // REAL kill: SIGTERM → grace → SIGKILL
	if err == nil && n == 1 {
		if _, statErr := os.Stat(fmt.Sprintf("/proc/%d/cmdline", fixturePID)); statErr == nil {
			time.Sleep(3 * time.Second)
		}
		if _, statErr := os.Stat(fmt.Sprintf("/proc/%d/cmdline", fixturePID)); statErr == nil {
			t.Fatalf("repair reported killed but PID %d still alive", fixturePID)
		}
		t.Logf("LIVE REPAIR KILLED fixture PID %d", fixturePID)
		return
	}
	// Not killed: either EPERM (root-owned fixture) or fixture vanished
	// between scan and kill. Root-owned is the expected live outcome here.
	if _, statErr := os.Stat(fmt.Sprintf("/proc/%d/cmdline", fixturePID)); statErr != nil {
		t.Logf("fixture vanished between scan and kill; repair path exercised")
		return
	}
	if !isKillPermissionDenied(err) {
		t.Fatalf("live repair failed for a non-permission reason: %v", err)
	}
	if procIsOurs(fixturePID) {
		t.Fatalf("fixture PID %d is owned by the daemon uid — kill should have succeeded", fixturePID)
	}
	if got := j.RoguePiReport(); len(got) != 0 {
		t.Fatalf("snapshot should be cleared even on escalation: %+v", got)
	}
	t.Logf("LIVE REPAIR DISPATCHED: real kill path attempted on PID %d; permission-denied escalation verified (root-owned rogue — root must kill PID %d)", fixturePID, fixturePID)
}
