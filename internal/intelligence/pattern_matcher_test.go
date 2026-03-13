package intelligence

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MockPatternStore for testing
type MockPatternStore struct {
	patterns []*ExtractedPattern
}

func NewMockPatternStore() *MockPatternStore {
	return &MockPatternStore{
		patterns: make([]*ExtractedPattern, 0),
	}
}

func (m *MockPatternStore) GetApprovedPatterns(ctx context.Context) ([]*ExtractedPattern, error) {
	return m.patterns, nil
}

func (m *MockPatternStore) GetPatternByID(ctx context.Context, patternID string) (*ExtractedPattern, error) {
	for _, p := range m.patterns {
		if p.ID == patternID {
			return p, nil
		}
	}
	return nil, nil
}

func (m *MockPatternStore) SearchPatterns(ctx context.Context, query string, tags []string) ([]*ExtractedPattern, error) {
	return m.patterns, nil
}

func (m *MockPatternStore) AddPattern(pattern *ExtractedPattern) {
	m.patterns = append(m.patterns, pattern)
}

func TestPatternMatcherConfig_Defaults(t *testing.T) {
	config := DefaultPatternMatcherConfig()

	if !config.Enabled {
		t.Error("Default Enabled should be true")
	}

	if !config.BlockOnFailure {
		t.Error("Default BlockOnFailure should be true")
	}

	if config.MaxPatternsInPrompt != 3 {
		t.Errorf("MaxPatternsInPrompt = %d, want 3", config.MaxPatternsInPrompt)
	}

	if config.MinConfidenceThreshold != 0.50 {
		t.Errorf("MinConfidenceThreshold = %v, want 0.50", config.MinConfidenceThreshold)
	}

	if config.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want 5m", config.CacheTTL)
	}
}

func TestPatternMatcher_New(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	store := NewMockPatternStore()

	matcher := NewPatternMatcher(config, store)

	if matcher == nil {
		t.Fatal("NewPatternMatcher returned nil")
	}

	if !matcher.IsEnabled() {
		t.Error("Matcher should be enabled")
	}
}

func TestPatternMatcher_FindPatterns_Disabled(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	config.Enabled = false
	store := NewMockPatternStore()
	matcher := NewPatternMatcher(config, store)

	query := PatternQuery{
		BeadID:      types.BeadID("test-bead"),
		Title:       "Test Bead",
		Description: "A test bead",
		BeadType:    "feature",
	}

	matches, err := matcher.FindPatterns(context.Background(), query)
	if err != nil {
		t.Fatalf("FindPatterns failed: %v", err)
	}

	if len(matches) != 0 {
		t.Error("Should return no matches when disabled")
	}
}

func TestPatternMatcher_FindPatterns_NoPatterns(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	store := NewMockPatternStore()
	matcher := NewPatternMatcher(config, store)

	query := PatternQuery{
		BeadID:      types.BeadID("test-bead"),
		Title:       "Error Handling",
		Description: "How to handle errors in Go",
		BeadType:    "feature",
	}

	matches, err := matcher.FindPatterns(context.Background(), query)
	if err != nil {
		t.Fatalf("FindPatterns failed: %v", err)
	}

	if len(matches) != 0 {
		t.Error("Should return no matches when no patterns exist")
	}
}

