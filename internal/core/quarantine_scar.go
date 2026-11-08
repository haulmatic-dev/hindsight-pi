package core

// quarantine_scar.go — FR-001 "Completion-time scar scrub in the chain
// worktree" (quarantine-truth PRD, docs/PRDs/quarantine-truth-prd-20261108.md).
//
// RED-phase inert stub. scarScrubImplemented=false keeps every contract test
// in quarantine_scar_test.go skipping loudly by default; set
// QUARANTINE_SCAR_ENFORCE=1 to fail loudly when the behavior is still missing.
// GREEN replaces these bodies with the real scrub: async (the completion
// detector's callback must return immediately), junk-safe dirty-worktree
// commit, member verification or shared-suite fallback, re-decide, then
// integrate-or-quarantine — with an in-memory marker cleared via defer+recover
// and 3h wall-clock cap, plus restart recovery for persisted
// scrub-in-progress registry entries.

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/junksig"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

const (
	// scrubInProgressStatus is the registry Status stamped when the
	// completion callback hands the chain to the scrub goroutine, so a
	// daemon restart mid-scrub knows a decision was in flight.
	scrubInProgressStatus = "scrub-in-progress"

	// scarScrubFailedEvent is emitted when the chain scrubbed to green but
	// the post-scrub choke gate failed at completion time (triage parity
	// with the M1 label_revalidated event).
	scarScrubFailedEvent = "scar_scrub_failed"

	// scarScrubFailedReason marks the post-scrub choke-gate failure path.
	scarScrubFailedReason = "suite_gate_post_scrub"

	// scarScrubRevalidatedReason marks the scrub-scrubbed-the-verdict path.
	scarScrubRevalidatedReason = "completion_scrub"

	// closedFailedSuiteLabel re-stamps a chain whose post-scrub choke gate
	// failed: scrub did not overturn the verdict.
	closedFailedSuiteLabel = "closed-failed-suite"

	// scarScrubWallClockCap bounds one scrub (marker-releasable: the cap is
	// enforced by the scrub goroutine, not a lock).
	scarScrubWallClockCap = 3 * time.Hour
)

// scarScrubImplemented gates the RED-phase tests (GREEN: implemented).
var scarScrubImplemented = true

// scarDecision is the outcome of one completion-time scrub pass.
type scarDecision string

const (
	scarIntegrated  scarDecision = "integrated"
	scarQuarantined scarDecision = "quarantined"
	scarSkipped     scarDecision = "skipped"
)

// scrubHooks are the scrub's dependency seams. Production builds wire them
// from the overseer (RunVerificationGateForRevalidation, gate_close paths,
// worktree manager integrate/quarantine); tests inject recorders.
type scrubHooks struct {
	// worktreeBranchOf resolves the chain worktree branch for the key
	// (hydra/wt-<key>, versioned variants); "" when none.
	worktreeBranchOf func(projectPath, key string) string

	// taints reports whether any chain member still carries closed-failed,
	// and which bead carries it.
	taints func(beadIDs []types.BeadID) (failed bool, stained string)

	// verify reruns a member's verification; ran=false means nothing to run
	// (no declared command) — the shared suite gate + foreign-failure
	// attribution must then provide the positive evidence.
	verify func(bead *types.Bead, projectPath, worktreePath string) (verified, ran bool)

	// dirtyPaths lists uncommitted worktree-relative changes.
	dirtyPaths func(worktreePath string) ([]string, error)

	// commitPaths commits the given paths (junk pre-filtered by the caller).
	commitPaths func(projectPath, worktreePath string, paths []string) error

	// suiteGate runs the post-scrub choke gate; nil error = green.
	suiteGate func(projectPath, worktreePath, chainKey string) error

	// fallbackForeign is the shared-gate fallback attribution: (all failing
	// packages foreign, attribution was checked).
	fallbackForeign func() (ok, checked bool)

	// integrate / quarantine close the verdict.
	integrate  func(projectPath, key, branch string) error
	quarantine func(projectPath, key, branch string) (string, error)

	addLabels    func(projectPath string, id types.BeadID, labels []string) error
	removeLabels func(projectPath string, id types.BeadID, labels []string) error

	// branchExists resolves whether a chain worktree branch still exists
	// (restart recovery).
	branchExists func(projectPath, branch string) bool
}

