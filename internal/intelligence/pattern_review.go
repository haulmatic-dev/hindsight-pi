package intelligence

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// PatternReviewConfig contains configuration for the pattern review workflow
type PatternReviewConfig struct {
	Enabled              bool          `yaml:"enabled"`
	AutoApproveThreshold float64       `yaml:"auto_approve_threshold"` // Quality score > this gets auto-approved
	AutoDiscardThreshold float64       `yaml:"auto_discard_threshold"` // Quality score < this gets auto-discarded
	RequireHumanReview   bool          `yaml:"require_human_review"`   // For cross-project sharing
	MaxPatternsInReview  int           `yaml:"max_patterns_in_review"` // Max pending patterns before blocking new ones
	DuplicateThreshold   float64       `yaml:"duplicate_threshold"`    // Similarity threshold for duplicate detection
	ReviewTimeout        time.Duration `yaml:"review_timeout"`         // How long before auto-approve/reject
}

// DefaultPatternReviewConfig returns the default configuration
func DefaultPatternReviewConfig() PatternReviewConfig {
	return PatternReviewConfig{
		Enabled:              true,
		AutoApproveThreshold: 0.75,  // Lowered from 0.90 for more auto-approvals
		AutoDiscardThreshold: 0.40,  // Lowered from 0.50 to keep borderline patterns,
		RequireHumanReview:   false, // Auto-approve by default, human review optional
		MaxPatternsInReview:  100,
		DuplicateThreshold:   0.95,
		ReviewTimeout:        24 * time.Hour,
	}
}

// DiscardReason represents why a pattern was discarded
type DiscardReason string

const (
	DiscardReasonLowQuality   DiscardReason = "low_quality"
	DiscardReasonSecurityRisk DiscardReason = "security_risk"
	DiscardReasonNotReusable  DiscardReason = "not_reusable"
	DiscardReasonDuplicate    DiscardReason = "duplicate"
	DiscardReasonLicenseIssue DiscardReason = "license_issue"
	DiscardReasonOther        DiscardReason = "other"
)

// IsValid returns true if the discard reason is valid
func (r DiscardReason) IsValid() bool {
	switch r {
	case DiscardReasonLowQuality, DiscardReasonSecurityRisk, DiscardReasonNotReusable,
		DiscardReasonDuplicate, DiscardReasonLicenseIssue, DiscardReasonOther:
		return true
	}
	return false
}

// ReviewState represents the current state of a pattern in review
type ReviewState string

const (
	ReviewStatePendingReview ReviewState = "pending_review"
	ReviewStateApproved      ReviewState = "approved"
	ReviewStateAutoApproved  ReviewState = "auto_approved"
	ReviewStateDiscarded     ReviewState = "discarded"
	ReviewStateAutoDiscarded ReviewState = "auto_discarded"
	ReviewStateUnderAppeal   ReviewState = "under_appeal"
)

// IsTerminal returns true if the review state is terminal (no more transitions)
func (r ReviewState) IsTerminal() bool {
	return r == ReviewStateApproved || r == ReviewStateAutoApproved ||
		r == ReviewStateDiscarded || r == ReviewStateAutoDiscarded
}

