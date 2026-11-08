package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// QuarantineReconciler closes the loop the chain-completion verdict opens:
// a quarantined chain used to wait forever for a human to merge its branch
// (the srv-smart-engine-ilu incident, 2026-10-16: one stale closed-failed
// label on NFR-001 quarantined 132 beads of verified work; PRD validation
// had PASSED). The reconciler re-verdicts recorded quarantines every tick —
// when no member carries closed-failed anymore (e.g. the post-close sweep
// revalidated the stale label against a green gate rerun) it integrates the
// quarantine branch through the normal IntegrateBranch path, removes the
// branch, and emits an event. Fail-closed throughout: any doubt leaves the
// quarantine in place.
//
// Membership is recorded at quarantine time (the completion detector deletes
// chain info after firing, so it cannot be re-asked later). Entries predating
// the reconciler bootstrap from hydra/quarantine/* branch scans and
// reconstruct membership with the registrar's dependents walk.
type QuarantineReconciler struct {
	mu           sync.Mutex
	registryPath string
	worktreeMgr  *WorktreeManager
	reader       *BeadFileReader
	events       *EventSink
	projects     func() []types.Project
	interval     time.Duration
	maxAttempts  int
	entries      map[string]*QuarantinedChainEntry
	stopCh       chan struct{}
	wg           sync.WaitGroup

	// Revalidation (M1): tainted members get ONE gate rerun per
	// revalidationCooldown; only a green run scrubs closed-failed. nil gate
	// disables revalidation (the verdict then just waits for M3 close-scrubs).
	// worktreePath is a reconciler-created scratch checkout of the quarantine
	// branch tip — the chain worktree is gone by quarantine time, and the
	// main checkout lacks the chain's code, so neither can verify truthfully.
	revalidateGate func(bead *types.Bead, projectPath, worktreePath string) (passed, ran bool)
	removeLabels   func(projectPath string, beadID types.BeadID, labels []string) error
	addLabels      func(projectPath string, beadID types.BeadID, labels []string) error

	// Unverifiable-scar exit (GREEN): classified revalidation surface.
	// Inert when the classified gate is nil (the legacy (passed, ran) gate
	// keeps working, and the sweep/who wiring is skipped).
	classifiedGate   func(bead *types.Bead, projectPath, worktreePath string) RevalidateVerdict
	classifiedRemove func(projectPath string, beadID types.BeadID, labels []string) error
	classifiedAdd    func(projectPath string, beadID types.BeadID, labels []string) error
}

// revalidationCooldown bounds gate reruns per quarantine entry: a coverage
// gate can be a multi-GB jest army, so a 5-minute reconcile tick must not
// re-spend it on every pass.
const revalidationCooldown = 30 * time.Minute

// SetRevalidation wires the gate-rerun + label operations (from the bead
// closer / beads client). All three required; nil gate disables the feature.
func (r *QuarantineReconciler) SetRevalidation(gate func(bead *types.Bead, projectPath, worktreePath string) (passed, ran bool), remove, add func(projectPath string, beadID types.BeadID, labels []string) error) {
	r.revalidateGate = gate
	r.removeLabels = remove
	r.addLabels = add
}

// QuarantinedChainEntry is one recorded quarantine.
type QuarantinedChainEntry struct {
	ProjectPath string   `json:"project_path"`
	RepoName    string   `json:"repo_name"`
	Key         string   `json:"key"`    // chain key (root bead ID string)
	Branch      string   `json:"branch"` // hydra/quarantine/<key>
	MemberIDs   []string `json:"member_ids"`
	// RanFalseStreaks tracks consecutive no-command revalidation outcomes per
	// member (unverifiable-scar exit); it round-trips through this registry
	// file so the bound survives a daemon restart.
	RanFalseStreaks map[string]int `json:"ran_false_streaks,omitempty"`
	// UnverifiableAt anchors each stamped closed-failed-unverifiable verdict
	// to the recorded store's modification time at stamping: the per-tick
	// sweep treats a member whose store was rewritten afterwards without the
	// scar as "operator removed the scar" and re-enables re-verdicting.
	UnverifiableAt map[string]time.Time `json:"unverifiable_at,omitempty"`
	// lastRevalidateRan (unpersisted) records that the last classified
	// outcome actually ran the gate — the revalidationCooldown bounds only
	// real reruns; no-command outcomes never consume it.
	lastRevalidateRan bool
	QuarantinedAt     time.Time `json:"quarantined_at"`
	IntegrateAttempts int       `json:"integrate_attempts"`
	LastRevalidation  time.Time `json:"last_revalidation,omitempty"`
	// Status carries the quarantine-registry scrub state (scrub-in-progress
	// when a completion callback handed the chain to the scrub goroutine so
	// a restarted daemon can recover it); empty in normal entries.
	Status string `json:"status,omitempty"`
}

