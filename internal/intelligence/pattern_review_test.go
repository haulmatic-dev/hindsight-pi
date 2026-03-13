package intelligence

import (
	"fmt"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestPatternReviewConfig_Defaults(t *testing.T) {
	config := DefaultPatternReviewConfig()

	if !config.Enabled {
		t.Error("Default Enabled should be true")
	}

	if config.AutoApproveThreshold != 0.75 {
		t.Errorf("AutoApproveThreshold = %v, want 0.75", config.AutoApproveThreshold)
	}

	if config.AutoDiscardThreshold != 0.40 {
		t.Errorf("AutoDiscardThreshold = %v, want 0.40", config.AutoDiscardThreshold)
	}

	if config.MaxPatternsInReview != 100 {
		t.Errorf("MaxPatternsInReview = %v, want 100", config.MaxPatternsInReview)
	}

	if config.DuplicateThreshold != 0.95 {
		t.Errorf("DuplicateThreshold = %v, want 0.95", config.DuplicateThreshold)
	}
}

func TestDiscardReason_IsValid(t *testing.T) {
	tests := []struct {
		reason   DiscardReason
		expected bool
	}{
		{DiscardReasonLowQuality, true},
		{DiscardReasonSecurityRisk, true},
		{DiscardReasonNotReusable, true},
		{DiscardReasonDuplicate, true},
		{DiscardReasonLicenseIssue, true},
		{DiscardReasonOther, true},
		{DiscardReason("invalid"), false},
	}

	for _, tt := range tests {
		if got := tt.reason.IsValid(); got != tt.expected {
			t.Errorf("IsValid(%v) = %v, want %v", tt.reason, got, tt.expected)
		}
	}
}

func TestReviewState_IsTerminal(t *testing.T) {
	terminalStates := []ReviewState{
		ReviewStateApproved,
		ReviewStateAutoApproved,
		ReviewStateDiscarded,
		ReviewStateAutoDiscarded,
	}

	nonTerminalStates := []ReviewState{
		ReviewStatePendingReview,
		ReviewStateUnderAppeal,
	}

	for _, state := range terminalStates {
		if !state.IsTerminal() {
			t.Errorf("State %v should be terminal", state)
		}
	}

	for _, state := range nonTerminalStates {
		if state.IsTerminal() {
			t.Errorf("State %v should not be terminal", state)
		}
	}
}

func TestPatternReviewService_New(t *testing.T) {
	config := DefaultPatternReviewConfig()
	svc := NewPatternReviewService(config)

	if svc == nil {
		t.Fatal("NewPatternReviewService returned nil")
	}

	if !svc.IsEnabled() {
		t.Error("Service should be enabled by default")
	}
}

func TestPatternReviewService_SubmitPattern_AutoApprove(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.90
	svc := NewPatternReviewService(config)

	// Create high-quality pattern (should auto-approve)
	pattern := &ExtractedPattern{
		ID:              "pattern-1",
		BeadID:          types.BeadID("bead-1"),
		Title:           "High Quality Pattern",
		Description:     "A very good pattern with great validation",
		Confidence:      0.95,
		ValidationScore: 0.92,
		Tags:            []string{"go", "api", "testing"},
	}

	entry, err := svc.SubmitPattern(pattern)
	if err != nil {
		t.Fatalf("SubmitPattern failed: %v", err)
	}

	if entry.State != ReviewStateAutoApproved {
		t.Errorf("Expected auto-approved, got %v", entry.State)
	}

	if entry.ReviewedBy != "auto" {
		t.Errorf("Expected reviewed by 'auto', got %v", entry.ReviewedBy)
	}

	// Verify it's in approved patterns
	if _, exists := svc.approvedPatterns[pattern.ID]; !exists {
		t.Error("Pattern should be in approved patterns")
	}
}

func TestPatternReviewService_SubmitPattern_AutoDiscard(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoDiscardThreshold = 0.50
	svc := NewPatternReviewService(config)

	// Create low-quality pattern (should auto-discard)
	pattern := &ExtractedPattern{
		ID:              "pattern-2",
		BeadID:          types.BeadID("bead-2"),
		Title:           "Low Quality Pattern",
		Description:     "A poor pattern",
		Confidence:      0.30,
		ValidationScore: 0.30,
		Tags:            []string{},
	}

	entry, err := svc.SubmitPattern(pattern)
	if err != nil {
		t.Fatalf("SubmitPattern failed: %v", err)
	}

	if entry.State != ReviewStateAutoDiscarded {
		t.Errorf("Expected auto-discarded, got %v", entry.State)
	}

	if entry.DiscardReason == nil {
		t.Error("DiscardReason should be set")
	}

	if *entry.DiscardReason != DiscardReasonLowQuality {
		t.Errorf("Expected discard reason low_quality, got %v", *entry.DiscardReason)
	}
}

func TestPatternReviewService_SubmitPattern_PendingReview(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.90
	config.AutoDiscardThreshold = 0.50
	svc := NewPatternReviewService(config)

	// Create medium-quality pattern (should go to review queue)
	pattern := &ExtractedPattern{
		ID:              "pattern-3",
		BeadID:          types.BeadID("bead-3"),
		Title:           "Medium Quality Pattern",
		Description:     "An okay pattern",
		Confidence:      0.70,
		ValidationScore: 0.70,
		Tags:            []string{"go"},
	}

	entry, err := svc.SubmitPattern(pattern)
	if err != nil {
		t.Fatalf("SubmitPattern failed: %v", err)
	}

	if entry.State != ReviewStatePendingReview {
		t.Errorf("Expected pending review, got %v", entry.State)
	}

	// Verify it's in review queue
	if _, exists := svc.reviewQueue[pattern.ID]; !exists {
		t.Error("Pattern should be in review queue")
	}
}

func TestPatternReviewService_ApprovePattern(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.99 // Ensure it goes to review queue
	svc := NewPatternReviewService(config)

	// Submit pattern to review queue
	pattern := &ExtractedPattern{
		ID:              "pattern-4",
		BeadID:          types.BeadID("bead-4"),
		Title:           "Pattern to Approve",
		Description:     "Good pattern",
		Confidence:      0.70,
		ValidationScore: 0.70,
		Tags:            []string{"go"},
	}

	svc.SubmitPattern(pattern)

	// Approve it
	err := svc.ApprovePattern(pattern.ID, "reviewer@example.com")
	if err != nil {
		t.Fatalf("ApprovePattern failed: %v", err)
	}

	// Verify state changed
	entry, _ := svc.GetReviewEntry(pattern.ID)
	if entry.State != ReviewStateApproved {
		t.Errorf("Expected approved, got %v", entry.State)
	}

	if entry.ReviewedBy != "reviewer@example.com" {
		t.Errorf("Expected reviewer reviewer@example.com, got %v", entry.ReviewedBy)
	}

	// Verify moved from queue to approved
	if _, inQueue := svc.reviewQueue[pattern.ID]; inQueue {
		t.Error("Pattern should not be in review queue after approval")
	}

	if _, inApproved := svc.approvedPatterns[pattern.ID]; !inApproved {
		t.Error("Pattern should be in approved patterns")
	}
}

func TestPatternReviewService_DiscardPattern(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.99 // Ensure it goes to review queue
	svc := NewPatternReviewService(config)

	// Submit pattern to review queue
	pattern := &ExtractedPattern{
		ID:              "pattern-5",
		BeadID:          types.BeadID("bead-5"),
		Title:           "Pattern to Discard",
		Description:     "Bad pattern",
		Confidence:      0.60,
		ValidationScore: 0.60,
		Tags:            []string{"go"},
	}

	svc.SubmitPattern(pattern)

	// Discard it
	err := svc.DiscardPattern(pattern.ID, "reviewer@example.com", DiscardReasonNotReusable, "Too specific")
	if err != nil {
		t.Fatalf("DiscardPattern failed: %v", err)
	}

	// Verify state changed
	entry, _ := svc.GetReviewEntry(pattern.ID)
	if entry.State != ReviewStateDiscarded {
		t.Errorf("Expected discarded, got %v", entry.State)
	}

	if entry.DiscardReason == nil || *entry.DiscardReason != DiscardReasonNotReusable {
		t.Error("DiscardReason should be not_reusable")
	}

	if entry.DiscardExplanation != "Too specific" {
		t.Errorf("Expected explanation 'Too specific', got %v", entry.DiscardExplanation)
	}
}

func TestPatternReviewService_DuplicateDetection(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.DuplicateThreshold = 0.80 // Lower threshold for testing
	svc := NewPatternReviewService(config)

	// Submit first pattern
	pattern1 := &ExtractedPattern{
		ID:              "pattern-original",
		BeadID:          types.BeadID("bead-1"),
		Title:           "Error Handling Pattern",
		Description:     "How to handle errors in Go code properly and gracefully",
		Confidence:      0.80,
		ValidationScore: 0.85,
		Tags:            []string{"go", "error-handling"},
	}

	svc.SubmitPattern(pattern1)
	// Manually add to existing patterns for duplicate detection
	svc.existingPatterns[pattern1.ID] = pattern1

	// Submit very similar pattern (should be auto-discarded as duplicate)
	pattern2 := &ExtractedPattern{
		ID:              "pattern-duplicate",
		BeadID:          types.BeadID("bead-2"),
		Title:           "Error Handling Pattern",                                  // Same title
		Description:     "How to handle errors in Go code properly and gracefully", // Same description
		Confidence:      0.85,
		ValidationScore: 0.88,
		Tags:            []string{"go", "error-handling"},
	}

	entry, err := svc.SubmitPattern(pattern2)
	if err != nil {
		t.Fatalf("SubmitPattern failed: %v", err)
	}

	if entry.State != ReviewStateAutoDiscarded {
		t.Errorf("Expected auto-discarded as duplicate, got %v", entry.State)
	}

	if entry.DiscardReason == nil || *entry.DiscardReason != DiscardReasonDuplicate {
		t.Error("DiscardReason should be duplicate")
	}
}

func TestPatternReviewService_GetPendingReviews(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.99 // Ensure patterns go to review queue
	svc := NewPatternReviewService(config)

	// Submit multiple patterns
	for i := 1; i <= 3; i++ {
		pattern := &ExtractedPattern{
			ID:              fmt.Sprintf("pattern-%d", i),
			BeadID:          types.BeadID(fmt.Sprintf("bead-%d", i)),
			Title:           fmt.Sprintf("Pattern %d", i),
			Description:     fmt.Sprintf("Description %d", i),
			Confidence:      0.60,
			ValidationScore: 0.60 + float64(i)*0.05,
			Tags:            []string{"go"},
		}
		svc.SubmitPattern(pattern)
	}

	pending := svc.GetPendingReviews()

	if len(pending) != 3 {
		t.Errorf("Expected 3 pending reviews, got %d", len(pending))
	}

	// Should be sorted by quality score (highest first)
	if pending[0].QualityScore < pending[1].QualityScore {
		t.Error("Pending reviews should be sorted by quality score (highest first)")
	}
}

func TestPatternReviewService_BulkOperations(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.99 // Ensure patterns go to review queue
	svc := NewPatternReviewService(config)

	// Submit multiple patterns
	patternIDs := []string{}
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("bulk-pattern-%d", i)
		patternIDs = append(patternIDs, id)
		pattern := &ExtractedPattern{
			ID:              id,
			BeadID:          types.BeadID(fmt.Sprintf("bead-%d", i)),
			Title:           fmt.Sprintf("Bulk Pattern %d", i),
			Description:     fmt.Sprintf("Description %d", i),
			Confidence:      0.60,
			ValidationScore: 0.60,
			Tags:            []string{"go"},
		}
		svc.SubmitPattern(pattern)
	}

	// Bulk approve first 3
	approved, err := svc.BulkApprove(patternIDs[:3], "bulk-reviewer")
	if err != nil {
		t.Logf("BulkApprove had some errors (expected): %v", err)
	}

	if approved != 3 {
		t.Errorf("Expected 3 approved, got %d", approved)
	}

	// Bulk discard last 2
	discarded, err := svc.BulkDiscard(patternIDs[3:], "bulk-reviewer", DiscardReasonNotReusable, "Bulk discard")
	if err != nil {
		t.Logf("BulkDiscard had some errors (expected): %v", err)
	}

	if discarded != 2 {
		t.Errorf("Expected 2 discarded, got %d", discarded)
	}

	// Verify counts
	stats := svc.GetStats()
	if stats.Approved != 3 {
		t.Errorf("Expected 3 approved in stats, got %d", stats.Approved)
	}
	if stats.Discarded != 2 {
		t.Errorf("Expected 2 discarded in stats, got %d", stats.Discarded)
	}
}

