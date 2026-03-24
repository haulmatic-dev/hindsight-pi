package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockPromptVariantStore is a mock implementation of PromptVariantStore
type MockPromptVariantStore struct {
	mock.Mock
}

func (m *MockPromptVariantStore) SaveVariant(ctx context.Context, variant *ABTestVariant) error {
	args := m.Called(ctx, variant)
	return args.Error(0)
}

func (m *MockPromptVariantStore) GetVariant(ctx context.Context, id string) (*ABTestVariant, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ABTestVariant), args.Error(1)
}

func (m *MockPromptVariantStore) GetVariantsByBeadType(ctx context.Context, beadType string) ([]*ABTestVariant, error) {
	args := m.Called(ctx, beadType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ABTestVariant), args.Error(1)
}

func (m *MockPromptVariantStore) UpdateVariant(ctx context.Context, variant *ABTestVariant) error {
	args := m.Called(ctx, variant)
	return args.Error(0)
}

func (m *MockPromptVariantStore) DeleteVariant(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockPromptVariantStore) GetActiveVariant(ctx context.Context, beadType string) (*ABTestVariant, error) {
	args := m.Called(ctx, beadType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ABTestVariant), args.Error(1)
}

func (m *MockPromptVariantStore) PromoteVariant(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// MockABTestResultStore is a mock implementation of ABTestResultStore
type MockABTestResultStore struct {
	mock.Mock
}

func (m *MockABTestResultStore) SaveResult(ctx context.Context, result *ABTestResult) error {
	args := m.Called(ctx, result)
	return args.Error(0)
}

func (m *MockABTestResultStore) GetResultsByVariant(ctx context.Context, variantID string) ([]*ABTestResult, error) {
	args := m.Called(ctx, variantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ABTestResult), args.Error(1)
}

func (m *MockABTestResultStore) GetResultsByBeadType(ctx context.Context, beadType string, limit int) ([]*ABTestResult, error) {
	args := m.Called(ctx, beadType, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ABTestResult), args.Error(1)
}

func (m *MockABTestResultStore) GetMetricsForVariant(ctx context.Context, variantID string) (*Metrics, error) {
	args := m.Called(ctx, variantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Metrics), args.Error(1)
}

func setupTestService() (*ABTestingService, *MockPromptVariantStore, *MockABTestResultStore) {
	mockVariantStore := new(MockPromptVariantStore)
	mockResultStore := new(MockABTestResultStore)
	service := NewABTestingService(mockVariantStore, mockResultStore)
	return service, mockVariantStore, mockResultStore
}

func TestRecordResult(t *testing.T) {
	service, _, mockResultStore := setupTestService()
	ctx := context.Background()

	t.Run("successful result recording", func(t *testing.T) {
		variantID := "variant_123"
		beadID := "bead_456"
		success := true
		durationMs := int64(1000)

		mockResultStore.On("SaveResult", ctx, mock.AnythingOfType("*services.ABTestResult")).Return(nil).Once()

		result, err := service.RecordResult(ctx, variantID, beadID, success, durationMs)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, variantID, result.VariantID)
		assert.Equal(t, beadID, result.BeadID)
		assert.Equal(t, success, result.Success)
		assert.Equal(t, durationMs, result.Duration)
		mockResultStore.AssertExpectations(t)
	})

	t.Run("failed result store save", func(t *testing.T) {
		mockResultStore.On("SaveResult", ctx, mock.AnythingOfType("*services.ABTestResult")).Return(assert.AnError).Once()

		result, err := service.RecordResult(ctx, "variant_123", "bead_456", true, 1000)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockResultStore.AssertExpectations(t)
	})
}

func TestGetVariantMetrics(t *testing.T) {
	service, _, mockResultStore := setupTestService()
	ctx := context.Background()

	t.Run("successful metrics retrieval", func(t *testing.T) {
		variantID := "variant_123"
		expectedMetrics := &Metrics{
			TotalBeads:    50,
			Successes:     45,
			Failures:      5,
			SuccessRate:   0.9,
			AvgDurationMs: 1200,
		}

		mockResultStore.On("GetMetricsForVariant", ctx, variantID).Return(expectedMetrics, nil).Once()

		metrics, err := service.GetVariantMetrics(ctx, variantID)

		assert.NoError(t, err)
		assert.Equal(t, expectedMetrics, metrics)
		mockResultStore.AssertExpectations(t)
	})

	t.Run("metrics not found", func(t *testing.T) {
		mockResultStore.On("GetMetricsForVariant", ctx, "nonexistent").Return(nil, assert.AnError).Once()

		metrics, err := service.GetVariantMetrics(ctx, "nonexistent")

		assert.Error(t, err)
		assert.Nil(t, metrics)
		mockResultStore.AssertExpectations(t)
	})
}

