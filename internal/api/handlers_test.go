package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/haulmatic-dev/hydra/internal/bv"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestServer(t *testing.T) (*httptest.Server, *core.Overseer, func()) {
	gin.SetMode(gin.TestMode)

	config := &core.Config{
		API: core.APIConfig{
			Enabled: true,
			Port:    0,
		},
		Overseer: core.OverseerConfig{
			MaxWorkers:            5,
			BeadDiscoveryInterval: 10 * time.Second,
			DefaultRetries:        3,
			WorkerTimeout:         30 * time.Minute,
		},
		MCP: core.MCPConfig{
			Enabled: false,
		},
	}

	overseer := core.NewOverseer(config)
	server := NewServer(config, overseer)
	ts := httptest.NewServer(server.Router())

	return ts, overseer, func() {
		ts.Close()
	}
}

func TestGetGraphMetricsHandler(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/v1/graph/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result bv.GraphMetricsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Verify response structure
	assert.NotEmpty(t, result.DataHash, "data_hash should not be empty")
	assert.False(t, result.Timestamp.IsZero(), "timestamp should not be zero")
	assert.GreaterOrEqual(t, result.TotalBeads, 0, "total_beads should be non-negative")

	// PageRank and Betweenness can be nil or empty when BV is unavailable
	// which is expected graceful degradation
	if result.PageRank != nil {
		assert.IsType(t, map[string]float64{}, result.PageRank)
	}
	if result.Betweenness != nil {
		assert.IsType(t, map[string]float64{}, result.Betweenness)
	}
	if result.CriticalPath != nil {
		assert.IsType(t, []string{}, result.CriticalPath)
	}
}

func TestGetGraphBottlenecksHandler(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/v1/graph/bottlenecks")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result bv.BottlenecksResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Verify response structure
	assert.NotEmpty(t, result.DataHash, "data_hash should not be empty")
	assert.False(t, result.Timestamp.IsZero(), "timestamp should not be zero")
	assert.NotNil(t, result.Bottlenecks, "bottlenecks should not be nil")
	assert.GreaterOrEqual(t, result.Count, 0, "count should be non-negative")
	assert.Equal(t, len(result.Bottlenecks), result.Count, "count should match length of bottlenecks")

	// Validate each bottleneck if any exist
	for _, b := range result.Bottlenecks {
		assert.NotEmpty(t, b.BeadID, "bead_id should not be empty")
		assert.NotEmpty(t, b.Title, "title should not be empty")
		assert.GreaterOrEqual(t, b.BlockedCount, 0, "blocked_count should be non-negative")
		assert.GreaterOrEqual(t, b.DependencyDepth, 0, "dependency_depth should be non-negative")
	}
}

func TestGraphMetricsWithProjectParam(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Test with project parameter
	resp, err := http.Get(ts.URL + "/api/v1/graph/metrics?project=test-project")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result bv.GraphMetricsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "test-project", result.Project)
}

func TestGraphBottlenecksWithProjectParam(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Test with project parameter
	resp, err := http.Get(ts.URL + "/api/v1/graph/bottlenecks?project=test-project")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result bv.BottlenecksResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "test-project", result.Project)
}

func TestSetMaxWorkersHandler_SyncsGovernor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := &core.Config{
		API: core.APIConfig{Enabled: true, Port: 0},
		Overseer: core.OverseerConfig{
			MaxWorkers:            5,
			BeadDiscoveryInterval: 10 * time.Second,
			DefaultRetries:        3,
			WorkerTimeout:         30 * time.Minute,
			ConcurrencyGovernor:   core.GovernorConfig{Enabled: true},
		},
		MCP: core.MCPConfig{Enabled: false},
	}
	overseer := core.NewOverseer(config)
	server := NewServer(config, overseer)
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	// Governor starts at the config-time ceiling.
	require.Equal(t, 5, overseer.GetConcurrencyLimit())

	// Runtime increase must reach the governor, not just the state controller.
	resp, err := http.Post(ts.URL+"/api/v1/overseer/max-workers",
		"application/json", strings.NewReader(`{"count": 8}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 8, overseer.GetMaxWorkers())
	assert.Equal(t, 8, overseer.GetConcurrencyLimit(),
		"governor limit must follow a runtime max-workers increase")

	// Runtime decrease must clamp the governor limit immediately.
	resp2, err := http.Post(ts.URL+"/api/v1/overseer/max-workers",
		"application/json", strings.NewReader(`{"count": 3}`))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, 3, overseer.GetConcurrencyLimit(),
		"governor limit must be clamped by a runtime max-workers decrease")

	// Invalid payload is rejected and must not change the limit.
	resp3, err := http.Post(ts.URL+"/api/v1/overseer/max-workers",
		"application/json", strings.NewReader(`{"count": 0}`))
	require.NoError(t, err)
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp3.StatusCode)
	assert.Equal(t, 3, overseer.GetConcurrencyLimit())
}