func TestPatternReviewService_Stats(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.90
	config.AutoDiscardThreshold = 0.50
	svc := NewPatternReviewService(config)

	// Submit patterns that will go to different states
	// Auto-approved
	svc.SubmitPattern(&ExtractedPattern{
		ID: "auto-approved-1", BeadID: types.BeadID("b1"),
		Title: "High", Description: "High quality",
		Confidence: 0.95, ValidationScore: 0.95, Tags: []string{"go"},
	})

	// Auto-discarded
	svc.SubmitPattern(&ExtractedPattern{
		ID: "auto-discarded-1", BeadID: types.BeadID("b2"),
		Title: "Low", Description: "Low quality",
		Confidence: 0.30, ValidationScore: 0.30, Tags: []string{},
	})

	// Pending review
	svc.SubmitPattern(&ExtractedPattern{
		ID: "pending-1", BeadID: types.BeadID("b3"),
		Title: "Medium", Description: "Medium quality",
		Confidence: 0.70, ValidationScore: 0.70, Tags: []string{"go"},
	})

	stats := svc.GetStats()

	if stats.TotalSubmitted != 3 {
		t.Errorf("Expected 3 total submitted, got %d", stats.TotalSubmitted)
	}

	if stats.PendingReview != 1 {
		t.Errorf("Expected 1 pending review, got %d", stats.PendingReview)
	}

	if stats.AutoApproved != 1 {
		t.Errorf("Expected 1 auto-approved, got %d", stats.AutoApproved)
	}

	if stats.AutoDiscarded != 1 {
		t.Errorf("Expected 1 auto-discarded, got %d", stats.AutoDiscarded)
	}
}

