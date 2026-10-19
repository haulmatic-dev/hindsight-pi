package core

// integration_retest.go implements the integration-retest stage (Tier A of
// the two-tier integration-conflict plan; Tier B — agentic conflict
// resolution — is a separate bead and intentionally absent here).
//
// Motivation: ajantha-srv-telematics-ykz sat parked with
// integration-exhausted for hours while the conflict it was parked for had
// already evaporated (later merges resolved the overlap). One merge-tree
// check proved it clean; manual gates + PR #280 landed it. That recheck is
// mechanical, so it is a pipeline stage, not a human task.
//
// Contract (per enabled project, driven from the overseer tick after the
// publish pass):
//
//  1. Select beads labelled integration-failed or integration-exhausted
//     whose hydra-owned branch (hydra/wt-<id>[-vN] or hydra/quarantine/<id>)
//     still exists, rate-limited to one retest per RetryInterval and capped
//     at MaxAttempts outcomes per bead. Beads hard-parked by Pan for other
//     causes (worktree-unavailable, infra-exhausted) are skipped.
//  2. git fetch origin <base>; git merge-tree --write-tree <base> <branch>.
//     Conflict -> typed integration_retest{result:conflict} event, stay
//     parked, count the attempt. This stage never resolves conflicts.
//  3. Clean -> scratch worktree at origin/<base> (never the user's main
//     checkout — dirty human checkouts poisoned 288 integrations in the
//     2026-09-22 telematics campaign), merge the branch --no-edit, run the
//     project's configured daemon gates (or detectable defaults:
//     `go build ./...` / `npm run build`; fail-open when undetectable).
//  4. Gates green -> land via the existing dogfood PR path
//     (PublishViaPR: dogfood/<id>-retest feature branch + gh pr create +
//     gh pr merge --admin, verified MERGED — branch protection respected,
//     never a direct push, never a force-push). On landed: close the bead,
//     strip the integration labels, emit
//     integration_retest{result:landed,pr:<url>}.
//  5. Gates red -> stay parked, emit integration_retest{result:gates-failed},
//     count the attempt.
//  6. Everything else fails open: gh/network/registry/git errors log and
//     skip the bead this tick. The stage never blocks the reconciler and
//     never panics the daemon (per-bead recover).

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Integration-retest outcome slugs carried in Event.Reason's sibling field
// and in the typed event's Fields["result"].
const (
	retestResultConflict      = "conflict"
	retestResultGatesFailed   = "gates-failed"
	retestResultLanded        = "landed"
	retestResultAttemptCapped = "attempt-capped"
)

// Labels the selector matches on, and labels that mean "parked by Pan (or
// the closer) for a different cause" — those beads are never retested here.
const (
	retestLabelFailed    = "integration-failed"
	retestLabelExhausted = "integration-exhausted"
)

// retestExcludedParkLabels: a bead carrying one of these is parked for a
// cause this stage does not own; retesting it would respawn it into the
// same wall (mirrors the scheduler/Charon skip lists).
var retestExcludedParkLabels = []string{worktreeUnavailableLabel, "infra-exhausted"}

// retestBranchPrefixes are the only branch families this stage will ever
// fetch, merge, or push from. Guardrail: hydra-owned refs only.
const (
	retestWorktreeBranchPrefix   = "hydra/wt-"
	retestQuarantineBranchPrefix = "hydra/quarantine/"
)

type retestOutcome struct {
	Result string
	PRURL  string
}

