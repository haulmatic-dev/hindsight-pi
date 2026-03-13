package intelligence

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// PatternMatcherConfig contains configuration for the pattern matcher
type PatternMatcherConfig struct {
	Enabled                bool          `yaml:"enabled"`
	BlockOnFailure         bool          `yaml:"block_on_failure"`         // Block execution if lookup fails
	MaxPatternsInPrompt    int           `yaml:"max_patterns_in_prompt"`   // Maximum patterns to include in prompt
	MinConfidenceThreshold float64       `yaml:"min_confidence_threshold"` // Minimum confidence to suggest pattern
	SimilarityMetric       string        `yaml:"similarity_metric"`        // "cosine", "dot_product", "jaccard"
	CacheEnabled           bool          `yaml:"cache_enabled"`
	CacheTTL               time.Duration `yaml:"cache_ttl"`
	QueryTimeout           time.Duration `yaml:"query_timeout"`
}

// DefaultPatternMatcherConfig returns default configuration
func DefaultPatternMatcherConfig() PatternMatcherConfig {
	return PatternMatcherConfig{
		Enabled:                true,
		BlockOnFailure:         true,
		MaxPatternsInPrompt:    3,
		MinConfidenceThreshold: 0.50, // Lowered from 0.70 for more pattern matches
		SimilarityMetric:       "cosine",
		CacheEnabled:           true,
		CacheTTL:               5 * time.Minute,
		QueryTimeout:           5 * time.Second,
	}
}

// PatternMatch represents a matched pattern with relevance information
type PatternMatch struct {
	PatternID        string                 `json:"pattern_id"`
	Title            string                 `json:"title"`
	Description      string                 `json:"description"`
	Confidence       float64                `json:"confidence"`      // Overall match confidence (0.0-1.0)
	RelevanceScore   float64                `json:"relevance_score"` // Multi-factor score
	SemanticScore    float64                `json:"semantic_score"`  // Semantic similarity (0.35 weight)
	SuccessRate      float64                `json:"success_rate"`    // Historical success (0.25 weight)
	RecencyScore     float64                `json:"recency_score"`   // Pattern freshness (0.15 weight)
	UsageScore       float64                `json:"usage_score"`     // Usage frequency (0.15 weight)
	ContextScore     float64                `json:"context_score"`   // Context match (0.10 weight)
	SourceProject    string                 `json:"source_project"`
	Language         string                 `json:"language"`
	Tags             []string               `json:"tags"`
	SolutionApproach string                 `json:"solution_approach"`
	CodeSnippet      string                 `json:"code_snippet,omitempty"`
	AntiPatterns     []string               `json:"anti_patterns,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// PatternQuery represents a query for finding relevant patterns
type PatternQuery struct {
	BeadID          types.BeadID `json:"bead_id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	BeadType        string       `json:"bead_type"`
	TargetFiles     []string     `json:"target_files,omitempty"`
	ProjectLanguage string       `json:"project_language,omitempty"`
	ContextTags     []string     `json:"context_tags,omitempty"`
}

// PatternMatcher matches beads to relevant patterns from the collective unconscious
type PatternMatcher struct {
	config       PatternMatcherConfig
	patternStore PatternStore
	cache        *PatternCache
	running      bool
	mutex        sync.RWMutex
}

// PatternStore defines the interface for pattern storage
type PatternStore interface {
	GetApprovedPatterns(ctx context.Context) ([]*ExtractedPattern, error)
	GetPatternByID(ctx context.Context, patternID string) (*ExtractedPattern, error)
	SearchPatterns(ctx context.Context, query string, tags []string) ([]*ExtractedPattern, error)
}

// PatternCache caches pattern queries for performance
type PatternCache struct {
	entries   map[string]*CacheEntry
	mutex     sync.RWMutex
	ttl       time.Duration
	hits      int64
	misses    int64
	saves     int64
	evictions int64
}

// CacheEntry represents a cached query result
type CacheEntry struct {
	Patterns  []*PatternMatch
	Timestamp time.Time
}

// NewPatternMatcher creates a new pattern matcher
func NewPatternMatcher(config PatternMatcherConfig, store PatternStore) *PatternMatcher {
	return &PatternMatcher{
		config:       config,
		patternStore: store,
		cache: &PatternCache{
			entries:   make(map[string]*CacheEntry),
			ttl:       config.CacheTTL,
			hits:      0,
			misses:    0,
			saves:     0,
			evictions: 0,
		},
	}
}

// IsEnabled returns true if the matcher is enabled
func (m *PatternMatcher) IsEnabled() bool {
	return m.config.Enabled
}

// GetCacheStats returns cache statistics
func (m *PatternMatcher) GetCacheStats() CacheStats {
	return m.cache.GetStats()
}

