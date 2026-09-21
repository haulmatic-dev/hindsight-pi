package core

import (
	"log"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
)

// chainBranchTipOf returns the commit at the head of the given branch in
// projectPath, or "" when it does not exist there — the exact shape
// Integrate silently treats as "nothing to integrate" success. Captured
// before teardown so integrate can be verified as landed (or honestly
// skipped) after the worktree is gone.
func chainBranchTipOf(projectPath, branch string) string {
	if branch == "" {
		return ""
	}
	if !branchExists(projectPath, branch) {
		return ""
	}
	tip, err := gitOut(projectPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(tip)
}

func chainBranchTip(m *WorktreeManager, projectPath, key string) string {
	if m == nil {
		return ""
	}
	return chainBranchTipOf(projectPath, m.branchName(key))
}

// resolveChainBranch returns the branch chain completion must integrate,
// in priority order, each fallback logged loudly:
//  1. the branch RECORDED at chain-worktree creation (ChainInfo.WorktreeBranch) —
//     the only name guaranteed correct when WorktreeManager versioned the
//     branch (hydra/wt-<key>-vN, webapp-tc5 incident);
//  2. exact hydra/wt-<key>;
//  3. newest hydra/wt-<key>-vN by N.
//
// Returns "" only when none exist — a genuinely absent worktree, which the
// caller must report as a typed skip (chain_integration_unverified), never
// a silent success.
func resolveChainBranch(m *WorktreeManager, projectPath, key, recorded string) string {
	if recorded != "" {
		return recorded
	}
	exact := m.branchName(key)
	if branchExists(projectPath, exact) {
		return exact
	}
	if newest := newestVersionedBranch(m, projectPath, key); newest != "" {
		log.Printf("[Worktree] chain %s: no exact branch %s — fell back to versioned branch %s (recorded key missing; reconcile the registrar path)", key, exact, newest)
		return newest
	}
	return ""
}

// ChainCompletionInitializer creates and configures the chain completion detector.
type ChainCompletionInitializer struct {
	overseer *Overseer
}

// NewChainCompletionInitializer creates a new initializer.
func NewChainCompletionInitializer(overseer *Overseer) *ChainCompletionInitializer {
	return &ChainCompletionInitializer{overseer: overseer}
}

// Initialize creates and configures the chain completion detector.
func (i *ChainCompletionInitializer) Initialize() {
	o := i.overseer
	config := ChainCompletionConfig{
		Enabled:           o.config.Overseer.ChainDetection.Enabled,
		DetectionInterval: o.config.Overseer.ChainDetection.Interval,
		Timeout:           o.config.Overseer.ChainDetection.Timeout,
	}

	if config.DetectionInterval == 0 {
		config.DetectionInterval = 10 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}

	o.chainCompletionDetector = NewChainCompletionDetector(config, o)
	o.chainCompletionDetector.Start()
	if o.workerSpawner != nil {
		o.workerSpawner.SetChainCompletionDetector(o.chainCompletionDetector)
		o.workerSpawner.SetEventSink(o.eventSink)
		if o.config != nil && o.config.ContextPack.Scout.Enabled {
			sc := resolveScoutConfig(o.config)
			o.workerSpawner.SetScoutRunner(&PiScoutRunner{
				Provider:   sc.provider,
				Model:      sc.model,
				APIKeyName: sc.apiKeyName,
				TurnBudget: sc.turnBudget,
			})
		}
	}

	extractor := NewChainPatternExtractor(
		o,
		intelligence.NewQualityGateService(),
		nil,
	)

	o.chainCompletionDetector.SetCallback(func(event ChainCompletedEvent) {
		key := event.ChainInfo.RootBeadID.String()
		log.Printf("[Overseer] Chain completed: root=%s", event.ChainInfo.RootBeadID)

		// Integrate against the path RECORDED at chain registration. The old
		// name lookup (findProjectPathForBeadID) returns the FIRST project
		// with a matching name — with two projects named "webapp" it picked
		// the wrong one, the chain's branch did not exist there, and
		// Integrate's "nothing to integrate" nil return read as success:
		// 17/17 beads closed with the work unintegrated (zpf incident,
		// 2026-09-16). Pre-upgrade chains have no recorded path; they fall
		// back to name resolution with the fallback logged.
		projectPath := event.ChainInfo.ProjectPath
		if projectPath == "" {
			projectPath = o.findProjectPathForBeadID(event.ChainInfo.RootBeadID)
			if projectPath != "" {
				log.Printf("[Overseer] chain %s: empty recorded ProjectPath, fell back to name resolution: %s", key, projectPath)
			}
		}
		if projectPath == "" {
			log.Printf("[Overseer] chain %s: SKIPPING PRD validation and integrate — no project path (recorded ProjectPath empty and name resolution found nothing)", key)
		} else {
			if err := o.RunPRDValidation(event.ChainInfo, projectPath); err != nil {
				log.Printf("[Overseer] chain %s: PRD validation failed: %v", key, err)
			} else {
				log.Printf("[Overseer] chain %s: PRD validation passed (%s)", key, projectPath)
			}
		}

		// Per-chain worktree isolation. "Completed" only means every bead reached
		// a terminal state — a chain whose beads closed as FAILED used to
		// integrate here exactly like a successful one, which left a
		// deliberately red test on the testbed's working branch (2026-07-28).
		// Failed chains now quarantine: work preserved on hydra/quarantine/<key>
		// for the fix bead to inspect, working branch untouched.
		if o.worktreeManager == nil {
			log.Printf("[Overseer] chain %s: worktree manager unavailable, integrate skipped", key)
		} else if projectPath != "" {
			// Resolve the REAL branch (recorded key first, then exact, then
			// newest -vN): silent branch versioning means the branch to merge
			// is not necessarily hydra/wt-<key> (webapp-tc5 stranded 1,642
			// insertions this way, 2026-09-16).
			branch := resolveChainBranch(o.worktreeManager, projectPath, key, event.ChainInfo.WorktreeBranch)
			if o.chainCompletionDetector != nil && o.chainCompletionDetector.ChainHasFailedBeads(event.ChainInfo.AllBeadIDs) {
				if qBranch, qErr := o.worktreeManager.QuarantineBranchAndRemove(projectPath, key, branch); qErr != nil {
					log.Printf("[Worktree] quarantine failed for chain %s: %v", event.ChainInfo.RootBeadID, qErr)
				} else if qBranch != "" {
					log.Printf("[Worktree] quarantined failed chain %s on %s (not integrated)", event.ChainInfo.RootBeadID, qBranch)
					if o.beadCloser != nil && o.beadCloser.events != nil {
						o.beadCloser.events.EmitRepo(projectPath, Event{Type: "chain_quarantined",
							Bead: key, Reason: "failed_beads_in_chain",
							Fields: map[string]any{"branch": qBranch}})
					}
				} else {
					log.Printf("[Worktree] chain %s: quarantine empty — no worktree or branch in %s (nothing preserved)", key, projectPath)
				}
			} else {
				// Capture the branch tip BEFORE teardown. Integrate returns nil
				// when neither worktree nor branch exists in projectPath — the
				// exact silent no-op that closed the zpf chain unintegrated —
				// so an empty tip is a skip with reason, and a captured tip lets
				// us verify after teardown that the merge actually landed
				// (closed != landed detector).
				tip := chainBranchTipOf(projectPath, branch)
				if tip == "" {
					log.Printf("[Worktree] chain %s: SKIPPING integrate in %s — no worktree or hydra/wt branch (nothing to integrate)", key, projectPath)
					if o.beadCloser != nil && o.beadCloser.events != nil {
						o.beadCloser.events.EmitRepo(projectPath, Event{Type: "chain_integration_unverified",
							Bead: key, Reason: "no_worktree_or_branch_after_resolution",
							Fields: map[string]any{"resolved_branch": branch}})
					}
				} else if err := o.worktreeManager.IntegrateBranchAndRemove(projectPath, key, branch); err != nil {
					log.Printf("[Worktree] integrate failed for chain %s: %v", event.ChainInfo.RootBeadID, err)
				} else if isAncestorOf(projectPath, tip, "HEAD") {
					log.Printf("[Worktree] integrated chain %s into %s", key, projectPath)
				} else {
					// Integrate's own post-merge ancestor check failed — the
					// landing is unverified (closed != landed detector).
					log.Printf("[Worktree] UNVERIFIED integrate for chain %s: pre-teardown tip %s not contained in HEAD of %s", key, tip, projectPath)
					if o.beadCloser != nil && o.beadCloser.events != nil {
						o.beadCloser.events.EmitRepo(projectPath, Event{Type: "chain_integration_unverified",
							Bead: key, Reason: "tip_not_contained_in_head",
							Fields: map[string]any{"tip": tip}})
					}
					if o.beadsClient != nil {
						if err := o.beadsClient.UpdateBeadLabels(projectPath, "", event.ChainInfo.RootBeadID, []string{"integration-unverified"}); err != nil {
							log.Printf("[Worktree] chain %s: failed to stamp integration-unverified label: %v", key, err)
						}
					}
				}
			}
		}

		if err := extractor.ExtractPatterns(event); err != nil {
			log.Printf("[Overseer] Chain pattern extraction failed: %v", err)
		}
	})

	log.Printf("[ChainCompletion] Detector initialized (interval: %v, timeout: %v)",
		config.DetectionInterval, config.Timeout)
}
