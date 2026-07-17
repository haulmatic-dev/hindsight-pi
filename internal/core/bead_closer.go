package core

import (
	"fmt"
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadCloser closes beads via beadsClient based on worker completion status.
type BeadCloser struct {
	beadsClient       BeadsClient
	intelligenceHub   *IntelligenceHub
	projectResolver   *ProjectResolver
	validationService *ValidationService
	config            *Config
	worktreeManager   *WorktreeManager        // per-chain git isolation; nil = disabled
	chainKeyFn        func(types.Bead) string // maps a bead to its chain's worktree key
	isChainMember     func(types.BeadID) bool // reports whether a bead belongs to a registered multi-bead chain
}

// SetWorktreeManager enables commit-on-close and singleton integrate-on-close
// into the bead's chain worktree. isChainMember is the source of truth for
// whether a bead belongs to a registered multi-bead chain; relying on
// chainKeyFn(bead) == bead.ID is insufficient because a chain root also maps
// to its own ID.
func (c *BeadCloser) SetWorktreeManager(m *WorktreeManager, chainKeyFn func(types.Bead) string, isChainMember func(types.BeadID) bool) {
	c.worktreeManager = m
	c.chainKeyFn = chainKeyFn
	c.isChainMember = isChainMember
}

// NewBeadCloser creates a new bead closer.
func NewBeadCloser(beadsClient BeadsClient, intelligenceHub *IntelligenceHub, projectResolver *ProjectResolver, validationService *ValidationService, config *Config) *BeadCloser {
	return &BeadCloser{
		beadsClient:       beadsClient,
		intelligenceHub:   intelligenceHub,
		projectResolver:   projectResolver,
		validationService: validationService,
		config:            config,
	}
}

// Close closes the bead via beadsClient based on worker completion status.
//
// Fix: the previous version only triggered TDD validation when
// `bead.Type == "refactor"`. In production the task-breakdown runner
// emits all TDD phase beads with `Type: "task"` and encodes the phase
// in labels, so the type check was dead code and validation never ran.
// We now inspect labels (tdd-refactor, tdd-green) and also fire
// validation when the GREEN bead closes, since that is the most likely
// place for an incomplete implementation to land.
//
// Fix: when `close_reason=failed` (worker exited non-zero, hit
// retries, etc.) we no longer let the close silently propagate to
// dependents. Config flag `validation.close_blocked_on_failed` (default
// true) gates this. When set, failed closures trigger a fix bead
// creation flow and the bead's dependents stay blocked on the fix.
//
// Infra-failure guard (2026-06-25): transient infrastructure failures
// (rate limits, timeouts, daemon locks) do not create fix beads.
// The bead stays open and the scheduler retries it with exponential
// backoff up to 1 hour, then hourly thereafter.  Only code-level
// failures (placeholder code, build errors, etc.) spawn fix beads.
func (c *BeadCloser) Close(worker *types.Worker, success bool, exitCode int) error {
	projectName := worker.BeadID.Project()
	repoName := worker.BeadID.Repo()

	log.Printf("[closeBead] Closing bead %s (project: %s, repo: %s)",
		worker.BeadID.String(), projectName, repoName)

	projectPath := c.projectResolver.ResolveProjectPathByWorker(worker, projectName)
	if projectPath == "" {
		return &ProjectNotFoundError{Project: projectName}
	}

	reason := statusReason(success)
	log.Printf("[closeBead] Closing with reason: %s (path: %s)", reason, projectPath)

	// Queue TLDR indexing on successful completion
	if success {
		repoPath := c.projectResolver.ResolveRepoPath(projectName, repoName, projectPath)
		if repoPath != "" {
			c.intelligenceHub.QueueRepoIndexing(projectName, repoPath)
		}
	}

	bead, err := c.beadsClient.GetBead(projectPath, repoName, worker.BeadID)
	if err == nil && bead.Status.IsTerminal() {
		log.Printf("[closeBead] Bead %s already closed (status: %s), skipping", worker.BeadID.String(), bead.Status)
		return nil
	}

	// Run TDD validation for TDD-phase beads before closing. The
	// check now looks at labels rather than `bead.Type` (which the
	// task-breakdown runner never sets to "refactor"). GREEN beads
	// are validated too because that is where incomplete implementations most often land.
	if success && bead != nil && c.shouldRunTDDValidation(bead) {
		if err := c.validationService.RunTDDValidation(*bead, projectPath); err != nil {
			log.Printf("[closeBead] TDD validation error for %s: %v", worker.BeadID.String(), err)
		}
	}

	// Fix: failed closures normally let the close proceed, which
	// meant 57% of beads (69/121 in one audit) closed as `failed` and
	// their dependents unblocked. The flag below lets operators keep
	// the old behaviour for fast iteration but defaults to blocking.
	if c.closeBlockedOnFailed() && reason == "failed" {
		if isInfrastructureExitCode(exitCode) {
			// Infrastructure failure: do not create a fix bead.  The bead
			// will be retried by the scheduler with exponential backoff
			// up to 1 hour, then hourly.  If retries are exhausted we
			// add the infra-exhausted label for visibility.
			log.Printf("[closeBead] Skipping fix bead for %s: infrastructure failure (exit %d)", worker.BeadID.String(), exitCode)
			if bead != nil && c.beadsClient != nil {
				if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, worker.BeadID, []string{"infra-exhausted"}); err != nil {
					log.Printf("[closeBead] Failed to add infra-exhausted label to %s: %v", worker.BeadID.String(), err)
				}
			}
		} else {
			log.Printf("[closeBead] Bead %s closed as failed with close_blocked_on_failed=true; creating fix bead to keep dependents blocked", worker.BeadID.String())
			if err := c.createFixBeadForFailedClose(bead, projectPath, repoName, worker.BeadID.String()); err != nil {
				log.Printf("[closeBead] Failed to create fix bead for %s: %v", worker.BeadID.String(), err)
			}
		}
	}

	// Verified-close gate (WO-2): a bead that "completed" must show real
	// work before it closes, otherwise a worker that reported success
	// without editing anything would close the bead and unblock its
	// dependents. Only the success/"completed" path is gated; failures are
	// handled above. When bead lookup failed we cannot verify, so we fall
	// back to the previous behaviour and close.
	if success && reason == "completed" && c.verifiedCloseEnabled() && bead != nil {
		if err := c.verifyBeforeClose(bead, projectPath, repoName); err != nil {
			// Rejected: the bead was reopened (or escalated) inside
			// verifyBeforeClose. Do not close it.
			return err
		}
	}

	// Commit the chain worktree's changes for this bead before closing, and
	// eagerly integrate singleton worktrees back into the working branch.
	// Multi-bead chains remain event-driven and are NOT integrated here.
	if success && reason == "completed" && c.worktreeManager != nil && c.chainKeyFn != nil && c.isChainMember != nil && bead != nil {
		key := c.chainKeyFn(*bead)
		if wt, err := c.worktreeManager.EnsureWorktree(projectPath, key); err == nil {
			msg := fmt.Sprintf("bead(%s): %s", bead.ID, bead.Title)
			if committed, cErr := c.worktreeManager.Commit(wt, msg); cErr != nil {
				log.Printf("[Worktree] commit failed for %s: %v", bead.ID, cErr)
			} else if committed {
				log.Printf("[Worktree] committed %s", bead.ID)
			}

			// Singletons (beads that belong to no registered multi-bead chain) have
			// no chain-completion event to trigger integration, so integrate now.
			if !c.isChainMember(bead.ID) {
				if iErr := c.worktreeManager.IntegrateAndRemove(projectPath, key); iErr != nil {
					log.Printf("[Worktree] integrate failed for %s: %v", bead.ID, iErr)
				} else {
					log.Printf("[Worktree] integrated singleton %s", bead.ID)
				}
			}
		} else {
			log.Printf("[Worktree] worktree lookup failed for %s: %v", bead.ID, err)
		}
	}

	return c.beadsClient.CloseBead(projectPath, repoName, worker.BeadID, reason)
}

