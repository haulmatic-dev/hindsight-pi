package core

// Tests for FR-001 "Completion-time scar scrub in the chain worktree"
// (quarantine-truth PRD, docs/PRDs/quarantine-truth-prd-20261108.md).
//
// RED discipline: skip loudly when the scrub is not implemented yet
// (scarScrubImplemented=false); QUARANTINE_SCAR_ENFORCE=1 proves the RED by
// failing at the enforcement gate instead of skipping.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

const scarScrubEnforceEnv = "QUARANTINE_SCAR_ENFORCE"

// requireScarScrubImpl is the skip-loudly gate shared by every scar-scrub
// contract test.
func requireScarScrubImpl(t *testing.T) {
	t.Helper()
	if scarScrubImplemented {
		return
	}
	if os.Getenv(scarScrubEnforceEnv) == "1" {
		t.Fatalf("%s=1: completion-time scar scrub not implemented yet — RED at the enforcement gate", scarScrubEnforceEnv)
	}
	t.Skipf("scar scrub not implemented yet; set %s=1 to fail loudly (RED discipline)", scarScrubEnforceEnv)
}

// scarFixture carries the fake chain + recorder hooks one test needs.
type scarFixture struct {
	projectPath string
	dir         string
	event       ChainCompletedEvent
	events      *EventSink
	hooks       *scrubHooks

	mu           sync.Mutex
	committed    [][]string
	verified     []string
	removed      [][]string
	added        [][]string
	integratedN  int
	quarantinedN int
}

func newScarFixture(t *testing.T, key, memberID string, memberLabels []string) *scarFixture {
	t.Helper()

	dir, _ := setupReconcilerRepo(t, key, memberLabels)
	f := &scarFixture{
		projectPath: dir,
		dir:         dir,
		events:      NewEventSink(t.TempDir()),
		hooks:       defaultScrubHooks(),
	}
	f.event = ChainCompletedEvent{
		ChainInfo: &ChainInfo{
			RootBeadID:     types.BeadID(key),
			AllBeadIDs:     []types.BeadID{types.BeadID(key), types.BeadID(memberID)},
			ProjectPath:    dir,
			WorktreeKey:    key,
			WorktreeBranch: "hydra/wt-" + key,
		},
	}

	f.hooks.taints = func(ids []types.BeadID) (bool, string) {
		// The member's labels live in the beads store fixture; a member with
		// the closed-failed label is the stale-taint scenario.
		reader := NewBeadFileReader([]types.Project{{Name: "test", Path: f.projectPath}})
		if failed, who := chainHasFailedBeadIDs(reader, []string{memberID}); failed {
			return true, who
		}
		return false, ""
	}
	f.hooks.verify = func(bead *types.Bead, projectPath, worktreePath string) (bool, bool) {
		f.mu.Lock()
		f.verified = append(f.verified, string(bead.ID))
		f.mu.Unlock()
		return true, true // green rerun scrubs the stale verdict
	}
	f.hooks.dirtyPaths = func(string) ([]string, error) { return nil, nil }
	f.hooks.commitPaths = func(projectPath, worktreePath string, paths []string) error {
		f.mu.Lock()
		f.committed = append(f.committed, paths)
		f.mu.Unlock()
		return nil
	}
	f.hooks.suiteGate = func(projectPath, worktreePath, chainKey string) error { return nil }
	f.hooks.fallbackForeign = func() (bool, bool) { return false, false }
	f.hooks.integrate = func(projectPath, key, branch string) error {
		f.mu.Lock()
		f.integratedN++
		f.mu.Unlock()
		return nil
	}
	f.hooks.quarantine = func(projectPath, key, branch string) (string, error) {
		f.mu.Lock()
		f.quarantinedN++
		f.mu.Unlock()
		return "hydra/quarantine/" + key, nil
	}
	f.hooks.addLabels = func(projectPath string, id types.BeadID, labels []string) error {
		f.mu.Lock()
		f.added = append(f.added, labels)
		f.mu.Unlock()
		return nil
	}
	f.hooks.removeLabels = func(projectPath string, id types.BeadID, labels []string) error {
		f.mu.Lock()
		f.removed = append(f.removed, labels)
		f.mu.Unlock()
		return nil
	}
	f.hooks.branchExists = func(projectPath, branch string) bool {
		return branchExists(projectPath, branch)
	}
	return f
}

func (f *scarFixture) requireNoQuarantine(t *testing.T, why string) {
	t.Helper()
	if f.quarantinedN != 0 {
		t.Fatalf("%s: chain must NOT quarantine (quarantine hook called %d times)", why, f.quarantinedN)
	}
}

