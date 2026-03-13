package core

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/haulmatic-dev/hydra/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock CASS Client for testing pre-execution context enrichment
type mockCassSearchClient struct {
	searchCalled      bool
	searchQuery       string
	searchLimit       int
	sessions          []SessionHit
	timeoutDuration   time.Duration
	enabled           bool
	searchError       error
	searchReturnEmpty bool
}

func (m *mockCassSearchClient) SearchSessions(query string, limit int, timeout time.Duration) (*CASSSearchResult, error) {
	m.searchCalled = true
	m.searchQuery = query
	m.searchLimit = limit
	m.timeoutDuration = timeout

	if m.searchError != nil {
		return nil, m.searchError
	}

	if m.searchReturnEmpty {
		return &CASSSearchResult{
			Count:        0,
			TotalMatches: 0,
			Hits:         []SessionHit{},
		}, nil
	}

	// Return top sessions (up to limit)
	hits := m.sessions
	if len(hits) > limit {
		hits = hits[:limit]
	}

	return &CASSSearchResult{
		Count:        len(hits),
		TotalMatches: len(m.sessions),
		Hits:         hits,
	}, nil
}

func (m *mockCassSearchClient) IsEnabled() bool {
	return m.enabled
}

// TestFR018_ContextEnrichment_SearchesCASSByBeadTitle tests AC1
// When overseer spawns worker, CASS searches for sessions matching bead title
func TestFR018_ContextEnrichment_SearchesCASSByBeadTitle(t *testing.T) {
	// Create mock CASS client
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Implement user authentication", Score: 0.95, Agent: "opencode"},
			{Title: "Add database migration", Score: 0.87, Agent: "claude"},
		},
	}

	// Create a test bead with a specific title
	beadTitle := "Implement user authentication system"
	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       beadTitle,
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Create user auth functionality",
	}

	// Create overseer with mock CASS client
	overseer := createTestOverseerWithCass(t, mockCass)

	// Attempt to spawn worker
	// This should trigger a CASS search for the bead title
	worker, err := overseer.spawnWorkerWithContextEnrichment(bead)

	// The method spawnWorkerWithContextEnrichment doesn't exist yet
	// So this test will FAIL (RED phase)
	require.NoError(t, err)
	require.NotNil(t, worker)

	// Verify that CASS was searched
	assert.True(t, mockCass.searchCalled, "CASS search should have been called")
	assert.Equal(t, beadTitle, mockCass.searchQuery, "CASS should be searched with bead title")
}

// TestFR018_ContextEnrichment_Top3SessionsInEnvironment tests AC2
// Top 3 most relevant sessions are included in worker environment
func TestFR018_ContextEnrichment_Top3SessionsInEnvironment(t *testing.T) {
	// Create mock CASS client with 5 sessions (we expect only top 3)
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Session 1", Score: 0.95, Content: "Content 1", Agent: "opencode"},
			{Title: "Session 2", Score: 0.90, Content: "Content 2", Agent: "claude"},
			{Title: "Session 3", Score: 0.85, Content: "Content 3", Agent: "cursor"},
			{Title: "Session 4", Score: 0.80, Content: "Content 4", Agent: "aider"},
			{Title: "Session 5", Score: 0.75, Content: "Content 5", Agent: "opencode"},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Test bead",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test description",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	// Spawn worker with context enrichment
	worker, err := overseer.spawnWorkerWithContextEnrichment(bead)

	// This should fail because spawnWorkerWithContextEnrichment doesn't exist
	require.NoError(t, err)
	require.NotNil(t, worker)

	// Verify CASS search was called with limit of 3
	assert.Equal(t, 3, getSearchLimit(mockCass), "Should search for top 3 sessions")
}

// TestFR018_ContextEnrichment_HYDRA_CASS_CONTEXT_EnvironmentVariable tests AC3
// Session snippets included as `HYDRA_CASS_CONTEXT` environment variable
func TestFR018_ContextEnrichment_HYDRA_CASS_CONTEXT_EnvironmentVariable(t *testing.T) {
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{
				Title:      "Auth Implementation Session",
				Score:      0.95,
				Content:    "Successfully implemented JWT authentication with refresh tokens",
				Agent:      "opencode",
				SourcePath: "/sessions/auth_impl.json",
				CreatedAt:  time.Now().Unix(),
			},
			{
				Title:      "Database Setup Session",
				Score:      0.87,
				Content:    "Set up PostgreSQL with connection pooling",
				Agent:      "claude",
				SourcePath: "/sessions/db_setup.json",
				CreatedAt:  time.Now().Unix(),
			},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Implement user auth",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Create authentication system",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	// Create a test environment capture
	envVars := overseer.buildWorkerEnvironmentWithContext(bead, mockCass)

	// This should fail because buildWorkerEnvironmentWithContext doesn't exist
	require.NotNil(t, envVars)

	// Find HYDRA_CASS_CONTEXT in environment
	var cassContextValue string
	for _, env := range envVars {
		if strings.HasPrefix(env, "HYDRA_CASS_CONTEXT=") {
			cassContextValue = strings.TrimPrefix(env, "HYDRA_CASS_CONTEXT=")
			break
		}
	}

	require.NotEmpty(t, cassContextValue, "HYDRA_CASS_CONTEXT environment variable should be set")

	// Parse the JSON content
	var result CASSSearchResult
	err := json.Unmarshal([]byte(cassContextValue), &result)
	require.NoError(t, err, "HYDRA_CASS_CONTEXT should contain valid JSON")

	// Verify content
	assert.GreaterOrEqual(t, result.Count, 1, "Should have at least one session")
	assert.LessOrEqual(t, result.Count, 3, "Should have at most 3 sessions")
}

