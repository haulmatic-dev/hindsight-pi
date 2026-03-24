package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateUtilityScore(t *testing.T) {
	now := time.Now()

	t.Run("calculate utility score with default weights", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights: DefaultUtilityWeights(),
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "insight_1",
			Content:        "Test insight",
			Depth:          Level3Pattern,
			AccessCount:    10,
			LastAccessed:   now,
			CreatedAt:      now.Add(-24 * time.Hour),
			RelevanceScore: 0.8,
		}

		score, err := service.CalculateUtilityScore(insight)

		assert.NoError(t, err)
		assert.NotNil(t, score)
		assert.Equal(t, "insight_1", score.InsightID)
		assert.Greater(t, score.Score, 0.0)
		assert.LessOrEqual(t, score.Score, 1.0)
	})

	t.Run("calculate with zero access count", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights: DefaultUtilityWeights(),
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "insight_2",
			AccessCount:    0,
			LastAccessed:   now.Add(-30 * 24 * time.Hour),
			CreatedAt:      now.Add(-60 * 24 * time.Hour),
			RelevanceScore: 0.5,
		}

		score, err := service.CalculateUtilityScore(insight)

		assert.NoError(t, err)
		assert.NotNil(t, score)
		assert.Equal(t, 0.0, score.Frequency)
	})

	t.Run("calculate with high relevance", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights: UtilityWeights{Alpha: 0.2, Beta: 0.3, Gamma: 0.5},
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "insight_3",
			AccessCount:    5,
			LastAccessed:   now,
			CreatedAt:      now.Add(-7 * 24 * time.Hour),
			RelevanceScore: 1.0,
		}

		score, err := service.CalculateUtilityScore(insight)

		assert.NoError(t, err)
		assert.Greater(t, score.Relevance, 0.0)
	})
}

func TestGetRetentionPeriod(t *testing.T) {
	t.Run("get retention period for mental model", func(t *testing.T) {
		policy := RetentionPolicy{
			DepthPeriods: map[InsightDepth]RetentionPeriod{
				Level5MentalModel: RetentionForever,
			},
		}
		service := NewRetentionService(policy)

		period, err := service.GetRetentionPeriod(Level5MentalModel)

		assert.NoError(t, err)
		assert.Equal(t, RetentionForever, period)
	})

	t.Run("get retention period for pattern level", func(t *testing.T) {
		policy := RetentionPolicy{
			DepthPeriods: map[InsightDepth]RetentionPeriod{
				Level3Pattern: Retention30Days,
			},
		}
		service := NewRetentionService(policy)

		period, err := service.GetRetentionPeriod(Level3Pattern)

		assert.NoError(t, err)
		assert.Equal(t, Retention30Days, period)
	})

	t.Run("get retention period not found", func(t *testing.T) {
		policy := RetentionPolicy{
			DepthPeriods: map[InsightDepth]RetentionPeriod{},
		}
		service := NewRetentionService(policy)

		period, err := service.GetRetentionPeriod(Level2Contextual)

		assert.Error(t, err)
		assert.Equal(t, RetentionPeriod(0), period)
	})
}

func TestShouldRetain(t *testing.T) {
	now := time.Now()

	t.Run("should retain high utility insight", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights:             DefaultUtilityWeights(),
			MinUtilityThreshold: 0.3,
			DepthPeriods: map[InsightDepth]RetentionPeriod{
				Level3Pattern: Retention30Days,
			},
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "high_utility",
			AccessCount:    20,
			LastAccessed:   now,
			CreatedAt:      now.Add(-7 * 24 * time.Hour),
			RelevanceScore: 0.9,
			Depth:          Level3Pattern,
		}

		shouldRetain, err := service.ShouldRetain(insight)

		assert.NoError(t, err)
		assert.True(t, shouldRetain)
	})

	t.Run("should not retain low utility insight", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights:             DefaultUtilityWeights(),
			MinUtilityThreshold: 0.7,
			DepthPeriods: map[InsightDepth]RetentionPeriod{
				Level2Contextual: Retention7Days,
			},
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "low_utility",
			AccessCount:    0,
			LastAccessed:   now.Add(-30 * 24 * time.Hour),
			CreatedAt:      now.Add(-60 * 24 * time.Hour),
			RelevanceScore: 0.1,
			Depth:          Level2Contextual,
		}

		shouldRetain, err := service.ShouldRetain(insight)

		assert.NoError(t, err)
		assert.False(t, shouldRetain)
	})

	t.Run("should retain mental model forever", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights:             DefaultUtilityWeights(),
			MinUtilityThreshold: 0.9,
			DepthPeriods: map[InsightDepth]RetentionPeriod{
				Level5MentalModel: RetentionForever,
			},
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "mental_model",
			AccessCount:    1,
			LastAccessed:   now.Add(-365 * 24 * time.Hour),
			CreatedAt:      now.Add(-730 * 24 * time.Hour),
			RelevanceScore: 0.5,
			Depth:          Level5MentalModel,
		}

		shouldRetain, err := service.ShouldRetain(insight)

		assert.NoError(t, err)
		assert.True(t, shouldRetain)
	})
}