func TestPatternReviewService_Disabled(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.Enabled = false
	svc := NewPatternReviewService(config)

	if svc.IsEnabled() {
		t.Error("Service should be disabled")
	}

	// Submit pattern when disabled (should auto-approve)
	pattern := &ExtractedPattern{
		ID:              "pattern-disabled",
		BeadID:          types.BeadID("bead-1"),
		Title:           "Any Pattern",
		Description:     "Any quality",
		Confidence:      0.10,
		ValidationScore: 0.10,
		Tags:            []string{},
	}

	entry, err := svc.SubmitPattern(pattern)
	if err != nil {
		t.Fatalf("SubmitPattern failed: %v", err)
	}

	if entry.State != ReviewStateAutoApproved {
		t.Errorf("Expected auto-approved when disabled, got %v", entry.State)
	}

	if entry.Metadata["auto_approve_reason"] != "review_disabled" {
		t.Error("Should indicate review was disabled")
	}
}

func TestPatternReviewService_QueueFull(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.MaxPatternsInReview = 2
	config.AutoApproveThreshold = 0.99 // Ensure patterns go to queue
	svc := NewPatternReviewService(config)

	// Fill the queue
	for i := 1; i <= 2; i++ {
		pattern := &ExtractedPattern{
			ID:              fmt.Sprintf("queue-pattern-%d", i),
			BeadID:          types.BeadID(fmt.Sprintf("bead-%d", i)),
			Title:           fmt.Sprintf("Pattern %d", i),
			Description:     fmt.Sprintf("Description %d", i),
			Confidence:      0.70,
			ValidationScore: 0.70,
			Tags:            []string{"go"},
		}
		svc.SubmitPattern(pattern)
	}

	// Try to add one more (should fail)
	pattern3 := &ExtractedPattern{
		ID:              "queue-pattern-3",
		BeadID:          types.BeadID("bead-3"),
		Title:           "Pattern 3",
		Description:     "Description 3",
		Confidence:      0.70,
		ValidationScore: 0.70,
		Tags:            []string{"go"},
	}

	_, err := svc.SubmitPattern(pattern3)
	if err == nil {
		t.Error("Expected error when queue is full")
	}

	if !strings.Contains(err.Error(), "review queue is full") {
		t.Errorf("Expected 'review queue is full' error, got: %v", err)
	}
}

