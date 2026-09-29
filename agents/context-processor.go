// Package agents provides multi-agent consensus facilitation for complex beads.
// The context processor queries Hindsight for relevant context before bead processing,
// with caching and graceful error handling to ensure low latency enrichment.
package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/degrade"
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/logger"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// QueryResult represents the result of a Hindsight query with metadata
type QueryResult struct {
	Memories  []hindsight.Memory
	FromCache bool
	Latency   time.Duration
	Error     error
}

// CacheEntry represents a cached query result with expiration
type CacheEntry struct {
	Result     *QueryResult
	Expiration time.Time
}

// QueryCache provides thread-safe caching for Hindsight queries
type QueryCache struct {
	entries map[string]*CacheEntry
	mutex   sync.RWMutex
	ttl     time.Duration
	stats   *cacheStats
}

// cacheStats tracks cache statistics
type cacheStats struct {
	hitCount      int64
	missCount     int64
	evictionCount int64
	totalQueries  int64
	totalLatency  time.Duration
	mutex         sync.RWMutex
}

// ContextProcessor analyzes beads and queries Hindsight for relevant context.
// It provides context enrichment with caching and graceful error handling.
type ContextProcessor struct {
	hindsightClient *hindsight.Client
	cache           *QueryCache
	logger          logger.Logger
	enabled         bool
	maxQueryLatency time.Duration
	processorID     string
}

// ContextProcessorConfig contains configuration for the context processor
type ContextProcessorConfig struct {
	Enabled         bool          `json:"enabled" yaml:"enabled"`
	CacheTTL        time.Duration `json:"cache_ttl" yaml:"cache_ttl"`
	MaxQueryLatency time.Duration `json:"max_query_latency" yaml:"max_query_latency"`
}

// DefaultContextProcessorConfig returns the default configuration
func DefaultContextProcessorConfig() ContextProcessorConfig {
	return ContextProcessorConfig{
		Enabled:         true,
		CacheTTL:        5 * time.Minute,
		// Measured 2026-09-28: server-side recall p50 is ~4.6s (with
		// HINDSIGHT_API_RERANKER_LOCAL_BUCKET_BATCHING=true). 500ms cancelled
		// EVERY daemon-side recall, silently emptying pre-injection fleet-wide.
		// 15s gives headroom; recall is fail-open, worst case a slow backend
		// delays prompt build slightly.
		MaxQueryLatency: 15 * time.Second,
	}
}

// NewQueryCache creates a new query cache with the specified TTL
func NewQueryCache(ttl time.Duration) *QueryCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &QueryCache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
		stats:   &cacheStats{},
	}
}

// Get retrieves a cached result if available and not expired
func (c *QueryCache) Get(key string) (*QueryResult, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		c.stats.mutex.Lock()
		c.stats.missCount++
		c.stats.mutex.Unlock()
		return nil, false
	}

	// Check if expired
	if time.Now().After(entry.Expiration) {
		c.stats.mutex.Lock()
		c.stats.missCount++
		c.stats.evictionCount++
		c.stats.mutex.Unlock()
		return nil, false
	}

	c.stats.mutex.Lock()
	c.stats.hitCount++
	c.stats.mutex.Unlock()

	return entry.Result, true
}

// Set stores a result in the cache with expiration
func (c *QueryCache) Set(key string, result *QueryResult) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.entries[key] = &CacheEntry{
		Result:     result,
		Expiration: time.Now().Add(c.ttl),
	}
}

// Clear removes all entries from the cache
func (c *QueryCache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.stats.mutex.Lock()
	c.stats.evictionCount += int64(len(c.entries))
	c.stats.mutex.Unlock()

	c.entries = make(map[string]*CacheEntry)
}

// Size returns the current number of cache entries
func (c *QueryCache) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.entries)
}

// NewContextProcessor creates a new context processor with Hindsight integration.
// The processor provides context enrichment with caching and graceful error handling.
func NewContextProcessor(hindsightClient *hindsight.Client, config ContextProcessorConfig, log logger.Logger) *ContextProcessor {
	if log == nil {
		log = logger.NewSimpleLogger("context-processor")
	}

	return &ContextProcessor{
		hindsightClient: hindsightClient,
		cache:           NewQueryCache(config.CacheTTL),
		logger:          log,
		enabled:         config.Enabled,
		maxQueryLatency: config.MaxQueryLatency,
		processorID:     fmt.Sprintf("context-processor-%d", time.Now().UnixNano()),
	}
}

