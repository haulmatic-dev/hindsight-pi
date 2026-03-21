// Package services provides retention policy services for Hydra.
// It implements utility-weighted retention policies to intelligently
// manage knowledge retention based on frequency, recency, and relevance.
package services

import (
	"errors"
	"math"
	"time"
)

// InsightDepth represents the depth level of an insight (1-5).
type InsightDepth int

const (
	// Level1Surface is surface-level information (e.g., logs, temporary data)
	Level1Surface InsightDepth = 1
	// Level2Contextual is contextual information (e.g., task-specific context)
	Level2Contextual InsightDepth = 2
	// Level3Pattern is pattern-level knowledge (e.g., recurring solutions)
	Level3Pattern InsightDepth = 3
	// Level4Principle is principle-level knowledge (e.g., design principles)
	Level4Principle InsightDepth = 4
	// Level5MentalModel is mental model knowledge (retain forever)
	Level5MentalModel InsightDepth = 5
)

// RetentionPeriod represents a duration for which insights should be retained.
type RetentionPeriod time.Duration

const (
	// Retention1Day retains insights for 1 day.
	Retention1Day RetentionPeriod = RetentionPeriod(24 * time.Hour)
	// Retention7Days retains insights for 7 days.
	Retention7Days RetentionPeriod = RetentionPeriod(7 * 24 * time.Hour)
	// Retention30Days retains insights for 30 days.
	Retention30Days RetentionPeriod = RetentionPeriod(30 * 24 * time.Hour)
	// Retention90Days retains insights for 90 days.
	Retention90Days RetentionPeriod = RetentionPeriod(90 * 24 * time.Hour)
	// RetentionForever retains insights indefinitely.
	RetentionForever RetentionPeriod = RetentionPeriod(-1)
)

// UtilityWeights represents the weight coefficients for utility calculation.
// U = α·frequency + β·recency + γ·relevance
type UtilityWeights struct {
	Alpha float64 // Weight for frequency component
	Beta  float64 // Weight for recency component
	Gamma float64 // Weight for relevance component
}

// DefaultUtilityWeights returns the default weights for utility calculation.
func DefaultUtilityWeights() UtilityWeights {
	// Default equal weighting: α = β = γ = 0.333...
	return UtilityWeights{
		Alpha: 0.333,
		Beta:  0.333,
		Gamma: 0.334,
	}
}

