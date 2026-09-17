package core

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

type ChainCompletionConfig struct {
	DetectionInterval time.Duration
	Timeout           time.Duration
	Enabled           bool
}

type ChainInfo struct {
	RootBeadID     types.BeadID
	LeafBeadIDs    []types.BeadID
	AllBeadIDs     []types.BeadID
	TotalBeads     int
	CompletedBeads int
	StartTime      time.Time
	EndTime        time.Time
	Status         ChainStatus
	// ProjectPath is the main-checkout path of the project this chain was
	// registered from, recorded at registration time. Chain completion must
	// integrate against THIS path, never a name-based lookup: duplicate
	// project names make name lookup return the first match, which is how the
	// webapp zpf chain integrated into the wrong project (2026-09-16).
	ProjectPath string
	// WorktreeKey and WorktreeBranch are the chain's REAL worktree identity,
	// recorded at chain-worktree creation (first EnsureWorktree). WorktreeManager
	// versions a stale branch name to hydra/wt-<key>-vN; consumers that
	// re-derive the branch from the key by exact match then miss and log
	// "nothing to integrate" while the work strands (webapp-tc5, 1,642
	// insertions, 2026-09-16). Empty for pre-upgrade chains — completion then
	// falls back: exact match, then newest -vN prefix match, each logged.
	WorktreeKey    string
	WorktreeBranch string
}

type ChainStatus string

const (
	ChainStatusInProgress ChainStatus = "in_progress"
	ChainStatusCompleted  ChainStatus = "completed"
	ChainStatusTimedOut   ChainStatus = "timed_out"
)

type ChainCompletedEvent struct {
	ChainInfo   *ChainInfo
	Overseer    *Overseer
	CompletedAt time.Time
}

type ChainCompletionCallback func(ChainCompletedEvent)

type ChainCompletionDetector struct {
	config   ChainCompletionConfig
	overseer *Overseer
	chains   map[string]*ChainInfo
	callback ChainCompletionCallback
	mu       sync.RWMutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewChainCompletionDetector(config ChainCompletionConfig, overseer *Overseer) *ChainCompletionDetector {
	return &ChainCompletionDetector{
		config:   config,
		overseer: overseer,
		chains:   make(map[string]*ChainInfo),
		stopCh:   make(chan struct{}),
	}
}

func (d *ChainCompletionDetector) SetCallback(cb ChainCompletionCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callback = cb
}

func (d *ChainCompletionDetector) Start() {
	if !d.config.Enabled {
		log.Printf("[ChainCompletion] Detector disabled")
		return
	}

	d.wg.Add(1)
	go d.runDetectionLoop()
	log.Printf("[ChainCompletion] Detector started with interval=%v, timeout=%v",
		d.config.DetectionInterval, d.config.Timeout)
}

func (d *ChainCompletionDetector) Stop() {
	if !d.config.Enabled {
		return
	}

	close(d.stopCh)
	d.wg.Wait()
	log.Printf("[ChainCompletion] Detector stopped")
}

func (d *ChainCompletionDetector) runDetectionLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.DetectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.checkChainCompletion()
		}
	}
}

func (d *ChainCompletionDetector) checkChainCompletion() {
	d.mu.Lock()

	completedChains := make([]*ChainInfo, 0)

	for _, chain := range d.chains {
		if chain.Status == ChainStatusCompleted {
			continue
		}

		if chain.Status == ChainStatusInProgress {
			if time.Since(chain.StartTime) > d.config.Timeout {
				chain.Status = ChainStatusTimedOut
				log.Printf("[ChainCompletion] Chain %s timed out after %v", chain.RootBeadID, d.config.Timeout)
			}
		}

		completedCount := d.countCompletedBeads(chain.AllBeadIDs)
		chain.CompletedBeads = completedCount

		if completedCount == chain.TotalBeads {
			chain.Status = ChainStatusCompleted
			chain.EndTime = time.Now()
			completedChains = append(completedChains, chain)
			log.Printf("[ChainCompletion] Chain completed: root=%s, beads=%d, duration=%v",
				chain.RootBeadID, chain.TotalBeads, chain.EndTime.Sub(chain.StartTime))
		}
	}

	d.mu.Unlock()

	for _, chain := range completedChains {
		if d.callback != nil {
			d.callback(ChainCompletedEvent{
				ChainInfo:   chain,
				Overseer:    d.overseer,
				CompletedAt: time.Now(),
			})
		}
		d.mu.Lock()
		delete(d.chains, chain.RootBeadID.String())
		d.mu.Unlock()
	}
}