// defaultScrubHooks is the SHARED hook set: the daemon wires it once at
// startup (wireScarScrubHooks) and the completion callback's launched
// goroutine consults whatever is wired. Tests grab this same instance and
// override fields (newScarFixture), which is what makes the launch-path test
// see the fixture's hooks without any extra seam.
var defaultScrubHookSet = &scrubHooks{}

func defaultScrubHooks() *scrubHooks { return defaultScrubHookSet }

// scarScrubPanicFallback runs when a scrub goroutine panics: production
// quarantines the chain so the work is preserved. Tests leave it nil.
var scarScrubPanicFallback func(o *Overseer, key string)

// scarJunkPath classifies a dirty path as junk (junksig-flagged): junk is
// left uncommitted by the pre-verdict commit. Seam for tests.
var scarJunkPath = junksig.IsJunkPath

// chainScarScrubber performs the scrub → re-decide → quarantine-or-integrate
// pass for one chain completion event.
type chainScarScrubber struct {
	o      *Overseer
	events *EventSink
	hooks  *scrubHooks
	cap    time.Duration // wall-clock cap; <=0 means scarScrubWallClockCap
}

func newChainScarScrubber(o *Overseer, events *EventSink, hooks *scrubHooks) *chainScarScrubber {
	if hooks == nil {
		hooks = defaultScrubHooks()
	}
	return &chainScarScrubber{o: o, events: events, hooks: hooks, cap: scarScrubWallClockCap}
}