// shouldRunTDDValidation reports whether RunTDDValidation should fire
// for a given bead. Mirrors the legacy `bead.Type == "refactor"` check
// but consults labels so it matches what the task-breakdown runner
// actually emits.
func (c *BeadCloser) shouldRunTDDValidation(bead *types.Bead) bool {
	if bead == nil {
		return false
	}
	for _, label := range bead.Labels {
		switch label {
		case "tdd-red", "tdd-green", "tdd-refactor", "tdd-syntax":
			return true
		}
	}
	// Legacy fallback: still allow callers that build beads by hand
	// with `Type: "refactor"`.
	return bead.Type == "refactor"
}

// closeBlockedOnFailed returns true when failed closures should
// create fix beads instead of allowing dependents to unblock.
func (c *BeadCloser) closeBlockedOnFailed() bool {
	if c.config == nil {
		return true // safe default
	}
	return c.config.Validation.CloseBlockedOnFailed
}

// createFixBeadForFailedClose creates a fix bead that blocks the
// failed bead's dependents. The fix bead re-runs the failed work.
//
// When `bead` is non-nil we read its title + priority for the fix
// bead's metadata. When it's nil (e.g., the bead lookup raced with
// the daemon lock and returned an error), we fall back to the
// worker.BeadID so the fix bead still gets created with a sensible
// title derived from the ID.
//
// MaxDepth guards against recursive fix-bead explosion. Without
// it, a fix bead that itself fails would create another fix bead
// with title "Fix: Fix: GAP: ..." and so on. We count the existing
// "Fix: " prefix on the original bead's title and skip creation
// when it exceeds 2 (i.e., the original gap + 1 retry level).
func (c *BeadCloser) createFixBeadForFailedClose(bead *types.Bead, projectPath, repoName, workerBeadID string) error {
	var title, priorityBeadID string
	priority := 1
	if bead != nil {
		// Recursion guard: skip if the original title already has
		// 3 or more "Fix: " prefixes (i.e., gap → fix → fix → fix
		// → blocked). Further fix beads would only repeat the
		// original work; better to surface the original gap bead.
		if strings.Count(bead.Title, "Fix: ") >= 3 {
			log.Printf("[BeadCloser] Refusing to create nested fix bead for %s (title has %d 'Fix: ' prefixes); deeper recursion would be noise", bead.ID, strings.Count(bead.Title, "Fix: "))
			return nil
		}
		title = "Fix: " + bead.Title
		priorityBeadID = bead.ID.String()
		priority = bead.Priority
	} else {
		// Fallback path: bead lookup failed (daemon lock, transient
		// error, etc.). Synthesize a useful title from the worker
		// bead ID so the fix bead is still meaningful in the audit
		// log.
		title = "Fix: failed-closure bead " + workerBeadID
		priorityBeadID = workerBeadID
	}
	description := "Auto-created fix bead because the original closed as `failed`.\n\n" +
		"Original bead: " + priorityBeadID + "\n" +
		"Close reason: failed (worker exited non-zero or hit retry exhaustion; " +
		"the original bead may still hold placeholder implementations)\n\n" +
		"Re-run the original task and ensure it closes as `completed`."
	req := types.CreateBeadRequest{
		Title:        title,
		Description:  description,
		Type:         "fix",
		Priority:     priority,
		Dependencies: []types.BeadID{types.BeadID(priorityBeadID)},
		Labels:       []string{"auto-fix", "failed-closure"},
	}
	_, err := c.beadsClient.CreateBead(projectPath, req)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

// verifiedCloseEnabled reports whether the verified-close gate is active.
func (c *BeadCloser) verifiedCloseEnabled() bool {
	return c.config != nil && c.config.Validation.Enabled
}

// verifyBeforeClose runs the verified-close checks for a completed bead.
// It returns nil when the bead may close, or a non-nil error (after
// reopening or escalating the bead) when the close must be aborted.
func (c *BeadCloser) verifyBeforeClose(bead *types.Bead, projectPath, repoName string) error {
	// 1. Blocking TDD validation (only gates TDD-phase beads).
	if c.shouldRunTDDValidation(bead) && c.validationService != nil {
		passed, err := c.validationService.TDDChainPasses(*bead, projectPath)
		if err != nil {
			log.Printf("[VerifiedClose] TDD verify error for %s (not blocking): %v", bead.ID, err)
		} else if !passed {
			return c.rejectClose(bead, projectPath, repoName, "tdd-validation-failed")
		}
	}

	// 2. Diff gate + 3. Target-files scope gate. Both gates need the list of
	//    changed files, so compute it once. Failing to read the working tree
	//    is logged and treated as "changed" (fail open) for the diff gate.
	repoPath := c.projectResolver.ResolveRepoPath(bead.Project, repoName, projectPath)
	if repoPath == "" {
		repoPath = projectPath
	}

	targets := beadTargetFiles(bead)
	needDiff := c.config.Validation.RequireDiffOnClose
	needScope := c.config.Validation.TargetScopeMode() != "off"

	var changed []string
	var changedErr error
	if needDiff || needScope {
		changed, changedErr = changedFiles(repoPath)
		if changedErr != nil {
			log.Printf("[VerifiedClose] diff check error for %s (not blocking): %v", bead.ID, changedErr)
		}
	}

	if needDiff && !hasTargetChanges(changed, targets) {
		return c.rejectClose(bead, projectPath, repoName, "empty-diff")
	}

	if needScope {
		mode := c.config.Validation.TargetScopeMode()
		if err := c.checkTargetScope(bead, projectPath, repoName, changed, targets, mode); err != nil {
			return err
		}
	}

	c.logCloseEvent(bead, "accepted", "verified", c.validationRetryCount(bead))
	return nil
}

// checkTargetScope handles the target-files scope gate and the target-less
// warning. It returns a non-nil error only when the configured mode is "block"
// and an off-scope change is found. Target-less implementation beads that edit
// non-allowlisted paths are warned but never rejected.
func (c *BeadCloser) checkTargetScope(bead *types.Bead, projectPath, repoName string, changed, targets []string, mode string) error {
	if len(targets) > 0 {
		violations := scopeViolations(changed, targets)
		if len(violations) == 0 {
			return nil
		}
		if mode == "block" {
			return c.rejectClose(bead, projectPath, repoName, "off-scope changes: "+strings.Join(violations, ", "))
		}
		log.Printf("[ScopeGate] bead %s changed off-scope files: %v", bead.ID, violations)
		return nil
	}

	if !beadTypeShouldDeclareTargets(bead.Type) {
		return nil
	}

	paths := targetlessWarningPaths(changed)
	if len(paths) > 0 {
		log.Printf("[ScopeGate] bead %s declared no Target Files but changed non-allowlisted paths: %v", bead.ID, paths)
	}
	return nil
}

// rejectClose reopens a bead that failed verification, or escalates to a
// human after MaxRounds rejections. It always returns a non-nil error so
// the caller aborts the close.
func (c *BeadCloser) rejectClose(bead *types.Bead, projectPath, repoName, reason string) error {
	beadID := string(bead.ID)

	maxRounds := 3
	if c.config != nil && c.config.Validation.MaxRounds > 0 {
		maxRounds = c.config.Validation.MaxRounds
	}

	currentAttempts := c.validationRetryCount(bead)

	if currentAttempts >= maxRounds {
		// Stop the reject/reopen loop; flag for a human instead of spinning.
		c.logCloseEvent(bead, "escalated", reason, currentAttempts)
		log.Printf("[VerifiedClose] Bead %s rejected %d/%d times (%s); escalating to human", beadID, currentAttempts, maxRounds, reason)
		if c.beadsClient != nil {
			if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"needs-human", "verified-close-escalated"}); err != nil {
				log.Printf("[VerifiedClose] Failed to label %s for escalation: %v", beadID, err)
			}
		}
		return fmt.Errorf("verified-close: bead %s escalated after %d rejections (%s)", beadID, currentAttempts, reason)
	}

	c.logCloseEvent(bead, "rejected", reason, currentAttempts+1)
	log.Printf("[VerifiedClose] Bead %s rejected (%s), round %d/%d — reopening", beadID, reason, currentAttempts+1, maxRounds)
	if c.validationService != nil && c.validationService.retryManager != nil {
		c.validationService.retryManager.IncrementValidationRetry(beadID)
	}
	if c.beadsClient != nil {
		if err := c.beadsClient.UpdateBeadStatus(projectPath, bead.ID, types.StatusOpen); err != nil {
			log.Printf("[VerifiedClose] Failed to reopen %s: %v", beadID, err)
		}
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"verified-close-reject-" + reason}); err != nil {
			log.Printf("[VerifiedClose] Failed to label %s: %v", beadID, err)
		}
	}
	return fmt.Errorf("verified-close: bead %s rejected (%s)", beadID, reason)
}

