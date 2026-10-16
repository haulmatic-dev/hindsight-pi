package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// getSemanticClusterer lazily builds the embedding-based clustering pass on
// first use (an API key may only be set at close time, and building it
// eagerly would log for every daemon without the pass enabled). Nil when
// disabled or unconfigured: callers treat nil as "string clustering only".
func (c *BeadCloser) getSemanticClusterer() *semanticClusterer {
	if c.config == nil || !c.config.Validation.SemanticClustering.Enabled {
		return nil
	}
	c.semanticOnce.Do(func() { c.semanticClusterer = newSemanticClusterer(c.config.Validation.SemanticClustering) })
	return c.semanticClusterer
}

// BeadCloser closes beads via beadsClient based on worker completion status.
type BeadCloser struct {
	sessionMu          sync.Mutex
	sessionBase        map[string]string // bead ID -> branch HEAD commit recorded at spawn; close-time committed evidence starts here
	integMu            sync.Mutex
	integFails         map[string]int       // bead ID -> consecutive integrate/push failures (reset on success; in-memory like other caps)
	integLastFail      map[string]time.Time // bead ID -> last integrate failure time (rapid-fire contention detection)
	sourceIndex        *BeadProvenanceIndex // nil-safe; bead->store provenance
	beadsClient        BeadsClient
	intelligenceHub    *IntelligenceHub
	projectResolver    *ProjectResolver
	validationService  *ValidationService
	config             *Config
	events             *EventSink              // nil-safe; structured decision stream (Shepherd C1)
	worktreeManager    *WorktreeManager        // per-chain git isolation; nil = disabled
	chainKeyFn         func(types.Bead) string // maps a bead to its chain's worktree key
	isChainMember      func(types.BeadID) bool // reports whether a bead belongs to a registered multi-bead chain
	retainer           *VerdictRetainer        // nil-safe; Hindsight verdict retention (Shepherd D3)
	gateRunner         *GateRunner             // nil until SetGateRunner; daemon_owned projects fail closed without it
	retryManager       *RetryManager           // clears retry residue on verified close
	workerSpawner      *WorkerSpawner          // clears spawn-failure residue on verified close
	sessionResumePilot *SessionResumePilot     // session-file resume quality metrics
	semanticClusterer  *semanticClusterer      // nil-safe; FR-020 semantic (embedding) clustering pass
	semanticOnce       sync.Once
	prWaitingRec       func(PRWaitingRecord) // nil-safe; PR aging sweep ledger (bead hydra-rrtq)
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

// SetEventSink attaches the structured event stream. Nil stays a no-op.
func (c *BeadCloser) SetEventSink(s *EventSink) { c.events = s }

// SetPRWaitingRecorder installs the PR aging sweep's ledger recorder. Called
// whenever a close leaves an integration PR open waiting on a human merge.
// nil = recording disabled (tools, tests without HYDRA_HOME).
func (c *BeadCloser) SetPRWaitingRecorder(fn func(PRWaitingRecord)) {
	if c == nil {
		return
	}
	c.prWaitingRec = fn
}

// recordPRWaitingCapture captures one waiting-on-human-merge PR into the
// ledger (nil-safe, fail-open).
func (c *BeadCloser) recordPRWaitingCapture(bead *types.Bead, projectPath, url string) {
	if c == nil || c.prWaitingRec == nil || bead == nil || url == "" {
		return
	}
	owner := ""
	if id, err := resolvePublishIdentityFor(projectPath); err == nil {
		owner = id.Owner
	}
	c.prWaitingRec(PRWaitingRecord{
		BeadID: string(bead.ID), ProjectPath: projectPath,
		Owner: owner, URL: url, Since: time.Now(),
	})
}

// ownerTokenForPublish resolves the bead owner's publish identity for the
// project and emits the typed identity event (publish_identity_resolved /
// publish_identity_missing). Fail-closed: an unresolved identity is an error.
func (c *BeadCloser) ownerTokenForPublish(projectPath string, bead types.Bead) (string, error) {
	id, err := resolvePublishIdentityFor(projectPath)
	if err != nil {
		if c.events != nil {
			c.events.EmitRepo(projectPath, Event{Type: "publish_identity_missing", Bead: string(bead.ID), Reason: "owner_pat_unresolved", Detail: err.Error()})
		}
		return "", err
	}
	if c.events != nil {
		c.events.EmitRepo(projectPath, Event{Type: "publish_identity_resolved", Bead: string(bead.ID), Fields: map[string]any{"owner": id.Owner, "token_source": id.Source}})
	}
	return id.Token, nil
}

// SetVerdictRetainer attaches Hindsight verdict retention (Shepherd D3).
// Nil stays a no-op.
func (c *BeadCloser) SetVerdictRetainer(r *VerdictRetainer) { c.retainer = r }

// SetRetryManager wires the retry manager so verified closes can purge
// retry state (fault/validation/spawn residue) and keep dead beads out of
// Pan candidacy.
func (c *BeadCloser) SetRetryManager(rm *RetryManager) { c.retryManager = rm }

// SetWorkerSpawner wires the worker spawner so verified closes can purge
// spawn-failure residue.
func (c *BeadCloser) SetWorkerSpawner(s *WorkerSpawner) { c.workerSpawner = s }

// SetSessionResumePilot wires the session-file resume pilot for outcome
// metrics and auto-disable.
func (c *BeadCloser) SetSessionResumePilot(p *SessionResumePilot) { c.sessionResumePilot = p }

// retainFailedClose retains a failed close as a Hindsight memory when a
// failure cause was extracted. Fail-open: nil retainer or nil memory (no bead,
// empty cause) does nothing. Emits verdict_retained only on an actual retain.
func (c *BeadCloser) retainFailedClose(bead *types.Bead, worker *types.Worker, exitCode int, cause string) {
	project := ""
	if bead != nil {
		project = bead.Project
	}
	mem := buildFailedCloseMemory(project, bead, cause, exitCode)
	if mem == nil || c.retainer == nil {
		return
	}
	c.retainer.Retain(mem)
	c.events.Emit(Event{Type: "verdict_retained", Project: project,
		Bead: worker.BeadID.String(), Reason: "failed_close",
		Fields: map[string]any{"kind": "failed_close", "exit_code": exitCode}})
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

// closeGateWorktree resolves the bead's chain worktree for close-time gates;
// "" when the worktree is gone (gates then fall back to the project path).
// RunVerificationGateForRevalidation reruns the bead's declared verification
// command in its chain worktree (same resolution as the close-time guard) on
// behalf of the post-close revalidation sweep. ran=false means there was
// nothing to run — no declared command, no gate runner, or a runner fault —
// and the caller must leave the closed-failed label in place (fail-closed:
// only positive green evidence overturns a failure verdict).
// worktreePath pins the gate directory when the caller already resolved the
// correct tree (the quarantine reconciler's scratch checkout of the
// quarantine branch tip); empty falls back to the close-time resolution.
func (c *BeadCloser) RunVerificationGateForRevalidation(bead *types.Bead, projectPath, worktreePath string) (passed, ran bool) {
	cmd, ok := verificationCommandFor(bead)
	if !ok || c.gateRunner == nil {
		return false, false
	}
	gatePath := projectPath
	if worktreePath != "" {
		gatePath = worktreePath
	} else if wt := closeGateWorktree(c, projectPath, bead); wt != "" {
		gatePath = wt
	}
	a, err := c.gateRunner.RunGate(GateRequest{
		Project: bead.Project, BeadID: bead.ID.String(), Gate: "verification-command",
		// Revalidation is a background operation, not a close gate: a full-repo
		// coverage suite legitimately runs past the 10-minute gate default, and
		// a timeout would read as RED and keep a truthful verdict from
		// overturning the stale one.
		Spec:              GateSpec{Cmd: cmd, Parser: "exit-code", Timeout: 90 * time.Minute},
		ProjectPath:       projectPath,
		WorktreePath:      gatePath,
		AllowMainCheckout: true,
	})
	if err != nil {
		log.Printf("[VerificationCmd] %s: revalidation runner fault (label stands): %v", bead.ID, err)
		return false, false
	}
	return a.Status == GateStatusPassed, true
}

func closeGateWorktree(c *BeadCloser, projectPath string, bead *types.Bead) string {
	if c.worktreeManager == nil || c.chainKeyFn == nil {
		return ""
	}
	if wt, ok := c.worktreeManager.ExistingWorktree(projectPath, c.chainKeyFn(*bead)); ok {
		return wt
	}
	return ""
}

func (c *BeadCloser) Close(worker *types.Worker, success bool, exitCode int) error {
	projectName := worker.BeadID.Project()
	repoName := worker.BeadID.Repo()

	log.Printf("[closeBead] Closing bead %s (project: %s, repo: %s)",
		worker.BeadID.String(), projectName, repoName)

	projectPath := c.projectResolver.ResolveProjectPathByWorker(worker, projectName)
	if projectPath == "" && c.sourceIndex != nil {
		// Provenance: the store that listed this bead wins over name
		// matching when several projects share a repo name.
		projectPath = c.sourceIndex.Lookup(worker.BeadID.String())
	}
	if projectPath == "" {
		// Synthetic workers (Pan's abandon-with-summary builds one with no
		// registry row and no ProjectPath) cannot resolve through the
		// registry-backed resolver. Fall back to the config resolver — the
		// same #225-fixed resolution family the Pan close paths use — so
		// shepherd-initiated closes work for every registered project.
		// Observed live 2026-09-15: abandoning stale hydra beads failed
		// with "project hydra not found" even though the config registers
		// it.
		projectPath = resolveProjectPathByName(c.config, projectName, repoName)
	}
	if projectPath == "" {
		return &ProjectNotFoundError{Project: projectName}
	}

	reason := statusReason(success)
	log.Printf("[closeBead] Closing with reason: %s (path: %s)", reason, projectPath)

	bead, err := c.beadsClient.GetBead(projectPath, repoName, worker.BeadID)
	if err != nil {
		return fmt.Errorf("[closeBead] cannot verify state of bead %s: %w", worker.BeadID.String(), err)
	}
	if bead == nil {
		return fmt.Errorf("[closeBead] bead %s not found; cannot verify state before closing", worker.BeadID.String())
	}
	if bead.Status.IsTerminal() {
		log.Printf("[closeBead] Bead %s already closed (status: %s), skipping", worker.BeadID.String(), bead.Status)
		return nil
	}

	// Human gates are never system-closed: approval beads require an
	// explicit human `br close`. Observed live 2026-08-06: this path
	// closed srv-smart-engine-56z with verdict "accepted" after a worker
	// review, bypassing the human gate.
	if bead != nil && (bead.Type == "approval" || bead.Type == "approval-gate") {
		log.Printf("[closeBead] REFUSING to close human gate %s (type %s) — requires explicit human br close",
			worker.BeadID.String(), bead.Type)
		return nil
	}

	// Queue TLDR indexing on successful completion
	if success {
		repoPath := c.projectResolver.ResolveRepoPath(projectName, repoName, projectPath)
		if repoPath != "" {
			c.intelligenceHub.QueueRepoIndexing(projectName, repoPath)
		}
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
		cause := ExtractFailureCause(workersLogDir(), worker.BeadID.String(), 2048)
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
			if err := c.createFixBeadForFailedCloseWithExitCode(bead, projectPath, repoName, worker.BeadID.String(), exitCode); err != nil {
				log.Printf("[closeBead] Failed to create fix bead for %s: %v", worker.BeadID.String(), err)
			}
		}
		c.retainFailedClose(bead, worker, exitCode, cause)
	}

	// Verified-close gate (WO-2): a bead that "completed" must show real
	// work before it closes, otherwise a worker that reported success
	// without editing anything would close the bead and unblock its
	// dependents. Only the success/"completed" path is gated; failures are
	// handled above. When bead lookup failed we cannot verify, so we fall
	// back to the previous behaviour and close.
	// Close-time hard gates (FR-002): deterministic checks (executable
	// acceptance, quarantine contradiction, dependency order) run before the
	// VerifiedClose round so a gate failure never costs an LLM judge round.
	if success && reason == "completed" && bead != nil {
		// Resolve the chain worktree once for every close-time gate: the
		// executable-acceptance command is repo-relative (npm run test:e2e)
		// and MUST run where the worker's code and node_modules live.
		// Passing "" ran it against the main checkout — older package.json,
		// no node_modules — producing "Missing script"/exit-127 rejections
		// that escalated six srv-jobfile beads (2026-10-05).
		gateWt := ""
		if c.worktreeManager != nil && c.chainKeyFn != nil {
			if wt, ok := c.worktreeManager.ExistingWorktree(projectPath, c.chainKeyFn(*bead)); ok {
				gateWt = wt
			}
		}
		if err := c.runCloseHardGates(bead, projectPath, repoName, gateWt); err != nil {
			return err
		}
	}

	if success && reason == "completed" && c.verifiedCloseEnabled() && bead != nil {
		if err := c.verifyBeforeClose(bead, projectPath, repoName); err != nil {
			// Rejected: the bead was reopened (or escalated) inside
			// verifyBeforeClose. Do not close it.
			return err
		}
	}

	// Daemon-owned deterministic gates (FR-002): on the success path, run
	// the project's declared gates daemon-side in the chain worktree BEFORE
	// commit/integrate/close. Zero LLM calls here; pass/fail comes from the
	// persisted artifact. Gate failure reopens the bead with the parsed
	// summary + raw log path instead of closing on agent-quoted output.
	if success && reason == "completed" && bead != nil {
		if err := c.runDaemonGates(bead, projectName, projectPath, repoName, closeGateWorktree(c, projectPath, bead)); err != nil {
			return err
		}
		// Re-check terminality after the (potentially minutes-long) gate run:
		// a duplicate close can start while the first is still running gates.
		// Observed live 2026-09-27 (hydra-kf2t): Charon orphan recovery
		// respawned a worker because the bead stayed in_progress during a
		// seven-minute daemon gate, producing a second close that raced the
		// first into integrate. Skip instead of double-integrating.
		if fresh, err := c.beadsClient.GetBead(projectPath, repoName, bead.ID); err == nil && fresh != nil && fresh.Status.IsTerminal() {
			log.Printf("[closeBead] Bead %s closed while gates ran (status: %s); skipping integrate/close", bead.ID, fresh.Status)
			return nil
		}
		// Escape-rate regression budget (NFR-001): the bead's own declared
		// verification command (metadata test_command / "Test Command:") must
		// exit 0 before the bead can reach closed-completed through ANY close
		// path on this success route — verified-close and singleton integrate
		// both pass through here. Nonzero exit rejects the close (reopen).
		if err := c.verificationCommandGuard(bead, projectPath); err != nil {
			return c.rejectClose(bead, projectPath, repoName, "verification-command-failed")
		}
	}

	// Commit the chain worktree's changes for this bead before closing, and
	// eagerly integrate singleton worktrees back into the working branch.
	// Multi-bead chains remain event-driven and are NOT integrated here.
	if success && reason == "completed" && c.worktreeManager != nil && c.chainKeyFn != nil && c.isChainMember != nil && bead != nil {
		key := c.chainKeyFn(*bead)
		if wt, err := c.worktreeManager.EnsureWorktree(projectPath, key); err == nil {
			// Readable, conventional-commit message derived from the bead
			// (type/title/description/PRD) instead of the bare "bead(id): title".
			msg := BuildCommitMessage(*bead)
			if committed, cErr := c.worktreeManager.CommitBead(wt, msg, string(bead.ID)); cErr != nil {
				return c.failIntegration(bead, projectPath, repoName, "commit", cErr)
			} else if committed {
				log.Printf("[Worktree] committed %s", bead.ID)
				AppendActivity(projectPath, string(bead.ID), "committed")
			} else {
				// Loud no-op: nothing to commit here with an accepted close
				// means the work never reached the worktree — the signature
				// of the 2026-09-16 nipuna discard. Silence hid it for days.
				log.Printf("[Worktree] %s: nothing to commit in worktree (close evidence falls back to main checkout)", bead.ID)
			}

			// Singletons (beads that belong to no registered multi-bead chain) have
			// no chain-completion event to trigger integration, so integrate now.
			// Fail-closed: the merge is verified inside Integrate, the push is
			// verified inside publishAfterIntegrate (ls-remote), and the worktree
			// is removed ONLY after both land. Either failure aborts the close,
			// emits integration-failed, and leaves the bead open with the
			// worktree preserved (hydra-xnx 2026-09-06: cleanup ran after a fake
			// success and the gate-verified commits dangled unreferenced).
			member := c.isChainMember(bead.ID)
			if member {
				log.Printf("[Worktree] close %s: key=%s chainMember=true — integration deferred to chain completion (reconciler is the stranded-work net)", bead.ID, key)
			} else {
				log.Printf("[Worktree] close %s: key=%s chainMember=false — singleton, integrating now", bead.ID, key)
			}
			if !member {
				// FR-010 PR-first close contract: when the project has a remote,
				// the singleton's worktree branch is pushed to origin and landed
				// via PR — no local merge into the working branch. The daemon
				// gates already ran above, so the PR merge happens gates-green.
				// Per-repo integration gate: serialize sibling closes so only
				// one bead at a time merges into the same base branch, and wait
				// out a short repo cooldown after a sibling's failure (shared
				// wedge must not burn every bead's exhaustion cap).
				integErr := integrationGate(projectPath, func() error {
					if hasGitRemote(projectPath) {
						if iErr := c.publishSingletonViaPR(projectPath, bead, key); iErr != nil {
							return errStage{"integrate", iErr}
						}
					} else {
						// No-remote fallback: legacy local --no-ff merge, then the
						// ordinary push path (a no-op when push is disabled).
						if iErr := c.worktreeManager.Integrate(projectPath, key); iErr != nil {
							return errStage{"integrate", iErr}
						}
						if pErr := c.publishAfterIntegrate(projectPath, bead); pErr != nil {
							return errStage{"push", pErr}
						}
					}
					return nil
				})
				if integErr != nil {
					es := integErr.(errStage)
					return c.failIntegration(bead, projectPath, repoName, es.stage, es.err)
				}
				resetIntegrationCooldown(projectPath)
				c.resetIntegrationFailures(string(bead.ID))
				c.worktreeManager.Remove(projectPath, key)
				log.Printf("[Worktree] integrated singleton %s", bead.ID)
				AppendActivity(projectPath, string(bead.ID), "integrated")
			}
		} else {
			log.Printf("[Worktree] worktree lookup failed for %s: %v", bead.ID, err)
		}
	}

	// File-backed close verdict: issues.jsonl has no close-reason field, so a
	// failed close is recorded as a label. The chain-completion detector reads
	// this to decide integrate-vs-quarantine, and it survives daemon restarts —
	// an in-memory map would not (proven: four mid-run restarts this week).
	if reason == "failed" && bead != nil && c.beadsClient != nil {
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{closedFailedLabel}); err != nil {
			log.Printf("[closeBead] Failed to add %s label to %s: %v", closedFailedLabel, bead.ID, err)
		}
	}
	// Close-time coherence: a completed close contradicts a pre-existing
	// closed-failed verdict on the same bead (mqn, 2026-10-16: closed with
	// reason "completed" while carrying closed-failed — the stale label then
	// quarantined a 132-bead chain whose PRD validation had passed). A
	// completed close scrubs the stale verdict and marks it revalidated.
	if reason == "completed" && bead != nil && c.beadsClient != nil && hasBeadLabel(bead.Labels, closedFailedLabel) {
		if err := c.beadsClient.RemoveBeadLabels(projectPath, repoName, bead.ID, []string{closedFailedLabel}); err != nil {
			log.Printf("[closeBead] Failed to scrub stale %s label from %s: %v", closedFailedLabel, bead.ID, err)
		} else {
			if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{closedFailedRevalidatedLabel}); err != nil {
				log.Printf("[closeBead] Failed to add %s label to %s: %v", closedFailedRevalidatedLabel, bead.ID, err)
			}
			log.Printf("[closeBead] %s closed completed — scrubbed stale %s label", bead.ID, closedFailedLabel)
			c.events.EmitRepo(projectPath, Event{Type: "label_revalidated", Project: repoName,
				Bead: bead.ID.String(), Reason: "completed_close_scrubs_closed_failed"})
		}
	}
	c.events.EmitRepo(projectPath, Event{Type: "bead_closed", Project: repoName,
		Bead: worker.BeadID.String(), Reason: reason,
		Fields: map[string]any{"exit_code": exitCode, "success": success}})
	// Sensor symmetry (resolve): the verified-close sensor's paired resolve
	// for every verified_close_rejected fire on this bead. Correlation is
	// deterministic per bead, so fires and resolves pair across restarts.
	if success && reason == "completed" && c.verifiedCloseEnabled() {
		c.events.EmitRepo(projectPath, Event{Type: "verified_close_passed", Project: repoName,
			Bead: worker.BeadID.String(), Reason: "completed",
			Detail: fmt.Sprintf("bead %s closed verified after validation", worker.BeadID.String()),
			Fields: map[string]any{"correlation_id": SensorCorrelation("verified-close", worker.BeadID.String())}})
	}

	if err := c.beadsClient.CloseBead(projectPath, repoName, worker.BeadID, reason); err != nil {
		return err
	}

	// FR-020: when a meta fix bead closes (any reason), release the parked
	// originals that were clustered against its failure signature.
	if bead != nil && c.fixClusteringEnabled() {
		c.releaseParkedBeads(projectPath, repoName, bead)
	}

	// Purge scheduler residue for verified closes so closed beads cannot
	// re-enter Pan candidacy or respawn decisions via stale retry/spawn
	// records. Failure here is logged but must not fail the close.
	if reason == "completed" {
		c.purgeCloseState(worker.BeadID)
	}
	if reason == "completed" && c.sessionResumePilot != nil && bead != nil {
		c.sessionResumePilot.RecordSuccess(hasBeadLabel(bead.Labels, sessionResumedLabel))
	}
	return nil
}