func TestGetInsightDepthRetentionPeriod(t *testing.T) {
	t.Run("mental model retention", func(t *testing.T) {
		period, err := GetInsightDepthRetentionPeriod(Level5MentalModel)

		assert.NoError(t, err)
		assert.Equal(t, RetentionForever, period)
	})

	t.Run("principle level retention", func(t *testing.T) {
		period, err := GetInsightDepthRetentionPeriod(Level4Principle)

		assert.NoError(t, err)
		assert.Equal(t, Retention90Days, period)
	})

	t.Run("pattern level retention", func(t *testing.T) {
		period, err := GetInsightDepthRetentionPeriod(Level3Pattern)

		assert.NoError(t, err)
		assert.Equal(t, Retention30Days, period)
	})

	t.Run("contextual level retention", func(t *testing.T) {
		period, err := GetInsightDepthRetentionPeriod(Level2Contextual)

		assert.NoError(t, err)
		assert.Equal(t, Retention7Days, period)
	})

	t.Run("surface level retention", func(t *testing.T) {
		period, err := GetInsightDepthRetentionPeriod(Level1Surface)

		assert.NoError(t, err)
		assert.Equal(t, Retention1Day, period)
	})

	t.Run("invalid depth level", func(t *testing.T) {
		period, err := GetInsightDepthRetentionPeriod(InsightDepth(99))

		assert.Error(t, err)
		assert.Equal(t, RetentionPeriod(0), period)
	})
}

func TestValidateUtilityWeights(t *testing.T) {
	t.Run("valid weights", func(t *testing.T) {
		weights := UtilityWeights{
			Alpha: 0.333,
			Beta:  0.333,
			Gamma: 0.334,
		}

		err := ValidateUtilityWeights(weights)

		assert.NoError(t, err)
	})

	t.Run("exactly 1.0 weights", func(t *testing.T) {
		weights := UtilityWeights{
			Alpha: 0.4,
			Beta:  0.3,
			Gamma: 0.3,
		}

		err := ValidateUtilityWeights(weights)

		assert.NoError(t, err)
	})

	t.Run("weights too high", func(t *testing.T) {
		weights := UtilityWeights{
			Alpha: 0.5,
			Beta:  0.4,
			Gamma: 0.3,
		}

		err := ValidateUtilityWeights(weights)

		assert.Error(t, err)
	})

	t.Run("weights too low", func(t *testing.T) {
		weights := UtilityWeights{
			Alpha: 0.2,
			Beta:  0.2,
			Gamma: 0.2,
		}

		err := ValidateUtilityWeights(weights)

		assert.Error(t, err)
	})

	t.Run("negative weights", func(t *testing.T) {
		weights := UtilityWeights{
			Alpha: -0.1,
			Beta:  0.6,
			Gamma: 0.5,
		}

		err := ValidateUtilityWeights(weights)

		assert.Error(t, err)
	})
}