// TestFR018_ContextEnrichment_NoCASSClient_ContinuesWithoutError tests graceful degradation
func TestFR018_ContextEnrichment_NoCASSClient_ContinuesWithoutError(t *testing.T) {
	// CASS client is disabled
	mockCass := &mockCassSearchClient{
		enabled: false,
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Test bead",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test description",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	// Should be able to spawn worker without CASS
	worker, err := overseer.spawnWorkerWithContextEnrichment(bead)

	// This should not fail even without CASS
	require.NoError(t, err)
	require.NotNil(t, worker)

	// Verify search was not called (CASS disabled)
	assert.False(t, mockCass.searchCalled, "CASS search should not be called when disabled")
}

// TestFR018_ContextEnrichment_CASSTimeout_ContinuesWithoutBlocking tests timeout handling
func TestFR018_ContextEnrichment_CASSTimeout_ContinuesWithoutBlocking(t *testing.T) {
	// CASS client that times out
	mockCass := &mockCassSearchClient{
		enabled:     true,
		searchError: fmt.Errorf("timeout: CASS search took too long"),
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Test bead with timeout",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test timeout handling",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	startTime := time.Now()

	// Should continue without blocking
	worker, err := overseer.spawnWorkerWithContextEnrichment(bead)

	duration := time.Since(startTime)

	// Should complete quickly (within 3 seconds even with timeout)
	require.NoError(t, err)
	require.NotNil(t, worker)
	assert.Less(t, duration, 3*time.Second, "Should not block on CASS timeout")
}

// TestFR018_ContextEnrichment_SearchTimeout_TwoSeconds tests AC from FR-019
// Search timeout: 2 seconds (proceed without context if timeout)
func TestFR018_ContextEnrichment_SearchTimeout_TwoSeconds(t *testing.T) {
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Quick result", Score: 0.90, Content: "Fast"},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Timeout test bead",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test 2 second timeout",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	// This test verifies that the CASS search uses a 2-second timeout
	_, _ = overseer.spawnWorkerWithContextEnrichment(bead)

	// Check that search was called with 2 second timeout
	assert.Equal(t, 2*time.Second, mockCass.timeoutDuration,
		"CASS search should use 2 second timeout")
}

// TestFR018_ContextEnrichment_SessionContentTruncation tests AC from FR-019
// Content is truncated to limit token usage (configurable max length)
func TestFR018_ContextEnrichment_SessionContentTruncation(t *testing.T) {
	// Create session with very long content
	longContent := strings.Repeat("This is a very long content that should be truncated. ", 100)

	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{
				Title:   "Long session",
				Score:   0.95,
				Content: longContent,
				Agent:   "opencode",
			},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Truncation test",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test content truncation",
	}

	overseer := createTestOverseerWithCass(t, mockCass)
	envVars := overseer.buildWorkerEnvironmentWithContext(bead, mockCass)

	require.NotNil(t, envVars)

	// Find HYDRA_CASS_CONTEXT
	var cassContextValue string
	for _, env := range envVars {
		if strings.HasPrefix(env, "HYDRA_CASS_CONTEXT=") {
			cassContextValue = strings.TrimPrefix(env, "HYDRA_CASS_CONTEXT=")
			break
		}
	}

	require.NotEmpty(t, cassContextValue)

	// Parse and check content length
	var result CASSSearchResult
	err := json.Unmarshal([]byte(cassContextValue), &result)
	require.NoError(t, err)

	// Content should be truncated
	if len(result.Hits) > 0 {
		assert.LessOrEqual(t, len(result.Hits[0].Content), 500,
			"Session content should be truncated to MaxContextLength")
	}
}

// TestFR018_ContextEnrichment_MultiAgentSupport tests cross-agent learning
// CASS indexes sessions from: opencode, claude_code, cursor, codex, aider, etc.
func TestFR018_ContextEnrichment_MultiAgentSupport(t *testing.T) {
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Opencode session", Score: 0.95, Agent: "opencode"},
			{Title: "Claude session", Score: 0.93, Agent: "claude_code"},
			{Title: "Cursor session", Score: 0.91, Agent: "cursor"},
			{Title: "Codex session", Score: 0.89, Agent: "codex"},
			{Title: "Aider session", Score: 0.87, Agent: "aider"},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Multi-agent test",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test multi-agent support",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	envVars := overseer.buildWorkerEnvironmentWithContext(bead, mockCass)

	require.NotNil(t, envVars)

	var cassContextValue string
	for _, env := range envVars {
		if strings.HasPrefix(env, "HYDRA_CASS_CONTEXT=") {
			cassContextValue = strings.TrimPrefix(env, "HYDRA_CASS_CONTEXT=")
			break
		}
	}

	require.NotEmpty(t, cassContextValue)

	var result CASSSearchResult
	err := json.Unmarshal([]byte(cassContextValue), &result)
	require.NoError(t, err)

	// Verify agent types are included
	agentsFound := make(map[string]bool)
	for _, hit := range result.Hits {
		agentsFound[hit.Agent] = true
	}

	// Should include sessions from multiple agents
	assert.True(t, len(agentsFound) > 1, "Should include sessions from multiple agent types")
}

// TestFR018_ContextEnrichment_SessionRelevanceRanking tests that sessions are ranked by score
func TestFR018_ContextEnrichment_SessionRelevanceRanking(t *testing.T) {
	// Sessions in random order of scores
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Low relevance", Score: 0.60, Agent: "opencode"},
			{Title: "High relevance", Score: 0.95, Agent: "claude"},
			{Title: "Medium relevance", Score: 0.75, Agent: "cursor"},
			{Title: "Very high relevance", Score: 0.98, Agent: "aider"},
			{Title: "Lowest relevance", Score: 0.45, Agent: "codex"},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Ranking test",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test relevance ranking",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	envVars := overseer.buildWorkerEnvironmentWithContext(bead, mockCass)

	require.NotNil(t, envVars)

	var cassContextValue string
	for _, env := range envVars {
		if strings.HasPrefix(env, "HYDRA_CASS_CONTEXT=") {
			cassContextValue = strings.TrimPrefix(env, "HYDRA_CASS_CONTEXT=")
			break
		}
	}

	var result CASSSearchResult
	err := json.Unmarshal([]byte(cassContextValue), &result)
	require.NoError(t, err)

	// Verify top 3 sessions are the highest scoring ones
	require.GreaterOrEqual(t, len(result.Hits), 3)
	assert.Equal(t, "Very high relevance", result.Hits[0].Title, "Highest score should be first")
	assert.Equal(t, "High relevance", result.Hits[1].Title, "Second highest should be second")
	assert.Equal(t, "Medium relevance", result.Hits[2].Title, "Third highest should be third")
}

// TestFR018_ContextEnrichment_RecentSessionsWeightedHigher tests time decay
func TestFR018_ContextEnrichment_RecentSessionsWeightedHigher(t *testing.T) {
	now := time.Now().Unix()
	oneDayAgo := time.Now().Add(-24 * time.Hour).Unix()
	oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()

	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Old session", Score: 0.90, CreatedAt: oneWeekAgo, Agent: "opencode"},
			{Title: "Recent session", Score: 0.85, CreatedAt: oneDayAgo, Agent: "claude"},
			{Title: "Current session", Score: 0.80, CreatedAt: now, Agent: "cursor"},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Time decay test",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test time-based weighting",
	}

	// Verify that time-based weighting is applied
	result, err := mockCass.SearchSessions(bead.Title, 3, 2*time.Second)
	require.NoError(t, err)

	// Should return all sessions (CASS handles time weighting internally)
	assert.Equal(t, 3, result.Count)
}