// purgeCloseState clears retry-manager and spawner residue for a bead that
// has just closed successfully. Keeps terminal beads from lingering in
// scheduler-level blocked-bead sources.
func (c *BeadCloser) purgeCloseState(beadID types.BeadID) {
	id := beadID.String()
	if c.retryManager != nil {
		c.retryManager.Clear(id)
		c.retryManager.ClearValidationRetry(id)
	}
	if c.workerSpawner != nil {
		c.workerSpawner.ClearSpawnFailure(id)
	}
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
// fixTitlePrefix is how an auto-created fix bead marks its title. Counting it is
// how nesting depth is detected.
const fixTitlePrefix = "Fix: "

// maxFixNesting caps auto-created fix beads at one per original — no nesting.
//
// Measured on run H: the previous cap of 3 produced 158 fix beads against 18 real
// tasks, with titles nested three deep ("Fix: Fix: Fix: NFR-001: ..."). Each extra
// level re-ran identical instructions through the same mechanism that had just
// failed, consuming a worker slot and provider quota per attempt.
const maxFixNesting = 1

// fixRecursionCappedLabel marks a bead whose fix bead was refused, so the outcome
// is visible instead of the bead quietly closing as failed and unblocking its
// dependents.
const fixRecursionCappedLabel = "fix-recursion-capped"

// closedFailedLabel marks a bead that closed with reason "failed". The beads
// store keeps no close reason, so this label is the durable record other
// components (chain verdicts, the Shepherd cron) read.
const closedFailedLabel = "closed-failed"

// closedFailedRevalidatedLabel marks a bead whose closed-failed verdict was
// later overturned by positive evidence (a completed close, or a green gate
// rerun by the post-close revalidation sweep). Audit trail only — the
// chain verdict keys on closedFailedLabel alone.
const closedFailedRevalidatedLabel = "closed-failed-revalidated"

// integrationExhaustedLabel marks a bead whose singleton integrate/push
// failed maxConsecutiveIntegrationFailures times in a row. Like
// fix-recursion-capped it is a durable cap: every respawn path (scheduler
// retry queue, DAG ready filter, Charon orphan recovery) must skip beads
// carrying it until Pan repair or a human clears the label.
const integrationExhaustedLabel = "integration-exhausted"

// integrationPendingLabel marks a bead whose WORK is complete (close gates
// and verification passed) but whose integrate/push step failed — typically
// on a transient remote condition (GitHub API 403, rate limits, org flaps).
// Instead of reopening the bead to the ready queue — which respawns a FULL
// LLM worker run (session resume is disabled, so ~11 spawns/45min per bead
// were burned re-doing finished work on srv-jobfile-65l+5my, 2026-10-02) —
// the bead stays parked in place and the mechanical integration-retest
// stage owns the retry. Every respawn path (scheduler retry queue, DAG
// ready filter, bead discovery, Charon orphan recovery, reconcileOrphanClaim)
// must skip beads carrying it; the retest stage and Pan repair (via
// integration-exhausted at the cap) are the only exits.
const integrationPendingLabel = "integration-pending"

// maxConsecutiveIntegrationFailures caps how many integrate/push failures a
// bead may accumulate before failIntegration escalates it to the durable
// integration-exhausted hard-park instead of a plain reopen. Uncapped, every
// failed close respawned the bead into the same failing integrate — webapp-9ah
// burned four full worker cycles in four minutes this way (2026-09-16).
const maxConsecutiveIntegrationFailures = 3

// integrationBackoffBase is the delay before the first integrate retry; it
// grows exponentially per consecutive failure. A retry arriving sooner than
// the base is rapid-fire contention and does not advance the cap. Var, not
// const: tests shrink it — the real sleeps (30s→210s cumulative) dominated
// the internal/core suite (2026-09-27 gate runs, 444s of 600s budget).
var integrationBackoffBase = 30 * time.Second

// integrationBackoffMax caps the exponential backoff between integrate retries.
const integrationBackoffMax = 8 * time.Minute

// extractConflictPaths pulls the file paths out of a git merge failure's
// stderr ("CONFLICT (content): Merge conflict in CHANGELOG.md" etc.) so the
// exhaustion event names the contended files. Fail-open: no matches = nil.
func extractConflictPaths(detail string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, line := range strings.Split(detail, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "CONFLICT") {
			continue
		}
		for _, sep := range []string{"Merge conflict in ", "add/rm conflict in ", "rename/rename conflict in ", "modify/delete: "} {
			if idx := strings.Index(line, sep); idx >= 0 {
				path := strings.TrimSpace(line[idx+len(sep):])
				if i := strings.IndexAny(path, " \t(:"); i >= 0 {
					path = strings.TrimRight(path[:i], " :(")
				}
				path = strings.Trim(path, " :(")
				if path != "" && !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
				break
			}
		}
	}
	return paths
}