// run performs one scrub pass: junk-safe pre-verdict commit, member
// verification (or shared-suite fallback), re-decide, then integrate through
// the gated path — or quarantine, with the closed-failed verdict re-stamped
// before the beads store can ever see a scrubbed-but-failing chain as clean.
func (s *chainScarScrubber) run(event ChainCompletedEvent, branch string) scarDecision {
	ci := event.ChainInfo
	if s == nil || s.hooks == nil || ci == nil {
		return s.quarantineNow(event, branch)
	}
	project := ci.ProjectPath
	key := ci.WorktreeKey
	if key == "" {
		key = ci.RootBeadID.String()
	}
	if branch == "" && s.hooks.worktreeBranchOf != nil {
		branch = s.hooks.worktreeBranchOf(project, key)
	}
	wt := NewWorktreeManager().worktreePath(project, key)
	start := time.Now()
	capped := func() bool {
		lim := s.cap
		if lim <= 0 {
			lim = scarScrubWallClockCap
		}
		return time.Since(start) > lim
	}

	// AC2: commit any dirty chain-worktree changes BEFORE any verdict runs,
	// excluding junksig-flagged paths. A commit failure skips the scrub
	// entirely (verification must never re-run atop uncommitted work) and
	// quarantines.
	if s.hooks.dirtyPaths != nil {
		paths, err := s.hooks.dirtyPaths(wt)
		if err != nil {
			log.Printf("[ScarScrub] chain %s: dirty-path probe failed (%v) — quarantining", key, err)
			return s.quarantineNow(event, branch)
		}
		keep := make([]string, 0, len(paths))
		for _, p := range paths {
			if !scarJunkPath(p) {
				keep = append(keep, p)
			}
		}
		if len(keep) > 0 && s.hooks.commitPaths != nil {
			if err := s.hooks.commitPaths(project, wt, keep); err != nil {
				log.Printf("[ScarScrub] chain %s: pre-verdict commit failed (%v) — skipping scrub, quarantining", key, err)
				return s.quarantineNow(event, branch)
			}
		}
	}

	failed, stained := false, ""
	if s.hooks.taints != nil {
		failed, stained = s.hooks.taints(ci.AllBeadIDs)
	}
	if !failed {
		log.Printf("[ScarScrub] chain %s: no closed-failed taint — nothing to scrub", key)
		return scarSkipped
	}
	bead := &types.Bead{ID: types.BeadID(stained)}

	scrubbed := false
	if s.hooks.verify != nil {
		verified, ran := s.hooks.verify(bead, project, wt)
		if capped() {
			log.Printf("[ScarScrub] chain %s: wall-clock cap hit during verification — quarantining", key)
			return s.quarantineNow(event, branch)
		}
		switch {
		case ran && verified:
			// Positive green evidence overturns the stale verdict (fail-closed:
			// anything else leaves labels untouched).
			s.scrubVerdict(project, bead)
			scrubbed = true
		case !ran:
			// No declared verification command: the shared suite gate plus
			// foreign-failure attribution must provide the evidence.
			if s.hooks.suiteGate != nil {
				if err := s.hooks.suiteGate(project, wt, key); err != nil {
					return s.restampQuarantine(event, branch, project, bead)
				}
				if ok, checked := s.hooks.fallbackForeign(); checked && ok {
					s.scrubVerdict(project, bead)
					scrubbed = true
				}
			}
		default:
			// ran && !verified: the rerun is red — verdict stands, no scrub.
			log.Printf("[ScarScrub] chain %s: member %s rerun red — verdict stands", key, stained)
		}
	}
	if capped() {
		log.Printf("[ScarScrub] chain %s: wall-clock cap hit — quarantining", key)
		return s.quarantineNow(event, branch)
	}
	if !scrubbed {
		return s.quarantineNow(event, branch)
	}

	// Post-scrub choke gate (AC4): the re-stamp happens BEFORE the decision is
	// recorded, so the beads store never shows a scrubbed chain as clean while
	// its integration is actually blocked.
	if s.hooks.suiteGate != nil {
		if err := s.hooks.suiteGate(project, wt, key); err != nil {
			return s.restampQuarantine(event, branch, project, bead)
		}
	}
	if s.hooks.integrate != nil {
		if err := s.hooks.integrate(project, key, branch); err != nil {
			log.Printf("[ScarScrub] chain %s: post-scrub integrate failed (%v) — quarantining", key, err)
			return s.quarantineNow(event, branch)
		}
	}
	log.Printf("[ScarScrub] chain %s: scrubbed to green — integrated via gated path", key)
	return scarIntegrated
}

// scrubVerdict removes the closed-failed label and stamps
// closed-failed-revalidated with the M1 (label_revalidated) cooldown semantics.
func (s *chainScarScrubber) scrubVerdict(project string, bead *types.Bead) {
	if s.hooks.removeLabels != nil {
		if err := s.hooks.removeLabels(project, bead.ID, []string{closedFailedLabel}); err != nil {
			log.Printf("[ScarScrub] %s: failed to scrub %s label: %v", bead.ID, closedFailedLabel, err)
		}
	}
	if s.hooks.addLabels != nil {
		if err := s.hooks.addLabels(project, bead.ID, []string{closedFailedRevalidatedLabel}); err != nil {
			log.Printf("[ScarScrub] %s: failed to add %s label: %v", bead.ID, closedFailedRevalidatedLabel, err)
		}
	}
	if s.events != nil {
		s.events.EmitRepo(project, Event{Type: "label_revalidated", Bead: bead.ID.String(),
			Reason: scarScrubRevalidatedReason,
			Detail: fmt.Sprintf("chain scrub: green re-run overturned the stale %s verdict", closedFailedLabel),
			Fields: map[string]any{"by": "completion_scrub"}})
	}
}

