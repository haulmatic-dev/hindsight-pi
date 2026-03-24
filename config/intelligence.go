// Package config provides configuration structures for Hydra's intelligence and performance systems.
// This includes performance requirements, monitoring thresholds, and operational limits.
package config

import (
	"fmt"
	"time"
)

// PerformanceConfig defines performance requirements and monitoring configuration
// for Hindsight operations and other intelligence services.
type PerformanceConfig struct {
	// Enabled determines if performance monitoring is active
	Enabled bool `yaml:"enabled"`

	// RecallPerformance configures memory recall (search) operations
	RecallPerformance RecallPerformanceConfig `yaml:"recall"`

	// RetainPerformance configures memory retention operations
	RetainPerformance RetainPerformanceConfig `yaml:"retain"`

	// ReflectPerformance configures pattern reflection/review operations
	ReflectPerformance ReflectPerformanceConfig `yaml:"reflect"`

	// Throughput configures query throughput requirements
	Throughput ThroughputConfig `yaml:"throughput"`

	// Startup configures server startup requirements
	Startup StartupConfig `yaml:"startup"`

	// Monitoring configures how performance metrics are collected and reported
	Monitoring PerformanceMonitoringConfig `yaml:"monitoring"`
}

// RecallPerformanceConfig defines performance requirements for memory recall (search) operations.
// Recall retrieves relevant memories from the knowledge base.
type RecallPerformanceConfig struct {
	// P95Latency is the maximum acceptable p95 latency (default: 500ms)
	P95Latency time.Duration `yaml:"p95_latency"`

	// P99Latency is the maximum acceptable p99 latency (default: 1s)
	P99Latency time.Duration `yaml:"p99_latency"`

	// CachedEmbeddingP95 is the target p95 latency with cached embeddings (default: 500ms)
	CachedEmbeddingP95 time.Duration `yaml:"cached_embedding_p95"`

	// Timeout is the maximum time to wait for a recall operation (default: 2s)
	Timeout time.Duration `yaml:"timeout"`

	// EnableCache determines if embedding caching is enabled
	EnableCache bool `yaml:"enable_cache"`
}

// RetainPerformanceConfig defines performance requirements for memory retention operations.
// Retain stores execution memories in the knowledge base, including LLM processing.
type RetainPerformanceConfig struct {
	// MaxLatency is the maximum acceptable latency (default: 5s)
	MaxLatency time.Duration `yaml:"max_latency"`

	// LLMProcessingTime is the expected LLM processing portion (default: 3s)
	LLMProcessingTime time.Duration `yaml:"llm_processing_time"`

	// Timeout is the maximum time to wait for a retain operation (default: 10s)
	Timeout time.Duration `yaml:"timeout"`

	// BatchSize is the maximum number of memories to retain in a batch (default: 10)
	BatchSize int `yaml:"batch_size"`
}

// ReflectPerformanceConfig defines performance requirements for pattern reflection.
// Reflect analyzes patterns and insights from execution memories.
type ReflectPerformanceConfig struct {
	// MaxLatency is the maximum acceptable latency (default: 10s)
	MaxLatency time.Duration `yaml:"max_latency"`

	// PerInsightLatency is the processing time per insight (default: 100ms)
	PerInsightLatency time.Duration `yaml:"per_insight_latency"`

	// MaxInsights is the maximum number of insights to process (default: 100)
	MaxInsights int `yaml:"max_insights"`

	// Timeout is the maximum time to wait for reflection (default: 30s)
	Timeout time.Duration `yaml:"timeout"`
}

// ThroughputConfig defines query throughput requirements.
type ThroughputConfig struct {
	// MinOpsPerSecond is the minimum required operations per second (default: 10)
	MinOpsPerSecond int `yaml:"min_ops_per_second"`

	// BurstSize is the maximum burst of operations allowed (default: 50)
	BurstSize int `yaml:"burst_size"`

	// MeasurementWindow is the time window for throughput calculation (default: 1m)
	MeasurementWindow time.Duration `yaml:"measurement_window"`
}