// NewQuarantineReconciler builds the reconciler. registryPath is
// $HYDRA_HOME/quarantined-chains.json; projects is re-read every reconcile so
// config reloads apply. A nil reader or worktree manager disables the
// reconciler (Start is a no-op).
func NewQuarantineReconciler(registryPath string, wm *WorktreeManager, reader *BeadFileReader, events *EventSink, projects func() []types.Project) *QuarantineReconciler {
	return &QuarantineReconciler{
		registryPath: registryPath,
		worktreeMgr:  wm,
		reader:       reader,
		events:       events,
		projects:     projects,
		interval:     5 * time.Minute,
		maxAttempts:  10,
		entries:      make(map[string]*QuarantinedChainEntry),
		stopCh:       make(chan struct{}),
	}
}

// Record persists a quarantine at the moment it happens. Called from the
// chain-completion callback right after QuarantineBranchAndRemove succeeds.
// Fail-open: a registry write failure must never gate the pipeline — the
// branch scan in reconcile() rediscovers the quarantine as a legacy entry.
func (r *QuarantineReconciler) Record(projectPath, repoName, key, branch string, memberIDs []types.BeadID) {
	if r == nil || branch == "" {
		return
	}
	ids := make([]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		ids = append(ids, string(id))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked()
	r.entries[registryKey(projectPath, branch)] = &QuarantinedChainEntry{
		ProjectPath:   projectPath,
		RepoName:      repoName,
		Key:           key,
		Branch:        branch,
		MemberIDs:     ids,
		QuarantinedAt: time.Now(),
	}
	if err := r.saveLocked(); err != nil {
		log.Printf("[QuarantineReconciler] registry write failed (rediscovery will cover): %v", err)
	}
}

// Start runs the reconcile loop. No-op when disabled.
func (r *QuarantineReconciler) Start() {
	if r == nil || r.worktreeMgr == nil || r.reader == nil || r.projects == nil {
		return
	}
	r.wg.Add(1)
	go r.run()
	log.Printf("[QuarantineReconciler] enabled (interval: %v, registry: %s)", r.interval, r.registryPath)
}

// Stop halts the loop.
func (r *QuarantineReconciler) Stop() {
	if r == nil {
		return
	}
	close(r.stopCh)
	r.wg.Wait()
}

func (r *QuarantineReconciler) run() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.Reconcile()
		}
	}
}

// Reconcile performs one pass: bootstrap legacy entries from branch scans,
// re-verdict each entry, integrate the clean ones. Exported for tests.
func (r *QuarantineReconciler) Reconcile() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked()
	r.bootstrapLocked()
	for _, e := range r.entries {
		r.reconcileEntryLocked(e)
	}
	if err := r.saveLocked(); err != nil {
		log.Printf("[QuarantineReconciler] registry write failed: %v", err)
	}
}

func registryKey(projectPath, branch string) string { return projectPath + "|" + branch }

func (r *QuarantineReconciler) loadLocked() {
	data, err := os.ReadFile(r.registryPath)
	if err != nil {
		return // no registry yet — fine
	}
	var list []QuarantinedChainEntry
	if err := json.Unmarshal(data, &list); err != nil {
		log.Printf("[QuarantineReconciler] registry corrupt (%v) — keeping in-memory entries only", err)
		return
	}
	for i := range list {
		e := list[i]
		if _, ok := r.entries[registryKey(e.ProjectPath, e.Branch)]; !ok {
			r.entries[registryKey(e.ProjectPath, e.Branch)] = &e
		}
	}
}