// restampQuarantine re-stamps closed-failed + closed-failed-suite in one
// pass and emits scar_scrub_failed (the triage-parity counterpart of
// label_revalidated), then returns the quarantine decision.
func (s *chainScarScrubber) restampQuarantine(event ChainCompletedEvent, branch, project string, bead *types.Bead) scarDecision {
	if s.hooks.addLabels != nil {
		if err := s.hooks.addLabels(project, bead.ID, []string{closedFailedLabel, closedFailedSuiteLabel}); err != nil {
			log.Printf("[ScarScrub] %s: re-stamp failed: %v", bead.ID, err)
		}
	}
	if s.events != nil {
		s.events.EmitRepo(project, Event{Type: scarScrubFailedEvent, Bead: bead.ID.String(),
			Reason: scarScrubFailedReason,
			Detail: "post-scrub choke gate failed — verdict re-stamped before Record, chain quarantined"})
	}
	return s.quarantineNow(event, branch)
}

// quarantineNow moves the chain worktree branch to hydra/quarantine/<key>
// through the hook (production: QuarantineBranchAndRemove + reconciler
// Record + chain_quarantined event) and returns the quarantine decision.
func (s *chainScarScrubber) quarantineNow(event ChainCompletedEvent, branch string) scarDecision {
	if s != nil && s.hooks != nil && s.hooks.quarantine != nil {
		ci := event.ChainInfo
		if ci != nil {
			project := ci.ProjectPath
			key := ci.WorktreeKey
			if key == "" {
				key = ci.RootBeadID.String()
			}
			if branch == "" && s.hooks.worktreeBranchOf != nil {
				branch = s.hooks.worktreeBranchOf(project, key)
			}
			if _, err := s.hooks.quarantine(project, key, branch); err != nil {
				log.Printf("[ScarScrub] chain %s: quarantine failed: %v", key, err)
			}
		}
	}
	return scarQuarantined
}

// scarScrubMarkers is the in-flight marker set, keyed by chain worktree key.
var scarScrubMarkers sync.Map

// scarMarkerGuarded runs fn with the chain key's scrub marker set; the marker
// is cleared via defer and a panic is recovered into a quarantine decision
// (fail-open to quarantine — never a lost chain, never a leaked marker).
func scarMarkerGuarded(o *Overseer, key string, fn func() scarDecision) (decision scarDecision) {
	scarScrubMarkers.Store(key, true)
	defer func() {
		scarScrubMarkers.Delete(key)
		if r := recover(); r != nil {
			log.Printf("[ScarScrub] chain %s: scrub panicked, failing open to quarantine: %v", key, r)
			decision = scarQuarantined
			if scarScrubPanicFallback != nil {
				scarScrubPanicFallback(o, key)
			}
		}
	}()
	return fn()
}

// scarScrubActive reports whether a scrub goroutine is currently in flight
// for the chain key (duplicate-completion guard).
func scarScrubActive(o *Overseer, key string) bool {
	_, ok := scarScrubMarkers.Load(key)
	return ok
}

// bossEvents resolves the event sink for a launch-time scrubber.
func bossEvents(o *Overseer) *EventSink {
	if o == nil {
		return nil
	}
	return o.eventSink
}

// launchScarScrub is the completion-callback entry point: returns
// immediately after marking the chain in flight, launching the scrub
// goroutine under scarMarkerGuarded. The marker is set synchronously so the
// scrub body (and the duplicate-completion guard) always observes it.
// launchScarScrub is the completion-callback entry point: returns
// immediately after marking the chain in flight, launching the scrub
// goroutine under scarMarkerGuarded. The marker is set synchronously so the
// Unwired hooks (no production taints probe / panic fallback installed) mean
// the overseer never went through wireScarScrubHooks — fail closed to the
// legacy immediate quarantine so a failed chain never silently integrates.
func launchScarScrub(o *Overseer, event ChainCompletedEvent, branch string) {
	ci := event.ChainInfo
	if ci == nil || ci.ProjectPath == "" {
		return
	}
	if o != nil && (defaultScrubHooks().taints == nil || scarScrubPanicFallback == nil) {
		scarLegacyQuarantine(o, event, branch)
		return
	}
	key := ci.WorktreeKey
	if key == "" {
		key = ci.RootBeadID.String()
	}
	if scarScrubActive(nil, key) {
		return // a scrub is already in flight for this chain
	}
	scarScrubMarkers.Store(key, true)
	go func() {
		dec := scarMarkerGuarded(o, key, func() scarDecision {
			return newChainScarScrubber(o, bossEvents(o), defaultScrubHooks()).run(event, branch)
		})
		scarAfterScrub(o, event, dec)
	}()
}

