package core

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
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

	// Start orphan process janitor
	if o.orphanJanitor != nil {
		o.orphanJanitor.Start()
		log.Println("🧹 Orphan janitor started")
	}

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

		panCfg := o.config.Shepherd.Pan
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
					for _, project := range o.projects {
						for _, target := range repoScanTargets(project) {
							beads, err := o.beadsClient.ListBeads(target, "")
							if err != nil {
								continue // fail open: one unreadable store must not blind the scan
							}
							for _, b := range beads {
								if b.Status.IsTerminal() {
									continue
								}
								for _, lbl := range b.Labels {
									switch lbl {
									case worktreeUnavailableLabel, integrationExhaustedLabel:
										blocked = append(blocked, pan.BlockedBead{
											BeadID: b.ID.String(),
											Reason: fmt.Sprintf("hard-parked by label %s (durable; scheduler never retries — clear only when the cause is fixed)", lbl),
											Since:  b.UpdatedAt,
										})
									case escalatedLabel:
										if time.Since(b.UpdatedAt) > 24*time.Hour {
											blocked = append(blocked, pan.BlockedBead{
												BeadID: b.ID.String(),
												Reason: "escalated to a human over 24h ago — stale escalation or genuinely waiting?",
												Since:  b.UpdatedAt,
											})
										}
									}
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
		}, o.registryStore, nil)

		// Resolve project path for a bead ID using the configured projects.
		// Falls back to repo-name and path-basename matches: multi-user discovery
		// registers projects as "<user>/<repo>" while bead IDs carry only the repo
		// name — the exact mismatch that made Pan's close paths fail 100% with
		// "project path not found" (2026-09-11/13).
		projectPathForID := func(id types.BeadID) string {
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
							if r.Path != "" {
								return r.Path
							}
							return filepath.Join(p.Path, r.Name)
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
			base := currentBranch(projectPath)
			changed, err := changedFilesWithBase(wt, base)
			if err != nil {
				log.Printf("[Pan] zero-diff check failed for %s (treating as unknown): %v", beadID, err)
				return false, false
			}
			return len(changed) == 0, true
		})
		o.panShepherd.SetOnCloseSatisfied(BuildCloseSatisfied(o))
		o.panShepherd.SetOnAbandonWithSummary(BuildAbandonWithSummary(o))

		// Pan repair handlers: whitelisted daemon-side actions that clear
		// scheduler-level blockers without a worker command. They are
		// fail-open: errors are logged and emitted as audit events only.
		o.panShepherd.SetRepairHandlers(map[string]func(beadID string) error{
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
					if lbl == worktreeUnavailableLabel || lbl == integrationExhaustedLabel {
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
	o.projects = projectScanner.Projects()
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

	return nil
}

// Stop stops all background services.
func (s *ServiceStarter) Stop() {
	o := s.overseer

	if o.orphanJanitor != nil {
		o.orphanJanitor.Stop()
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
}