// QueryHindsight queries Hindsight for relevant context about a bead.
// Returns cached results if available and within latency requirements.
// On failure, logs a warning and returns empty result without error.
func (p *ContextProcessor) QueryHindsight(ctx context.Context, bead types.Bead) (*QueryResult, error) {
	if !p.enabled {
		return &QueryResult{Memories: []hindsight.Memory{}}, nil
	}

	// Generate query from bead
	query := GenerateQueryFromBead(bead)

	// Check cache first
	cacheKey := fmt.Sprintf("%s:%s", bead.ID, query)
	if cached, found := p.cache.Get(cacheKey); found {
		p.logger.Debug("Cache hit for bead context",
			"bead_id", string(bead.ID),
			"memory_count", len(cached.Memories),
		)
		return cached, nil
	}

	// Query Hindsight with timeout
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, p.maxQueryLatency)
	defer cancel()

	// FR-032: count the fail-open recall attempt so a subsystem at 0% success
	// (e.g. the 500ms-timeout bug, hydra-bye7) surfaces in the Pan digest.
	degrade.RecordAttempt("hindsight-recall")
	resp, err := p.hindsightClient.Search(query, string(bead.Type), 5)
	latency := time.Since(start)

	if err != nil {
		p.logger.Warn("Hindsight query failed, returning empty context",
			"bead_id", string(bead.ID),
			"error", err,
		)
		return &QueryResult{
			Memories:  []hindsight.Memory{},
			FromCache: false,
			Latency:   latency,
			Error:     err,
		}, nil
	}

	result := &QueryResult{
		Memories:  resp.Memories,
		FromCache: false,
		Latency:   latency,
	}

	// Cache the result
	p.cache.Set(cacheKey, result)
	degrade.RecordSuccess("hindsight-recall")

	p.logger.Debug("Hindsight query completed",
		"bead_id", string(bead.ID),
		"memory_count", len(resp.Memories),
		"latency", latency,
	)

	return result, nil
}

// QueryHindsightByID queries Hindsight using a specific bead ID.
// Useful for querying context for beads that may not be fully loaded.
func (p *ContextProcessor) QueryHindsightByID(ctx context.Context, beadID types.BeadID) (*QueryResult, error) {
	if !p.enabled {
		return &QueryResult{Memories: []hindsight.Memory{}}, nil
	}

	// Check cache first
	cacheKey := fmt.Sprintf("id:%s", beadID)
	if cached, found := p.cache.Get(cacheKey); found {
		return cached, nil
	}

	// Query by bead ID
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, p.maxQueryLatency)
	defer cancel()

	degrade.RecordAttempt("hindsight-recall")
	resp, err := p.hindsightClient.SearchByBead(string(beadID), "")
	latency := time.Since(start)

	if err != nil {
		p.logger.Warn("Hindsight query by ID failed",
			"bead_id", string(beadID),
			"error", err,
		)
		return &QueryResult{
			Memories:  []hindsight.Memory{},
			FromCache: false,
			Latency:   latency,
			Error:     err,
		}, nil
	}

	result := &QueryResult{
		Memories:  resp.Memories,
		FromCache: false,
		Latency:   latency,
	}

	p.cache.Set(cacheKey, result)
	degrade.RecordSuccess("hindsight-recall")

	return result, nil
}

// QueryHindsightWithQuery queries Hindsight using a custom query string.
// Allows for more specific context queries beyond bead metadata.
func (p *ContextProcessor) QueryHindsightWithQuery(ctx context.Context, query string, limit int) (*QueryResult, error) {
	if !p.enabled {
		return &QueryResult{Memories: []hindsight.Memory{}}, nil
	}

	if limit <= 0 {
		limit = 5
	}

	// Check cache first
	cacheKey := fmt.Sprintf("query:%s:%d", query, limit)
	if cached, found := p.cache.Get(cacheKey); found {
		return cached, nil
	}

	// Execute query
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, p.maxQueryLatency)
	defer cancel()

	degrade.RecordAttempt("hindsight-recall")
	resp, err := p.hindsightClient.Search(query, "", limit)
	latency := time.Since(start)

	if err != nil {
		p.logger.Warn("Hindsight custom query failed",
			"query", query,
			"error", err,
		)
		return &QueryResult{
			Memories:  []hindsight.Memory{},
			FromCache: false,
			Latency:   latency,
			Error:     err,
		}, nil
	}

	result := &QueryResult{
		Memories:  resp.Memories,
		FromCache: false,
		Latency:   latency,
	}

	p.cache.Set(cacheKey, result)
	degrade.RecordSuccess("hindsight-recall")

	return result, nil
}