func TestPatternReviewService_Tokenize(t *testing.T) {
	svc := NewPatternReviewService(DefaultPatternReviewConfig())

	text := "How to handle errors in Go code properly"
	words := svc.tokenize(text)

	// Should contain meaningful words (length > 2)
	expected := map[string]bool{
		"how": true, "handle": true, "errors": true, "code": true, "properly": true,
	}

	for word := range expected {
		if !words[word] {
			t.Errorf("Expected word '%s' in tokenized output", word)
		}
	}

	// Should not contain short words like "in", "to"
	if words["in"] || words["to"] {
		t.Error("Should not contain short words (length <= 2)")
	}
}

func TestPatternReviewService_Similarity(t *testing.T) {
	svc := NewPatternReviewService(DefaultPatternReviewConfig())

	// Identical texts should have similarity 1.0
	sim1 := svc.calculateSimilarity(
		&ExtractedPattern{Title: "Error Handling", Description: "How to handle errors"},
		&ExtractedPattern{Title: "Error Handling", Description: "How to handle errors"},
	)
	if sim1 != 1.0 {
		t.Errorf("Identical texts should have similarity 1.0, got %f", sim1)
	}

	// Completely different texts should have low similarity
	sim2 := svc.calculateSimilarity(
		&ExtractedPattern{Title: "Error Handling", Description: "How to handle errors"},
		&ExtractedPattern{Title: "Database Connection", Description: "How to connect to database"},
	)
	if sim2 > 0.5 {
		t.Errorf("Different texts should have low similarity, got %f", sim2)
	}

	// Partially similar
	sim3 := svc.calculateSimilarity(
		&ExtractedPattern{Title: "Error Handling", Description: "How to handle errors in Go"},
		&ExtractedPattern{Title: "Error Handling", Description: "How to handle errors in Python"},
	)
	if sim3 < 0.3 || sim3 > 0.9 {
		t.Errorf("Partially similar texts should have medium similarity, got %f", sim3)
	}
}

