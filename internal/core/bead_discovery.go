package core

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"sync"

	"strings"

	"github.com/haulmatic-dev/hydra/internal/bv"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BeadDiscovery finds beads that are ready to execute from various sources.
type BeadDiscovery struct {
	beadsClient BeadsClient
	bvClient    *bv.Client
	projects    []types.Project
	completed   map[string]bool
	useBV       bool
	dag         *DAG
	dagMu       *sync.RWMutex
	storeGuard  *BeadStoreGuard // nil = corruption guard disabled
}

// NewBeadDiscovery creates a new bead discovery service.
func NewBeadDiscovery(beadsClient BeadsClient, bvClient *bv.Client, projects []types.Project, completed map[string]bool, dag *DAG, dagMu *sync.RWMutex) *BeadDiscovery {
	return &BeadDiscovery{
		beadsClient: beadsClient,
		bvClient:    bvClient,
		projects:    projects,
		completed:   completed,
		dag:         dag,
		dagMu:       dagMu,
	}
}

// SetProjects updates the projects list for bead discovery.
// Must be called after project discovery to ensure beads can be found.
func (d *BeadDiscovery) SetProjects(projects []types.Project) {
	d.projects = projects
}

// SetStoreGuard attaches the bead-store corruption guard. When set, projects
// whose store the guard has blocked are excluded from every discovery path
// (fail-closed scheduling) until a verified restore unblocks them.
func (d *BeadDiscovery) SetStoreGuard(g *BeadStoreGuard) {
	d.storeGuard = g
}

// guardBlocked reports whether the store guard has excluded this project.
func (d *BeadDiscovery) guardBlocked(project types.Project) bool {
	return d.storeGuard != nil && d.storeGuard.IsBlocked(project.Path)
}

// FindReadyBeads retrieves beads that are ready to execute.
// activeWorkers should be a snapshot of currently running worker IDs.
// Priority: 1) bv --robot-plan, 2) br ready, 3) bv --robot-triage
func (d *BeadDiscovery) FindReadyBeads(activeWorkers map[string]bool) ([]types.Bead, error) {
	// 1. Try bv --robot-plan first
	if d.bvClient != nil {
		planBeads, planErr := d.findFromBVPlan(activeWorkers)
		if planErr != nil {
			log.Printf("⚠️  [BV-Plan] failed: %v", planErr)
		}
		if planErr == nil && len(planBeads) > 0 {
			log.Printf("✅ Using bv robot-plan: found %d actionable beads", len(planBeads))
			return planBeads, nil
		}
	}

	// 2. Fallback to br ready
	brBeads, brErr := d.findFromBR(activeWorkers)
	if brErr != nil {
		log.Printf("⚠️  br ready failed: %v", brErr)
	}
	if brErr == nil && len(brBeads) > 0 {
		log.Printf("✅ Using br ready: found %d unblocked beads", len(brBeads))
		return brBeads, nil
	}

	// 3. Fallback to bv --robot-triage
	if d.bvClient != nil {
		log.Printf("⚠️  br ready failed or returned 0 beads, falling back to bv triage")
		beads, err := d.findFromBV(activeWorkers)
		if err != nil {
			return nil, fmt.Errorf("bv triage failed: %w", err)
		}
		return beads, nil
	}

	if brErr != nil {
		return nil, fmt.Errorf("all bead discovery methods failed: br: %w", brErr)
	}
	return nil, fmt.Errorf("no beads found")
}

