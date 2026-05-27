package core

import (
	"bufio"
	"context"
	"encoding/json"
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
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/internal/pi"
	"github.com/haulmatic-dev/hydra/internal/services"
	"github.com/haulmatic-dev/hydra/internal/validation"
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

// Pattern represents a learned pattern from Hindsight
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

// BeadsClient defines the interface for beads operations
// This allows for mocking in tests
type BeadsClient interface {
	ListBeads(projectPath, repoName string) ([]types.Bead, error)
	GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error)
	CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error
	GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error)
	UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error
	CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error)
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
	config          *Config
	projects        []types.Project
	activeWorkers   map[string]*types.Worker
	workerMutex     sync.RWMutex
	stateController *StateController // Runtime state (running, paused, maxWorkers, interval)
	configManager   *ConfigManager   // Dynamic configuration management
	beadsClient     BeadsClient
	spawner         WorkerSpawner // Interface for spawning workers (allows mocking in tests)
	dag             *DAG
	dagMu           sync.RWMutex
	dagNavigator    *DAGNavigator
	completed       map[string]bool  // Track completed beads for this session
	retryManager    *RetryManager    // Retry and validation-retry tracking
	intelligenceHub *IntelligenceHub // Encapsulates TLDR, GPTCache, Hindsight clients
	// TODO: Migrate these fields to IntelligenceHub
	tldrClient         *intelligence.TLDRClient        // TLDR client for impact analysis
	gptCache           *GPTCacheClient                 // GPTCache client for caching
	hindsightClient    HindsightClient                 // Hindsight client for pre-execution context retrieval
	batchRetentionSvc  *services.BatchRetentionService // Batch retention service for efficient memory storage
	hindsightLifecycle *services.HindsightLifecycle    // Hindsight lifecycle manager for memory management
	intelMutex         sync.RWMutex                    // Mutex for intelligence clients
	bvClient           *bv.Client                      // BV client for graph analytics
	settledStateSvc    *SettledStateService            // Settled-state detection service

	// MCP (Model Context Protocol) fields
	mcpClient          *mcp.HTTPClient             // MCP HTTP client (Streamable HTTP)
	mcpReservationMgr  *mcp.ReservationManager     // MCP reservation manager
	mcpConfig          *MCPConfig                  // MCP configuration
	activeReservations map[string]*mcp.Reservation // Active file reservations
	mcpMu              sync.RWMutex                // Mutex for MCP operations
	mcpMessageHandler  func(mcp.Message)           // Handler for incoming MCP messages

	// Resource monitoring for adaptive scaling
	resourceMonitor *ResourceMonitor // Monitors CPU/Memory usage

	// Orphan process janitor for cleaning up orphaned worker processes
	orphanJanitor *Charon // Cleans up orphaned worker processes

	// Chain completion detector for end-of-chain pattern extraction
	chainCompletionDetector *ChainCompletionDetector // Detects chain completion and triggers pattern extraction
	chainRegistrar          *ChainRegistrar          // Registers bead chains from issues.jsonl
	chainsRegistered        bool                     // Track if chains have been registered (only once at startup)

	// DAG builder for dependency graph construction
	dagBuilder *DAGBuilder // Builds DAG from project beads

	// Impact analyzer for TLDR-based impact analysis
	impactAnalyzer *ImpactAnalyzer // Analyzes code impact using TLDR

	// Worker tracker for persistent PID tracking
	workerTracker *WorkerTracker // Tracks worker PIDs to identify Hydra workers

	// Spawn rate limiting and backoff
	spawnRateLimiter *SpawnRateLimiter // Tracks spawn timestamps and failure backoff

	// Agent connector for modular agent support
	agentConnector agents.AgentConnector // Agent connector interface
	agentConnMu    sync.RWMutex          // Mutex for agent connector

	// Skill updater for periodic skill updates
	skillUpdater *pi.SkillUpdater // Skill updater for background updates

	// TLDR indexer for deterministic code indexing
	tldrIndexer *TLDRIndexer // Indexes repositories after code changes

	// Epic scanner for periodic epic auto-close
	epicScanner *EpicScanner // Scans and closes epics when all tasks complete

	// Validation support for end-of-chain verification
	validator *validation.Validator // Validates bead implementations before closure

	// Bead file reader for fallback bead lookups
	beadFileReader *BeadFileReader

	// Bead analyzer for metadata extraction
	beadAnalyzer *BeadAnalyzer

	// ErrorLogger provides structured logging
	errorLogger *ErrorLogger
}

