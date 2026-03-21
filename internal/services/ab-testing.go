package services

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// ABTestVariant represents a single prompt variant in an A/B test
type ABTestVariant struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Prompt     string                 `json:"prompt"`
	BeadType   string                 `json:"bead_type"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	IsControl  bool                   `json:"is_control"`
	IsWinner   bool                   `json:"is_winner"`
	PromotedAt *time.Time             `json:"promoted_at,omitempty"`
}

// ABTestResult represents the outcome of a single bead processed with a variant
type ABTestResult struct {
	ID        string    `json:"id"`
	VariantID string    `json:"variant_id"`
	BeadID    string    `json:"bead_id"`
	Success   bool      `json:"success"`
	Duration  int64     `json:"duration_ms"`
	Metrics   Metrics   `json:"metrics"`
	CreatedAt time.Time `json:"created_at"`
}

// Metrics tracks success metrics for a variant
type Metrics struct {
	TotalBeads      int     `json:"total_beads"`
	Successes       int     `json:"successes"`
	Failures        int     `json:"failures"`
	SuccessRate     float64 `json:"success_rate"`
	AvgDurationMs   int64   `json:"avg_duration_ms"`
	TokenEfficiency float64 `json:"token_efficiency"`
	QualityScore    float64 `json:"quality_score"`
}

// StatisticalResult contains statistical significance test results
type StatisticalResult struct {
	VariantAID      string  `json:"variant_a_id"`
	VariantBID      string  `json:"variant_b_id"`
	VariantAMetrics Metrics `json:"variant_a_metrics"`
	VariantBMetrics Metrics `json:"variant_b_metrics"`
	PValue          float64 `json:"p_value"`
	Significant     bool    `json:"significant"`
	ConfidenceLevel float64 `json:"confidence_level"`
	EffectSize      float64 `json:"effect_size"`
	WinnerID        string  `json:"winner_id,omitempty"`
	SampleSizeA     int     `json:"sample_size_a"`
	SampleSizeB     int     `json:"sample_size_b"`
}

// ABTestConfiguration configures A/B testing for a bead type
type ABTestConfiguration struct {
	BeadType           string           `json:"bead_type"`
	Enabled            bool             `json:"enabled"`
	MinSampleSize      int              `json:"min_sample_size"`
	MaxVariants        int              `json:"max_variants"`
	ConfidenceLevel    float64          `json:"confidence_level"`
	AutoPromoteEnabled bool             `json:"auto_promote_enabled"`
	ValidationPeriod   time.Duration    `json:"validation_period"`
	PromotionThreshold float64          `json:"promotion_threshold"`
	Variants           []*ABTestVariant `json:"variants,omitempty"`
}

// PromptVariantStore defines the interface for storing prompt variants
type PromptVariantStore interface {
	SaveVariant(ctx context.Context, variant *ABTestVariant) error
	GetVariant(ctx context.Context, id string) (*ABTestVariant, error)
	GetVariantsByBeadType(ctx context.Context, beadType string) ([]*ABTestVariant, error)
	UpdateVariant(ctx context.Context, variant *ABTestVariant) error
	DeleteVariant(ctx context.Context, id string) error
	GetActiveVariant(ctx context.Context, beadType string) (*ABTestVariant, error)
	PromoteVariant(ctx context.Context, id string) error
}

// ABTestResultStore defines the interface for storing test results
type ABTestResultStore interface {
	SaveResult(ctx context.Context, result *ABTestResult) error
	GetResultsByVariant(ctx context.Context, variantID string) ([]*ABTestResult, error)
	GetResultsByBeadType(ctx context.Context, beadType string, limit int) ([]*ABTestResult, error)
	GetMetricsForVariant(ctx context.Context, variantID string) (*Metrics, error)
}

// ABTestingService manages A/B prompt testing
type ABTestingService struct {
	variantStore PromptVariantStore
	resultStore  ABTestResultStore
	configs      map[string]*ABTestConfiguration
	mu           sync.RWMutex
}

// defaultMinSampleSize is the minimum number of samples required before
// auto-promotion can occur (as per requirements: 30+ samples)
const defaultMinSampleSize = 30

// defaultConfidenceLevel is the standard confidence level for statistical tests (95%)
const defaultConfidenceLevel = 0.95

// defaultPValueThreshold corresponds to p < 0.05 for statistical significance
const defaultPValueThreshold = 0.05

// NewABTestingService creates a new A/B testing service
func NewABTestingService(variantStore PromptVariantStore, resultStore ABTestResultStore) *ABTestingService {
	return &ABTestingService{
		variantStore: variantStore,
		resultStore:  resultStore,
		configs:      make(map[string]*ABTestConfiguration),
	}
}

// CreateVariant creates a new prompt variant for A/B testing
func (s *ABTestingService) CreateVariant(ctx context.Context, beadType string, name string, prompt string) (*ABTestVariant, error) {
	if beadType == "" || name == "" || prompt == "" {
		return nil, fmt.Errorf("beadType, name, and prompt are required")
	}

	variant := &ABTestVariant{
		ID:        generateVariantID(),
		Name:      name,
		Prompt:    prompt,
		BeadType:  beadType,
		Metadata:  make(map[string]interface{}),
		CreatedAt: time.Now(),
		IsControl: false,
		IsWinner:  false,
	}

	if err := s.variantStore.SaveVariant(ctx, variant); err != nil {
		return nil, fmt.Errorf("failed to save variant: %w", err)
	}

	return variant, nil
}

// generateVariantID generates a unique variant ID
func generateVariantID() string {
	return fmt.Sprintf("variant_%d", time.Now().UnixNano())
}

// CreateTest creates an A/B test with 2-3 prompt variants
func (s *ABTestingService) CreateTest(ctx context.Context, beadType string, variants []*ABTestVariant) error {
	if beadType == "" {
		return fmt.Errorf("beadType is required")
	}

	if len(variants) < 2 || len(variants) > 3 {
		return ErrInvalidVariantCount
	}

	// Mark first variant as control
	variants[0].IsControl = true

	// Save all variants
	for _, variant := range variants {
		variant.BeadType = beadType
		if variant.ID == "" {
			variant.ID = generateVariantID()
		}
		if variant.CreatedAt.IsZero() {
			variant.CreatedAt = time.Now()
		}
		if variant.Metadata == nil {
			variant.Metadata = make(map[string]interface{})
		}

		if err := s.variantStore.SaveVariant(ctx, variant); err != nil {
			return fmt.Errorf("failed to save variant %s: %w", variant.Name, err)
		}
	}

	// Create default configuration
	config := &ABTestConfiguration{
		BeadType:           beadType,
		Enabled:            true,
		MinSampleSize:      defaultMinSampleSize,
		MaxVariants:        3,
		ConfidenceLevel:    defaultConfidenceLevel,
		AutoPromoteEnabled: true,
		ValidationPeriod:   24 * time.Hour,
		PromotionThreshold: defaultPValueThreshold,
		Variants:           variants,
	}

	s.mu.Lock()
	s.configs[beadType] = config
	s.mu.Unlock()

	return nil
}

// RecordResult records the outcome of processing a bead with a variant
func (s *ABTestingService) RecordResult(ctx context.Context, variantID string, beadID string, success bool, durationMs int64) (*ABTestResult, error) {
	result := &ABTestResult{
		ID:        fmt.Sprintf("result_%d", time.Now().UnixNano()),
		VariantID: variantID,
		BeadID:    beadID,
		Success:   success,
		Duration:  durationMs,
		Metrics: Metrics{
			TotalBeads: 1,
		},
		CreatedAt: time.Now(),
	}

	if success {
		result.Metrics.Successes = 1
		result.Metrics.SuccessRate = 1.0
	} else {
		result.Metrics.Failures = 1
		result.Metrics.SuccessRate = 0.0
	}

	if err := s.resultStore.SaveResult(ctx, result); err != nil {
		return nil, fmt.Errorf("failed to save result: %w", err)
	}

	return result, nil
}

// GetVariantMetrics retrieves success metrics for a variant
func (s *ABTestingService) GetVariantMetrics(ctx context.Context, variantID string) (*Metrics, error) {
	metrics, err := s.resultStore.GetMetricsForVariant(ctx, variantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics for variant %s: %w", variantID, err)
	}
	return metrics, nil
}

// CompareVariants performs statistical significance testing between two variants
func (s *ABTestingService) CompareVariants(ctx context.Context, variantAID, variantBID string) (*StatisticalResult, error) {
	metricsA, err := s.resultStore.GetMetricsForVariant(ctx, variantAID)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics for variant A: %w", err)
	}

	metricsB, err := s.resultStore.GetMetricsForVariant(ctx, variantBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics for variant B: %w", err)
	}

	// Calculate p-value using two-proportion z-test
	pValue := CalculatePValue(metricsA.Successes, metricsA.TotalBeads, metricsB.Successes, metricsB.TotalBeads)

	// Determine if result is statistically significant
	significant := IsStatisticallySignificant(pValue, defaultConfidenceLevel)

	result := &StatisticalResult{
		VariantAID:      variantAID,
		VariantBID:      variantBID,
		VariantAMetrics: *metricsA,
		VariantBMetrics: *metricsB,
		PValue:          pValue,
		Significant:     significant,
		ConfidenceLevel: defaultConfidenceLevel,
		SampleSizeA:     metricsA.TotalBeads,
		SampleSizeB:     metricsB.TotalBeads,
	}

	// Calculate effect size (Cohen's h for proportions)
	if metricsA.TotalBeads > 0 && metricsB.TotalBeads > 0 {
		p1 := float64(metricsA.Successes) / float64(metricsA.TotalBeads)
		p2 := float64(metricsB.Successes) / float64(metricsB.TotalBeads)
		result.EffectSize = math.Abs(2 * (math.Asin(math.Sqrt(p1)) - math.Asin(math.Sqrt(p2))))
	}

	// Determine winner if significant
	if significant {
		if metricsA.SuccessRate > metricsB.SuccessRate {
			result.WinnerID = variantAID
		} else if metricsB.SuccessRate > metricsA.SuccessRate {
			result.WinnerID = variantBID
		}
	}

	return result, nil
}

// DetermineWinner determines if there's a statistically significant winner
func (s *ABTestingService) DetermineWinner(ctx context.Context, beadType string) (*ABTestVariant, *StatisticalResult, error) {
	variants, err := s.variantStore.GetVariantsByBeadType(ctx, beadType)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get variants: %w", err)
	}

	if len(variants) < 2 {
		return nil, nil, ErrNoActiveTest
	}

	// Get configuration for this bead type
	s.mu.RLock()
	config, exists := s.configs[beadType]
	s.mu.RUnlock()

	if !exists {
		config = &ABTestConfiguration{
			MinSampleSize:   defaultMinSampleSize,
			ConfidenceLevel: defaultConfidenceLevel,
		}
	}

	// Find control and best performing non-control variant
	var controlVariant *ABTestVariant
	var controlMetrics *Metrics
	var bestVariant *ABTestVariant
	var bestMetrics *Metrics

	for _, variant := range variants {
		metrics, err := s.resultStore.GetMetricsForVariant(ctx, variant.ID)
		if err != nil {
			continue
		}

		// Check minimum sample size
		if metrics.TotalBeads < config.MinSampleSize {
			continue
		}

		if variant.IsControl {
			controlVariant = variant
			controlMetrics = metrics
		} else {
			// Track best non-control variant
			if bestMetrics == nil || metrics.SuccessRate > bestMetrics.SuccessRate {
				bestVariant = variant
				bestMetrics = metrics
			}
		}
	}

	if controlVariant == nil || controlMetrics == nil {
		return nil, nil, fmt.Errorf("no control variant with metrics found")
	}

	if bestVariant == nil {
		return nil, nil, ErrTestNotSignificant
	}

	// Perform statistical comparison
	statResult, err := s.CompareVariants(ctx, controlVariant.ID, bestVariant.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compare variants: %w", err)
	}

	if !statResult.Significant {
		return nil, statResult, ErrTestNotSignificant
	}

	// Determine which variant is the winner based on statistical result
	var winner *ABTestVariant
	if statResult.WinnerID == controlVariant.ID {
		winner = controlVariant
	} else {
		winner = bestVariant
	}

	return winner, statResult, nil
}

// PromoteWinner promotes the winning variant to production
func (s *ABTestingService) PromoteWinner(ctx context.Context, variantID string) error {
	if err := s.variantStore.PromoteVariant(ctx, variantID); err != nil {
		return fmt.Errorf("failed to promote variant %s: %w", variantID, err)
	}
	return nil
}

// IsStatisticallySignificant checks if p-value indicates significance at given level.
// Returns true if p-value is less than (1 - confidenceLevel), indicating statistical significance.
// For standard 95% confidence level, returns true if p < 0.05.
func IsStatisticallySignificant(pValue, confidenceLevel float64) bool {
	if confidenceLevel <= 0 || confidenceLevel >= 1 {
		confidenceLevel = defaultConfidenceLevel
	}
	alpha := 1.0 - confidenceLevel
	return pValue < alpha
}

// CalculatePValue calculates the p-value for two proportions using a two-proportion z-test.
// This tests whether the success rates of two variants are significantly different.
//
// The z-test formula for two proportions:
//
//	z = (p1 - p2) / sqrt(p*(1-p) * (1/n1 + 1/n2))
//
// where:
//
//	p1 = successesA/totalA, p2 = successesB/totalB
//	p = (successesA + successesB) / (totalA + totalB) [pooled proportion]
//	n1 = totalA, n2 = totalB
//
// Returns the two-tailed p-value. A small p-value (< 0.05) indicates
// that the difference between the two proportions is statistically significant.
func CalculatePValue(successesA, totalA, successesB, totalB int) float64 {
	// Validate inputs
	if totalA <= 0 || totalB <= 0 {
		return 1.0 // No valid data, no significant difference
	}
	if successesA < 0 || successesB < 0 {
		return 1.0
	}
	if successesA > totalA || successesB > totalB {
		return 1.0
	}

	// Calculate proportions
	p1 := float64(successesA) / float64(totalA)
	p2 := float64(successesB) / float64(totalB)

	// Calculate pooled proportion
	pooled := float64(successesA+successesB) / float64(totalA+totalB)

	// If pooled proportion is 0 or 1, no variance to test
	if pooled <= 0 || pooled >= 1 {
		return 1.0
	}

	// Calculate standard error
	se := math.Sqrt(pooled * (1 - pooled) * (1.0/float64(totalA) + 1.0/float64(totalB)))

	// Avoid division by zero
	if se == 0 {
		return 1.0
	}

	// Calculate z-score
	z := (p1 - p2) / se

	// Calculate two-tailed p-value
	pValue := 2 * (1 - normalCDF(math.Abs(z)))

	// Ensure p-value is within valid range [0, 1]
	if pValue < 0 {
		pValue = 0
	}
	if pValue > 1 {
		pValue = 1
	}

	return pValue
}

// normalCDF calculates the cumulative distribution function for the standard normal distribution.
// Uses the approximation from Abramowitz and Stegun (1964).
func normalCDF(x float64) float64 {
	// Constants for approximation
	const (
		a1 = 0.254829592
		a2 = -0.284496736
		a3 = 1.421413741
		a4 = -1.453152027
		a5 = 1.061405429
		p  = 0.3275911
	)

	// Save the sign of x
	sign := 1.0
	if x < 0 {
		sign = -1.0
	}
	x = math.Abs(x) / math.Sqrt(2.0)

	// Abramowitz and Stegun formula 7.1.26
	t := 1.0 / (1.0 + p*x)
	y := 1.0 - (((((a5*t+a4)*t)+a3)*t+a2)*t+a1)*t*math.Exp(-x*x)

	return 0.5 * (1.0 + sign*y)
}

// GetActiveVariant returns the currently active variant for a bead type
func (s *ABTestingService) GetActiveVariant(ctx context.Context, beadType string) (*ABTestVariant, error) {
	variant, err := s.variantStore.GetActiveVariant(ctx, beadType)
	if err != nil {
		return nil, fmt.Errorf("failed to get active variant for bead type %s: %w", beadType, err)
	}
	return variant, nil
}

// GetTestConfiguration retrieves the A/B test configuration for a bead type
func (s *ABTestingService) GetTestConfiguration(ctx context.Context, beadType string) (*ABTestConfiguration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config, exists := s.configs[beadType]
	if !exists {
		return nil, fmt.Errorf("no configuration found for bead type: %s", beadType)
	}

	return config, nil
}

// UpdateTestConfiguration updates the A/B test configuration
func (s *ABTestingService) UpdateTestConfiguration(ctx context.Context, config *ABTestConfiguration) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	if config.BeadType == "" {
		return fmt.Errorf("bead type is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.configs[config.BeadType] = config
	return nil
}

// ListActiveTests returns all bead types with active A/B tests
func (s *ABTestingService) ListActiveTests(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeTests := make([]string, 0)
	for beadType, config := range s.configs {
		if config.Enabled {
			activeTests = append(activeTests, beadType)
		}
	}

	return activeTests, nil
}

// GetTestResults returns aggregated results for a bead type
func (s *ABTestingService) GetTestResults(ctx context.Context, beadType string) (map[string]*Metrics, error) {
	variants, err := s.variantStore.GetVariantsByBeadType(ctx, beadType)
	if err != nil {
		return nil, fmt.Errorf("failed to get variants for bead type %s: %w", beadType, err)
	}

	results := make(map[string]*Metrics)
	for _, variant := range variants {
		metrics, err := s.resultStore.GetMetricsForVariant(ctx, variant.ID)
		if err != nil {
			continue // Skip variants without metrics
		}
		results[variant.ID] = metrics
	}

	return results, nil
}

// ValidateSampleSize checks if we have enough samples for statistical significance.
// Returns true if both variants have at least minSampleSize total samples.
// Per requirements, auto-promotion requires 30+ samples.
func ValidateSampleSize(metricsA, metricsB *Metrics, minSampleSize int) (bool, error) {
	if minSampleSize <= 0 {
		minSampleSize = defaultMinSampleSize
	}

	if metricsA == nil || metricsB == nil {
		return false, fmt.Errorf("metrics cannot be nil: %w", ErrInsufficientData)
	}

	sufficient := metricsA.TotalBeads >= minSampleSize && metricsB.TotalBeads >= minSampleSize
	return sufficient, nil
}

// ErrVariantNotFound is returned when a variant is not found
var ErrVariantNotFound = fmt.Errorf("variant not found")

// ErrInsufficientData is returned when there's not enough data for statistical testing
var ErrInsufficientData = fmt.Errorf("insufficient data for statistical testing")

// ErrInvalidVariantCount is returned when too many or too few variants are provided
var ErrInvalidVariantCount = fmt.Errorf("invalid number of variants, must be 2-3")

// ErrNoActiveTest is returned when no active test exists for a bead type
var ErrNoActiveTest = fmt.Errorf("no active test for bead type")

// ErrTestNotSignificant is returned when results are not statistically significant
var ErrTestNotSignificant = fmt.Errorf("test results are not statistically significant")
