package intelligence

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// FeedbackOutcome represents the outcome of using a pattern
type FeedbackOutcome string

const (
	OutcomeUsed      FeedbackOutcome = "used"      // Pattern was applied as-is
	OutcomeModified  FeedbackOutcome = "modified"  // Pattern was adapted/modified
	OutcomeIgnored   FeedbackOutcome = "ignored"   // Pattern was suggested but not used
	OutcomeFailed    FeedbackOutcome = "failed"    // Pattern led to failure
	OutcomeSucceeded FeedbackOutcome = "succeeded" // Pattern led to success
)

// FeedbackEntry represents feedback for a pattern usage
type FeedbackEntry struct {
	ID             string          `json:"id"`
	PatternID      string          `json:"pattern_id"`
	BeadID         types.BeadID    `json:"bead_id"`
	Outcome        FeedbackOutcome `json:"outcome"`
	ExplicitRating int             `json:"explicit_rating,omitempty"` // 1-5 scale
	TimeToComplete time.Duration   `json:"time_to_complete,omitempty"`
	Adaptations    []string        `json:"adaptations,omitempty"` // What was changed
	Comments       string          `json:"comments,omitempty"`
	SubmittedAt    time.Time       `json:"submitted_at"`
	SubmittedBy    string          `json:"submitted_by,omitempty"`
}