func TestCompareVariants(t *testing.T) {
	service, _, mockResultStore := setupTestService()
	ctx := context.Background()

	t.Run("successful comparison with significant difference", func(t *testing.T) {
		variantAID := "variant_a"
		variantBID := "variant_b"

		metricsA := &Metrics{
			TotalBeads:  50,
			Successes:   45,
			Failures:    5,
			SuccessRate: 0.9,
		}
		metricsB := &Metrics{
			TotalBeads:  50,
			Successes:   35,
			Failures:    15,
			SuccessRate: 0.7,
		}

		mockResultStore.On("GetMetricsForVariant", ctx, variantAID).Return(metricsA, nil).Once()
		mockResultStore.On("GetMetricsForVariant", ctx, variantBID).Return(metricsB, nil).Once()

		result, err := service.CompareVariants(ctx, variantAID, variantBID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, variantAID, result.VariantAID)
		assert.Equal(t, variantBID, result.VariantBID)
		assert.True(t, result.Significant)
		assert.Equal(t, variantAID, result.WinnerID)
		mockResultStore.AssertExpectations(t)
	})

	t.Run("comparison with insufficient data", func(t *testing.T) {
		mockResultStore.On("GetMetricsForVariant", ctx, "variant_a").Return(nil, ErrInsufficientData).Once()

		result, err := service.CompareVariants(ctx, "variant_a", "variant_b")

		assert.Error(t, err)
		assert.Nil(t, result)
		mockResultStore.AssertExpectations(t)
	})
}

func TestDetermineWinner(t *testing.T) {
	service, mockVariantStore, mockResultStore := setupTestService()
	ctx := context.Background()

	t.Run("successful winner determination", func(t *testing.T) {
		beadType := "test_bead"
		variants := []*ABTestVariant{
			{ID: "variant_a", BeadType: beadType, IsControl: true},
			{ID: "variant_b", BeadType: beadType},
		}

		metricsA := &Metrics{TotalBeads: 100, Successes: 90, Failures: 10, SuccessRate: 0.9}
		metricsB := &Metrics{TotalBeads: 100, Successes: 70, Failures: 30, SuccessRate: 0.7}

		mockVariantStore.On("GetVariantsByBeadType", ctx, beadType).Return(variants, nil).Once()
		mockResultStore.On("GetMetricsForVariant", ctx, "variant_a").Return(metricsA, nil).Twice()
		mockResultStore.On("GetMetricsForVariant", ctx, "variant_b").Return(metricsB, nil).Twice()

		winner, result, err := service.DetermineWinner(ctx, beadType)

		assert.NoError(t, err)
		assert.NotNil(t, winner)
		assert.NotNil(t, result)
		assert.Equal(t, "variant_a", winner.ID)
		assert.True(t, result.Significant)
		mockVariantStore.AssertExpectations(t)
		mockResultStore.AssertExpectations(t)
	})

	t.Run("no active test", func(t *testing.T) {
		mockVariantStore.On("GetVariantsByBeadType", ctx, "empty_bead").Return([]*ABTestVariant{}, nil).Once()

		winner, result, err := service.DetermineWinner(ctx, "empty_bead")

		assert.Error(t, err)
		assert.Nil(t, winner)
		assert.Nil(t, result)
		mockVariantStore.AssertExpectations(t)
	})
}

func TestPromoteWinner(t *testing.T) {
	service, mockVariantStore, _ := setupTestService()
	ctx := context.Background()

	t.Run("successful promotion", func(t *testing.T) {
		variantID := "variant_123"

		mockVariantStore.On("PromoteVariant", ctx, variantID).Return(nil).Once()

		err := service.PromoteWinner(ctx, variantID)

		assert.NoError(t, err)
		mockVariantStore.AssertExpectations(t)
	})

	t.Run("promotion failure", func(t *testing.T) {
		mockVariantStore.On("PromoteVariant", ctx, "invalid").Return(assert.AnError).Once()

		err := service.PromoteWinner(ctx, "invalid")

		assert.Error(t, err)
		mockVariantStore.AssertExpectations(t)
	})
}

func TestGetActiveVariant(t *testing.T) {
	service, mockVariantStore, _ := setupTestService()
	ctx := context.Background()

	t.Run("successful retrieval", func(t *testing.T) {
		beadType := "test_bead"
		expectedVariant := &ABTestVariant{
			ID:       "variant_123",
			BeadType: beadType,
			IsWinner: true,
		}

		mockVariantStore.On("GetActiveVariant", ctx, beadType).Return(expectedVariant, nil).Once()

		variant, err := service.GetActiveVariant(ctx, beadType)

		assert.NoError(t, err)
		assert.Equal(t, expectedVariant, variant)
		mockVariantStore.AssertExpectations(t)
	})

	t.Run("no active variant", func(t *testing.T) {
		mockVariantStore.On("GetActiveVariant", ctx, "inactive_bead").Return(nil, ErrVariantNotFound).Once()

		variant, err := service.GetActiveVariant(ctx, "inactive_bead")

		assert.Error(t, err)
		assert.Nil(t, variant)
		mockVariantStore.AssertExpectations(t)
	})
}

