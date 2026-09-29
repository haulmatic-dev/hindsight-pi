package api

// FR-033 operator park/unpark/quarantine API tests.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOperatorRouter(t *testing.T, store *registry.Store) (*httptest.Server, *core.Overseer) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	config := &core.Config{
		API:      core.APIConfig{Enabled: true, Port: 0},
		Overseer: core.OverseerConfig{MaxWorkers: 2, BeadDiscoveryInterval: 10 * time.Second, DefaultRetries: 1, WorkerTimeout: 30 * time.Minute},
		MCP:      core.MCPConfig{Enabled: false},
	}
	overseer := core.NewOverseer(config)

	r := gin.New()
	v1 := r.Group("/api/v1")
	RegisterOperatorRoutes(v1, overseer, func() *registry.Store { return store })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, overseer
}

func openOperatorStore(t *testing.T) *registry.Store {
	t.Helper()
	st, err := registry.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	return st
}

func TestParkUnparkRoundTrip(t *testing.T) {
	store := openOperatorStore(t)
	srv, _ := setupOperatorRouter(t, store)

	resp := postJSON(t, srv.URL+"/api/v1/projects/proj-a/park", map[string]string{"reason": "operator hold"})
	require.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()

	info, ok, err := store.ProjectParkInfo("proj-a")
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, info.Parked)
	assert.Equal(t, "operator hold", info.Reason)

	resp = postJSON(t, srv.URL+"/api/v1/projects/proj-a/unpark", map[string]string{"reason": "resolved"})
	require.Equal(t, 200, resp.StatusCode)
	resp.Body.Close()

	info, ok, err = store.ProjectParkInfo("proj-a")
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, info.Parked)
	assert.Equal(t, "resolved", info.Reason)
}

func TestParkRegistryUnavailable503(t *testing.T) {
	srv, _ := setupOperatorRouter(t, nil)
	resp := postJSON(t, srv.URL+"/api/v1/projects/proj-a/park", nil)
	assert.Equal(t, 503, resp.StatusCode)
	resp.Body.Close()
}

func TestClearQuarantineUnknownProject(t *testing.T) {
	srv, _ := setupOperatorRouter(t, openOperatorStore(t))
	resp := postJSON(t, srv.URL+"/api/v1/quarantine/bead-1/clear", map[string]string{"project": "nope"})
	assert.Equal(t, 404, resp.StatusCode)
	resp.Body.Close()
}

func TestClearQuarantineMissingProjectField(t *testing.T) {
	srv, _ := setupOperatorRouter(t, openOperatorStore(t))
	resp := postJSON(t, srv.URL+"/api/v1/quarantine/bead-1/clear", map[string]string{})
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()
}

func TestListQuarantineEmpty(t *testing.T) {
	srv, _ := setupOperatorRouter(t, openOperatorStore(t))
	resp, err := http.Get(srv.URL + "/api/v1/quarantine")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	var out struct {
		Projects []map[string]any `json:"projects"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	// Response shape is valid (an empty config has no projects).
	assert.NotNil(t, out.Projects)
}

func TestIntersectLabels(t *testing.T) {
	got := intersectLabels([]string{"stranded-conflict", "open", "integration-exhausted"}, quarantineLabels)
	assert.ElementsMatch(t, []string{"stranded-conflict", "integration-exhausted"}, got)
	assert.Empty(t, intersectLabels([]string{"open"}, quarantineLabels))
}
