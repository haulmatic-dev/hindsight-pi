package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ServiceStarter starts all overseer background services.
type ServiceStarter struct {
	overseer *Overseer
}

// NewServiceStarter creates a new service starter.
func NewServiceStarter(overseer *Overseer) *ServiceStarter {
	return &ServiceStarter{overseer: overseer}
}

// Start begins all background services.
func (s *ServiceStarter) Start(ctx context.Context) error {
	o := s.overseer

	// Tier-1 PR auto-merge allowlist (operator publish policy 2026-09-29):
	// repos listed in output.pr_auto_merge_repos / matching deploy.repo_path
	// admin-merge their integration PRs; product repos stop at PR-open and
	// the close is treated as waiting-on-human-merge, not an integration
	// failure. nil would mean legacy always-merge, so install unconditionally.
	cfg := o.config
	SetPRAutoMergeGate(func(projectPath string) bool { return cfg.IsPRAutoMergeRepo(projectPath) })

	// Start orphan process janitor
	if o.orphanJanitor != nil {
		o.orphanJanitor.Start()
		log.Println("🧹 Orphan janitor started")
	}

	// Start stale-escalation ager (no-op when disabled)
	if o.escalationTTL != nil {
		o.escalationTTL.Start()
	}
	// Retroactive pan-filed promotion (nil-client inert)
	if o.panPromoter != nil {
		o.panPromoter.Start()
	}
	if o.integrationSweep != nil {
		o.integrationSweep.Start()
	}
	if o.evidenceSweep != nil {
		o.evidenceSweep.Start()
	}

	// Credential preflight (2026-10-01 drift incident): probe each project
	// repo's origin with the resolved publish identity; stale/revoked ->
	// typed event + auto-park, instead of silent publish failures. Run in a
	// background goroutine so a slow remote never blocks startup; fail-open.
	go o.RunCredentialPreflight()

	// Credential recovery sweep: a project auto-parked by the preflight must
	// come back on its own once the PAT is fixed — re-probe parked-only
	// projects periodically (cheap: usually zero parked). Without this the
	// park outlived the credential fix by 26h on hydra itself (2026-10-06).
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			o.RunCredentialRecoverySweep()
		}
	}()
	// PR aging weekly digest (no-op when disabled)
	if o.prAging != nil {
		o.prAging.Start()
	}
	// Git-ownership preflight (fmsfe wedge): run `git rev-parse` as the
	// daemon's (hydra) user against every project repo BEFORE the project
	// is enabled for work; a dubious-ownership refusal fails closed with a
	// typed event + auto-park carrying the exact remediation command.
	go o.RunGitOwnershipPreflight()

	// Open the worker registry (daemon-owned SQLite truth; PRD
	// worker-park-and-resume Part 4) and construct Pan, the shepherd.
	// Done here rather than in NewOverseer so tests constructing an Overseer
	// never touch the filesystem. Fail-open: if the registry cannot be
	// opened, the daemon runs without it exactly as before, and Pan (which
	// reads it) is not started either.
	if regStore, err := registry.Open(registryPath()); err != nil {
		log.Printf("[Overseer] Warning: registry unavailable, running without it: %v", err)
	} else {
		o.registryStore = regStore
		log.Printf("[Registry] Initialized")
		if o.workerSpawner != nil {
			o.workerSpawner.SetRegistryStore(regStore)
		}
		// Charon's stuck-worker sweep judges liveness from the registry
		// (heartbeat), not the wall clock: parked and actively-reporting
		// workers are never killed on time alone.
		if o.orphanJanitor != nil {
			o.orphanJanitor.SetRegistry(regStore)
		}

		// Reap stale terminal rows and dead worker records on startup so
		// Pan and the scheduler never act on zombie state from prior runs.
		if n, err := regStore.ReapStale(time.Now().UTC().Add(-24 * time.Hour)); err != nil {
			log.Printf("[Registry] ReapStale warning: %v", err)
		} else if n > 0 {
			log.Printf("[Registry] Reaped %d stale worker row(s)", n)
		}

		// Reconcile any non-terminal rows whose PID is dead after a restart.
		// This must run before Pan's first tick so orphan rows never become
		// candidates for undeliverable commands.
		if n, ids, err := regStore.ReconcileDeadPIDs(); err != nil {
			log.Printf("[Registry] ReconcileDeadPIDs warning: %v", err)
		} else if n > 0 {
			log.Printf("[Registry] Reconciled %d dead worker row(s) orphaned by restart: %s", n, strings.Join(ids, ", "))
		}

		// Daily token-usage rollup: persists worker token totals into the
		// durable token_usage_daily table at 00:05 UTC and emits a typed
		// event per row, so spend trends survive ReapStale/ReapGhosts pruning.
		o.tokenUsageRollup = NewTokenUsageRollupJob(func() *registry.Store { return o.registryStore }, o.eventSink)
		o.tokenUsageRollup.Start()

		panCfg := o.config.Shepherd.Pan
		// blockedLogged dedups per-process candidate logging: each blocked
		// candidate is logged once per daemon run (keyed bead+reason) so a
		// candidate that never reaches the judge is visible in the log
		// (srv-jobfile-eci, 2026-10-06: past leaf grace, invisible to Pan,
		// and the silent fail-open below gave no way to tell scan-error
		// from no-candidate).
		blockedLogged := make(map[string]bool)
		o.panShepherd = pan.New(pan.Config{
			Enabled:          panCfg.Enabled,
			Interval:         time.Duration(panCfg.IntervalSeconds) * time.Second,
			HungAfter:        time.Duration(panCfg.HungAfterSeconds) * time.Second,
			ParkOverdueGrace: time.Duration(panCfg.ParkOverdueGraceSeconds) * time.Second,
			NudgeCap:         panCfg.NudgeCap,
			Provider:         panCfg.Provider,
			Model:            panCfg.Model,
			Thinking:         panCfg.Thinking,
			APIKeyName:       panCfg.APIKeyName,
			TokenBudget:      o.config.Overseer.MaxBeadTokens,
			BlockedBeads: func() []pan.BlockedBead {
				var blocked []pan.BlockedBead
				if o.retryManager == nil || o.workerSpawner == nil {
					return blocked
				}
				maxRetries := o.config.Overseer.DefaultRetries
				if maxRetries <= 0 {
					maxRetries = defaultMaxRetries
				}
				for beadID, count := range o.retryManager.Snapshot() {
					if count < maxRetries {
						continue
					}
					reason, attempts, since := o.retryManager.LastFailure(beadID)
					blocked = append(blocked, pan.BlockedBead{
						BeadID: beadID,
						Reason: fmt.Sprintf("retry exhausted after %d attempts: %s", attempts, reason),
						Since:  since,
					})
				}
				for _, beadID := range o.workerSpawner.ListSpawnFailures() {
					already := false
					for _, b := range blocked {
						if b.BeadID == beadID {
							already = true
							break
						}
					}
					if already {
						continue
					}
					if reason, since, ok := o.workerSpawner.LastSpawnFailure(beadID); ok {
						// Stale-entry purge (fmsfe-npa 2026-10-02): a spawn-failure
						// entry survives until the bead closes, so the digest kept
						// re-surfacing a phantom "N beads blocked by spawn failed"
						// cohort — Pan re-filed wedge beads in a loop while the
						// store itself showed 0 blocked. Purge entries that can no
						// longer represent a real blocker: superseded by a live
						// registry worker, or aged out past the TTL (terminal beads
						// stop respawning, so TTL eventually covers them too).
						if staleSpawnFailure(o.workerSpawner, beadID, since) {
							o.workerSpawner.ClearSpawnFailure(beadID)
							continue
						}
						blocked = append(blocked, pan.BlockedBead{
							BeadID: beadID,
							Reason: fmt.Sprintf("spawn failed: %s", reason),
							Since:  since,
						})
					}
				}
				// Hard-parked beads: durable labels the scheduler never retries.
				// Without this, a labeled bead with no retry record (e.g. after a
				// restart cleared in-memory retry state) is invisible to Pan — the
				// 2026-09-13 webapp wedge needed manual label clearing twice.
				if o.beadsClient != nil {
					wedgeGrace := time.Duration(panCfg.EscalatedWedgeGraceSeconds) * time.Second
					if wedgeGrace <= 0 {
						wedgeGrace = time.Hour
					}
					leafGrace := time.Duration(panCfg.EscalatedLeafGraceSeconds) * time.Second
					if leafGrace <= 0 {
						leafGrace = 24 * time.Hour
					}
					for _, project := range o.projects {
						for _, target := range repoScanTargets(project) {
							beads, err := o.beadsClient.ListBeads(target, "")
							if err != nil {
								log.Printf("[PanSweep] store scan failed for %s (fail-open, beads there are invisible to Pan this cycle): %v", target, err)
								continue // fail open: one unreadable store must not blind the scan
							}
							// dependent_count comes from br list itself (parser.go maps it
							// into Metadata; the full dependencies list is NOT in list
							// output). An escalated chain head stalls its whole subtree
							// (srv-jobfile 2026-10-05: 2 wedged escalations, br ready=0
							// for the whole repo), while an escalated leaf can safely
							// wait out the 24h human window.
							for _, b := range beads {
								if b.Status.IsTerminal() {
									continue
								}
								dependents := 0
								if b.Metadata != nil {
									dependents, _ = strconv.Atoi(b.Metadata["dependent_count"])
								}
								// hydra-1hbo: real retest counters feed the blocked reason so
								// Pan stops narrating mechanical retries that are not running.
								var retestStatsFn func(string) (int, time.Time, bool)
								retestMax := 0
								if o.integrationRetest != nil {
									retestStatsFn = o.integrationRetest.RetestStats
									retestMax = o.config.IntegrationRetest.MaxAttempts
								}
								if reason, ok := panBlockedReasonWithRetest(b, time.Now(), wedgeGrace, leafGrace, dependents, retestStatsFn, retestMax); ok {
									key := b.ID.String() + "|" + reason
									if !blockedLogged[key] {
										blockedLogged[key] = true
										log.Printf("[PanSweep] blocked candidate: %s — %s", b.ID.String(), reason)
									}
									blocked = append(blocked, pan.BlockedBead{
										BeadID: b.ID.String(),
										Reason: reason,
										Since:  b.UpdatedAt,
									})
								}
							}
						}
					}
				}
				// Stranded pushes: commits on the integration/working branch that never
				// reached origin (branch protection, husky hook failures, dead pushes).
				for _, pp := range o.PushPending() {
					blocked = append(blocked, pan.BlockedBead{
						BeadID: pp.ProjectName + "-push-pending",
						Reason: fmt.Sprintf("PUSH-PENDING: %s holds commits not on origin: %s", pp.ProjectPath, pp.Reason),
						Since:  pp.Since,
					})
				}
				return blocked
			},
			// Fleet health digest: Pan's eyes (see collectHealthDigest).
			DigestFunc: o.collectHealthDigest,
			// Tier-1 trajectory pre-filters: mechanical outlier detection over
			// live workers' pi sessions (see collectWorkerTrajectory). Tier-2
			// LLM judgment only wakes on flagged trajectories.
			TrajectoryFunc: o.collectWorkerTrajectory,
		}, o.registryStore, nil)

		// Judge-degradation telemetry: Pan's pi judge failing silently for weeks
		// (6494/6494 invocations, Aug–Sep 2026) proved the fallback hides outages.
		// Surface degraded/recovered transitions as daemon events + ERROR logs.
		o.panShepherd.OnJudgeFailure = func(consecutive int, err error) {
			o.eventSink.Emit(Event{
				Type:   "pan_judge_degraded",
				Reason: "pi_invocation_failed",
				Detail: fmt.Sprintf("Pan judge degraded: %d consecutive pi invocation failures; deterministic rules in force; last error: %v", consecutive, err),
				Fields: map[string]any{"consecutive_failures": consecutive},
			})
		}
		o.panShepherd.OnJudgeRecovered = func(failures int) {
			o.eventSink.Emit(Event{
				Type:   "pan_judge_recovered",
				Reason: "pi_invocation_succeeded",
				Detail: fmt.Sprintf("Pan judge recovered after %d consecutive pi invocation failure(s)", failures),
				Fields: map[string]any{"outage_failures": failures},
			})
		}

		// Resolve project path for a bead ID using the configured projects.
		// Falls back to repo-name and path-basename matches: multi-user discovery
		// registers projects as "<user>/<repo>" while bead IDs carry only the repo
		// name — the exact mismatch that made Pan's close paths fail 100% with
		// "project path not found" (2026-09-11/13).
		projectPathForID := func(id types.BeadID) string {
			// Provenance first: the index knows which store listed this bead,
			// the only correct answer when several projects share a repo name.
			if p := o.beadSource.Lookup(id.String()); p != "" {
				return p
			}
			projectName := id.Project()
			repoName := id.Repo()
			for _, p := range o.projects {
				if p.Name != projectName {
					continue
				}
				if p.Type == "multi" && repoName != "" {
					return filepath.Join(p.Path, repoName)
				}
				return p.Path
			}
			for _, p := range o.projects {
				if p.Type == "multi" {
					for _, r := range p.Repos {
						if r.Name == projectName || (repoName != "" && r.Name == repoName) {
							return resolveRepoPath(p, r)
						}
					}
				}
				if filepath.Base(p.Path) == projectName {
					return p.Path
				}
			}
			return ""
		}

		o.panShepherd.SetZeroDiff(func(beadID string) (bool, bool) {
			id := types.BeadID(beadID)
			projectPath := projectPathForID(id)
			if projectPath == "" || o.worktreeManager == nil {
				return false, false
			}
			key := o.chainKeyForBead(types.Bead{ID: id})
			wt, ok := o.worktreeManager.ExistingWorktree(projectPath, key)
			if !ok {
				return false, false
			}
			base, baseSource, berr := ResolveIntegrationBase(o.config, projectPath)
			if berr != nil {
				log.Printf("[Pan] zero-diff check failed for %s (treating as unknown): %v", beadID, berr)
				return false, false
			}
			logIntegrationBaseMismatch(projectPath, base, baseSource)
			changed, err := changedFilesWithBase(wt, base)
			if err != nil {
				log.Printf("[Pan] zero-diff check failed for %s (treating as unknown): %v", beadID, err)
				return false, false
			}
			return len(changed) == 0, true
		})
		o.panShepherd.SetOnCloseSatisfied(BuildCloseSatisfied(o))
		// Closure-audit duty: open auto-fix beads become judgment candidates;
		// a recorded verdict alone never closes them (jx78 hollow verdict).
		o.panShepherd.SetFixBeads(BuildFixBeads(o))
		o.panShepherd.SetFixVerifier(BuildFixVerifier(o))
		o.panShepherd.SetOnAbandonWithSummary(BuildAbandonWithSummary(o))

		// Pan repair handlers: whitelisted daemon-side actions that clear
		// scheduler-level blockers without a worker command. They are
		// fail-open: errors are logged and emitted as audit events only.
		// Sensor symmetry: every repair verdict is wrapped so each verb emits
		// repair_succeeded (resolve) or repair_failed (fire) with a per
		// verb+bead correlation ID; failures here are 218-vs-28 invisible
		// outcomes in the pre-symmetry measurement window.
		repairRaw := map[string]func(beadID string) error{
			"repair-worktree": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("repair-worktree: project path unresolved for %s", beadID)
				}
				if o.worktreeManager == nil {
					return fmt.Errorf("repair-worktree: no worktree manager for %s", beadID)
				}
				key := o.chainKeyForBead(types.Bead{ID: id})
				o.worktreeManager.Remove(projectPath, key)
				if _, err := o.worktreeManager.EnsureWorktree(projectPath, key); err != nil {
					return fmt.Errorf("repair-worktree: recreate %s: %w", beadID, err)
				}
				log.Printf("[PanRepair] repair-worktree succeeded for %s", beadID)
				return nil
			},
			"reap-placeholder": func(beadID string) error {
				if o.workerSpawner == nil {
					return fmt.Errorf("reap-placeholder: no worker spawner")
				}
				if !o.workerSpawner.ClearSpawningPlaceholder(beadID) {
					log.Printf("[PanRepair] reap-placeholder: no spawning placeholder for %s", beadID)
				}
				return nil
			},
			"reset-retries": func(beadID string) error {
				if o.retryManager == nil {
					return fmt.Errorf("reset-retries: no retry manager")
				}
				prev := o.retryManager.ResetRetryCount(beadID)
				log.Printf("[PanRepair] reset-retries for %s (previous fault count: %d)", beadID, prev)
				return nil
			},
			"repair-bead-store": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("repair-bead-store: project path unresolved for %s", beadID)
				}
				if o.storeGuard == nil {
					return fmt.Errorf("repair-bead-store: no store guard")
				}
				// Restores .beads/issues.jsonl from the newest .br_history
				// snapshot and unblocks the project only if the open-count
				// verifies recovered. Failure leaves the project blocked.
				if err := o.storeGuard.Restore(projectPath); err != nil {
					return fmt.Errorf("repair-bead-store: %w", err)
				}
				log.Printf("[PanRepair] repair-bead-store succeeded for %s (project %s)", beadID, projectPath)
				return nil
			},
			// clear-hard-park un-parks a bead whose durable park label has
			// outlived its cause: prunes stale worktree registrations (the cheap
			// shared cause — xw3 class), removes the label, reopens the bead,
			// and resets its retry counter. Refuses when no hard-park label is
			// present so Pan cannot use it as a generic reopen.
			"clear-hard-park": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("clear-hard-park: project path unresolved for %s", beadID)
				}
				bead, err := o.beadsClient.GetBead(projectPath, id.Repo(), id)
				if err != nil {
					return fmt.Errorf("clear-hard-park: GetBead %s: %w", beadID, err)
				}
				if bead == nil {
					return fmt.Errorf("clear-hard-park: %s not found", beadID)
				}
				var toClear []string
				for _, lbl := range bead.Labels {
					if lbl == worktreeUnavailableLabel || lbl == integrationExhaustedLabel || lbl == gitOwnershipLabel {
						toClear = append(toClear, lbl)
					}
				}
				if len(toClear) == 0 {
					return fmt.Errorf("clear-hard-park: %s carries no hard-park label", beadID)
				}
				if _, err := gitOut(projectPath, "worktree", "prune"); err != nil {
					log.Printf("[PanRepair] clear-hard-park: worktree prune for %s failed (continuing): %v", projectPath, err)
				}
				if err := o.beadsClient.RemoveBeadLabels(projectPath, id.Repo(), id, toClear); err != nil {
					return fmt.Errorf("clear-hard-park: remove labels on %s: %w", beadID, err)
				}
				if bead.Status != types.StatusOpen {
					if err := o.beadsClient.UpdateBeadStatus(projectPath, id, types.StatusOpen); err != nil {
						log.Printf("[PanRepair] clear-hard-park: reopen %s failed (continuing): %v", beadID, err)
					}
				}
				if o.retryManager != nil {
					o.retryManager.ResetRetryCount(beadID)
				}
				log.Printf("[PanRepair] clear-hard-park cleared %v on %s", toClear, beadID)
				return nil
			},
			// clear-escalation drops a stale human-escalation mark: removes the
			// escalated/needs-human/verified-close-escalated labels, reopens the
			// bead, and resets its retry counter so the scheduler gives it one
			// fair retry. Refuses when no escalation label is present so Pan
			// cannot use it as a generic reopen.
			"clear-escalation": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("clear-escalation: project path unresolved for %s", beadID)
				}
				bead, err := o.beadsClient.GetBead(projectPath, id.Repo(), id)
				if err != nil {
					return fmt.Errorf("clear-escalation: GetBead %s: %w", beadID, err)
				}
				if bead == nil {
					return fmt.Errorf("clear-escalation: %s not found", beadID)
				}
				var toClear []string
				escLabels := map[string]bool{escalatedLabel: true, "needs-human": true, "verified-close-escalated": true}
				for _, lbl := range bead.Labels {
					if escLabels[lbl] {
						toClear = append(toClear, lbl)
					}
				}
				if !escLabels[escalatedLabel] {
					return fmt.Errorf("clear-escalation: %s carries no %q label", beadID, escalatedLabel)
				}
				if err := o.beadsClient.RemoveBeadLabels(projectPath, id.Repo(), id, toClear); err != nil {
					return fmt.Errorf("clear-escalation: remove labels on %s: %w", beadID, err)
				}
				if bead.Status != types.StatusOpen {
					if err := o.beadsClient.UpdateBeadStatus(projectPath, id, types.StatusOpen); err != nil {
						log.Printf("[PanRepair] clear-escalation: reopen %s failed (continuing): %v", beadID, err)
					}
				}
				if o.retryManager != nil {
					o.retryManager.ResetRetryCount(beadID)
				}
				log.Printf("[PanRepair] clear-escalation cleared %v on %s", toClear, beadID)
				return nil
			},
			// prune-worktree-registration runs git worktree prune on the bead's
			// project — the project-scoped fix for the registered-but-missing
			// wedge class. Rate-limited per project, not per bead.
			"prune-worktree-registration": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("prune-worktree-registration: project path unresolved for %s", beadID)
				}
				if _, err := gitOut(projectPath, "worktree", "prune"); err != nil {
					return fmt.Errorf("prune-worktree-registration: %w", err)
				}
				log.Printf("[PanRepair] prune-worktree-registration pruned %s", projectPath)
				return nil
			},
			// recover-spawn-cohort is the one-pass project-scoped repair for the
			// spawn-failure wedge class (fmsfe-npa 2026-10-02): prune stale
			// worktree registrations, then clear every recorded spawn failure
			// belonging to this project together with its retry counter. One
			// invocation recovers the whole batch; per-bead repairs cannot.
			"recover-spawn-cohort": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("recover-spawn-cohort: project path unresolved for %s", beadID)
				}
				if o.workerSpawner == nil {
					return fmt.Errorf("recover-spawn-cohort: no worker spawner")
				}
				if _, err := gitOut(projectPath, "worktree", "prune"); err != nil {
					log.Printf("[PanRepair] recover-spawn-cohort: worktree prune for %s failed (continuing): %v", projectPath, err)
				}
				cleared := 0
				for _, bid := range o.workerSpawner.ListSpawnFailures() {
					if projectPathForID(types.BeadID(bid)) != projectPath {
						continue
					}
					o.workerSpawner.ClearSpawnFailure(bid)
					if o.retryManager != nil {
						o.retryManager.ResetRetryCount(bid)
					}
					cleared++
				}
				if cleared == 0 {
					return fmt.Errorf("recover-spawn-cohort: no spawn failures recorded for %s", projectPath)
				}
				log.Printf("[PanRepair] recover-spawn-cohort cleared %d spawn failure(s) in %s", cleared, projectPath)
				return nil
			},
			// re-push-integration reruns the publisher for the project of a
			// "<project>-push-pending" pseudo-bead and fails unless the stranded
			// push actually cleared.
			"re-push-integration": func(beadID string) error {
				name := strings.TrimSuffix(beadID, "-push-pending")
				for _, project := range o.projects {
					if project.Name != name {
						continue
					}
					active := make(map[string]bool, len(o.activeWorkers))
					for wid := range o.activeWorkers {
						active[wid] = true
					}
					pushed, reason := o.publishOneProject(project, active)
					if !pushed && !benignPublishReasons[reason] {
						return fmt.Errorf("re-push-integration: still pending for %s: %s", project.Path, reason)
					}
					log.Printf("[PanRepair] re-push-integration %s: pushed=%v reason=%q", project.Path, pushed, reason)
					return nil
				}
				return fmt.Errorf("re-push-integration: no project named %s", name)
			},
			"integrate-stranded": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("integrate-stranded: project path unresolved for %s", beadID)
				}
				if o.worktreeManager == nil {
					return fmt.Errorf("integrate-stranded: no worktree manager for %s", beadID)
				}
				key := o.chainKeyForBead(types.Bead{ID: id})
				return IntegrateStrandedRepair(projectPath, key, o.worktreeManager, o.beadsClient, o.eventSink)
			},
			"park-campaign":   o.parkCampaignRepair,
			"unpark-campaign": o.unparkCampaignRepair,
			// retry-quarantine requeues a stranded-conflict bead whose
			// hydra/quarantine/<key> branch now merges clean against the base
			// (the base moved since the conflict). Still conflicted -> refused,
			// bead stays quarantined.
			"retry-quarantine": func(beadID string) error {
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("retry-quarantine: project path unresolved for %s", beadID)
				}
				key := o.chainKeyForBead(types.Bead{ID: id})
				if err := QuarantineReattemptRepair(projectPath, key, o.beadsClient, o.eventSink); err != nil {
					return err
				}
				if o.retryManager != nil {
					o.retryManager.ResetRetryCount(beadID)
				}
				return nil
			},
			// kill-stalled-worker implements the zero-token-stall repair
			// (2026-10-20 incident): kills the stalled worker by its EXPLICIT
			// registry PID (never by PGID — close gates share the daemon's
			// process group), marks the worker row dead, and reopens the bead
			// for respawn with one fair retry.
			"kill-stalled-worker": func(beadID string) error {
				if o.orphanJanitor == nil {
					return fmt.Errorf("kill-stalled-worker: no Charon janitor configured")
				}
				if err := o.orphanJanitor.KillStalledWorker(beadID); err != nil {
					return err
				}
				id := types.BeadID(beadID)
				projectPath := projectPathForID(id)
				if projectPath == "" {
					return fmt.Errorf("kill-stalled-worker: project path unresolved for %s", beadID)
				}
				if err := o.beadsClient.UpdateBeadStatus(projectPath, id, types.StatusOpen); err != nil {
					return fmt.Errorf("kill-stalled-worker: reopen %s: %w", beadID, err)
				}
				if o.retryManager != nil {
					o.retryManager.ResetRetryCount(beadID)
				}
				return nil
			},
			// kill-rogue-pi implements the rogue-pi repair: kills every rogue
			// bare-pi process Charon recorded, by EXPLICIT PID only. beadID is
			// unused — the finding is host-level.
			"kill-rogue-pi": func(beadID string) error {
				if o.orphanJanitor == nil {
					return fmt.Errorf("kill-rogue-pi: no Charon janitor configured")
				}
				killed, err := o.orphanJanitor.KillRoguePis()
				if killed > 0 {
					log.Printf("[PanRepair] kill-rogue-pi: killed %d rogue pi process(es)", killed)
				}
				return err
			},
		}
		// Sensor symmetry: every repair verdict emits repair_succeeded
		// (resolve) or repair_failed (fire) with a per verb+bead correlation
		// ID; failures here were 218-vs-28 invisible outcomes in the
		// pre-symmetry measurement window.
		o.panShepherd.SetRepairHandlers(wrapRepairHandlers(repairRaw, o.eventSink))
		o.panShepherd.SetLogImprovement(o.logImprovementRepair)

		// FR-022 bead-filing actuator: Pan files improvement beads for human
		// triage. The filer resolves the project (real beads via provenance,
		// pseudo-beads by stripping the suffix) and enforces the per-project
		// open-filed cap.
		o.panShepherd.SetBeadFiler(func(beadID, title, description string) error {
			id := types.BeadID(beadID)
			projectPath := projectPathForID(id)
			if projectPath == "" {
				// Pseudo-beads (push-pending / campaign-anomaly) resolve by
				// stripping the suffix.
				trimmed := strings.TrimSuffix(strings.TrimSuffix(beadID, "-push-pending"), "-campaign-anomaly")
				if trimmed != beadID {
					projectPath = projectPathForID(types.BeadID(trimmed))
				}
			}
			if projectPath == "" {
				return fmt.Errorf("file-bead: project path unresolved for %s", beadID)
			}
			return o.filePanBead(projectPath, beadID, title, description)
		})

		// Project-scoped repairs (prune, re-push) rate-limit per project path;
		// the pseudo-bead form "<project>-push-pending" resolves by name.
		o.panShepherd.SetProjectScope(func(beadID string) string {
			if strings.HasSuffix(beadID, "-push-pending") {
				name := strings.TrimSuffix(beadID, "-push-pending")
				for _, project := range o.projects {
					if project.Name == name {
						return project.Path
					}
				}
				return ""
			}
			return projectPathForID(types.BeadID(beadID))
		})

		// Pan should not judge beads that have already reached a terminal
		// state. Cache the lookup for 5 minutes: terminal is monotonic, so
		// a stale false only costs one extra tick before the row is reaped.
		terminalCache := make(map[string]struct {
			terminal bool
			at       time.Time
		})
		var terminalMu sync.Mutex
		const terminalTTL = 5 * time.Minute
		o.panShepherd.SetTerminalBead(func(beadID string) bool {
			id := types.BeadID(beadID)
			projectPath := projectPathForID(id)
			if projectPath == "" {
				return false
			}
			repoName := id.Repo()

			terminalMu.Lock()
			entry, ok := terminalCache[beadID]
			terminalMu.Unlock()
			if ok && time.Since(entry.at) < terminalTTL {
				return entry.terminal
			}

			bead, err := o.beadsClient.GetBead(projectPath, repoName, id)
			terminal := err == nil && bead != nil && bead.Status.IsTerminal()

			terminalMu.Lock()
			terminalCache[beadID] = struct {
				terminal bool
				at       time.Time
			}{terminal, time.Now()}
			terminalMu.Unlock()

			return terminal
		})

		// Budget arbitration: a fresh Pan continue verdict stays Charon's
		// hard backstop (converging work must be allowed to finish).
		if o.orphanJanitor != nil {
			o.orphanJanitor.SetPanArbiter(func(beadID string) bool {
				return o.panShepherd.ContinuedRecently(beadID, 5*time.Minute)
			})
		}
	}

	// Start Pan, the shepherd (fail-open; nil when the registry is unavailable)
	if o.panShepherd != nil {
		o.panShepherd.Start()
		log.Println("🐐 Pan shepherd started")
	}

	// Run Pi ecosystem setup
	if err := o.runPiSetup(); err != nil {
		log.Printf("[Overseer] Pi setup warning: %v", err)
	}

	// Discover projects from system config
	projectScanner := NewProjectScanner(o.config)
	if err := projectScanner.Discover(); err != nil {
		return &ServiceError{Op: "discover projects", Err: err}
	}
	configProjects := projectScanner.Projects()
	o.projects = configProjects
	log.Printf("📁 Discovered %d projects from config", len(o.projects))
	if o.config != nil && o.config.OutputPushEnabled() && o.config.AutoPREnabled() {
		log.Println("[AutoPR] Enabled: protected-branch push rejections will open a PR")
	}

	// Discover multi-user projects (gated: projects.multi_user_discovery,
	// default true). Disabled narrows execution to projects.list only.
	if o.config.MultiUserDiscovery {
		multiUserDiscovery := NewMultiUserDiscovery()
		multiUserDiscovery.SetExclude(o.config.AutoDiscoverExclude)
		if err := multiUserDiscovery.Discover(); err != nil {
			log.Printf("[MultiUser] Discovery error: %v", err)
		} else {
			userProjects := multiUserDiscovery.Projects()
			if len(userProjects) > 0 {
				o.projects = append(o.projects, userProjects...)
				log.Printf("👥 Discovered %d user projects from %d users", len(userProjects), len(multiUserDiscovery.Users()))
			}
			// Warn about config-listed projects whose repo basename collides
			// with another registered project. PrefixGuard does not mutate these.
			LogConfigPrefixCollisionGuidance(configProjects, o.projects)
			// Store user contexts on overseer and worker spawner for per-user attribution
			o.userContexts = multiUserDiscovery.Users()
			o.workerSpawner.SetUserContexts(o.userContexts)
		}
	} else {
		log.Printf("👥 Multi-user discovery disabled (projects.multi_user_discovery: false) — projects.list only")
	}

	// Initialize bead file reader and analyzer
	o.beadFileReader = NewBeadFileReader(o.projects)
	o.beadAnalyzer.SetProjects(o.projects)

	// Quarantine reconciler: constructed here, not in NewOverseer — it needs
	// beadFileReader, which does not exist yet at NewOverseer time (first
	// deploy silently never built it, 2026-10-16). Requires worktree isolation.
	if o.worktreeManager != nil && o.quarantineReconciler == nil {
		if home := os.Getenv("HYDRA_HOME"); home != "" {
			o.quarantineReconciler = NewQuarantineReconciler(
				filepath.Join(home, "quarantined-chains.json"),
				o.worktreeManager, o.beadFileReader, o.eventSink,
				func() []types.Project { return o.projects })
			o.quarantineReconciler.SetRevalidation(
				o.beadCloser.RunVerificationGateForRevalidation,
				func(pp string, id types.BeadID, l []string) error {
					return o.beadsClient.RemoveBeadLabels(pp, "", id, l)
				},
				func(pp string, id types.BeadID, l []string) error {
					return o.beadsClient.UpdateBeadLabels(pp, "", id, l)
				})
			o.quarantineReconciler.Start()
		}
	}

	// Update bead discovery with discovered projects
	if o.beadDiscovery != nil {
		o.beadDiscovery.SetProjects(o.projects)
	}

	// Initialize chain registrar (depends on chainCompletionDetector and beadFileReader)
	if o.chainCompletionDetector != nil && o.beadFileReader != nil {
		o.chainRegistrar = NewChainRegistrar(o.chainCompletionDetector, o.beadFileReader)
	}

	// Start settled-state detection service if enabled
	if o.settledStateSvc != nil {
		if err := o.settledStateSvc.Start(ctx); err != nil {
			log.Printf("[SettledState] Failed to start service: %v", err)
		} else {
			log.Println("🔒 Settled-state detection service started")
		}
	}

	// Start skill updater if initialized
	if o.skillUpdater != nil {
		o.skillUpdater.Start()
		log.Println("📚 Skill updater started")
	}

	// Refresh each project's code graph. The graphify indexer has no background
	// worker to start: each refresh is a bounded one-shot subprocess.
	if o.intelligenceHub.GraphifyIndexer() != nil {
		o.intelligenceHub.QueueInitialIndexing(o.projects)
		log.Println("🕸️  Graphify code-graph refresh queued")
	}

	// Start epic scanner
	if o.epicScanner != nil {
		o.epicScanner.Start()
		log.Println("🏛️  Epic scanner started")
	}

	// Start the periodic LLM PR-triage pass (hydra-muvh, FR-030). Advice
	// only: overlapping dogfood/* PRs get a recommendation comment + advisory
	// labels; merges and closes stay human. Fail-open: disabled or failing,
	// the daemon runs exactly as before.
	if o.triageSvc != nil {
		o.triageSvc.Start()
		log.Println("🧭 PR-triage pass started")
	}

	return nil
}

