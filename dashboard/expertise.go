// Package dashboard provides expertise analytics and visualization capabilities
// for monitoring pattern success rates, knowledge gaps, and cross-project reuse metrics.
package dashboard

import (
	"context"
	"time"
)

// SuccessRatePoint represents a single data point for success rate over time.
type SuccessRatePoint struct {
	Timestamp   time.Time `json:"timestamp"`
	SuccessRate float64   `json:"success_rate"`
	TotalBeads  int       `json:"total_beads"`
	Successful  int       `json:"successful"`
}

// PatternMetrics represents metrics for a specific pattern.
type PatternMetrics struct {
	PatternID      string    `json:"pattern_id"`
	PatternName    string    `json:"pattern_name"`
	RecallCount    int       `json:"recall_count"`
	SuccessCount   int       `json:"success_count"`
	SuccessRate    float64   `json:"success_rate"`
	LastUsed       time.Time `json:"last_used"`
	ProjectsUsedIn []string  `json:"projects_used_in"`
}

// KnowledgeGap represents an area where queries are frequent but results are lacking.
type KnowledgeGap struct {
	Query        string    `json:"query"`
	QueryCount   int       `json:"query_count"`
	ResultCount  int       `json:"result_count"`
	AvgRelevance float64   `json:"avg_relevance"`
	LastQueried  time.Time `json:"last_queried"`
}

// ReuseMetrics tracks cross-project pattern reuse statistics.
type ReuseMetrics struct {
	PatternID      string    `json:"pattern_id"`
	SourceProject  string    `json:"source_project"`
	TargetProjects []string  `json:"target_projects"`
	ReuseCount     int       `json:"reuse_count"`
	AdoptionRate   float64   `json:"adoption_rate"`
	FirstDetected  time.Time `json:"first_detected"`
}

// Dashboard provides expertise analytics capabilities.
type Dashboard struct {
	dataProvider DataProvider
}

// DataProvider defines the interface for fetching dashboard data.
type DataProvider interface {
	GetSuccessRateHistory(ctx context.Context, timeframe Timeframe) ([]SuccessRatePoint, error)
	GetTopPatterns(ctx context.Context, limit int) ([]PatternMetrics, error)
	GetKnowledgeGaps(ctx context.Context, minQueries int) ([]KnowledgeGap, error)
	GetReuseMetrics(ctx context.Context) ([]ReuseMetrics, error)
}

// Timeframe defines a time range for queries.
type Timeframe struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// NewDashboard creates a new expertise dashboard with the specified data provider.
func NewDashboard(provider DataProvider) *Dashboard {
	return &Dashboard{
		dataProvider: provider,
	}
}

// GetSuccessRateOverTime returns the one-shot success rate trend over the specified timeframe.
func (d *Dashboard) GetSuccessRateOverTime(ctx context.Context, timeframe Timeframe) ([]SuccessRatePoint, error) {
	if d.dataProvider == nil {
		return nil, nil
	}
	return d.dataProvider.GetSuccessRateHistory(ctx, timeframe)
}

// GetMostValuablePatterns returns patterns with high recall and high success rates.
func (d *Dashboard) GetMostValuablePatterns(ctx context.Context, limit int) ([]PatternMetrics, error) {
	if d.dataProvider == nil {
		return nil, nil
	}
	return d.dataProvider.GetTopPatterns(ctx, limit)
}

// GetKnowledgeGaps identifies areas with frequent queries but low results.
func (d *Dashboard) GetKnowledgeGaps(ctx context.Context, minQueries int) ([]KnowledgeGap, error) {
	if d.dataProvider == nil {
		return nil, nil
	}
	return d.dataProvider.GetKnowledgeGaps(ctx, minQueries)
}

// GetCrossProjectReuse returns metrics on pattern reuse across projects.
func (d *Dashboard) GetCrossProjectReuse(ctx context.Context) ([]ReuseMetrics, error) {
	if d.dataProvider == nil {
		return nil, nil
	}
	return d.dataProvider.GetReuseMetrics(ctx)
}

// ValidatePattern checks if a pattern meets the threshold for being considered valuable.
func ValidatePattern(pattern PatternMetrics, minRecall int, minSuccessRate float64) bool {
	return pattern.RecallCount >= minRecall && pattern.SuccessRate >= minSuccessRate
}

// IsKnowledgeGap determines if a query represents a knowledge gap.
func IsKnowledgeGap(gap KnowledgeGap, threshold float64) bool {
	if gap.QueryCount == 0 {
		return false
	}
	ratio := float64(gap.ResultCount) / float64(gap.QueryCount)
	return ratio < threshold
}