// escalatedLabel is the durable marker for beads a verified-close rejection
// escalated to a human at max attempts. Unlike a status, a label is visible
// to every consumer that decides respawn: Charon orphan recovery, the
// scheduler ready filter, and the retry-eligible path must all skip beads
// carrying it (2026-09-06: empty-diff churn loop — escalation left the bead
// bare in_progress, Charon reopened it, it respawned into the same death).
const escalatedLabel = "escalated"

// panFiledLabel is declared in bead_discovery.go (same package); it mirrors
// internal/pan.FiledLabel. Charon must not reopen pan-filed beads for orphan
// recovery (triage beads legitimately sit in_progress without a worker).

// hasBeadLabel reports whether labels contains label.
func hasBeadLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

func (c *BeadCloser) createFixBeadForFailedClose(bead *types.Bead, projectPath, repoName, workerBeadID string) error {
	return c.createFixBeadForFailedCloseWithExitCode(bead, projectPath, repoName, workerBeadID, 0)
}

func (c *BeadCloser) createFixBeadForFailedCloseWithExitCode(bead *types.Bead, projectPath, repoName, workerBeadID string, exitCode int) error {
	var title, priorityBeadID string
	priority := 1
	if bead != nil {
		// Recursion guard. One fix bead per original, and no nesting.
		//
		// This used to allow three levels, which is how run H produced titles
		// like "Fix: Fix: Fix: NFR-001: The impact path stays cheap and offline"
		// and 158 fix beads against 18 real tasks. A fix bead exists to redo work
		// that failed; if the fix itself fails, creating another copy re-runs the
		// same instructions through the same mechanism and fails the same way.
		// The second and third attempts were pure churn: they consumed workers,
		// tokens and provider quota, and buried the original gap.
		if depth := strings.Count(bead.Title, fixTitlePrefix); depth >= maxFixNesting {
			log.Printf("[BeadCloser] Refusing to create a nested fix bead for %s (title already has %d %q prefix(es), cap %d). "+
				"The fix mechanism has already failed on this work; labelling for escalation instead of re-running it.",
				bead.ID, depth, fixTitlePrefix, maxFixNesting)
			// Label it. Without this the bead simply closes as failed and its
			// dependents unblock — the exact regression close_blocked_on_failed
			// exists to prevent — with no record of why no fix bead appeared.
			if c.beadsClient != nil {
				if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{fixRecursionCappedLabel}); err != nil {
					log.Printf("[BeadCloser] Failed to label %s with %s: %v", bead.ID, fixRecursionCappedLabel, err)
				}
				c.events.EmitRepo(projectPath, Event{Type: "fix_capped", Project: repoName,
					Bead: bead.ID.String(), Reason: "recursion_capped",
					Detail: "fix bead refused: the fix mechanism already failed on this work"})
			}
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
	// Dedup before create: one open/in-progress fix bead per original.
	if bead != nil {
		dupID, err := c.findOpenFixBeadForOriginal(projectPath, repoName, types.BeadID(priorityBeadID), title)
		if err != nil {
			log.Printf("[BeadCloser] Could not list beads for dedup while creating fix bead for %s: %v", priorityBeadID, err)
		} else if dupID != "" {
			log.Printf("[BeadCloser] Skipping duplicate fix bead for original %s; existing open fix bead %s already tracks it", priorityBeadID, dupID)
			c.events.EmitRepo(projectPath, Event{Type: "fix_deduped", Project: repoName,
				Bead: priorityBeadID, Reason: "duplicate_fix_exists",
				Fields: map[string]any{"existing_fix": dupID}})
			return nil
		}
	}

	// FR-020: failure-signature clustering. Extract the cause once, then
	// check for an open meta fix bead carrying the same failure signature.
	// One signature = one meta bead; later failures with the same signature
	// are parked against it instead of spawning another per-bead fix bead
	// (570 integrate failures from a handful of clusters produced 86
	// quarantine branches before this). List errors fail open: when we
	// cannot list beads we fall through to the legacy per-bead fix bead.
	var cause string
	var fixSigLabel string
	clusteringActive := c.fixClusteringEnabled()
	if clusteringActive {
		cause = ExtractFailureCause(workersLogDir(), priorityBeadID, 2048)
		sigLabel := SignatureLabel(FailureSignature("worker-exit", cause))
		metaID, err := c.findOpenFixBeadBySignature(projectPath, repoName, sigLabel)
		if err != nil {
			log.Printf("[BeadCloser] Could not list beads for signature clustering while creating fix bead for %s: %v", priorityBeadID, err)
		} else if metaID != "" {
			log.Printf("[BeadCloser] Parking %s against meta fix bead %s (signature %s) instead of creating a new per-bead fix bead", priorityBeadID, metaID, sigLabel)
			if c.beadsClient != nil {
				if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{fixClusteredLabel, sigLabel}); err != nil {
					log.Printf("[BeadCloser] Failed to label %s with %s: %v", bead.ID, fixClusteredLabel, err)
				}
				AppendActivity(projectPath, string(bead.ID), fmt.Sprintf("parked: open meta fix bead %s tracks this failure signature", metaID))
				c.events.EmitRepo(projectPath, Event{Type: "fix_clustered", Project: repoName,
					Bead: priorityBeadID, Reason: "meta_fix_exists",
					Fields: map[string]any{"meta_fix": metaID, "signature": sigLabel}})
			}
			return nil
		}
		// No open meta bead with this signature. FR-020 follow-up (bead
		// hydra-n5qt): before filing a new meta bead, run the semantic
		// (embedding) pass — exact string equality split one root cause
		// into 4 clusters during the 2026-09-29 telematics drain. Any
		// embedding failure fails open: none of this blocks filing.
		if sc := c.getSemanticClusterer(); sc != nil {
			semCtx, semCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer semCancel()
			openMeta := c.openMetaFixBeads(projectPath, repoName)
			if len(openMeta) > 0 {
				cands := make([]semanticCandidate, 0, len(openMeta))
				for _, m := range openMeta {
					mSig := firstFixSigLabel(m.Labels)
					cands = append(cands, semanticCandidate{
						id:    m.ID,
						sig:   mSig,
						cause: causeFromFixDescription(m.Description),
					})
				}
				if dup := sc.findSemanticDuplicate(semCtx, sigLabel, cause, cands); dup != "" {
					log.Printf("[BeadCloser] Parking %s against meta fix bead %s (semantic match) instead of creating a new per-bead fix bead", priorityBeadID, dup)
					if c.beadsClient != nil {
						if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{fixClusteredLabel, sigLabel}); err != nil {
							log.Printf("[BeadCloser] Failed to label %s with %s: %v", bead.ID, fixClusteredLabel, err)
						}
						AppendActivity(projectPath, string(bead.ID), fmt.Sprintf("parked: open meta fix bead %s tracks this failure (semantic cluster match)", dup))
						c.events.EmitRepo(projectPath, Event{Type: "fix_semantic_clustered", Project: repoName,
							Bead: priorityBeadID, Reason: "semantic_duplicate",
							Fields: map[string]any{"meta_fix": dup, "signature": sigLabel}})
					}
					return nil
				}
			}
		}
		// Still no cluster: the fix bead we are about to
		// create becomes the cluster's meta bead and carries the signature label.
		fixSigLabel = sigLabel
	}

	var description strings.Builder
	fmt.Fprintf(&description, "Auto-created fix bead because the original closed as `failed`.\n\n"+
		"Original bead: %s\n"+
		"Close reason: failed (worker exited non-zero or hit retry exhaustion; the original bead may still hold placeholder implementations)\n"+
		"Exit code: %d\n\n", priorityBeadID, exitCode)

	if cause == "" {
		cause = ExtractFailureCause(workersLogDir(), priorityBeadID, 2048)
	}
	if cause != "" {
		description.WriteString("### What failed last time\n\n")
		description.WriteString("```\n")
		description.WriteString(cause)
		description.WriteString("\n```\n\n")
	}
	// Include the original attempt's recorded HYDRA_VERDICT verbatim (observed
	// 2026-09-25: tin0, fix bead for jx78, spent 60+ min rewriting work the
	// original had already committed green — its done verdict was never shown
	// to the fix worker). Without it the fix worker cannot tell finished work
	// from broken work.
	if verdict := ExtractRecordedVerdict(workersLogDir(), priorityBeadID, 600); verdict != "" {
		description.WriteString("### Original verdict already recorded (verify, do not blindly redo)\n\n")
		description.WriteString("```\n")
		description.WriteString(verdict)
		description.WriteString("\n```\n\n")
	}
	description.WriteString("VERIFY FIRST: re-run the original task's verification commands against the worktree you inherited. If they all pass, close this fix bead immediately citing that evidence and do NOT modify files. Only redo what verification proves broken.")

	req := types.CreateBeadRequest{
		Title:        title,
		Description:  description.String(),
		Type:         "fix",
		Priority:     priority,
		Dependencies: []types.BeadID{types.BeadID(priorityBeadID)},
		Labels:       []string{"auto-fix", "failed-closure"},
	}
	if fixSigLabel != "" {
		req.Labels = append(req.Labels, fixSigLabel)
	}
	newID, err := c.beadsClient.CreateBead(projectPath, req)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	if err == nil {
		c.events.EmitRepo(projectPath, Event{Type: "fix_created", Project: repoName,
			Bead: string(newID), Reason: "failed_close",
			Fields: map[string]any{"original": priorityBeadID, "exit_code": exitCode}})
	}
	if c.sessionResumePilot != nil && bead != nil {
		c.sessionResumePilot.RecordFix(hasBeadLabel(bead.Labels, sessionResumedLabel))
	}
	return nil
}

