//go:build integration

package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TuningConfig holds tunable parameters for Collective Unconscious
type TuningConfig struct {
	// Pattern Review thresholds
	AutoApproveThreshold float64 // Auto-approve if quality >= this (default: 0.90)
	AutoDiscardThreshold float64 // Auto-discard if quality < this (default: 0.50)

	// Pattern Matching thresholds
	MinConfidenceThreshold float64 // Minimum confidence to suggest pattern (default: 0.70)

	// Quality Gate thresholds
	StaticAnalysisThreshold float64 // Min score for static analysis (default: 0.7)
	TestCoverageThreshold   float64 // Min score for test coverage (default: 0.8)
	DocumentationThreshold  float64 // Min score for documentation (default: 0.7)
	ReviewScoreThreshold    float64 // Min score for review quality (default: 0.7)

	// Relevance Scoring Weights (must sum to 1.0)
	SemanticWeight float64 // Weight for semantic similarity (default: 0.35)
	SuccessWeight  float64 // Weight for historical success (default: 0.25)
	RecencyWeight  float64 // Weight for pattern freshness (default: 0.15)
	UsageWeight    float64 // Weight for usage frequency (default: 0.15)
	ContextWeight  float64 // Weight for context match (default: 0.10)
}

// BeadStats holds statistics about beads
type BeadStats struct {
	Total             int
	Extracted         int
	Approved          int
	Discarded         int
	RejectedByGates   int
	AvgDescriptionLen int
	AvgTitleLen       int
}

// TuningAnalyzer analyzes and recommends tuning adjustments
type TuningAnalyzer struct {
	beads []Bead
	stats BeadStats
}

// NewTuningAnalyzer creates a new analyzer
func NewTuningAnalyzer(beads []Bead) *TuningAnalyzer {
	return &TuningAnalyzer{
		beads: beads,
	}
}

// Analyze reviews all beads and provides tuning recommendations
func (a *TuningAnalyzer) Analyze() error {
	fmt.Println("🔍 Analyzing bead data for tuning recommendations...")
	fmt.Println(strings.Repeat("-", 60))

	// Calculate basic stats
	a.stats.Total = len(a.beads)
	totalDescLen := 0
	totalTitleLen := 0

	for _, bead := range a.beads {
		totalDescLen += len(bead.Description)
		totalTitleLen += len(bead.Title)
	}

	if a.stats.Total > 0 {
		a.stats.AvgDescriptionLen = totalDescLen / a.stats.Total
		a.stats.AvgTitleLen = totalTitleLen / a.stats.Total
	}

	// Simulate extraction with current thresholds
	config := intelligence.DefaultPatternReviewConfig()
	reviewService := intelligence.NewPatternReviewService(config)
	qualityService := intelligence.NewQualityGateService()

	for _, bead := range a.beads {
		pattern := a.createPatternFromBead(&bead)

		// Check quality gates
		validationResult, err := qualityService.Validate(pattern)
		if err != nil || !validationResult.Passed {
			a.stats.RejectedByGates++
			continue
		}

		// Try to submit for review
		reviewEntry, err := reviewService.SubmitPattern(pattern)
		if err != nil {
			continue
		}

		a.stats.Extracted++

		if reviewEntry.State == intelligence.ReviewStateAutoApproved ||
			reviewEntry.State == intelligence.ReviewStateApproved {
			a.stats.Approved++
		} else if reviewEntry.State == intelligence.ReviewStateAutoDiscarded ||
			reviewEntry.State == intelligence.ReviewStateDiscarded {
			a.stats.Discarded++
		}
	}

	// Print current performance
	fmt.Printf("\n📊 CURRENT PERFORMANCE:\n")
	fmt.Printf("   Total Beads: %d\n", a.stats.Total)
	fmt.Printf("   Extracted: %d (%.1f%%)\n", a.stats.Extracted, float64(a.stats.Extracted)/float64(a.stats.Total)*100)
	fmt.Printf("   Approved: %d (%.1f%%)\n", a.stats.Approved, float64(a.stats.Approved)/float64(a.stats.Total)*100)
	fmt.Printf("   Rejected by Gates: %d (%.1f%%)\n", a.stats.RejectedByGates, float64(a.stats.RejectedByGates)/float64(a.stats.Total)*100)
	fmt.Printf("   Avg Description Length: %d chars\n", a.stats.AvgDescriptionLen)
	fmt.Printf("   Avg Title Length: %d chars\n", a.stats.AvgTitleLen)

	return nil
}