func TestNormalizeFrequency(t *testing.T) {
	t.Run("normalize zero frequency", func(t *testing.T) {
		result := NormalizeFrequency(0, 100)

		assert.Equal(t, 0.0, result)
	})

	t.Run("normalize maximum frequency", func(t *testing.T) {
		result := NormalizeFrequency(100, 100)

		assert.Equal(t, 1.0, result)
	})

	t.Run("normalize half frequency", func(t *testing.T) {
		result := NormalizeFrequency(50, 100)

		// Logarithmic scaling: ln(50+1)/ln(100+1) = ln(51)/ln(101) ≈ 0.87
		assert.Greater(t, result, 0.8)
		assert.Less(t, result, 0.9)
	})

	t.Run("normalize with zero max", func(t *testing.T) {
		result := NormalizeFrequency(10, 0)

		assert.Equal(t, 0.0, result)
	})

	t.Run("normalize logarithmic growth", func(t *testing.T) {
		low := NormalizeFrequency(1, 1000)
		high := NormalizeFrequency(100, 1000)

		// Higher frequency should have higher normalized value
		assert.Greater(t, high, low)
	})
}

func TestNormalizeRecency(t *testing.T) {
	now := time.Now()

	t.Run("normalize current time", func(t *testing.T) {
		result := NormalizeRecency(now, now, 30*24*time.Hour)

		assert.Equal(t, 1.0, result)
	})

	t.Run("normalize old time", func(t *testing.T) {
		oldTime := now.Add(-30 * 24 * time.Hour)
		result := NormalizeRecency(oldTime, now, 30*24*time.Hour)

		// With exponential decay (lambda=3), at max age the score is exp(-3) ≈ 0.05
		assert.Less(t, result, 0.1)
	})

	t.Run("normalize half age", func(t *testing.T) {
		halfAge := now.Add(-15 * 24 * time.Hour)
		result := NormalizeRecency(halfAge, now, 30*24*time.Hour)

		// With exponential decay at half max age: exp(-1.5) ≈ 0.22
		assert.Greater(t, result, 0.15)
		assert.Less(t, result, 0.30)
	})

	t.Run("normalize with zero max age", func(t *testing.T) {
		result := NormalizeRecency(now, now, 0)

		assert.Equal(t, 1.0, result)
	})

	t.Run("normalize future time", func(t *testing.T) {
		futureTime := now.Add(24 * time.Hour)
		result := NormalizeRecency(futureTime, now, 30*24*time.Hour)

		// Future time should be clamped to 1.0
		assert.Equal(t, 1.0, result)
	})

	t.Run("normalize exponential decay", func(t *testing.T) {
		recent := now.Add(-1 * 24 * time.Hour)
		old := now.Add(-20 * 24 * time.Hour)

		recentScore := NormalizeRecency(recent, now, 30*24*time.Hour)
		oldScore := NormalizeRecency(old, now, 30*24*time.Hour)

		// Recent should have higher score
		assert.Greater(t, recentScore, oldScore)
		// Recent should be close to 1.0
		assert.Greater(t, recentScore, 0.9)
		// Old should be significantly lower
		assert.Less(t, oldScore, 0.5)
	})
}

func TestNewRetentionService(t *testing.T) {
	t.Run("create service with default policy", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights:      DefaultUtilityWeights(),
			DepthPeriods: make(map[InsightDepth]RetentionPeriod),
		}

		service := NewRetentionService(policy)

		assert.NotNil(t, service)
	})
}

func TestInsightDepthConstants(t *testing.T) {
	t.Run("verify depth levels", func(t *testing.T) {
		assert.Equal(t, InsightDepth(1), Level1Surface)
		assert.Equal(t, InsightDepth(2), Level2Contextual)
		assert.Equal(t, InsightDepth(3), Level3Pattern)
		assert.Equal(t, InsightDepth(4), Level4Principle)
		assert.Equal(t, InsightDepth(5), Level5MentalModel)
	})
}

func TestRetentionPeriodConstants(t *testing.T) {
	t.Run("verify retention periods", func(t *testing.T) {
		assert.Equal(t, 24*time.Hour, time.Duration(Retention1Day))
		assert.Equal(t, 7*24*time.Hour, time.Duration(Retention7Days))
		assert.Equal(t, 30*24*time.Hour, time.Duration(Retention30Days))
		assert.Equal(t, 90*24*time.Hour, time.Duration(Retention90Days))
		assert.Equal(t, time.Duration(-1), time.Duration(RetentionForever))
	})
}

