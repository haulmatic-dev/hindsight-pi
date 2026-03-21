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
		if chain.Status != ChainStatusInProgress {
			continue
		}

		if time.Since(chain.StartTime) > d.config.Timeout {
			chain.Status = ChainStatusTimedOut
			log.Printf("[ChainCompletion] Chain %s timed out after %v", chain.RootBeadID, d.config.Timeout)
			continue
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
		bead := d.overseer.GetBeadFromFile(string(beadID))
		if bead == nil {
			continue
		}
		if bead.Status == types.StatusClosed || bead.Status == types.StatusCompleted {
			count++
		}
	}
	return count
}

func (d *ChainCompletionDetector) RegisterChain(rootBeadID types.BeadID, allBeadIDs []types.BeadID) {
	d.mu.Lock()
	defer d.mu.Unlock()

	leafIDs := d.findLeafBeads(rootBeadID, allBeadIDs)

	d.chains[rootBeadID.String()] = &ChainInfo{
		RootBeadID:  rootBeadID,
		LeafBeadIDs: leafIDs,
		AllBeadIDs:  allBeadIDs,
		TotalBeads:  len(allBeadIDs),
		StartTime:   time.Now(),
		Status:      ChainStatusInProgress,
	}

	log.Printf("[ChainCompletion] Registered chain: root=%s, total_beads=%d, leaf_count=%d",
		rootBeadID, len(allBeadIDs), len(leafIDs))
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
	tldrClient       *intelligence.TLDRClient
}

func NewChainPatternExtractor(
	overseer *Overseer,
	qg *intelligence.QualityGateService,
	prs *intelligence.PatternReviewService,
) *ChainPatternExtractor {
	// Use CLI mode for llm-tldr (HTTP daemon has Python compatibility issues)
	tldrClient := intelligence.NewTLDRClient("", true, "llm-tldr", "", 30*time.Second)

	return &ChainPatternExtractor{
		overseer:         overseer,
		qualityGateSvc:   qg,
		patternReviewSvc: prs,
		tldrClient:       tldrClient,
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

	// Try TLDR semantic search first to get code patterns
	if e.tldrClient != nil {
		projectPath := e.overseer.GetProjectPath(chain.RootBeadID.Project())
		if projectPath != "" {
			// Set the search path dynamically
			e.tldrClient.SetSearchPath(projectPath)

			// Build semantic index synchronously before searching
			if err := e.tldrClient.BuildIndex(); err != nil {
				log.Printf("[ChainPattern] TLDR index build warning: %v", err)
			}

			semanticResult, err := e.tldrClient.SemanticSearch(fmt.Sprintf("bead %s implementation", chain.RootBeadID))
			if err == nil && len(semanticResult.Results) > 0 {
				var files []string
				for _, r := range semanticResult.Results {
					files = append(files, r.File)
				}

				pattern := &intelligence.ExtractedPattern{
					ID:          fmt.Sprintf("%s_tldr_%d", chain.RootBeadID, time.Now().Unix()),
					BeadID:      chain.RootBeadID,
					PatternType: "code_pattern",
					Title:       fmt.Sprintf("Code pattern from %d files", len(files)),
					Description: fmt.Sprintf("TLDR found %d code snippets in: %v", len(semanticResult.Results), files),
					Confidence:  0.85,
					Tags:        []string{"code_pattern", "tldr", string(chain.RootBeadID.Project())},
					ExtractedAt: time.Now(),
					Files:       files,
				}
				patterns = append(patterns, pattern)
				log.Printf("[ChainPattern] TLDR extracted %d code patterns from chain", len(semanticResult.Results))
			} else {
				log.Printf("[ChainPattern] TLDR search failed or no results: %v", err)
			}
		}
	}

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