// Stop stops all background services.
func (s *ServiceStarter) Stop() {
	o := s.overseer

	if o.orphanJanitor != nil {
		o.orphanJanitor.Stop()
	}
	if o.escalationTTL != nil {
		o.escalationTTL.Stop()
	}
	if o.panPromoter != nil {
		o.panPromoter.Stop()
	}
	if o.integrationSweep != nil {
		o.integrationSweep.Stop()
	}
	if o.quarantineReconciler != nil {
		o.quarantineReconciler.Stop()
	}
	if o.evidenceSweep != nil {
		o.evidenceSweep.Stop()
	}
	if o.tokenUsageRollup != nil {
		o.tokenUsageRollup.Stop()
	}
	if o.settledStateSvc != nil {
		if err := o.settledStateSvc.Stop(); err != nil {
			log.Printf("[SettledState] Error stopping service: %v", err)
		} else {
			log.Println("🔒 Settled-state detection service stopped")
		}
	}
	if o.skillUpdater != nil {
		o.skillUpdater.Stop()
		log.Println("📚 Skill updater stopped")
	}

	if o.epicScanner != nil {
		o.epicScanner.Stop()
		log.Println("🏛️  Epic scanner stopped")
	}

	if o.triageSvc != nil {
		o.triageSvc.Stop()
		log.Println("🧭 PR-triage pass stopped")
	}
	if o.prAging != nil {
		o.prAging.Stop()
		log.Println("📋 PR aging sweeper stopped")
	}
}