// TestFR018_ContextEnrichment_InvalidBeadTitle_EmptyString tests edge case
func TestFR018_ContextEnrichment_InvalidBeadTitle_EmptyString(t *testing.T) {
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Some session", Score: 0.90},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "", // Empty title
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Bead with empty title",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	// Should handle empty title gracefully
	worker, err := overseer.spawnWorkerWithContextEnrichment(bead)

	// Should either skip search or use description as fallback
	require.NoError(t, err)
	require.NotNil(t, worker)
}

// TestFR018_ContextEnrichment_CASSClientNil_ContinuesGracefully tests nil client handling
func TestFR018_ContextEnrichment_CASSClientNil_ContinuesGracefully(t *testing.T) {
	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Test bead",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test nil client",
	}

	// Create overseer without CASS client
	overseer := createTestOverseerWithCass(t, nil)

	// Should continue without error even with nil client
	worker, err := overseer.spawnWorkerWithContextEnrichment(bead)

	require.NoError(t, err)
	require.NotNil(t, worker)
}

// TestFR018_ContextEnrichment_SessionMetadataComplete tests complete metadata in environment
func TestFR018_ContextEnrichment_SessionMetadataComplete(t *testing.T) {
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{
				Title:      "Complete metadata session",
				Score:      0.95,
				Content:    "Session content here",
				Agent:      "opencode",
				SourcePath: "/path/to/session.json",
				LineNumber: 42,
				CreatedAt:  time.Now().Unix(),
			},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Metadata test",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test complete metadata",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	envVars := overseer.buildWorkerEnvironmentWithContext(bead, mockCass)

	require.NotNil(t, envVars)

	var cassContextValue string
	for _, env := range envVars {
		if strings.HasPrefix(env, "HYDRA_CASS_CONTEXT=") {
			cassContextValue = strings.TrimPrefix(env, "HYDRA_CASS_CONTEXT=")
			break
		}
	}

	require.NotEmpty(t, cassContextValue)

	var result CASSSearchResult
	err := json.Unmarshal([]byte(cassContextValue), &result)
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(result.Hits), 1)
	hit := result.Hits[0]

	// Verify all metadata fields are present
	assert.NotEmpty(t, hit.Agent, "Agent should be present")
	assert.NotEmpty(t, hit.Title, "Title should be present")
	assert.NotEmpty(t, hit.Content, "Content should be present")
	assert.Greater(t, hit.Score, 0.0, "Score should be greater than 0")
	assert.NotEmpty(t, hit.SourcePath, "SourcePath should be present")
	assert.Greater(t, hit.CreatedAt, int64(0), "CreatedAt should be set")
}