// scarLegacyQuarantine is the pre-scrub completion behavior kept as the
// fail-closed fallback when the scrub hooks are not wired: quarantine the
// failed chain immediately.
func scarLegacyQuarantine(o *Overseer, event ChainCompletedEvent, branch string) {
	ci := event.ChainInfo
	if o == nil || ci == nil {
		return
	}
	project, key := ci.ProjectPath, ci.WorktreeKey
	if key == "" {
		key = ci.RootBeadID.String()
	}
	if o.worktreeManager == nil {
		log.Printf("[Worktree] chain %s: worktree manager unavailable, quarantine skipped", key)
		return
	}
	if qBranch, qerr := o.worktreeManager.QuarantineBranchAndRemove(project, key, branch); qerr != nil {
		log.Printf("[Worktree] quarantine failed for chain %s: %v", ci.RootBeadID, qerr)
	} else if qBranch != "" {
		log.Printf("[Worktree] quarantined failed chain %s on %s (not integrated)", ci.RootBeadID, qBranch)
		if o.quarantineReconciler != nil {
			o.quarantineReconciler.Record(project, filepath.Base(project), key, qBranch, ci.AllBeadIDs)
		}
		if o.beadCloser != nil && o.beadCloser.events != nil {
			o.beadCloser.events.EmitRepo(project, Event{Type: "chain_quarantined",
				Bead: key, Reason: "failed_beads_in_chain",
				Fields: map[string]any{"branch": qBranch}})
		}
	} else {
		log.Printf("[Worktree] chain %s: quarantine empty — no worktree or branch in %s (nothing preserved)", key, project)
	}
}

// scarAfterScrub handles the decisions the scrub pass left to the launch
// path: a skipped scrub (no closed-failed taint left) still owes the chain a
// gated integration, and a quarantine decision owes the registry a clear of
// the scrub-in-progress status (already handled by the quarantine hook).
func scarAfterScrub(o *Overseer, event ChainCompletedEvent, dec scarDecision) {
	ci := event.ChainInfo
	if o == nil || ci == nil {
		return
	}
	project := ci.ProjectPath
	key := ci.WorktreeKey
	if key == "" {
		key = ci.RootBeadID.String()
	}
	if dec == scarSkipped && o.worktreeManager != nil {
		branch := ci.WorktreeBranch
		if branch == "" {
			branch = scrubWorktreeBranch(project, key)
		}
		if err := o.worktreeManager.IntegrateBranchAndRemove(project, key, branch); err != nil {
			log.Printf("[ScarScrub] chain %s: post-skip integrate failed: %v", key, err)
			if _, qerr := o.worktreeManager.QuarantineBranchAndRemove(project, key, branch); qerr != nil {
				log.Printf("[ScarScrub] chain %s: post-skip quarantine also failed: %v", key, qerr)
			}
		}
	}
}

// scrubRegistryEntry is the quarantine registry row extended with the FR-001
// scrub status (GREEN folds a Status field into QuarantinedChainEntry so the
// persisted registry itself can carry scrub-in-progress).
type scrubRegistryEntry struct {
	QuarantinedChainEntry
	// Status: scrubInProgressStatus when a completion callback handed the
	// chain to the scrub goroutine and the daemon died mid-run.
	Status string
}