// findFromBR retrieves beads using br ready.
func (d *BeadDiscovery) findFromBR(activeWorkers map[string]bool) ([]types.Bead, error) {
	var allReadyBeads []types.Bead
	for _, project := range d.projects {
		if d.guardBlocked(project) {
			// Fail-closed: store presumed corrupt, excluded until restored.
			continue
		}
		// Multi projects: br must run per-repo, never in the parent dir (which
		// has no .beads and would fail every list — 2026-09-11 webapp kills).
		// The store guard sees the project aggregate and only when every repo
		// listed cleanly — a partial list would corrupt the open-count baseline.
		var projectBeads []types.Bead
		allReposListed := true
		for _, target := range repoScanTargets(project) {
			beads, err := d.beadsClient.ListBeads(target, "")
			if err != nil {
				// A single repo's list failure (e.g. a stale path, a parent dir
				// with no .beads, or a transient br error) must not abort discovery
				// for every other project. Skip it this tick and continue.
				log.Printf("⚠️  [br] list failed for project %s (path %s): %v — skipping this repo this tick", project.Name, target, err)
				if d.storeGuard != nil {
					d.storeGuard.NoteListFailure(project, err)
				}
				allReposListed = false
				continue
			}
			projectBeads = append(projectBeads, beads...)
		}
		if allReposListed && d.storeGuard != nil {
			d.storeGuard.Check(project, projectBeads)
			if d.guardBlocked(project) {
				// Signal fired this tick: exclude the project's beads
				// immediately rather than scheduling from a corrupt store.
				continue
			}
		}
		allReadyBeads = append(allReadyBeads, projectBeads...)
	}

	// The completed set is a cache of "bead closed in the store". A close
	// that was later rejected/escalated (bead left in_progress) or a bead
	// reopened externally leaves a stale entry that would filter the bead
	// from scheduling until daemon restart (gybo4 2026-09-09: 'Filtered out
	// 1 beads: completed=1' for a bead whose last verdict was escalated;
	// spawned instantly after restart). Re-validate membership against the
	// store status we just listed before filtering.
	d.pruneStaleCompleted(allReadyBeads)

	filteredCounts := map[string]int{}
	var readyBeads []types.Bead

	for _, bead := range allReadyBeads {
		beadIDStr := string(bead.ID)

		if activeWorkers[beadIDStr] {
			filteredCounts["activeWorkers"]++
			continue
		}
		if d.completed[beadIDStr] {
			filteredCounts["completed"]++
			continue
		}
		if bead.Type == "approval-gate" {
			filteredCounts["approvalGate"]++
			continue
		}
		// bv's cache can be stale: never schedule a bead the store says is terminal.
		if bead.Status.IsTerminal() {
			continue
		}

		if bead.Type == "approval" && bead.Status == types.StatusInProgress {
			filteredCounts["approvalInProgress"]++
			continue
		}

		readyBeads = append(readyBeads, bead)
	}

	if len(allReadyBeads) > 0 && len(readyBeads) < len(allReadyBeads) {
		log.Printf("[FilterDebug] Filtered out %d beads: activeWorkers=%d, completed=%d, approvalGate=%d, approvalInProgress=%d",
			len(allReadyBeads)-len(readyBeads),
			filteredCounts["activeWorkers"],
			filteredCounts["completed"],
			filteredCounts["approvalGate"],
			filteredCounts["approvalInProgress"])
	}

	sort.SliceStable(readyBeads, func(i, j int) bool {
		return readyBeads[i].Priority < readyBeads[j].Priority
	})

	log.Printf("📋 Found %d ready beads from 'br ready' (after filtering)", len(readyBeads))
	return d.filterByDAG(readyBeads), nil
}

// pruneStaleCompleted drops completed-set entries whose listed store status
// is non-terminal. Terminal statuses (closed/completed/failed) genuinely
// belong in the set; anything else contradicts "closed" and means the close
// was rejected/escalated or the bead was reopened after being cached.
func (d *BeadDiscovery) pruneStaleCompleted(listed []types.Bead) {
	for _, bead := range listed {
		id := string(bead.ID)
		if !d.completed[id] || bead.Status.IsTerminal() {
			continue
		}
		delete(d.completed, id)
		log.Printf("[FilterDebug] Pruned stale completed entry for %s (store status %q; close rejected/escalated or reopened) — bead is schedulable again", id, bead.Status)
	}
}

// stillCompleted reports whether a completed-set entry is backed by a
// terminal store status. The set is a cache of "bead closed in the store";
// a close later rejected/escalated (bead left in_progress) or an external
// reopen leaves a stale entry that would filter the bead from scheduling
// until daemon restart (gybo4 2026-09-09). Non-terminal store evidence prunes
// the entry and reports not-completed so the bead schedules again. A store
// read failure keeps the cached entry (fail closed toward the filter) and
// re-checks next tick.
func (d *BeadDiscovery) stillCompleted(projectPath, repoName, beadIDStr string) bool {
	if !d.completed[beadIDStr] {
		return false
	}
	bead, err := d.beadsClient.GetBead(projectPath, repoName, types.BeadID(beadIDStr))
	if err != nil || bead == nil {
		return true
	}
	if bead.Status.IsTerminal() {
		return true
	}
	delete(d.completed, beadIDStr)
	log.Printf("[FilterDebug] Pruned stale completed entry for %s (store status %q; close rejected/escalated or reopened) — bead is schedulable again", beadIDStr, bead.Status)
	return false
}