func TestGetTestConfiguration(t *testing.T) {
	service, _, _ := setupTestService()
	ctx := context.Background()

	t.Run("existing configuration", func(t *testing.T) {
		beadType := "test_bead"
		expectedConfig := &ABTestConfiguration{
			BeadType:        beadType,
			Enabled:         true,
			MinSampleSize:   30,
			ConfidenceLevel: 0.95,
		}

		service.mu.Lock()
		service.configs[beadType] = expectedConfig
		service.mu.Unlock()

		config, err := service.GetTestConfiguration(ctx, beadType)

		assert.NoError(t, err)
		assert.Equal(t, expectedConfig, config)
	})

	t.Run("nonexistent configuration", func(t *testing.T) {
		config, err := service.GetTestConfiguration(ctx, "unknown_bead")

		assert.Error(t, err)
		assert.Nil(t, config)
	})
}

func TestUpdateTestConfiguration(t *testing.T) {
	service, _, _ := setupTestService()
	ctx := context.Background()

	t.Run("successful update", func(t *testing.T) {
		config := &ABTestConfiguration{
			BeadType:        "test_bead",
			Enabled:         true,
			MinSampleSize:   50,
			ConfidenceLevel: 0.99,
		}

		err := service.UpdateTestConfiguration(ctx, config)

		assert.NoError(t, err)

		// Verify the config was stored
		service.mu.RLock()
		storedConfig := service.configs["test_bead"]
		service.mu.RUnlock()

		assert.Equal(t, config, storedConfig)
	})

	t.Run("update with nil config", func(t *testing.T) {
		err := service.UpdateTestConfiguration(ctx, nil)

		assert.Error(t, err)
	})

	t.Run("update with empty bead type", func(t *testing.T) {
		config := &ABTestConfiguration{
			BeadType: "",
		}

		err := service.UpdateTestConfiguration(ctx, config)

		assert.Error(t, err)
	})
}

func TestListActiveTests(t *testing.T) {
	service, _, _ := setupTestService()
	ctx := context.Background()

	t.Run("list active tests", func(t *testing.T) {
		// Set up test configurations
		service.mu.Lock()
		service.configs["bead_a"] = &ABTestConfiguration{BeadType: "bead_a", Enabled: true}
		service.configs["bead_b"] = &ABTestConfiguration{BeadType: "bead_b", Enabled: true}
		service.configs["bead_c"] = &ABTestConfiguration{BeadType: "bead_c", Enabled: false}
		service.mu.Unlock()

		beadTypes, err := service.ListActiveTests(ctx)

		assert.NoError(t, err)
		assert.Len(t, beadTypes, 2)
		assert.Contains(t, beadTypes, "bead_a")
		assert.Contains(t, beadTypes, "bead_b")
	})

	t.Run("no active tests", func(t *testing.T) {
		// Clear all configs
		service.mu.Lock()
		service.configs = make(map[string]*ABTestConfiguration)
		service.mu.Unlock()

		beadTypes, err := service.ListActiveTests(ctx)

		assert.NoError(t, err)
		assert.Empty(t, beadTypes)
	})
}

func TestGetTestResults(t *testing.T) {
	service, mockVariantStore, mockResultStore := setupTestService()
	ctx := context.Background()

	t.Run("successful retrieval", func(t *testing.T) {
		beadType := "test_bead"
		variants := []*ABTestVariant{
			{ID: "variant_a", BeadType: beadType},
			{ID: "variant_b", BeadType: beadType},
		}

		metricsA := &Metrics{TotalBeads: 50, Successes: 45}
		metricsB := &Metrics{TotalBeads: 50, Successes: 40}

		mockVariantStore.On("GetVariantsByBeadType", ctx, beadType).Return(variants, nil).Once()
		mockResultStore.On("GetMetricsForVariant", ctx, "variant_a").Return(metricsA, nil).Once()
		mockResultStore.On("GetMetricsForVariant", ctx, "variant_b").Return(metricsB, nil).Once()

		results, err := service.GetTestResults(ctx, beadType)

		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, metricsA, results["variant_a"])
		assert.Equal(t, metricsB, results["variant_b"])
		mockVariantStore.AssertExpectations(t)
		mockResultStore.AssertExpectations(t)
	})

	t.Run("no variants found", func(t *testing.T) {
		mockVariantStore.On("GetVariantsByBeadType", ctx, "empty_bead").Return([]*ABTestVariant{}, nil).Once()

		results, err := service.GetTestResults(ctx, "empty_bead")

		assert.NoError(t, err)
		assert.Empty(t, results)
		mockVariantStore.AssertExpectations(t)
	})
}