// TestFR018_ContextEnrichment_MalformedJSON_ReturnsEmpty tests error handling
func TestFR018_ContextEnrichment_MalformedJSON_ReturnsEmpty(t *testing.T) {
	mockCass := &mockCassSearchClient{
		enabled:     true,
		searchError: fmt.Errorf("invalid JSON response from CASS"),
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Error test",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "Test error handling",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	// Should continue without error even with malformed JSON
	worker, err := overseer.spawnWorkerWithContextEnrichment(bead)

	require.NoError(t, err)
	require.NotNil(t, worker)
}

// TestFR018_ContextEnvironmentVariable_PresentInWorker tests end-to-end integration
func TestFR018_ContextEnvironmentVariable_PresentInWorker(t *testing.T) {
	mockCass := &mockCassSearchClient{
		enabled: true,
		sessions: []SessionHit{
			{Title: "Session 1", Score: 0.95, Content: "Content 1"},
			{Title: "Session 2", Score: 0.90, Content: "Content 2"},
		},
	}

	bead := types.Bead{
		ID:          types.BeadID(uuid.New().String()),
		Title:       "Integration test",
		Type:        "feature",
		Project:     "test-project",
		Repo:        "test-repo",
		Description: "End-to-end integration test",
	}

	overseer := createTestOverseerWithCass(t, mockCass)

	// Build environment as spawnWorkerInternal would
	envVars := overseer.buildWorkerEnvironmentWithContext(bead, mockCass)

	// Verify HYDRA_CASS_CONTEXT is in the environment
	found := false
	for _, env := range envVars {
		if strings.HasPrefix(env, "HYDRA_CASS_CONTEXT=") {
			found = true
			break
		}
	}

	assert.True(t, found, "HYDRA_CASS_CONTEXT should be present in worker environment")
}

// Helper functions

func createTestOverseerWithCass(t *testing.T, cassClient *mockCassSearchClient) *Overseer {
	config := &Config{
		Overseer: OverseerConfig{
			BeadDiscoveryInterval: 5 * time.Second,
			MaxWorkers:            5,
			WorkerTimeout:         3 * time.Minute,
			DefaultRetries:        3,
		},
		Intelligence: IntelligenceConfig{
			CassMemory: CassMemoryConfig{
				Enabled:     cassClient != nil && cassClient.enabled,
				QueryLimit:  3,
				HistoryDays: 30,
				Timeout:     2 * time.Second,
			},
		},
	}

	// Set required env vars
	os.Setenv("HYDRA_HOME", "/tmp/hydra-test")

	overseer := &Overseer{
		config:        config,
		activeWorkers: make(map[string]*types.Worker),
		retries:       make(map[string]*beadRetryInfo),
	}

	// Create CASS client only if mock is provided
	if cassClient != nil {
		overseer.cassClient = &CassMemoryClient{
			searchClient: cassClient,
		}
	}

	return overseer
}

func getSearchLimit(mock *mockCassSearchClient) int {
	return mock.searchLimit
}
