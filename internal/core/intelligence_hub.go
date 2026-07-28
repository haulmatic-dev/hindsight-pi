package core

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/services"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// IntelligenceHub encapsulates all external intelligence service clients.
// It manages the GPTCache and Hindsight clients plus the graphify code-graph
// indexer. llm-tldr was removed: unmaintained, zero semantic units on Go, and
// its service was not running in production.
type IntelligenceHub struct {
	mu                sync.RWMutex
	gptCache          *GPTCacheClient
	hindsightClient   HindsightClient
	batchRetentionSvc *services.BatchRetentionService
	graphifyIndexer   *GraphifyIndexer
	config            IntelligenceConfig
}

// NewIntelligenceHub creates a hub from the given config.
func NewIntelligenceHub(config IntelligenceConfig) *IntelligenceHub {
	return &IntelligenceHub{config: config}
}

// Initialize sets up all enabled intelligence clients.
func (h *IntelligenceHub) Initialize() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Graphify replaces llm-tldr as the structural-intelligence layer. Agents
	// reach it through the `graphify` skill (frontmatter-only prompt cost); the
	// hub's only job is keeping each project's graph fresh with `graphify
	// update`, which is AST-only and needs no model call.
	if h.config.Graphify.Enabled {
		h.graphifyIndexer = NewGraphifyIndexer(h.config.Graphify)
		if h.graphifyIndexer.Available() {
			log.Printf("[IntelligenceHub] Graphify indexer initialized (refresh: %s, timeout: %s)",
				h.config.Graphify.RefreshInterval, h.config.Graphify.Timeout)
		} else {
			// Not fatal: the skill still works against whatever graph is on
			// disk, and agents fall back to reading files.
			log.Printf("[IntelligenceHub] ⚠️  Graphify enabled but binary %q not on PATH — graphs will not be refreshed (install: pip install graphifyy)",
				h.config.Graphify.Binary)
		}
	} else {
		// Warn rather than stay quiet. LoadConfig assigns
		// `config.Intelligence = raw.Intelligence` wholesale, so a config file
		// with an `intelligence:` block but no `graphify:` key silently discards
		// the DefaultConfig value and lands here with the structural layer off.
		// That exact shape — present in code, inert in the process — has now
		// bitten this config four times (min_score, a self-referential
		// fallback_provider, mcp.enabled with no server, agents.pi.mode), and
		// every time it was found by accident rather than by a signal.
		log.Printf("[IntelligenceHub] ⚠️  Graphify DISABLED — no code graph will be built or refreshed. " +
			"If this was not deliberate, add an `intelligence.graphify.enabled: true` block: a config " +
			"with an `intelligence:` section but no `graphify:` key does NOT inherit the default.")
	}

	if h.config.GPTCache.Enabled {
		h.gptCache = NewGPTCacheClient(
			h.config.GPTCache.ServerURL,
			5*time.Second,
		)
		log.Printf("[IntelligenceHub] GPTCache client initialized (server: %s)", h.config.GPTCache.ServerURL)
	}

	if h.config.Hindsight.Enabled {
		h.hindsightClient = hindsight.NewClient(
			hindsight.WithBaseURL(h.config.Hindsight.BaseURL),
			hindsight.WithTimeout(h.config.Hindsight.Timeout),
			hindsight.WithDefaultLimit(h.config.Hindsight.Limit),
			hindsight.WithMaxTokens(h.config.Hindsight.MaxTokens),
			hindsight.WithEnabled(true),
		)
		log.Printf("[IntelligenceHub] Hindsight client initialized (baseURL: %s)", h.config.Hindsight.BaseURL)

		os.Setenv("HYDRA_HISTORIESIGHT_ENABLED", "true")
		os.Setenv("HYDRA_HISTORIESIGHT_BASE_URL", h.config.Hindsight.BaseURL)
		os.Setenv("HYDRA_HISTORIESIGHT_TIMEOUT", h.config.Hindsight.Timeout.String())
		os.Setenv("HYDRA_HISTORIESIGHT_LIMIT", fmt.Sprintf("%d", h.config.Hindsight.Limit))
		os.Setenv("HYDRA_HISTORIESIGHT_MAX_TOKENS", fmt.Sprintf("%d", h.config.Hindsight.MaxTokens))

		h.batchRetentionSvc = services.NewBatchRetentionService(h.hindsightClient)
		log.Printf("[IntelligenceHub] Batch retention service initialized")
	}
}

// Enabled reports whether any intelligence service is enabled.
func (h *IntelligenceHub) Enabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config.Graphify.Enabled || h.config.GPTCache.Enabled || h.config.Hindsight.Enabled
}

// Hindsight returns the Hindsight client (may be nil).
func (h *IntelligenceHub) Hindsight() HindsightClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.hindsightClient
}

// GPTCache returns the GPTCache client (may be nil).
func (h *IntelligenceHub) GPTCache() *GPTCacheClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.gptCache
}

// BatchRetention returns the batch retention service (may be nil).
func (h *IntelligenceHub) BatchRetention() *services.BatchRetentionService {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.batchRetentionSvc
}

// SubmitExecutionResult submits an execution result to batch retention.
func (h *IntelligenceHub) SubmitExecutionResult(bead types.Bead, execResult services.ExecutionResult, workerID string) {
	if h.batchRetentionSvc == nil {
		return
	}
	if err := h.batchRetentionSvc.SubmitExecutionResult(bead, execResult, workerID); err != nil {
		log.Printf("[IntelligenceHub] Failed to submit execution result: %v", err)
	} else {
		log.Printf("[IntelligenceHub] Submitted execution result for bead %s", bead.ID)
	}
}

// GraphifyIndexer returns the graphify indexer (may be nil).
func (h *IntelligenceHub) GraphifyIndexer() *GraphifyIndexer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.graphifyIndexer
}

// QueueInitialIndexing refreshes every project's code graph.
//
// Named for the legacy indexing it replaced; the work is now a graphify graph
// refresh per project.
func (h *IntelligenceHub) QueueInitialIndexing(projects []types.Project) {
	h.refreshGraphs(projects)
}

// refreshGraphs asks the graphify indexer to rebuild each project's graph if it
// is due. Per project, never per bead: beads run in throwaway worktrees, so a
// per-bead build would repeat the whole extraction hundreds of times per run.
func (h *IntelligenceHub) refreshGraphs(projects []types.Project) {
	indexer := h.GraphifyIndexer()
	if indexer == nil {
		return
	}
	for _, project := range projects {
		if project.Type == types.RepoTypeMulti {
			for _, repo := range project.Repos {
				if indexer.Refresh(repo.Path) {
					log.Printf("[Graphify] refreshing graph: %s/%s", project.Name, repo.Name)
				}
			}
			continue
		}
		if indexer.Refresh(project.Path) {
			log.Printf("[Graphify] refreshing graph: %s", project.Name)
		}
	}
}

// QueueRepoIndexing queues a single repository for indexing.
func (h *IntelligenceHub) QueueRepoIndexing(projectName, repoPath string) {
	if indexer := h.GraphifyIndexer(); indexer != nil && indexer.Refresh(repoPath) {
		log.Printf("[Graphify] refreshing graph: %s/%s", projectName, repoPath)
	}
}