// PatternReviewEntry tracks a pattern through the review process
type PatternReviewEntry struct {
	PatternID          string                 `json:"pattern_id"`
	BeadID             types.BeadID           `json:"bead_id"`
	State              ReviewState            `json:"state"`
	SubmittedAt        time.Time              `json:"submitted_at"`
	ReviewedAt         *time.Time             `json:"reviewed_at,omitempty"`
	ReviewedBy         string                 `json:"reviewed_by,omitempty"`
	QualityScore       float64                `json:"quality_score"`
	DiscardReason      *DiscardReason         `json:"discard_reason,omitempty"`
	DiscardExplanation string                 `json:"discard_explanation,omitempty"`
	DuplicateOf        string                 `json:"duplicate_of,omitempty"`
	AppealSubmitted    bool                   `json:"appeal_submitted"`
	AppealReason       string                 `json:"appeal_reason,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

// QualityGate represents a quality check for patterns
type QualityGate struct {
	Name    string  `json:"name"`
	Passed  bool    `json:"passed"`
	Score   float64 `json:"score"`
	Message string  `json:"message,omitempty"`
	Weight  float64 `json:"weight"`
}

// PatternReviewService manages the pattern review workflow
type PatternReviewService struct {
	config            PatternReviewConfig
	reviewQueue       map[string]*PatternReviewEntry
	approvedPatterns  map[string]*PatternReviewEntry
	discardedPatterns map[string]*PatternReviewEntry
	existingPatterns  map[string]*ExtractedPattern // For duplicate detection
	mutex             sync.RWMutex
	reviewCallbacks   []func(*PatternReviewEntry)
	approvalCallbacks []func(*PatternReviewEntry)
	discardCallbacks  []func(*PatternReviewEntry)
}

// NewPatternReviewService creates a new pattern review service
func NewPatternReviewService(config PatternReviewConfig) *PatternReviewService {
	return &PatternReviewService{
		config:            config,
		reviewQueue:       make(map[string]*PatternReviewEntry),
		approvedPatterns:  make(map[string]*PatternReviewEntry),
		discardedPatterns: make(map[string]*PatternReviewEntry),
		existingPatterns:  make(map[string]*ExtractedPattern),
		reviewCallbacks:   make([]func(*PatternReviewEntry), 0),
		approvalCallbacks: make([]func(*PatternReviewEntry), 0),
		discardCallbacks:  make([]func(*PatternReviewEntry), 0),
	}
}

// OnReview registers a callback for when a pattern enters review
func (s *PatternReviewService) OnReview(callback func(*PatternReviewEntry)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.reviewCallbacks = append(s.reviewCallbacks, callback)
}

// OnApproval registers a callback for when a pattern is approved
func (s *PatternReviewService) OnApproval(callback func(*PatternReviewEntry)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.approvalCallbacks = append(s.approvalCallbacks, callback)
}

// OnDiscard registers a callback for when a pattern is discarded
func (s *PatternReviewService) OnDiscard(callback func(*PatternReviewEntry)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.discardCallbacks = append(s.discardCallbacks, callback)
}

// SubmitPattern submits a pattern for review
func (s *PatternReviewService) SubmitPattern(pattern *ExtractedPattern) (*PatternReviewEntry, error) {
	if !s.config.Enabled {
		// If review is disabled, auto-approve everything
		return s.autoApprovePattern(pattern, "review_disabled"), nil
	}

	s.mutex.Lock()

	// Check if review queue is full
	if len(s.reviewQueue) >= s.config.MaxPatternsInReview {
		s.mutex.Unlock()
		return nil, fmt.Errorf("review queue is full (max %d patterns)", s.config.MaxPatternsInReview)
	}

	// Calculate quality score
	qualityScore := s.calculateQualityScore(pattern)

	// Check for duplicates
	if duplicateID := s.findDuplicate(pattern); duplicateID != "" {
		s.mutex.Unlock()
		return s.autoDiscardPattern(pattern, DiscardReasonDuplicate,
			fmt.Sprintf("Duplicate of pattern %s (similarity > %.2f)", duplicateID, s.config.DuplicateThreshold),
			duplicateID), nil
	}

	// Auto-approve high-quality patterns
	if qualityScore >= s.config.AutoApproveThreshold {
		s.mutex.Unlock()
		return s.autoApprovePattern(pattern, fmt.Sprintf("quality_score_%.2f", qualityScore)), nil
	}

	// Auto-discard low-quality patterns
	if qualityScore < s.config.AutoDiscardThreshold {
		s.mutex.Unlock()
		return s.autoDiscardPattern(pattern, DiscardReasonLowQuality,
			fmt.Sprintf("Quality score %.2f below threshold %.2f", qualityScore, s.config.AutoDiscardThreshold),
			""), nil
	}

	// Add to review queue
	entry := &PatternReviewEntry{
		PatternID:    pattern.ID,
		BeadID:       pattern.BeadID,
		State:        ReviewStatePendingReview,
		SubmittedAt:  time.Now(),
		QualityScore: qualityScore,
		Metadata:     make(map[string]interface{}),
	}

	s.reviewQueue[pattern.ID] = entry

	// Notify callbacks
	for _, cb := range s.reviewCallbacks {
		go cb(entry)
	}

	log.Printf("[PatternReview] Pattern %s submitted for review (quality: %.2f)", pattern.ID, qualityScore)

	s.mutex.Unlock()
	return entry, nil
}

// ApprovePattern approves a pattern (manual review)
func (s *PatternReviewService) ApprovePattern(patternID string, reviewer string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	entry, exists := s.reviewQueue[patternID]
	if !exists {
		return fmt.Errorf("pattern %s not found in review queue", patternID)
	}

	if entry.State.IsTerminal() {
		return fmt.Errorf("pattern %s is already in terminal state: %s", patternID, entry.State)
	}

	now := time.Now()
	entry.State = ReviewStateApproved
	entry.ReviewedAt = &now
	entry.ReviewedBy = reviewer

	// Move from queue to approved
	delete(s.reviewQueue, patternID)
	s.approvedPatterns[patternID] = entry

	// Notify callbacks
	for _, cb := range s.approvalCallbacks {
		go cb(entry)
	}

	log.Printf("[PatternReview] Pattern %s approved by %s", patternID, reviewer)

	return nil
}

// DiscardPattern discards a pattern (manual review)
func (s *PatternReviewService) DiscardPattern(patternID string, reviewer string, reason DiscardReason, explanation string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !reason.IsValid() {
		return fmt.Errorf("invalid discard reason: %s", reason)
	}

	entry, exists := s.reviewQueue[patternID]
	if !exists {
		return fmt.Errorf("pattern %s not found in review queue", patternID)
	}

	if entry.State.IsTerminal() {
		return fmt.Errorf("pattern %s is already in terminal state: %s", patternID, entry.State)
	}

	now := time.Now()
	entry.State = ReviewStateDiscarded
	entry.ReviewedAt = &now
	entry.ReviewedBy = reviewer
	entry.DiscardReason = &reason
	entry.DiscardExplanation = explanation

	// Move from queue to discarded
	delete(s.reviewQueue, patternID)
	s.discardedPatterns[patternID] = entry

	// Notify callbacks
	for _, cb := range s.discardCallbacks {
		go cb(entry)
	}

	log.Printf("[PatternReview] Pattern %s discarded by %s (reason: %s)", patternID, reviewer, reason)

	return nil
}

// SubmitAppeal allows a pattern author to appeal a discard decision
func (s *PatternReviewService) SubmitAppeal(patternID string, reason string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	entry, exists := s.discardedPatterns[patternID]
	if !exists {
		return fmt.Errorf("pattern %s not found in discarded patterns", patternID)
	}

	if entry.AppealSubmitted {
		return fmt.Errorf("pattern %s already has an appeal submitted", patternID)
	}

	entry.AppealSubmitted = true
	entry.AppealReason = reason
	entry.State = ReviewStateUnderAppeal

	// Move back to review queue for re-evaluation
	s.discardedPatterns[patternID] = entry
	s.reviewQueue[patternID] = entry

	log.Printf("[PatternReview] Appeal submitted for pattern %s", patternID)

	return nil
}

// GetReviewEntry returns a review entry by pattern ID
func (s *PatternReviewService) GetReviewEntry(patternID string) (*PatternReviewEntry, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Check all collections
	if entry, exists := s.reviewQueue[patternID]; exists {
		return entry, nil
	}
	if entry, exists := s.approvedPatterns[patternID]; exists {
		return entry, nil
	}
	if entry, exists := s.discardedPatterns[patternID]; exists {
		return entry, nil
	}

	return nil, fmt.Errorf("pattern %s not found", patternID)
}

// GetPendingReviews returns all patterns pending review
func (s *PatternReviewService) GetPendingReviews() []*PatternReviewEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	entries := make([]*PatternReviewEntry, 0, len(s.reviewQueue))
	for _, entry := range s.reviewQueue {
		if entry.State == ReviewStatePendingReview {
			entries = append(entries, entry)
		}
	}

	// Sort by quality score (highest first)
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].QualityScore > entries[i].QualityScore {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	return entries
}

// GetApprovedPatterns returns all approved patterns
func (s *PatternReviewService) GetApprovedPatterns() []*PatternReviewEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	entries := make([]*PatternReviewEntry, 0, len(s.approvedPatterns))
	for _, entry := range s.approvedPatterns {
		entries = append(entries, entry)
	}
	return entries
}

// GetDiscardedPatterns returns all discarded patterns
func (s *PatternReviewService) GetDiscardedPatterns() []*PatternReviewEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	entries := make([]*PatternReviewEntry, 0, len(s.discardedPatterns))
	for _, entry := range s.discardedPatterns {
		entries = append(entries, entry)
	}
	return entries
}

// BulkApprove approves multiple patterns at once
func (s *PatternReviewService) BulkApprove(patternIDs []string, reviewer string) (int, error) {
	approved := 0
	var lastErr error

	for _, id := range patternIDs {
		if err := s.ApprovePattern(id, reviewer); err != nil {
			lastErr = err
			log.Printf("[PatternReview] Failed to approve pattern %s: %v", id, err)
		} else {
			approved++
		}
	}

	return approved, lastErr
}

// BulkDiscard discards multiple patterns at once
func (s *PatternReviewService) BulkDiscard(patternIDs []string, reviewer string, reason DiscardReason, explanation string) (int, error) {
	discarded := 0
	var lastErr error

	for _, id := range patternIDs {
		if err := s.DiscardPattern(id, reviewer, reason, explanation); err != nil {
			lastErr = err
			log.Printf("[PatternReview] Failed to discard pattern %s: %v", id, err)
		} else {
			discarded++
		}
	}

	return discarded, lastErr
}

// GetStats returns review statistics
func (s *PatternReviewService) GetStats() PatternReviewStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := PatternReviewStats{
		TotalSubmitted: len(s.reviewQueue) + len(s.approvedPatterns) + len(s.discardedPatterns),
		PendingReview:  0,
		Approved:       len(s.approvedPatterns),
		AutoApproved:   0,
		Discarded:      len(s.discardedPatterns),
		AutoDiscarded:  0,
		UnderAppeal:    0,
	}

	for _, entry := range s.reviewQueue {
		switch entry.State {
		case ReviewStatePendingReview:
			stats.PendingReview++
		case ReviewStateUnderAppeal:
			stats.UnderAppeal++
		}
	}

	for _, entry := range s.approvedPatterns {
		if entry.State == ReviewStateAutoApproved {
			stats.AutoApproved++
		}
	}

	for _, entry := range s.discardedPatterns {
		if entry.State == ReviewStateAutoDiscarded {
			stats.AutoDiscarded++
		}
	}

	return stats
}

// PatternReviewStats contains review statistics
type PatternReviewStats struct {
	TotalSubmitted int `json:"total_submitted"`
	PendingReview  int `json:"pending_review"`
	Approved       int `json:"approved"`
	AutoApproved   int `json:"auto_approved"`
	Discarded      int `json:"discarded"`
	AutoDiscarded  int `json:"auto_discarded"`
	UnderAppeal    int `json:"under_appeal"`
}

// IsEnabled returns true if the service is enabled
func (s *PatternReviewService) IsEnabled() bool {
	return s.config.Enabled
}

// Internal helper functions

// Quality scoring constants
const (
	qualityScoreMax          = 1.0
	qualityConfidenceBoost   = 0.1
	qualityTagBoost          = 0.05
	qualityConfidenceThreshold = 0.9
	qualityTagThreshold      = 3
)

// calculateQualityScore computes a quality score for a pattern based on
// its validation score, confidence level, and metadata quality.
// The score is capped at 1.0 and boosts are applied for:
// - High confidence patterns (>= 0.9): +0.1
// - Well-tagged patterns (>= 3 tags): +0.05
func (s *PatternReviewService) calculateQualityScore(pattern *ExtractedPattern) float64 {
	if pattern == nil {
		return 0.0
	}

	score := pattern.ValidationScore
	score += s.calculateConfidenceBoost(pattern.Confidence)
	score += s.calculateTagBoost(pattern.Tags)

	return minFloat64(score, qualityScoreMax)
}

// calculateConfidenceBoost returns the boost amount based on pattern confidence
func (s *PatternReviewService) calculateConfidenceBoost(confidence float64) float64 {
	if confidence >= qualityConfidenceThreshold {
		return qualityConfidenceBoost
	}
	return 0.0
}

// calculateTagBoost returns the boost amount based on tag count
func (s *PatternReviewService) calculateTagBoost(tags []string) float64 {
	if len(tags) >= qualityTagThreshold {
		return qualityTagBoost
	}
	return 0.0
}

// minFloat64 returns the smaller of two float64 values
func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// boolToScore converts a boolean to a score (1.0 for true, 0.0 for false)
func (s *PatternReviewService) boolToScore(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

func (s *PatternReviewService) findDuplicate(pattern *ExtractedPattern) string {
	for id, existing := range s.existingPatterns {
		if s.calculateSimilarity(pattern, existing) >= s.config.DuplicateThreshold {
			return id
		}
	}
	return ""
}

func (s *PatternReviewService) calculateSimilarity(a, b *ExtractedPattern) float64 {
	// Compare titles and descriptions
	if (a.Title == "" && a.Description == "") || (b.Title == "" && b.Description == "") {
		return 0.0
	}

	// Combine title and description for comparison
	aText := a.Title + " " + a.Description
	bText := b.Title + " " + b.Description

	// Use Jaccard similarity on words
	aWords := s.tokenize(aText)
	bWords := s.tokenize(bText)

	intersection := 0
	for word := range aWords {
		if bWords[word] {
			intersection++
		}
	}

	union := len(aWords) + len(bWords) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

func (s *PatternReviewService) tokenize(text string) map[string]bool {
	words := make(map[string]bool)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		// Remove punctuation
		word = strings.TrimFunc(word, func(r rune) bool {
			return r == '.' || r == ',' || r == '!' || r == '?' || r == ';' || r == ':'
		})
		if len(word) > 2 { // Ignore very short words
			words[word] = true
		}
	}
	return words
}

func (s *PatternReviewService) autoApprovePattern(pattern *ExtractedPattern, reason string) *PatternReviewEntry {
	now := time.Now()
	entry := &PatternReviewEntry{
		PatternID:    pattern.ID,
		BeadID:       pattern.BeadID,
		State:        ReviewStateAutoApproved,
		SubmittedAt:  now,
		ReviewedAt:   &now,
		ReviewedBy:   "auto",
		QualityScore: s.calculateQualityScore(pattern),
		Metadata: map[string]interface{}{
			"auto_approve_reason": reason,
		},
	}

	s.mutex.Lock()
	s.approvedPatterns[pattern.ID] = entry
	s.mutex.Unlock()

	for _, cb := range s.approvalCallbacks {
		go cb(entry)
	}

	log.Printf("[PatternReview] Pattern %s auto-approved (reason: %s)", pattern.ID, reason)

	return entry
}

func (s *PatternReviewService) autoDiscardPattern(pattern *ExtractedPattern, reason DiscardReason, explanation string, duplicateOf string) *PatternReviewEntry {
	now := time.Now()
	entry := &PatternReviewEntry{
		PatternID:          pattern.ID,
		BeadID:             pattern.BeadID,
		State:              ReviewStateAutoDiscarded,
		SubmittedAt:        now,
		ReviewedAt:         &now,
		ReviewedBy:         "auto",
		QualityScore:       s.calculateQualityScore(pattern),
		DiscardReason:      &reason,
		DiscardExplanation: explanation,
		DuplicateOf:        duplicateOf,
		Metadata:           make(map[string]interface{}),
	}

	s.mutex.Lock()
	s.discardedPatterns[pattern.ID] = entry
	s.mutex.Unlock()

	for _, cb := range s.discardCallbacks {
		go cb(entry)
	}

	log.Printf("[PatternReview] Pattern %s auto-discarded (reason: %s)", pattern.ID, reason)

	return entry
}