func TestPatternMatcher_FindPatterns_Success(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	config.MinConfidenceThreshold = 0.0 // Accept all for testing
	store := NewMockPatternStore()

	// Add test patterns
	store.AddPattern(&ExtractedPattern{
		ID:              "pattern-1",
		Title:           "Error Handling Pattern",
		Description:     "How to handle errors in Go code properly",
		Confidence:      0.9,
		ValidationScore: 0.9,
		Tags:            []string{"go", "error-handling"},
		Metadata: map[string]interface{}{
			"success_rate": 0.95,
			"usage_count":  50,
			"extracted_at": time.Now().Add(-7 * 24 * time.Hour),
		},
	})

	store.AddPattern(&ExtractedPattern{
		ID:              "pattern-2",
		Title:           "Database Connection",
		Description:     "How to connect to PostgreSQL database",
		Confidence:      0.8,
		ValidationScore: 0.8,
		Tags:            []string{"go", "database", "postgresql"},
		Metadata: map[string]interface{}{
			"success_rate": 0.88,
			"usage_count":  30,
			"extracted_at": time.Now().Add(-30 * 24 * time.Hour),
		},
	})

	matcher := NewPatternMatcher(config, store)

	query := PatternQuery{
		BeadID:          types.BeadID("test-bead"),
		Title:           "Error Handling Implementation",
		Description:     "Need to handle errors in Go code properly",
		BeadType:        "feature",
		ProjectLanguage: "go",
		ContextTags:     []string{"go", "error-handling"},
	}

	matches, err := matcher.FindPatterns(context.Background(), query)
	if err != nil {
		t.Fatalf("FindPatterns failed: %v", err)
	}

	if len(matches) == 0 {
		t.Error("Should return matches")
	}

	// Should be sorted by relevance
	if len(matches) > 1 {
		if matches[0].RelevanceScore < matches[1].RelevanceScore {
			t.Error("Matches should be sorted by relevance (highest first)")
		}
	}
}

func TestPatternMatcher_MaxPatternsLimit(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	config.MaxPatternsInPrompt = 2
	config.MinConfidenceThreshold = 0.0
	store := NewMockPatternStore()

	// Add more patterns than limit
	for i := 1; i <= 5; i++ {
		store.AddPattern(&ExtractedPattern{
			ID:              fmt.Sprintf("pattern-%d", i),
			Title:           fmt.Sprintf("Pattern %d", i),
			Description:     fmt.Sprintf("Description %d", i),
			Confidence:      0.9,
			ValidationScore: 0.9,
			Tags:            []string{"go"},
			Metadata: map[string]interface{}{
				"success_rate": 0.9,
				"usage_count":  10,
				"extracted_at": time.Now(),
			},
		})
	}

	matcher := NewPatternMatcher(config, store)

	query := PatternQuery{
		BeadID:      types.BeadID("test-bead"),
		Title:       "Test",
		Description: "Test",
		BeadType:    "feature",
	}

	matches, err := matcher.FindPatterns(context.Background(), query)
	if err != nil {
		t.Fatalf("FindPatterns failed: %v", err)
	}

	if len(matches) > config.MaxPatternsInPrompt {
		t.Errorf("Should return at most %d patterns, got %d", config.MaxPatternsInPrompt, len(matches))
	}
}

func TestPatternMatcher_ConfidenceThreshold(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	config.MinConfidenceThreshold = 0.80
	store := NewMockPatternStore()

	matcher := NewPatternMatcher(config, store)

	// Test query
	query := PatternQuery{
		BeadID:      types.BeadID("test-bead"),
		Title:       "Test",
		Description: "Test query",
		BeadType:    "feature",
	}

	// Add low-quality pattern
	store.AddPattern(&ExtractedPattern{
		ID:              "low-quality",
		Title:           "Low Quality",
		Description:     "Not relevant",
		Confidence:      0.3,
		ValidationScore: 0.3,
		Tags:            []string{},
		Metadata: map[string]interface{}{
			"success_rate": 0.5,
			"usage_count":  1,
			"extracted_at": time.Now().Add(-60 * 24 * time.Hour),
		},
	})

	matches, err := matcher.FindPatterns(context.Background(), query)
	if err != nil {
		t.Fatalf("FindPatterns failed: %v", err)
	}

	// All matches should be above threshold
	for _, match := range matches {
		if match.Confidence < config.MinConfidenceThreshold {
			t.Errorf("Match confidence %.2f below threshold %.2f", match.Confidence, config.MinConfidenceThreshold)
		}
	}
}