// findOpenFixBeadForOriginal returns the ID of an existing open or in-progress
// fix bead for the given original. If none exists, it returns an empty ID and
// a nil error. Listing errors are returned so the caller can decide whether to
// fail open or log and continue.
//
// Matching is by exact fix title first, dependency second: `br list --json`
// returns dependencies as null (only dependency_count is populated), so the
// dependency check alone never matched anything — dedup was structurally dead
// from the day it shipped (fix_deduped events: zero across every run; run D3
// created four identical fix beads for one original). The title of a fix bead
// is deterministic ("Fix: " + original title), so an exact title match on a
// non-terminal fix bead identifies a duplicate without a per-bead `br show`.
func (c *BeadCloser) findOpenFixBeadForOriginal(projectPath, repoName string, originalID types.BeadID, fixTitle string) (types.BeadID, error) {
	if c.beadsClient == nil {
		return "", nil
	}
	beads, err := c.beadsClient.ListBeads(projectPath, repoName)
	if err != nil {
		return "", err
	}
	for _, b := range beads {
		if b.Type != "fix" || b.Status.IsTerminal() {
			continue
		}
		if fixTitle != "" && b.Title == fixTitle {
			return b.ID, nil
		}
		for _, dep := range b.Dependencies {
			if dep == originalID {
				return b.ID, nil
			}
		}
	}
	return "", nil
}

// fixClusteringEnabled reports whether failure-signature clustering
// (FR-020) is active. Default is on; disable_fix_signature_clustering
// restores the legacy one-fix-bead-per-failure behaviour.
func (c *BeadCloser) fixClusteringEnabled() bool {
	return c.config == nil || !c.config.Validation.DisableFixSignatureClustering
}

// findOpenFixBeadBySignature returns the ID of an open or in-progress meta
// fix bead carrying the given signature label. Empty ID + nil error when
// none exists.
func (c *BeadCloser) findOpenFixBeadBySignature(projectPath, repoName, sigLabel string) (types.BeadID, error) {
	if c.beadsClient == nil {
		return "", nil
	}
	beads, err := c.beadsClient.ListBeads(projectPath, repoName)
	if err != nil {
		return "", err
	}
	for _, b := range beads {
		if b.Type != "fix" || b.Status.IsTerminal() {
			continue
		}
		if hasBeadLabel(b.Labels, sigLabel) {
			return b.ID, nil
		}
	}
	return "", nil
}

// releaseParkedBeads clears the fix-clustered label from every open bead
// parked against a closed meta fix bead, so its dependents and the
// discovery loop can pick the parked originals up again (PRD FR-020:
// "on meta-bead close, parked beads are released"). Fail-open: a release
// error is logged and must not fail the meta bead's own close.
func (c *BeadCloser) releaseParkedBeads(projectPath, repoName string, meta *types.Bead) {
	if c.beadsClient == nil || meta == nil {
		return
	}
	for _, label := range meta.Labels {
		if !strings.HasPrefix(label, "fix-sig-") {
			continue
		}
		beads, err := c.beadsClient.ListBeads(projectPath, repoName)
		if err != nil {
			log.Printf("[BeadCloser] Could not list beads to release parked originals of meta fix bead %s: %v", meta.ID, err)
			return
		}
		for _, b := range beads {
			// Parked originals are usually closed-failed (that is why they
			// were being fixed) — do NOT skip terminal beads here; release
			// must reopen them (PRD FR-020).
			if !hasBeadLabel(b.Labels, fixClusteredLabel) || !hasBeadLabel(b.Labels, label) {
				continue
			}
			if err := c.beadsClient.RemoveBeadLabels(projectPath, repoName, b.ID, []string{fixClusteredLabel}); err != nil {
				log.Printf("[BeadCloser] Failed to release parked bead %s (meta %s): %v", b.ID, meta.ID, err)
				continue
			}
			// A parked bead that already closed as failed is reopened: release
			// means it becomes eligible for the scheduler again (PRD FR-020).
			if b.Status.IsTerminal() {
				if err := c.beadsClient.UpdateBeadStatus(projectPath, b.ID, types.StatusOpen); err != nil {
					log.Printf("[BeadCloser] Failed to reopen released bead %s (meta %s): %v", b.ID, meta.ID, err)
				}
			}
			AppendActivity(projectPath, string(b.ID), fmt.Sprintf("released: meta fix bead %s closed", meta.ID))
			c.events.EmitRepo(projectPath, Event{Type: "fix_released", Project: repoName,
				Bead: b.ID.String(), Reason: "meta_fix_closed",
				Fields: map[string]any{"meta_fix": meta.ID, "signature": label}})
			log.Printf("[BeadCloser] Released parked bead %s: meta fix bead %s closed", b.ID, meta.ID)
		}
	}
}