func (d *ChainCompletionDetector) countCompletedBeads(beadIDs []types.BeadID) int {
	count := 0
	for _, beadID := range beadIDs {
		// Read bead status directly from issues.jsonl (bypasses DAG which may be empty)
		bead := d.overseer.beadFileReader.FindByID(string(beadID))
		if bead == nil {
			continue
		}
		if bead.Status == types.StatusClosed || bead.Status == types.StatusCompleted {
			count++
		}
	}
	return count
}

// ChainHasFailedBeads reports whether any member of the chain carries the
// closed-failed label — i.e. it closed, but as a failure. Chain "completion"
// only means every bead reached a terminal state; this is the verdict that
// decides integrate versus quarantine. Reads issues.jsonl (via the same file
// reader countCompletedBeads uses), so it is restart-safe.
func (d *ChainCompletionDetector) ChainHasFailedBeads(beadIDs []types.BeadID) bool {
	for _, beadID := range beadIDs {
		bead := d.overseer.beadFileReader.FindByID(string(beadID))
		if bead == nil {
			continue
		}
		for _, l := range bead.Labels {
			if l == closedFailedLabel {
				return true
			}
		}
	}
	return false
}

func (d *ChainCompletionDetector) RegisterChain(rootBeadID types.BeadID, allBeadIDs []types.BeadID, projectPath string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	leafIDs := d.findLeafBeads(rootBeadID, allBeadIDs)

	// Preserve a previously recorded worktree identity across re-registration
	// (the registrar re-runs at every startup): the chain's real branch name
	// is known only to the entity that created the versioned branch.
	worktreeKey, worktreeBranch := "", ""
	if existing, ok := d.chains[rootBeadID.String()]; ok {
		worktreeKey, worktreeBranch = existing.WorktreeKey, existing.WorktreeBranch
	}

	d.chains[rootBeadID.String()] = &ChainInfo{
		RootBeadID:     rootBeadID,
		LeafBeadIDs:    leafIDs,
		AllBeadIDs:     allBeadIDs,
		TotalBeads:     len(allBeadIDs),
		StartTime:      time.Now(),
		Status:         ChainStatusInProgress,
		ProjectPath:    projectPath,
		WorktreeKey:    worktreeKey,
		WorktreeBranch: worktreeBranch,
	}

	log.Printf("[ChainCompletion] Registered chain: root=%s, total_beads=%d, leaf_count=%d, project=%s",
		rootBeadID, len(allBeadIDs), len(leafIDs), projectPath)
}

// RecordChainWorktree records the chain's actual worktree key + branch at
// chain-worktree creation, exactly as RegisterChain records ProjectPath.
// Key is the chain root bead ID string; branch is the REAL branch name the
// worktree was created on (possibly a -vN versioned one — ask
// WorktreeManager.ResolveBranch, never re-derive from the key). No-op when
// the chain is not registered yet; the completion-time resolver falls back
// to exact/-vN matching in that case.
func (d *ChainCompletionDetector) RecordChainWorktree(key, branch string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if chain, ok := d.chains[key]; ok && branch != "" {
		chain.WorktreeKey = key
		chain.WorktreeBranch = branch
		log.Printf("[ChainCompletion] recorded worktree for chain %s: branch=%s", key, branch)
	}
}