// StartupConfig defines server startup performance requirements.
type StartupConfig struct {
	// MaxStartupTime is the maximum acceptable startup time (default: 30s)
	MaxStartupTime time.Duration `yaml:"max_startup_time"`

	// HealthCheckTimeout is the timeout for health checks during startup (default: 5s)
	HealthCheckTimeout time.Duration `yaml:"health_check_timeout"`

	// MaxRetries is the maximum number of startup retries (default: 3)
	MaxRetries int `yaml:"max_retries"`

	// RetryDelay is the delay between startup retries (default: 2s)
	RetryDelay time.Duration `yaml:"retry_delay"`
}

// PerformanceMonitoringConfig configures how performance metrics are collected.
type PerformanceMonitoringConfig struct {
	// Enabled determines if performance monitoring is active
	Enabled bool `yaml:"enabled"`

	// MetricsRetention is how long to keep performance metrics (default: 7d)
	MetricsRetention time.Duration `yaml:"metrics_retention"`

	// SamplingRate is the fraction of operations to sample (1.0 = all) (default: 1.0)
	SamplingRate float64 `yaml:"sampling_rate"`

	// AlertThresholds defines when to alert on performance degradation
	AlertThresholds AlertThresholdConfig `yaml:"alert_thresholds"`

	// ExportInterval is how often to export metrics (default: 1m)
	ExportInterval time.Duration `yaml:"export_interval"`
}

// AlertThresholdConfig defines thresholds for performance alerts.
type AlertThresholdConfig struct {
	// LatencyMultiplier triggers alert when latency exceeds Nx requirement (default: 2.0)
	LatencyMultiplier float64 `yaml:"latency_multiplier"`

	// ErrorRateThreshold triggers alert when error rate exceeds this (default: 0.05)
	ErrorRateThreshold float64 `yaml:"error_rate_threshold"`

	// ThroughputDropThreshold triggers alert when throughput drops below this % (default: 0.8)
	ThroughputDropThreshold float64 `yaml:"throughput_drop_threshold"`
}

// DefaultPerformanceConfig returns the default performance configuration.
// These defaults align with the NFR-001 requirements.
func DefaultPerformanceConfig() *PerformanceConfig {
	return &PerformanceConfig{
		Enabled: true,
		RecallPerformance: RecallPerformanceConfig{
			P95Latency:         500 * time.Millisecond,
			P99Latency:         1 * time.Second,
			CachedEmbeddingP95: 500 * time.Millisecond,
			Timeout:            2 * time.Second,
			EnableCache:        true,
		},
		RetainPerformance: RetainPerformanceConfig{
			MaxLatency:        5 * time.Second,
			LLMProcessingTime: 3 * time.Second,
			Timeout:           10 * time.Second,
			BatchSize:         10,
		},
		ReflectPerformance: ReflectPerformanceConfig{
			MaxLatency:        10 * time.Second,
			PerInsightLatency: 100 * time.Millisecond,
			MaxInsights:       100,
			Timeout:           30 * time.Second,
		},
		Throughput: ThroughputConfig{
			MinOpsPerSecond:   10,
			BurstSize:         50,
			MeasurementWindow: 1 * time.Minute,
		},
		Startup: StartupConfig{
			MaxStartupTime:     30 * time.Second,
			HealthCheckTimeout: 5 * time.Second,
			MaxRetries:         3,
			RetryDelay:         2 * time.Second,
		},
		Monitoring: PerformanceMonitoringConfig{
			Enabled:          true,
			MetricsRetention: 7 * 24 * time.Hour,
			SamplingRate:     1.0,
			AlertThresholds: AlertThresholdConfig{
				LatencyMultiplier:       2.0,
				ErrorRateThreshold:      0.05,
				ThroughputDropThreshold: 0.8,
			},
			ExportInterval: 1 * time.Minute,
		},
	}
}