// Insight represents a piece of knowledge or information to be retained.
type Insight struct {
	ID             string            `json:"id"`
	Content        string            `json:"content"`
	Depth          InsightDepth      `json:"depth"`
	AccessCount    int               `json:"access_count"`
	LastAccessed   time.Time         `json:"last_accessed"`
	CreatedAt      time.Time         `json:"created_at"`
	RelevanceScore float64           `json:"relevance_score"` // 0.0 to 1.0
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// UtilityScore represents the calculated utility score for an insight.
type UtilityScore struct {
	InsightID string  `json:"insight_id"`
	Score     float64 `json:"score"`     // 0.0 to 1.0
	Frequency float64 `json:"frequency"` // Normalized frequency component
	Recency   float64 `json:"recency"`   // Normalized recency component
	Relevance float64 `json:"relevance"` // Normalized relevance component
}

// RetentionPolicy defines the retention configuration for insights.
type RetentionPolicy struct {
	Weights             UtilityWeights                   `json:"weights"`
	DepthPeriods        map[InsightDepth]RetentionPeriod `json:"depth_periods"`
	MinUtilityThreshold float64                          `json:"min_utility_threshold"`
}

// RetentionService provides utility-weighted retention policy management.
type RetentionService struct {
	policy RetentionPolicy
}

// NewRetentionService creates a new retention service with the specified policy.
func NewRetentionService(policy RetentionPolicy) *RetentionService {
	return &RetentionService{
		policy: policy,
	}
}

// CalculateUtilityScore calculates the utility score for an insight.
// Formula: U = α·frequency + β·recency + γ·relevance
// Returns error if calculation fails.
func (s *RetentionService) CalculateUtilityScore(insight Insight, referenceTime time.Time, maxAccessCount int) (UtilityScore, error) {
	if maxAccessCount <= 0 {
		maxAccessCount = 1
	}

	// Calculate normalized components
	frequency := NormalizeFrequency(insight.AccessCount, maxAccessCount)
	recency := NormalizeRecency(insight.LastAccessed, referenceTime, 90*24*time.Hour)
	relevance := insight.RelevanceScore

	// Apply weights: U = α·frequency + β·recency + γ·relevance
	score := s.policy.Weights.Alpha*frequency +
		s.policy.Weights.Beta*recency +
		s.policy.Weights.Gamma*relevance

	return UtilityScore{
		InsightID: insight.ID,
		Score:     score,
		Frequency: frequency,
		Recency:   recency,
		Relevance: relevance,
	}, nil
}

// GetRetentionPeriod returns the retention period for a given insight depth.
// Level 5 (Mental Models) returns RetentionForever.
func (s *RetentionService) GetRetentionPeriod(depth InsightDepth) (RetentionPeriod, error) {
	return GetInsightDepthRetentionPeriod(depth)
}

// ShouldRetain determines if an insight should be retained based on its utility score
// and depth level. Returns true if the insight should be kept.
func (s *RetentionService) ShouldRetain(insight Insight, utilityScore UtilityScore) (bool, error) {
	// Level 5 (Mental Models) should always be retained
	if insight.Depth == Level5MentalModel {
		return true, nil
	}

	// Check if utility score meets minimum threshold
	if utilityScore.Score >= s.policy.MinUtilityThreshold {
		return true, nil
	}

	return false, nil
}

// GetInsightDepthRetentionPeriod returns the default retention period for an insight depth level.
// This is a package-level function that provides the standard retention periods.
func GetInsightDepthRetentionPeriod(depth InsightDepth) (RetentionPeriod, error) {
	switch depth {
	case Level1Surface:
		return Retention1Day, nil
	case Level2Contextual:
		return Retention7Days, nil
	case Level3Pattern:
		return Retention30Days, nil
	case Level4Principle:
		return Retention90Days, nil
	case Level5MentalModel:
		return RetentionForever, nil
	default:
		return 0, errors.New("invalid insight depth")
	}
}

// ValidateUtilityWeights validates that the weights sum to approximately 1.0.
func ValidateUtilityWeights(weights UtilityWeights) error {
	sum := weights.Alpha + weights.Beta + weights.Gamma
	// Allow for small floating point errors (within 0.01)
	if math.Abs(sum-1.0) > 0.01 {
		return errors.New("utility weights must sum to approximately 1.0")
	}
	return nil
}

// NormalizeFrequency normalizes an access count to a 0-1 scale.
// Uses logarithmic scaling to handle varying access patterns.
func NormalizeFrequency(accessCount int, maxCount int) float64 {
	if accessCount <= 0 {
		return 0.0
	}
	if maxCount <= 0 {
		maxCount = 1
	}
	if accessCount >= maxCount {
		return 1.0
	}

	// Use logarithmic scaling: log(1 + count) / log(1 + max)
	normalized := math.Log1p(float64(accessCount)) / math.Log1p(float64(maxCount))
	return math.Min(1.0, math.Max(0.0, normalized))
}

// NormalizeRecency normalizes a time value to a 0-1 scale based on recency.
// More recent = higher score (closer to 1.0).
func NormalizeRecency(lastAccessed time.Time, referenceTime time.Time, maxAge time.Duration) float64 {
	if maxAge <= 0 {
		return 1.0
	}

	age := referenceTime.Sub(lastAccessed)
	if age <= 0 {
		return 1.0
	}
	if age >= maxAge {
		return 0.0
	}

	// Linear decay: 1.0 (newest) to 0.0 (oldest)
	return 1.0 - (float64(age) / float64(maxAge))
}
