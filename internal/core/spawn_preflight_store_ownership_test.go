package core

// spawn_preflight_store_ownership_test.go — tests for the foreign-uid store
// spawn preflight (bug hydra-6g4u: daemon uid 999 writes .beads stores inside
// user-owned repos; br's fsqlite sidecars are uid-locked, so cross-uid WRITES
// can fail). 2026-10-13: downgraded from fail-closed to fail-open — the
// uid-mismatch heuristic blocked every spawn into user-owned repos even
// though cross-uid reads work and workers run as the repo owner via su.
// daemonUID is a test seam (production: os.Geteuid()).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// writeFakeBr places a stub `br` executable at the front of PATH so the
// store-ownership preflight's `br list --json --limit 1` probe can be driven
// deterministically without a real beads store (CMM: stub br in temp PATH).
func writeFakeBr(t *testing.T, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	if err := os.WriteFile(filepath.Join(dir, "br"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// newOwnershipTestOverseer builds an Overseer wired to an EventSink the same
// way the spawn preflight emits events (via beadCloser.events).
func newOwnershipTestOverseer(t *testing.T) (*Overseer, *EventSink) {
	sink := NewEventSink(t.TempDir())
	o := &Overseer{beadCloser: &BeadCloser{events: sink}}
	return o, sink
}

// foreignBeadsStore creates a .beads store (plus optional inner files) and
// stubs daemonUID so the store is foreign to the "daemon".
func foreignBeadsStore(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	beads := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(beads)
	owner := int(st.Sys().(*syscall.Stat_t).Uid)
	oldUID := daemonUID
	daemonUID = func() int { return owner + 1 } // simulate foreign daemon
	t.Cleanup(func() { daemonUID = oldUID })
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(beads, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSpawnPreflightStoreOwnership_ForeignOwnerWarnsAndPasses(t *testing.T) {
	oldUID := daemonUID
	daemonUID = func() int { return 999 }
	defer func() { daemonUID = oldUID }()

	// Test tmp dirs are owned by the test's own uid — which differs from the
	// stubbed daemon uid 999 (unless the test itself runs as uid 999, in which
	// case skip the mismatch test: the guard correctly passes).
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, ".beads"))
	owner := int(st.Sys().(*syscall.Stat_t).Uid)
	if owner == 999 {
		t.Skip("test process runs as uid 999; cannot create a foreign-owned store")
	}

	o := &Overseer{}
	err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, dir)
	if err != nil {
		t.Fatalf("foreign-uid store must fail open (warn only), got %v", err)
	}
}

func TestSpawnPreflightStoreOwnership_MissingStorePasses(t *testing.T) {
	o := &Overseer{}
	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, t.TempDir()); err != nil {
		t.Fatalf("missing .beads store must fail open, got %v", err)
	}
	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, ""); err != nil {
		t.Fatalf("empty projectPath must pass, got %v", err)
	}
}

// Probe exit 0 (br list succeeds on the foreign store) => the preflight must
// stay silent: debug-level logging only, no spawn_warn_store_ownership event.
func TestStoreOwnershipPreflight_ProbeSuccessIsSilent(t *testing.T) {
	writeFakeBr(t, 0)
	dir := foreignBeadsStore(t, ".br_history", "issues.jsonl")
	o, sink := newOwnershipTestOverseer(t)

	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, dir); err != nil {
		t.Fatalf("successful probe must not block, got %v", err)
	}
	if sinkHasEvent(sink, "spawn_warn_store_ownership") {
		t.Fatal("successful br probe must suppress spawn_warn_store_ownership event (debug-level only)")
	}
}

// Probe failure => warn + event, and the event detail must name the actual
// offending paths (relative to the store, up to 3), not a generic
// store-wide string.
func TestStoreOwnershipPreflight_ProbeFailureWarnsWithPaths(t *testing.T) {
	writeFakeBr(t, 1)
	dir := foreignBeadsStore(t, ".br_history", "issues.jsonl", "beads.db")
	o, sink := newOwnershipTestOverseer(t)

	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, dir); err != nil {
		t.Fatalf("failed probe must still fail open, got %v", err)
	}
	if !sinkHasEvent(sink, "spawn_warn_store_ownership") {
		t.Fatal("failed br probe must emit spawn_warn_store_ownership")
	}
	path := filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	data, _ := os.ReadFile(path)
	detail := string(data)
	for _, want := range []string{".beads/.br_history:", ".beads/issues.jsonl:", ".beads/beads.db:"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("event detail must name offending path %q, got: %s", want, detail)
		}
	}
}

// More than 3 offending paths => detail is capped at 3.
func TestStoreOwnershipPreflight_CapsOffendingPathsAtThree(t *testing.T) {
	writeFakeBr(t, 1)
	dir := foreignBeadsStore(t, ".br_history", "beads.db", "beads.db-wal", "issues.jsonl")
	o, sink := newOwnershipTestOverseer(t)

	o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, dir)
	path := filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	data, _ := os.ReadFile(path)
	detail := string(data)
	if strings.Count(detail, ": uid ") > 3 {
		t.Fatalf("detail must cap offending paths at 3, got: %s", detail)
	}
	if strings.Contains(detail, ".beads/issues.jsonl") {
		t.Fatalf("4th offending path must be omitted, got: %s", detail)
	}
}

func TestSpawnPreflightStoreOwnership_MatchingOwnerPasses(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, ".beads"))
	owner := int(st.Sys().(*syscall.Stat_t).Uid)
	oldUID := daemonUID
	daemonUID = func() int { return owner }
	defer func() { daemonUID = oldUID }()

	o := &Overseer{}
	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, dir); err != nil {
		t.Fatalf("matching-uid store must pass, got %v", err)
	}
}