// panBlockedReason maps a non-terminal bead's durable labels to a Pan blocked
// candidacy reason. Returns ok=false when no label warrants a candidate.
// Extracted from the BlockedBeads sweep so label→candidate policy is
// unit-testable; the sweep itself only aggregates per project store.
//
// wedgeGrace + blockedDependents implement the wedge fast-path: an escalated
// bead that still has live dependents stalls its dependency subtree (every
// escalation leaves the bead in_progress with no worker by construction, so
// Charon's orphan recovery correctly refuses to reopen it — the chain then
// starves: srv-jobfile 2026-10-05, 2 escalated heads, br ready=0, zero
// spawns for 3.5h). A chain-head escalation cannot wait, so it becomes a
// Pan candidate after wedgeGrace with a reason that tells the judge the
// chain is stalled. A leaf escalation becomes a candidate after leafGrace
// (default 24h, configurable via pan.escalated_leaf_grace_seconds): beyond
// grace it also gets the repair:clear-escalation preference, because an
// escalation whose cause was a daemon defect fixed since the escalation
// should be retried automatically rather than sitting a day for a human
// (srv-jobfile-eci, 2026-10-05: gate-extraction defect fixed by deploy,
// bead idled escalated overnight with no path back).
func panBlockedReason(b types.Bead, now time.Time, wedgeGrace, leafGrace time.Duration, blockedDependents int) (string, bool) {
	return panBlockedReasonWithRetest(b, now, wedgeGrace, leafGrace, blockedDependents, nil, 0)
}

