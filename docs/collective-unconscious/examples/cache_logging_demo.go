package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MockPatternStore for demonstration
type MockPatternStore struct {
	patterns []*intelligence.ExtractedPattern
}

func (m *MockPatternStore) GetApprovedPatterns(ctx context.Context) ([]*intelligence.ExtractedPattern, error) {
	return m.patterns, nil
}

func (m *MockPatternStore) GetPatternByID(ctx context.Context, patternID string) (*intelligence.ExtractedPattern, error) {
	for _, p := range m.patterns {
		if p.ID == patternID {
			return p, nil
		}
	}
	return nil, nil
}

func (m *MockPatternStore) SearchPatterns(ctx context.Context, query string, tags []string) ([]*intelligence.ExtractedPattern, error) {
	return m.patterns, nil
}

func main() {
	fmt.Println("=== Collective Unconscious Cache Logging Demo ===")

	// Create pattern store with sample patterns
	store := &MockPatternStore{
		patterns: []*intelligence.ExtractedPattern{
			{
				ID:              "pattern-1",
				Title:           "Error Handling Pattern",
				Description:     "Comprehensive error handling for Go APIs",
				Confidence:      0.95,
				ValidationScore: 0.92,
				Tags:            []string{"go", "api", "error-handling"},
				Metadata: map[string]interface{}{
					"source_project":       "auth-service",
					"success_rate":         0.95,
					"usage_count":          100,
					"extracted_at":         time.Now(),
					"security_scan_passed": true,
				},
			},
			{
				ID:              "pattern-2",
				Title:           "Database Connection Pool",
				Description:     "Manage database connections efficiently",
				Confidence:      0.88,
				ValidationScore: 0.85,
				Tags:            []string{"go", "database", "postgresql"},
				Metadata: map[string]interface{}{
					"source_project":       "payment-service",
					"success_rate":         0.92,
					"usage_count":          75,
					"extracted_at":         time.Now().Add(-24 * time.Hour),
					"security_scan_passed": true,
				},
			},
		},
	}

	// Create matcher with caching enabled
	config := intelligence.DefaultPatternMatcherConfig()
	config.MinConfidenceThreshold = 0.05
	matcher := intelligence.NewPatternMatcher(config, store)

	fmt.Println("1️⃣ First Query (Cache Miss Expected):")
	fmt.Println("----------------------------------------")

	query1 := intelligence.PatternQuery{
		BeadID:          types.BeadID("test-project/bead-1"),
		Title:           "Add Error Handling",
		Description:     "Implement error handling for API endpoints",
		BeadType:        "feature",
		ProjectLanguage: "go",
		ContextTags:     []string{"go", "api", "error-handling"},
	}

	matches, err := matcher.FindPatterns(context.Background(), query1)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("\n✅ Found %d patterns\n", len(matches))

	// Log cache stats after first query
	fmt.Println("\n📊 Cache Stats After First Query:")
	matcher.LogCacheStats()

	fmt.Println("\n\n2️⃣ Second Query (Cache Hit Expected):")
	fmt.Println("----------------------------------------")

	// Same query - should hit cache
	matches2, err := matcher.FindPatterns(context.Background(), query1)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("\n✅ Found %d patterns (from cache)\n", len(matches2))

	// Log cache stats after second query
	fmt.Println("\n📊 Cache Stats After Second Query:")
	matcher.LogCacheStats()

	fmt.Println("\n\n3️⃣ Different Query (New Cache Entry):")
	fmt.Println("----------------------------------------")

	query2 := intelligence.PatternQuery{
		BeadID:          types.BeadID("test-project/bead-2"),
		Title:           "Database Connection",
		Description:     "Set up database connection pool",
		BeadType:        "feature",
		ProjectLanguage: "go",
		ContextTags:     []string{"go", "database"},
	}

	matches3, err := matcher.FindPatterns(context.Background(), query2)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("\n✅ Found %d patterns\n", len(matches3))

	// Final cache stats
	fmt.Println("\n📊 Final Cache Stats:")
	matcher.LogCacheStats()

	fmt.Println("\n\n=== Demo Complete ===")
	fmt.Println("\n📋 Logged Events:")
	fmt.Println("  • Cache MISS - When pattern not found in cache")
	fmt.Println("  • Cache SAVE - When patterns are cached after lookup")
	fmt.Println("  • Cache HIT - When patterns found in cache")
	fmt.Println("  • Cache Stats - Size, hits, misses, saves, hit rate")
}