func TestPatternReviewService_GetReviewEntry_NotFound(t *testing.T) {
	svc := NewPatternReviewService(DefaultPatternReviewConfig())

	_, err := svc.GetReviewEntry("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent pattern")
	}
}

func TestPatternReviewService_InvalidDiscardReason(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.99
	svc := NewPatternReviewService(config)

	// Add pattern to queue
	pattern := &ExtractedPattern{
		ID:              "invalid-discard-test",
		BeadID:          types.BeadID("bead-1"),
		Title:           "Test",
		Description:     "Test pattern",
		Confidence:      0.70,
		ValidationScore: 0.70,
		Tags:            []string{"go"},
	}
	svc.SubmitPattern(pattern)

	// Try to discard with invalid reason
	err := svc.DiscardPattern(pattern.ID, "reviewer", DiscardReason("invalid_reason"), "test")
	if err == nil {
		t.Error("Expected error for invalid discard reason")
	}
}

func TestPatternReviewService_AlreadyTerminal(t *testing.T) {
	config := DefaultPatternReviewConfig()
	config.AutoApproveThreshold = 0.99
	svc := NewPatternReviewService(config)

	// Add and approve pattern
	pattern := &ExtractedPattern{
		ID:              "terminal-test",
		BeadID:          types.BeadID("bead-1"),
		Title:           "Test",
		Description:     "Test pattern",
		Confidence:      0.70,
		ValidationScore: 0.70,
		Tags:            []string{"go"},
	}
	svc.SubmitPattern(pattern)
	svc.ApprovePattern(pattern.ID, "reviewer")

	// Try to approve again
	err := svc.ApprovePattern(pattern.ID, "reviewer2")
	if err == nil {
		t.Error("Expected error when trying to approve already-approved pattern")
	}
}