func (f *scarFixture) requireOneEvent(t *testing.T, evType, reason string) {
	t.Helper()
	got := readEventsOfType(t, f.events, evType)
	if len(got) != 1 {
		t.Fatalf("%s: expected exactly 1 %q event, got %d", reason, evType, len(got))
	}
	if got[0].Reason != reason {
		t.Fatalf("%s: %q event reason = %q, want %q", reason, evType, got[0].Reason, reason)
	}
}

// AC1: a chain whose only taint is a stale closed-failed verdict integrates
// through the gated completion path, not quarantine.
func TestScarScrub_StaleVerdictIntegratesThroughGatedPath(t *testing.T) {
	requireScarScrubImpl(t)

	t.Run("member rerun green scrubs label and integrates", func(t *testing.T) {
		requireScarScrubImpl(t)
		key := "test-scar-ac1-stale"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})

		s := newChainScarScrubber(nil, f.events, f.hooks)
		dec := s.run(f.event, f.event.ChainInfo.WorktreeBranch)

		if dec != scarIntegrated {
			t.Fatalf("decision = %q, want %q (stale-tainted members must scrub, not quarantine)", dec, scarIntegrated)
		}
		f.requireNoQuarantine(t, "AC1 stale-taint integration")
		if f.integratedN != 1 {
			t.Fatalf("AC1: IntegrateBranchAndRemove-called hook count = %d, want 1", f.integratedN)
		}

		// The green verification run scrubs the stale closed-failed verdict
		// with the M1 label pair under the revalidation cooldown semantics.
		f.mu.Lock()
		removed, added := f.removed, f.added
		f.mu.Unlock()
		if len(removed) != 1 || len(removed[0]) != 1 || removed[0][0] != closedFailedLabel {
			t.Fatalf("AC1: labels removed = %v, want exactly [%s] on the tainted member", removed, closedFailedLabel)
		}
		if len(added) != 1 || len(added[0]) != 1 || added[0][0] != closedFailedRevalidatedLabel {
			t.Fatalf("AC1: labels added = %v, want exactly [%s] on the scrubbed member", added, closedFailedRevalidatedLabel)
		}
		f.requireOneEvent(t, "label_revalidated", scarScrubRevalidatedReason)
	})

	t.Run("shared suite gate green with only-foreign failing packages scrubs", func(t *testing.T) {
		requireScarScrubImpl(t)
		key := "test-scar-ac1-fallback"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})

		// The member has no declared verification command: nothing to rerun.
		// The shared suite gate is green but reports one failing package,
		// which must be attributed as foreign before the scrub proceeds.
		f.hooks.verify = func(*types.Bead, string, string) (bool, bool) { return false, false }
		f.hooks.suiteGate = func(projectPath, worktreePath, chainKey string) error { return nil }
		f.hooks.fallbackForeign = func() (bool, bool) { return true, true }

		s := newChainScarScrubber(nil, f.events, f.hooks)
		dec := s.run(f.event, f.event.ChainInfo.WorktreeBranch)

		if dec != scarIntegrated {
			t.Fatalf("decision = %q, want %q (suite green + all-foreign failures must scrub)", dec, scarIntegrated)
		}
		f.requireNoQuarantine(t, "AC1 fallback integration")
	})
}

// AC2: a dirty chain worktree is committed junk-safe before the verdict;
// a commit failure skips the scrub and quarantines.
func TestScarScrub_DirtyWorktreeCommittedJunkSafe(t *testing.T) {
	requireScarScrubImpl(t)

	const junkName = "debug.log"
	prevJunk := scarJunkPath
	scarJunkPath = func(p string) bool {
		return strings.HasSuffix(p, junkName)
	}
	t.Cleanup(func() { scarJunkPath = prevJunk })

	t.Run("junk-flagged paths excluded from pre-verdict commit", func(t *testing.T) {
		requireScarScrubImpl(t)
		key := "test-scar-ac2-junk"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
		f.hooks.dirtyPaths = func(string) ([]string, error) {
			return []string{filepath.Join("internal", "app.go"), junkName}, nil
		}

		s := newChainScarScrubber(nil, f.events, f.hooks)
		dec := s.run(f.event, f.event.ChainInfo.WorktreeBranch)

		if dec != scarIntegrated {
			t.Fatalf("decision = %q, want %q", dec, scarIntegrated)
		}
		f.mu.Lock()
		committed := f.committed
		f.mu.Unlock()
		if len(committed) != 1 {
			t.Fatalf("AC2: pre-verdict commit hook called %d times, want 1", len(committed))
		}
		if len(committed[0]) != 1 || committed[0][0] != filepath.Join("internal", "app.go") {
			t.Fatalf("AC2: committed paths = %v, want only the non-junk path %q (junksig-flagged %q must stay uncommitted)", committed[0], filepath.Join("internal", "app.go"), junkName)
		}
	})

	t.Run("commit failure skips scrub and quarantines", func(t *testing.T) {
		requireScarScrubImpl(t)
		key := "test-scar-ac2-fail"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
		f.hooks.dirtyPaths = func(string) ([]string, error) { return []string{"app.go"}, nil }
		wantErr := errors.New("git add: exit status 128")
		f.hooks.commitPaths = func(projectPath, worktreePath string, paths []string) error { return wantErr }

		s := newChainScarScrubber(nil, f.events, f.hooks)
		dec := s.run(f.event, f.event.ChainInfo.WorktreeBranch)

		if dec != scarQuarantined {
			t.Fatalf("decision = %q, want %q (commit failure must never integrate uncommitted work)", dec, scarQuarantined)
		}
		f.mu.Lock()
		verified := f.verified
		f.mu.Unlock()
		if len(verified) != 0 {
			t.Fatalf("AC2: verification must be SKIPPED on commit failure, verified members = %v", verified)
		}
		if f.quarantinedN != 1 {
			t.Fatalf("AC2: quarantine hook count = %d, want 1", f.quarantinedN)
		}
	})
}