func TestPatternMatcher_CalculateRecencyScore(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	store := NewMockPatternStore()
	matcher := NewPatternMatcher(config, store)

	now := time.Now()

	tests := []struct {
		name      string
		extracted time.Time
		minScore  float64
		maxScore  float64
	}{
		{
			name:      "Fresh pattern (1 day)",
			extracted: now.Add(-1 * 24 * time.Hour),
			minScore:  0.9,
			maxScore:  1.0,
		},
		{
			name:      "Medium age (30 days)",
			extracted: now.Add(-30 * 24 * time.Hour),
			minScore:  0.6,
			maxScore:  0.8,
		},
		{
			name:      "Old pattern (100 days)",
			extracted: now.Add(-100 * 24 * time.Hour),
			minScore:  0.0,
			maxScore:  0.2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := &ExtractedPattern{
				ID:       "test",
				Metadata: map[string]interface{}{"extracted_at": tt.extracted},
			}
			score := matcher.calculateRecencyScore(pattern)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("Recency score %.2f not in range [%.2f, %.2f]", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestPatternMatcher_CalculateUsageScore(t *testing.T) {
	config := DefaultPatternMatcherConfig()
	store := NewMockPatternStore()
	matcher := NewPatternMatcher(config, store)

	tests := []struct {
		usageCount int
		minScore   float64
	}{
		{0, 0.0},
		{1, 0.3},
		{10, 0.6},
		{100, 0.89}, // Allow for floating point precision
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Usage_%d", tt.usageCount), func(t *testing.T) {
			pattern := &ExtractedPattern{
				ID:       "test",
				Metadata: map[string]interface{}{"usage_count": tt.usageCount},
			}
			score := matcher.calculateUsageScore(pattern)
			if score < tt.minScore {
				t.Errorf("Usage score %.2f below minimum %.2f for count %d", score, tt.minScore, tt.usageCount)
			}
		})
	}
}

func TestPatternCache(t *testing.T) {
	cache := &PatternCache{
		entries: make(map[string]*CacheEntry),
		ttl:     5 * time.Minute,
	}

	// Test set and get
	key := "test-key"
	patterns := []*PatternMatch{
		{PatternID: "1", Confidence: 0.9},
	}

	cache.Set(key, patterns)

	retrieved := cache.Get(key)
	if len(retrieved) != 1 || retrieved[0].PatternID != "1" {
		t.Error("Cache get should return cached patterns")
	}

	// Test cache miss
	notFound := cache.Get("non-existent")
	if notFound != nil {
		t.Error("Cache miss should return nil")
	}

	// Test expiration
	cache.ttl = 1 * time.Nanosecond
	time.Sleep(10 * time.Nanosecond)
	expired := cache.Get(key)
	if expired != nil {
		t.Error("Expired cache entry should return nil")
	}
}

func TestFormatPatternsForPrompt(t *testing.T) {
	matches := []*PatternMatch{
		{
			PatternID:        "pattern-1",
			Title:            "Error Handling",
			Description:      "How to handle errors",
			Confidence:       0.95,
			RelevanceScore:   0.92,
			SemanticScore:    0.90,
			SuccessRate:      0.95,
			RecencyScore:     0.85,
			SourceProject:    "auth-service",
			SolutionApproach: "Use error wrapping with context",
			AntiPatterns:     []string{"panic", "log.Fatal"},
		},
	}

	result := FormatPatternsForPrompt(matches)

	if result == "" {
		t.Error("FormatPatternsForPrompt should return non-empty string")
	}

	// Should contain key information
	if !strings.Contains(result, "Error Handling") {
		t.Error("Should contain pattern title")
	}

	if !strings.Contains(result, "95%") {
		t.Error("Should contain confidence percentage")
	}

	if !strings.Contains(result, "auth-service") {
		t.Error("Should contain source project")
	}
}

func TestFormatPatternsForPrompt_Empty(t *testing.T) {
	result := FormatPatternsForPrompt(nil)
	if result != "" {
		t.Error("Empty matches should return empty string")
	}

	result = FormatPatternsForPrompt([]*PatternMatch{})
	if result != "" {
		t.Error("Empty slice should return empty string")
	}
}