// IntegrationRetestStage rechecks integration-parked beads and lands the
// provably clean ones. All state is in-memory (attempt counters and
// last-attempt times): a daemon restart re-arms the cap, which is
// deliberately conservative — worst case is MaxAttempts extra merge-tree
// checks per bead per restart, all read-only until gates pass.
type IntegrationRetestStage struct {
	cfg         IntegrationRetestConfig
	full        *Config
	beadsClient BeadsClient
	events      *EventSink
	gateRunner  *GateRunner
	home        string // HYDRA_HOME; scratch worktrees live under it

	now func() time.Time

	// Injectable seams for tests. Defaults are the production impls.
	gitFn       func(dir string, args ...string) (string, error)
	mergeTreeFn func(projectPath, base, branch string) (out string, exit int, err error)
	landFn      func(projectPath, base, featureBranch, title, token string) (string, error)
	tokenFn     func(projectPath string) (string, error)

	// chainKeyFn maps a bead ID to its chain's worktree key (chain root ID),
	// falling back to the bead's own ID when it belongs to no chain. Nil = no
	// chain-root fallback: the selector only ever matches the bead's own
	// branches. Mirrors BeadCloser/Charon's identical seam (bead_closer.go).
	chainKeyFn func(types.BeadID) string

	mu          sync.Mutex
	attempts    map[string]int
	lastAttempt map[string]time.Time
	lastOpen    map[string]time.Time // repoPath -> last successful integration (fast-path window)
	sweeping    map[string]bool      // repoPath -> fast-path sweep in flight
}

// retestFastPathWindow: how long after a successful integration on a repo
// the per-bead RetryInterval is bypassed for that repo's parked beads. The
// attempt cap (MaxAttempts) always applies.
const retestFastPathWindow = 5 * time.Minute

// NewIntegrationRetestStage builds the stage. Nil beadsClient/events are
// tolerated (stage is inert); the gate runner may be nil when no project
// declares daemon-owned gates.
func NewIntegrationRetestStage(cfg IntegrationRetestConfig, full *Config, beadsClient BeadsClient, events *EventSink, gateRunner *GateRunner, home string, chainKeyFn func(beadID types.BeadID) string) *IntegrationRetestStage {
	cfg.applyDefaults()
	return &IntegrationRetestStage{
		cfg:         cfg,
		full:        full,
		beadsClient: beadsClient,
		events:      events,
		gateRunner:  gateRunner,
		home:        home,
		chainKeyFn:  chainKeyFn,
		now:         time.Now,
		gitFn:       gitOut,
		mergeTreeFn: defaultMergeTree,
		landFn:      PublishViaPR,
		tokenFn: func(projectPath string) (string, error) {
			id, err := resolvePublishIdentityFor(projectPath)
			if err != nil {
				return "", err
			}
			return id.Token, nil
		},
		attempts:    map[string]int{},
		lastAttempt: map[string]time.Time{},
		lastOpen:    map[string]time.Time{},
		sweeping:    map[string]bool{},
	}
}

// SetChainKeyForBeadID wires the chain-key resolver used by findBranch's
// chain-root fallback. Optional: nil keeps the own-branch-only selector.
func (s *IntegrationRetestStage) SetChainKeyForBeadID(fn func(types.BeadID) string) {
	s.chainKeyFn = fn
}

// Enabled reports whether the stage should run at all.
func (s *IntegrationRetestStage) Enabled() bool { return s != nil && s.cfg.Enabled }

// Run executes one retest sweep over every project. Per-project and per-bead
// work is isolated: an error (or panic) on one bead never reaches the tick.
func (s *IntegrationRetestStage) Run(projects []types.Project, activeKeys map[string]bool) {
	if !s.Enabled() {
		return
	}
	for _, project := range projects {
		s.runProject(project, activeKeys)
	}
}

func (s *IntegrationRetestStage) runProject(project types.Project, activeKeys map[string]bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[IntegrationRetest] PANIC recovering for %s: %v (stage continues)", project.Name, r)
			s.mu.Lock()
			delete(s.sweeping, project.Path)
			s.mu.Unlock()
		}
	}()
	if s.beadsClient == nil || project.Path == "" {
		return
	}
	if project.Type == types.RepoTypeMulti && len(project.Repos) > 0 {
		s.runProjectRepos(project, activeKeys)
		return
	}
	beads, err := s.beadsClient.ListBeads(project.Path, "")
	if err != nil {
		log.Printf("[IntegrationRetest] list beads for %s: %v (skip tick)", project.Name, err)
		return
	}
	for i := range beads {
		bead := &beads[i]
		if !s.selectBead(project.Path, bead, activeKeys) {
			continue
		}
		s.runBead(project, project.Path, bead)
	}
}