// AC3: a scrub that panics or hits the wall-clock cap releases the in-memory
// marker (defer + recover) and fails open to quarantine.
func TestScarScrub_PanicAndTimeoutFailOpenToQuarantine(t *testing.T) {
	requireScarScrubImpl(t)

	t.Run("panic recovered to quarantine, marker cleared", func(t *testing.T) {
		requireScarScrubImpl(t)
		key := "test-scar-ac3-panic"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
		f.hooks.verify = func(*types.Bead, string, string) (bool, bool) {
			panic("index out of range in scrub re-decide")
		}

		var dec scarDecision
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("AC3: panics must be recovered by the marker guard, leaked: %v", r)
				}
			}()
			dec = scarMarkerGuarded(nil, key, func() scarDecision {
				return newChainScarScrubber(nil, f.events, f.hooks).run(f.event, f.event.ChainInfo.WorktreeBranch)
			})
		}()

		if dec != scarQuarantined {
			t.Fatalf("AC3: decision after panic = %q, want %q (fail open to quarantine, never lose the chain)", dec, scarQuarantined)
		}
		if scarScrubActive(nil, key) {
			t.Fatal("AC3: in-memory scrub marker must be cleared after the panic")
		}
	})

	t.Run("wall-clock cap releases marker and quarantines", func(t *testing.T) {
		requireScarScrubImpl(t)
		if scarScrubWallClockCap != 3*time.Hour {
			t.Fatalf("AC3: scarScrubWallClockCap = %v, want 3h", scarScrubWallClockCap)
		}
		key := "test-scar-ac3-timeout"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
		f.hooks.verify = func(*types.Bead, string, string) (bool, bool) {
			time.Sleep(400 * time.Millisecond)
			return true, true
		}
		s := newChainScarScrubber(nil, f.events, f.hooks)
		s.cap = 50 * time.Millisecond

		var dec scarDecision
		dec = scarMarkerGuarded(nil, key, func() scarDecision {
			return s.run(f.event, f.event.ChainInfo.WorktreeBranch)
		})

		if dec != scarQuarantined {
			t.Fatalf("AC3: decision after cap = %q, want %q", dec, scarQuarantined)
		}
		if scarScrubActive(nil, key) {
			t.Fatal("AC3: marker must be released after the wall-clock cap")
		}
	})
}

// AC4: when the scrub turns the chain green but the post-scrub choke gate
// fails, the closed-failed + closed-failed-suite labels are re-stamped BEFORE
// Record() and a scar_scrub_failed event is emitted.
func TestScarScrub_PostScrubGateFailureRestampsAndEmits(t *testing.T) {
	requireScarScrubImpl(t)

	key := "test-scar-ac4-choke"
	f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
	f.hooks.suiteGate = func(projectPath, worktreePath, chainKey string) error {
		return errors.New("chain suite gate: full suite failed")
	}

	s := newChainScarScrubber(nil, f.events, f.hooks)
	dec := s.run(f.event, f.event.ChainInfo.WorktreeBranch)

	if dec != scarQuarantined {
		t.Fatalf("AC4: decision = %q, want %q (choke gate failed — quarantine, do not integrate)", dec, scarQuarantined)
	}
	if f.integratedN != 0 {
		t.Fatalf("AC4: integrate hook called %d times after a failed choke gate, want 0", f.integratedN)
	}

	// Re-stamp: closed-failed may have been scrubbed earlier in this pass —
	// both the verdict label and the suite-scar label must be present in one
	// re-stamp, so the beads store never shows an open-failed chain as clean.
	f.mu.Lock()
	added := f.added
	f.mu.Unlock()
	foundVerdict, foundSuite := false, false
	for _, labels := range added {
		for _, l := range labels {
			if l == closedFailedLabel {
				foundVerdict = true
			}
			if l == closedFailedSuiteLabel {
				foundSuite = true
			}
		}
	}
	if !foundVerdict || !foundSuite {
		t.Fatalf("AC4: labels added after choke-gate failure = %v, want both [%s] and [%s] re-stamped", added, closedFailedLabel, closedFailedSuiteLabel)
	}

	// scar_scrub_failed is the triage-parity counterpart to label_revalidated.
	f.requireOneEvent(t, scarScrubFailedEvent, scarScrubFailedReason)
}