// logCloseEvent emits a single structured close-decision line for
// downstream metrics (one-shot rate, rejection tracking). model/tokens are
// not available on the worker today, so they are omitted.
func (c *BeadCloser) logCloseEvent(bead *types.Bead, verdict, reason string, attempt int) {
	log.Printf("[CloseEvent] {\"bead_id\":%q,\"verdict\":%q,\"reason\":%q,\"attempt\":%d}",
		string(bead.ID), verdict, reason, attempt)
}

// validationRetryCount returns the validation retry count for a bead,
// or 0 when the validation service or its retry manager is nil. It keeps
// CloseEvent attempt values consistent across accepted, rejected, and
// escalated paths.
func (c *BeadCloser) validationRetryCount(bead *types.Bead) int {
	if c.validationService == nil || c.validationService.retryManager == nil {
		return 0
	}
	return c.validationService.retryManager.GetValidationRetryCount(string(bead.ID))
}

// beadTargetFiles extracts a bead's declared target files so the diff
// check can be scoped. It reads Metadata["files"] then Metadata["target_files"]
// then a "Files:" line in the description. Returns nil when none are declared
// (caller then checks the whole repo).
func beadTargetFiles(bead *types.Bead) []string {
	if bead == nil {
		return nil
	}
	if bead.Metadata != nil {
		// Workers and validators populate different keys; reconcile both
		// so the close-path gate never misses declared targets.
		for _, key := range []string{"files", "target_files"} {
			if v, ok := bead.Metadata[key]; ok && strings.TrimSpace(v) != "" {
				return splitFileList(v)
			}
		}
	}
	for _, line := range strings.Split(bead.Description, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(l), "files:") {
			return splitFileList(l[len("files:"):])
		}
	}
	return nil
}

func splitFileList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		p = strings.TrimSpace(p)
		if p != "" && !strings.EqualFold(p, "TBD") {
			out = append(out, p)
		}
	}
	return out
}