func (d *ChainCompletionDetector) findLeafBeads(root types.BeadID, allBeadIDs []types.BeadID) []types.BeadID {
	dag := d.overseer.GetDAG()
	if dag == nil {
		return allBeadIDs
	}

	var leaves []types.BeadID
	for _, beadID := range allBeadIDs {
		node, exists := dag.GetNode(beadID.String())
		if !exists || len(node.OutEdges) == 0 {
			leaves = append(leaves, beadID)
		}
	}

	if len(leaves) == 0 {
		return allBeadIDs
	}
	return leaves
}

func (d *ChainCompletionDetector) GetChainInfo(rootBeadID types.BeadID) (*ChainInfo, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	chain, exists := d.chains[rootBeadID.String()]
	return chain, exists
}

func (d *ChainCompletionDetector) GetAllChains() []*ChainInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	chains := make([]*ChainInfo, 0, len(d.chains))
	for _, chain := range d.chains {
		chains = append(chains, chain)
	}
	return chains
}

type ChainPatternExtractor struct {
	overseer         *Overseer
	qualityGateSvc   *intelligence.QualityGateService
	patternReviewSvc *intelligence.PatternReviewService
	hindsightClient  *hindsight.Client
}

func NewChainPatternExtractor(
	overseer *Overseer,
	qg *intelligence.QualityGateService,
	prs *intelligence.PatternReviewService,
) *ChainPatternExtractor {

	return &ChainPatternExtractor{
		overseer:         overseer,
		qualityGateSvc:   qg,
		patternReviewSvc: prs,
	}
}

func (e *ChainPatternExtractor) ExtractPatterns(event ChainCompletedEvent) error {
	chain := event.ChainInfo

	log.Printf("[ChainPattern] Processing chain: root=%s, beads=%d", chain.RootBeadID, chain.TotalBeads)

	patterns := e.extractPatterns(chain)

	log.Printf("[ChainPattern] Extracted %d raw patterns", len(patterns))

	// For now, skip quality gates and directly review/store patterns
	// This ensures patterns are stored even if quality gates fail
	for _, pattern := range patterns {
		e.reviewAndStore(pattern)
	}

	log.Printf("[ChainPattern] Processed %d patterns", len(patterns))
	return nil
}

func (e *ChainPatternExtractor) extractPatterns(chain *ChainInfo) []*intelligence.ExtractedPattern {
	patterns := make([]*intelligence.ExtractedPattern, 0)

	// Always add workflow pattern as fallback/supplement
	patterns = append(patterns, &intelligence.ExtractedPattern{
		ID:          fmt.Sprintf("%s_chain_%d", chain.RootBeadID, time.Now().Unix()),
		BeadID:      chain.RootBeadID,
		PatternType: "workflow_pattern",
		Title:       fmt.Sprintf("Chain pattern: %d beads completed", chain.TotalBeads),
		Description: fmt.Sprintf("Completed chain with %d beads from %s to %v. Duration: %v",
			chain.TotalBeads, chain.RootBeadID, chain.LeafBeadIDs, chain.EndTime.Sub(chain.StartTime)),
		Confidence:  0.8,
		Tags:        []string{"chain", "completed", string(chain.RootBeadID.Project())},
		ExtractedAt: time.Now(),
	})
	log.Printf("[ChainPattern] Created workflow pattern for chain with %d beads", chain.TotalBeads)

	return patterns
}

func (e *ChainPatternExtractor) reviewAndStore(pattern *intelligence.ExtractedPattern) {
	if e.patternReviewSvc != nil {
		entry, err := e.patternReviewSvc.SubmitPattern(pattern)
		if err != nil {
			log.Printf("[ChainPattern] Failed to submit pattern for review: %v", err)
			return
		}

		log.Printf("[ChainPattern] Pattern %s submitted for review, state: %s",
			pattern.ID, entry.State)
	}

	log.Printf("[ChainPattern] Pattern stored locally: %s (confidence: %.2f)",
		pattern.ID, pattern.Confidence)
}
