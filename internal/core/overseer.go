package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/agents"
	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/internal/bv"
	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/internal/pi"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// IntelligenceBundle contains context and patterns gathered from intelligence services
type IntelligenceBundle struct {
	Patterns        []Pattern       `json:"patterns"`
	ContextEntries  []ContextEntry  `json:"context_entries"`
	ImpactAnalysis  *ImpactAnalysis `json:"impact_analysis,omitempty"`
	ScopeValidated  bool            `json:"scope_validated"`
	ValidationError string          `json:"validation_error,omitempty"`
	GatheredAt      time.Time       `json:"gathered_at"`
}

// Pattern represents a learned pattern from cass_memory
type Pattern struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Confidence  float64   `json:"confidence"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
}

// ContextEntry represents a relevant historical context entry
type ContextEntry struct {
	ID        string    `json:"id"`
	BeadType  string    `json:"bead_type"`
	Files     []string  `json:"files"`
	Summary   string    `json:"summary"`
	Relevance float64   `json:"relevance"`
	CreatedAt time.Time `json:"created_at"`
}

// ImpactAnalysis represents TLDR impact analysis results
type ImpactAnalysis struct {
	TargetFiles     []string   `json:"target_files"`
	AffectedFiles   []string   `json:"affected_files"`
	CallGraph       *CallGraph `json:"call_graph,omitempty"`
	Complexity      int        `json:"complexity"`
	RiskLevel       string     `json:"risk_level"`
	Recommendations []string   `json:"recommendations"`
}

// CallGraph represents function call relationships
type CallGraph struct {
	Nodes []CallNode `json:"nodes"`
	Edges []CallEdge `json:"edges"`
}

// CallNode represents a function or method in the call graph
type CallNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	File     string `json:"file"`
	NodeType string `json:"node_type"`
}

// CallEdge represents a call relationship between nodes
type CallEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// CassMemoryClient provides access to cass_memory service
// CASSSearchResult represents a search result from CASS memory
type CASSSearchResult struct {
	Count        int          `json:"count"`
	TotalMatches int          `json:"total_matches"`
	Hits         []SessionHit `json:"hits"`
}

// SessionHit represents a single search result from CASS memory
type SessionHit struct {
	Agent      string  `json:"agent"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
	SourcePath string  `json:"source_path"`
	LineNumber int     `json:"line_number"`
	CreatedAt  int64   `json:"created_at"`
}

type CassMemoryClient struct {
	baseURL      string
	httpClient   *http.Client
	queryLimit   int
	searchClient interface {
		SearchSessions(query string, limit int, timeout time.Duration) (*CASSSearchResult, error)
		IsEnabled() bool
	}
}

// NewCassMemoryClient creates a new cass_memory client
func NewCassMemoryClient(baseURL string, timeout time.Duration, queryLimit int) *CassMemoryClient {
	return &CassMemoryClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		queryLimit: queryLimit,
	}
}

