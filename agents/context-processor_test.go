package agents

import (
	"context"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/logger"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestDefaultContextProcessorConfig(t *testing.T) {
	config := DefaultContextProcessorConfig()

	if !config.Enabled {
		t.Error("Default Enabled should be true")
	}

	if config.CacheTTL != 5*time.Minute {
		t.Errorf("Default CacheTTL = %v, want 5m", config.CacheTTL)
	}

	if config.MaxQueryLatency != 15*time.Second {
		t.Errorf("Default MaxQueryLatency = %v, want 15s", config.MaxQueryLatency)
	}
}

func TestNewQueryCache(t *testing.T) {
	cache := NewQueryCache(5 * time.Minute)
	if cache == nil {
		t.Fatal("NewQueryCache returned nil")
	}

	if cache.ttl != 5*time.Minute {
		t.Errorf("Cache TTL = %v, want 5m", cache.ttl)
	}

	if len(cache.entries) != 0 {
		t.Error("New cache should be empty")
	}
}

func TestNewQueryCache_DefaultTTL(t *testing.T) {
	cache := NewQueryCache(0)
	if cache.ttl != 5*time.Minute {
		t.Errorf("Cache should default to 5m TTL, got %v", cache.ttl)
	}
}

func TestQueryCache_SetAndGet(t *testing.T) {
	cache := NewQueryCache(5 * time.Minute)

	result := &QueryResult{
		Memories: []hindsight.Memory{
			{ID: "mem-1", Title: "Test Memory"},
		},
	}

	cache.Set("test-key", result)

	retrieved, found := cache.Get("test-key")
	if !found {
		t.Fatal("Expected to find cached result")
	}

	if len(retrieved.Memories) != 1 {
		t.Errorf("Expected 1 memory, got %d", len(retrieved.Memories))
	}
}

func TestQueryCache_Get_NotFound(t *testing.T) {
	cache := NewQueryCache(5 * time.Minute)

	_, found := cache.Get("nonexistent-key")
	if found {
		t.Error("Should not find nonexistent key")
	}
}

func TestQueryCache_Get_Expired(t *testing.T) {
	cache := NewQueryCache(1 * time.Millisecond)

	result := &QueryResult{
		Memories: []hindsight.Memory{
			{ID: "mem-1", Title: "Test Memory"},
		},
	}

	cache.Set("test-key", result)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	_, found := cache.Get("test-key")
	if found {
		t.Error("Should not find expired entry")
	}
}

func TestQueryCache_Clear(t *testing.T) {
	cache := NewQueryCache(5 * time.Minute)

	cache.Set("key1", &QueryResult{})
	cache.Set("key2", &QueryResult{})

	if cache.Size() != 2 {
		t.Errorf("Expected 2 entries, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", cache.Size())
	}
}

func TestQueryCache_Size(t *testing.T) {
	cache := NewQueryCache(5 * time.Minute)

	if cache.Size() != 0 {
		t.Errorf("Expected size 0, got %d", cache.Size())
	}

	cache.Set("key1", &QueryResult{})
	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}
}

func TestNewContextProcessor(t *testing.T) {
	hindsightClient := hindsight.NewClient()
	config := DefaultContextProcessorConfig()
	log := logger.NewSimpleLogger("test")

	processor := NewContextProcessor(hindsightClient, config, log)

	if processor == nil {
		t.Fatal("NewContextProcessor returned nil")
	}

	if processor.hindsightClient != hindsightClient {
		t.Error("Hindsight client not set correctly")
	}

	if processor.cache == nil {
		t.Error("Cache not initialized")
	}

	if processor.enabled != config.Enabled {
		t.Error("Enabled not set correctly")
	}
}

func TestNewContextProcessor_NilLogger(t *testing.T) {
	hindsightClient := hindsight.NewClient()
	config := DefaultContextProcessorConfig()

	processor := NewContextProcessor(hindsightClient, config, nil)

	if processor == nil {
		t.Fatal("NewContextProcessor returned nil")
	}

	if processor.logger == nil {
		t.Error("Should have default logger when nil provided")
	}
}

func TestContextProcessor_IsEnabled(t *testing.T) {
	// Test enabled
	hindsightClient := hindsight.NewClient()
	config := ContextProcessorConfig{Enabled: true}
	processor := NewContextProcessor(hindsightClient, config, nil)

	if !processor.IsEnabled() {
		t.Error("IsEnabled should return true when enabled")
	}

	// Test disabled
	config2 := ContextProcessorConfig{Enabled: false}
	processor2 := NewContextProcessor(hindsightClient, config2, nil)

	if processor2.IsEnabled() {
		t.Error("IsEnabled should return false when disabled")
	}
}