// Recommendations provides specific tuning recommendations
func (a *TuningAnalyzer) Recommendations() {
	fmt.Println("\n💡 TUNING RECOMMENDATIONS:")
	fmt.Println(strings.Repeat("-", 60))

	extractionRate := float64(a.stats.Extracted) / float64(a.stats.Total) * 100

	if extractionRate < 10 {
		fmt.Println("\n⚠️  LOW EXTRACTION RATE (", extractionRate, "%) - SUGGESTED FIXES:")
		fmt.Println()
		fmt.Println("1. Lower Auto-Approve Threshold:")
		fmt.Println("   Current: 0.90")
		fmt.Println("   Suggested: 0.75-0.80")
		fmt.Println("   Impact: More patterns will be auto-approved")
		fmt.Println()
		fmt.Println("2. Lower Quality Gate Thresholds:")
		fmt.Println("   Current: Static Analysis >= 0.7, Documentation >= 0.7")
		fmt.Println("   Suggested: Static Analysis >= 0.5, Documentation >= 0.5")
		fmt.Println("   Impact: More patterns pass quality gates")
		fmt.Println()
		fmt.Println("3. Adjust Description Quality Scoring:")
		fmt.Printf("   Current avg description length: %d chars\n", a.stats.AvgDescriptionLen)
		if a.stats.AvgDescriptionLen < 100 {
			fmt.Println("   Suggested: Lower documentation threshold from 100 chars to 50")
		}
	}

	if extractionRate > 90 {
		fmt.Println("\n⚠️  HIGH EXTRACTION RATE - May be too permissive")
		fmt.Println("   Consider raising thresholds to ensure only high-quality patterns")
	}

	// Pattern matching recommendations
	fmt.Println("\n🔍 PATTERN MATCHING TUNING:")
	fmt.Println()
	fmt.Println("1. Lower Confidence Threshold:")
	fmt.Println("   Current: 0.70")
	fmt.Println("   Suggested: 0.50 for broader matching")
	fmt.Println("   Suggested: 0.60 for balanced precision/recall")
	fmt.Println()
	fmt.Println("2. Adjust Relevance Weights:")
	fmt.Println("   If matches aren't relevant enough:")
	fmt.Println("   - Increase SemanticWeight (0.35 → 0.45)")
	fmt.Println("   If successful patterns aren't prioritized:")
	fmt.Println("   - Increase SuccessWeight (0.25 → 0.35)")

	// Dataset-specific recommendations
	fmt.Println("\n📋 DATASET-SPECIFIC ADJUSTMENTS:")
	fmt.Println()
	if a.stats.AvgDescriptionLen < 50 {
		fmt.Println("⚠️  Short descriptions detected")
		fmt.Println("   - Lower DocumentationGate threshold")
		fmt.Println("   - Focus more on title matching")
	}

	if a.stats.AvgTitleLen < 20 {
		fmt.Println("⚠️  Short titles detected")
		fmt.Println("   - Increase weight of description in semantic matching")
	}
}

// TestImprovedConfig tests with recommended thresholds
func (a *TuningAnalyzer) TestImprovedConfig() {
	fmt.Println("\n🧪 TESTING IMPROVED CONFIGURATION:")
	fmt.Println(strings.Repeat("-", 60))

	// Create improved config
	improvedConfig := intelligence.PatternReviewConfig{
		Enabled:              true,
		AutoApproveThreshold: 0.75, // Lowered from 0.90
		AutoDiscardThreshold: 0.40, // Lowered from 0.50
		RequireHumanReview:   false,
		MaxPatternsInReview:  100,
		DuplicateThreshold:   0.95,
		ReviewTimeout:        24 * time.Hour,
	}

	reviewService := intelligence.NewPatternReviewService(improvedConfig)
	qualityService := intelligence.NewQualityGateService()

	extracted := 0
	approved := 0

	for _, bead := range a.beads {
		pattern := a.createPatternFromBead(&bead)

		validationResult, err := qualityService.Validate(pattern)
		if err != nil || !validationResult.Passed {
			continue
		}

		reviewEntry, err := reviewService.SubmitPattern(pattern)
		if err != nil {
			continue
		}

		extracted++

		if reviewEntry.State == intelligence.ReviewStateAutoApproved ||
			reviewEntry.State == intelligence.ReviewStateApproved {
			approved++
		}
	}

	improvedRate := float64(extracted) / float64(a.stats.Total) * 100

	fmt.Printf("\n📊 WITH IMPROVED THRESHOLDS:\n")
	fmt.Printf("   Extracted: %d (%.1f%%) [was: %.1f%%]\n", extracted, improvedRate, float64(a.stats.Extracted)/float64(a.stats.Total)*100)
	fmt.Printf("   Improvement: +%.1f%% more patterns\n", improvedRate-float64(a.stats.Extracted)/float64(a.stats.Total)*100)
}