// runProjectRepos rechecks a multi-repo project's parked beads per repo: the
// parent directory has no .beads store (br fails with exit status 2) and no
// git repo (worktree/git plumbing would all fail), so every operation must
// run against the per-repo directory (nipuna.j fms incident, 2026-09-30).
func (s *IntegrationRetestStage) runProjectRepos(project types.Project, activeKeys map[string]bool) {
	for _, repo := range project.Repos {
		repoPath := resolveRepoPath(project, repo)
		if repoPath == "" {
			continue
		}
		beads, err := s.beadsClient.ListBeads(repoPath, "")
		if err != nil {
			log.Printf("[IntegrationRetest] list beads for %s/%s: %v (skip repo tick)", project.Name, repo.Name, err)
			continue
		}
		for i := range beads {
			bead := &beads[i]
			if !s.selectBead(repoPath, bead, activeKeys) {
				continue
			}
			s.runBead(project, repoPath, bead)
		}
	}
}

// NotifyRepoOpen is the fast-path absorption hook: called after a successful
// integration on projectPath, it (a) opens the fast-path window for that
// repo and (b) sweeps the repo's parked beads immediately in the background.
// Serialized per repo (a sweep already in flight skips), fully fail-open.
func (s *IntegrationRetestStage) NotifyRepoOpen(projectPath string, projects []types.Project) {
	if s == nil || !s.Enabled() || projectPath == "" {
		return
	}
	s.mu.Lock()
	if s.sweeping[projectPath] {
		s.mu.Unlock()
		return
	}
	s.sweeping[projectPath] = true
	s.mu.Unlock()
	var target *types.Project
	for i := range projects {
		if projects[i].Path == projectPath {
			target = &projects[i]
			break
		}
	}
	if target == nil {
		return
	}
	s.mu.Lock()
	s.lastOpen[projectPath] = s.now()
	s.mu.Unlock()
	go func(p types.Project) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[IntegrationRetest] PANIC in fast-path sweep for %s: %v", filepath.Base(p.Path), r)
			}
			s.mu.Lock()
			delete(s.sweeping, p.Path)
			s.mu.Unlock()
		}()
		log.Printf("[IntegrationRetest] fast-path absorption: integration opened on %s — sweeping parked beads now", filepath.Base(p.Path))
		s.runProject(p, nil)
		// Belt-and-braces: a panic that escaped runProject's own recover must
		// still clear the flag so later opens can sweep again.
		s.mu.Lock()
		delete(s.sweeping, p.Path)
		s.mu.Unlock()
	}(*target)
}

// selectBead is the pure selector: label match, not terminal, not parked for
// another cause, not running under a live worker, rate-limit and attempt-cap
// pass, and a hydra-owned branch still exists.
func (s *IntegrationRetestStage) selectBead(projectPath string, bead *types.Bead, activeKeys map[string]bool) bool {
	if bead == nil || bead.Status.IsTerminal() {
		return false
	}
	if !hasBeadLabel(bead.Labels, retestLabelFailed) && !hasBeadLabel(bead.Labels, retestLabelExhausted) && !hasBeadLabel(bead.Labels, integrationPendingLabel) {
		return false
	}
	for _, l := range retestExcludedParkLabels {
		if hasBeadLabel(bead.Labels, l) {
			return false
		}
	}
	if activeKeys[string(bead.ID)] {
		return false
	}
	if !s.allowAttempt(projectPath, string(bead.ID)) {
		return false
	}
	return s.findBranch(projectPath, bead) != ""
}

// allowAttempt enforces the per-bead retry interval and the global attempt
// cap. Errors and panics never increment — only counted outcomes do.
// When the repo's integration path recently opened (fast-path absorption),
// the retry-interval gap is bypassed so a backlog of parked siblings is
// absorbed immediately instead of one-per-RetryInterval.
func (s *IntegrationRetestStage) allowAttempt(projectPath, beadID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts[beadID] >= s.cfg.MaxAttempts {
		return false
	}
	if last, ok := s.lastAttempt[beadID]; ok && s.now().Sub(last) < s.cfg.RetryIntervalDuration() {
		// Fast-path bypass: only while the window is open for this repo.
		opened, ok := s.lastOpen[projectPath]
		if !ok || s.now().Sub(opened) >= retestFastPathWindow {
			return false
		}
	}
	s.lastAttempt[beadID] = s.now()
	return true
}