func TestContextProcessor_QueryHindsight_Disabled(t *testing.T) {
	hindsightClient := hindsight.NewClient()
	config := ContextProcessorConfig{Enabled: false}
	processor := NewContextProcessor(hindsightClient, config, nil)
	ctx := context.Background()

	bead := types.Bead{
		ID:    "test-bead",
		Title: "Test Bead",
	}

	result, err := processor.QueryHindsight(ctx, bead)
	if err != nil {
		t.Errorf("QueryHindsight when disabled should not error: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	if len(result.Memories) != 0 {
		t.Error("Should return empty memories when disabled")
	}
}

func TestContextProcessor_InvalidateCache(t *testing.T) {
	hindsightClient := hindsight.NewClient()
	config := DefaultContextProcessorConfig()
	processor := NewContextProcessor(hindsightClient, config, nil)

	processor.cache.Set("key1", &QueryResult{})
	processor.cache.Set("key2", &QueryResult{})

	if processor.cache.Size() != 2 {
		t.Errorf("Expected 2 entries, got %d", processor.cache.Size())
	}

	processor.InvalidateCache()

	if processor.cache.Size() != 0 {
		t.Errorf("Expected 0 entries after invalidate, got %d", processor.cache.Size())
	}
}

func TestContextProcessor_GetCachedResult(t *testing.T) {
	hindsightClient := hindsight.NewClient()
	config := DefaultContextProcessorConfig()
	processor := NewContextProcessor(hindsightClient, config, nil)

	result := &QueryResult{
		Memories: []hindsight.Memory{
			{ID: "mem-1", Title: "Test"},
		},
	}

	processor.cache.Set("test-key", result)

	cached, found := processor.GetCachedResult("test-key")
	if !found {
		t.Fatal("Should find cached result")
	}

	if len(cached.Memories) != 1 {
		t.Error("Cached result should have 1 memory")
	}
}

func TestContextProcessor_GetCacheStats(t *testing.T) {
	hindsightClient := hindsight.NewClient()
	config := DefaultContextProcessorConfig()
	processor := NewContextProcessor(hindsightClient, config, nil)

	stats := processor.GetCacheStats()

	if stats.Size != 0 {
		t.Errorf("Expected size 0, got %d", stats.Size)
	}
}

func TestGenerateQueryFromBead(t *testing.T) {
	bead := types.Bead{
		ID:          "test-bead",
		Title:       "Test Title",
		Type:        "implement",
		Description: "This is a test description",
	}

	query := GenerateQueryFromBead(bead)

	if query == "" {
		t.Error("Query should not be empty")
	}

	if !contains(query, "Test Title") {
		t.Error("Query should contain title")
	}

	if !contains(query, "implement") {
		t.Error("Query should contain type")
	}
}

func TestGenerateQueryFromBead_Empty(t *testing.T) {
	bead := types.Bead{
		ID: "test-bead",
	}

	query := GenerateQueryFromBead(bead)

	if query != "" {
		t.Errorf("Query should be empty for empty bead, got: %s", query)
	}
}

func TestFilterRelevantMemories(t *testing.T) {
	memories := []hindsight.Memory{
		{ID: "mem-1", Relevance: 0.9},
		{ID: "mem-2", Relevance: 0.5},
		{ID: "mem-3", Relevance: 0.3},
	}

	filtered := filterRelevantMemories(memories, 0.6)

	if len(filtered) != 1 {
		t.Errorf("Expected 1 memory with relevance >= 0.6, got %d", len(filtered))
	}

	if filtered[0].ID != "mem-1" {
		t.Error("Should keep only mem-1")
	}
}

func TestFilterRelevantMemories_ZeroScore(t *testing.T) {
	memories := []hindsight.Memory{
		{ID: "mem-1", Relevance: 0.9},
		{ID: "mem-2", Relevance: 0.5},
	}

	filtered := filterRelevantMemories(memories, 0)

	if len(filtered) != 2 {
		t.Errorf("Should return all memories when minScore is 0, got %d", len(filtered))
	}
}

func TestMergeMemories(t *testing.T) {
	list1 := []hindsight.Memory{
		{ID: "mem-1", Title: "Memory 1"},
		{ID: "mem-2", Title: "Memory 2"},
	}

	list2 := []hindsight.Memory{
		{ID: "mem-2", Title: "Memory 2 Duplicate"},
		{ID: "mem-3", Title: "Memory 3"},
	}

	merged := mergeMemories(list1, list2)

	if len(merged) != 3 {
		t.Errorf("Expected 3 unique memories, got %d", len(merged))
	}

	// Check that mem-2 appears only once
	count := 0
	for _, mem := range merged {
		if mem.ID == "mem-2" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("mem-2 should appear exactly once, appeared %d times", count)
	}
}

func TestMergeMemories_Empty(t *testing.T) {
	merged := mergeMemories()

	if len(merged) != 0 {
		t.Errorf("Expected empty result for no input, got %d", len(merged))
	}
}

func TestContextEnrichment(t *testing.T) {
	enrichment := ContextEnrichment{
		BeadID:     "test-bead",
		Query:      "test query",
		Memories:   []hindsight.Memory{{ID: "mem-1"}},
		FromCache:  false,
		QueryTime:  100 * time.Millisecond,
		EnrichedAt: time.Now(),
	}

	if enrichment.BeadID != "test-bead" {
		t.Error("BeadID not set correctly")
	}

	if enrichment.Query != "test query" {
		t.Error("Query not set correctly")
	}

	if len(enrichment.Memories) != 1 {
		t.Error("Memories not set correctly")
	}
}

func TestCacheStats(t *testing.T) {
	stats := CacheStats{
		Size:           10,
		HitCount:       100,
		MissCount:      20,
		EvictionCount:  5,
		TotalQueries:   120,
		AverageLatency: 50 * time.Millisecond,
	}

	if stats.Size != 10 {
		t.Error("Size not set correctly")
	}

	if stats.HitCount != 100 {
		t.Error("HitCount not set correctly")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