func (a *TuningAnalyzer) createPatternFromBead(bead *Bead) *intelligence.ExtractedPattern {
	// Calculate quality score based on bead content
	quality := 0.5
	if len(bead.Description) > 100 {
		quality += 0.2
	}
	if len(bead.Description) > 200 {
		quality += 0.1
	}
	if bead.Priority <= 2 {
		quality += 0.1
	}
	if len(bead.Title) > 20 {
		quality += 0.1
	}
	if quality > 1.0 {
		quality = 1.0
	}

	return &intelligence.ExtractedPattern{
		ID:              fmt.Sprintf("pattern-%s", bead.ID),
		BeadID:          types.BeadID(bead.ID),
		Title:           bead.Title,
		Description:     bead.Description,
		Confidence:      quality,
		ValidationScore: quality,
		Tags:            a.extractTags(bead),
		Metadata: map[string]interface{}{
			"source_project":       "hydra-test-project",
			"success_rate":         0.9,
			"usage_count":          10,
			"extracted_at":         time.Now(),
			"security_scan_passed": true,
			"review_score":         quality * 5,
		},
	}
}

func (a *TuningAnalyzer) extractTags(bead *Bead) []string {
	tags := []string{"javascript", "node"}

	titleLower := strings.ToLower(bead.Title)
	descLower := strings.ToLower(bead.Description)

	if strings.Contains(titleLower, "test") || strings.Contains(descLower, "test") {
		tags = append(tags, "testing")
	}
	if strings.Contains(titleLower, "error") || strings.Contains(descLower, "error") {
		tags = append(tags, "error-handling")
	}
	if strings.Contains(titleLower, "api") || strings.Contains(descLower, "api") {
		tags = append(tags, "api")
	}
	if strings.Contains(titleLower, "calc") || strings.Contains(descLower, "arithmetic") {
		tags = append(tags, "calculator")
	}

	return tags
}

// PrintYAMLConfig outputs the recommended YAML configuration
func (a *TuningAnalyzer) PrintYAMLConfig() {
	fmt.Println("\n📝 RECOMMENDED YAML CONFIGURATION:")
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println(`
collective_unconscious:
  enabled: true
  
  review:
    enabled: true
    auto_approve_threshold: 0.75    # Lowered from 0.90 for more extractions
    auto_discard_threshold: 0.40    # Lowered from 0.50 to keep borderline patterns
    require_human_review: false
    max_patterns_in_review: 100
    duplicate_threshold: 0.95
    review_timeout: 24h
    
  matching:
    enabled: true
    block_on_failure: true
    max_patterns_in_prompt: 3
    min_confidence_threshold: 0.50   # Lowered from 0.70 for more matches
    cache_enabled: true
    cache_ttl: 5m
    query_timeout: 500ms
    
  # Alternative: Strict Mode (for production with high-quality requirements)
  # review:
  #   auto_approve_threshold: 0.85
  #   auto_discard_threshold: 0.60
  # matching:
  #   min_confidence_threshold: 0.70`)
}

func TestTuningAnalyzer(t *testing.T) {
	t.Log("🔧 Collective Unconscious Tuning Analyzer")
	t.Log(strings.Repeat("=", 60))

	// Check if test data exists
	if _, err := os.Stat("/tmp/real-world-test/beads.db"); os.IsNotExist(err) {
		t.Skip("Test data not found at /tmp/real-world-test/")
	}

	// Load beads
	beads, err := LoadBeadsFromDatabase("/tmp/real-world-test/beads.db")
	if err != nil {
		t.Fatalf("Failed to load beads: %v", err)
	}

	// Create analyzer
	analyzer := NewTuningAnalyzer(beads)

	// Run analysis
	if err := analyzer.Analyze(); err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Show recommendations
	analyzer.Recommendations()

	// Test improved config
	analyzer.TestImprovedConfig()

	// Print YAML config
	analyzer.PrintYAMLConfig()

	t.Log("\n" + strings.Repeat("=", 60))
	t.Log("✅ Analysis complete!")
}