// resolveRepoName returns the repo a bv item belongs to. If bv emits a
// per-item repo field, that value wins. For multi-repo projects we invoke
// bv per repo, so the scan target determines the source repo. The legacy
// Repos[0] fallback is only used for single-repo projects.
func (d *BeadDiscovery) resolveRepoName(project types.Project, target string, itemRepo string, fallbackLogged map[string]bool) string {
	if itemRepo != "" {
		return itemRepo
	}
	if project.Type != types.RepoTypeMulti {
		return ""
	}
	if len(project.Repos) == 1 {
		return project.Repos[0].Name
	}
	// bv emitted no per-item repo field for a multi-repo project. We are
	// invoking bv per repo, so the scan target tells us the source repo.
	rel, err := filepath.Rel(project.Path, target)
	if err == nil && rel != "." && !strings.Contains(rel, string(filepath.Separator)) {
		d.logRepoFallbackOnce(project.Name, rel, fallbackLogged)
		return rel
	}
	// Defensive fallback: should never happen for a valid multi-repo target.
	d.logRepoFallbackOnce(project.Name, "", fallbackLogged)
	return ""
}

func (d *BeadDiscovery) logRepoFallbackOnce(projectName string, repoName string, fallbackLogged map[string]bool) {
	if fallbackLogged[projectName] {
		return
	}
	fallbackLogged[projectName] = true
	log.Printf("⚠️  [BV] items for project %s do not include a repo field; attributing beads from target repo %q", projectName, repoName)
}