// countAttempt records a counted outcome (conflict, gates-failed).
func (s *IntegrationRetestStage) countAttempt(beadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[beadID]++
}

// resetAttempts clears counters after a land.
func (s *IntegrationRetestStage) resetAttempts(beadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, beadID)
	delete(s.lastAttempt, beadID)
}

// findBranch locates the bead's hydra-owned branch: hydra/wt-<id>[-vN] first
// (local refs, then origin), then hydra/quarantine/<id> (2026-09-22
// telematics audit: quarantine branches carried real work while beads read
// closed). Returns "" when none exists. Read-only: rev-parse/for-each-ref
// and ls-remote only.
func (s *IntegrationRetestStage) findBranch(projectPath string, bead *types.Bead) string {
	if projectPath == "" {
		return ""
	}
	id := string(bead.ID)
	// Chain-root worktree fallback: chain-member beads park their work on the
	// chain root's branch (hydra/wt-<chain-root>), so when the bead's own ID
	// matches nothing we retry with the chain key before giving up.
	ids := []string{id}
	if s.chainKeyFn != nil {
		if ck := s.chainKeyFn(bead.ID); ck != "" && ck != id {
			ids = append(ids, ck)
		}
	}

	for _, cid := range ids {
		patterns := []string{
			"refs/heads/" + retestWorktreeBranchPrefix + cid,
			"refs/heads/" + retestQuarantineBranchPrefix + cid,
		}
		for _, pat := range patterns {
			if out, err := s.gitFn(projectPath, "for-each-ref", "--format=%(refname:short)", pat); err == nil {
				if branch := firstLine(out); branch != "" {
					return branch
				}
			}
		}
		// Versioned worktree branches: hydra/wt-<cid>-vN (webapp-tc5 incident).
		if out, err := s.gitFn(projectPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+retestWorktreeBranchPrefix+cid+"-v*"); err == nil {
			if branch := lastLine(out); branch != "" {
				return branch
			}
		}
		// Local clone may be stale; ask the remote before giving up.
		for _, pat := range patterns {
			short := strings.TrimPrefix(pat, "refs/heads/")
			if out, err := s.gitFn(projectPath, "ls-remote", "--heads", "origin", short+"*"); err == nil {
				fields := strings.Fields(out)
				for _, f := range fields {
					if strings.HasPrefix(f, retestWorktreeBranchPrefix) || strings.HasPrefix(f, retestQuarantineBranchPrefix) {
						return f
					}
				}
			}
		}
	}
	return ""
}

