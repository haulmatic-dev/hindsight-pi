package core

import (
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadCloser closes beads via beadsClient based on worker completion status.
type BeadCloser struct {
	beadsClient      BeadsClient
	intelligenceHub   *IntelligenceHub
	projectResolver   *ProjectResolver
	validationService *ValidationService
	config            *Config
}

// NewBeadCloser creates a new bead closer.
func NewBeadCloser(beadsClient BeadsClient, intelligenceHub *IntelligenceHub, projectResolver *ProjectResolver, validationService *ValidationService, config *Config) *BeadCloser {
	return &BeadCloser{
		beadsClient:      beadsClient,
		intelligenceHub:   intelligenceHub,
		projectResolver:   projectResolver,
		validationService: validationService,
		config:            config,
	}
}

// Close closes the bead via beadsClient based on worker completion status.
//
// Stub fix: the previous version only triggered TDD validation when
// `bead.Type == "refactor"`. In production the task-breakdown runner
// emits all TDD phase beads with `Type: "task"` and encodes the phase
// in labels, so the type check was dead code and validation never ran.
// We now inspect labels (tdd-refactor, tdd-green) and also fire
// validation when the GREEN bead closes, since that is the most likely
// place for a stub to land.
//
// Stub fix: when `close_reason=failed` (worker exited non-zero, hit
// retries, etc.) we no longer let the close silently propagate to
// dependents. Config flag `validation.close_blocked_on_failed` (default
// true) gates this. When set, failed closures trigger a fix bead
// creation flow and the bead's dependents stay blocked on the fix.
//
// Infra-failure guard (2026-06-25): transient infrastructure failures
// (rate limits, timeouts, daemon locks) do not create fix beads.
// The bead stays open and the scheduler retries it with exponential
// backoff up to 1 hour, then hourly thereafter.  Only code-level
// failures (stubs, build errors, etc.) spawn fix beads.
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
	// are validated too because that is where stubs most often land.
	if success && bead != nil && c.shouldRunTDDValidation(bead) {
		if err := c.validationService.RunTDDValidation(*bead, projectPath); err != nil {
			log.Printf("[closeBead] TDD validation error for %s: %v", worker.BeadID.String(), err)
		}
	}

	// Stub fix: failed closures normally let the close proceed, which
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
		"the original bead may still hold stub implementations)\n\n" +
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