// recoverScrubInProgress re-runs the decision for a persisted
// scrub-in-progress registry entry after a daemon restart: the decision
// re-runs only if the chain worktree branch still exists (probing
// hydra/wt-<key>, NOT the quarantine branch — the scrub operates on the
// chain worktree before any verdict moved the branch); otherwise the entry
// is dropped. Synchronous on purpose: tests assert the outcome directly, and
// production wraps the call in a goroutine from reconcileEntryLocked.
func recoverScrubInProgress(r *QuarantineReconciler, e *scrubRegistryEntry, hooks *scrubHooks) {
	if r == nil || e == nil {
		return
	}
	h := hooks
	if h == nil {
		h = defaultScrubHooks()
	}
	regKey := registryKey(e.ProjectPath, e.Branch)
	alive := false
	if h.branchExists != nil {
		alive = h.branchExists(e.ProjectPath, e.Branch)
	}
	if !alive {
		r.mu.Lock()
		delete(r.entries, regKey)
		_ = r.saveLocked()
		r.mu.Unlock()
		log.Printf("[ScarScrub] chain %s: branch %s gone after restart — dropping scrub-in-progress entry", e.Key, e.Branch)
		return
	}
	event := ChainCompletedEvent{ChainInfo: &ChainInfo{
		RootBeadID:     types.BeadID(e.Key),
		AllBeadIDs:     beadIDsFromStrings(e.MemberIDs),
		ProjectPath:    e.ProjectPath,
		WorktreeKey:    e.Key,
		WorktreeBranch: e.Branch,
	}}
	dec := newChainScarScrubber(nil, r.events, h).run(event, e.Branch)
	r.mu.Lock()
	switch dec {
	case scarIntegrated:
		delete(r.entries, regKey)
	default:
		if q := r.entries[regKey]; q != nil {
			q.Status = "" // back to a normal quarantined entry
		}
	}
	_ = r.saveLocked()
	r.mu.Unlock()
}

// MarkScrubInProgress stamps (or creates) the registry entry for the chain
// worktree key with scrub-in-progress and persists it, so a daemon restart
// mid-scrub re-runs the decision instead of losing it.
func (r *QuarantineReconciler) MarkScrubInProgress(projectPath, repoName, key, branch string, memberIDs []types.BeadID) {
	if r == nil || branch == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked()
	k := registryKey(projectPath, branch)
	e := r.entries[k]
	if e == nil {
		e = &QuarantinedChainEntry{
			ProjectPath: projectPath, RepoName: repoName, Key: key,
			Branch: branch, MemberIDs: beadIDStrings(memberIDs), QuarantinedAt: time.Now(),
		}
		r.entries[k] = e
	}
	e.Status = scrubInProgressStatus
	if err := r.saveLocked(); err != nil {
		log.Printf("[ScarScrub] chain %s: failed to persist scrub-in-progress status: %v", key, err)
	}
}

// beadIDsFromStrings converts registry member IDs to typed bead IDs.
func beadIDsFromStrings(ids []string) []types.BeadID {
	out := make([]types.BeadID, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			out = append(out, types.BeadID(id))
		}
	}
	return out
}

