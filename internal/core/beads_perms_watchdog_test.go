package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// captureSink records events emitted by the watchdog.
type watchdogCaptureSink struct {
	events []Event
}

func (c *watchdogCaptureSink) EmitRepo(_ string, ev Event) Event {
	c.events = append(c.events, ev)
	return ev
}

// fakeProject returns a single-repo project rooted at dir.
func fakeProject(dir string) func() []types.Project {
	return func() []types.Project {
		return []types.Project{{Name: "proj", Path: dir, Type: types.RepoTypeSingle}}
	}
}

// makeStore creates a .beads store with issues.jsonl at the given mode.
func makeStore(t *testing.T, repoDir string, jsonlMode os.FileMode) string {
	t.Helper()
	beadsDir := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), jsonlMode); err != nil {
		t.Fatal(err)
	}
	return beadsDir
}

// TestWatchdog_RepairedStoreNoEvent: a drifted store the daemon CAN repair is
// fixed in-tick and emits nothing (repair succeeds, conforming again).
func TestWatchdog_RepairedStoreNoEvent(t *testing.T) {
	if beadsGroupGID < 0 {
		t.Skip("no beads group on this host")
	}
	repo := t.TempDir()
	makeStore(t, repo, 0o600) // non-conforming: issues.jsonl 0600, dir not setgid
	sink := &watchdogCaptureSink{}
	w := NewBeadsPermsWatchdog(fakeProject(repo), time.Minute, sink)
	fi, _ := os.Stat(filepath.Join(repo, ".beads"))
	if beadsPermsConforming(filepath.Join(repo, ".beads"), fi) {
		t.Fatal("precondition: store must start non-conforming")
	}
	w.checkStore("proj", repo)
	fi, _ = os.Stat(filepath.Join(repo, ".beads"))
	if !beadsPermsConforming(filepath.Join(repo, ".beads"), fi) {
		t.Fatalf("store not repaired: mode %v", fi.Mode())
	}
	if len(sink.events) != 0 {
		t.Fatalf("expected no events, got %v", sink.events)
	}
}

// TestWatchdog_LockedOutStoreEmitsTypedEventThrottled: when the repair cannot
// be applied (owner locked the store), the watchdog emits exactly one
// beads_store_locked event naming the root-side remedy, and throttles repeats
// within the 10m window.
func TestWatchdog_LockedOutStoreEmitsTypedEventThrottled(t *testing.T) {
	repo := t.TempDir()
	makeStore(t, repo, 0o600)
	sink := &watchdogCaptureSink{}
	w := NewBeadsPermsWatchdog(fakeProject(repo), time.Minute, sink)
	calls := 0
	w.repair = func(string) error { calls++; return os.ErrPermission }
	w.checkStore("proj", repo)
	if calls != 1 {
		t.Fatalf("repair called %d times, want 1", calls)
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d: %v", len(sink.events), sink.events)
	}
	ev := sink.events[0]
	if ev.Type != beadsStoreLockedEvent {
		t.Fatalf("event type %q, want %q", ev.Type, beadsStoreLockedEvent)
	}
	if ev.Reason != "owner_locked" {
		t.Fatalf("reason %q, want owner_locked", ev.Reason)
	}
	if ev.Detail == "" || !strings.Contains(ev.Detail, "root") {
		t.Fatalf("detail missing root-side remedy: %q", ev.Detail)
	}
	// Throttle: second tick within window emits nothing.
	w.checkStore("proj", repo)
	if len(sink.events) != 1 {
		t.Fatalf("throttle failed: %d events after second check", len(sink.events))
	}
	// Expiry: after the window the event re-fires (lockout persists).
	w.now = func() time.Time { return time.Now().Add(beadsStoreLockedThrottle + time.Minute) }
	w.checkStore("proj", repo)
	if len(sink.events) != 2 {
		t.Fatalf("expected re-fire after TTL, got %d events", len(sink.events))
	}
}

// TestWatchdog_ConformingStoreUntouched: a conforming store costs stats only —
// no repair call, no event, mode unchanged.
func TestWatchdog_ConformingStoreUntouched(t *testing.T) {
	if beadsGroupGID < 0 {
		t.Skip("no beads group on this host")
	}
	repo := t.TempDir()
	beadsDir := makeStore(t, repo, 0o660)
	// Apply the convention via the real onboarding, making it conforming.
	if err := onboardBeadsPerms(repo); err != nil {
		t.Fatalf("onboard: %v", err)
	}
	sink := &watchdogCaptureSink{}
	w := NewBeadsPermsWatchdog(fakeProject(repo), time.Minute, sink)
	w.repair = func(string) error { t.Fatal("repair must not be called on a conforming store"); return nil }
	w.checkStore("proj", repo)
	if len(sink.events) != 0 {
		t.Fatalf("unexpected events: %v", sink.events)
	}
	fi, _ := os.Stat(filepath.Join(beadsDir, "issues.jsonl"))
	if fi.Mode().Perm() != 0o660 {
		t.Fatalf("conforming file mode changed: %v", fi.Mode().Perm())
	}
}

// TestWatchdog_DisabledAndMissingStore: interval<=0 disables the scan; a repo
// without .beads is skipped silently.
func TestWatchdog_DisabledAndMissingStore(t *testing.T) {
	repo := t.TempDir()
	sink := &watchdogCaptureSink{}
	w := NewBeadsPermsWatchdog(fakeProject(repo), 0, sink)
	w.repair = func(string) error { t.Fatal("must not run when disabled"); return nil }
	w.scan() // disabled: no-op
	// missing store
	w2 := NewBeadsPermsWatchdog(fakeProject(repo), time.Minute, sink)
	w2.repair = func(string) error { t.Fatal("must not run without a store"); return nil }
	w2.checkStore("proj", repo)
}

// TestWatchdog_MultiRepoUsesRepoDirs: multi projects are checked per repo dir.
func TestWatchdog_MultiRepoUsesRepoDirs(t *testing.T) {
	root := t.TempDir()
	repoA := filepath.Join(root, "a")
	repoB := filepath.Join(root, "b")
	makeStore(t, repoA, 0o600)
	makeStore(t, repoB, 0o600)
	sink := &watchdogCaptureSink{}
	w := NewBeadsPermsWatchdog(func() []types.Project {
		return []types.Project{{
			Name: "proj", Path: root, Type: types.RepoTypeMulti,
			Repos: []types.Repo{{Name: "a", Path: "a"}, {Name: "b", Path: "b"}},
		}}
	}, time.Minute, sink)
	calls := 0
	w.repair = func(repoDir string) error { calls++; return os.ErrPermission }
	w.scan()
	if calls != 2 {
		t.Fatalf("repair called %d times for 2 repos, want 2", calls)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 events (one per repo), got %d", len(sink.events))
	}
}