func TestUtilityScoreCalculationMath(t *testing.T) {
	now := time.Now()

	t.Run("verify utility formula U = α·frequency + β·recency + γ·relevance", func(t *testing.T) {
		// Use simple weights that sum to 1.0
		weights := UtilityWeights{Alpha: 0.4, Beta: 0.3, Gamma: 0.3}
		policy := RetentionPolicy{Weights: weights}
		service := NewRetentionService(policy)

		// Create insight with known values
		// AccessCount: 10 out of max 100 = normalized ~0.5
		// LastAccessed: now (current) = normalized 1.0
		// RelevanceScore: 0.8
		insight := Insight{
			ID:             "math_test",
			AccessCount:    10,
			LastAccessed:   now,
			CreatedAt:      now.Add(-24 * time.Hour),
			RelevanceScore: 0.8,
			Depth:          Level3Pattern,
		}

		score, err := service.CalculateUtilityScore(insight)

		assert.NoError(t, err)
		// Manual calculation:
		// frequency = log(10+1)/log(100+1) = log(11)/log(101) ≈ 0.52
		// recency = 1.0 (accessed now)
		// relevance = 0.8
		// U = 0.4*0.52 + 0.3*1.0 + 0.3*0.8 = 0.208 + 0.3 + 0.24 = 0.748
		assert.Greater(t, score.Score, 0.7)
		assert.Less(t, score.Score, 0.8)
	})
}

func TestEdgeCases(t *testing.T) {
	now := time.Now()

	t.Run("insight with nil metadata", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights:             DefaultUtilityWeights(),
			MinUtilityThreshold: 0.0,
			DepthPeriods: map[InsightDepth]RetentionPeriod{
				Level1Surface: Retention1Day,
			},
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "nil_metadata",
			AccessCount:    5,
			LastAccessed:   now,
			CreatedAt:      now,
			RelevanceScore: 0.5,
			Depth:          Level1Surface,
			Metadata:       nil,
		}

		score, err := service.CalculateUtilityScore(insight)

		assert.NoError(t, err)
		assert.NotNil(t, score)
	})

	t.Run("very old insight", func(t *testing.T) {
		policy := RetentionPolicy{
			Weights:             DefaultUtilityWeights(),
			MinUtilityThreshold: 0.0,
		}
		service := NewRetentionService(policy)

		insight := Insight{
			ID:             "very_old",
			AccessCount:    100,
			LastAccessed:   now.Add(-365 * 24 * time.Hour),
			CreatedAt:      now.Add(-730 * 24 * time.Hour),
			RelevanceScore: 1.0,
			Depth:          Level3Pattern,
		}

		score, err := service.CalculateUtilityScore(insight)

		assert.NoError(t, err)
		// Recency should be very low due to old last access
		assert.Less(t, score.Recency, 0.1)
	})

	t.Run("weights sum with floating point tolerance", func(t *testing.T) {
		// Weights that sum to 0.9999999999 (very close to 1.0)
		weights := UtilityWeights{
			Alpha: 0.3333333333,
			Beta:  0.3333333333,
			Gamma: 0.3333333333,
		}

		err := ValidateUtilityWeights(weights)

		assert.NoError(t, err)
	})
}

func BenchmarkNormalizeFrequency(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NormalizeFrequency(i%100, 1000)
	}
}

func BenchmarkNormalizeRecency(b *testing.B) {
	now := time.Now()
	for i := 0; i < b.N; i++ {
		NormalizeRecency(now.Add(-time.Duration(i%30)*24*time.Hour), now, 90*24*time.Hour)
	}
}

func BenchmarkCalculateUtilityScore(b *testing.B) {
	now := time.Now()
	policy := RetentionPolicy{
		Weights:      DefaultUtilityWeights(),
		DepthPeriods: make(map[InsightDepth]RetentionPeriod),
	}
	service := NewRetentionService(policy)
	insight := Insight{
		ID:             "benchmark",
		AccessCount:    50,
		LastAccessed:   now,
		CreatedAt:      now.Add(-30 * 24 * time.Hour),
		RelevanceScore: 0.75,
		Depth:          Level3Pattern,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.CalculateUtilityScore(insight)
	}
}