// runBead executes the full retest pipeline for one bead. Every step fails
// open: an error logs, emits nothing fatal, and leaves the bead parked.
func (s *IntegrationRetestStage) runBead(project types.Project, projectPath string, bead *types.Bead) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[IntegrationRetest] PANIC on %s: %v (bead stays parked)", bead.ID, r)
		}
	}()
	beadID := string(bead.ID)
	branch := s.findBranch(projectPath, bead)
	if branch == "" {
		return
	}
	// Guardrail, belt-and-braces: only hydra-owned prefixes, ever.
	if !strings.HasPrefix(branch, retestWorktreeBranchPrefix) && !strings.HasPrefix(branch, retestQuarantineBranchPrefix) {
		log.Printf("[IntegrationRetest] %s: refusing non-hydra branch %q", beadID, branch)
		return
	}

	base, err := (&WorktreeManager{}).WorkingBranch(projectPath)
	if err != nil || base == "" {
		log.Printf("[IntegrationRetest] %s: no working branch (%v); skip", beadID, err)
		return
	}

	if _, err := s.gitFn(projectPath, "fetch", "origin", base); err != nil {
		log.Printf("[IntegrationRetest] %s: fetch origin/%s: %v (skip tick)", beadID, base, err)
		return
	}

	// Landed-check via commit ancestry: if the branch tip is already an
	// ancestor of origin/<base>, the work landed previously (close-path
	// integration, a sibling's retest, fast-path absorption) and only the
	// bead never closed. Close it directly — no PR, no gates rerun: a PR here
	// would carry an empty diff and fail the rdju no-diff class, and the
	// close-reject ladder would grind. Ancestor test runs after the fetch so
	// origin/<base> is fresh; merge-tree clean is NOT this check — clean
	// means "mergeable", ancestry means "already merged".
	if s.isAncestorOfBase(projectPath, branch, "origin/"+base) {
		log.Printf("[IntegrationRetest] %s: branch %s already an ancestor of origin/%s — landed, closing", beadID, branch, base)
		s.closeLanded(project, projectPath, bead,
			fmt.Sprintf("branch %s already an ancestor of origin/%s — work landed, bead closed without PR", branch, base))
		return
	}

	clean, err := s.mergeTreeClean(projectPath, "origin/"+base, branch)
	if err != nil {
		log.Printf("[IntegrationRetest] %s: merge-tree: %v (skip tick)", beadID, err)
		return
	}
	if !clean {
		s.countAttempt(beadID)
		s.emit(projectPath, project.Name, beadID, retestResultConflict, "",
			fmt.Sprintf("branch %s still conflicts with origin/%s — stays parked (Tier B agentic resolution not implemented)", branch, base))
		return
	}

	scratch, err := s.scratchWorktree(projectPath, project.Name, beadID, "origin/"+base)
	if err != nil {
		log.Printf("[IntegrationRetest] %s: scratch worktree: %v (skip tick)", beadID, err)
		return
	}
	defer s.removeScratch(projectPath, scratch)

	if _, err := s.gitFn(scratch, "merge", "--no-edit", branch); err != nil {
		// Race: clean at merge-tree, dirty at merge (base moved mid-tick).
		// Not a conflict resolution candidate — just count and park.
		s.countAttempt(beadID)
		s.emit(projectPath, project.Name, beadID, retestResultConflict, "",
			fmt.Sprintf("merge of %s failed in scratch worktree: %v", branch, err))
		return
	}

	if err := s.runGates(project.Name, bead, projectPath, scratch); err != nil {
		s.countAttempt(beadID)
		s.emit(projectPath, project.Name, beadID, retestResultGatesFailed, "",
			fmt.Sprintf("gates red on retest merge: %v", err))
		return
	}

	outcome, err := s.land(projectPath, scratch, base, bead)
	if err != nil {
		log.Printf("[IntegrationRetest] %s: land: %v (skip tick; beads/gates untouched)", beadID, err)
		return
	}
	s.resetAttempts(beadID)
	if s.beadsClient != nil {
		if err := s.beadsClient.CloseBead(projectPath, "", bead.ID, "integration-retest landed via PR "+outcome.PRURL); err != nil {
			log.Printf("[IntegrationRetest] %s: close after land: %v", beadID, err)
		}
		if err := s.beadsClient.RemoveBeadLabels(projectPath, "", bead.ID, []string{retestLabelFailed, retestLabelExhausted, integrationPendingLabel}); err != nil {
			log.Printf("[IntegrationRetest] %s: strip labels after land: %v", beadID, err)
		}
	}
	AppendActivity(projectPath, beadID, "integration-retest: landed via PR "+outcome.PRURL)
	s.emit(projectPath, project.Name, beadID, retestResultLanded, outcome.PRURL,
		"retest merge clean, gates green — landed via PR "+outcome.PRURL)
}

// closeLanded closes an already-landed bead: reset attempt counters, close
// with a landed reason, strip the park labels, and emit the landed event.
// Shared by the PR land path and the ancestry landed-check.
func (s *IntegrationRetestStage) closeLanded(project types.Project, projectPath string, bead *types.Bead, detail string) {
	beadID := string(bead.ID)
	s.resetAttempts(beadID)
	if s.beadsClient != nil {
		if err := s.beadsClient.CloseBead(projectPath, "", bead.ID, "integration-retest: "+detail); err != nil {
			log.Printf("[IntegrationRetest] %s: close after landed-check: %v", beadID, err)
		}
		if err := s.beadsClient.RemoveBeadLabels(projectPath, "", bead.ID, []string{retestLabelFailed, retestLabelExhausted, integrationPendingLabel}); err != nil {
			log.Printf("[IntegrationRetest] %s: strip labels after landed-check: %v", beadID, err)
		}
	}
	AppendActivity(projectPath, beadID, "integration-retest: "+detail)
	s.emit(projectPath, project.Name, beadID, retestResultLanded, "", detail)
}