// beadIDStrings converts typed bead IDs to registry member IDs.
func beadIDStrings(ids []types.BeadID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// wireScarScrubHooks installs the production scrub hooks (shared default
// set). Built from the overseer: the revalidation gate, gate_close suite
// machinery, worktree manager integrate/quarantine, and the beads client.
func wireScarScrubHooks(o *Overseer) {
	if o == nil {
		return
	}
	h := defaultScrubHooks()
	h.taints = func(beadIDs []types.BeadID) (bool, string) {
		return chainHasFailedBeadIDs(o.beadFileReader, beadIDStrings(beadIDs))
	}
	h.verify = func(bead *types.Bead, projectPath, worktreePath string) (bool, bool) {
		if bead == nil || o.beadCloser == nil {
			return false, false
		}
		// Resolve the REAL bead (the scrubber only carries the ID) so the
		// declared verification command is available.
		if o.beadFileReader != nil {
			if real := o.beadFileReader.FindByID(bead.ID.String()); real != nil {
				bead = real
			}
		}
		return o.beadCloser.RunVerificationGateForRevalidation(bead, projectPath, worktreePath)
	}
	h.dirtyPaths = func(worktreePath string) ([]string, error) {
		out, err := gitOut(worktreePath, "status", "--porcelain")
		if err != nil {
			return nil, err
		}
		var paths []string
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !strings.Contains(line, " ") {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			path := strings.TrimSpace(parts[1])
			if strings.Contains(path, " -> ") { // renames: track the new path
				path = strings.TrimSpace(strings.SplitN(path, "->", 2)[1])
			}
			if path != "" {
				paths = append(paths, path)
			}
		}
		return paths, nil
	}
	h.commitPaths = func(projectPath, worktreePath string, paths []string) error {
		args := append([]string{"commit", "--only", "-m",
			fmt.Sprintf("chain scrub: preserve uncommitted chain work (junk-safe, %s)", time.Now().UTC().Format("2006-01-02T15:04:05Z")),
			"--"}, paths...)
		if _, err := gitOut(worktreePath, args...); err != nil {
			return fmt.Errorf("junk-safe pre-verdict commit in %s: %w", worktreePath, err)
		}
		return nil
	}
	// fallbackForeign seam carries no arguments, so the production suiteGate
	// closure remembers the probe context it was last called with; with two
	// chains scrubbing concurrently the attribution can read the other chain's
	// probe — accepted for GREEN (wrong = fail-closed: verdict stands), and the
	// declared-command path dominates in production.
	h.suiteGate = func(projectPath, worktreePath, chainKey string) error {
		scarProbeMu.Lock()
		scarProbeLast = scarProbeCtx{projectPath: projectPath, worktreePath: worktreePath, chainKey: chainKey}
		scarProbeMu.Unlock()
		m := o.worktreeManager
		if m == nil || m.SuiteGate == nil {
			return nil // no daemon-owned gate wired — fail open
		}
		return m.SuiteGate(projectPath, worktreePath, chainKey)
	}
	h.fallbackForeign = func() (bool, bool) { return scarFallbackForeign(o) }
	h.integrate = func(projectPath, key, branch string) error {
		if o.worktreeManager == nil {
			return fmt.Errorf("no worktree manager")
		}
		return o.worktreeManager.IntegrateBranchAndRemove(projectPath, key, branch)
	}
	h.quarantine = func(projectPath, key, branch string) (string, error) {
		qBranch, err := scarQuarantineChain(o, projectPath, key, branch)
		return qBranch, err
	}
	h.addLabels = func(projectPath string, id types.BeadID, labels []string) error {
		if o.beadsClient == nil {
			return fmt.Errorf("no beads client")
		}
		return o.beadsClient.UpdateBeadLabels(projectPath, "", id, labels)
	}
	h.removeLabels = func(projectPath string, id types.BeadID, labels []string) error {
		if o.beadsClient == nil {
			return fmt.Errorf("no beads client")
		}
		return o.beadsClient.RemoveBeadLabels(projectPath, "", id, labels)
	}
	h.branchExists = func(projectPath, branch string) bool {
		return branchExists(projectPath, branch)
	}
	h.worktreeBranchOf = func(projectPath, key string) string {
		return scrubWorktreeBranch(projectPath, key)
	}
	scarScrubPanicFallback = func(_ *Overseer, key string) {
		projectPath := o.findProjectPathForBeadID(types.BeadID(key))
		if projectPath == "" {
			return
		}
		branch := scrubWorktreeBranch(projectPath, key)
		if branch == "" {
			return
		}
		if _, err := scarQuarantineChain(o, projectPath, key, branch); err != nil {
			log.Printf("[ScarScrub] chain %s: panic fallback quarantine failed: %v", key, err)
		}
	}
}

// scarProbeCtx is the most recent suite-gate probe context (see the
// fallbackForeign seam comment on wireScarScrubHooks).
type scarProbeCtx struct{ projectPath, worktreePath, chainKey string }

var (
	scarProbeMu   sync.Mutex
	scarProbeLast scarProbeCtx
)

// scarFallbackForeign is the production shared-gate fallback attribution:
// rerun the daemon-owned full test suite in the chain worktree, parse the
// failing Go packages, and require every one of them to sit outside the
// chain worktree's own diff (checked=true once attribution actually ran).
func scarFallbackForeign(o *Overseer) (ok, checked bool) {
	c := o.beadCloser
	if c == nil || c.gateRunner == nil || c.config == nil {
		return false, false
	}
	scarProbeMu.Lock()
	probe := scarProbeLast
	scarProbeMu.Unlock()
	if probe.chainKey == "" || probe.projectPath == "" {
		return false, false
	}
	projectName := ""
	for i := range o.config.Projects {
		if o.config.Projects[i].Path == probe.projectPath {
			projectName = o.config.Projects[i].Name
		}
	}
	gates := c.gatesFor(projectName)
	if gates == nil || !gates.DaemonOwned || gates.Test.Cmd == "" {
		return false, false
	}
	a, err := c.gateRunner.RunGate(GateRequest{
		Project: projectName, BeadID: probe.chainKey, Gate: "chain-suite",
		Spec: gates.Test, ProjectPath: probe.projectPath, WorktreePath: probe.worktreePath,
	})
	if err != nil {
		return false, true // attribution ran but is inconclusive — fail-closed
	}
	if a.Status == GateStatusPassed {
		return true, true
	}
	failing := parseFailingGoPackages(a.Summary + "\n" + a.RawLog)
	if len(failing) == 0 {
		return false, true
	}
	changed, err := c.changedFilePaths(probe.worktreePath)
	if err != nil || len(changed) == 0 {
		return false, true
	}
	for _, pkg := range failing {
		pkgDir := goPackageToDir(pkg)
		for _, f := range changed {
			if strings.HasPrefix(f, pkgDir) {
				return false, true // a failing package is the chain's own
			}
		}
	}
	return true, true // all failing packages are foreign
}

// scarQuarantineChain is the production quarantine hook body: move the chain
// branch to hydra/quarantine/<key>, record it with the reconciler (so fix
// beads and revalidation see it), and emit the chain_quarantined event.
func scarQuarantineChain(o *Overseer, projectPath, key, branch string) (string, error) {
	if o.worktreeManager == nil {
		return "", fmt.Errorf("no worktree manager")
	}
	qBranch, err := o.worktreeManager.QuarantineBranchAndRemove(projectPath, key, branch)
	if err != nil {
		return "", err
	}
	if qBranch == "" {
		return "", nil // nothing to preserve (worktree and branch gone)
	}
	if o.quarantineReconciler != nil {
		o.quarantineReconciler.Record(projectPath, filepath.Base(projectPath), key, qBranch, nil)
	}
	if o.beadCloser != nil && o.beadCloser.events != nil {
		o.beadCloser.events.EmitRepo(projectPath, Event{Type: "chain_quarantined",
			Bead: key, Reason: "scar_scrub_quarantine",
			Fields: map[string]any{"branch": qBranch}})
	}
	return qBranch, nil
}

// scrubWorktreeBranch resolves the chain worktree branch name for the key
// (hydra/wt-<key>, versioned variants per resolveChainBranch); "" when none.
// Seam for tests.
var scrubWorktreeBranch = func(projectPath, key string) string {
	m := NewWorktreeManager()
	return resolveChainBranch(m, projectPath, key, "")
}