// findFromBV retrieves beads using bv --robot-triage.
func (d *BeadDiscovery) findFromBV(activeWorkers map[string]bool) ([]types.Bead, error) {
	if d.bvClient == nil {
		return nil, fmt.Errorf("bv client not initialized")
	}

	var allReadyBeads []types.Bead

	for _, project := range d.projects {
		if d.guardBlocked(project) {
			// Fail-closed: store presumed corrupt, excluded until restored.
			continue
		}

		targets := repoScanTargets(project)
		if project.Type == types.RepoTypeMulti && len(targets) == 0 {
			// Empty multi (no discovered repos with stores yet): bv has
			// nothing to triage and exits non-zero every tick. Skip quietly.
			continue
		}

		fallbackLogged := map[string]bool{}

		for _, target := range targets {
			triage, err := d.bvClient.GetTriage(target)
			if err != nil {
				// Per-project isolation: one project's triage failure must not
				// abort the whole scheduler tick. Skip it and keep scheduling the
				// rest.
				log.Printf("⚠️  [BV] triage failed for project %s (path %s): %v — skipping this repo this tick", project.Name, target, err)
				continue
			}

			log.Printf("[BV] Triage for %s/%s: %d recommendations, %d ready, %d blocked",
				project.Name, filepath.Base(target), len(triage.Recommendations), triage.ReadyCount, triage.BlockedCount)

			for _, rec := range triage.Recommendations {
				beadID := types.BeadID(rec.BeadID)
				beadIDStr := string(beadID)

				if activeWorkers[beadIDStr] {
					continue
				}

				repoName := d.resolveRepoName(project, target, rec.Repo, fallbackLogged)

				if d.stillCompleted(project.Path, repoName, beadIDStr) {
					continue
				}
				if len(rec.BlockedBy) > 0 {
					log.Printf("⏸️  [BV] Skipping blocked bead: %s (blocked by: %v)", beadID, rec.BlockedBy)
					continue
				}
				if rec.Type == "approval-gate" {
					log.Printf("⏸️  [BV] Skipping approval gate: %s", beadID)
					continue
				}

				bead, err := d.beadsClient.GetBead(project.Path, repoName, beadID)
				if err != nil {
					bead = &types.Bead{
						ID:          beadID,
						Title:       rec.Title,
						Type:        rec.Type,
						Priority:    rec.Priority,
						Project:     project.Name,
						Repo:        repoName,
						Status:      types.StatusOpen,
						Labels:      rec.Labels,
						Description: rec.Reason,
						Metadata: map[string]string{
							"bv_confidence":     fmt.Sprintf("%f", rec.Confidence),
							"bv_reason":         rec.Reason,
							"bv_estimated_time": rec.EstimatedTime,
						},
					}
				}

				// bv's cache can be stale: never schedule a bead the store says is terminal.
				if bead.Status.IsTerminal() {
					continue
				}

				if bead.Type == "approval" && bead.Status == types.StatusInProgress {
					log.Printf("⏸️  [BV] Skipping approval bead (waiting for user approval): %s", beadID)
					continue
				}

				if bead.Metadata == nil {
					bead.Metadata = make(map[string]string)
				}
				bead.Metadata["bv_confidence"] = fmt.Sprintf("%f", rec.Confidence)
				bead.Metadata["bv_reason"] = rec.Reason

				allReadyBeads = append(allReadyBeads, *bead)
			}

			for _, blocker := range triage.BlockersToClear {
				if !blocker.Actionable {
					continue
				}

				beadID := types.BeadID(blocker.BeadID)
				beadIDStr := string(beadID)

				if activeWorkers[beadIDStr] {
					continue
				}

				repoName := d.resolveRepoName(project, target, blocker.Repo, fallbackLogged)

				if d.stillCompleted(project.Path, repoName, beadIDStr) {
					continue
				}

				alreadyAdded := false
				for _, existing := range allReadyBeads {
					if existing.ID == beadID {
						alreadyAdded = true
						break
					}
				}
				if alreadyAdded {
					continue
				}

				if len(blocker.BlockedBy) > 0 {
					log.Printf("⏸️  [BV] Skipping blocked bead from blockers_to_clear: %s (blocked by: %v)", beadID, blocker.BlockedBy)
					continue
				}

				bead, err := d.beadsClient.GetBead(project.Path, repoName, beadID)
				if err != nil {
					bead = &types.Bead{
						ID:       beadID,
						Title:    blocker.Title,
						Type:     "task",
						Priority: 3,
						Project:  project.Name,
						Repo:     repoName,
						Status:   types.StatusOpen,
						Metadata: map[string]string{
							"bv_source":     "blockers_to_clear",
							"bv_unblocks":   fmt.Sprintf("%d", blocker.UnblocksCount),
							"bv_confidence": "0.8",
							"bv_reason":     fmt.Sprintf("Unblocks %d items", blocker.UnblocksCount),
						},
					}
				}

				if bead.Metadata == nil {
					bead.Metadata = make(map[string]string)
				}
				bead.Metadata["bv_source"] = "blockers_to_clear"
				bead.Metadata["bv_unblocks"] = fmt.Sprintf("%d", blocker.UnblocksCount)
				bead.Metadata["bv_confidence"] = "0.8"
				bead.Metadata["bv_reason"] = fmt.Sprintf("Unblocks %d items: %v", blocker.UnblocksCount, blocker.UnblocksIDs)

				allReadyBeads = append(allReadyBeads, *bead)
			}
		}
	}

	sort.SliceStable(allReadyBeads, func(i, j int) bool {
		confI := 0.0
		confJ := 0.0
		fmt.Sscanf(allReadyBeads[i].Metadata["bv_confidence"], "%f", &confI)
		fmt.Sscanf(allReadyBeads[j].Metadata["bv_confidence"], "%f", &confJ)
		if confI != confJ {
			return confI > confJ
		}
		return allReadyBeads[i].Priority < allReadyBeads[j].Priority
	})

	log.Printf("📋 [BV] Found %d ready beads from triage (after filtering)", len(allReadyBeads))
	return d.filterByDAG(allReadyBeads), nil
}