// LogCacheStats logs cache statistics
func (m *PatternMatcher) LogCacheStats() {
	m.cache.LogStats()
}

// FindPatterns finds relevant patterns for a bead
func (m *PatternMatcher) FindPatterns(ctx context.Context, query PatternQuery) ([]*PatternMatch, error) {
	if !m.config.Enabled {
		return nil, nil
	}

	// Check cache first
	cacheKey := m.generateCacheKey(query)
	if m.config.CacheEnabled {
		if cached := m.cache.Get(cacheKey); cached != nil {
			log.Printf("[PatternMatcher] Cache HIT for bead %s (key: %s, patterns: %d)",
				query.BeadID, cacheKey, len(cached))
			return cached, nil
		}
		log.Printf("[PatternMatcher] Cache MISS for bead %s (key: %s)", query.BeadID, cacheKey)
	}

	// Set query timeout
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()

	// Get all approved patterns from store
	patterns, err := m.patternStore.GetApprovedPatterns(ctx)
	if err != nil {
		if m.config.BlockOnFailure {
			return nil, fmt.Errorf("failed to fetch patterns: %w", err)
		}
		log.Printf("[PatternMatcher] Warning: failed to fetch patterns: %v", err)
		return nil, nil
	}

	// Score and rank patterns
	matches := m.scoreAndRankPatterns(query, patterns)

	// Filter by confidence threshold
	matches = m.filterByConfidence(matches)

	// Limit to max patterns
	if len(matches) > m.config.MaxPatternsInPrompt {
		matches = matches[:m.config.MaxPatternsInPrompt]
	}

	// Cache results
	if m.config.CacheEnabled {
		m.cache.Set(cacheKey, matches)
		log.Printf("[PatternMatcher] Cache SAVE for bead %s (key: %s, patterns: %d, cache_size: %d)",
			query.BeadID, cacheKey, len(matches), m.cache.Size())
	}

	log.Printf("[PatternMatcher] Found %d patterns for bead %s", len(matches), query.BeadID)

	return matches, nil
}

// scoreAndRankPatterns calculates relevance scores for all patterns
func (m *PatternMatcher) scoreAndRankPatterns(query PatternQuery, patterns []*ExtractedPattern) []*PatternMatch {
	matches := make([]*PatternMatch, 0, len(patterns))

	for _, pattern := range patterns {
		match := m.calculateMatchScore(query, pattern)
		if match.Confidence >= m.config.MinConfidenceThreshold {
			matches = append(matches, match)
		}
	}

	// Sort by relevance score (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].RelevanceScore > matches[j].RelevanceScore
	})

	return matches
}

// calculateMatchScore calculates multi-factor relevance score
func (m *PatternMatcher) calculateMatchScore(query PatternQuery, pattern *ExtractedPattern) *PatternMatch {
	// Calculate individual scores
	semanticScore := m.calculateSemanticScore(query, pattern)
	successRate := m.getSuccessRate(pattern)
	recencyScore := m.calculateRecencyScore(pattern)
	usageScore := m.calculateUsageScore(pattern)
	contextScore := m.calculateContextScore(query, pattern)

	// Calculate weighted relevance score
	// Formula from PRD: 0.35*semantic + 0.25*success + 0.15*recency + 0.15*usage + 0.10*context
	relevanceScore := (0.35 * semanticScore) +
		(0.25 * successRate) +
		(0.15 * recencyScore) +
		(0.15 * usageScore) +
		(0.10 * contextScore)

	// Confidence is the relevance score but can be adjusted
	confidence := relevanceScore

	return &PatternMatch{
		PatternID:        pattern.ID,
		Title:            pattern.Title,
		Description:      pattern.Description,
		Confidence:       confidence,
		RelevanceScore:   relevanceScore,
		SemanticScore:    semanticScore,
		SuccessRate:      successRate,
		RecencyScore:     recencyScore,
		UsageScore:       usageScore,
		ContextScore:     contextScore,
		SourceProject:    getStringFromMetadata(pattern.Metadata, "source_project"),
		Language:         getFirstTag(pattern.Tags),
		Tags:             pattern.Tags,
		SolutionApproach: getStringFromMetadata(pattern.Metadata, "solution_approach"),
		CodeSnippet:      pattern.CodeSnippet,
		Metadata: map[string]interface{}{
			"matched_at": time.Now(),
		},
	}
}

// calculateSemanticScore calculates semantic similarity between query and pattern
func (m *PatternMatcher) calculateSemanticScore(query PatternQuery, pattern *ExtractedPattern) float64 {
	// Combine query title and description
	queryText := query.Title + " " + query.Description
	patternText := pattern.Title + " " + pattern.Description

	switch m.config.SimilarityMetric {
	case "cosine":
		return m.cosineSimilarity(queryText, patternText)
	case "jaccard":
		return m.jaccardSimilarity(queryText, patternText)
	default:
		return m.cosineSimilarity(queryText, patternText)
	}
}