// Validate validates the performance configuration.
func (c *PerformanceConfig) Validate() error {
	if c == nil {
		return &ValidationError{Field: "config", Message: "config is nil"}
	}

	if !c.Enabled {
		return nil
	}

	// Validate recall performance
	if c.RecallPerformance.P95Latency <= 0 {
		return &ValidationError{Field: "recall.p95_latency", Message: "must be positive"}
	}
	if c.RecallPerformance.P99Latency <= 0 {
		return &ValidationError{Field: "recall.p99_latency", Message: "must be positive"}
	}
	if c.RecallPerformance.Timeout < c.RecallPerformance.P99Latency {
		return &ValidationError{Field: "recall.timeout", Message: "must be >= p99_latency"}
	}

	// Validate retain performance
	if c.RetainPerformance.MaxLatency <= 0 {
		return &ValidationError{Field: "retain.max_latency", Message: "must be positive"}
	}
	if c.RetainPerformance.Timeout < c.RetainPerformance.MaxLatency {
		return &ValidationError{Field: "retain.timeout", Message: "must be >= max_latency"}
	}
	if c.RetainPerformance.BatchSize <= 0 {
		return &ValidationError{Field: "retain.batch_size", Message: "must be positive"}
	}

	// Validate reflect performance
	if c.ReflectPerformance.MaxLatency <= 0 {
		return &ValidationError{Field: "reflect.max_latency", Message: "must be positive"}
	}
	if c.ReflectPerformance.Timeout < c.ReflectPerformance.MaxLatency {
		return &ValidationError{Field: "reflect.timeout", Message: "must be >= max_latency"}
	}
	if c.ReflectPerformance.MaxInsights <= 0 {
		return &ValidationError{Field: "reflect.max_insights", Message: "must be positive"}
	}

	// Validate throughput
	if c.Throughput.MinOpsPerSecond <= 0 {
		return &ValidationError{Field: "throughput.min_ops_per_second", Message: "must be positive"}
	}
	if c.Throughput.BurstSize < c.Throughput.MinOpsPerSecond {
		return &ValidationError{Field: "throughput.burst_size", Message: "must be >= min_ops_per_second"}
	}

	// Validate startup
	if c.Startup.MaxStartupTime <= 0 {
		return &ValidationError{Field: "startup.max_startup_time", Message: "must be positive"}
	}

	// Validate monitoring
	if c.Monitoring.SamplingRate < 0 || c.Monitoring.SamplingRate > 1 {
		return &ValidationError{Field: "monitoring.sampling_rate", Message: "must be between 0 and 1"}
	}

	return nil
}

// CheckRecallLatency checks if recall latency meets requirements.
func (c *PerformanceConfig) CheckRecallLatency(latency time.Duration, cached bool) error {
	if !c.Enabled {
		return nil
	}

	threshold := c.RecallPerformance.P95Latency
	if cached {
		threshold = c.RecallPerformance.CachedEmbeddingP95
	}

	if latency > threshold {
		return fmt.Errorf("recall latency %v exceeds requirement %v (cached=%v)",
			latency, threshold, cached)
	}

	return nil
}

// CheckRetainLatency checks if retain latency meets requirements.
func (c *PerformanceConfig) CheckRetainLatency(latency time.Duration) error {
	if !c.Enabled {
		return nil
	}

	if latency > c.RetainPerformance.MaxLatency {
		return fmt.Errorf("retain latency %v exceeds requirement %v",
			latency, c.RetainPerformance.MaxLatency)
	}

	return nil
}

// CheckReflectLatency checks if reflect latency meets requirements.
func (c *PerformanceConfig) CheckReflectLatency(latency time.Duration, insightCount int) error {
	if !c.Enabled {
		return nil
	}

	// Calculate expected latency based on insight count
	expectedMax := c.ReflectPerformance.MaxLatency
	if insightCount > 0 {
		calculatedMax := time.Duration(insightCount) * c.ReflectPerformance.PerInsightLatency
		if calculatedMax > expectedMax {
			expectedMax = calculatedMax
		}
	}

	if latency > expectedMax {
		return fmt.Errorf("reflect latency %v exceeds requirement %v (insights=%d)",
			latency, expectedMax, insightCount)
	}

	return nil
}