// PatternScore tracks a pattern's current score and metrics
type PatternScore struct {
	PatternID         string        `json:"pattern_id"`
	CurrentScore      float64       `json:"current_score"` // 0.0 - 1.0
	BaseScore         float64       `json:"base_score"`    // Original quality score
	SuccessCount      int           `json:"success_count"`
	FailureCount      int           `json:"failure_count"`
	UsageCount        int           `json:"usage_count"`
	IgnoreCount       int           `json:"ignore_count"`
	LastUsedAt        time.Time     `json:"last_used_at"`
	FirstUsedAt       time.Time     `json:"first_used_at"`
	AverageTimeToUse  time.Duration `json:"average_time_to_use"`
	TotalFeedback     int           `json:"total_feedback"`
	ExplicitRatingSum int           `json:"explicit_rating_sum"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

// FeedbackService manages pattern feedback and score updates
type FeedbackService struct {
	config          types.CUFeedbackConfig
	scores          map[string]*PatternScore
	feedbackHistory map[string][]*FeedbackEntry
	mutex           sync.RWMutex
	updateCallbacks []func(patternID string, oldScore, newScore float64)
}

// NewFeedbackService creates a new feedback service
func NewFeedbackService(config types.CUFeedbackConfig) *FeedbackService {
	return &FeedbackService{
		config:          config,
		scores:          make(map[string]*PatternScore),
		feedbackHistory: make(map[string][]*FeedbackEntry),
		updateCallbacks: make([]func(patternID string, oldScore, newScore float64), 0),
	}
}

// IsEnabled returns true if the service is enabled
func (s *FeedbackService) IsEnabled() bool {
	return s.config.TrackOutcomes
}

// RecordOutcome records the outcome of using a pattern
func (s *FeedbackService) RecordOutcome(ctx context.Context, entry *FeedbackEntry) error {
	if !s.config.TrackOutcomes {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Initialize pattern score if not exists
	score, exists := s.scores[entry.PatternID]
	if !exists {
		score = &PatternScore{
			PatternID:    entry.PatternID,
			CurrentScore: 0.5, // Start neutral
			BaseScore:    0.5,
			FirstUsedAt:  time.Now(),
		}
		s.scores[entry.PatternID] = score
	}

	oldScore := score.CurrentScore

	// Update metrics based on outcome
	switch entry.Outcome {
	case OutcomeUsed, OutcomeSucceeded:
		score.SuccessCount++
		score.UsageCount++
		score.CurrentScore = s.adjustScore(score.CurrentScore, 0.02)
	case OutcomeModified:
		score.UsageCount++
		score.CurrentScore = s.adjustScore(score.CurrentScore, 0.01)
	case OutcomeIgnored:
		score.IgnoreCount++
		score.CurrentScore = s.adjustScore(score.CurrentScore, -0.01)
	case OutcomeFailed:
		score.FailureCount++
		score.UsageCount++
		score.CurrentScore = s.adjustScore(score.CurrentScore, -0.05)
	}

	// Update time tracking
	score.LastUsedAt = time.Now()
	if entry.TimeToComplete > 0 {
		if score.AverageTimeToUse == 0 {
			score.AverageTimeToUse = entry.TimeToComplete
		} else {
			// Rolling average
			totalUses := score.SuccessCount + score.FailureCount
			score.AverageTimeToUse = (score.AverageTimeToUse*time.Duration(totalUses-1) + entry.TimeToComplete) / time.Duration(totalUses)
		}
	}

	// Update explicit rating if provided
	if entry.ExplicitRating > 0 {
		score.TotalFeedback++
		score.ExplicitRatingSum += entry.ExplicitRating
		// Weight explicit feedback 2x
		ratingAdjustment := (float64(entry.ExplicitRating) - 3.0) * 0.02 // -0.04 to +0.04
		score.CurrentScore = s.adjustScore(score.CurrentScore, ratingAdjustment*2)
	}

	score.UpdatedAt = time.Now()

	// Store feedback in history
	s.feedbackHistory[entry.PatternID] = append(s.feedbackHistory[entry.PatternID], entry)

	// Notify callbacks
	for _, cb := range s.updateCallbacks {
		go cb(entry.PatternID, oldScore, score.CurrentScore)
	}

	log.Printf("[Feedback] Recorded outcome %s for pattern %s (score: %.2f -> %.2f)",
		entry.Outcome, entry.PatternID, oldScore, score.CurrentScore)

	return nil
}

// RecordExplicitFeedback records explicit user feedback (thumbs up/down)
func (s *FeedbackService) RecordExplicitFeedback(patternID string, rating int, comments string, user string) error {
	if !s.config.CollectExplicitFeedback {
		return nil
	}

	if rating < 1 || rating > 5 {
		return fmt.Errorf("rating must be between 1 and 5, got %d", rating)
	}

	entry := &FeedbackEntry{
		ID:             generateFeedbackID(),
		PatternID:      patternID,
		Outcome:        OutcomeUsed,
		ExplicitRating: rating,
		Comments:       comments,
		SubmittedAt:    time.Now(),
		SubmittedBy:    user,
	}

	return s.RecordOutcome(context.Background(), entry)
}

// GetPatternScore returns the current score for a pattern
func (s *FeedbackService) GetPatternScore(patternID string) (*PatternScore, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	score, exists := s.scores[patternID]
	if !exists {
		return nil, fmt.Errorf("pattern %s not found", patternID)
	}

	// Apply decay before returning
	decayedScore := s.applyDecay(score)
	score.CurrentScore = decayedScore

	return score, nil
}

// GetAllScores returns all pattern scores
func (s *FeedbackService) GetAllScores() []*PatternScore {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	scores := make([]*PatternScore, 0, len(s.scores))
	for _, score := range s.scores {
		// Apply decay
		score.CurrentScore = s.applyDecay(score)
		scores = append(scores, score)
	}

	return scores
}

// GetTopPatterns returns top N patterns by score
func (s *FeedbackService) GetTopPatterns(n int) []*PatternScore {
	scores := s.GetAllScores()

	// Sort by current score (descending)
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].CurrentScore > scores[i].CurrentScore {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	if n > len(scores) {
		n = len(scores)
	}

	return scores[:n]
}

// GetPatternFeedbackHistory returns feedback history for a pattern
func (s *FeedbackService) GetPatternFeedbackHistory(patternID string) ([]*FeedbackEntry, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	history, exists := s.feedbackHistory[patternID]
	if !exists {
		return nil, fmt.Errorf("no feedback history for pattern %s", patternID)
	}

	return history, nil
}

// OnScoreUpdate registers a callback for score updates
func (s *FeedbackService) OnScoreUpdate(callback func(patternID string, oldScore, newScore float64)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.updateCallbacks = append(s.updateCallbacks, callback)
}

// CalculateSuccessRate returns the success rate for a pattern
func (s *FeedbackService) CalculateSuccessRate(patternID string) (float64, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	score, exists := s.scores[patternID]
	if !exists {
		return 0, fmt.Errorf("pattern %s not found", patternID)
	}

	total := score.SuccessCount + score.FailureCount
	if total == 0 {
		return 0, nil
	}

	return float64(score.SuccessCount) / float64(total), nil
}

// CalculateAverageRating returns the average explicit rating
func (s *FeedbackService) CalculateAverageRating(patternID string) (float64, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	score, exists := s.scores[patternID]
	if !exists {
		return 0, fmt.Errorf("pattern %s not found", patternID)
	}

	if score.TotalFeedback == 0 {
		return 0, nil
	}

	return float64(score.ExplicitRatingSum) / float64(score.TotalFeedback), nil
}

// ResetScore resets a pattern's score to its base value
func (s *FeedbackService) ResetScore(patternID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	score, exists := s.scores[patternID]
	if !exists {
		return fmt.Errorf("pattern %s not found", patternID)
	}

	score.CurrentScore = score.BaseScore
	score.SuccessCount = 0
	score.FailureCount = 0
	score.UsageCount = 0
	score.IgnoreCount = 0
	score.UpdatedAt = time.Now()

	return nil
}

// DeletePattern removes a pattern and its feedback history
func (s *FeedbackService) DeletePattern(patternID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.scores, patternID)
	delete(s.feedbackHistory, patternID)

	return nil
}

// GetStats returns aggregate statistics
func (s *FeedbackService) GetStats() FeedbackStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := FeedbackStats{
		TotalPatterns:  len(s.scores),
		TotalFeedback:  0,
		AverageScore:   0,
		HighPerformers: 0,
		LowPerformers:  0,
	}

	var totalScore float64
	for _, score := range s.scores {
		totalScore += score.CurrentScore
		stats.TotalFeedback += score.TotalFeedback

		if score.CurrentScore >= 0.85 {
			stats.HighPerformers++
		}
		if score.CurrentScore < 0.50 {
			stats.LowPerformers++
		}
	}

	if stats.TotalPatterns > 0 {
		stats.AverageScore = totalScore / float64(stats.TotalPatterns)
	}

	return stats
}

// FeedbackStats contains aggregate feedback statistics
type FeedbackStats struct {
	TotalPatterns  int     `json:"total_patterns"`
	TotalFeedback  int     `json:"total_feedback"`
	AverageScore   float64 `json:"average_score"`
	HighPerformers int     `json:"high_performers"` // Score >= 0.85
	LowPerformers  int     `json:"low_performers"`  // Score < 0.50
}

// Internal helper functions

func (s *FeedbackService) adjustScore(current, delta float64) float64 {
	newScore := current + delta
	if newScore > 1.0 {
		return 1.0
	}
	if newScore < 0.0 {
		return 0.0
	}
	return newScore
}

func (s *FeedbackService) applyDecay(score *PatternScore) float64 {
	if s.config.DecayRate == 0 {
		return score.CurrentScore
	}

	daysSinceUpdate := time.Since(score.UpdatedAt).Hours() / 24
	if daysSinceUpdate <= 0 {
		return score.CurrentScore
	}

	// Exponential decay
	decayFactor := math.Exp(-s.config.DecayRate * daysSinceUpdate)
	decayedScore := score.BaseScore + (score.CurrentScore-score.BaseScore)*decayFactor

	return decayedScore
}

func generateFeedbackID() string {
	return fmt.Sprintf("fb-%d", time.Now().UnixNano())
}