// isAncestorOfBase reports whether the branch tip is already contained in
// the base ref — i.e. the work landed previously and only the bead never
// closed. Read-only; an error reads as "not landed" (fail-open, the normal
// retest flow continues).
func (s *IntegrationRetestStage) isAncestorOfBase(projectPath, branch, baseRef string) bool {
	_, err := s.gitFn(projectPath, "merge-base", "--is-ancestor", branch, baseRef)
	return err == nil
}

// mergeTreeClean runs `git merge-tree --write-tree <base> <branch>` and
// reports whether the merge is conflict-free. Exit 0 = clean; exit 1 =
// conflicts (CONFLICT lines on stdout); anything else = error. Parsing is a
// separate pure function for unit testing.
func (s *IntegrationRetestStage) mergeTreeClean(projectPath, base, branch string) (bool, error) {
	out, exit, err := s.mergeTreeFn(projectPath, base, branch)
	return parseMergeTreeResult(exit, out, err)
}

// defaultMergeTree runs merge-tree preserving the exit code, which gitOut
// (CombinedOutput + wrapped error) does not.
func defaultMergeTree(projectPath, base, branch string) (string, int, error) {
	cmd := exec.Command("git", "merge-tree", "--write-tree", base, branch)
	cmd.Dir = projectPath
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode(), nil
	}
	return string(out), -1, err
}

// parseMergeTreeResult interprets merge-tree output: clean when exit 0;
// conflicted when exit 1 with CONFLICT markers; otherwise an error.
func parseMergeTreeResult(exit int, out string, runErr error) (bool, error) {
	if runErr != nil {
		return false, runErr
	}
	switch exit {
	case 0:
		return true, nil
	case 1:
		if strings.Contains(out, "CONFLICT") {
			return false, nil
		}
		// Exit 1 without CONFLICT markers is a merge-tree usage/anomaly —
		// treat as error (fail open), not as a clean merge.
		return false, fmt.Errorf("merge-tree exited 1 without CONFLICT markers: %s", firstLine(out))
	default:
		return false, fmt.Errorf("merge-tree exited %d: %s", exit, firstLine(out))
	}
}

// scratchWorktree creates a disposable worktree at origin/<base>. The merge
// NEVER happens in the user's main checkout (2026-09-22 telematics audit:
// 288 auto-commit-dirty-main integration failures that campaign).
func (s *IntegrationRetestStage) scratchWorktree(projectPath, projectName, beadID, baseRef string) (string, error) {
	root := s.home
	if root == "" {
		root = os.TempDir()
	}
	dir := filepath.Join(root, "integration-retest", prSafeName(projectName)+"-"+prSafeName(beadID)+"-"+fmt.Sprint(s.now().Unix()))
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if _, err := s.gitFn(projectPath, "worktree", "add", "--detach", dir, baseRef); err != nil {
		return "", fmt.Errorf("worktree add: %w", err)
	}
	return dir, nil
}

func (s *IntegrationRetestStage) removeScratch(projectPath, scratch string) {
	if scratch == "" {
		return
	}
	if _, err := s.gitFn(projectPath, "worktree", "remove", "--force", scratch); err != nil {
		log.Printf("[IntegrationRetest] scratch cleanup %s: %v", scratch, err)
	}
}

// runGates runs the project's declared daemon gates against the scratch
// merge, cheapest first (lint → build → test). With no gates configured it
// falls back to detectable defaults (`go build ./...`, `npm run build`) and
// fails open when neither is detectable.
func (s *IntegrationRetestStage) runGates(projectName string, bead *types.Bead, projectPath, worktreePath string) error {
	gates := gateSetForProject(s.full, projectName)
	if gates != nil && gates.DaemonOwned {
		if s.gateRunner == nil {
			return fmt.Errorf("daemon_owned gates declared for %s but no gate runner attached (fail closed)", projectName)
		}
		named := []struct {
			name string
			spec GateSpec
		}{
			{"lint", gates.Lint},
			{"build", gates.Build},
			{"test", gates.Test},
		}
		scope := ""
		if targets := beadTargetFiles(bead); len(targets) > 0 {
			scope = targets[0]
		}
		for _, g := range named {
			if g.spec.Cmd == "" {
				continue
			}
			a, err := s.gateRunner.RunGate(GateRequest{
				Project: projectName, BeadID: string(bead.ID), Gate: g.name,
				Spec: g.spec, ProjectPath: projectPath, WorktreePath: worktreePath,
				Scope: scope,
			})
			if err != nil {
				return fmt.Errorf("gate %s: %w", g.name, err)
			}
			if a.Status != GateStatusPassed {
				return fmt.Errorf("gate %s failed (see artifact %s)", g.name, a.RawLog)
			}
		}
		return nil
	}
	return s.runDefaultGates(projectName, bead, projectPath, worktreePath)
}