// verifiedCloseEnabled reports whether the verified-close gate is active.
func (c *BeadCloser) verifiedCloseEnabled() bool {
	return c.config != nil && c.config.Validation.Enabled
}

// publishAfterIntegrate pushes the working branch to the configured remote
// after a singleton bead integrates, when output push is enabled. The push is
// fail-closed: PushBranch verifies the remote ref advanced (ls-remote), and
// any rejection (GH006 branch protection, non-FF) is returned as an error
// carrying the exact remote stderr. When output.pr_merge is set (permanent
// branch protection on the working branch), the direct push is skipped
// entirely and the work lands via PublishViaPR — feature branch + gh pr
// create + gh pr merge --admin, verified MERGED — instead. Returns nil when
// push is disabled or when a rejected push was routed to a successfully
// created auto-PR (work is then preserved on the dogfood branch + PR instead
// of the working branch).
func (c *BeadCloser) emitIntegrationBaseMismatch(projectPath, base, source string) {
	logIntegrationBaseMismatch(projectPath, base, source)
	if cur := currentBranch(projectPath); cur != "" && cur != base {
		c.events.EmitRepo(projectPath, Event{Type: "integration_base_mismatch",
			Reason: "checkout_on_other_branch",
			Detail: fmt.Sprintf("resolved integration base %q (source: %s) differs from checkout branch %q — using %q", base, source, cur, base),
			Fields: map[string]any{"resolved_base": base, "source": source, "checkout_branch": cur}})
	}
}

func (c *BeadCloser) publishAfterIntegrate(projectPath string, bead *types.Bead) error {
	if c.config == nil || !c.config.OutputPushEnabled() || c.worktreeManager == nil {
		return nil
	}
	branch, err := c.worktreeManager.WorkingBranch(projectPath)
	if err != nil || branch == "" {
		AppendActivity(projectPath, string(bead.ID), "push skipped: no branch")
		if err != nil {
			return fmt.Errorf("push %s: no working branch: %w", bead.ID, err)
		}
		return fmt.Errorf("push %s: no working branch", bead.ID)
	}
	remote := c.config.OutputPushRemote()
	// PR target is the deterministic integration base, not the checkout's
	// current branch (telematics 2026-09-29). The push still goes to the
	// checkout branch's namesake on the remote; only the PR base changes.
	prBase, prBaseSource, prBaseErr := ResolveIntegrationBase(c.config, projectPath)
	if prBaseErr != nil {
		return fmt.Errorf("push %s: %w", bead.ID, prBaseErr)
	}
	if prBase == "" {
		return fmt.Errorf("push %s: no integration base for %s", bead.ID, projectPath)
	}
	c.emitIntegrationBaseMismatch(projectPath, prBase, prBaseSource)
	if c.config.PRMergeEnabled() {
		head := IntegrationBranchPrefix(projectPath) + string(bead.ID)
		// Duplicate-landing dedup: a racing close attempt may have already
		// landed this bead's tip via PR (hydra-kf2t, 2026-09-27: two closes
		// integrated the same singleton seconds apart). Skip when the local
		// tip is already contained in the remote base branch.
		if tipMergedProbe(projectPath, branch) {
			log.Printf("[Output] %s tip already merged into %s — duplicate integrate after PR landing, no-op", bead.ID, branch)
			AppendActivity(projectPath, string(bead.ID), "already landed (duplicate integrate no-op)")
			return nil
		}
		token, terr := c.ownerTokenForPublish(projectPath, *bead)
		if terr != nil {
			return fmt.Errorf("publish identity: %w", terr)
		}
		url, err := PublishViaPR(projectPath, prBase, head, bead.Title, token)
		if err != nil {
			// Tier-2 (product) repo: the PR is open but not auto-merged —
			// treat the close as waiting-on-human-merge, not a failure
			// (operator publish policy 2026-09-29).
			var pw *PRWaitingError
			if errors.As(err, &pw) {
				log.Printf("[Output] %s PR open, waiting on human merge: %s", bead.ID, pw.URL)
				AppendActivity(projectPath, string(bead.ID), "PR open — waiting on human merge: "+pw.URL)
				c.recordPRWaitingCapture(bead, projectPath, pw.URL)
				return nil
			}
			// Benign race loser: the winning close's PR merge landed the same
			// tip between our pre-check and gh pr create, so GitHub reports
			// "No commits between". Treat as landed, not as an integration
			// failure.
			if isPRNoCommitsBetween(err) {
				log.Printf("[Output] %s already merged into %s (gh pr create reported no commits between) — duplicate integrate treated as landed", bead.ID, branch)
				AppendActivity(projectPath, string(bead.ID), "already landed (duplicate integrate no-op)")
				return nil
			}
			// Benign duplicate-PR race: a PR for this dogfood head already
			// exists (created by an earlier close or integration-retest). The
			// PR is the landing vehicle — PublishViaPR already merged it; only
			// surface an error when the merge itself failed (fleet wedge
			// 2026-09-29: qj74/otae/0po4/242b/ycmu hard-parked
			// integration-exhausted on this error with an open PR waiting).
			if isPRAlreadyExists(err) {
				log.Printf("[Output] %s: existing PR for %s → %s reused and merged", bead.ID, head, prBase)
				AppendActivity(projectPath, string(bead.ID), "landed via existing PR")
				return nil
			}
			log.Printf("[Output] PR merge failed for %s: %s", bead.ID, err)
			AppendActivity(projectPath, string(bead.ID), "PR merge failed: "+err.Error())
			return fmt.Errorf("push %s via PR to %s/%s: %w", bead.ID, remote, prBase, err)
		}
		// Logged only after gh pr view confirmed the PR reached MERGED state —
		// never on gh exit alone.
		log.Printf("[Output] landed via PR for %s: %s (%s → %s)", bead.ID, url, head, prBase)
		AppendActivity(projectPath, string(bead.ID), "landed via PR")
		return nil
	}
	// Non-FF recovery (fetch + merge + one retry) handles the common
	// divergence case where local is behind origin because a PR merged out
	// of band — the hydra-7k1 2026-09-16 stranding. The push embeds the
	// project owner's token (hydra-k3nb L1): a helper-based push would
	// silently authenticate as the daemon owner on product repos.
	token, terr := c.ownerTokenForPublish(projectPath, *bead)
	if terr != nil {
		return fmt.Errorf("push %s: publish identity: %w", bead.ID, terr)
	}
	pushed, reason := PushBranchWithTokenNonFFRecovery(projectPath, branch, token)
	if pushed {
		// Logged only after PushBranch's ls-remote verification confirmed the
		// remote ref matches the local head — never on command exit alone.
		log.Printf("[Output] pushed %s to %s/%s", bead.ID, remote, branch)
		AppendActivity(projectPath, string(bead.ID), "pushed")
		return nil
	}
	if strings.HasPrefix(reason, "remote at") {
		log.Printf("[Output] push verification FAILED for %s: %s", bead.ID, reason)
	}
	log.Printf("[Output] push skipped for %s: %s", bead.ID, reason)
	AppendActivity(projectPath, string(bead.ID), "push skipped: "+reason)
	if IsProtectedBranchRejection(reason) || strings.HasPrefix(reason, "remote at") {
		if maybeOpenAutoPR(c.beadsClient, c.config, projectPath, bead.ID.Repo(), branch, bead) {
			log.Printf("[Output] push rejected for %s (%s); auto-PR opened — closing via PR", bead.ID, reason)
			AppendActivity(projectPath, string(bead.ID), "pushed via auto-PR")
			return nil
		}
	}
	cls := ClassifyPushFailure(reason)
	action := ResolvePublishPath(cls, IsHydraOwnedBranch(branch, c.config.WritableBranchPrefixes()), true)
	publishEvents.EmitRepo(projectPath, Event{Type: "publish_path_classified", Project: filepath.Base(projectPath), Reason: string(cls),
		Detail: "push failure classified " + string(cls) + " -> " + string(action) + " for " + branch + ": " + reason,
		Fields: map[string]any{"bead": string(bead.ID), "branch": branch, "class": string(cls), "action": string(action), "reason": reason, "project_path": projectPath}})
	log.Printf("[Output] push failure for %s classified %s -> %s: %s", bead.ID, cls, action, reason)
	return fmt.Errorf("push %s to %s/%s: %s", bead.ID, remote, branch, reason)
}

// hasGitRemote reports whether projectPath has any git remote configured.
// FR-010: a configured remote selects the PR-first close contract; its
// absence selects the legacy local-merge fallback.
func hasGitRemote(projectPath string) bool {
	out, err := gitOut(projectPath, "remote")
	return err == nil && strings.TrimSpace(out) != ""
}