// NewOverseer creates a new overseer instance
func NewOverseer(config *Config) *Overseer {
	maxRetries := config.Overseer.DefaultRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	o := &Overseer{
		config:           config,
		activeWorkers:    make(map[string]*types.Worker),
		stateController:  NewStateController(config.Overseer.MaxWorkers, config.Overseer.BeadDiscoveryInterval),
		beadsClient:      beads.NewClient(),
		bvClient:         bv.NewClient(),
		spawner:          &defaultWorkerSpawner{},
		dag:              NewDAG(),
		dagNavigator:     NewDAGNavigator(NewDAG()),
		completed:        make(map[string]bool),
		retryManager:     NewRetryManager(defaultBaseDelay, defaultMaxDelay),
		spawnRateLimiter: NewSpawnRateLimiter(),
		intelligenceHub:  NewIntelligenceHub(config.Intelligence),
	}

	// Initialize intelligence hub
	o.intelligenceHub.Initialize()

	// Initialize config manager
	o.configManager = NewConfigManager(o)

	// Initialize DAG builder
	o.dagBuilder = NewDAGBuilder(o.errorLogger)

	// Initialize impact analyzer
	o.impactAnalyzer = NewImpactAnalyzer(o.findProjectPath)

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

	// Initialize validation service
	o.validator = validation.NewValidator(validation.DefaultValidationConfig())
	log.Printf("[Validation] Initialized bead validation service")

	// Initialize error logger
	o.errorLogger = NewErrorLogger(o.retryManager.GetRetryCount)

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
		o.retryManager.SetMaxDelay(config.Overseer.WorkerTimeout / 6) // Reasonable fraction
		if o.retryManager.MaxDelay() > defaultMaxDelay {
			o.retryManager.SetMaxDelay(defaultMaxDelay)
		}
	}

	// Initialize epic scanner (5 minute interval)
	o.epicScanner = NewEpicScanner(5*time.Minute, o.beadsClient)
	log.Printf("[EpicScanner] Initialized with 5 minute scan interval")

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

	// Set up Hindsight reflector for settled-state reflection
	hindsightClient := o.intelligenceHub.Hindsight()
	if hindsightClient != nil && hindsightClient.IsEnabled() {
		o.settledStateSvc.SetReflector(hindsightClient)
		log.Printf("[SettledState] Hindsight reflection enabled")
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

		// Run PRD validation
		projectPath := o.findProjectPathForBeadID(event.ChainInfo.RootBeadID)
		if projectPath != "" {
			if err := o.RunPRDValidation(event.ChainInfo, projectPath); err != nil {
				log.Printf("[Overseer] PRD validation failed: %v", err)
			}
		}

		if err := extractor.ExtractPatterns(event); err != nil {
			log.Printf("[Overseer] Chain pattern extraction failed: %v", err)
		}
	})

	log.Printf("[ChainCompletion] Detector initialized (interval: %v, timeout: %v)",
		config.DetectionInterval, config.Timeout)
}

// analyzeImpactWithTLDR performs impact analysis using TLDR service
func (o *Overseer) analyzeImpactWithTLDR(client *intelligence.TLDRClient, bead *types.Bead, files []string) (*ImpactAnalysis, error) {
	return o.impactAnalyzer.Analyze(client, bead, files)
}