// retestSilenceThreshold: integration-pending age beyond which ZERO recorded
// retest attempts is reported to Pan as RETEST SILENCE (hydra-1hbo). ~1.5x
// the default hourly RetryInterval so normal waiting never trips it.
const retestSilenceThreshold = 90 * time.Minute

// panBlockedReasonWithRetest is panBlockedReason with the retest-stage
// attempt counters (hydra-1hbo). stats==nil keeps the legacy wording.
// maxAttempts comes from the stage config so the exhausted-branch reads
// correctly regardless of yaml tuning.
func panBlockedReasonWithRetest(b types.Bead, now time.Time, wedgeGrace, leafGrace time.Duration, blockedDependents int, stats func(beadID string) (int, time.Time, bool), maxAttempts int) (string, bool) {
	if b.Status.IsTerminal() {
		return "", false
	}
	if leafGrace <= 0 {
		leafGrace = 24 * time.Hour
	}
	for _, lbl := range b.Labels {
		switch lbl {
		case worktreeUnavailableLabel, integrationExhaustedLabel:
			return fmt.Sprintf("hard-parked by label %s (durable; scheduler never retries — clear only when the cause is fixed)", lbl), true
		case integrationPendingLabel:
			age := now.Sub(b.UpdatedAt)
			if stats != nil {
				attempts, last, ok := stats(b.ID.String())
				if ok && attempts == 0 && age > retestSilenceThreshold {
					return fmt.Sprintf("RETEST SILENCE: work complete, parked %v with ZERO recorded retest attempts (selector mismatch suspected — hydra-1hbo data) — escalate: file a bead and alert; do NOT assume mechanical retries are happening", age.Round(time.Minute)), true
				}
				if ok && maxAttempts > 0 && attempts >= maxAttempts {
					return fmt.Sprintf("work complete, integrate failed %v ago; retest attempts EXHAUSTED (%d/%d) — repair or human action required", age.Round(time.Minute), attempts, maxAttempts), true
				}
				if ok && attempts > 0 && !last.IsZero() {
					return fmt.Sprintf("work complete, integrate failed %v ago; retest ACTIVE (attempt %d, last %v ago) — leave parked, the stage owns retry", age.Round(time.Minute), attempts, now.Sub(last).Round(time.Minute)), true
				}
			}
			return fmt.Sprintf("work complete, integrate/push failed %v ago (label %s) — parked, NOT reopened; the integration-retest stage retries it mechanically, repair only if the retest attempts are exhausted", age.Round(time.Minute), lbl), true
		case gitOwnershipLabel:
			return "hard-parked by label git-ownership (git refused the repo as the hydra user; fix is: git config --global --add safe.directory <repo-path> — human/T3 action)", true
		case escalatedLabel:
			age := now.Sub(b.UpdatedAt)
			if blockedDependents > 0 && wedgeGrace > 0 && age > wedgeGrace {
				return fmt.Sprintf("escalated to a human %v ago and WEDGING %d dependent bead(s) — the dependency chain is stalled and no human has acted; prefer repair:clear-escalation so the bead gets one fair retry (the escalation re-fires if the failure recurs)", age.Round(time.Minute), blockedDependents), true
			}
			if age > leafGrace {
				return fmt.Sprintf("escalated to a human %v ago with no dependents blocked — beyond the %v leaf grace; prefer repair:clear-escalation so the bead gets one fair retry (the escalation re-fires if the failure recurs; if it was a daemon defect that has since been fixed, the retry should just pass)", age.Round(time.Minute), leafGrace), true
			}
		case strandedConflictLabel:
			return "quarantined by integrate-stranded (merge conflict) — human review or re-attempt needed", true
		}
	}
	return "", false
}

// spawnFailureTTL bounds how long a recorded spawn-failure entry stays
// visible to Pan after its cause could not have produced a live worker.
const spawnFailureTTL = 15 * time.Minute

// staleSpawnFailure reports whether a recorded spawn-failure entry can no
// longer represent a real blocker (fmsfe-npa 2026-10-02): superseded by a
// live registry worker, or older than the TTL with no live worker (terminal
// beads never respawn, so the TTL eventually clears their entries too).
// Fail-open toward keeping the entry visible: only positive evidence purges.
func staleSpawnFailure(spawner *WorkerSpawner, beadID string, since time.Time) bool {
	if spawner == nil {
		return false
	}
	if spawner.HasLiveRegistryWorker(beadID) {
		return true
	}
	return !since.IsZero() && time.Since(since) > spawnFailureTTL
}