// runDefaultGates applies the detectable-default gate set for projects with
// no configured gates. Undetectable stacks fail open (no gates = nothing to
// prove mechanically; the bead was already gated when it first closed).
func (s *IntegrationRetestStage) runDefaultGates(projectName string, bead *types.Bead, projectPath, worktreePath string) error {
	if s.gateRunner == nil {
		return nil
	}
	var spec GateSpec
	switch {
	case fileExists(filepath.Join(worktreePath, "go.mod")):
		spec = GateSpec{Cmd: "go build ./..."}
	case npmHasBuildScript(worktreePath):
		spec = GateSpec{Cmd: "npm run build"}
	default:
		log.Printf("[IntegrationRetest] %s/%s: no gates configured and no defaults detectable — failing open", projectName, bead.ID)
		return nil
	}
	a, err := s.gateRunner.RunGate(GateRequest{
		Project: projectName, BeadID: string(bead.ID), Gate: "build",
		Spec: spec, ProjectPath: projectPath, WorktreePath: worktreePath,
	})
	if err != nil {
		return fmt.Errorf("default gate build: %w", err)
	}
	if a.Status != GateStatusPassed {
		return fmt.Errorf("default gate build failed (see artifact %s)", a.RawLog)
	}
	return nil
}

// land pushes the scratch merge through the existing dogfood PR path:
// dogfood/<bead-id>-retest feature branch + gh pr create + gh pr merge
// --admin, verified MERGED. Branch protection is respected (the base is
// never pushed directly) and nothing is ever force-pushed — the dogfood
// branch is created fresh from this scratch merge.
func (s *IntegrationRetestStage) land(projectPath, scratch, base string, bead *types.Bead) (retestOutcome, error) {
	feature := IntegrationBranchPrefix(projectPath) + string(bead.ID) + "-retest"
	if _, err := s.gitFn(scratch, "switch", "-c", feature); err != nil {
		return retestOutcome{}, fmt.Errorf("create %s: %w", feature, err)
	}
	// Owner-identity (fail-closed): the retest lands under the bead owner's
	// PAT; an unresolved identity aborts the retest attempt.
	token, err := s.tokenFn(projectPath)
	if err != nil {
		return retestOutcome{}, fmt.Errorf("publish identity: %w", err)
	}
	url, err := s.landFn(scratch, base, feature, bead.Title+" (integration retest)", token)
	if err != nil {
		return retestOutcome{}, err
	}
	return retestOutcome{Result: retestResultLanded, PRURL: url}, nil
}

func (s *IntegrationRetestStage) emit(projectPath, projectName, beadID, result, prURL, detail string) {
	if s.events == nil {
		return
	}
	fields := map[string]any{"result": result}
	if prURL != "" {
		fields["pr"] = prURL
	}
	s.events.EmitRepo(projectPath, Event{Type: "integration_retest", Project: projectName,
		Bead: beadID, Reason: result, Detail: detail, Fields: fields})
}

// gateSetForProject returns the declared gate set for a project, or nil.
// Same lookup as BeadCloser.gatesFor; duplicated here to keep the stage
// decoupled from the closer's lifecycle.
func gateSetForProject(cfg *Config, projectName string) *GateSet {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == projectName {
			return cfg.Projects[i].Gates
		}
	}
	return nil
}

// npmHasBuildScript reports whether package.json declares a "build" script.
// Parsing is deliberately shallow (strings, not JSON) — a false positive
// just runs a gate that fails, a false negative fails open.
func npmHasBuildScript(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "\"build\"")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