// findProjectPath finds the project path for a given bead.
// Falls back to beadAnalyzer if initialized, otherwise uses local projects.
func (o *Overseer) findProjectPath(bead *types.Bead) string {
	if o.beadAnalyzer != nil {
		return o.beadAnalyzer.FindProjectPath(bead)
	}
	for _, project := range o.projects {
		if project.Name == bead.Project {
			if bead.Repo != "" && project.Type == types.RepoTypeMulti {
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
func (o *Overseer) isIntelligenceEnabled() bool {
	return o.config.Intelligence.TLDR.Enabled || o.config.Intelligence.GPTCache.Enabled
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
	projectScanner := NewProjectScanner(o.config)
	if err := projectScanner.Discover(); err != nil {
		return fmt.Errorf("failed to discover projects: %w", err)
	}
	o.projects = projectScanner.Projects()

	log.Printf("📁 Discovered %d projects", len(o.projects))

	// Initialize bead file reader and analyzer
	o.beadFileReader = NewBeadFileReader(o.projects)
	o.beadAnalyzer = NewBeadAnalyzer(o.projects)

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
	if indexer := o.intelligenceHub.Indexer(); indexer != nil {
		indexer.Start()
		log.Println("📇 TLDR indexer started")

		// Queue initial indexing for all project repos
		o.intelligenceHub.QueueInitialIndexing(o.projects)
	}

	// Start epic scanner
	if o.epicScanner != nil {
		o.epicScanner.Start()
		log.Println("🏛️  Epic scanner started")
	}

	o.stateController.SetRunning(true)
	ticker := time.NewTicker(o.stateController.GetBeadDiscoveryInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			o.stateController.SetRunning(false)
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
			if indexer := o.intelligenceHub.Indexer(); indexer != nil {
				indexer.Stop()
				log.Println("📇 TLDR indexer stopped")
			}
			// Stop epic scanner
			if o.epicScanner != nil {
				o.epicScanner.Stop()
				log.Println("🏛️  Epic scanner stopped")
			}
			return nil
		case <-ticker.C:
			if err := o.tick(); err != nil {
				log.Printf("Error during tick: %v", err)
			}
		}
	}
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
		o.errorLogger.GracefulDegradation("Overseer", "buildDAG", "using existing DAG", err)
		// Continue with existing DAG if rebuild fails
	}

	// Register chains only ONCE at startup (not on every discovery cycle)
	// This is called from the main loop but we track registration state
	if o.chainCompletionDetector != nil && !o.chainsRegistered {
		o.chainRegistrar.RegisterFromProjects(o.projects)
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
		o.chainRegistrar.RegisterFromProjects(o.projects)
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
		if count, _, _, exists := o.retryManager.RetryInfo(beadIDStr); exists {
			log.Printf("[Retry] Attempting retry %d/%d for bead %s",
				count+1, o.config.Overseer.DefaultRetries, bead.ID)
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
		projectPath := o.beadAnalyzer.FindProjectPath(&bead)
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
		if !o.spawnRateLimiter.CanSpawn() {
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
				o.retryManager.RecordRetry(beadIDStr, err)
			}
			o.errorLogger.Error("Overseer", "spawnWorker", beadIDStr, err, "error")
			continue
		}

		// Record successful spawn for rate limiting
		o.spawnRateLimiter.RecordSpawn()

		// Clear retry state on successful spawn
		o.retryManager.ClearRetry(beadIDStr)
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
	retrySnapshot := o.retryManager.Snapshot()
	if len(retrySnapshot) == 0 {
		return readyBeads
	}

	// Create a map of already ready beads for deduplication
	readyMap := make(map[string]types.Bead, len(readyBeads))
	for _, bead := range readyBeads {
		readyMap[string(bead.ID)] = bead
	}

	// Check failed beads for retry eligibility
	for beadID := range retrySnapshot {
		// Skip if already in ready pool
		if _, exists := readyMap[beadID]; exists {
			continue
		}

		if !o.retryManager.ShouldRetry(beadID, o.config.Overseer.DefaultRetries) {
			continue
		}

		count, _, _, _ := o.retryManager.RetryInfo(beadID)
		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}

		// Try to fetch the bead from the DAG
		if bead, err := o.getBeadFromDAG(beadID); err == nil {
			readyBeads = append(readyBeads, bead)
			log.Printf("[Retry] Bead %s added to retry queue (attempt %d/%d)",
				beadID, count+1, maxRetries)
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

	if o.dag == nil {
		log.Printf("[DAG] DAG was nil, creating new instance")
		o.dag = NewDAG()
		o.dagNavigator = NewDAGNavigator(o.dag)
	}

	if err := o.dagBuilder.Build(o.dag, o.beadsClient, o.projects); err != nil {
		return err
	}

	return nil
}

// GetBeadFromFile reads a single bead from issues.jsonl
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

// getReadyBeads retrieves beads that are ready to execute.
// Priority:
//  1. bv --robot-plan (dependency-respecting tracks with impact scoring)
//  2. br ready (all unblocked beads, maximum parallelism)
//  3. bv --robot-triage (intelligent prioritization fallback)
func (o *Overseer) getReadyBeads() ([]types.Bead, error) {
	// 1. Try bv --robot-plan first for dependency-respecting scheduling
	if o.bvClient != nil {
		planBeads, planErr := o.getReadyBeadsFromBVPlan()
		if planErr != nil {
			log.Printf("⚠️  [BV-Plan] failed: %v", planErr)
		}
		if planErr == nil && len(planBeads) > 0 {
			log.Printf("✅ Using bv robot-plan: found %d actionable beads", len(planBeads))
			return planBeads, nil
		}
	}

	// 2. Fallback to br ready for maximum parallelism
	brBeads, brErr := o.getReadyBeadsFromBR()
	if brErr != nil {
		log.Printf("⚠️  br ready failed: %v", brErr)
	}
	if brErr == nil && len(brBeads) > 0 {
		log.Printf("✅ Using br ready: found %d unblocked beads", len(brBeads))
		return brBeads, nil
	}

	// 3. Fallback to bv --robot-triage for intelligent prioritization
	if o.bvClient != nil {
		log.Printf("⚠️  br ready failed or returned 0 beads, falling back to bv triage")
		beads, err := o.getReadyBeadsFromBV()
		if err != nil {
			return nil, fmt.Errorf("bv triage failed: %w", err)
		}
		return beads, nil
	}

	if brErr != nil {
		return nil, fmt.Errorf("all bead discovery methods failed: br: %w", brErr)
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
	filteredCounts := map[string]int{
		"activeWorkers":      0,
		"completed":          0,
		"approvalGate":       0,
		"approvalInProgress": 0,
	}
	for _, bead := range allReadyBeads {
		beadIDStr := string(bead.ID)

		// Filter: not already running
		if _, isRunning := o.activeWorkers[beadIDStr]; isRunning {
			log.Printf("[FilterDebug] Filtering out %s: already in activeWorkers", beadIDStr)
			filteredCounts["activeWorkers"]++
			continue
		}

		// Filter: not already completed in this session
		if o.completed[beadIDStr] {
			log.Printf("[FilterDebug] Filtering out %s: already completed in this session", beadIDStr)
			filteredCounts["completed"]++
			continue
		}

		// Skip approval gates
		if bead.Type == "approval-gate" {
			log.Printf("[FilterDebug] Filtering out %s: approval-gate type", beadIDStr)
			filteredCounts["approvalGate"]++
			continue
		}

		// Skip approval beads that are in progress (waiting for user approval)
		if bead.Type == "approval" && bead.Status == types.StatusInProgress {
			log.Printf("[FilterDebug] Filtering out %s: approval in progress", beadIDStr)
			filteredCounts["approvalInProgress"]++
			continue
		}

		// Note: br ready already returns beads with satisfied dependencies
		// The bead.Dependencies field shows all dependencies, but br ready ensures
		// they are resolved. We don't need to filter here - br ready did it already.

		readyBeads = append(readyBeads, bead)
	}
	o.workerMutex.RUnlock()

	// Log filter statistics
	if len(allReadyBeads) > 0 && len(readyBeads) < len(allReadyBeads) {
		log.Printf("[FilterDebug] Filtered out %d beads: activeWorkers=%d, completed=%d, approvalGate=%d, approvalInProgress=%d",
			len(allReadyBeads)-len(readyBeads),
			filteredCounts["activeWorkers"],
			filteredCounts["completed"],
			filteredCounts["approvalGate"],
			filteredCounts["approvalInProgress"])
	}

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

// getReadyBeadsFromBVPlan retrieves beads using bv --robot-plan.
// It returns parallel execution tracks with dependency-respecting ordering,
// critical-path awareness, and impact scoring (unblocks count).
func (o *Overseer) getReadyBeadsFromBVPlan() ([]types.Bead, error) {
	if o.bvClient == nil {
		return nil, fmt.Errorf("bv client not initialized")
	}

	var allReadyBeads []types.Bead

	for _, project := range o.projects {
		plan, err := o.bvClient.GetPlan(project.Path)
		if err != nil {
			log.Printf("⚠️  [BV-Plan] GetPlan failed for %s: %v", project.Name, err)
			continue
		}

		log.Printf("[BV-Plan] %s: %d tracks, %d actionable, %d blocked, highest_impact=%s",
			project.Name, len(plan.Tracks), plan.TotalActionable, plan.TotalBlocked, plan.Summary.HighestImpact)

		for _, track := range plan.Tracks {
			for _, item := range track.Items {
				beadID := types.BeadID(item.ID)
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

				// Fetch full bead details
				var repoName string
				if project.Type != types.RepoTypeSingle && len(project.Repos) > 0 {
					repoName = project.Repos[0].Name
				}

				bead, err := o.beadsClient.GetBead(project.Path, repoName, beadID)
				if err != nil {
					log.Printf("⚠️  [BV-Plan] Could not get bead details for %s: %v", beadID, err)
					// Fallback: create minimal bead from plan data
					bead = &types.Bead{
						ID:       beadID,
						Title:    item.Title,
						Priority: item.Priority,
						Project:  project.Name,
						Repo:     repoName,
						Status:   types.StatusOpen,
						Metadata: map[string]string{
							"bv_track":  track.TrackID,
							"bv_source": "robot-plan",
						},
					}
				}

				// Skip approval gates
				if bead.Type == "approval-gate" {
					continue
				}

				// Skip approval beads in progress
				if bead.Type == "approval" && bead.Status == types.StatusInProgress {
					continue
				}

				if bead.Metadata == nil {
					bead.Metadata = make(map[string]string)
				}
				bead.Metadata["bv_track"] = track.TrackID
				bead.Metadata["bv_source"] = "robot-plan"
				if len(item.Unblocks) > 0 {
					bead.Metadata["bv_unblocks"] = strings.Join(item.Unblocks, ",")
				}

				allReadyBeads = append(allReadyBeads, *bead)
			}
		}
	}

	log.Printf("📋 [BV-Plan] Found %d ready beads from plan (after filtering)", len(allReadyBeads))
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
	if o.spawnRateLimiter.shouldBackoff() {
		backoff := time.Duration(1<<uint(o.spawnRateLimiter.FailureCount()-1)) * time.Second
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
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
					o.errorLogger.Error("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
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
			o.errorLogger.Error("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
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
	if count, lastErr, _, exists := o.retryManager.RetryInfo(beadIDStr); exists {
		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}
		if count >= maxRetries {
			err := &RetryExhaustedError{
				BeadID:    beadIDStr,
				Attempts:  count,
				LastError: lastErr,
			}
			o.errorLogger.Error("spawnWorker", "retry_exhausted", beadIDStr, err, "error")
			return err
		}
	}

	// Create the worker using the worker binary with Pi agent support
	// This ensures Hindsight memory retention happens after execution
	worker, err := o.spawnWorkerInternal(bead)
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
	o.spawnRateLimiter.ClearFailures()

	return nil
}

// HindsightClient defines the interface for Hindsight memory operations
type HindsightClient interface {
	SearchByBead(title, beadType string) (*hindsight.SearchResponse, error)
	Reflect(beadID types.BeadID, context map[string]interface{}) ([]hindsight.ReflectionInsight, error)
	Retain(memory *hindsight.ExecutionMemory) (*hindsight.RetentionResult, error)
	IsEnabled() bool
}

// spawnWorkerInternal creates and starts a real worker process for a bead
// This is the actual implementation used by defaultWorkerSpawner
func (o *Overseer) spawnWorkerInternal(bead types.Bead) (*types.Worker, error) {
	beadIDStr := bead.ID.String()

	// Determine agent type
	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) == 0 {
		err := fmt.Errorf("no agent type found for bead type: %s", bead.Type)
		o.errorLogger.Error("spawnWorker", "agent_type", beadIDStr, err, "error")
		return nil, err
	}

	agentType := agentTypes[0] // Use primary agent

	retryCount := o.retryManager.GetRetryCount(beadIDStr)
	if retryCount > 0 {
		log.Printf("[Worker] Spawning retry worker for bead: %s (attempt %d, agent: %s)", beadIDStr, retryCount+1, agentType)
	} else {
		log.Printf("[Worker] Spawning worker for bead: %s (agent: %s)", beadIDStr, agentType)
	}

	// Find project path
	projectPath := o.beadAnalyzer.FindProjectPath(&bead)
	if projectPath == "" {
		projectPath = bead.Project // Fallback to project name
	}

	// Mark bead as in_progress before spawning worker
	if o.beadsClient != nil && projectPath != "" {
		log.Printf("[Overseer] Marking bead %s as in_progress", beadIDStr)
		if err := o.beadsClient.UpdateBeadStatus(projectPath, bead.ID, types.StatusInProgress); err != nil {
			// Non-fatal: bead may already be in_progress or locked by concurrent process
			log.Printf("[Overseer] Debug: could not mark bead %s as in_progress: %v", beadIDStr, err)
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

	// Add full bead JSON for enhanced worker context
	if bead.ID != "" {
		// Filter empty dependency entries that break BeadID unmarshaling
		cleanBead := bead
		cleanDeps := make([]types.BeadID, 0, len(bead.Dependencies))
		for _, dep := range bead.Dependencies {
			if dep != "" {
				cleanDeps = append(cleanDeps, dep)
			}
		}
		cleanBead.Dependencies = cleanDeps
		beadJSON, err := json.Marshal(cleanBead)
		if err == nil {
			env = append(env, fmt.Sprintf("HYDRA_BEAD_JSON=%s", string(beadJSON)))
		} else {
			log.Printf("[WARN] Failed to marshal bead to JSON: %v", err)
		}
	}

	// Pass API key to worker if configured
	if o.config != nil && o.config.Agents.Pi.ApiKeyName != "" {
		if apiKey := os.Getenv(o.config.Agents.Pi.ApiKeyName); apiKey != "" {
			env = append(env, fmt.Sprintf("%s=%s", o.config.Agents.Pi.ApiKeyName, apiKey))
		}
	}

	// Add Hindsight configuration for memory-based context retrieval
	if o.config != nil && o.config.Intelligence.Hindsight.Enabled {
		h := o.config.Intelligence.Hindsight
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_ENABLED=true"))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_BASE_URL=%s", h.BaseURL))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_TIMEOUT=%s", h.Timeout))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_LIMIT=%d", h.Limit))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_MIN_SCORE=%.2f", h.MinScore))
		log.Printf("[Worker] Hindsight config passed to worker (baseURL: %s)", h.BaseURL)
	}

	// Add Pi configuration for Pi agent execution (always enabled)
	p := o.config.Agents.Pi
	// Fallback to pi.standard-agent.llm if agents.pi is not set
	std := o.config.Pi.StandardAgent.LLM
	if p.Provider == "" && std.Provider != "" {
		p.Provider = std.Provider
	}
	if p.Model == "" && std.Model != "" {
		p.Model = std.Model
	}
	if p.ThinkingLevel == "" && std.ThinkingLevel != "" {
		p.ThinkingLevel = std.ThinkingLevel
	}
	if p.Timeout == "" && std.Timeout != "" {
		p.Timeout = std.Timeout
	}
	if p.ApiKeyName == "" && std.ApiKeyName != "" {
		p.ApiKeyName = std.ApiKeyName
	}
	if p.Provider == "" {
		p.Provider = "kimi-coding"
	}
	if p.Model == "" {
		p.Model = "kimi-for-coding"
	}
	if p.ThinkingLevel == "" {
		p.ThinkingLevel = "on"
	}
	if p.Timeout == "" {
		p.Timeout = "10m"
	}
	if p.ApiKeyName == "" {
		p.ApiKeyName = "KIMI_API_KEY"
	}
	env = append(env, fmt.Sprintf("HYDRA_PI_ENABLED=true"))
	env = append(env, fmt.Sprintf("HYDRA_PI_PROVIDER=%s", p.Provider))
	env = append(env, fmt.Sprintf("HYDRA_PI_MODEL=%s", p.Model))
	env = append(env, fmt.Sprintf("HYDRA_PI_THINKING_LEVEL=%s", p.ThinkingLevel))
	env = append(env, fmt.Sprintf("HYDRA_PI_TIMEOUT=%s", p.Timeout))
	env = append(env, fmt.Sprintf("HYDRA_PI_API_KEY_NAME=%s", p.ApiKeyName))
	env = append(env, fmt.Sprintf("HYDRA_PI_CONFIG_DIR=%s", o.config.Pi.ConfigDir))
	log.Printf("[Worker] Pi config passed to worker (provider: %s, model: %s)", p.Provider, p.Model)

	cmd.Env = env

	// Capture stdout and stderr from worker
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start process
	if err := cmd.Start(); err != nil {
		spawnErr := &WorkerSpawnError{
			BeadID:    beadIDStr,
			AgentType: string(agentType),
			Err:       err,
		}
		o.errorLogger.Error("spawnWorker", "process_start", beadIDStr, spawnErr, "error")

		// Record spawn failure for exponential backoff
		if strings.Contains(err.Error(), "resource temporarily unavailable") ||
			strings.Contains(err.Error(), "EAGAIN") ||
			strings.Contains(err.Error(), "fork") {
			o.spawnRateLimiter.RecordFailure()
		}

		return nil, spawnErr
	}

	log.Printf("[Worker] Process started with PID %d for bead %s (workerTracker nil: %v)", cmd.Process.Pid, beadIDStr, o.workerTracker == nil)

	// Stream worker stdout/stderr to logs
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			log.Printf("[Worker:%s] %s", beadIDStr, scanner.Text())
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			log.Printf("[Worker:%s] [stderr] %s", beadIDStr, scanner.Text())
		}
	}()

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
	o.retryManager.ClearRetry(worker.ID)

	o.dagMu.Lock()
	defer o.dagMu.Unlock()

	newlyReady, err := o.dag.MarkCompleted(worker.ID)
	if err != nil {
		o.errorLogger.Error("DAG", "MarkCompleted", worker.ID, err, "warn")
		return nil
	}

	if len(newlyReady) > 0 {
		log.Printf("[DAG] %d dependents now ready after %s completion", len(newlyReady), worker.ID)
	}

	// Note: Don't mark as completed here - wait for closeBead to succeed
	// o.completed[worker.ID] = true

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
	o.retryManager.RecordRetry(worker.ID, workerErr)

	retryCount := o.retryManager.GetRetryCount(worker.ID)
	maxRetries := o.getMaxRetries()

	if retryCount >= maxRetries {
		// Exhausted retries, log as error
		log.Printf("[Worker] Failed permanently: %s (exit code: %d, duration: %v, retries: %d/%d)",
			worker.ID, exitCode, duration, retryCount, maxRetries)
		worker.Status = string(types.WorkerStatusFailed)
	} else {
		// Will retry, log as warning
		backoff := calculateBackoff(retryCount, o.retryManager.BaseDelay(), o.retryManager.MaxDelay())
		log.Printf("[Worker] Failed but will retry: %s (exit code: %d, duration: %v, retry %d/%d, backoff: %v)",
			worker.ID, exitCode, duration, retryCount, maxRetries, backoff)
		worker.Status = string(types.WorkerStatusRetryPending)
	}

	o.dagMu.Lock()
	defer o.dagMu.Unlock()
	if err := o.dag.MarkFailed(worker.ID); err != nil {
		o.errorLogger.Error("DAG", "MarkFailed", worker.ID, err, "warn")
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
		o.errorLogger.Error("beadsClient", "CloseBead", comp.worker.ID, comp.closeError, "warn")
		if isDependencyError(comp.closeError) {
			log.Printf("[Worker] Bead %s processed but not closed due to dependencies - will retry later", comp.worker.BeadID.String())
			success = false
			exitCode = 1
		}
	} else if success {
		// Only mark as completed if close succeeded and worker succeeded
		o.completed[comp.worker.ID] = true
		log.Printf("[Worker] Bead %s marked as completed in session", comp.worker.ID)
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
	} else if o.retryManager.GetRetryCount(worker.ID) < o.getMaxRetries() {
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
			o.intelligenceHub.QueueRepoIndexing(projectName, repoPath)
		}
	}

	// Verify bead is not already closed (worker may have closed it)
	bead, err := o.beadsClient.GetBead(projectPath, repoName, worker.BeadID)
	if err == nil && bead.Status.IsTerminal() {
		log.Printf("[closeBead] Bead %s already closed (status: %s), skipping", worker.BeadID.String(), bead.Status)
		return nil
	}

	// Run TDD validation for REFACTOR beads before closing
	if success && bead != nil && bead.Type == "refactor" {
		if err := o.RunTDDValidation(*bead, projectPath); err != nil {
			log.Printf("[closeBead] TDD validation error for %s: %v", worker.BeadID.String(), err)
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
	if !o.spawnRateLimiter.CanSpawn() {
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

	// Skip epics (organizational containers, not actionable)
	if node.Bead.Type == "epic" {
		log.Printf("⏸️  Skipping epic (organizational): %s", beadID)
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

// findProjectPathForBeadID finds the project path for a given bead ID
func (o *Overseer) findProjectPathForBeadID(beadID types.BeadID) string {
	projectName := beadID.Project()
	return o.GetProjectPath(projectName)
}

// IsRunning returns whether the overseer is currently running
func (o *Overseer) IsRunning() bool {
	return o.stateController.IsRunning()
}

// IsPaused returns whether the overseer is paused
func (o *Overseer) IsPaused() bool {
	return o.stateController.IsPaused()
}

// Pause stops the overseer from spawning new workers
func (o *Overseer) Pause() {
	o.stateController.Pause()
	log.Println("⏸️  Overseer paused")
}

// Resume allows the overseer to spawn workers again
func (o *Overseer) Resume() {
	o.stateController.Resume()
	log.Println("▶️  Overseer resumed")
}

// SetMaxWorkers updates the maximum number of concurrent workers
func (o *Overseer) SetMaxWorkers(count int) {
	o.stateController.SetMaxWorkers(count)
	log.Printf("🔧 Max workers set to %d", count)
}

// GetMaxWorkers returns the current maximum worker count
func (o *Overseer) GetMaxWorkers() int {
	return o.stateController.GetMaxWorkers()
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
	var beads []types.Bead
	for _, node := range readyNodes {
		// Skip approval gates
		if node.Bead.Type == "approval-gate" {
			continue
		}
		// Skip beads already completed in this session
		if o.completed[string(node.Bead.ID)] {
			continue
		}
		beads = append(beads, node.Bead)
	}

	// Sort by priority descending (highest first)
	sort.Slice(beads, func(i, j int) bool {
		return beads[i].Priority > beads[j].Priority
	})

	return beads
}

// GetBeadDependencies returns the IDs of beads that the given bead depends on.
func (o *Overseer) GetBeadDependencies(beadID string) ([]string, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dagNavigator.GetDependencies(beadID)
}

// GetBeadDependents returns the IDs of beads that depend on the given bead.
func (o *Overseer) GetBeadDependents(beadID string) ([]string, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dagNavigator.GetDependents(beadID)
}

// GetBeadFromDAG retrieves a single bead from the DAG by its ID.
func (o *Overseer) GetBeadFromDAG(beadID string) (types.Bead, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dagNavigator.GetBead(beadID)
}

// SetBeadDiscoveryInterval updates the bead discovery interval for the overseer
func (o *Overseer) SetBeadDiscoveryInterval(interval time.Duration) {
	o.stateController.SetBeadDiscoveryInterval(interval)
	log.Printf("🔧 Bead discovery interval set to %v", interval)
}

// GetBeadDiscoveryInterval returns the current bead discovery interval
func (o *Overseer) GetBeadDiscoveryInterval() time.Duration {
	return o.stateController.GetBeadDiscoveryInterval()
}

// SetRetryDelay updates the base retry delay for the overseer
func (o *Overseer) SetRetryDelay(delay time.Duration) {
	o.retryManager.SetBaseDelay(delay)
	log.Printf("🔧 Retry delay set to %v", delay)
}

// GetRetryDelay returns the current base retry delay
func (o *Overseer) GetRetryDelay() time.Duration {
	return o.retryManager.BaseDelay()
}

// CloseBead closes a bead using the beads client
// If validation is enabled, it validates end-of-chain beads before closure
func (o *Overseer) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	// Check if validation is enabled
	if o.validator != nil {
		return validation.CloseBeadWithValidation(
			o.beadsClient,
			o.validator,
			projectPath,
			repoName,
			beadID,
			reason,
		)
	}

	// Validation disabled, close directly
	return o.beadsClient.CloseBead(projectPath, repoName, beadID, reason)
}

// RefreshToolClients reinitializes tool clients based on detected config changes.
func (o *Overseer) RefreshToolClients(changes []ToolConfigChange) {
	o.configManager.RefreshToolClients(changes)
}

// applyToolConfigChange applies a single tool configuration change.
func (o *Overseer) applyToolConfigChange(change ToolConfigChange) {
	o.configManager.applyToolConfigChange(change)
}

// refreshTLDRClient reinitializes the TLDR client.
func (o *Overseer) refreshTLDRClient(change ToolConfigChange) {
	o.configManager.refreshTLDRClient(change)
}

// refreshGPTCacheClient reinitializes the GPTCache client.
func (o *Overseer) refreshGPTCacheClient(change ToolConfigChange) {
	o.configManager.refreshGPTCacheClient(change)
}

// refreshMCPClient reinitializes the MCP client.
func (o *Overseer) refreshMCPClient(change ToolConfigChange) {
	o.configManager.refreshMCPClient(change)
}

// UpdateConfig updates the overseer's configuration and refreshes affected clients.
func (o *Overseer) UpdateConfig(newConfig *Config) error {
	return o.configManager.UpdateConfig(newConfig)
}

// GetBeadsClient returns the beads client
func (o *Overseer) GetBeadsClient() BeadsClient {
	return o.beadsClient
}

// GetBVClient returns the BV client for graph analytics
func (o *Overseer) GetBVClient() *bv.Client {
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
		log.Printf("[MCP] Debug: registration failed: %v", err)
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

	// Submit execution result to batch retention service for Hindsight storage
	if o.intelligenceHub.BatchRetention() != nil {
		o.submitExecutionResultToBatchRetention(msg)
	} else {
		log.Printf("[MCP] Warning: batch retention service not available, skipping Hindsight retention")
	}

	// Trigger scheduling cycle for next tasks
	go o.triggerImmediateSchedule()
}

// submitExecutionResultToBatchRetention extracts execution result from MCP message
// and submits it to the batch retention service for Hindsight storage
func (o *Overseer) submitExecutionResultToBatchRetention(msg *mcp.Message) {
	beadIDStr, ok := msg.Payload["bead_id"].(string)
	if !ok || beadIDStr == "" {
		return
	}

	// Parse bead ID
	beadID := types.BeadID(beadIDStr)

	// Extract execution result data
	status, _ := msg.Payload["status"].(string)
	success := status == "success"

	exitCode := 0
	if code, ok := msg.Payload["exit_code"].(float64); ok {
		exitCode = int(code)
	}

	duration := 0 * time.Second
	if dur, ok := msg.Payload["duration"].(float64); ok {
		duration = time.Duration(dur * float64(time.Second))
	}

	attempts := 1
	if att, ok := msg.Payload["attempts"].(float64); ok {
		attempts = int(att)
	}

	agentType, _ := msg.Payload["agent_type"].(string)
	output, _ := msg.Payload["output"].(string)
	errorStr, _ := msg.Payload["error"].(string)
	workerID, _ := msg.Payload["agent_id"].(string)

	// Build bead struct with available info
	bead := types.Bead{
		ID:    beadID,
		Title: getStringFromPayload(msg.Payload, "bead_title"),
		Type:  getStringFromPayload(msg.Payload, "bead_type"),
	}

	// Build execution result
	execResult := services.ExecutionResult{
		BeadID:    beadID,
		Success:   success,
		ExitCode:  exitCode,
		Output:    output,
		Error:     errorStr,
		Duration:  duration,
		Attempts:  attempts,
		AgentType: agentType,
		Timestamp: time.Now(),
	}

	// Submit to batch retention service
	if batchRetention := o.intelligenceHub.BatchRetention(); batchRetention != nil {
		if err := batchRetention.SubmitExecutionResult(bead, execResult, workerID); err != nil {
			log.Printf("[MCP] Failed to submit execution result to batch retention: %v", err)
		} else {
			log.Printf("[MCP] Submitted execution result to batch retention for bead %s", beadIDStr)
		}
	}
}

// getStringFromPayload safely extracts a string value from a map
func getStringFromPayload(payload map[string]interface{}, key string) string {
	if val, ok := payload[key].(string); ok {
		return val
	}
	return ""
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
		if o.retryManager.GetRetryCount(worker.ID) >= o.config.Overseer.DefaultRetries {
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
		log.Printf("[MCP] Debug: task completion broadcast failed: %v", err)
	} else {
		log.Printf("[MCP] Task completion broadcast: bead=%s, status=%s", worker.ID, status)
	}
}

// spawnWorkerWithContextEnrichment spawns a worker with context enrichment
// (Legacy function - CASS memory integration removed, now just calls spawnWorkerInternal)
func (o *Overseer) spawnWorkerWithContextEnrichment(bead types.Bead) (*types.Worker, error) {
	// Call spawnWorkerInternal to create the worker
	return o.spawnWorkerInternal(bead)
}

// buildWorkerEnvironmentWithContext builds worker environment with context enrichment
// Includes Hindsight configuration for memory-based context retrieval
func (o *Overseer) buildWorkerEnvironmentWithContext(bead types.Bead) []string {
	// Start with basic environment
	env := []string{
		fmt.Sprintf("HYDRA_BEAD_ID=%s", bead.ID),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_BEAD_TITLE=%s", bead.Title),
		fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", bead.Description),
	}

	// Add Hindsight configuration if enabled
	if o.config.Intelligence.Hindsight.Enabled {
		h := o.config.Intelligence.Hindsight
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_ENABLED=true"))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_BASE_URL=%s", h.BaseURL))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_TIMEOUT=%s", h.Timeout))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_LIMIT=%d", h.Limit))
		env = append(env, fmt.Sprintf("HYDRA_HISTORIESIGHT_MIN_SCORE=%.2f", h.MinScore))
	}

	// Add Pi configuration (always enabled)
	p := o.config.Agents.Pi
	// Fallback to pi.standard-agent.llm if agents.pi is not set
	std := o.config.Pi.StandardAgent.LLM
	if p.Provider == "" && std.Provider != "" {
		p.Provider = std.Provider
	}
	if p.Model == "" && std.Model != "" {
		p.Model = std.Model
	}
	if p.ThinkingLevel == "" && std.ThinkingLevel != "" {
		p.ThinkingLevel = std.ThinkingLevel
	}
	if p.Timeout == "" && std.Timeout != "" {
		p.Timeout = std.Timeout
	}
	if p.ApiKeyName == "" && std.ApiKeyName != "" {
		p.ApiKeyName = std.ApiKeyName
	}
	if p.Provider == "" {
		p.Provider = "kimi-coding"
	}
	if p.Model == "" {
		p.Model = "kimi-for-coding"
	}
	if p.ThinkingLevel == "" {
		p.ThinkingLevel = "on"
	}
	if p.Timeout == "" {
		p.Timeout = "10m"
	}
	if p.ApiKeyName == "" {
		p.ApiKeyName = "KIMI_API_KEY"
	}
	env = append(env, fmt.Sprintf("HYDRA_PI_ENABLED=true"))
	env = append(env, fmt.Sprintf("HYDRA_PI_PROVIDER=%s", p.Provider))
	env = append(env, fmt.Sprintf("HYDRA_PI_MODEL=%s", p.Model))
	env = append(env, fmt.Sprintf("HYDRA_PI_THINKING_LEVEL=%s", p.ThinkingLevel))
	env = append(env, fmt.Sprintf("HYDRA_PI_TIMEOUT=%s", p.Timeout))
	env = append(env, fmt.Sprintf("HYDRA_PI_API_KEY_NAME=%s", p.ApiKeyName))
	env = append(env, fmt.Sprintf("HYDRA_PI_CONFIG_DIR=%s", o.config.Pi.ConfigDir))

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
		piConfigDir = ".pi/hydra-config"
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
