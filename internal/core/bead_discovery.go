package core

import (
	"fmt"
	"log"
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
		beads, err := d.beadsClient.ListBeads(project.Path, "")
		if err != nil {
			// A single project's list failure (e.g. a stale path, a parent dir
			// with no .beads, or a transient br error) must not abort discovery
			// for every other project. Skip it this tick and continue.
			log.Printf("⚠️  [br] list failed for project %s: %v — skipping this project this tick", project.Name, err)
			continue
		}
		allReadyBeads = append(allReadyBeads, beads...)
	}

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

// findFromBV retrieves beads using bv --robot-triage.
func (d *BeadDiscovery) findFromBV(activeWorkers map[string]bool) ([]types.Bead, error) {
	if d.bvClient == nil {
		return nil, fmt.Errorf("bv client not initialized")
	}

	var allReadyBeads []types.Bead

	for _, project := range d.projects {
		triage, err := d.bvClient.GetTriage(project.Path)
		if err != nil {
			// Per-project isolation: one project's triage failure must not
			// abort the whole scheduler tick. Skip it and keep scheduling the
			// rest.
			log.Printf("⚠️  [BV] triage failed for project %s: %v — skipping this project this tick", project.Name, err)
			continue
		}

		log.Printf("[BV] Triage for %s: %d recommendations, %d ready, %d blocked",
			project.Name, len(triage.Recommendations), triage.ReadyCount, triage.BlockedCount)

		for _, rec := range triage.Recommendations {
			beadID := types.BeadID(rec.BeadID)
			beadIDStr := string(beadID)

			if activeWorkers[beadIDStr] {
				continue
			}
			if d.completed[beadIDStr] {
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

			var repoName string
			if project.Type != types.RepoTypeSingle && len(project.Repos) > 0 {
				repoName = project.Repos[0].Name
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
			if d.completed[beadIDStr] {
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

			var repoName string
			if project.Type != types.RepoTypeSingle && len(project.Repos) > 0 {
				repoName = project.Repos[0].Name
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
		plan, err := d.bvClient.GetPlan(project.Path)
		if err != nil {
			// Per-project isolation: skip a project whose plan failed rather
			// than aborting planning for every project this tick.
			log.Printf("⚠️  [BV-Plan] plan failed for project %s: %v — skipping this project this tick", project.Name, err)
			continue
		}

		log.Printf("[BV-Plan] Plan for %s: %d tracks, %d total actionable beads",
			project.Name, len(plan.Tracks), plan.TotalActionable)

		for _, track := range plan.Tracks {
			log.Printf("[BV-Plan] Track %s: %d beads", track.TrackID, len(track.Items))

			for _, item := range track.Items {
				beadID := types.BeadID(item.ID)
				beadIDStr := string(beadID)

				if activeWorkers[beadIDStr] {
					continue
				}
				if d.completed[beadIDStr] {
					continue
				}

				var repoName string
				if project.Type != types.RepoTypeSingle && len(project.Repos) > 0 {
					repoName = project.Repos[0].Name
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