// GetCachedResult retrieves a result from cache without querying Hindsight.
// Returns nil if not found or expired.
func (p *ContextProcessor) GetCachedResult(key string) (*QueryResult, bool) {
	return p.cache.Get(key)
}

// InvalidateCache removes all cached entries.
// Useful for testing or when cache needs to be refreshed.
func (p *ContextProcessor) InvalidateCache() {
	p.cache.Clear()
	p.logger.Info("Context processor cache invalidated")
}

// IsEnabled returns whether the context processor is enabled
func (p *ContextProcessor) IsEnabled() bool {
	return p.enabled
}

// GetCacheStats returns statistics about the cache
func (p *ContextProcessor) GetCacheStats() CacheStats {
	p.cache.stats.mutex.RLock()
	defer p.cache.stats.mutex.RUnlock()

	avgLatency := time.Duration(0)
	if p.cache.stats.totalQueries > 0 {
		avgLatency = p.cache.stats.totalLatency / time.Duration(p.cache.stats.totalQueries)
	}

	return CacheStats{
		Size:           p.cache.Size(),
		HitCount:       p.cache.stats.hitCount,
		MissCount:      p.cache.stats.missCount,
		EvictionCount:  p.cache.stats.evictionCount,
		TotalQueries:   p.cache.stats.totalQueries,
		AverageLatency: avgLatency,
	}
}

// CacheStats contains statistics about the query cache
type CacheStats struct {
	Size           int           `json:"size"`
	HitCount       int64         `json:"hit_count"`
	MissCount      int64         `json:"miss_count"`
	EvictionCount  int64         `json:"eviction_count"`
	TotalQueries   int64         `json:"total_queries"`
	AverageLatency time.Duration `json:"average_latency"`
}

// ContextEnrichment represents enriched context for a bead
type ContextEnrichment struct {
	BeadID     types.BeadID       `json:"bead_id"`
	Query      string             `json:"query"`
	Memories   []hindsight.Memory `json:"memories"`
	FromCache  bool               `json:"from_cache"`
	QueryTime  time.Duration      `json:"query_time"`
	EnrichedAt time.Time          `json:"enriched_at"`
}

// EnrichBeadContext queries Hindsight and returns enriched context for a bead.
// This is the high-level API for bead context enrichment.
func (p *ContextProcessor) EnrichBeadContext(ctx context.Context, bead types.Bead) (*ContextEnrichment, error) {
	start := time.Now()

	result, err := p.QueryHindsight(ctx, bead)
	if err != nil {
		return nil, err
	}

	return &ContextEnrichment{
		BeadID:     bead.ID,
		Query:      GenerateQueryFromBead(bead),
		Memories:   result.Memories,
		FromCache:  result.FromCache,
		QueryTime:  time.Since(start),
		EnrichedAt: time.Now(),
	}, nil
}

// GenerateQueryFromBead creates a search query from bead metadata
func GenerateQueryFromBead(bead types.Bead) string {
	var parts []string

	// Add title
	if bead.Title != "" {
		parts = append(parts, bead.Title)
	}

	// Add type
	if bead.Type != "" {
		parts = append(parts, bead.Type)
	}

	// Add description keywords (first 50 chars)
	if bead.Description != "" {
		desc := bead.Description
		if len(desc) > 50 {
			desc = desc[:50]
		}
		parts = append(parts, desc)
	}

	return strings.Join(parts, " ")
}

// filterRelevantMemories filters memories by minimum relevance score
func filterRelevantMemories(memories []hindsight.Memory, minScore float64) []hindsight.Memory {
	if minScore <= 0 {
		return memories
	}

	var filtered []hindsight.Memory
	for _, mem := range memories {
		if mem.Relevance >= minScore || mem.Score >= minScore {
			filtered = append(filtered, mem)
		}
	}

	return filtered
}

// mergeMemories merges multiple memory lists, removing duplicates by ID
func mergeMemories(memories ...[]hindsight.Memory) []hindsight.Memory {
	seen := make(map[string]bool)
	var merged []hindsight.Memory

	for _, list := range memories {
		for _, mem := range list {
			if !seen[mem.ID] {
				seen[mem.ID] = true
				merged = append(merged, mem)
			}
		}
	}

	return merged
}