// findFromBVPlan retrieves beads using bv --robot-plan.
func (d *BeadDiscovery) findFromBVPlan(activeWorkers map[string]bool) ([]types.Bead, error) {
	if d.bvClient == nil {
		return nil, fmt.Errorf("bv client not initialized")
	}

	var allReadyBeads []types.Bead

	for _, project := range d.projects {
		if d.guardBlocked(project) {
			// Fail-closed: store presumed corrupt, excluded until restored.
			continue
		}

		targets := repoScanTargets(project)
		if project.Type == types.RepoTypeMulti && len(targets) == 0 {
			// Empty multi (no discovered repos with stores yet): bv has
			// nothing to plan and exits non-zero every tick. Skip quietly.
			continue
		}

		fallbackLogged := map[string]bool{}

		for _, target := range targets {
			plan, err := d.bvClient.GetPlan(target)
			if err != nil {
				// Per-project isolation: skip a project whose plan failed rather
				// than aborting planning for every project this tick.
				log.Printf("⚠️  [BV-Plan] plan failed for project %s (path %s): %v — skipping this repo this tick", project.Name, target, err)
				continue
			}

			log.Printf("[BV-Plan] Plan for %s/%s: %d tracks, %d total actionable beads",
				project.Name, filepath.Base(target), len(plan.Tracks), plan.TotalActionable)

			for _, track := range plan.Tracks {
				log.Printf("[BV-Plan] Track %s: %d beads", track.TrackID, len(track.Items))

				for _, item := range track.Items {
					beadID := types.BeadID(item.ID)
					beadIDStr := string(beadID)

					repoName := d.resolveRepoName(project, target, item.Repo, fallbackLogged)

					if activeWorkers[beadIDStr] {
						continue
					}
					if d.stillCompleted(project.Path, repoName, beadIDStr) {
						continue
					}

					bead, err := d.beadsClient.GetBead(project.Path, repoName, beadID)
					if err != nil {
						log.Printf("⚠️  [BV-Plan] Could not get bead details for %s: %v", beadID, err)
						bead = &types.Bead{
							ID:       beadID,
							Title:    item.Title,
							Type:     "task",
							Priority: item.Priority,
							Project:  project.Name,
							Repo:     repoName,
							Status:   types.StatusOpen,
							Metadata: map[string]string{
								"bv_plan_track": track.TrackID,
								"bv_unblocks":   strings.Join(item.Unblocks, ","),
							},
						}
					}

					// bv's cache can be stale: never schedule a bead the store says is terminal.
					if bead.Status.IsTerminal() {
						continue
					}

					if bead.Metadata == nil {
						bead.Metadata = make(map[string]string)
					}
					bead.Metadata["bv_plan_track"] = track.TrackID
					if len(item.Unblocks) > 0 {
						bead.Metadata["bv_unblocks"] = strings.Join(item.Unblocks, ",")
					}

					allReadyBeads = append(allReadyBeads, *bead)
				}
			}
		}
	}

	sort.SliceStable(allReadyBeads, func(i, j int) bool {
		priorityI := 0.0
		priorityJ := 0.0
		fmt.Sscanf(allReadyBeads[i].Metadata["bv_plan_priority"], "%f", &priorityI)
		fmt.Sscanf(allReadyBeads[j].Metadata["bv_plan_priority"], "%f", &priorityJ)
		if priorityI != priorityJ {
			return priorityI > priorityJ
		}
		return allReadyBeads[i].Priority < allReadyBeads[j].Priority
	})

	log.Printf("📋 [BV-Plan] Found %d ready beads from plan (after filtering)", len(allReadyBeads))
	return d.filterByDAG(allReadyBeads), nil
}

// filterByDAG intersects candidate beads with the DAG's ready nodes.
// This prevents duplicate scheduling when external sources (br, bv) return beads
// whose dependencies are not yet satisfied in our view of the world.
func (d *BeadDiscovery) filterByDAG(candidates []types.Bead) []types.Bead {
	if d.dag == nil || d.dagMu == nil {
		return candidates
	}
	d.dagMu.RLock()
	readyNodes := d.dag.GetReadyNodes()
	d.dagMu.RUnlock()

	readySet := make(map[string]bool, len(readyNodes))
	for _, node := range readyNodes {
		readySet[string(node.Bead.ID)] = true
	}

	var filtered []types.Bead
	for _, bead := range candidates {
		if readySet[string(bead.ID)] {
			filtered = append(filtered, bead)
		}
	}

	if len(filtered) != len(candidates) {
		log.Printf("[DAG-Filter] Filtered %d candidates down to %d DAG-ready beads", len(candidates), len(filtered))
	}
	return filtered
}