// publishSingletonViaPR implements the FR-010 PR-first close contract for
// singleton beads: push the worktree branch to origin and land it via PR
// (gh pr create into the working branch + gh pr merge --admin, verified
// MERGED). No local merge into the working branch happens here — the local
// `git merge --no-ff` is demoted to the no-remote fallback in Close. The
// daemon gates already ran before this point, so the PR merge lands
// gates-green. Duplicate-landing races (tipMergedProbe pre-check + "No
// commits between" from gh pr create) are treated as landed, matching
// publishAfterIntegrate.
func (c *BeadCloser) publishSingletonViaPR(projectPath string, bead *types.Bead, key string) error {
	base, baseSource, err := ResolveIntegrationBase(c.config, projectPath)
	if err != nil {
		return fmt.Errorf("pr-first close %s: %w", bead.ID, err)
	}
	if base == "" {
		return fmt.Errorf("pr-first close %s: no integration base for %s", bead.ID, projectPath)
	}
	c.emitIntegrationBaseMismatch(projectPath, base, baseSource)
	// Duplicate-landing pre-check on the WORKTREE BRANCH tip (not the
	// checkout HEAD): a racing close may have already landed this branch via
	// PR. Fail-open on any git fault so the normal PR path proceeds.
	if tip, err := gitOut(projectPath, "rev-parse", "--verify", "--quiet", c.worktreeManager.BranchName(key)); err == nil {
		if _, ferr := gitOut(projectPath, "fetch", "--quiet", "origin", base); ferr == nil && isAncestorOf(projectPath, strings.TrimSpace(tip), "origin/"+base) {
			log.Printf("[Output] %s branch tip already merged into %s — duplicate PR-first close, no-op", bead.ID, base)
			AppendActivity(projectPath, string(bead.ID), "already landed (duplicate integrate no-op)")
			return nil
		}
	}
	head := IntegrationBranchPrefix(projectPath) + string(bead.ID)
	token, terr := c.ownerTokenForPublish(projectPath, *bead)
	if terr != nil {
		return fmt.Errorf("publish identity: %w", terr)
	}
	url, err := PublishBranchViaPR(projectPath, base, c.worktreeManager.BranchName(key), head, bead.Title, token)
	if err != nil {
		// Tier-2 (product) repo: PR open, waiting on human merge — close
		// succeeds without integration failure (operator policy 2026-09-29).
		var pw *PRWaitingError
		if errors.As(err, &pw) {
			log.Printf("[Output] %s PR-first close: PR open, waiting on human merge: %s", bead.ID, pw.URL)
			AppendActivity(projectPath, string(bead.ID), "PR open — waiting on human merge: "+pw.URL)
			c.recordPRWaitingCapture(bead, projectPath, pw.URL)
			return nil
		}
		if isPRNoCommitsBetween(err) {
			log.Printf("[Output] %s already merged into %s (gh pr create reported no commits between) — duplicate PR-first close treated as landed", bead.ID, base)
			AppendActivity(projectPath, string(bead.ID), "already landed (duplicate integrate no-op)")
			return nil
		}
		AppendActivity(projectPath, string(bead.ID), "PR-first close failed: "+err.Error())
		return fmt.Errorf("pr-first close %s via %s → %s: %w", bead.ID, head, base, err)
	}
	log.Printf("[Output] PR-first close landed %s via PR %s (%s → %s)", bead.ID, url, head, base)
	AppendActivity(projectPath, string(bead.ID), "landed via PR")
	return nil
}

// resetIntegrationFailures clears a bead's consecutive-failure counter after
// a successful integrate+push so one later isolated failure cannot trip the
// cap on stale history.
func (c *BeadCloser) resetIntegrationFailures(beadID string) {
	c.integMu.Lock()
	delete(c.integFails, beadID)
	c.integMu.Unlock()
}

// failIntegration aborts a close whose integrate/push step failed
// verification. The bead is parked in place with the integration-pending
// label (NOT reopened to the ready queue — a reopen respawns a full LLM
// worker run over already-finished work), a typed integration-failed event
// carries the exact error, and the worktree is preserved for the retest
// stage. Always returns a non-nil error so the caller aborts the close.
func (c *BeadCloser) failIntegration(bead *types.Bead, projectPath, repoName, stage string, cause error) error {
	beadID := string(bead.ID)
	detail := cause.Error()
	log.Printf("[Integrate] FAILED %s at %s: %s — parked integration-pending, worktree preserved", beadID, stage, detail)

	// Contention backoff: parallel same-repo closes merge conflicting
	// CHANGELOG.md/.gitignore entries within seconds of each other (telematics
	// cd8h/yc2/tg7g/rylg, 2026-09-24: three failures in eleven minutes, none
	// backoff-spaced). Space retries exponentially before the caller re-enters
	// integration; rapid-fire re-entry (arriving before the backoff horizon)
	// is contention, not a distinct failure, and does not advance the cap.
	c.integMu.Lock()
	last := c.integLastFail[beadID]
	now := time.Now()
	if c.integLastFail == nil {
		c.integLastFail = map[string]time.Time{}
	}
	c.integLastFail[beadID] = now
	c.integMu.Unlock()
	rapidFire := !last.IsZero() && now.Sub(last) < integrationBackoffBase

	// Terminal bead: integration already happened on a previous close attempt;
	// do not resurrect (same guard as rejectClose).
	if c.beadsClient != nil {
		if fresh, err := c.beadsClient.GetBead(projectPath, repoName, bead.ID); err == nil && fresh != nil && fresh.Status.IsTerminal() {
			log.Printf("[Integrate] Bead %s already terminal (%s); skipping integration-failed reopen (%s)", beadID, fresh.Status, stage)
			return fmt.Errorf("integration-failed: bead %s %s: %w", beadID, stage, cause)
		}
	}

	c.events.EmitRepo(projectPath, Event{Type: "integration-failed", Project: repoName,
		Bead: beadID, Reason: stage + "_failed",
		Detail: fmt.Sprintf("close integration failed at %s: %s", stage, detail),
		Fields: map[string]any{"stage": stage, "error": detail}})
	AppendActivity(projectPath, beadID, "integration-failed: "+stage)

	c.integMu.Lock()
	if c.integFails == nil {
		c.integFails = map[string]int{}
	}
	if rapidFire {
		log.Printf("[Integrate] %s rapid-fire retry (%v since last failure) — contention, not counted toward exhaustion cap", beadID, now.Sub(last).Round(time.Second))
	} else {
		c.integFails[beadID]++
	}
	failures := c.integFails[beadID]
	c.integMu.Unlock()
	if !rapidFire {
		delay := integrationBackoffBase * time.Duration(1<<min(failures-1, 4)) // 30s,1m,2m,4m,8m capped
		if delay > integrationBackoffMax {
			delay = integrationBackoffMax
		}
		log.Printf("[Integrate] %s backing off %v before next integrate attempt", beadID, delay)
		time.Sleep(delay)
	}

	if c.beadsClient != nil {
		// Park as integration-pending, do NOT reopen to open: the work is
		// already done and verified; a reopen sends the bead back into the
		// ready queue and burns a full worker LLM run re-doing it. The
		// integration-retest stage re-attempts the integrate mechanically.
		labels := []string{"integration-failed", integrationPendingLabel}
		if failures >= maxConsecutiveIntegrationFailures {
			// Cap reached: a plain reopen would respawn the bead into the
			// same failing integrate forever. The durable hard-park label is
			// the one every respawn path (scheduler, Charon, Pan) already
			// skips; only Pan repair or a human clears it.
			labels = append(labels, integrationExhaustedLabel)
			conflictPaths := extractConflictPaths(detail)
			evtFields := map[string]any{}
			if len(conflictPaths) > 0 {
				evtFields["conflict_paths"] = conflictPaths
			}
			c.events.EmitRepo(projectPath, Event{Type: "integration-exhausted", Project: repoName,
				Bead: beadID, Reason: "consecutive_failures_capped",
				Detail: fmt.Sprintf("%d consecutive integration failures (last at %s: %s) — hard-parked", failures, stage, detail),
				Fields: evtFields})
			AppendActivity(projectPath, beadID, "integration-exhausted: hard-parked")
			log.Printf("[Integrate] %s hit %d consecutive integration failures — labeled %s (hard-park; clear only when the cause is fixed) conflict_paths=%v",
				beadID, failures, integrationExhaustedLabel, conflictPaths)
		}
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, labels); err != nil {
			log.Printf("[Integrate] Failed to label %s: %v", beadID, err)
		}
	}
	return fmt.Errorf("integration-failed: bead %s %s: %w", beadID, stage, cause)
}

// verifyBeforeClose runs the verified-close checks for a completed bead.
// It returns nil when the bead may close, or a non-nil error (after
// reopening or escalating the bead) when the close must be aborted.
// verifyBeforeClose runs the VerifiedClose gate chain for a "completed"
// close: terminal-state skip, TDD validation, evidence collection (diff +
// scope + already-integrated heuristics), then the final accept/reject.
// Rejections go through rejectClose so the reopen/attempt-cap/escalate
// semantics of the VerifiedClose rounds are preserved.
func (c *BeadCloser) verifyBeforeClose(bead *types.Bead, projectPath, repoName string) error {
	terminal, err := c.skipIfTerminal(bead, projectPath, repoName)
	if err != nil {
		return err
	}
	if terminal {
		return nil
	}
	if err := c.verifyTDDChain(bead, projectPath, repoName); err != nil {
		return err
	}

	repoPath, mainRepoPath, switchedToWorktree := c.resolveEvidenceRepo(bead, projectPath, repoName)
	base, baseSource, err := c.closeIntegrationBase(bead, projectPath, repoPath)
	if err != nil {
		return err
	}

	targets := beadTargetFiles(bead)
	needDiff := c.config.Validation.RequireDiffOnClose
	needScope := c.config.Validation.TargetScopeMode() != "off"

	changed := c.collectCloseEvidence(bead, projectPath, repoPath, mainRepoPath, switchedToWorktree, base, needDiff, needScope)

	if needDiff && !hasTargetChanges(changed, targets) {
		if !c.acceptAlreadyIntegrated(bead, projectPath, repoName, repoPath, switchedToWorktree, base, baseSource, targets) {
			// Pre-close diff gate (duty 2, 2026-09-24 campaign): an empty
			// target diff is only closable when the worker's HYDRA_VERDICT
			// self-reports an accepted reason (docs-only, chain-deferred-to-root,
			// superseded). Empty + no reason => reject as before: reopen with
			// prior context, never spawn a fix bead.
			if !c.tryAcceptEmptyClose(bead, repoName, repoPath, changed) {
				c.emitEmptyDiffCompleted(bead, repoName, repoPath, changed, targets)
				return c.rejectClose(bead, projectPath, repoName, "empty-diff")
			}
			// Docs-only changes sit outside code targets by definition, so the
			// target scope gate would immediately re-reject the same close the
			// empty gate just accepted. Skip scope once the empty gate accepts.
			needScope = false
		}
	}

	if needScope {
		mode := c.config.Validation.TargetScopeMode()
		if err := c.checkTargetScope(bead, projectPath, repoName, changed, targets, mode); err != nil {
			c.emitEmptyDiffCompleted(bead, repoName, repoPath, changed, targets)
			return err
		}
	}

	// Adversarial close gate (bounded veto): last gate before acceptance.
	// Deterministic gates have already passed; the semantic reviewer scores
	// the diff against the acceptance criteria. Fail-open on its own errors.
	if err := c.verifyAdversarial(bead, projectPath, repoName, repoPath, base); err != nil {
		return err
	}

	c.logCloseEvent(bead, "accepted", "verified", c.validationRetryCount(bead))
	return nil
}