// AC5: a daemon restart mid-scrub recovers from the persisted
// scrub-in-progress registry entry — the decision re-runs when the chain
// worktree branch still exists, and the entry is dropped when it does not.
func TestScarScrub_RestartRecoversScrubInProgress(t *testing.T) {
	requireScarScrubImpl(t)

	t.Run("branch exists — decision re-runs", func(t *testing.T) {
		requireScarScrubImpl(t)
		key := "test-scar-ac5-alive"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
		r := newTestReconciler(t, f.dir, nil)
		entry := &scrubRegistryEntry{
			Status: scrubInProgressStatus,
			QuarantinedChainEntry: QuarantinedChainEntry{
				ProjectPath:   f.dir,
				RepoName:      "test",
				Key:           key,
				Branch:        "hydra/wt-" + key,
				MemberIDs:     []string{key, "member-1"},
				QuarantinedAt: time.Now(),
			},
		}
		r.mu.Lock()
		r.entries[registryKey(entry.ProjectPath, entry.Branch)] = &entry.QuarantinedChainEntry
		r.mu.Unlock()

		f.hooks.branchExists = func(projectPath, branch string) bool {
			// Recovery probes the chain worktree branch, not the quarantine
			// branch — the scrub is operating on hydra/wt-<key> before any
			// verdict moved the branch.
			return strings.HasPrefix(branch, "hydra/wt-")
		}
		recoverScrubInProgress(r, entry, f.hooks)

		f.mu.Lock()
		verified := f.verified
		f.mu.Unlock()
		if len(verified) == 0 {
			t.Fatal("AC5: recovery with a live chain branch must re-run the decision (verification hook never ran)")
		}
	})

	t.Run("branch gone — entry dropped", func(t *testing.T) {
		requireScarScrubImpl(t)
		key := "test-scar-ac5-gone"
		f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
		r := newTestReconciler(t, f.dir, nil)
		entry := &scrubRegistryEntry{
			Status: scrubInProgressStatus,
			QuarantinedChainEntry: QuarantinedChainEntry{
				ProjectPath:   f.dir,
				RepoName:      "test",
				Key:           key,
				Branch:        "hydra/wt-" + key,
				MemberIDs:     []string{key, "member-1"},
				QuarantinedAt: time.Now(),
			},
		}
		regKey := registryKey(entry.ProjectPath, entry.Branch)
		r.mu.Lock()
		r.entries[regKey] = &entry.QuarantinedChainEntry
		r.mu.Unlock()

		f.hooks.branchExists = func(string, string) bool { return false }
		recoverScrubInProgress(r, entry, f.hooks)

		r.mu.Lock()
		_, stillThere := r.entries[regKey]
		r.mu.Unlock()
		if stillThere {
			t.Fatal("AC5: scrub-in-progress entry must be dropped when the chain branch no longer exists")
		}
	})
}

// Marker discipline across the launch path: launchScarScrub returns
// immediately (the detection loop deletes chain info inline) and the marker
// is visible only while the scrub goroutine runs.
func TestScarScrub_LaunchReturnsImmediatelyAndMarks(t *testing.T) {
	requireScarScrubImpl(t)

	key := "test-scar-launch"
	f := newScarFixture(t, key, "member-1", []string{closedFailedLabel})
	f.hooks.verify = func(bead *types.Bead, p, w string) (bool, bool) {
		// While the scrub body runs, the marker must be set.
		if !scarScrubActive(nil, key) {
			return false, false
		}
		f.mu.Lock()
		f.verified = append(f.verified, string(bead.ID))
		f.mu.Unlock()
		return true, true
	}

	launchScarScrub(nil, f.event, f.event.ChainInfo.WorktreeBranch)

	// The launch itself must not block for the scrub to finish.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if !scarScrubActive(nil, key) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("AC-scrub-launch: scrub marker never released after completion")
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	verified := f.verified
	f.mu.Unlock()
	if len(verified) == 0 {
		t.Fatal("scrub goroutine never ran the verification hook")
	}

	_ = fmt.Sprint() // keep fmt import stable for future assertions
}