func (r *QuarantineReconciler) saveLocked() error {
	if r.registryPath == "" {
		return nil
	}
	list := make([]QuarantinedChainEntry, 0, len(r.entries))
	for _, e := range r.entries {
		list = append(list, *e)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(r.registryPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := r.registryPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.registryPath)
}

// bootstrapLocked discovers hydra/quarantine/* branches with no registry
// entry (quarantines from before the reconciler existed) and adds them with
// empty membership — reconstructed lazily at reconcile time.
func (r *QuarantineReconciler) bootstrapLocked() {
	for _, p := range r.projects() {
		out, err := gitOut(p.Path, "branch", "--list", "hydra/quarantine/*", "--format", "%(refname:short)")
		if err != nil {
			continue
		}
		for _, branch := range strings.Split(strings.TrimSpace(out), "\n") {
			branch = strings.TrimSpace(branch)
			if branch == "" {
				continue
			}
			k := registryKey(p.Path, branch)
			if _, ok := r.entries[k]; ok {
				continue
			}
			key := strings.TrimPrefix(branch, "hydra/quarantine/")
			log.Printf("[QuarantineReconciler] discovered legacy quarantine %s in %s — registering for reconciliation", branch, p.Path)
			r.entries[k] = &QuarantinedChainEntry{
				ProjectPath:   p.Path,
				RepoName:      p.Name,
				Key:           key,
				Branch:        branch,
				QuarantinedAt: time.Now(),
			}
		}
	}
}

func (r *QuarantineReconciler) reconcileEntryLocked(e *QuarantinedChainEntry) {
	// FR-001: a scrub was in flight when the daemon died — recover the
	// decision asynchronously; the normal reconcile pass must not touch the
	// entry while the scrub goroutine may delete or re-stamp it.
	if e.Status == scrubInProgressStatus {
		go recoverScrubInProgress(r, &scrubRegistryEntry{QuarantinedChainEntry: *e}, nil)
		return
	}
	if !branchExists(e.ProjectPath, e.Branch) {
		// Branch gone — someone (or an earlier pass) already handled it.
		log.Printf("[QuarantineReconciler] %s: branch %s no longer exists — dropping entry", e.Key, e.Branch)
		delete(r.entries, registryKey(e.ProjectPath, e.Branch))
		return
	}
	if e.IntegrateAttempts >= r.maxAttempts {
		return // fail-closed: leaves the quarantine for human inspection
	}

	members := e.MemberIDs
	if len(members) == 0 {
		members = r.reconstructMembersLocked(e)
		if len(members) == 0 {
			return // cannot judge — fail closed, try again next tick
		}
		e.MemberIDs = members
	}

	// Per-tick unverifiable sweep: drop streaks/verdicts whose member no
	// longer carries closed-failed (green scrub) and clear verdicts whose
	// scar an operator removed.
	r.sweepUnverifiableStreaks(e, func(id string) []string {
		if b := r.reader.FindByID(id); b != nil {
			return b.Labels
		}
		return nil
	})

	failed, firstFailed := chainHasFailedBeadIDs(r.reader, members)
	if failed {
		// M1: try to overturn the stale verdict with positive evidence before
		// giving up on this pass. Scoped to quarantined-chain members only —
		// never a project-wide scan.
		who := firstFailed
		if r.classifiedGate != nil && unverifiableImplemented {
			// Stamped (closed-failed-unverifiable) members are skipped in the
			// `who` selection; when none remains selectable the verdicts just
			// stand — the quarantine holds either way (fail-closed).
			who = revalidateTargetFor(r.reader, members, func(id string) bool {
				return !e.UnverifiableAt[id].IsZero()
			})
			if who == "" {
				return
			}
		}
		r.revalidateMemberLocked(e, who)
		if stillFailed, _ := chainHasFailedBeadIDs(r.reader, members); stillFailed {
			return // verdict stands — wait for the next pass or a close-scrub
		}
	}

	// Tip captured at verdict; IntegrateBranch verifies containment at merge.
	// A branch updated between the two simply merges its newer tip — the
	// verdict covered membership (labels), not content, so a newer tip of
	// the same chain is still the chain's work.
	tipBefore, _ := gitOut(e.ProjectPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+e.Branch)
	log.Printf("[QuarantineReconciler] chain %s: no failed members — re-integrating %s (tip %s)", e.Key, e.Branch, strings.TrimSpace(tipBefore))

	if err := r.worktreeMgr.IntegrateBranch(e.ProjectPath, e.Key, e.Branch); err != nil {
		e.IntegrateAttempts++
		// Suite gate blocked (typed error — no string sniffing): keep the
		// quarantine branch + entry and emit exactly one
		// quarantine_suite_gate_blocked event naming the chain.
		if errors.Is(err, ErrQuarantineSuiteGateBlocked) {
			log.Printf("[QuarantineReconciler] chain %s: re-integrate blocked by suite gate (attempt %d/%d) — quarantine kept", e.Key, e.IntegrateAttempts, r.maxAttempts)
			if r.events != nil {
				r.events.EmitRepo(e.ProjectPath, Event{Type: "quarantine_suite_gate_blocked",
					Bead: e.Key, Reason: "suite_gate_failed",
					Fields: map[string]any{"branch": e.Branch, "attempt": e.IntegrateAttempts, "error": fmt.Sprint(err)}})
			}
			return
		}
		log.Printf("[QuarantineReconciler] chain %s: re-integrate failed (attempt %d/%d): %v", e.Key, e.IntegrateAttempts, r.maxAttempts, err)
		if r.events != nil {
			r.events.EmitRepo(e.ProjectPath, Event{Type: "chain_reintegrate_failed",
				Bead: e.Key, Reason: "integrate_error",
				Fields: map[string]any{"branch": e.Branch, "attempt": e.IntegrateAttempts, "error": fmt.Sprint(err)}})
		}
		return
	}

	// Integrated — remove the quarantine branch and the entry.
	if _, err := gitOut(e.ProjectPath, "branch", "-D", e.Branch); err != nil {
		log.Printf("[QuarantineReconciler] chain %s: integrated but branch delete failed: %v", e.Key, err)
	}
	delete(r.entries, registryKey(e.ProjectPath, e.Branch))
	// Parked-backlog drain (chain-aware fast-path absorption): the chain's
	// re-integration just opened the integration lane for this repo. Reset
	// the per-repo cooldown and fire the open notifier so parked siblings —
	// including chain members matched via the integration-retest stage's
	// chain-root branch fallback — are swept immediately instead of waiting
	// out the hourly RetryInterval. Fail-open: NotifyRepoOpen recovers its
	// own panics and the notifier is nil when no retest stage is armed.
	resetIntegrationCooldown(e.ProjectPath)
	log.Printf("[QuarantineReconciler] ✅ chain %s re-integrated from quarantine and removed (%s)", e.Key, e.Branch)
	if r.events != nil {
		r.events.EmitRepo(e.ProjectPath, Event{Type: "chain_reintegrated",
			Bead: e.Key, Reason: "failed_beads_cleared",
			Fields: map[string]any{"branch": e.Branch, "members": len(e.MemberIDs)}})
	}
}

// reconstructMembersLocked walks the dependents graph from the chain root
// using the same read of issues.jsonl the registrar performs. Returns the
// root plus every bead that transitively depends on it.
func (r *QuarantineReconciler) reconstructMembersLocked(e *QuarantinedChainEntry) []string {
	issuesFile := filepath.Join(e.ProjectPath, ".beads", "issues.jsonl")
	beads, err := r.reader.ReadFile(issuesFile)
	if err != nil {
		log.Printf("[QuarantineReconciler] cannot read %s: %v", issuesFile, err)
		return nil
	}
	dependents := make(map[string][]string)
	known := make(map[string]bool, len(beads))
	for _, b := range beads {
		known[string(b.ID)] = true
		for _, dep := range b.Dependencies {
			dependents[string(dep)] = append(dependents[string(dep)], string(b.ID))
		}
	}
	if !known[e.Key] {
		return nil
	}
	var members []string
	queue := []string{e.Key}
	seen := map[string]bool{e.Key: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		members = append(members, cur)
		for _, next := range dependents[cur] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return members
}

// revalidateMemberLocked reruns the tainted member's declared verification
// command in its chain worktree. Only a green run scrubs closed-failed
// (fail-closed: red, unrunnable, missing bead, or a label-op failure all
// leave the verdict untouched). Escalation/human labels are never touched.
func (r *QuarantineReconciler) revalidateMemberLocked(e *QuarantinedChainEntry, beadID string) {
	classified := r.classifiedGate != nil && unverifiableImplemented
	if !classified && (r.revalidateGate == nil || r.removeLabels == nil || r.addLabels == nil) {
		return
	}
	if classified {
		// The cooldown bounds real reruns only: a no-command outcome consumes
		// no gate budget and must be re-evaluated every tick, or the bounded
		// streak can never reach three.
		if e.lastRevalidateRan && time.Since(e.LastRevalidation) < revalidationCooldown {
			return
		}
		e.lastRevalidateRan = false
	} else if time.Since(e.LastRevalidation) < revalidationCooldown {
		return
	} else {
		e.LastRevalidation = time.Now()
	}
	bead := r.reader.FindByID(beadID)
	if bead == nil {
		return
	}
	// Scratch worktree at the quarantine branch tip: the chain worktree was
	// removed at quarantine and the main checkout lacks the chain's code, so
	// only a fresh checkout of the branch verifies the real artifact.
	// node_modules is symlinked from the main checkout when present — a full
	// npm ci per revalidation is minutes + GBs for identical deps.
	scratch := r.worktreeMgr.worktreePath(e.ProjectPath, "revalidate-"+e.Key)
	if isGitWorktree(scratch) {
		_, _ = gitOut(e.ProjectPath, "worktree", "remove", scratch, "--force")
	}
	// core.hooksPath=/dev/null: repo hooks (husky post-checkout) run during
	// worktree add and fail in the daemon's clean environment, killing the
	// scratch checkout before the gate can run (live 2026-10-16: ilu
	// revalidation died on .husky/_/h with exit status 2). Daemon-internal
	// git operations have no business triggering repo hooks.
	if _, err := gitOut(e.ProjectPath, "-c", "core.hooksPath=/dev/null", "worktree", "add", "--detach", scratch, e.Branch); err != nil {
		log.Printf("[QuarantineReconciler] %s: scratch worktree for revalidation failed (label stands): %v", e.Key, err)
		return
	}
	defer func() {
		_, _ = gitOut(e.ProjectPath, "worktree", "remove", scratch, "--force")
		_, _ = gitOut(e.ProjectPath, "worktree", "prune")
	}()
	mainMods := filepath.Join(e.ProjectPath, "node_modules")
	scratchMods := filepath.Join(scratch, "node_modules")
	if st, err := os.Stat(mainMods); err == nil && st.IsDir() {
		if _, err := os.Lstat(scratchMods); os.IsNotExist(err) {
			_ = os.Symlink(mainMods, scratchMods)
		}
	}
	if classified {
		v := r.classifiedGate(bead, e.ProjectPath, scratch)
		e.lastRevalidateRan = v.Ran
		if v.Ran {
			e.LastRevalidation = time.Now() // a real rerun — bound the next one
		}
		if v.Ran && v.Passed {
			// Green rerun scrubs closed-failed (fail-closed streak of reds and
			// faults untouched; escalation/human labels never touched).
			if err := r.classifiedRemove(e.ProjectPath, bead.ID, []string{closedFailedLabel}); err != nil {
				log.Printf("[QuarantineReconciler] %s: gate rerun green but label removal failed on %s: %v", e.Key, beadID, err)
				return
			}
			if err := r.classifiedAdd(e.ProjectPath, bead.ID, []string{closedFailedRevalidatedLabel}); err != nil {
				log.Printf("[QuarantineReconciler] %s: revalidated label add failed on %s: %v", e.Key, beadID, err)
				return
			}
			delete(e.RanFalseStreaks, beadID)
			delete(e.UnverifiableAt, beadID)
			log.Printf("[QuarantineReconciler] %s: member %s closed-failed revalidated (gate rerun green) — label removed", e.Key, beadID)
			if r.events != nil {
				r.events.EmitRepo(e.ProjectPath, Event{Type: "label_revalidated", Project: e.RepoName,
					Bead: beadID, Reason: "gate_rerun_green"})
			}
			return
		}
		r.noteRevalidateOutcome(e, beadID, v)
		return
	}
	passed, ran := r.revalidateGate(bead, e.ProjectPath, scratch)
	if !ran || !passed {
		log.Printf("[QuarantineReconciler] %s: revalidation of %s: ran=%v passed=%v — closed-failed stands", e.Key, beadID, ran, passed)
		return
	}
	if err := r.removeLabels(e.ProjectPath, bead.ID, []string{closedFailedLabel}); err != nil {
		log.Printf("[QuarantineReconciler] %s: gate rerun green but label removal failed on %s: %v", e.Key, beadID, err)
		return
	}
	if err := r.addLabels(e.ProjectPath, bead.ID, []string{closedFailedRevalidatedLabel}); err != nil {
		log.Printf("[QuarantineReconciler] %s: revalidated label add failed on %s: %v", e.Key, beadID, err)
	}
	log.Printf("[QuarantineReconciler] %s: member %s closed-failed revalidated (gate rerun green) — label removed", e.Key, beadID)
	if r.events != nil {
		r.events.EmitRepo(e.ProjectPath, Event{Type: "label_revalidated", Project: e.RepoName,
			Bead: beadID, Reason: "gate_rerun_green"})
	}
}

// chainHasFailedBeadIDs reports whether any member carries closed-failed.
// Same semantics as ChainCompletionDetector.ChainHasFailedBeads, operating on
// plain ID strings for the reconciler's recorded membership.
func chainHasFailedBeadIDs(reader *BeadFileReader, memberIDs []string) (bool, string) {
	for _, id := range memberIDs {
		bead := reader.FindByID(id)
		if bead == nil {
			continue
		}
		for _, l := range bead.Labels {
			if l == closedFailedLabel {
				return true, id
			}
		}
	}
	return false, ""
}