// skipIfTerminal reports whether the bead is already terminal (verification
// skipped entirely: a resurrected close attempt on an already-closed bead
// would only find an empty diff and reject). Fails closed when the fresh
// state cannot be read.
func (c *BeadCloser) skipIfTerminal(bead *types.Bead, projectPath, repoName string) (bool, error) {
	if c.beadsClient == nil {
		return false, nil
	}
	fresh, err := c.beadsClient.GetBead(projectPath, repoName, bead.ID)
	if err != nil {
		return false, fmt.Errorf("[VerifiedClose] cannot verify state of bead %s: %w", bead.ID, err)
	}
	if fresh != nil && fresh.Status.IsTerminal() {
		log.Printf("[VerifiedClose] Bead %s already terminal (%s); skipping verification", bead.ID, fresh.Status)
		return true, nil
	}
	return false, nil
}

// verifyTDDChain runs blocking TDD validation (only gates TDD-phase beads);
// a validation-service error is logged and does not block the close.
func (c *BeadCloser) verifyTDDChain(bead *types.Bead, projectPath, repoName string) error {
	if !c.shouldRunTDDValidation(bead) || c.validationService == nil {
		return nil
	}
	passed, err := c.validationService.TDDChainPasses(*bead, projectPath)
	if err != nil {
		log.Printf("[VerifiedClose] TDD verify error for %s (not blocking): %v", bead.ID, err)
		return nil
	}
	if !passed {
		return c.rejectClose(bead, projectPath, repoName, "tdd-validation-failed")
	}
	return nil
}

// resolveEvidenceRepo returns the directory whose diff/scope evidence counts
// for this close (a chain worktree when one exists, else the resolved repo
// path) plus the main checkout path for leak-union and ancestry checks.
func (c *BeadCloser) resolveEvidenceRepo(bead *types.Bead, projectPath, repoName string) (repoPath, mainRepoPath string, switchedToWorktree bool) {
	repoPath = c.projectResolver.ResolveRepoPath(bead.Project, repoName, projectPath)
	if repoPath == "" {
		repoPath = projectPath
	}
	mainRepoPath = projectPath
	if c.worktreeManager != nil && c.chainKeyFn != nil {
		if wt, ok := c.worktreeManager.ExistingWorktree(projectPath, c.chainKeyFn(*bead)); ok {
			repoPath = wt
			switchedToWorktree = true
		}
	}
	return repoPath, mainRepoPath, switchedToWorktree
}

// closeIntegrationBase resolves the deterministic integration base
// (config knob or origin default) for committed-work detection — NEVER the
// main checkout's current branch, which is incidental operator state
// (telematics 2026-09-29). Empty in main-checkout mode.
func (c *BeadCloser) closeIntegrationBase(bead *types.Bead, projectPath, repoPath string) (base, source string, err error) {
	if repoPath == projectPath {
		return "", "", nil
	}
	base, source, err = ResolveIntegrationBase(c.config, projectPath)
	if err != nil {
		return "", "", fmt.Errorf("committed-work detection for %s: %w", bead.ID, err)
	}
	logIntegrationBaseMismatch(projectPath, base, source)
	return base, source, nil
}

// collectCloseEvidence gathers the changed-file evidence for the diff and
// scope gates: the evidence repo's diff against the integration base, with
// two documented adjustments —
//   - main-checkout mode: only committed changes since the branch/session
//     base count (uncommitted edits are revertible; nipuna 2026-09-16),
//   - worktree mode: uncommitted changes in the main checkout are unioned
//     in as "main-checkout leaks" (incident 2jjz8; the dirty-main
//     auto-commit sweeps them onto the branch at integration time).
//
// Evidence-read failures are logged and fail open.
func (c *BeadCloser) collectCloseEvidence(bead *types.Bead, projectPath, repoPath, mainRepoPath string, switchedToWorktree bool, base string, needDiff, needScope bool) []string {
	if !needDiff && !needScope {
		return nil
	}
	changed, changedErr := changedFilesWithBase(repoPath, base)
	if changedErr != nil {
		log.Printf("[VerifiedClose] diff check error for %s (not blocking): %v", bead.ID, changedErr)
	}

	if !switchedToWorktree && needDiff && changedErr == nil {
		// Main-checkout mode: uncommitted edits are revertible and have been
		// observed to be reverted AFTER close acceptance (2026-09-16 nipuna
		// incident: 14 false closes). Only committed changes since the branch
		// diverged from origin count as evidence. Unavailable base (no upstream
		// etc.) fails open.
		sessionBase := c.sessionBaseFor(bead.ID.String())
		if sessionBase != "" {
			// Session-scoped: only commits made after the bead's spawn
			// count. Pre-existing campaign commits must not satisfy the
			// gate for a no-work session.
			if committed, cerr := committedChangesSinceBase(projectPath, sessionBase); cerr == nil {
				changed = committed
			} else {
				log.Printf("[VerifiedClose] session-evidence base unavailable for %s (fail open): %v", bead.ID, cerr)
			}
		} else if committed, cerr := committedChangesSinceBranchBase(projectPath); cerr == nil {
			changed = committed
		} else {
			log.Printf("[VerifiedClose] committed-evidence base unavailable for %s (fail open): %v", bead.ID, cerr)
		}
	}

	if switchedToWorktree {
		changed = unionMainCheckoutLeaks(bead.ID, projectPath, mainRepoPath, changed)
	}
	return changed
}

// unionMainCheckoutLeaks merges uncommitted main-checkout changes into the
// collected evidence (incident 2jjz8: the agent wrote into the main checkout
// instead of its worktree). The dirty-main auto-commit already sweeps these
// files onto the branch at integration time, so the work is preserved either
// way. Returns the evidence unchanged when the main checkout is clean or
// unreadable.
func unionMainCheckoutLeaks(beadID types.BeadID, projectPath, mainRepoPath string, changed []string) []string {
	mainChanged, mainErr := changedFiles(mainRepoPath)
	if mainErr != nil {
		log.Printf("[VerifiedClose] main-checkout diff check error for %s (not blocking): %v", beadID, mainErr)
		return changed
	}
	if len(mainChanged) == 0 {
		return changed
	}
	seen := make(map[string]bool, len(changed))
	for _, f := range changed {
		seen[f] = true
	}
	for _, f := range mainChanged {
		if !seen[f] {
			log.Printf("[VerifiedClose] %s main-checkout leak: %s", beadID, f)
			changed = append(changed, f)
			seen[f] = true
		}
	}
	return changed
}

// acceptAlreadyIntegrated reports whether the bead's work has already landed
// on the integration base (5k33p / dy0+ig9hw class: the worker's commits may
// have been integrated before verified-close ran; the worktree can be clean
// while the main checkout already contains the bead's work). Three
// escalating checks, all fail open:
//  1. chain branch tip is an ancestor of main HEAD (full integrate),
//  2. per-target merge-base heuristic (partial integrations),
//  3. worktree HEAD commit is an ancestor of main HEAD (locally merged,
//     push pending).
func (c *BeadCloser) acceptAlreadyIntegrated(bead *types.Bead, projectPath, repoName, repoPath string, switchedToWorktree bool, base, baseSource string, targets []string) bool {
	if !switchedToWorktree {
		return false
	}
	if chainBranch := currentBranch(repoPath); chainBranch != "" {
		logIntegrationBaseMismatch(projectPath, base, baseSource)
		if c.chainBranchIntegrated(bead, projectPath, base, chainBranch, targets) {
			return true
		}
	}
	return c.worktreeHEADIntegratedLocally(bead, projectPath, repoName, repoPath)
}

// chainBranchIntegrated runs the branch-level already-integrated checks:
// direct ancestry of the chain branch tip in main HEAD, then the per-target
// merge-base heuristic for partial integrations.
func (c *BeadCloser) chainBranchIntegrated(bead *types.Bead, projectPath, base, chainBranch string, targets []string) bool {
	isAncestor, err := branchIsAncestorOfMain(projectPath, chainBranch)
	if err != nil {
		log.Printf("[VerifiedClose] ancestry check failed for %s (not blocking): %v", bead.ID, err)
	} else if isAncestor {
		log.Printf("[VerifiedClose] %s chain branch %s is ancestor of main HEAD; treating as already integrated", bead.ID, chainBranch)
		return true
	}

	mainChanged, mainErr := changedFilesSinceMergeBase(projectPath, base, chainBranch)
	if mainErr != nil {
		log.Printf("[VerifiedClose] already-integrated check failed for %s (not blocking): %v", bead.ID, mainErr)
		return false
	}
	if hasTargetChanges(mainChanged, targets) {
		log.Printf("[VerifiedClose] %s targets already integrated on %s; treating as satisfied", bead.ID, base)
		return true
	}
	return false
}