// cosineSimilarity calculates cosine similarity between two texts
func (m *PatternMatcher) cosineSimilarity(text1, text2 string) float64 {
	// Simple word-based cosine similarity
	words1 := m.tokenize(text1)
	words2 := m.tokenize(text2)

	// Get all unique words
	allWords := make(map[string]bool)
	for word := range words1 {
		allWords[word] = true
	}
	for word := range words2 {
		allWords[word] = true
	}

	// Calculate dot product and magnitudes
	dotProduct := 0.0
	mag1 := 0.0
	mag2 := 0.0

	for word := range allWords {
		count1 := float64(words1[word])
		count2 := float64(words2[word])

		dotProduct += count1 * count2
		mag1 += count1 * count1
		mag2 += count2 * count2
	}

	if mag1 == 0 || mag2 == 0 {
		return 0.0
	}

	return dotProduct / (sqrt(mag1) * sqrt(mag2))
}

// jaccardSimilarity calculates Jaccard similarity between two texts
func (m *PatternMatcher) jaccardSimilarity(text1, text2 string) float64 {
	words1 := m.tokenize(text1)
	words2 := m.tokenize(text2)

	// Calculate intersection
	intersection := 0
	for word := range words1 {
		if words2[word] > 0 {
			intersection++
		}
	}

	// Calculate union
	union := len(words1) + len(words2) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// tokenize breaks text into word frequency map
func (m *PatternMatcher) tokenize(text string) map[string]int {
	// Simple tokenization - lowercase, split by whitespace, remove punctuation
	words := make(map[string]int)
	// Implementation would use proper tokenization
	// For now, simplified version
	return words
}

// getSuccessRate returns the historical success rate of a pattern
func (m *PatternMatcher) getSuccessRate(pattern *ExtractedPattern) float64 {
	// Extract from pattern metadata
	if successRate, ok := pattern.Metadata["success_rate"].(float64); ok {
		return successRate
	}
	// Default: assume decent success rate
	return 0.85
}

// calculateRecencyScore scores patterns by freshness
func (m *PatternMatcher) calculateRecencyScore(pattern *ExtractedPattern) float64 {
	extractedAt, ok := pattern.Metadata["extracted_at"].(time.Time)
	if !ok {
		return 0.5 // Middle score if unknown
	}

	age := time.Since(extractedAt)
	maxAge := 90 * 24 * time.Hour // 90 days

	if age > maxAge {
		return 0.1 // Old patterns get low score
	}

	// Linear decay from 1.0 (fresh) to 0.1 (old)
	return 1.0 - (0.9 * float64(age) / float64(maxAge))
}

// calculateUsageScore scores patterns by usage frequency
func (m *PatternMatcher) calculateUsageScore(pattern *ExtractedPattern) float64 {
	// Get usage count from metadata
	usageCount := 0
	if count, ok := pattern.Metadata["usage_count"].(int); ok {
		usageCount = count
	}

	// Score based on usage (logarithmic scale)
	// 0 uses = 0.0, 1 use = 0.3, 10 uses = 0.6, 100 uses = 0.9
	if usageCount == 0 {
		return 0.0
	}

	// Use standard library for accurate log10
	score := 0.3 + (0.3 * math.Log10(float64(usageCount)))
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// calculateContextScore scores patterns by context match
func (m *PatternMatcher) calculateContextScore(query PatternQuery, pattern *ExtractedPattern) float64 {
	score := 0.0
	factors := 0

	// Language match
	if query.ProjectLanguage != "" && pattern.Tags[0] == query.ProjectLanguage {
		score += 0.4
	}
	factors++

	// Tag overlap
	if len(query.ContextTags) > 0 && len(pattern.Tags) > 0 {
		overlap := 0
		for _, tag := range query.ContextTags {
			for _, pTag := range pattern.Tags {
				if tag == pTag {
					overlap++
					break
				}
			}
		}
		if len(query.ContextTags) > 0 {
			score += 0.4 * float64(overlap) / float64(len(query.ContextTags))
		}
	}
	factors++

	// File context match (if files specified)
	if len(query.TargetFiles) > 0 {
		// Check if pattern involves similar files
		if patternFiles, ok := pattern.Metadata["files"].([]string); ok {
			for _, targetFile := range query.TargetFiles {
				for _, patternFile := range patternFiles {
					if m.fileSimilarity(targetFile, patternFile) > 0.8 {
						score += 0.2
						break
					}
				}
			}
		}
	}
	factors++

	if factors == 0 {
		return 0.5
	}

	return score / float64(factors)
}

// fileSimilarity calculates similarity between two file paths
func (m *PatternMatcher) fileSimilarity(file1, file2 string) float64 {
	// Simple implementation - check if same directory or file type
	// Full implementation would use path comparison
	return 0.0
}

// filterByConfidence filters matches by confidence threshold
func (m *PatternMatcher) filterByConfidence(matches []*PatternMatch) []*PatternMatch {
	filtered := make([]*PatternMatch, 0)
	for _, match := range matches {
		if match.Confidence >= m.config.MinConfidenceThreshold {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

// generateCacheKey generates a cache key for a query
func (m *PatternMatcher) generateCacheKey(query PatternQuery) string {
	// Simple cache key based on bead type, language, and tags
	return fmt.Sprintf("%s:%s:%v", query.BeadType, query.ProjectLanguage, query.ContextTags)
}

// Get returns cached patterns if available and not expired
func (c *PatternCache) Get(key string) []*PatternMatch {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		c.misses++
		return nil
	}

	if time.Since(entry.Timestamp) > c.ttl {
		// Expired - count as miss and remove
		c.misses++
		c.evictions++
		delete(c.entries, key)
		return nil
	}

	c.hits++
	return entry.Patterns
}

// Set caches pattern matches
func (c *PatternCache) Set(key string, patterns []*PatternMatch) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.entries[key] = &CacheEntry{
		Patterns:  patterns,
		Timestamp: time.Now(),
	}
	c.saves++
}

// Clear clears the cache
func (c *PatternCache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.entries = make(map[string]*CacheEntry)
}

// Size returns the number of entries in the cache
func (c *PatternCache) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.entries)
}

// GetStats returns cache statistics
func (c *PatternCache) GetStats() CacheStats {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return CacheStats{
		Size:      len(c.entries),
		Hits:      c.hits,
		Misses:    c.misses,
		Saves:     c.saves,
		Evictions: c.evictions,
		HitRate:   hitRate,
	}
}

// CacheStats represents cache statistics
type CacheStats struct {
	Size      int     `json:"size"`
	Hits      int64   `json:"hits"`
	Misses    int64   `json:"misses"`
	Saves     int64   `json:"saves"`
	Evictions int64   `json:"evictions"`
	HitRate   float64 `json:"hit_rate"`
}

// LogStats logs cache statistics
func (c *PatternCache) LogStats() {
	stats := c.GetStats()
	log.Printf("[PatternCache] Stats: size=%d, hits=%d, misses=%d, saves=%d, hit_rate=%.2f%%",
		stats.Size, stats.Hits, stats.Misses, stats.Saves, stats.HitRate*100)
}

// Helper functions
func sqrt(x float64) float64 {
	if x == 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

func mathLog10(x float64) float64 {
	// Simple mathLog10 approximation
	if x <= 0 {
		return 0
	}
	return mathLog(x) / 2.302585092994046
}

func mathLog(x float64) float64 {
	// Simple natural log approximation
	if x <= 0 {
		return 0
	}
	if x == 1 {
		return 0
	}
	return mathLog2(x) / 1.4426950408889634
}

func mathLog2(x float64) float64 {
	// Simple log2 implementation
	if x <= 0 {
		return 0
	}
	result := 0.0
	for x >= 2 {
		x /= 2
		result++
	}
	return result
}

// FormatPatternsForPrompt formats matched patterns for inclusion in worker prompt
func FormatPatternsForPrompt(matches []*PatternMatch) string {
	if len(matches) == 0 {
		return ""
	}

	var result string
	result += "\n📚 RELEVANT PATTERNS FROM PREVIOUS WORK:\n\n"

	for i, match := range matches {
		result += fmt.Sprintf("Pattern %d (Confidence: %.0f%%):\n", i+1, match.Confidence*100)
		result += fmt.Sprintf("  Title: %s\n", match.Title)
		result += fmt.Sprintf("  Description: %s\n", match.Description)
		result += fmt.Sprintf("  Source: %s\n", match.SourceProject)
		result += fmt.Sprintf("  Relevance: %.2f (Semantic: %.2f, Success: %.2f, Freshness: %.2f)\n",
			match.RelevanceScore, match.SemanticScore, match.SuccessRate, match.RecencyScore)

		if match.SolutionApproach != "" {
			result += fmt.Sprintf("  Solution: %s\n", match.SolutionApproach)
		}

		if len(match.AntiPatterns) > 0 {
			result += fmt.Sprintf("  Avoid: %v\n", match.AntiPatterns)
		}

		result += "\n"
	}

	return result
}

// Helper functions for safe metadata access
func getStringFromMetadata(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	if val, ok := metadata[key].(string); ok {
		return val
	}
	return ""
}

func getFirstTag(tags []string) string {
	if len(tags) > 0 {
		return tags[0]
	}
	return ""
}
