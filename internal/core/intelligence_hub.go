package core

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/internal/services"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// IntelligenceHub encapsulates all external intelligence service clients.
// It manages TLDR, GPTCache, and Hindsight clients, plus the TLDR indexer.
type IntelligenceHub struct {
	mu                sync.RWMutex
	tldrClient        *intelligence.TLDRClient
	gptCache          *GPTCacheClient
	hindsightClient   HindsightClient
	batchRetentionSvc *services.BatchRetentionService
	tldrIndexer       *TLDRIndexer
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

	if h.config.TLDR.Enabled {
		h.tldrClient = intelligence.NewTLDRClient(
			h.config.TLDR.BaseURL,
			true, // useCLI
			"llm-tldr",
			"",
			h.config.TLDR.Timeout,
		)
		log.Printf("[IntelligenceHub] TLDR client initialized (baseURL: %s)", h.config.TLDR.BaseURL)

		if h.config.TLDR.Indexing.Enabled {
			h.tldrIndexer = NewTLDRIndexer(
				h.config.TLDR.Indexing.MaxConcurrent,
				h.config.TLDR.Indexing.RetryAttempts,
				h.config.TLDR.Indexing.RetryDelay,
				"llm-tldr",
			)
			log.Printf("[IntelligenceHub] TLDR indexer initialized")
		}
	}

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
	return h.config.TLDR.Enabled || h.config.GPTCache.Enabled || h.config.Hindsight.Enabled
}

// TLDR returns the TLDR client (may be nil).
func (h *IntelligenceHub) TLDR() *intelligence.TLDRClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.tldrClient
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

// Indexer returns the TLDR indexer (may be nil).
func (h *IntelligenceHub) Indexer() *TLDRIndexer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.tldrIndexer
}

// GraphifyIndexer returns the graphify indexer (may be nil).
func (h *IntelligenceHub) GraphifyIndexer() *GraphifyIndexer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.graphifyIndexer
}

// QueueInitialIndexing queues all project repositories for indexing.
func (h *IntelligenceHub) QueueInitialIndexing(projects []types.Project) {
	// Graph refresh runs independently of the legacy indexer, so a disabled or
	// removed TLDR path cannot stop graphs from being built.
	h.refreshGraphs(projects)

	if h.tldrIndexer == nil {
		return
	}
	log.Printf("[IntelligenceHub] Queueing initial indexing for %d projects", len(projects))
	for _, project := range projects {
		if project.Type == types.RepoTypeMulti {
			for _, repo := range project.Repos {
				if h.tldrIndexer.QueueIndex(repo.Path) {
					log.Printf("[IntelligenceHub] Queued: %s/%s", project.Name, repo.Name)
				}
			}
		} else {
			if h.tldrIndexer.QueueIndex(project.Path) {
				log.Printf("[IntelligenceHub] Queued: %s", project.Name)
			}
		}
	}
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

	if h.tldrIndexer == nil {
		return
	}
	if h.tldrIndexer.QueueIndex(repoPath) {
		log.Printf("[IntelligenceHub] Queued: %s/%s", projectName, repoPath)
	}
}