// worktreeHEADIntegratedLocally is the durable, commit-level already-
// integrated test. Hydra-7k1 2026-09-16: the singleton integrate step merged
// the worktree branch into the main checkout and THEN the push was rejected
// (non-fast-forward), reopening the bead. From then on the worktree diff was
// empty (HEAD is contained in the main checkout via the merge) while the
// branch-name checks could not run at all — the branch was already deleted —
// so every re-close died empty-diff and the bead escalated after 3 rounds.
// Worktree and main checkout share an object database, so worktree HEAD is
// resolvable in the main repo and `merge-base --is-ancestor <sha> HEAD`
// answers "is the work landed" regardless of branch names.
func (c *BeadCloser) worktreeHEADIntegratedLocally(bead *types.Bead, projectPath, repoName, repoPath string) bool {
	headOut, herr := gitOut(repoPath, "rev-parse", "HEAD")
	if herr != nil {
		return false
	}
	head := strings.TrimSpace(headOut)
	if head == "" {
		return false
	}
	isAncestor, aerr := commitIsAncestorOfMain(projectPath, head)
	if aerr != nil {
		log.Printf("[VerifiedClose] commit-ancestry check failed for %s (not blocking): %v", bead.ID, aerr)
		return false
	}
	if !isAncestor {
		return false
	}
	log.Printf("[VerifiedClose] %s worktree HEAD %s is ancestor of main HEAD (locally integrated, push pending); treating as satisfied", bead.ID, head)
	c.events.EmitRepo(projectPath, Event{Type: "close-accepted-already-integrated", Project: repoName,
		Bead: string(bead.ID), Reason: "worktree_head_ancestor_of_main",
		Detail: fmt.Sprintf("worktree HEAD %s contained in main HEAD; original push failure left origin behind — publisher will land it", head)})
	AppendActivity(projectPath, string(bead.ID), "close-accepted: work already integrated locally (push pending)")
	return true
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

	// Context-pack consult: a guided bead carries its chain's file anchors in
	// agent_context even when its description declares no Target Files. The
	// pack is written to every chain member at first-member spawn, so the
	// bead itself is the chain lookup — no extra resolution needed here.
	// Enforcing against the anchors turns guided beads from advisory-scoped
	// into really-scoped.
	if pack, err := ParseContextPack(bead.AgentContext); err == nil && pack != nil && len(pack.Files) > 0 {
		anchorTargets := make([]string, 0, len(pack.Files))
		for _, a := range pack.Files {
			anchorTargets = append(anchorTargets, a.Path)
		}
		violations := scopeViolations(changed, anchorTargets)
		if len(violations) == 0 {
			return nil
		}
		if mode == "block" {
			return c.rejectClose(bead, projectPath, repoName, "off-scope changes (context-pack anchors): "+strings.Join(violations, ", "))
		}
		log.Printf("[ScopeGate] bead %s changed off-scope files vs context-pack anchors: %v", bead.ID, violations)
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
	closeRejectAttempt := func(prev types.Bead, n int) {
		if c.beadsClient == nil {
			return
		}
		// Replace the previous attempt label so the ladder holds exactly one
		// marker (the highest round); RemoveBeadLabels errors are logged, not
		// fatal — an extra attempt label only over-counts conservatively.
		for _, l := range prev.Labels {
			if strings.HasPrefix(l, closeRejectAttemptPrefix) {
				if err := c.beadsClient.RemoveBeadLabels(projectPath, repoName, bead.ID, []string{l}); err != nil {
					log.Printf("[VerifiedClose] Failed to remove stale attempt label %q from %s: %v", l, beadID, err)
				}
			}
		}
		label := fmt.Sprintf("%s%d", closeRejectAttemptPrefix, n)
		if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{label}); err != nil {
			log.Printf("[VerifiedClose] Failed to label %s with %q: %v", beadID, label, err)
		}
	}

	// Never reopen a terminal bead: a second close attempt after a successful
	// close+integration sees an empty diff and must not resurrect the bead
	// (2026-08-24/25 resurrection loop: i1g, 5r0, bz5, aga accepted-then-reopened).
	var fresh *types.Bead
	if c.beadsClient != nil {
		f, err := c.beadsClient.GetBead(projectPath, repoName, bead.ID)
		if err != nil {
			return fmt.Errorf("[VerifiedClose] cannot verify state of bead %s before reject: %w", beadID, err)
		}
		fresh = f
		if fresh != nil && fresh.Status.IsTerminal() {
			log.Printf("[VerifiedClose] Bead %s already terminal (%s); skipping reject (%s)", beadID, fresh.Status, reason)
			return nil
		}
	}

	maxRounds := 3
	if c.config != nil && c.config.Validation.MaxRounds > 0 {
		maxRounds = c.config.Validation.MaxRounds
	}

	currentAttempts := c.validationRetryCount(bead)

	if currentAttempts >= maxRounds {
		// Stop the reject/reopen loop; flag for a human instead of spinning.
		c.logCloseEvent(bead, "escalated", reason, currentAttempts)
		log.Printf("[VerifiedClose] Bead %s rejected %d/%d times (%s); escalating to human", beadID, currentAttempts, maxRounds, reason)
		// Sensor symmetry (fire): an escalation is a sensor fire; the paired
		// resolve is verified_close_passed with the same correlation ID.
		c.events.EmitRepo(projectPath, Event{Type: "verified_close_rejected", Project: repoName,
			Bead: beadID, Reason: reason,
			Detail: fmt.Sprintf("verified-close rejected %d/%d times (%s); escalated to human", currentAttempts, maxRounds, reason),
			Fields: map[string]any{"attempts": currentAttempts, "max_rounds": maxRounds, "correlation_id": SensorCorrelation("verified-close", beadID), "outcome": "escalated"}})
		if c.beadsClient != nil {
			if err := c.beadsClient.UpdateBeadLabels(projectPath, repoName, bead.ID, []string{"needs-human", "verified-close-escalated", escalatedLabel}); err != nil {
				log.Printf("[VerifiedClose] Failed to label %s for escalation: %v", beadID, err)
			}
			if fresh != nil {
				closeRejectAttempt(*fresh, currentAttempts)
			}
		}
		c.events.EmitRepo(projectPath, Event{Type: "bead.escalated", Project: repoName,
			Bead: beadID, Reason: reason,
			Detail: fmt.Sprintf("verified-close rejected %d/%d times (%s); escalated to human, bead left in_progress", currentAttempts, maxRounds, reason),
			Fields: map[string]any{"attempts": currentAttempts, "max_rounds": maxRounds, "correlation_id": SensorCorrelation("verified-close", beadID), "outcome": "escalated"}})
		return fmt.Errorf("verified-close: bead %s escalated after %d rejections (%s)", beadID, currentAttempts, reason)
	}

	c.logCloseEvent(bead, "rejected", reason, currentAttempts+1)
	log.Printf("[VerifiedClose] Bead %s rejected (%s), round %d/%d — reopening", beadID, reason, currentAttempts+1, maxRounds)
	// Sensor symmetry (fire): each rejection round is a sensor fire; the
	// paired resolve (verified_close_passed) carries the same correlation ID.
	c.events.EmitRepo(projectPath, Event{Type: "verified_close_rejected", Project: repoName,
		Bead: beadID, Reason: reason,
		Detail: fmt.Sprintf("verified-close rejected %s (round %d/%d); bead reopened", beadID, currentAttempts+1, maxRounds),
		Fields: map[string]any{"round": currentAttempts + 1, "correlation_id": SensorCorrelation("verified-close", beadID), "outcome": "reopened"}})
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
		prev := bead
		if fresh != nil {
			prev = fresh
		}
		closeRejectAttempt(*prev, currentAttempts+1)
	}
	if c.sessionResumePilot != nil {
		c.sessionResumePilot.RecordReject(hasBeadLabel(bead.Labels, sessionResumedLabel))
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
	n := 0
	if c.validationService != nil && c.validationService.retryManager != nil {
		n = c.validationService.retryManager.GetValidationRetryCount(string(bead.ID))
	}
	// Durable floor: every verified-close rejection leaves a
	// verified-close-reject-attempt-N label on the bead (rejectClose writes
	// it). Count the highest such attempt so a daemon restart or a Charon
	// orphan reopening — both of which reset the in-memory retry manager —
	// cannot restart the escalation ladder at zero (ee5r/lkbo/ejsk
	// 2026-10-04: gate failures cycled across restarts and never reached max
	// rounds, so nobody was alerted).
	if d := durableRejectAttempts(bead.Labels); d > n {
		n = d
	}
	return n
}

// durableRejectAttempts returns the highest N found among labels of the form
// verified-close-reject-attempt-N (0 when none are present).
func durableRejectAttempts(labels []string) int {
	max := 0
	for _, l := range labels {
		if s, ok := strings.CutPrefix(l, closeRejectAttemptPrefix); ok {
			if n, err := strconv.Atoi(s); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}

// closeRejectAttemptPrefix is the durable per-attempt escalation ladder
// label written by rejectClose (one label per round, replaced each round).
const closeRejectAttemptPrefix = "verified-close-reject-attempt-"

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

// SetSourceIndex wires the bead->store provenance index (index consulted
// after the worker's own ProjectPath, before config name matching).
func (c *BeadCloser) SetSourceIndex(idx *BeadProvenanceIndex) {
	c.sourceIndex = idx
}

// SetSessionBase records the branch HEAD commit at spawn time. Close-time
// committed evidence in main-checkout mode is scoped to commits AFTER this
// point, so pre-existing campaign commits cannot satisfy the diff gate for
// a no-work session (2026-09-16 nipuna epic incident). The literal "HEAD"
// resolves at close time (test fixtures).
func (c *BeadCloser) SetSessionBase(beadID, commit string) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.sessionBase == nil {
		c.sessionBase = make(map[string]string)
	}
	c.sessionBase[beadID] = commit
}

func (c *BeadCloser) sessionBaseFor(beadID string) string {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.sessionBase[beadID]
}
