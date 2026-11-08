package core

// bead_claims_cache_test.go — RED contract tests for FR-002 "Claim re-proof
// at respawn with payload persistence". Pins the parent ACs:
//
//  1. verified-claim results cached on the bead payload via durable labels
//     `claims-ok-<first8>` (fingerprint) + `claims-ok-at-<unix>` (timestamp);
//     respawn reuses cached passes without re-running probes unless the
//     worktree base_sha changed.
//  2. changed base_sha → full re-proof; broken claims dropped with a typed
//     `claim_dropped` event `{bead, assertion}` (mirror of `pack_miss`);
//     `hard:` dropped claims still refuse the spawn (fail-closed untouched).
//  3. cached-pass reuse adds <5ms to the spawn path (wall-time bound, probe
//     asserted uninvoked).
//
// Same RED discipline as the drift-check suite: tests SKIP loudly by default
// (suite stays green pre-GREEN); BEAD_CLAIMS_CACHE_ENFORCE=1 fails them
// proving RED detects the missing contract.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func claimCacheEnforced() bool {
	return strings.TrimSpace(firstEnvOr("BEAD_CLAIMS_CACHE_ENFORCE", "")) == "1"
}

func claimCacheImplementedGate(t *testing.T) {
	t.Helper()
	if claimCacheImplemented {
		return
	}
	msg := "RED: claim re-proof cache contract not yet implemented"
	if claimCacheEnforced() {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

// claimCacheHarness returns an Overseer wired to a real EventSink, the sink's
// central dir (for event readback), a mutable probe count, and a probe runner
// override hook.
func claimCacheHarness(t *testing.T) (o *Overseer, eventsDir string, probeCount *int, setProbe func(func(cmd string, timeout time.Duration) (string, error))) {
	t.Helper()
	dir := t.TempDir()
	sink := NewEventSink(dir)
	o = &Overseer{beadCloser: &BeadCloser{events: sink}}
	o.beadsClient = newMockBeadsClient()
	n := 0
	probeCount = &n
	setProbe = func(fn func(cmd string, timeout time.Duration) (string, error)) {
		claimCheckRunner = func(cmd string, timeout time.Duration) (string, error) {
			*probeCount++
			if fn == nil {
				return "ok", nil
			}
			return fn(cmd, timeout)
		}
	}
	t.Cleanup(func() { claimCheckRunner = nil })
	return o, dir, probeCount, setProbe
}

func setClaimHeadProbe(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	old := claimHeadProbe
	claimHeadProbe = fn
	t.Cleanup(func() { claimHeadProbe = old })
}

func claimTestBead(desc string) types.Bead {
	return types.Bead{
		ID:          types.BeadID("hydra-clm1"),
		Title:       "claim cache fixture",
		Type:        "implement",
		Status:      types.StatusOpen,
		Description: desc,
		Project:     "proj",
	}
}

var claimsOkLabelRe = regexp.MustCompile(`^claims-ok-[0-9a-f]{8}$`)
var claimsOkAtRe = regexp.MustCompile(`^claims-ok-at-\d+$`)

func labelsOf(o *Overseer, id types.BeadID) []string {
	b, err := o.beadsClient.GetBead("", "", id)
	if err != nil || b == nil {
		return nil
	}
	return b.Labels
}

func readClaimEvents(t *testing.T, dir, evType string) []Event {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(entries) == 0 {
		return nil
	}
	var out []Event
	for _, p := range entries {
		evs, err := ReadEvents(p)
		if err != nil {
			continue
		}
		for _, ev := range evs {
			if ev.Type == evType {
				out = append(out, ev)
			}
		}
	}
	return out
}

// 1. Full pass on first spawn → cache labels written (fingerprint + timestamp),
// spawn proceeds.
func TestClaimCache_PassOnFirstSpawnWritesLabels(t *testing.T) {
	claimCacheImplementedGate(t)
	o, _, probes, setProbe := claimCacheHarness(t)
	setProbe(nil)
	setClaimHeadProbe(t, func(string) (string, error) { return "head-1", nil })

	bead := claimTestBead("claim: tests pass (verify: echo t1)")
	if err := o.spawnPreflightBeadClaims(bead, t.TempDir()); err != nil {
		t.Fatalf("spawn should proceed after full pass: %v", err)
	}
	if *probes != 1 {
		t.Fatalf("probe should run once on first spawn, ran %d", *probes)
	}
	ls := labelsOf(o, bead.ID)
	var fp, at bool
	for _, l := range ls {
		if claimsOkLabelRe.MatchString(l) {
			fp = true
		}
		if claimsOkAtRe.MatchString(l) {
			at = true
		}
	}
	if !fp || !at {
		t.Fatalf("expected claims-ok-<fp8> + claims-ok-at-<unix> labels, got %v", ls)
	}
}

// 2. Warm cache + unchanged base_sha → respawn does NOT re-run probes
// (<5ms) and spawn proceeds.
func TestClaimCache_WarmCacheSameBaseSHA_ReusesWithoutProbes(t *testing.T) {
	claimCacheImplementedGate(t)
	o, _, probes, setProbe := claimCacheHarness(t)
	setProbe(nil)
	setClaimHeadProbe(t, func(string) (string, error) { return "head-1", nil })

	bead := claimTestBead("claim: tests pass (verify: echo t2)")
	if err := o.spawnPreflightBeadClaims(bead, t.TempDir()); err != nil {
		t.Fatalf("first spawn failed: %v", err)
	}
	*probes = 0

	// Simulate respawn: bead re-read from the store (labels now cached).
	respawn, _ := o.beadsClient.GetBead("", "", bead.ID)
	start := time.Now()
	err := o.spawnPreflightBeadClaims(*respawn, t.TempDir())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("cached pass should allow spawn: %v", err)
	}
	if *probes != 0 {
		t.Fatalf("cached pass must not re-run probes, ran %d", *probes)
	}
	if elapsed > 5*time.Millisecond {
		t.Fatalf("cached-pass path took %s; hard bound <5ms", elapsed)
	}
}

// 3. Changed base_sha → full re-proof: probes run again and the cache is
// refreshed (new claims-ok-at timestamp).
func TestClaimCache_ChangedBaseSHA_RepoofsAndRefreshesCache(t *testing.T) {
	claimCacheImplementedGate(t)
	o, _, probes, setProbe := claimCacheHarness(t)
	setProbe(nil)
	head := "head-1"
	setClaimHeadProbe(t, func(string) (string, error) { return head, nil })

	bead := claimTestBead("claim: tests pass (verify: echo t3)")
	if err := o.spawnPreflightBeadClaims(bead, t.TempDir()); err != nil {
		t.Fatalf("first spawn failed: %v", err)
	}
	firstAt := ""
	for _, l := range labelsOf(o, bead.ID) {
		if claimsOkAtRe.MatchString(l) {
			firstAt = l
		}
	}
	*probes = 0
	time.Sleep(2 * time.Millisecond) // ensure a newer unix timestamp

	// base_sha moved → re-proof.
	head = "head-2"
	respawn, _ := o.beadsClient.GetBead("", "", bead.ID)
	if err := o.spawnPreflightBeadClaims(*respawn, t.TempDir()); err != nil {
		t.Fatalf("re-proof of passing claims must allow spawn: %v", err)
	}
	if *probes == 0 {
		t.Fatal("changed base_sha must fully re-proof (probes re-run)")
	}
	secondAt := ""
	for _, l := range labelsOf(o, bead.ID) {
		if claimsOkAtRe.MatchString(l) {
			secondAt = l
		}
	}
	if secondAt == "" || secondAt == firstAt {
		t.Fatalf("cache must refresh claims-ok-at on re-proof: first=%q second=%q", firstAt, secondAt)
	}
}

// 4. Re-proof of a previously-cached now-broken soft claim → typed
// `claim_dropped` event {bead, assertion}; spawn proceeds.
func TestClaimCache_Reproof_BrokenClaimDropped(t *testing.T) {
	claimCacheImplementedGate(t)
	o, dir, probes, setProbe := claimCacheHarness(t)
	setProbe(nil)
	head := "head-1"
	setClaimHeadProbe(t, func(string) (string, error) { return head, nil })

	desc := "claim: lint clean (verify: echo lint-ok)"
	bead := claimTestBead(desc)
	if err := o.spawnPreflightBeadClaims(bead, t.TempDir()); err != nil {
		t.Fatalf("first spawn failed: %v", err)
	}
	*probes = 0

	// Environment moved; the claim is now dead.
	head = "head-2"
	setProbe(func(string, time.Duration) (string, error) {
		return "FAIL: assertion no longer holds", fmt.Errorf("exit status 1")
	})
	respawn, _ := o.beadsClient.GetBead("", "", bead.ID)
	if err := o.spawnPreflightBeadClaims(*respawn, t.TempDir()); err != nil {
		t.Fatalf("soft broken claim is dropped (warn), spawn proceeds; got %v", err)
	}
	events := readClaimEvents(t, dir, "claim_dropped")
	if len(events) != 1 {
		t.Fatalf("expected exactly one claim_dropped event, got %d", len(events))
	}
	if events[0].Bead != string(bead.ID) {
		t.Fatalf("claim_dropped event bead = %q, want %q", events[0].Bead, bead.ID)
	}
	if !strings.Contains(events[0].Detail, "lint clean") {
		t.Fatalf("claim_dropped assertion missing from detail: %q", events[0].Detail)
	}
}

// 4b. `hard:` dropped claim still refuses the spawn (fail-closed gate is
// not weakened by dropping).
func TestClaimCache_Reproof_BrokenHardClaimRefusesSpawn(t *testing.T) {
	claimCacheImplementedGate(t)
	o, _, probes, setProbe := claimCacheHarness(t)
	setProbe(nil)
	head := "head-1"
	setClaimHeadProbe(t, func(string) (string, error) { return head, nil })

	bead := claimTestBead("hard: claim: disk ok (verify: echo disk-ok)")
	if err := o.spawnPreflightBeadClaims(bead, t.TempDir()); err != nil {
		t.Fatalf("first spawn failed: %v", err)
	}
	*probes = 0

	head = "head-2"
	setProbe(func(string, time.Duration) (string, error) {
		return "FAIL: disk full", fmt.Errorf("exit status 1")
	})
	respawn, _ := o.beadsClient.GetBead("", "", bead.ID)
	err := o.spawnPreflightBeadClaims(*respawn, t.TempDir())
	var sup *SpawnSuppressedError
	if err == nil {
		t.Fatal("hard claim still fails closed after base_sha change; spawn must be refused")
	}
	if !errors.As(err, &sup) {
		t.Fatalf("want SpawnSuppressedError, got %T: %v", err, err)
	}
}

// 5. Unknown probe outcomes are never cached (tooling failure may recover).
func TestClaimCache_UnknownNeverCached(t *testing.T) {
	claimCacheImplementedGate(t)
	o, _, _, setProbe := claimCacheHarness(t)
	setProbe(func(string, time.Duration) (string, error) {
		return "", context.DeadlineExceeded
	})
	setClaimHeadProbe(t, func(string) (string, error) { return "head-1", nil })

	bead := claimTestBead("claim: unknown-ish (verify: sleep 5)")
	if err := o.spawnPreflightBeadClaims(bead, t.TempDir()); err != nil {
		t.Fatalf("unknown failures fail open: %v", err)
	}
	for _, l := range labelsOf(o, bead.ID) {
		if claimsOkLabelRe.MatchString(l) || claimsOkAtRe.MatchString(l) {
			t.Fatalf("unknown outcome must not be cached; label %q present", l)
		}
	}
}

// 6. Failing claims with no prior cache → no cache labels are written.
func TestClaimCache_FailedClaimsNeverCached(t *testing.T) {
	claimCacheImplementedGate(t)
	o, _, _, setProbe := claimCacheHarness(t)
	setProbe(func(string, time.Duration) (string, error) {
		return "FAIL", fmt.Errorf("exit status 1")
	})
	setClaimHeadProbe(t, func(string) (string, error) { return "head-1", nil })

	bead := claimTestBead("claim: always broken (verify: false)")
	if err := o.spawnPreflightBeadClaims(bead, t.TempDir()); err != nil {
		t.Fatalf("soft claim failure must not refuse spawn: %v", err)
	}
	for _, l := range labelsOf(o, bead.ID) {
		if claimsOkLabelRe.MatchString(l) {
			t.Fatalf("failed claim cached as ok: %q", l)
		}
	}
}