// CheckThroughput checks if throughput meets requirements.
func (c *PerformanceConfig) CheckThroughput(opsPerSecond float64) error {
	if !c.Enabled {
		return nil
	}

	minThroughput := float64(c.Throughput.MinOpsPerSecond)
	if opsPerSecond < minThroughput*c.Monitoring.AlertThresholds.ThroughputDropThreshold {
		return fmt.Errorf("throughput %.2f ops/sec below requirement %.2f ops/sec",
			opsPerSecond, minThroughput)
	}

	return nil
}

// CheckStartupTime checks if startup time meets requirements.
func (c *PerformanceConfig) CheckStartupTime(startupTime time.Duration) error {
	if !c.Enabled {
		return nil
	}

	if startupTime > c.Startup.MaxStartupTime {
		return fmt.Errorf("startup time %v exceeds requirement %v",
			startupTime, c.Startup.MaxStartupTime)
	}

	return nil
}

// GetRecallTimeout returns the appropriate timeout for recall operations.
func (c *PerformanceConfig) GetRecallTimeout() time.Duration {
	if c == nil || !c.Enabled {
		return 2 * time.Second
	}
	return c.RecallPerformance.Timeout
}

// GetRetainTimeout returns the appropriate timeout for retain operations.
func (c *PerformanceConfig) GetRetainTimeout() time.Duration {
	if c == nil || !c.Enabled {
		return 10 * time.Second
	}
	return c.RetainPerformance.Timeout
}

// GetReflectTimeout returns the appropriate timeout for reflect operations.
func (c *PerformanceConfig) GetReflectTimeout() time.Duration {
	if c == nil || !c.Enabled {
		return 30 * time.Second
	}
	return c.ReflectPerformance.Timeout
}

// IntelligenceConfig combines all intelligence-related configurations.
type IntelligenceConfig struct {
	// Performance defines performance requirements and monitoring
	Performance PerformanceConfig `yaml:"performance"`

	// Hindsight configures the Hindsight memory service
	Hindsight HindsightConfig `yaml:"hindsight"`

	// PatternReview configures the pattern review workflow
	PatternReview PatternReviewConfig `yaml:"pattern_review"`
}

// DefaultIntelligenceConfig returns the default intelligence configuration.
func DefaultIntelligenceConfig() *IntelligenceConfig {
	return &IntelligenceConfig{
		Performance:   *DefaultPerformanceConfig(),
		Hindsight:     *DefaultHindsightConfig(),
		PatternReview: DefaultPatternReviewConfig(),
	}
}

// Validate validates the intelligence configuration.
func (c *IntelligenceConfig) Validate() error {
	if err := c.Performance.Validate(); err != nil {
		return fmt.Errorf("performance config: %w", err)
	}
	if err := c.Hindsight.Validate(); err != nil {
		return fmt.Errorf("hindsight config: %w", err)
	}
	return nil
}

// PatternReviewConfig contains configuration for the pattern review workflow.
// This is defined here for integration with intelligence config.
type PatternReviewConfig struct {
	Enabled              bool          `yaml:"enabled"`
	AutoApproveThreshold float64       `yaml:"auto_approve_threshold"`
	AutoDiscardThreshold float64       `yaml:"auto_discard_threshold"`
	RequireHumanReview   bool          `yaml:"require_human_review"`
	MaxPatternsInReview  int           `yaml:"max_patterns_in_review"`
	DuplicateThreshold   float64       `yaml:"duplicate_threshold"`
	ReviewTimeout        time.Duration `yaml:"review_timeout"`
}

// DefaultPatternReviewConfig returns the default pattern review configuration.
func DefaultPatternReviewConfig() PatternReviewConfig {
	return PatternReviewConfig{
		Enabled:              true,
		AutoApproveThreshold: 0.75,
		AutoDiscardThreshold: 0.40,
		RequireHumanReview:   false,
		MaxPatternsInReview:  100,
		DuplicateThreshold:   0.95,
		ReviewTimeout:        24 * time.Hour,
	}
}