// QueryContext queries cass_memory for relevant context
func (c *CassMemoryClient) QueryContext(beadType string, files []string, days int) ([]ContextEntry, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("cass_memory base URL not configured")
	}

	reqBody := map[string]interface{}{
		"bead_type": beadType,
		"files":     files,
		"days":      days,
		"limit":     c.queryLimit,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/context/query", c.baseURL)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cass_memory request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cass_memory returned status %d", resp.StatusCode)
	}

	var result struct {
		Entries []ContextEntry `json:"entries"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode cass_memory response: %w", err)
	}

	return result.Entries, nil
}

// GPTCacheClient provides access to GPTCache service
type GPTCacheClient struct {
	serverURL  string
	httpClient *http.Client
}

// NewGPTCacheClient creates a new GPTCache client
func NewGPTCacheClient(serverURL string, timeout time.Duration) *GPTCacheClient {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &GPTCacheClient{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// IsHealthy checks if the GPTCache service is available
func (c *GPTCacheClient) IsHealthy() bool {
	if c.serverURL == "" {
		return false
	}

	url := fmt.Sprintf("%s/health", c.serverURL)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// Retry configuration constants
const (
	defaultMaxRetries    = 3
	defaultBaseDelay     = 5 * time.Second
	defaultMaxDelay      = 5 * time.Minute
	defaultRetryCooldown = 60 * time.Second // Increased from 10s to prevent retry explosions
)

// Rate limiting constants
const (
	maxWorkersPerWindow = 5                      // Max workers to spawn in time window
	rateLimitWindow     = 10 * time.Second       // Time window for rate limiting
	minSpawnInterval    = 500 * time.Millisecond // Minimum time between individual spawns (0.5s)
)

// beadRetryInfo tracks retry state for a bead
type beadRetryInfo struct {
	count     int
	lastError time.Time
	lastErr   error
}

// calculateBackoff calculates exponential backoff delay with jitter protection
// Formula: baseDelay * 2^attempt, capped at maxDelay
func calculateBackoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	if attempt <= 0 {
		return baseDelay
	}

	// Calculate exponential delay: baseDelay * 2^attempt
	delay := baseDelay * time.Duration(1<<uint(attempt))

	// Cap at maxDelay
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// canSpawnWithinRateLimit checks if we can spawn a worker
// Returns true if minimum interval has passed since last spawn (prevents bursts)
func (o *Overseer) canSpawnWithinRateLimit() bool {
	o.spawnRateMu.Lock()
	defer o.spawnRateMu.Unlock()

	now := time.Now()

	// Clean up old timestamps (keep last 5 minutes for metrics)
	cutoff := now.Add(-5 * time.Minute)
	validTimestamps := make([]time.Time, 0, len(o.spawnTimestamps))
	for _, ts := range o.spawnTimestamps {
		if ts.After(cutoff) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	o.spawnTimestamps = validTimestamps

	// Check minimum interval between spawns (prevents bursts)
	if len(o.spawnTimestamps) > 0 {
		lastSpawn := o.spawnTimestamps[len(o.spawnTimestamps)-1]
		timeSinceLastSpawn := now.Sub(lastSpawn)
		if timeSinceLastSpawn < minSpawnInterval {
			waitTime := minSpawnInterval - timeSinceLastSpawn
			log.Printf("[RateLimit] Blocking spawn: minimum interval not met (last spawn %v ago, need to wait %v)",
				timeSinceLastSpawn.Round(time.Millisecond), waitTime.Round(time.Millisecond))
			return false
		}
	}

	return true
}

// recordWorkerSpawn records a worker spawn timestamp for rate limiting
func (o *Overseer) recordWorkerSpawn() {
	o.spawnRateMu.Lock()
	defer o.spawnRateMu.Unlock()

	o.spawnTimestamps = append(o.spawnTimestamps, time.Now())
	log.Printf("[RateLimit] Spawn recorded: %d/%d workers in last %v",
		len(o.spawnTimestamps), maxWorkersPerWindow, rateLimitWindow)
}

// BeadsClient defines the interface for beads operations
// This allows for mocking in tests
type BeadsClient interface {
	ListBeads(projectPath, repoName string) ([]types.Bead, error)
	GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error)
	CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error
	GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error)
	UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error
}

// WorkerSpawner defines the interface for spawning workers
// This allows for mocking in tests
type WorkerSpawner interface {
	Spawn(bead types.Bead, overseer *Overseer) (*types.Worker, error)
}

// defaultWorkerSpawner is the default implementation that spawns real worker processes
type defaultWorkerSpawner struct{}

// Spawn creates and starts a real worker process for a bead
func (d *defaultWorkerSpawner) Spawn(bead types.Bead, overseer *Overseer) (*types.Worker, error) {
	if err := overseer.spawnWorker(bead); err != nil {
		return nil, err
	}
	// Return the worker from active workers map
	overseer.workerMutex.RLock()
	worker := overseer.activeWorkers[string(bead.ID)]
	overseer.workerMutex.RUnlock()
	return worker, nil
}

// Overseer is the main scheduler daemon
type Overseer struct {
	config                *Config
	projects              []types.Project
	activeWorkers         map[string]*types.Worker
	workerMutex           sync.RWMutex
	running               bool
	paused                bool
	pauseMutex            sync.RWMutex
	beadDiscoveryInterval time.Duration
	maxWorkers            int
	maxWorkersMux         sync.RWMutex
	beadsClient           BeadsClient
	spawner               WorkerSpawner // Interface for spawning workers (allows mocking in tests)
	dag                   *DAG
	dagMu                 sync.RWMutex
	completed             map[string]bool           // Track completed beads for this session
	retries               map[string]*beadRetryInfo // Track retry state for failed beads
	retriesMu             sync.RWMutex              // Mutex for retries map
	baseDelay             time.Duration             // Base delay for exponential backoff
	maxDelay              time.Duration             // Maximum delay for exponential backoff
	cassClient            *CassMemoryClient         // cass_memory client for context retrieval
	tldrClient            *intelligence.TLDRClient  // TLDR client for impact analysis
	gptCache              *GPTCacheClient           // GPTCache client for caching
	bvClient              *bv.Client                // BV client for graph analytics
	intelMutex            sync.RWMutex              // Mutex for intelligence clients
	settledStateSvc       *SettledStateService      // Settled-state detection service

	// MCP (Model Context Protocol) fields
	mcpClient          *mcp.HTTPClient             // MCP HTTP client (Streamable HTTP)
	mcpReservationMgr  *mcp.ReservationManager     // MCP reservation manager
	mcpConfig          *MCPConfig                  // MCP configuration
	activeReservations map[string]*mcp.Reservation // Active file reservations
	mcpMu              sync.RWMutex                // Mutex for MCP operations
	mcpMessageHandler  func(mcp.Message)           // Handler for incoming MCP messages

	// Resource monitoring for adaptive scaling
	resourceMonitor *ResourceMonitor // Monitors CPU/Memory usage

	// Orphan process janitor for cleaning up orphaned opencode processes
	orphanJanitor *Charon // Cleans up orphaned opencode processes

	// Chain completion detector for end-of-chain pattern extraction
	chainCompletionDetector *ChainCompletionDetector // Detects chain completion and triggers pattern extraction
	chainsRegistered        bool                     // Track if chains have been registered (only once at startup)

	// Worker tracker for persistent PID tracking
	workerTracker *WorkerTracker // Tracks worker PIDs to identify Hydra workers

	// Rate limiting: track spawn timestamps to limit workers per time window
	spawnTimestamps []time.Time // Timestamps of recent worker spawns
	spawnRateMu     sync.Mutex  // Mutex for spawn rate tracking

	// Spawn backoff tracking
	lastSpawnFailure time.Time  // Last time a spawn failed (for exponential backoff)
	spawnFailures    int        // Consecutive spawn failures count
	spawnBackoffMu   sync.Mutex // Mutex for spawn backoff state

	// Agent connector for modular agent support
	agentConnector agents.AgentConnector // Agent connector interface
	agentConnMu    sync.RWMutex          // Mutex for agent connector

	// Skill updater for periodic skill updates
	skillUpdater *pi.SkillUpdater // Skill updater for background updates

	// TLDR indexer for deterministic code indexing
	tldrIndexer *TLDRIndexer // Indexes repositories after code changes
}

// NewOverseer creates a new overseer instance
func NewOverseer(config *Config) *Overseer {
	maxRetries := config.Overseer.DefaultRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	o := &Overseer{
		config:                config,
		activeWorkers:         make(map[string]*types.Worker),
		beadDiscoveryInterval: config.Overseer.BeadDiscoveryInterval,
		maxWorkers:            config.Overseer.MaxWorkers,
		beadsClient:           beads.NewClient(),
		bvClient:              bv.NewClient(),
		spawner:               &defaultWorkerSpawner{},
		dag:                   NewDAG(),
		completed:             make(map[string]bool),
		retries:               make(map[string]*beadRetryInfo),
		baseDelay:             defaultBaseDelay,
		maxDelay:              defaultMaxDelay,
	}

	// Initialize intelligence clients if enabled
	o.initializeIntelligenceClients()

	// Initialize MCP client if enabled
	if config.MCP.Enabled {
		if err := o.initializeMCP(); err != nil {
			log.Printf("[MCP] Failed to initialize MCP: %v", err)
		}
	}

	// Initialize resource monitor with config values (0 or unset defaults to 70%)
	maxCPU := 70.0
	maxMemory := 70.0
	if config.Overseer.ResourceMonitoring.MaxCPUPercent > 0 {
		maxCPU = config.Overseer.ResourceMonitoring.MaxCPUPercent
	}
	if config.Overseer.ResourceMonitoring.MaxMemoryPercent > 0 {
		maxMemory = config.Overseer.ResourceMonitoring.MaxMemoryPercent
	}
	o.resourceMonitor = NewResourceMonitor(maxCPU, maxMemory)
	log.Printf("[ResourceMonitor] Initialized with CPU: %.1f%%, Memory: %.1f%% limits (0=unlimited)", maxCPU, maxMemory)

	// Initialize worker tracker for PID tracking
	workerTracker, err := NewWorkerTracker(".")
	if err != nil {
		log.Printf("[Overseer] Warning: failed to initialize worker tracker: %v", err)
		// Continue without tracker - Charon will just have limited visibility
	} else {
		o.workerTracker = workerTracker
		log.Printf("[WorkerTracker] Initialized")
	}

	// Initialize orphan process janitor with worker timeout for stuck worker detection
	workerTimeout := 5 * time.Minute // Default 5 minutes
	if config.Overseer.WorkerTimeout > 0 {
		workerTimeout = config.Overseer.WorkerTimeout
	}
	o.orphanJanitor = NewCharon(o.beadsClient.(*beads.Client), o.workerTracker, workerTimeout)

	// Initialize agent connector (Pi by default)
	piConfig := agents.PiConfig{
		Provider:      config.Agents.Pi.Provider,
		Model:         config.Agents.Pi.Model,
		ThinkingLevel: config.Agents.Pi.ThinkingLevel,
		Timeout:       config.Agents.Pi.Timeout,
		ApiKeyName:    config.Agents.Pi.ApiKeyName,
		ConfigDir:     config.Pi.ConfigDir,
	}
	o.agentConnector = agents.NewPiConnector(piConfig, log.Default())
	log.Printf("[AgentConnector] Initialized Pi connector (provider: %s, model: %s, apiKey: %s)",
		piConfig.Provider, piConfig.Model, piConfig.ApiKeyName)

	// Initialize settled-state detection service if enabled
	if config.Intelligence.SettledState.Enabled {
		o.initializeSettledStateService()
	}

	// Initialize chain completion detector if enabled
	if config.Overseer.ChainDetection.Enabled {
		o.initializeChainCompletionDetector()
	}

	// Initialize skill updater if auto-update is enabled
	if config.Pi.Skills.AutoUpdate {
		if err := o.initializeSkillUpdater(); err != nil {
			log.Printf("[Overseer] Warning: failed to initialize skill updater: %v", err)
		}
	}

	// Override defaults if configured
	if config.Overseer.WorkerTimeout > 0 {
		// WorkerTimeout can be used to derive maxDelay
		o.maxDelay = config.Overseer.WorkerTimeout / 6 // Reasonable fraction
		if o.maxDelay > defaultMaxDelay {
			o.maxDelay = defaultMaxDelay
		}
	}

	return o
}

// initializeSkillUpdater creates and configures the skill updater
func (o *Overseer) initializeSkillUpdater() error {
	configDir := o.config.Pi.ConfigDir
	if configDir == "" {
		configDir = "./pi-config"
	}

	updateInterval := o.config.Pi.Skills.UpdateInterval
	if updateInterval == "" {
		updateInterval = "24h"
	}

	setupManager := pi.NewSetupManager(configDir, configDir)
	skillUpdater, err := pi.NewSkillUpdater(setupManager, updateInterval)
	if err != nil {
		return fmt.Errorf("failed to create skill updater: %w", err)
	}

	o.skillUpdater = skillUpdater
	log.Printf("[SkillUpdater] Initialized with interval: %s", updateInterval)
	return nil
}

// initializeIntelligenceClients creates intelligence service clients based on config
func (o *Overseer) initializeIntelligenceClients() {
	o.intelMutex.Lock()
	defer o.intelMutex.Unlock()

	intellConfig := o.config.Intelligence

	// Initialize cass_memory client if enabled
	if intellConfig.CassMemory.Enabled {
		o.cassClient = NewCassMemoryClient(
			"http://127.0.0.1:5000", // Default cass_memory port
			intellConfig.CassMemory.Timeout,
			intellConfig.CassMemory.QueryLimit,
		)
		log.Printf("[Intelligence] cass_memory client initialized (limit: %d, timeout: %v)",
			intellConfig.CassMemory.QueryLimit, intellConfig.CassMemory.Timeout)
	}

	// Initialize TLDR client if enabled (CLI mode)
	if intellConfig.TLDR.Enabled {
		o.tldrClient = intelligence.NewTLDRClient(
			intellConfig.TLDR.BaseURL,
			true, // useCLI
			"llm-tldr",
			"",
			intellConfig.TLDR.Timeout,
		)
		log.Printf("[Intelligence] TLDR client initialized (baseURL: %s, maxFiles: %d)",
			intellConfig.TLDR.BaseURL, intellConfig.TLDR.MaxFiles)

		// Initialize TLDR indexer if enabled
		if intellConfig.TLDR.Indexing.Enabled {
			o.tldrIndexer = NewTLDRIndexer(
				intellConfig.TLDR.Indexing.MaxConcurrent,
				intellConfig.TLDR.Indexing.RetryAttempts,
				intellConfig.TLDR.Indexing.RetryDelay,
				"llm-tldr",
			)
			log.Printf("[Intelligence] TLDR indexer initialized (maxConcurrent: %d, queueSize: %d)",
				intellConfig.TLDR.Indexing.MaxConcurrent, intellConfig.TLDR.Indexing.QueueSize)
		}
	}

	// Initialize GPTCache client if enabled
	if intellConfig.GPTCache.Enabled {
		o.gptCache = NewGPTCacheClient(
			intellConfig.GPTCache.ServerURL,
			5*time.Second,
		)
		log.Printf("[Intelligence] GPTCache client initialized (server: %s)",
			intellConfig.GPTCache.ServerURL)
	}
}

// initializeSettledStateService creates and configures the settled-state detection service
func (o *Overseer) initializeSettledStateService() {
	config := o.config.Intelligence.SettledState

	// Create the service
	o.settledStateSvc = NewSettledStateService(config, o.beadsClient, o)

	// Set up pattern extractor if enabled
	if o.config.Intelligence.Patterns.Enabled {
		extractorConfig := intelligence.DefaultPatternExtractorConfig()
		extractorConfig.Enabled = o.config.Intelligence.Patterns.Enabled
		extractorConfig.MinConfidence = o.config.Intelligence.Patterns.MinConfidence
		extractorConfig.MaxPatternsPerBead = o.config.Intelligence.Patterns.MaxPatternsPerBead
		extractorConfig.ExtractCodeSnippets = o.config.Intelligence.Patterns.ExtractCodeSnippets

		if o.config.Intelligence.Patterns.StoragePath != "" {
			extractorConfig.StoragePath = o.config.Intelligence.Patterns.StoragePath
		}

		extractor, err := intelligence.NewBeadPatternExtractor(extractorConfig)
		if err != nil {
			log.Printf("[SettledState] Failed to create pattern extractor: %v", err)
		} else {
			o.settledStateSvc.SetPatternExtractor(extractor)
			log.Printf("[SettledState] Pattern extractor initialized (storage: %s)", extractorConfig.StoragePath)
		}
	}

	// Register callback for when beads become settled
	o.settledStateSvc.OnSettled(func(beadID types.BeadID, info *types.SettledBeadInfo) {
		log.Printf("[Overseer] Bead settled: %s (stability: %.2f)", beadID, info.StabilityScore)
	})

	log.Printf("[SettledState] Service initialized (check interval: %v, settlement delay: %v)",
		config.CheckInterval, config.SettlementDelay)
}

// initializeChainCompletionDetector creates and configures the chain completion detector
func (o *Overseer) initializeChainCompletionDetector() {
	config := ChainCompletionConfig{
		Enabled:           o.config.Overseer.ChainDetection.Enabled,
		DetectionInterval: o.config.Overseer.ChainDetection.Interval,
		Timeout:           o.config.Overseer.ChainDetection.Timeout,
	}

	if config.DetectionInterval == 0 {
		config.DetectionInterval = 10 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}

	o.chainCompletionDetector = NewChainCompletionDetector(config, o)

	o.chainCompletionDetector.Start() // Start the polling loop

	extractor := NewChainPatternExtractor(
		o,
		intelligence.NewQualityGateService(),
		nil,
	)

	o.chainCompletionDetector.SetCallback(func(event ChainCompletedEvent) {
		log.Printf("[Overseer] Chain completed: root=%s", event.ChainInfo.RootBeadID)
		if err := extractor.ExtractPatterns(event); err != nil {
			log.Printf("[Overseer] Chain pattern extraction failed: %v", err)
		}
	})

	log.Printf("[ChainCompletion] Detector initialized (interval: %v, timeout: %v)",
		config.DetectionInterval, config.Timeout)
}

// queryCassMemoryContext queries cass_memory for relevant context with error handling
func (o *Overseer) queryCassMemoryContext(client *CassMemoryClient, bead *types.Bead, files []string) ([]ContextEntry, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), o.config.Intelligence.CassMemory.Timeout)
	defer cancel()

	// Run query in goroutine to allow cancellation
	type result struct {
		entries []ContextEntry
		err     error
	}

	resultChan := make(chan result, 1)

	go func() {
		entries, err := client.QueryContext(
			bead.Type,
			files,
			o.config.Intelligence.CassMemory.HistoryDays,
		)
		resultChan <- result{entries: entries, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("cass_memory query timed out: %w", ctx.Err())
	case res := <-resultChan:
		return res.entries, res.err
	}
}

// analyzeImpactWithTLDR performs impact analysis using TLDR service
func (o *Overseer) analyzeImpactWithTLDR(client *intelligence.TLDRClient, bead *types.Bead, files []string) (*ImpactAnalysis, error) {
	// Find project path for the bead
	projectPath := o.findProjectPath(bead)
	if projectPath == "" {
		return nil, fmt.Errorf("could not find project path for bead %s", bead.ID)
	}

	// Set search path for CLI mode
	client.SetSearchPath(projectPath)

	// Analyze first file (TLDR analyzes one file at a time)
	if len(files) == 0 {
		return nil, fmt.Errorf("no files to analyze")
	}

	// Use depth 2 for moderate analysis depth
	resp, err := client.ImpactAnalysis(files[0], 2)
	if err != nil {
		return nil, fmt.Errorf("TLDR impact analysis failed: %w", err)
	}

	// Convert TLDRImpactResponse to ImpactAnalysis
	analysis := &ImpactAnalysis{
		TargetFiles: files,
	}

	// Build call graph from TLDR response
	if len(resp.Impact.Callers) > 0 || len(resp.Impact.Callees) > 0 {
		callGraph := &CallGraph{}

		// Add caller nodes
		for _, caller := range resp.Impact.Callers {
			nodeID := fmt.Sprintf("%s:%s", caller.File, caller.Function)
			callGraph.Nodes = append(callGraph.Nodes, CallNode{
				ID:       nodeID,
				Name:     caller.Function,
				File:     caller.File,
				NodeType: "caller",
			})
		}

		// Add callee nodes
		for _, callee := range resp.Impact.Callees {
			nodeID := fmt.Sprintf("%s:%s", callee.File, callee.Function)
			callGraph.Nodes = append(callGraph.Nodes, CallNode{
				ID:       nodeID,
				Name:     callee.Function,
				File:     callee.File,
				NodeType: "callee",
			})
		}

		analysis.CallGraph = callGraph
	}

	return analysis, nil
}

// extractTargetFiles extracts target file paths from bead metadata and description
func (o *Overseer) extractTargetFiles(bead *types.Bead) []string {
	var files []string
	seen := make(map[string]bool)

	// Check metadata for explicit file list
	if bead.Metadata != nil {
		if fileList, ok := bead.Metadata["files"]; ok {
			for _, file := range strings.Split(fileList, ",") {
				file = strings.TrimSpace(file)
				if file != "" && !seen[file] {
					files = append(files, file)
					seen[file] = true
				}
			}
		}
	}

	// Parse description for file references (e.g., "modify internal/core/config.go")
	// This is a simple heuristic - can be enhanced with more sophisticated parsing
	desc := bead.Description + " " + bead.Title
	words := strings.Fields(desc)

	for _, word := range words {
		// Check for common file patterns
		if strings.Contains(word, "/") || strings.Contains(word, ".go") ||
			strings.Contains(word, ".js") || strings.Contains(word, ".ts") ||
			strings.Contains(word, ".py") || strings.Contains(word, ".java") {
			// Clean up the word (remove punctuation)
			cleaned := strings.Trim(word, ".,;:!?()[]{}\"")
			if cleaned != "" && !seen[cleaned] && len(cleaned) < 200 {
				files = append(files, cleaned)
				seen[cleaned] = true
			}
		}
	}

	return files
}

// findProjectPath finds the project path for a given bead
func (o *Overseer) findProjectPath(bead *types.Bead) string {
	for _, project := range o.projects {
		if project.Name == bead.Project {
			if bead.Repo != "" && project.Type == types.RepoTypeMulti {
				// For multi-repo projects, return the specific repo path
				return filepath.Join(project.Path, bead.Repo)
			}
			return project.Path
		}
	}
	return ""
}

// validateScope validates the scope of changes against configured limits
func (o *Overseer) validateScope(bundle *IntelligenceBundle, targetFiles []string) error {
	config := o.config.Intelligence

	// Validate file count against TLDR max files
	if config.TLDR.Enabled && len(targetFiles) > config.TLDR.MaxFiles {
		return fmt.Errorf("too many target files: %d exceeds limit of %d",
			len(targetFiles), config.TLDR.MaxFiles)
	}

	// Validate impact scope if analysis available
	if bundle.ImpactAnalysis != nil {
		totalFiles := len(targetFiles) + len(bundle.ImpactAnalysis.AffectedFiles)

		if totalFiles > config.TLDR.MaxFiles*2 {
			return fmt.Errorf("impact scope too large: %d total files affected exceeds safe threshold",
				totalFiles)
		}

		// Check complexity score
		if bundle.ImpactAnalysis.Complexity > 100 {
			return fmt.Errorf("complexity score too high: %d (max recommended: 100)",
				bundle.ImpactAnalysis.Complexity)
		}

		// Flag high-risk changes for review
		if bundle.ImpactAnalysis.RiskLevel == "high" {
			log.Printf("[Intelligence] Warning: High risk level detected for change scope")
		}
	}

	return nil
}

// shouldRetry checks if a bead should be retried based on retry count and cooldown
func (o *Overseer) shouldRetry(beadID string) bool {
	o.retriesMu.RLock()
	defer o.retriesMu.RUnlock()

	retryInfo, exists := o.retries[beadID]
	if !exists {
		// No retries yet, allow first attempt
		return true
	}

	maxRetries := o.config.Overseer.DefaultRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	// Check if we've exceeded max retries
	if retryInfo.count >= maxRetries {
		log.Printf("[Retry] Bead %s exhausted all %d retry attempts", beadID, maxRetries)
		return false
	}

	// Check if enough time has passed since last attempt (cooldown + exponential backoff)
	backoff := calculateBackoff(retryInfo.count, o.baseDelay, o.maxDelay)
	minInterval := defaultRetryCooldown + backoff
	elapsed := time.Since(retryInfo.lastError)

	if elapsed < minInterval {
		log.Printf("[Retry] Bead %s in cooldown period (%v remaining)", beadID, minInterval-elapsed)
		return false
	}

	return true
}

// recordRetry records a retry attempt for a bead
func (o *Overseer) recordRetry(beadID string, err error) {
	o.retriesMu.Lock()
	defer o.retriesMu.Unlock()

	if info, exists := o.retries[beadID]; exists {
		info.count++
		info.lastError = time.Now()
		info.lastErr = err
		log.Printf("[Retry] Bead %s retry attempt %d recorded: %v", beadID, info.count, err)
	} else {
		o.retries[beadID] = &beadRetryInfo{
			count:     1,
			lastError: time.Now(),
			lastErr:   err,
		}
		log.Printf("[Retry] Bead %s first retry recorded: %v", beadID, err)
	}
}

// clearRetry removes retry tracking for a bead (called on success)
func (o *Overseer) clearRetry(beadID string) {
	o.retriesMu.Lock()
	defer o.retriesMu.Unlock()

	if _, exists := o.retries[beadID]; exists {
		delete(o.retries, beadID)
		log.Printf("[Retry] Bead %s retry state cleared after successful execution", beadID)
	}
}

// getRetryCount returns the current retry count for a bead
func (o *Overseer) getRetryCount(beadID string) int {
	o.retriesMu.RLock()
	defer o.retriesMu.RUnlock()

	if info, exists := o.retries[beadID]; exists {
		return info.count
	}
	return 0
}

// recordSpawnFailure records a spawn failure and calculates backoff
func (o *Overseer) recordSpawnFailure() time.Duration {
	o.spawnBackoffMu.Lock()
	defer o.spawnBackoffMu.Unlock()

	o.spawnFailures++
	o.lastSpawnFailure = time.Now()

	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, max 60s
	backoff := time.Duration(1<<uint(o.spawnFailures-1)) * time.Second
	if backoff > 60*time.Second {
		backoff = 60 * time.Second
	}

	log.Printf("[Overseer] Spawn failure recorded (%d consecutive). Backoff: %v",
		o.spawnFailures, backoff)

	return backoff
}

// clearSpawnFailures resets the spawn failure count after a successful spawn
func (o *Overseer) clearSpawnFailures() {
	o.spawnBackoffMu.Lock()
	defer o.spawnBackoffMu.Unlock()

	if o.spawnFailures > 0 {
		log.Printf("[Overseer] Spawn failures cleared (was %d)", o.spawnFailures)
		o.spawnFailures = 0
	}
}

// shouldSpawnBackoff returns true if we should wait before spawning more workers
func (o *Overseer) shouldSpawnBackoff() bool {
	o.spawnBackoffMu.Lock()
	defer o.spawnBackoffMu.Unlock()

	if o.spawnFailures == 0 {
		return false
	}

	// Calculate backoff duration
	backoff := time.Duration(1<<uint(o.spawnFailures-1)) * time.Second
	if backoff > 60*time.Second {
		backoff = 60 * time.Second
	}

	elapsed := time.Since(o.lastSpawnFailure)
	shouldWait := elapsed < backoff

	if shouldWait {
		remaining := backoff - elapsed
		log.Printf("[Overseer] Spawn backoff active: %v remaining", remaining)
	}

	return shouldWait
}

// logError logs an error with structured context information
func (o *Overseer) logError(component, operation, beadID string, err error, severity string) {
	retryCount := o.getRetryCount(beadID)

	switch severity {
	case "fatal":
		log.Printf("[FATAL] [%s] %s failed | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	case "error":
		log.Printf("[ERROR] [%s] %s failed | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	case "warn", "warning":
		log.Printf("[WARN] [%s] %s issue | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	default:
		log.Printf("[INFO] [%s] %s | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	}
}

// logGracefulDegradation logs a non-fatal error that the system recovered from
func (o *Overseer) logGracefulDegradation(component, operation, action string, err error) {
	log.Printf("[DEGRADED] [%s] %s failed but continuing | recovery=%s | error=%v",
		component, operation, action, err)
}

// isIntelligenceEnabled checks if any intelligence services are enabled
func (o *Overseer) isIntelligenceEnabled() bool {
	o.intelMutex.RLock()
	defer o.intelMutex.RUnlock()

	return o.config.Intelligence.CassMemory.Enabled ||
		o.config.Intelligence.TLDR.Enabled ||
		o.config.Intelligence.GPTCache.Enabled
}

// Start begins the overseer's main loop
func (o *Overseer) Start(ctx context.Context) error {
	log.Println("🚀 Starting Hydra overseer...")

	// Start orphan process janitor
	if o.orphanJanitor != nil {
		o.orphanJanitor.Start()
		log.Println("🧹 Orphan janitor started")
	}

	// Run Pi ecosystem setup
	if err := o.runPiSetup(); err != nil {
		log.Printf("[Overseer] Pi setup warning: %v", err)
		// Continue even if Pi setup fails - not critical for Hydra operation
	}

	// Discover projects
	if err := o.discoverProjects(); err != nil {
		return fmt.Errorf("failed to discover projects: %w", err)
	}

	log.Printf("📁 Discovered %d projects", len(o.projects))

	// Start settled-state detection service if enabled
	if o.settledStateSvc != nil {
		if err := o.settledStateSvc.Start(ctx); err != nil {
			log.Printf("[SettledState] Failed to start service: %v", err)
		} else {
			log.Println("🔒 Settled-state detection service started")
		}
	}

	// Start skill updater if initialized
	if o.skillUpdater != nil {
		o.skillUpdater.Start()
		log.Println("📚 Skill updater started")
	}

	// Start TLDR indexer if enabled
	if o.tldrIndexer != nil {
		o.tldrIndexer.Start()
		log.Println("📇 TLDR indexer started")

		// Queue initial indexing for all project repos
		o.queueInitialIndexing()
	}

	o.running = true
	ticker := time.NewTicker(o.beadDiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			o.running = false
			// Stop orphan janitor
			if o.orphanJanitor != nil {
				o.orphanJanitor.Stop()
			}
			// Stop settled-state detection service
			if o.settledStateSvc != nil {
				if err := o.settledStateSvc.Stop(); err != nil {
					log.Printf("[SettledState] Error stopping service: %v", err)
				} else {
					log.Println("🔒 Settled-state detection service stopped")
				}
			}
			// Stop skill updater
			if o.skillUpdater != nil {
				o.skillUpdater.Stop()
				log.Println("📚 Skill updater stopped")
			}
			// Stop TLDR indexer
			if o.tldrIndexer != nil {
				o.tldrIndexer.Stop()
				log.Println("📇 TLDR indexer stopped")
			}
			return nil
		case <-ticker.C:
			if err := o.tick(); err != nil {
				log.Printf("Error during tick: %v", err)
			}
		}
	}
}

// queueInitialIndexing queues all project repositories for initial TLDR indexing
func (o *Overseer) queueInitialIndexing() {
	if o.tldrIndexer == nil {
		return
	}

	log.Printf("[TLDRIndexer] Queueing initial indexing for %d projects", len(o.projects))

	for _, project := range o.projects {
		if project.Type == "multi" {
			// Multi-repo project: index each repo
			for _, repo := range project.Repos {
				if o.tldrIndexer.QueueIndex(repo.Path) {
					log.Printf("[TLDRIndexer] Queued for indexing: %s/%s", project.Name, repo.Name)
				}
			}
		} else {
			// Single repo project: index project path
			if o.tldrIndexer.QueueIndex(project.Path) {
				log.Printf("[TLDRIndexer] Queued for indexing: %s", project.Name)
			}
		}
	}
}

// queueRepoIndexing queues a repository for TLDR indexing after bead completion
func (o *Overseer) queueRepoIndexing(projectName, repoPath string) {
	if o.tldrIndexer == nil {
		return
	}

	if o.tldrIndexer.QueueIndex(repoPath) {
		log.Printf("[TLDRIndexer] Queued for indexing: %s (%s)", projectName, repoPath)
	}
}

// discoverProjects finds all projects in ~/source-code/
func (o *Overseer) discoverProjects() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	rootPath := filepath.Join(homeDir, "source-code")

	// If projects are configured, use those
	if len(o.config.Projects) > 0 {
		for _, pc := range o.config.Projects {
			project := types.Project{
				Name: pc.Name,
				Path: pc.Path,
				Type: types.RepoType(pc.Type),
			}

			if pc.Type == "multi" {
				for _, rc := range pc.Repos {
					project.Repos = append(project.Repos, types.Repo{
						Name: rc.Name,
						Path: rc.Path,
					})
				}
			}

			o.projects = append(o.projects, project)
		}
		return nil
	}

	// Auto-discovery
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("⚠️  Projects directory not found: %s", rootPath)
			return nil
		}
		return fmt.Errorf("failed to read projects directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectPath := filepath.Join(rootPath, entry.Name())
		project, err := o.inspectProject(projectPath, entry.Name())
		if err != nil {
			log.Printf("⚠️  Could not inspect project %s: %v", entry.Name(), err)
			continue
		}

		if project != nil {
			o.projects = append(o.projects, *project)
		}
	}

	return nil
}

// inspectProject determines if a directory is a project and its type
func (o *Overseer) inspectProject(path, name string) (*types.Project, error) {
	// Check for .beads directory at root (single repo)
	beadsPath := filepath.Join(path, ".beads", "beads.db")
	if _, err := os.Stat(beadsPath); err == nil {
		return &types.Project{
			Name: name,
			Path: path,
			Type: types.RepoTypeSingle,
		}, nil
	}

	// Check subdirectories for .beads (multi-repo)
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var repos []types.Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subBeadsPath := filepath.Join(path, entry.Name(), ".beads", "beads.db")
		if _, err := os.Stat(subBeadsPath); err == nil {
			repos = append(repos, types.Repo{
				Name: entry.Name(),
				Path: entry.Name(),
			})
		}
	}

	if len(repos) > 0 {
		return &types.Project{
			Name:  name,
			Path:  path,
			Type:  types.RepoTypeMulti,
			Repos: repos,
		}, nil
	}

	// No beads found
	return nil, nil
}

// tick performs one iteration of the scheduling loop
func (o *Overseer) tick() error {
	// Check if paused
	if o.IsPaused() {
		return nil
	}

	// Get active worker count
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	maxWorkers := o.GetMaxWorkers()

	// Handle max_workers = 0 as "unlimited"
	var availableSlots int
	if maxWorkers == 0 {
		// Unlimited mode: allow spawning based on available CPU/Memory resources only
		// Still limit to reasonable batch size to prevent overwhelming the system
		availableSlots = 20 // Reasonable default batch size for unlimited mode
		log.Printf("📊 Unlimited worker mode active (max_workers=0), allowing up to %d slots per tick", availableSlots)
	} else {
		availableSlots = maxWorkers - activeCount
		if availableSlots <= 0 {
			return nil
		}
	}

	// Build/refresh DAG from all beads
	if err := o.buildDAG(); err != nil {
		// Graceful degradation: log error and continue with existing DAG
		o.logGracefulDegradation("Overseer", "buildDAG", "using existing DAG", err)
		// Continue with existing DAG if rebuild fails
	}

	// Register chains only ONCE at startup (not on every discovery cycle)
	// This is called from the main loop but we track registration state
	if o.chainCompletionDetector != nil && !o.chainsRegistered {
		o.registerChainsFromBeadsClient()
		o.chainsRegistered = true
	}

	// Get ready beads using beadsClient (calls 'br ready' directly)
	// This is more reliable than DAG which may have stale dependency data
	readyBeads, err := o.getReadyBeads()
	if err != nil {
		return fmt.Errorf("failed to get ready beads: %w", err)
	}

	// Include failed beads that are eligible for retry
	retryBeads := o.getRetryEligibleBeads(readyBeads)

	if len(retryBeads) == 0 {
		return nil
	}

	// Count retry beads separately for logging
	originalCount := len(readyBeads)
	retryCount := len(retryBeads) - originalCount

	if retryCount > 0 {
		log.Printf("📊 Active workers: %d/%d, Ready beads: %d (original: %d, retries: %d)",
			activeCount, maxWorkers, len(retryBeads), originalCount, retryCount)
	} else {
		log.Printf("📊 Active workers: %d/%d, Ready beads from DAG: %d", activeCount, maxWorkers, len(retryBeads))
	}

	// Apply 70% batch spawning rule: only spawn up to 70% of available slots per tick
	// This prevents overwhelming the system and allows for better resource management
	batchLimit := int(float64(availableSlots) * 0.7)
	if batchLimit < 1 {
		batchLimit = 1 // Always allow at least 1 worker to spawn
	}
	log.Printf("📊 Batch spawning limit: %d workers (70%% of %d available slots)", batchLimit, availableSlots)

	// Register chains before spawning workers (to catch beads just before execution)
	// This ensures we track chains even when beads complete in milliseconds
	if o.chainCompletionDetector != nil && !o.chainsRegistered && o.dag != nil {
		o.registerChainsFromBeadsClient()
		o.chainsRegistered = true
		log.Printf("[ChainCompletion] Registered chains before worker spawn")
	}

	// Spawn workers for ready beads
	spawnedCount := 0
	for _, bead := range retryBeads {
		// CRITICAL: Recalculate available slots before each spawn
		// This prevents race conditions where workers complete between spawns
		o.workerMutex.RLock()
		currentActive := len(o.activeWorkers)
		o.workerMutex.RUnlock()

		currentAvailable := maxWorkers - currentActive
		if maxWorkers == 0 {
			currentAvailable = 20 // Unlimited mode
		}

		if currentAvailable <= 0 || spawnedCount >= batchLimit {
			if spawnedCount >= batchLimit {
				log.Printf("📊 Reached batch limit (%d workers), deferring remaining beads to next tick", batchLimit)
			} else if currentAvailable <= 0 {
				log.Printf("📊 No available slots (active: %d/%d), breaking spawn loop", currentActive, maxWorkers)
			}
			break
		}

		beadIDStr := string(bead.ID)

		// Check if this is a retry
		if retryInfo, exists := o.retries[beadIDStr]; exists {
			log.Printf("[Retry] Attempting retry %d/%d for bead %s",
				retryInfo.count+1, o.config.Overseer.DefaultRetries, bead.ID)
		}

		// Skip approval gates (require manual br close)
		if bead.Type == "approval-gate" {
			log.Printf("⏸️  Skipping approval gate: %s", bead.ID)
			continue
		}

		// Skip approval beads that are in progress (waiting for user approval)
		if bead.Type == "approval" && bead.Status == types.StatusInProgress {
			log.Printf("⏸️  Skipping approval bead (waiting for user approval): %s", bead.ID)
			continue
		}

		// Skip if project path is not found (can't execute without knowing where the project is)
		projectPath := o.findProjectPath(&bead)
		if projectPath == "" {
			log.Printf("⚠️  Skipping bead %s: project '%s' not configured or found", bead.ID, bead.Project)
			continue
		}

		// Check resource limits before spawning
		if o.resourceMonitor != nil && !o.resourceMonitor.CanSpawnWorker() {
			log.Printf("[ResourceMonitor] Blocking worker spawn for bead %s: resource limits exceeded", bead.ID)
			// Don't decrease availableSlots - we want to retry this bead
			continue
		}

		// Check rate limiting: max 5 workers per 10 seconds
		if !o.canSpawnWithinRateLimit() {
			log.Printf("[RateLimit] Blocking spawn for bead %s: rate limit exceeded", bead.ID)
			// Break the loop - no more spawns in this window
			break
		}

		if err := o.spawnWorker(bead); err != nil {
			// Don't record retry for worker limit or rate limit - these are resource constraints, not failures
			// The bead will naturally be retried in the next tick without accumulating failure counts
			errMsg := err.Error()
			isResourceConstraint := strings.Contains(errMsg, "worker limit reached") ||
				strings.Contains(errMsg, "rate limit exceeded") ||
				strings.Contains(errMsg, "resource limits exceeded")

			if !isResourceConstraint {
				o.recordRetry(beadIDStr, err)
			}
			o.logError("Overseer", "spawnWorker", beadIDStr, err, "error")
			continue
		}

		// Record successful spawn for rate limiting
		o.recordWorkerSpawn()

		// Clear retry state on successful spawn
		o.clearRetry(beadIDStr)
		availableSlots--
		spawnedCount++
	}

	if spawnedCount > 0 {
		log.Printf("📊 Spawned %d workers this tick (batch limit: %d)", spawnedCount, batchLimit)
	}

	return nil
}

// getRetryEligibleBeads returns beads that are eligible for retry
// It filters the ready beads and adds any failed beads that should be retried
func (o *Overseer) getRetryEligibleBeads(readyBeads []types.Bead) []types.Bead {
	o.retriesMu.RLock()
	defer o.retriesMu.RUnlock()

	if len(o.retries) == 0 {
		return readyBeads
	}

	// Create a map of already ready beads for deduplication
	readyMap := make(map[string]types.Bead, len(readyBeads))
	for _, bead := range readyBeads {
		readyMap[string(bead.ID)] = bead
	}

	// Check failed beads for retry eligibility
	for beadID, retryInfo := range o.retries {
		// Skip if already in ready pool
		if _, exists := readyMap[beadID]; exists {
			continue
		}

		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}

		// Check if retries exhausted
		if retryInfo.count >= maxRetries {
			log.Printf("[Retry] Bead %s exhausted all %d retry attempts, skipping", beadID, maxRetries)
			continue
		}

		// Check cooldown period
		backoff := calculateBackoff(retryInfo.count, o.baseDelay, o.maxDelay)
		minInterval := defaultRetryCooldown + backoff
		if time.Since(retryInfo.lastError) < minInterval {
			continue
		}

		// Try to fetch the bead from the DAG
		if bead, err := o.getBeadFromDAG(beadID); err == nil {
			readyBeads = append(readyBeads, bead)
			log.Printf("[Retry] Bead %s added to retry queue (attempt %d/%d)",
				beadID, retryInfo.count+1, maxRetries)
		} else {
			log.Printf("[Retry] Could not fetch bead %s for retry: %v", beadID, err)
		}
	}

	return readyBeads
}

// getBeadFromDAG retrieves a bead from the DAG by ID
func (o *Overseer) getBeadFromDAG(beadID string) (types.Bead, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	// Try to find the bead in the DAG
	// This is a simplified implementation - in production, the DAG might have
	// a direct lookup method
	for _, node := range o.dag.GetReadyNodes() {
		if string(node.Bead.ID) == beadID {
			return node.Bead, nil
		}
	}

	return types.Bead{}, fmt.Errorf("bead %s not found in DAG", beadID)
}

// buildDAG fetches all beads from all projects and builds/refreshs the DAG
func (o *Overseer) buildDAG() error {
	o.dagMu.Lock()
	defer o.dagMu.Unlock()

	// If DAG is nil, create a new one (graceful degradation)
	if o.dag == nil {
		log.Printf("[DAG] DAG was nil, creating new instance")
		o.dag = NewDAG()
	}

	// Clear existing DAG
	o.dag.Clear()

	var allBeads []types.Bead
	var fetchErrors []error

	// Fetch all beads from all projects
	for _, project := range o.projects {
		if project.Type == types.RepoTypeSingle {
			beads, err := o.beadsClient.ListBeads(project.Path, "")
			if err != nil {
				// Graceful degradation: log error and continue
				fetchErr := &BeadFetchError{
					Op:      "list",
					Project: project.Name,
					Err:     err,
				}
				o.logGracefulDegradation("beadsClient", "ListBeads",
					fmt.Sprintf("skipping project %s", project.Name), fetchErr)
				fetchErrors = append(fetchErrors, fetchErr)
				continue
			}

			// Fetch full details for each bead to get dependencies
			for _, bead := range beads {
				fullBead, err := o.beadsClient.GetBead(project.Path, "", bead.ID)
				if err != nil {
					// Graceful degradation: use list bead if details fetch fails
					fetchErr := &BeadFetchError{
						Op:      "get",
						Project: project.Name,
						Err:     err,
					}
					o.logGracefulDegradation("beadsClient", "GetBead",
						fmt.Sprintf("using list data for %s", bead.ID), fetchErr)
					allBeads = append(allBeads, bead)
				} else {
					allBeads = append(allBeads, *fullBead)
				}
			}
		} else {
			// Multi-repo project
			for _, repo := range project.Repos {
				beads, err := o.beadsClient.ListBeads(project.Path, repo.Name)
				if err != nil {
					// Graceful degradation: log error and continue
					fetchErr := &BeadFetchError{
						Op:      "list",
						Project: project.Name,
						Repo:    repo.Name,
						Err:     err,
					}
					o.logGracefulDegradation("beadsClient", "ListBeads",
						fmt.Sprintf("skipping repo %s/%s", project.Name, repo.Name), fetchErr)
					fetchErrors = append(fetchErrors, fetchErr)
					continue
				}

				// Fetch full details for each bead to get dependencies
				for _, bead := range beads {
					fullBead, err := o.beadsClient.GetBead(project.Path, repo.Name, bead.ID)
					if err != nil {
						// Graceful degradation: use list bead if details fetch fails
						fetchErr := &BeadFetchError{
							Op:      "get",
							Project: project.Name,
							Repo:    repo.Name,
							Err:     err,
						}
						o.logGracefulDegradation("beadsClient", "GetBead",
							fmt.Sprintf("using list data for %s", bead.ID), fetchErr)
						allBeads = append(allBeads, bead)
					} else {
						allBeads = append(allBeads, *fullBead)
					}
				}
			}
		}
	}

	log.Printf("[DAG] Building DAG with %d beads (from %d projects)", len(allBeads), len(o.projects))

	// Add all beads as nodes to the DAG
	nodeErrors := 0
	for _, bead := range allBeads {
		if err := o.dag.AddNode(bead); err != nil {
			if errors.Is(err, ErrNodeAlreadyExists) {
				log.Printf("[DAG] Duplicate bead skipped: %s", bead.ID)
				continue
			}
			o.logError("DAG", "AddNode", string(bead.ID), err, "warn")
			nodeErrors++
		}
	}

	// Add edges for dependencies
	edgeErrors := 0
	for _, bead := range allBeads {
		beadID := string(bead.ID)
		for _, depID := range bead.Dependencies {
			depIDStr := string(depID)
			if err := o.dag.AddEdge(beadID, depIDStr); err != nil {
				switch {
				case errors.Is(err, ErrNodeNotFound):
					log.Printf("[DAG] Dependency not found: %s -> %s (may be in different project)", depIDStr, beadID)
				case errors.Is(err, ErrSelfReference):
					log.Printf("[DAG] Self-reference detected in bead %s, skipping edge", beadID)
				case errors.Is(err, ErrEdgeCreatesCycle):
					log.Printf("[DAG] Cycle would be created: %s -> %s, skipping edge", depIDStr, beadID)
				default:
					o.logError("DAG", "AddEdge", beadID, err, "warn")
				}
				edgeErrors++
				// Continue even if edge creation fails (graceful degradation)
			}
		}
	}

	// Detect and log cycles
	if cycle, err := o.dag.DetectCycle(); err != nil {
		o.logError("DAG", "DetectCycle", "", err, "error")
	} else if cycle != nil {
		log.Printf("[DAG] Cycle detected: %v", cycle)
	}

	stats := DAGStats{
		TotalNodes:     o.dag.GetNodeCount(),
		ReadyNodes:     len(o.dag.GetReadyNodes()),
		BlockedNodes:   o.dag.GetBlockedCount(),
		CompletedNodes: o.dag.GetCompletedCount(),
		FailedNodes:    o.dag.GetFailedCount(),
	}

	log.Printf("[DAG] Build complete: %+v (fetch errors: %d, node errors: %d, edge errors: %d)",
		stats, len(fetchErrors), nodeErrors, edgeErrors)

	// Return error only if we couldn't fetch any beads at all
	if len(allBeads) == 0 && len(fetchErrors) > 0 {
		return fmt.Errorf("failed to fetch any beads: %d fetch errors occurred", len(fetchErrors))
	}

	return nil
}

// registerChainsFromBeadsClient registers chains by reading beads directly from .beads/issues.jsonl
// This gets ALL beads regardless of status (open/closed), unlike br list which only returns open
func (o *Overseer) registerChainsFromBeadsClient() {
	if o.chainCompletionDetector == nil {
		return
	}

	allBeadMap := make(map[string]types.Bead)
	dependsOnMap := make(map[string][]string) // beadID -> list of beads that depend on it

	for _, project := range o.projects {
		issuesFile := filepath.Join(project.Path, ".beads", "issues.jsonl")
		beads, err := o.readBeadsFromFile(issuesFile)
		if err != nil {
			log.Printf("[ChainCompletion] Could not read beads from %s: %v", issuesFile, err)
			continue
		}
		log.Printf("[ChainCompletion] Read %d beads from %s", len(beads), project.Name)
		for _, bead := range beads {
			allBeadMap[string(bead.ID)] = bead
			// Build inverse map: what depends on this bead
			// If bead B has depends_on_id = A, then B depends on A, so A is blocked by B
			// We want to find: given A, what beads depend on A?
			for _, dep := range bead.Dependencies {
				depStr := string(dep)
				dependsOnMap[depStr] = append(dependsOnMap[depStr], string(bead.ID))
			}
		}
	}

	if len(allBeadMap) == 0 {
		log.Printf("[ChainCompletion] No beads found, skipping chain registration")
		return
	}

	// Find roots (beads with no dependencies - they don't depend on anything)
	roots := make([]types.Bead, 0)
	for _, bead := range allBeadMap {
		if len(bead.Dependencies) == 0 {
			roots = append(roots, bead)
		}
	}

	log.Printf("[ChainCompletion] Found %d root beads", len(roots))

	chainCount := 0
	visited := make(map[string]bool)

	// For each root, traverse down to find all dependents
	for _, root := range roots {
		var chainBeads []types.BeadID

		var dfs func(beadID string)
		dfs = func(beadID string) {
			if visited[beadID] {
				return
			}
			visited[beadID] = true

			bid, _ := types.ParseBeadID(beadID)
			chainBeads = append(chainBeads, bid)

			// Find all beads that depend on this one (inverse lookup)
			for _, dependent := range dependsOnMap[beadID] {
				dfs(dependent)
			}
		}

		dfs(string(root.ID))

		if len(chainBeads) > 1 {
			o.chainCompletionDetector.RegisterChain(chainBeads[0], chainBeads)
			chainCount++
			log.Printf("[ChainCompletion] Registered chain with %d beads starting from %s", len(chainBeads), root.ID)
		}
	}

	log.Printf("[ChainCompletion] Registered %d chains from %d total beads", chainCount, len(allBeadMap))
}

// GetBeadFromFile reads a single bead from issues.jsonl
func (o *Overseer) GetBeadFromFile(beadID string) *types.Bead {
	for _, project := range o.projects {
		issuesFile := filepath.Join(project.Path, ".beads", "issues.jsonl")
		beads, err := o.readBeadsFromFile(issuesFile)
		if err != nil {
			continue
		}
		for _, bead := range beads {
			if string(bead.ID) == beadID {
				return &bead
			}
		}
	}
	return nil
}

// readBeadsFromFile reads beads from .beads/issues.jsonl file
func (o *Overseer) readBeadsFromFile(filepath string) ([]types.Bead, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	var beads []types.Bead
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse as generic map first to handle the nested dependencies
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		var bead types.Bead
		// Manually map fields
		if id, ok := raw["id"].(string); ok {
			bead.ID = types.BeadID(id)
		}
		if title, ok := raw["title"].(string); ok {
			bead.Title = title
		}
		if desc, ok := raw["description"].(string); ok {
			bead.Description = desc
		}
		if t, ok := raw["type"].(string); ok {
			bead.Type = t
		}
		if status, ok := raw["status"].(string); ok {
			bead.Status = types.BeadStatus(status)
		}
		if project, ok := raw["project"].(string); ok {
			bead.Project = project
		}
		if repo, ok := raw["source_repo"].(string); ok {
			bead.Repo = repo
		}

		// Extract depends_on (what this bead needs to complete first)
		// This is the INVERSE of Dependencies - it tells us what this bead DEPENDS on
		if deps, ok := raw["dependencies"].([]interface{}); ok {
			for _, d := range deps {
				if depMap, ok := d.(map[string]interface{}); ok {
					if dependsOn, ok := depMap["depends_on_id"].(string); ok {
						// Store as DependsOn (what this bead depends on)
						bead.Dependencies = append(bead.Dependencies, types.BeadID(dependsOn))
					}
				}
			}
		}

		beads = append(beads, bead)
	}

	log.Printf("[ChainCompletion] Parsed %d beads from %s", len(beads), filepath)
	return beads, nil
}

func (o *Overseer) getAllDescendants(nodeID string) []string {
	var descendants []string
	visited := make(map[string]bool)

	var dfs func(id string)
	dfs = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		node, exists := o.dag.GetNode(id)
		if !exists {
			return
		}

		for _, outEdge := range node.OutEdges {
			descendants = append(descendants, outEdge)
			dfs(outEdge)
		}
	}

	dfs(nodeID)
	return descendants
}

// getReadyBeads retrieves beads that are ready to execute
// Primary: uses br ready for maximum parallelism (returns all unblocked beads)
// Falls back to bv --robot-triage for intelligent prioritization if br returns insufficient beads
func (o *Overseer) getReadyBeads() ([]types.Bead, error) {
	// Try br ready first for maximum parallelism (returns all 20+ unblocked beads)
	// This is more reliable than BV triage which filters to only 1-2 beads
	brBeads, brErr := o.getReadyBeadsFromBR()
	if brErr != nil {
		log.Printf("⚠️  br ready failed: %v", brErr)
	}

	// If br ready worked and returned beads, use them
	if brErr == nil && len(brBeads) > 0 {
		log.Printf("✅ Using br ready: found %d unblocked beads", len(brBeads))
		return brBeads, nil
	}

	// Fallback to bv triage if br ready failed or returned no beads
	if o.bvClient != nil {
		log.Printf("⚠️  br ready failed or returned 0 beads, falling back to bv triage")
		beads, err := o.getReadyBeadsFromBV()
		if err != nil {
			return nil, fmt.Errorf("bv triage failed: %w", err)
		}
		return beads, nil
	}

	// If both fail, return error
	if brErr != nil {
		return nil, fmt.Errorf("both br ready and bv triage failed: br: %w, bv: nil", brErr)
	}

	return nil, fmt.Errorf("no beads found")
}

// getReadyBeadsFromBR retrieves beads using br ready (legacy method)
func (o *Overseer) getReadyBeadsFromBR() ([]types.Bead, error) {
	var allReadyBeads []types.Bead

	for _, project := range o.projects {
		if project.Type == types.RepoTypeSingle {
			beads, err := o.beadsClient.GetReadyBeads(project.Name, project.Path, "")
			if err != nil {
				return nil, fmt.Errorf("could not get beads for project %s: %w", project.Name, err)
			}
			allReadyBeads = append(allReadyBeads, beads...)
		} else {
			// Multi-repo project
			for _, repo := range project.Repos {
				beads, err := o.beadsClient.GetReadyBeads(project.Name, project.Path, repo.Name)
				if err != nil {
					return nil, fmt.Errorf("could not get beads for repo %s/%s: %w", project.Name, repo.Name, err)
				}
				allReadyBeads = append(allReadyBeads, beads...)
			}
		}
	}

	// Filter out beads that are already running or completed
	o.workerMutex.RLock()
	var readyBeads []types.Bead
	for _, bead := range allReadyBeads {
		beadIDStr := string(bead.ID)

		// Filter: not already running
		if _, isRunning := o.activeWorkers[beadIDStr]; isRunning {
			continue
		}

		// Filter: not already completed in this session
		if o.completed[beadIDStr] {
			continue
		}

		// Skip approval gates
		if bead.Type == "approval-gate" {
			continue
		}

		// Skip approval beads that are in progress (waiting for user approval)
		if bead.Type == "approval" && bead.Status == types.StatusInProgress {
			continue
		}

		// CRITICAL: Skip beads with unresolved dependencies
		// br ready may return beads that have dependencies not yet satisfied
		if len(bead.Dependencies) > 0 {
			log.Printf("⏸️  [BR] Skipping bead with unresolved dependencies: %s (depends on: %v)", beadIDStr, bead.Dependencies)
			continue
		}

		readyBeads = append(readyBeads, bead)
	}
	o.workerMutex.RUnlock()

	// Sort by priority (high to low)
	sort.Slice(readyBeads, func(i, j int) bool {
		return readyBeads[i].Priority > readyBeads[j].Priority
	})

	log.Printf("📋 Found %d ready beads from 'br ready' (after filtering)", len(readyBeads))

	return readyBeads, nil
}

// getReadyBeadsFromBV retrieves beads that are ready to execute using bv --robot-triage
// This provides intelligent prioritization with PageRank, critical path, and dependency analysis
// Falls back to br ready if bv is not available (fail hard if bv fails when enabled)
func (o *Overseer) getReadyBeadsFromBV() ([]types.Bead, error) {
	if o.bvClient == nil {
		return nil, fmt.Errorf("bv client not initialized")
	}

	var allReadyBeads []types.Bead

	for _, project := range o.projects {
		// Get triage from bv for this project
		triage, err := o.bvClient.GetTriage(project.Path)
		if err != nil {
			return nil, fmt.Errorf("bv triage failed for project %s: %w", project.Name, err)
		}

		log.Printf("[BV] Triage for %s: %d recommendations, %d ready, %d blocked",
			project.Name, len(triage.Recommendations), triage.ReadyCount, triage.BlockedCount)

		// Process each recommendation
		for _, rec := range triage.Recommendations {
			beadID := types.BeadID(rec.BeadID)
			beadIDStr := string(beadID)

			// Filter: not already running
			o.workerMutex.RLock()
			if _, isRunning := o.activeWorkers[beadIDStr]; isRunning {
				o.workerMutex.RUnlock()
				continue
			}
			o.workerMutex.RUnlock()

			// Filter: not already completed in this session
			if o.completed[beadIDStr] {
				continue
			}

			// Skip beads that are blocked by dependencies
			if len(rec.BlockedBy) > 0 {
				log.Printf("⏸️  [BV] Skipping blocked bead: %s (blocked by: %v)", beadID, rec.BlockedBy)
				continue
			}

			// Skip approval gates
			if rec.Type == "approval-gate" {
				log.Printf("⏸️  [BV] Skipping approval gate: %s", beadID)
				continue
			}

			// Fetch full bead details from database (triage only has summary)
			var repoName string
			if project.Type != types.RepoTypeSingle && len(project.Repos) > 0 {
				repoName = project.Repos[0].Name // Use first repo for multi-repo projects
			}

			bead, err := o.beadsClient.GetBead(project.Path, repoName, beadID)
			if err != nil {
				log.Printf("⚠️  [BV] Could not get bead details for %s: %v", beadID, err)
				// Create minimal bead from triage data as fallback
				bead = &types.Bead{
					ID:          beadID,
					Title:       rec.Title,
					Type:        rec.Type,
					Priority:    rec.Priority,
					Project:     project.Name,
					Repo:        repoName,
					Status:      types.StatusOpen, // Assume open since bv recommended it
					Labels:      rec.Labels,
					Description: rec.Reason, // Use reason as description
					Metadata: map[string]string{
						"bv_confidence":     fmt.Sprintf("%f", rec.Confidence),
						"bv_reason":         rec.Reason,
						"bv_estimated_time": rec.EstimatedTime,
					},
				}
			}

			// Skip approval beads that are in progress (waiting for user approval)
			if bead.Type == "approval" && bead.Status == types.StatusInProgress {
				log.Printf("⏸️  [BV] Skipping approval bead (waiting for user approval): %s", beadID)
				continue
			}

			// Add confidence score to metadata for logging/debugging
			if bead.Metadata == nil {
				bead.Metadata = make(map[string]string)
			}
			bead.Metadata["bv_confidence"] = fmt.Sprintf("%f", rec.Confidence)
			bead.Metadata["bv_reason"] = rec.Reason

			allReadyBeads = append(allReadyBeads, *bead)
		}

		// Also process blockers_to_clear for actionable beads not in recommendations
		for _, blocker := range triage.BlockersToClear {
			if !blocker.Actionable {
				continue // Skip non-actionable blockers
			}

			beadID := types.BeadID(blocker.BeadID)
			beadIDStr := string(beadID)

			// Filter: not already running
			o.workerMutex.RLock()
			if _, isRunning := o.activeWorkers[beadIDStr]; isRunning {
				o.workerMutex.RUnlock()
				continue
			}
			o.workerMutex.RUnlock()

			// Filter: not already completed
			if o.completed[beadIDStr] {
				continue
			}

			// Skip if already in allReadyBeads (avoid duplicates)
			alreadyAdded := false
			for _, existing := range allReadyBeads {
				if existing.ID == beadID {
					alreadyAdded = true
					break
				}
			}
			if alreadyAdded {
				continue
			}

			// Skip if blocked
			if len(blocker.BlockedBy) > 0 {
				log.Printf("⏸️  [BV] Skipping blocked bead from blockers_to_clear: %s (blocked by: %v)", beadID, blocker.BlockedBy)
				continue
			}

			log.Printf("[BV] Found actionable blocker to clear: %s - %s", beadID, blocker.Title)

			// Fetch full bead details
			var repoName string
			if project.Type != types.RepoTypeSingle && len(project.Repos) > 0 {
				repoName = project.Repos[0].Name
			}

			bead, err := o.beadsClient.GetBead(project.Path, repoName, beadID)
			if err != nil {
				log.Printf("⚠️  [BV] Could not get bead details for blocker %s: %v", beadID, err)
				// Create minimal bead as fallback
				bead = &types.Bead{
					ID:       beadID,
					Title:    blocker.Title,
					Type:     "task",
					Priority: 3, // Default priority
					Project:  project.Name,
					Repo:     repoName,
					Status:   types.StatusOpen,
					Metadata: map[string]string{
						"bv_source":     "blockers_to_clear",
						"bv_unblocks":   fmt.Sprintf("%d", blocker.UnblocksCount),
						"bv_confidence": "0.8",
						"bv_reason":     fmt.Sprintf("Unblocks %d items", blocker.UnblocksCount),
					},
				}
			}

			// Add metadata for sorting
			if bead.Metadata == nil {
				bead.Metadata = make(map[string]string)
			}
			bead.Metadata["bv_source"] = "blockers_to_clear"
			bead.Metadata["bv_unblocks"] = fmt.Sprintf("%d", blocker.UnblocksCount)
			bead.Metadata["bv_confidence"] = "0.8" // High confidence for unblocking beads
			bead.Metadata["bv_reason"] = fmt.Sprintf("Unblocks %d items: %v", blocker.UnblocksCount, blocker.UnblocksIDs)

			allReadyBeads = append(allReadyBeads, *bead)
		}
	}

	// Sort by bv confidence (highest first), then by priority
	sort.Slice(allReadyBeads, func(i, j int) bool {
		confI := 0.0
		confJ := 0.0
		if c, err := fmt.Sscanf(allReadyBeads[i].Metadata["bv_confidence"], "%f", &confI); err == nil && c == 1 {
			// parsed
		}
		if c, err := fmt.Sscanf(allReadyBeads[j].Metadata["bv_confidence"], "%f", &confJ); err == nil && c == 1 {
			// parsed
		}

		// Higher confidence first, then higher priority
		if confI != confJ {
			return confI > confJ
		}
		return allReadyBeads[i].Priority > allReadyBeads[j].Priority
	})

	log.Printf("📋 [BV] Found %d ready beads from triage (after filtering)", len(allReadyBeads))

	return allReadyBeads, nil
}

// spawnWorker creates and starts a worker for a bead using the configured spawner
func (o *Overseer) spawnWorker(bead types.Bead) error {
	log.Printf("[DEBUG] spawnWorker called with bead: ID='%s', Title='%s', Project='%s'", bead.ID, bead.Title, bead.Project)
	beadIDStr := bead.ID.String()
	if beadIDStr == "" {
		log.Printf("[ERROR] spawnWorker called with empty bead ID! Bead: %+v", bead)
		return fmt.Errorf("cannot spawn worker for bead with empty ID")
	}

	// Check if we should back off due to recent spawn failures
	if o.shouldSpawnBackoff() {
		o.spawnBackoffMu.Lock()
		backoff := time.Duration(1<<uint(o.spawnFailures-1)) * time.Second
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
		o.spawnBackoffMu.Unlock()
		return fmt.Errorf("spawn backoff active: waiting %v before retry", backoff)
	}

	// CRITICAL: Atomic check-and-reserve pattern to prevent race conditions
	// Hold lock during check AND registration to ensure accurate worker count
	o.workerMutex.Lock()

	// Check if worker already exists in activeWorkers
	if _, exists := o.activeWorkers[beadIDStr]; exists {
		// Check if worker is actually still running via WorkerTracker
		if o.workerTracker != nil {
			if workerRecord, found := o.workerTracker.GetWorkerByBead(beadIDStr); found {
				// If worker is marked as completed/failed in tracker, clean it up
				if workerRecord.Status == "completed" || workerRecord.Status == "failed" {
					log.Printf("[Overseer] Cleaning up stale worker %s (status: %s) from activeWorkers", beadIDStr, workerRecord.Status)
					delete(o.activeWorkers, beadIDStr)
					// Fall through to create new worker
				} else {
					// Worker is still tracked and not completed - it exists
					o.workerMutex.Unlock()
					o.logError("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
					return fmt.Errorf("worker already exists for bead %s", beadIDStr)
				}
			} else {
				// Worker in activeWorkers but not in tracker - stale entry, clean it up
				log.Printf("[Overseer] Cleaning up orphan worker %s (not in tracker)", beadIDStr)
				delete(o.activeWorkers, beadIDStr)
				// Fall through to create new worker
			}
		} else {
			// No worker tracker, just check activeWorkers
			o.workerMutex.Unlock()
			o.logError("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
			return fmt.Errorf("worker already exists for bead %s", beadIDStr)
		}
	}

	// Check worker limit
	activeCount := len(o.activeWorkers)
	maxWorkers := o.GetMaxWorkers()
	if activeCount >= maxWorkers {
		o.workerMutex.Unlock()
		log.Printf("[Overseer] BLOCKING spawn for %s: at worker limit (%d/%d)", beadIDStr, activeCount, maxWorkers)
		return fmt.Errorf("worker limit reached: %d/%d workers active", activeCount, maxWorkers)
	}

	// Pre-register worker slot (will be updated with actual worker after spawn)
	// This prevents other ticks from spawning workers beyond the limit
	placeholderWorker := &types.Worker{
		ID:     beadIDStr,
		Status: "spawning",
	}
	o.activeWorkers[beadIDStr] = placeholderWorker
	log.Printf("[WorkerTracking] Added placeholder for %s to activeWorkers. Count: %d", beadIDStr, len(o.activeWorkers))
	o.workerMutex.Unlock()

	// Check if bead has exhausted retries
	if retryInfo, exists := o.retries[beadIDStr]; exists {
		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}
		if retryInfo.count >= maxRetries {
			err := &RetryExhaustedError{
				BeadID:    beadIDStr,
				Attempts:  retryInfo.count,
				LastError: retryInfo.lastErr,
			}
			o.logError("spawnWorker", "retry_exhausted", beadIDStr, err, "error")
			return err
		}
	}

	// Create the worker using the agent connector
	worker, err := o.spawnWorkerViaAgent(bead)
	if err != nil {
		// Remove placeholder on spawn failure
		o.workerMutex.Lock()
		delete(o.activeWorkers, beadIDStr)
		o.workerMutex.Unlock()
		return err
	}

	// Update placeholder with real worker
	o.workerMutex.Lock()
	o.activeWorkers[beadIDStr] = worker
	o.workerMutex.Unlock()

	// Clear spawn failure count on successful spawn
	o.clearSpawnFailures()

	return nil
}

// spawnWorkerInternal creates and starts a real worker process for a bead
// This is the actual implementation used by defaultWorkerSpawner
func (o *Overseer) spawnWorkerInternal(bead types.Bead) (*types.Worker, error) {
	beadIDStr := bead.ID.String()

	// Determine agent type
	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) == 0 {
		err := fmt.Errorf("no agent type found for bead type: %s", bead.Type)
		o.logError("spawnWorker", "agent_type", beadIDStr, err, "error")
		return nil, err
	}

	agentType := agentTypes[0] // Use primary agent

	retryCount := o.getRetryCount(beadIDStr)
	if retryCount > 0 {
		log.Printf("[Worker] Spawning retry worker for bead: %s (attempt %d, agent: %s)", beadIDStr, retryCount+1, agentType)
	} else {
		log.Printf("[Worker] Spawning worker for bead: %s (agent: %s)", beadIDStr, agentType)
	}

	// Find project path
	projectPath := o.findProjectPath(&bead)
	if projectPath == "" {
		projectPath = bead.Project // Fallback to project name
	}

	// Mark bead as in_progress before spawning worker
	if o.beadsClient != nil && projectPath != "" {
		log.Printf("[Overseer] Marking bead %s as in_progress", beadIDStr)
		if err := o.beadsClient.UpdateBeadStatus(projectPath, bead.ID, types.StatusInProgress); err != nil {
			log.Printf("[Overseer] Warning: failed to mark bead %s as in_progress: %v", beadIDStr, err)
			// Continue anyway - this is not fatal
		}
	}

	// Build worker command - use compiled binary if available, otherwise fallback to go run
	workerBinary := os.Getenv("HYDRA_WORKER_BINARY")
	if workerBinary == "" {
		workerBinary = filepath.Join(os.Getenv("HYDRA_HOME"), "bin", "worker")
	}

	// Check if compiled binary exists, otherwise use go run
	var cmd *exec.Cmd
	if _, err := os.Stat(workerBinary); err == nil {
		cmd = exec.Command(workerBinary)
	} else {
		// Fallback to go run
		cmd = exec.Command("go", "run", "cmd/worker/main.go")
	}
	env := append(os.Environ(),
		"HYDRA_WORKER=true", // Clear identifier that this is a Hydra-spawned worker
		fmt.Sprintf("HYDRA_BEAD_ID=%s", beadIDStr),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_PROJECT_PATH=%s", projectPath),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
		fmt.Sprintf("HYDRA_RETRY_COUNT=%d", retryCount),
		fmt.Sprintf("HYDRA_BEAD_TITLE=%s", bead.Title),
		fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", bead.Description),
	)

	// Pass API key to worker if configured
	if o.config != nil && o.config.Agents.Pi.ApiKeyName != "" {
		if apiKey := os.Getenv(o.config.Agents.Pi.ApiKeyName); apiKey != "" {
			env = append(env, fmt.Sprintf("%s=%s", o.config.Agents.Pi.ApiKeyName, apiKey))
		}
	}

	cmd.Env = env

	// Start process
	if err := cmd.Start(); err != nil {
		spawnErr := &WorkerSpawnError{
			BeadID:    beadIDStr,
			AgentType: string(agentType),
			Err:       err,
		}
		o.logError("spawnWorker", "process_start", beadIDStr, spawnErr, "error")

		// Record spawn failure for exponential backoff
		if strings.Contains(err.Error(), "resource temporarily unavailable") ||
			strings.Contains(err.Error(), "EAGAIN") ||
			strings.Contains(err.Error(), "fork") {
			o.recordSpawnFailure()
		}

		return nil, spawnErr
	}

	log.Printf("[Worker] Process started with PID %d for bead %s (workerTracker nil: %v)", cmd.Process.Pid, beadIDStr, o.workerTracker == nil)

	// Create worker record
	worker := &types.Worker{
		ID:          beadIDStr,
		BeadID:      bead.ID,
		PID:         cmd.Process.Pid,
		AgentType:   agentType,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: projectPath,
	}

	// Track worker PID for orphan detection
	if o.workerTracker != nil {
		if cmd.Process.Pid <= 0 {
			// Skip tracking placeholder PIDs
			log.Printf("[Overseer] Skipping tracking of invalid PID %d for bead %s", cmd.Process.Pid, beadIDStr)
		} else {
			log.Printf("[Overseer] Tracking worker PID %d for bead %s", cmd.Process.Pid, beadIDStr)
			if err := o.workerTracker.TrackWorker(cmd.Process.Pid, beadIDStr, projectPath); err != nil {
				log.Printf("[Overseer] Warning: failed to track worker PID %d: %v", cmd.Process.Pid, err)
			} else {
				log.Printf("[Overseer] Successfully tracked worker PID %d for bead %s", cmd.Process.Pid, beadIDStr)
			}
		}
	} else {
		log.Printf("[Overseer] Warning: workerTracker is nil, cannot track worker PID %d", cmd.Process.Pid)
	}

	// Monitor in goroutine
	go o.monitorWorker(worker, cmd)

	return worker, nil
}

// workerCompletion encapsulates the result of a worker's execution
type workerCompletion struct {
	worker     *types.Worker
	success    bool
	exitCode   int
	duration   time.Duration
	newlyReady []string
	closeError error
}

// monitorWorker watches a worker process until completion
func (o *Overseer) monitorWorker(worker *types.Worker, cmd *exec.Cmd) {
	completion := o.waitForWorker(worker, cmd)
	o.handleWorkerCompletion(completion)
}

// waitForWorker waits for a worker process to complete and returns completion details
func (o *Overseer) waitForWorker(worker *types.Worker, cmd *exec.Cmd) workerCompletion {
	log.Printf("[DEBUG] waitForWorker: waiting for worker %s (PID %d)", worker.ID, worker.PID)

	// Create timeout channel
	timeout := o.config.Overseer.WorkerTimeout
	done := make(chan error, 1)

	// Wait for process in goroutine
	go func() {
		done <- cmd.Wait()
	}()

	// Wait for either process completion or timeout
	var err error
	select {
	case err = <-done:
		// Process completed normally
	case <-time.After(timeout):
		// Timeout - kill the process
		log.Printf("[Overseer] Worker %s timed out after %v, killing process", worker.ID, timeout)
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("[Overseer] Failed to kill timed out worker: %v", err)
		}
		err = fmt.Errorf("worker timeout after %v", timeout)
	}

	exitCode := extractExitCode(err)
	success := exitCode == 0
	duration := time.Since(worker.StartTime)
	log.Printf("[DEBUG] waitForWorker: worker %s finished with exitCode=%d, success=%v", worker.ID, exitCode, success)

	var newlyReady []string
	if success {
		log.Printf("[DEBUG] waitForWorker: calling handleWorkerSuccess for %s", worker.ID)
		newlyReady = o.handleWorkerSuccess(worker, duration)
		log.Printf("[DEBUG] waitForWorker: handleWorkerSuccess returned for %s", worker.ID)
	} else {
		o.handleWorkerFailure(worker, exitCode, duration)
	}

	o.removeActiveWorker(worker.ID)

	// Mark worker as completed in tracker
	if o.workerTracker != nil {
		if err := o.workerTracker.MarkWorkerCompleted(worker.PID, exitCode); err != nil {
			log.Printf("[Overseer] Warning: failed to mark worker PID %d completed: %v", worker.PID, err)
		}
	}

	closeErr := o.closeBead(worker, success)

	return workerCompletion{
		worker:     worker,
		success:    success,
		exitCode:   exitCode,
		duration:   duration,
		newlyReady: newlyReady,
		closeError: closeErr,
	}
}

// extractExitCode extracts the exit code from an exec error
func extractExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		return exitError.ExitCode()
	}
	return -1
}

// handleWorkerSuccess processes a successful worker completion
func (o *Overseer) handleWorkerSuccess(worker *types.Worker, duration time.Duration) []string {
	log.Printf("[Worker] Completed: %s (duration: %v)", worker.ID, duration)
	worker.Status = string(types.WorkerStatusCompleted)
	o.clearRetry(worker.ID)

	o.dagMu.Lock()
	defer o.dagMu.Unlock()

	newlyReady, err := o.dag.MarkCompleted(worker.ID)
	if err != nil {
		o.logError("DAG", "MarkCompleted", worker.ID, err, "warn")
		return nil
	}

	if len(newlyReady) > 0 {
		log.Printf("[DAG] %d dependents now ready after %s completion", len(newlyReady), worker.ID)
	}

	o.completed[worker.ID] = true

	// Track bead for settled-state detection
	log.Printf("[DEBUG] handleWorkerSuccess: settledStateSvc=%v, BeadID=%s", o.settledStateSvc != nil, worker.BeadID)
	if o.settledStateSvc != nil {
		if err := o.settledStateSvc.TrackBead(worker.BeadID); err != nil {
			log.Printf("[SettledState] Failed to track bead %s: %v", worker.BeadID, err)
		}
	} else {
		log.Printf("[DEBUG] handleWorkerSuccess: settledStateSvc is NIL, cannot track bead %s", worker.BeadID)
	}

	return newlyReady
}

// handleWorkerFailure processes a failed worker completion
func (o *Overseer) handleWorkerFailure(worker *types.Worker, exitCode int, duration time.Duration) {
	workerErr := fmt.Errorf("worker exited with code %d", exitCode)
	o.recordRetry(worker.ID, workerErr)

	retryCount := o.getRetryCount(worker.ID)
	maxRetries := o.getMaxRetries()

	if retryCount >= maxRetries {
		// Exhausted retries, log as error
		log.Printf("[Worker] Failed permanently: %s (exit code: %d, duration: %v, retries: %d/%d)",
			worker.ID, exitCode, duration, retryCount, maxRetries)
		worker.Status = string(types.WorkerStatusFailed)
	} else {
		// Will retry, log as warning
		backoff := calculateBackoff(retryCount, o.baseDelay, o.maxDelay)
		log.Printf("[Worker] Failed but will retry: %s (exit code: %d, duration: %v, retry %d/%d, backoff: %v)",
			worker.ID, exitCode, duration, retryCount, maxRetries, backoff)
		worker.Status = string(types.WorkerStatusRetryPending)
	}

	o.dagMu.Lock()
	defer o.dagMu.Unlock()
	if err := o.dag.MarkFailed(worker.ID); err != nil {
		o.logError("DAG", "MarkFailed", worker.ID, err, "warn")
	}
}

// getMaxRetries returns the configured max retries with default fallback
func (o *Overseer) getMaxRetries() int {
	if o.config.Overseer.DefaultRetries > 0 {
		return o.config.Overseer.DefaultRetries
	}
	return defaultMaxRetries
}

// removeActiveWorker removes a worker from the active workers map
func (o *Overseer) removeActiveWorker(workerID string) {
	o.workerMutex.Lock()
	defer o.workerMutex.Unlock()
	countBefore := len(o.activeWorkers)
	delete(o.activeWorkers, workerID)
	countAfter := len(o.activeWorkers)
	log.Printf("[WorkerTracking] Removed worker %s from activeWorkers. Count: %d -> %d", workerID, countBefore, countAfter)
}

// handleWorkerCompletion handles the final steps after worker completion
func (o *Overseer) handleWorkerCompletion(comp workerCompletion) {
	success := comp.success
	exitCode := comp.exitCode

	if comp.closeError != nil {
		o.logError("beadsClient", "CloseBead", comp.worker.ID, comp.closeError, "warn")
		if isDependencyError(comp.closeError) {
			log.Printf("[Worker] Bead %s processed but not closed due to dependencies - will retry later", comp.worker.BeadID.String())
			success = false
			exitCode = 1
		}
	}

	o.scheduleAfterCompletion(comp.worker, success, comp.newlyReady)
	o.sendMCPWorkerCompletion(comp.worker, success, exitCode, comp.duration)
}

// isDependencyError checks if an error is related to unresolved dependencies
func isDependencyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unresolved dependencies") || strings.Contains(errStr, "dependencies")
}

// scheduleAfterCompletion triggers scheduling based on completion outcome
func (o *Overseer) scheduleAfterCompletion(worker *types.Worker, success bool, newlyReady []string) {
	if success {
		if len(newlyReady) > 0 {
			log.Printf("[Scheduler] Triggering immediate scheduling for %d newly ready beads", len(newlyReady))
		} else {
			log.Printf("[Scheduler] Worker completed, triggering immediate scheduling cycle")
		}
		go o.triggerImmediateSchedule()
	} else if o.getRetryCount(worker.ID) < o.getMaxRetries() {
		log.Printf("[Scheduler] Triggering scheduling to attempt retry for %s", worker.ID)
		go o.triggerImmediateSchedule()
	}
}

// closeBead closes the bead via beadsClient based on worker completion status.
// It determines the appropriate status reason and handles project path resolution.
// On successful completion, it queues the repository for TLDR indexing.
func (o *Overseer) closeBead(worker *types.Worker, success bool) error {
	projectName := worker.BeadID.Project()
	repoName := worker.BeadID.Repo()

	log.Printf("[closeBead] Closing bead %s (project: %s, repo: %s)",
		worker.BeadID.String(), projectName, repoName)

	projectPath := o.resolveProjectPath(worker, projectName)
	if projectPath == "" {
		return fmt.Errorf("project %s not found", projectName)
	}

	reason := statusReason(success)
	log.Printf("[closeBead] Closing with reason: %s (path: %s)", reason, projectPath)

	// Queue TLDR indexing on successful completion
	if success && o.config.Intelligence.TLDR.Indexing.Enabled {
		// Find the repo path
		repoPath := o.resolveRepoPath(projectName, repoName, projectPath)
		if repoPath != "" {
			o.queueRepoIndexing(projectName, repoPath)
		}
	}

	return o.beadsClient.CloseBead(projectPath, repoName, worker.BeadID, reason)
}

// resolveRepoPath finds the repository path for a given project and repo
func (o *Overseer) resolveRepoPath(projectName, repoName, projectPath string) string {
	// Find the project
	for _, p := range o.projects {
		if p.Name == projectName {
			if p.Type == "multi" {
				// Multi-repo: find specific repo
				for _, r := range p.Repos {
					if r.Name == repoName {
						return r.Path
					}
				}
			}
			// Single repo or default to project path
			return p.Path
		}
	}
	// Fallback to project path
	return projectPath
}

// resolveProjectPath determines the project path using multiple resolution strategies
func (o *Overseer) resolveProjectPath(worker *types.Worker, projectName string) string {
	// Priority 1: Use path stored in worker record
	if worker.ProjectPath != "" {
		return worker.ProjectPath
	}

	// Priority 2: Find by project name in discovered projects
	for _, p := range o.projects {
		if p.Name == projectName {
			return p.Path
		}
	}

	// Priority 3: Find by project name in config
	if o.config != nil {
		for _, proj := range o.config.Projects {
			if proj.Name == projectName {
				return proj.Path
			}
		}
	}

	// Priority 4: Use first project with valid .beads directory
	for _, proj := range o.projects {
		if hasBeadsDirectory(proj.Path) {
			return proj.Path
		}
	}

	log.Printf("[resolveProjectPath] ERROR: Project %s not found in %d projects", projectName, len(o.projects))
	return ""
}

// hasBeadsDirectory checks if a path contains a .beads directory
func hasBeadsDirectory(projectPath string) bool {
	beadsDir := filepath.Join(projectPath, ".beads")
	_, err := os.Stat(beadsDir)
	return err == nil
}

// statusReason returns the appropriate status string based on success.
// This is a pure function with no dependencies on Overseer state.
func statusReason(success bool) string {
	if success {
		return "completed"
	}
	return "failed"
}

// triggerImmediateSchedule spawns workers for ready beads without waiting for next poll
func (o *Overseer) triggerImmediateSchedule() {
	if o.IsPaused() {
		log.Println("⏸️  Immediate scheduling skipped: overseer is paused")
		return
	}

	// Check rate limit before spawning (same as tick())
	if !o.canSpawnWithinRateLimit() {
		log.Println("[RateLimit] Immediate scheduling blocked: rate limit exceeded")
		return
	}

	availableSlots := o.getAvailableWorkerSlots()
	if availableSlots <= 0 {
		return
	}

	readyNodes := o.getReadyNodes()
	if len(readyNodes) == 0 {
		return
	}

	log.Printf("📊 Immediate schedule: %d ready beads available", len(readyNodes))
	spawned := o.spawnWorkersForNodes(readyNodes, availableSlots)

	if spawned > 0 {
		log.Printf("🚀 Immediate scheduling complete: spawned %d workers", spawned)
	}
}

// getAvailableWorkerSlots returns the number of available worker slots
func (o *Overseer) getAvailableWorkerSlots() int {
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	maxWorkers := o.GetMaxWorkers()
	available := maxWorkers - activeCount

	if available <= 0 {
		log.Printf("📊 No available slots (%d/%d workers active)", activeCount, maxWorkers)
	}
	return available
}

// getReadyNodes returns nodes that are ready for execution
func (o *Overseer) getReadyNodes() []*Node {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dag.GetReadyNodes()
}

// spawnWorkersForNodes spawns workers for the given nodes up to available slots
func (o *Overseer) spawnWorkersForNodes(nodes []*Node, maxSlots int) int {
	spawned := 0
	for _, node := range nodes {
		// CRITICAL: Recalculate available slots before each spawn
		// This prevents race conditions between multiple callers
		o.workerMutex.RLock()
		currentActive := len(o.activeWorkers)
		o.workerMutex.RUnlock()

		maxWorkers := o.GetMaxWorkers()
		currentAvailable := maxWorkers - currentActive
		if maxWorkers == 0 {
			currentAvailable = 20 // Unlimited mode
		}

		// Use minimum of maxSlots (batch limit) and currentAvailable (real-time)
		effectiveLimit := maxSlots
		if currentAvailable < effectiveLimit {
			effectiveLimit = currentAvailable
		}

		if spawned >= effectiveLimit {
			if currentAvailable <= 0 {
				log.Printf("📊 spawnWorkersForNodes: No available slots (active: %d/%d), stopping", currentActive, maxWorkers)
			}
			break
		}

		if !o.shouldSpawnWorker(node) {
			continue
		}

		if err := o.spawnWorker(node.Bead); err != nil {
			log.Printf("❌ Failed to spawn worker for bead %s: %v", node.Bead.ID, err)
			continue
		}
		spawned++
	}
	return spawned
}

// shouldSpawnWorker determines if a worker should be spawned for a node
func (o *Overseer) shouldSpawnWorker(node *Node) bool {
	beadID := string(node.Bead.ID)

	if o.isBeadRunning(beadID) {
		return false
	}

	if node.Bead.Type == "approval-gate" {
		log.Printf("⏸️  Skipping approval gate: %s", beadID)
		return false
	}

	// Skip approval beads that are in progress (waiting for user approval)
	if node.Bead.Type == "approval" && node.Bead.Status == types.StatusInProgress {
		log.Printf("⏸️  Skipping approval bead (waiting for user approval): %s", beadID)
		return false
	}

	return true
}

// isBeadRunning checks if a bead is currently being executed by an active worker
func (o *Overseer) isBeadRunning(beadID string) bool {
	o.workerMutex.RLock()
	defer o.workerMutex.RUnlock()

	_, exists := o.activeWorkers[beadID]
	return exists
}

// GetActiveWorkerCount returns the number of currently active workers
func (o *Overseer) GetActiveWorkerCount() int {
	o.workerMutex.RLock()
	defer o.workerMutex.RUnlock()
	return len(o.activeWorkers)
}

// GetActiveWorkers returns a slice of all active workers
func (o *Overseer) GetActiveWorkers() []types.Worker {
	o.workerMutex.RLock()
	defer o.workerMutex.RUnlock()

	workers := make([]types.Worker, 0, len(o.activeWorkers))
	for _, worker := range o.activeWorkers {
		workers = append(workers, *worker)
	}
	return workers
}

// GetProjects returns the list of discovered projects
func (o *Overseer) GetProjects() []types.Project {
	return o.projects
}

// GetProjectPath returns the project path for a given project name
func (o *Overseer) GetProjectPath(projectName string) string {
	for _, project := range o.projects {
		if project.Name == projectName {
			return project.Path
		}
	}
	return ""
}

// IsRunning returns whether the overseer is currently running
func (o *Overseer) IsRunning() bool {
	return o.running
}

// IsPaused returns whether the overseer is paused
func (o *Overseer) IsPaused() bool {
	o.pauseMutex.RLock()
	defer o.pauseMutex.RUnlock()
	return o.paused
}

// Pause stops the overseer from spawning new workers
func (o *Overseer) Pause() {
	o.pauseMutex.Lock()
	defer o.pauseMutex.Unlock()
	o.paused = true
	log.Println("⏸️  Overseer paused")
}

// Resume allows the overseer to spawn workers again
func (o *Overseer) Resume() {
	o.pauseMutex.Lock()
	defer o.pauseMutex.Unlock()
	o.paused = false
	log.Println("▶️  Overseer resumed")
}

// SetMaxWorkers updates the maximum number of concurrent workers
func (o *Overseer) SetMaxWorkers(count int) {
	o.maxWorkersMux.Lock()
	defer o.maxWorkersMux.Unlock()
	o.maxWorkers = count
	log.Printf("🔧 Max workers set to %d", count)
}

// GetMaxWorkers returns the current maximum worker count
func (o *Overseer) GetMaxWorkers() int {
	o.maxWorkersMux.RLock()
	defer o.maxWorkersMux.RUnlock()
	return o.maxWorkers
}

// GetDAGStats returns statistics about the current DAG
func (o *Overseer) GetDAGStats() DAGStats {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return DAGStats{}
	}

	return DAGStats{
		TotalNodes:     o.dag.GetNodeCount(),
		ReadyNodes:     len(o.dag.GetReadyNodes()),
		BlockedNodes:   o.dag.GetBlockedCount(),
		CompletedNodes: o.dag.GetCompletedCount(),
		FailedNodes:    o.dag.GetFailedCount(),
	}
}

// GetReadyBeadsFromDAG returns beads that are ready for execution from the DAG
func (o *Overseer) GetReadyBeadsFromDAG() []types.Bead {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return nil
	}

	readyNodes := o.dag.GetReadyNodes()
	beads := make([]types.Bead, len(readyNodes))
	for i, node := range readyNodes {
		beads[i] = node.Bead
	}

	return beads
}

// GetBeadDependencies returns the IDs of beads that the given bead depends on.
// This is a thread-safe wrapper around the DAG's GetDependencies method.
func (o *Overseer) GetBeadDependencies(beadID string) ([]string, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return nil, fmt.Errorf("DAG not initialized")
	}

	return o.dag.GetDependencies(beadID)
}

// GetBeadDependents returns the IDs of beads that depend on the given bead.
// This is a thread-safe wrapper around the DAG's GetOutEdges method.
func (o *Overseer) GetBeadDependents(beadID string) ([]string, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return nil, fmt.Errorf("DAG not initialized")
	}

	return o.dag.GetOutEdges(beadID)
}

// GetBeadFromDAG retrieves a single bead from the DAG by its ID.
// Returns error if the bead is not found in the DAG.
func (o *Overseer) GetBeadFromDAG(beadID string) (types.Bead, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	if o.dag == nil {
		return types.Bead{}, fmt.Errorf("DAG not initialized")
	}

	node, exists := o.dag.GetNode(beadID)
	if !exists {
		return types.Bead{}, fmt.Errorf("bead %s not found in DAG", beadID)
	}

	return node.Bead, nil
}

// SetBeadDiscoveryInterval updates the bead discovery interval for the overseer
func (o *Overseer) SetBeadDiscoveryInterval(interval time.Duration) {
	o.beadDiscoveryInterval = interval
	log.Printf("🔧 Bead discovery interval set to %v", interval)
}

// GetBeadDiscoveryInterval returns the current bead discovery interval
func (o *Overseer) GetBeadDiscoveryInterval() time.Duration {
	return o.beadDiscoveryInterval
}

// SetRetryDelay updates the base retry delay for the overseer
func (o *Overseer) SetRetryDelay(delay time.Duration) {
	o.baseDelay = delay
	log.Printf("🔧 Retry delay set to %v", delay)
}

// GetRetryDelay returns the current base retry delay
func (o *Overseer) GetRetryDelay() time.Duration {
	return o.baseDelay
}

// CloseBead closes a bead using the beads client
func (o *Overseer) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return o.beadsClient.CloseBead(projectPath, repoName, beadID, reason)
}

// RefreshToolClients reinitializes tool clients based on detected config changes
// This method is thread-safe and can be called when configuration changes
func (o *Overseer) RefreshToolClients(changes []ToolConfigChange) {
	for _, change := range changes {
		o.applyToolConfigChange(change)
	}
}

// applyToolConfigChange applies a single tool configuration change
func (o *Overseer) applyToolConfigChange(change ToolConfigChange) {
	switch change.Tool {
	case ToolCassMemory:
		o.refreshCassMemoryClient(change)
	case ToolTLDR:
		o.refreshTLDRClient(change)
	case ToolGPTCache:
		o.refreshGPTCacheClient(change)
	case ToolMCP:
		o.refreshMCPClient(change)
	}
}

// refreshCassMemoryClient reinitializes the cass_memory client
func (o *Overseer) refreshCassMemoryClient(change ToolConfigChange) {
	o.intelMutex.Lock()
	defer o.intelMutex.Unlock()

	config := o.config.Intelligence.CassMemory

	if !config.Enabled {
		// Disable the client
		if o.cassClient != nil {
			o.cassClient = nil
			log.Printf("[Intelligence] cass_memory client disabled")
		}
		return
	}

	// Reinitialize the client with new settings
	o.cassClient = NewCassMemoryClient(
		"http://127.0.0.1:5000",
		config.Timeout,
		config.QueryLimit,
	)

	if change.EnabledBefore && change.EnabledAfter {
		log.Printf("[Intelligence] cass_memory client reinitialized (timeout: %v, limit: %d)",
			config.Timeout, config.QueryLimit)
	} else {
		log.Printf("[Intelligence] cass_memory client initialized (timeout: %v, limit: %d)",
			config.Timeout, config.QueryLimit)
	}
}

// refreshTLDRClient reinitializes the TLDR client
func (o *Overseer) refreshTLDRClient(change ToolConfigChange) {
	o.intelMutex.Lock()
	defer o.intelMutex.Unlock()

	config := o.config.Intelligence.TLDR

	if !config.Enabled {
		// Disable the client
		if o.tldrClient != nil {
			o.tldrClient = nil
			log.Printf("[Intelligence] TLDR client disabled")
		}
		return
	}

	// Reinitialize the client with new settings (CLI mode)
	o.tldrClient = intelligence.NewTLDRClient(
		config.BaseURL,
		true, // useCLI
		"llm-tldr",
		"",
		config.Timeout,
	)

	if change.EnabledBefore && change.EnabledAfter {
		log.Printf("[Intelligence] TLDR client reinitialized (baseURL: %s, timeout: %v)",
			config.BaseURL, config.Timeout)
	} else {
		log.Printf("[Intelligence] TLDR client initialized (baseURL: %s, timeout: %v)",
			config.BaseURL, config.Timeout)
	}
}

// refreshGPTCacheClient reinitializes the GPTCache client
func (o *Overseer) refreshGPTCacheClient(change ToolConfigChange) {
	o.intelMutex.Lock()
	defer o.intelMutex.Unlock()

	config := o.config.Intelligence.GPTCache

	if !config.Enabled {
		// Disable the client
		if o.gptCache != nil {
			o.gptCache = nil
			log.Printf("[Intelligence] GPTCache client disabled")
		}
		return
	}

	// Reinitialize the client with new settings
	o.gptCache = NewGPTCacheClient(
		config.ServerURL,
		5*time.Second,
	)

	if change.EnabledBefore && change.EnabledAfter {
		log.Printf("[Intelligence] GPTCache client reinitialized (server: %s)",
			config.ServerURL)
	} else {
		log.Printf("[Intelligence] GPTCache client initialized (server: %s)",
			config.ServerURL)
	}
}

// refreshMCPClient reinitializes the MCP client
func (o *Overseer) refreshMCPClient(change ToolConfigChange) {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	config := o.config.MCP

	// Stop existing client if running
	if o.mcpClient != nil && o.mcpClient.IsConnected() {
		if err := o.StopMCP(); err != nil {
			log.Printf("[MCP] Error stopping existing client: %v", err)
		}
	}

	if !config.Enabled {
		o.mcpClient = nil
		o.mcpReservationMgr = nil
		o.activeReservations = nil
		log.Printf("[MCP] MCP client disabled")
		return
	}

	// Reinitialize MCP with new config
	o.mcpConfig = &config
	o.activeReservations = make(map[string]*mcp.Reservation)

	// Initialize reservation manager with configured TTL
	ttl := config.FileReservationTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	o.mcpReservationMgr = mcp.NewReservationManager()

	// Initialize HTTP MCP client
	agentID := fmt.Sprintf("hydra-overseer-%d", time.Now().Unix())
	httpURL := config.URL
	if httpURL == "" {
		httpURL = fmt.Sprintf("http://127.0.0.1:%d", config.Port)
	}
	// Convert ws:// to http:// if needed
	if strings.HasPrefix(httpURL, "ws://") {
		httpURL = "http://" + strings.TrimPrefix(httpURL, "ws://")
	}
	// Remove /ws suffix if present
	httpURL = strings.TrimSuffix(httpURL, "/ws")

	var err error
	o.mcpClient, err = mcp.NewHTTPClient(httpURL, agentID)
	if err != nil {
		log.Printf("[MCP] Failed to recreate MCP client: %v", err)
		return
	}

	// Set up message handler
	o.mcpClient.SetMessageHandler(o.handleMCPMessage)

	// Start the MCP listener
	if err := o.StartMCPListener(); err != nil {
		log.Printf("[MCP] Failed to restart MCP listener: %v", err)
		return
	}

	if change.EnabledBefore && change.EnabledAfter {
		log.Printf("[MCP] MCP client reinitialized (URL: %s)", httpURL)
	} else {
		log.Printf("[MCP] MCP client initialized (URL: %s)", httpURL)
	}
}

// UpdateConfig updates the overseer's configuration and refreshes affected clients
// This method should be called when the config file changes
func (o *Overseer) UpdateConfig(newConfig *Config) {
	if newConfig == nil {
		log.Printf("[Overseer] Cannot update with nil config")
		return
	}

	// Detect which tool configs have changed
	detector := NewConfigChangeDetector()
	changes := detector.DetectChanges(o.config, newConfig)

	if len(changes) == 0 {
		log.Printf("[Overseer] No tool configuration changes detected")
		return
	}

	log.Printf("[Overseer] Detected %d tool configuration changes", len(changes))

	// Update the config reference
	o.config = newConfig

	// Refresh affected clients
	o.RefreshToolClients(changes)
}

// GetBeadsClient returns the beads client
func (o *Overseer) GetBeadsClient() BeadsClient {
	return o.beadsClient
}

// GetBVClient returns the BV client for graph analytics
func (o *Overseer) GetBVClient() *bv.Client {
	o.intelMutex.RLock()
	defer o.intelMutex.RUnlock()
	return o.bvClient
}

// GetDAG returns the current DAG instance.
// This provides access to the dependency graph for field resolvers.
func (o *Overseer) GetDAG() *DAG {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dag
}

// GetSettledStateService returns the settled-state detection service.
// This provides access to settled-state tracking for field resolvers.
func (o *Overseer) GetSettledStateService() *SettledStateService {
	return o.settledStateSvc
}

// initializeMCP initializes the MCP client and reservation manager
func (o *Overseer) initializeMCP() error {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if !o.config.MCP.Enabled {
		return nil
	}

	o.mcpConfig = &o.config.MCP
	o.activeReservations = make(map[string]*mcp.Reservation)

	// Initialize reservation manager with configured TTL
	ttl := o.config.MCP.FileReservationTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	o.mcpReservationMgr = mcp.NewReservationManager()

	// Initialize HTTP MCP client (MCP Agent Mail uses Streamable HTTP, not WebSocket)
	agentID := fmt.Sprintf("hydra-overseer-%d", time.Now().Unix())
	httpURL := o.config.MCP.URL
	if httpURL == "" {
		httpURL = fmt.Sprintf("http://127.0.0.1:%d", o.config.MCP.Port)
	}
	// Convert ws:// to http:// if needed
	if strings.HasPrefix(httpURL, "ws://") {
		httpURL = "http://" + strings.TrimPrefix(httpURL, "ws://")
	}
	// Remove /ws suffix if present
	httpURL = strings.TrimSuffix(httpURL, "/ws")

	var err error
	o.mcpClient, err = mcp.NewHTTPClient(httpURL, agentID)
	if err != nil {
		log.Printf("[MCP] Failed to create MCP client: %v", err)
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	// Set up message handler
	o.mcpClient.SetMessageHandler(o.handleMCPMessage)

	// Start the MCP listener (connects to WebSocket server)
	if err := o.StartMCPListener(); err != nil {
		log.Printf("[MCP] Failed to start MCP listener: %v", err)
		return fmt.Errorf("failed to start MCP listener: %w", err)
	}

	log.Printf("[MCP] MCP client initialized (URL: %s, Agent: %s)", httpURL, agentID)
	return nil
}

// StartMCPListener connects to the MCP server and starts listening for messages
// Note: Caller must hold o.mcpMu lock before calling this function
func (o *Overseer) StartMCPListener() error {
	// Note: mcpMu lock should be held by caller (initializeMCP)
	if o.mcpClient == nil {
		return fmt.Errorf("MCP client not initialized")
	}

	if err := o.mcpClient.Connect(); err != nil {
		log.Printf("[MCP] Failed to connect: %v", err)
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	// Register the overseer as an agent
	metadata := mcp.RegistrationMetadata{
		AgentID:   "hydra-overseer",
		Type:      "orchestrator",
		StartTime: time.Now(),
		Metadata: map[string]string{
			"role":    "overseer",
			"version": "1.0.0",
		},
	}

	if err := o.mcpClient.Register(metadata); err != nil {
		log.Printf("[MCP] Failed to register: %v", err)
		// Continue even if registration fails
	} else {
		log.Printf("[MCP] Successfully registered with MCP server")
	}

	return nil
}

// StopMCP disconnects from the MCP server
func (o *Overseer) StopMCP() error {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if o.mcpClient == nil {
		return nil
	}

	// Deregister before disconnecting
	if o.mcpClient.IsConnected() {
		if err := o.mcpClient.Deregister(mcp.RegistrationMetadata{}); err != nil {
			log.Printf("[MCP] Failed to deregister: %v", err)
		}
	}

	if err := o.mcpClient.Disconnect(); err != nil {
		log.Printf("[MCP] Error during disconnect: %v", err)
		return err
	}

	log.Printf("[MCP] Disconnected from MCP server")
	return nil
}

// handleMCPMessage processes incoming MCP messages
func (o *Overseer) handleMCPMessage(msg mcp.Message) {
	log.Printf("[MCP] Received message: type=%s, from=%s", msg.Type, msg.From)

	switch msg.Type {
	case mcp.MsgTypeTaskComplete:
		o.handleTaskCompleteMessage(&msg)
	case mcp.MsgTypeTaskStart:
		o.handleTaskStartMessage(&msg)
	case mcp.MsgTypeHeartbeat:
		// Heartbeat messages are handled automatically
		log.Printf("[MCP] Heartbeat from %s", msg.From)
	case mcp.MsgTypeLog:
		o.handleLogMessage(&msg)
	case mcp.MsgTypeFileReserved:
		o.handleFileReservedMessage(&msg)
	case mcp.MsgTypeFileReleased:
		o.handleFileReleasedMessage(&msg)
	default:
		log.Printf("[MCP] Unknown message type: %s", msg.Type)
	}
}

// HandleMCPMessage processes an MCP message (public API)
func (o *Overseer) HandleMCPMessage(msg *mcp.Message) error {
	if msg == nil {
		return fmt.Errorf("nil message")
	}
	o.handleMCPMessage(*msg)
	return nil
}

// handleTaskCompleteMessage processes task completion messages from workers
func (o *Overseer) handleTaskCompleteMessage(msg *mcp.Message) {
	beadID, ok := msg.Payload["bead_id"].(string)
	if !ok || beadID == "" {
		log.Printf("[MCP] Task complete message missing bead_id")
		return
	}

	status, _ := msg.Payload["status"].(string)
	exitCode := 0
	if code, ok := msg.Payload["exit_code"].(float64); ok {
		exitCode = int(code)
	}

	log.Printf("[MCP] Task complete: bead=%s, status=%s, exit_code=%d", beadID, status, exitCode)

	// Trigger scheduling cycle for next tasks
	go o.triggerImmediateSchedule()
}

// handleTaskStartMessage processes task start messages
func (o *Overseer) handleTaskStartMessage(msg *mcp.Message) {
	beadID, ok := msg.Payload["bead_id"].(string)
	if !ok || beadID == "" {
		return
	}

	log.Printf("[MCP] Task started: bead=%s, agent=%s", beadID, msg.From)
}

// handleLogMessage processes log messages from agents
func (o *Overseer) handleLogMessage(msg *mcp.Message) {
	level, _ := msg.Payload["level"].(string)
	message, _ := msg.Payload["message"].(string)
	beadID, _ := msg.Payload["bead_id"].(string)

	log.Printf("[MCP Log] [%s] [bead=%s] %s", level, beadID, message)
}

// handleFileReservedMessage processes file reservation messages
func (o *Overseer) handleFileReservedMessage(msg *mcp.Message) {
	beadID, _ := msg.Payload["bead_id"].(string)
	filePath, _ := msg.Payload["file_path"].(string)

	log.Printf("[MCP] File reserved: bead=%s, file=%s", beadID, filePath)
}

// handleFileReleasedMessage processes file release messages
func (o *Overseer) handleFileReleasedMessage(msg *mcp.Message) {
	beadID, _ := msg.Payload["bead_id"].(string)
	filePath, _ := msg.Payload["file_path"].(string)
	committed, _ := msg.Payload["committed"].(bool)

	log.Printf("[MCP] File released: bead=%s, file=%s, committed=%v", beadID, filePath, committed)
}

// GetActiveReservations returns all active file reservations
func (o *Overseer) GetActiveReservations() []*mcp.Reservation {
	o.mcpMu.RLock()
	defer o.mcpMu.RUnlock()

	if o.mcpReservationMgr == nil {
		return nil
	}

	allRes := o.mcpReservationMgr.GetAllReservations()
	var active []*mcp.Reservation
	for _, res := range allRes {
		if !res.IsExpired() {
			active = append(active, res)
		}
	}

	return active
}

// ForceReleaseReservation forcibly releases a reservation by ID
func (o *Overseer) ForceReleaseReservation(reservationID string) error {
	o.mcpMu.Lock()
	defer o.mcpMu.Unlock()

	if o.mcpReservationMgr == nil {
		return fmt.Errorf("reservation manager not initialized")
	}

	o.mcpReservationMgr.RemoveReservation(reservationID)
	log.Printf("[MCP] Force released reservation: %s", reservationID)
	return nil
}

// BroadcastToWorkers sends a broadcast message to all connected workers
func (o *Overseer) BroadcastToWorkers(msgType string, payload map[string]interface{}) error {
	o.mcpMu.RLock()
	client := o.mcpClient
	o.mcpMu.RUnlock()

	if client == nil {
		return fmt.Errorf("MCP client not initialized")
	}

	if !client.IsConnected() {
		return fmt.Errorf("MCP client not connected")
	}

	if err := client.Broadcast(msgType, payload); err != nil {
		return fmt.Errorf("failed to broadcast message: %w", err)
	}

	log.Printf("[MCP] Broadcast message sent: type=%s", msgType)
	return nil
}

// SendLogToMCP sends a log message to the MCP server
func (o *Overseer) SendLogToMCP(level, message string) error {
	o.mcpMu.RLock()
	client := o.mcpClient
	o.mcpMu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("MCP client not available")
	}

	return client.SendLog("", level, message)
}

// GetMCPStatus returns the current MCP connection status
func (o *Overseer) GetMCPStatus() mcp.ClientStatus {
	o.mcpMu.RLock()
	defer o.mcpMu.RUnlock()

	if o.mcpClient == nil {
		return mcp.ClientStatus{
			Enabled:   o.config.MCP.Enabled,
			Connected: false,
		}
	}

	return o.mcpClient.GetStatus()
}

// IsMCPConnected returns whether the MCP client is connected
func (o *Overseer) IsMCPConnected() bool {
	o.mcpMu.RLock()
	defer o.mcpMu.RUnlock()

	if o.mcpClient == nil {
		return false
	}

	return o.mcpClient.IsConnected()
}

// sendMCPWorkerCompletion sends a task completion message via MCP
func (o *Overseer) sendMCPWorkerCompletion(worker *types.Worker, success bool, exitCode int, duration time.Duration) {
	o.mcpMu.RLock()
	client := o.mcpClient
	o.mcpMu.RUnlock()

	if client == nil || !client.IsConnected() {
		return
	}

	status := "success"
	if !success {
		if o.getRetryCount(worker.ID) >= o.config.Overseer.DefaultRetries {
			status = "failure"
		} else {
			status = "partial"
		}
	}

	payload := map[string]interface{}{
		"bead_id":    worker.ID,
		"status":     status,
		"exit_code":  exitCode,
		"duration":   duration.String(),
		"agent_type": string(worker.AgentType),
	}

	if err := client.Broadcast(mcp.MsgTypeTaskComplete, payload); err != nil {
		log.Printf("[MCP] Failed to send task completion: %v", err)
	} else {
		log.Printf("[MCP] Task completion broadcast: bead=%s, status=%s", worker.ID, status)
	}
}

// spawnWorkerWithContextEnrichment spawns a worker with CASS context enrichment
// It searches CASS for relevant sessions based on bead title and includes them in environment
func (o *Overseer) spawnWorkerWithContextEnrichment(bead types.Bead) (*types.Worker, error) {
	// Check if CASS client has search capability (for testing)
	if o.cassClient != nil && o.cassClient.searchClient != nil {
		// Search CASS for relevant sessions
		if o.cassClient.searchClient != nil && o.cassClient.searchClient.IsEnabled() {
			// Search with 2 second timeout and limit of 3
			result, err := o.cassClient.searchClient.SearchSessions(bead.Title, 3, 2*time.Second)
			if err == nil && result != nil && len(result.Hits) > 0 {
				// Format sessions as context
				sessions := make([]map[string]interface{}, 0, len(result.Hits))
				for _, hit := range result.Hits {
					sessions = append(sessions, map[string]interface{}{
						"agent":       hit.Agent,
						"title":       hit.Title,
						"content":     hit.Content,
						"score":       hit.Score,
						"source_path": hit.SourcePath,
						"line_number": hit.LineNumber,
						"created_at":  hit.CreatedAt,
					})
				}

				// Marshal to JSON and store in metadata
				if contextJSON, err := json.Marshal(sessions); err == nil {
					if bead.Metadata == nil {
						bead.Metadata = make(map[string]string)
					}
					bead.Metadata["HYDRA_CASS_CONTEXT"] = string(contextJSON)
				}
			}
		}
	}

	// Call spawnWorkerInternal to create the worker
	return o.spawnWorkerInternal(bead)
}

// buildWorkerEnvironmentWithContext builds worker environment with CASS context enrichment
// It searches CASS for sessions related to the bead and includes top results
func (o *Overseer) buildWorkerEnvironmentWithContext(bead types.Bead, cassClient interface{}) []string {
	// Start with basic environment
	env := []string{
		fmt.Sprintf("HYDRA_BEAD_ID=%s", bead.ID),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_BEAD_TITLE=%s", bead.Title),
		fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", bead.Description),
	}

	// If CASS client is provided and enabled, search for relevant sessions
	if cassClient != nil {
		if searcher, ok := cassClient.(interface {
			SearchSessions(query string, limit int, timeout time.Duration) (*CASSSearchResult, error)
			IsEnabled() bool
		}); ok && searcher.IsEnabled() {
			// Search with 2 second timeout as per requirements
			result, err := searcher.SearchSessions(bead.Title, 3, 2*time.Second)
			if err == nil && result != nil && len(result.Hits) > 0 {
				// Marshal the entire result (including Count and TotalMatches) to JSON and add to environment
				if contextJSON, err := json.Marshal(result); err == nil {
					env = append(env, fmt.Sprintf("HYDRA_CASS_CONTEXT=%s", string(contextJSON)))
				}
			}
		}
	}

	return env
}

// runPiSetup initializes and runs the Pi ecosystem setup
func (o *Overseer) runPiSetup() error {
	log.Println("[Overseer] Initializing Pi ecosystem setup...")

	// Import the pi package and create setup manager
	configDir := o.config.Pi.ConfigDir
	piConfigDir := o.config.Pi.PiConfigDir
	if configDir == "" {
		configDir = ""
	}
	if piConfigDir == "" {
		piConfigDir = "pi-config"
	}
	setupManager := pi.NewSetupManager(configDir, piConfigDir)

	// Run setup
	result, err := setupManager.RunSetup()
	if err != nil {
		return fmt.Errorf("pi setup failed: %w", err)
	}

	// Log results
	if result.PiInstalled {
		log.Println("[Overseer] ✅ Pi agent ready")
	} else {
		log.Println("[Overseer] ⚠️  Pi agent not installed")
	}

	if result.LSPInstalled {
		log.Println("[Overseer] ✅ LSP servers ready")
	} else {
		log.Println("[Overseer] ⚠️  LSP servers may be missing")
	}

	if result.SkillsInstalled {
		log.Println("[Overseer] ✅ Pi skills ready")
	} else {
		log.Println("[Overseer] ⚠️  Pi skills may be missing")
	}

	if len(result.Errors) > 0 {
		log.Printf("[Overseer] Pi setup completed with %d errors", len(result.Errors))
		for _, err := range result.Errors {
			log.Printf("[Overseer]   - %s", err)
		}
	}

	return nil
}
