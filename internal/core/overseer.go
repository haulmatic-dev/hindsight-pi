package core

import (
	"context"
	"log"
	"os/exec"
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
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// IntelligenceBundle, Pattern, ContextEntry, ImpactAnalysis, CallGraph, CallNode, CallEdge
// moved to pkg/types/intelligence.go

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

// Overseer is the main scheduler daemon
type Overseer struct {
	config          *Config
	projects        []types.Project
	activeWorkers   map[string]*types.Worker
	workerMutex     sync.RWMutex
	stateController *StateController // Runtime state (running, paused, maxWorkers, interval)
	configManager   *ConfigManager   // Dynamic configuration management
	beadsClient     BeadsClient
	workerSpawner   *WorkerSpawner
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

	// MCP manager for Model Context Protocol operations
	mcpManager *MCPManager

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

	// SkillUpdaterInitializer creates and configures the skill updater
	skillUpdaterInitializer *SkillUpdaterInitializer

	// TLDR indexer for deterministic code indexing
	tldrIndexer *TLDRIndexer // Indexes repositories after code changes

	// Epic scanner for periodic epic auto-close
	epicScanner *EpicScanner // Scans and closes epics when all tasks complete

	// ValidationService orchestrates TDD, PRD, and end-of-chain validation
	validationService *ValidationService

	// Bead file reader for fallback bead lookups
	beadFileReader *BeadFileReader

	// Bead analyzer for metadata extraction
	beadAnalyzer *BeadAnalyzer

	// ErrorLogger provides structured logging
	errorLogger *ErrorLogger

	// WorkerEnvBuilder builds environment variables for worker processes
	workerEnvBuilder *WorkerEnvBuilder

	// BeadDiscovery finds beads ready to execute from various sources
	beadDiscovery *BeadDiscovery

	// ProjectResolver resolves project and repo paths for beads
	projectResolver *ProjectResolver

	// WorkerLifecycle manages worker process lifecycle
	workerLifecycle *WorkerLifecycle

	// Scheduler orchestrates the main scheduling loop
	scheduler *Scheduler

	// ServiceStarter starts and stops background services
	serviceStarter *ServiceStarter

	// ScopeValidator validates the scope of changes
	scopeValidator *ScopeValidator

	// SettledStateInitializer creates and configures settled-state service
	settledStateInitializer *SettledStateInitializer

	// ChainCompletionInitializer creates and configures chain completion detector
	chainCompletionInitializer *ChainCompletionInitializer

	// PiSetupManager initializes and runs Pi ecosystem setup
	piSetupManager *PiSetupManager

	// BeadCloser closes beads based on worker completion
	beadCloser *BeadCloser
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
		dag:              NewDAG(),
		completed:        make(map[string]bool),
		retryManager:     NewRetryManager(defaultBaseDelay, defaultMaxDelay),
		spawnRateLimiter: NewSpawnRateLimiter(),
		intelligenceHub:  NewIntelligenceHub(config.Intelligence),
	}

	// Initialize error logger FIRST — many components depend on it
	o.errorLogger = NewErrorLogger(o.retryManager.GetRetryCount)

	// Initialize intelligence hub
	o.intelligenceHub.Initialize()

	// Initialize config manager
	o.configManager = NewConfigManager(o)

	// Initialize DAG builder
	o.dagBuilder = NewDAGBuilder(o.errorLogger)

	// Initialize impact analyzer
	o.impactAnalyzer = NewImpactAnalyzer(o.findProjectPath)

	// Initialize worker environment builder
	o.workerEnvBuilder = NewWorkerEnvBuilder(config)

	o.beadDiscovery = NewBeadDiscovery(o.beadsClient, o.bvClient, o.projects, o.completed, o.dag, &o.dagMu)

	// Initialize project resolver
	o.projectResolver = NewProjectResolver(o.projects)

	// Initialize MCP manager
	o.mcpManager = NewMCPManager(o)
	if config.MCP.Enabled {
		if err := o.mcpManager.Initialize(&config.MCP); err != nil {
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

	// Initialize worker lifecycle manager
	o.workerLifecycle = NewWorkerLifecycle(
		o.retryManager,
		o.errorLogger,
		o.workerTracker,
		o.settledStateSvc,
		o.dag,
		&o.dagMu,
		o.completed,
		o.removeActiveWorker,
		o.closeBead,
		o.triggerImmediateSchedule,
		o.sendMCPWorkerCompletion,
		o.getMaxRetries,
	)

	// Initialize bead analyzer (projects will be updated after discovery)
	o.beadAnalyzer = NewBeadAnalyzer(o.projects)

	// Initialize worker spawner
	o.workerSpawner = NewWorkerSpawner(
		o.activeWorkers,
		&o.workerMutex,
		o.workerTracker,
		o.spawnRateLimiter,
		o.errorLogger,
		o.retryManager,
		config,
		o.beadsClient,
		o.beadAnalyzer,
		o.workerEnvBuilder,
		o.workerLifecycle,
	)

	// Initialize scheduler
	o.scheduler = NewScheduler(o)
	o.scopeValidator = NewScopeValidator(config)

	// Initialize service starter
	o.serviceStarter = NewServiceStarter(o)

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
	o.settledStateInitializer = NewSettledStateInitializer(o)
	if config.Intelligence.SettledState.Enabled {
		o.settledStateInitializer.Initialize()
	}

	// Initialize validation service
	o.validationService = NewValidationService(config, o.retryManager, o.beadsClient)
	log.Printf("[Validation] Initialized bead validation service")

	// Initialize chain completion detector if enabled
	o.chainCompletionInitializer = NewChainCompletionInitializer(o)
	if config.Overseer.ChainDetection.Enabled {
		o.chainCompletionInitializer.Initialize()
	}

	// Initialize Pi setup manager
	o.piSetupManager = NewPiSetupManager(config)

	// Initialize bead closer
	o.beadCloser = NewBeadCloser(o.beadsClient, o.intelligenceHub, o.projectResolver, o.validationService)

	// Initialize skill updater initializer
	o.skillUpdaterInitializer = NewSkillUpdaterInitializer(config, &o.skillUpdater)

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
	return o.skillUpdaterInitializer.Initialize()
}

// initializeIntelligenceClients creates intelligence service clients based on config
func (o *Overseer) initializeSettledStateService() {
	o.settledStateInitializer.Initialize()
}

// initializeChainCompletionDetector creates and configures the chain completion detector
func (o *Overseer) initializeChainCompletionDetector() {
	o.chainCompletionInitializer.Initialize()
}

// analyzeImpactWithTLDR performs impact analysis using TLDR service
func (o *Overseer) analyzeImpactWithTLDR(client *intelligence.TLDRClient, bead *types.Bead, files []string) (*types.ImpactAnalysis, error) {
	return o.impactAnalyzer.Analyze(client, bead, files)
}

// findProjectPath finds the project path for a given bead.
// Falls back to beadAnalyzer if initialized, otherwise uses projectResolver.
func (o *Overseer) findProjectPath(bead *types.Bead) string {
	if o.beadAnalyzer != nil {
		return o.beadAnalyzer.FindProjectPath(bead)
	}
	return o.projectResolver.ResolveProjectPath(bead)
}

// validateScope validates the scope of changes against configured limits
func (o *Overseer) validateScope(bundle *types.IntelligenceBundle, targetFiles []string) error {
	return o.scopeValidator.Validate(bundle, targetFiles)
}

// shouldRetry checks if a bead should be retried based on retry count and cooldown
func (o *Overseer) isIntelligenceEnabled() bool {
	return o.config.Intelligence.TLDR.Enabled || o.config.Intelligence.GPTCache.Enabled
}

// Start begins the overseer's main loop
func (o *Overseer) Start(ctx context.Context) error {
	log.Println("🚀 Starting Hydra overseer...")

	if err := o.serviceStarter.Start(ctx); err != nil {
		return err
	}

	o.stateController.SetRunning(true)
	ticker := time.NewTicker(o.stateController.GetBeadDiscoveryInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			o.stateController.SetRunning(false)
			o.serviceStarter.Stop()
			return nil
		case <-ticker.C:
			if err := o.tick(); err != nil {
				log.Printf("Error during tick: %v", err)
			}
		}
	}
}

// tick performs one iteration of the scheduling loop.
func (o *Overseer) tick() error {
	return o.scheduler.Tick()
}

// getRetryEligibleBeads returns beads that are eligible for retry
// It filters the ready beads and adds any failed beads that should be retried
func (o *Overseer) getRetryEligibleBeads(readyBeads []types.Bead) []types.Bead {
	return o.scheduler.getRetryEligibleBeads(readyBeads)
}

// getBeadFromDAG retrieves a bead from the DAG by ID
func (o *Overseer) getBeadFromDAG(beadID string) (types.Bead, error) {
	return o.scheduler.getBeadFromDAG(beadID)
}

// buildDAG fetches all beads from all projects and builds/refreshs the DAG
func (o *Overseer) buildDAG() error {
	o.dagMu.Lock()
	defer o.dagMu.Unlock()

	if o.dag == nil {
		log.Printf("[DAG] DAG was nil, creating new instance")
		o.dag = NewDAG()
	}
	o.dagNavigator = NewDAGNavigator(o.dag)

	if err := o.dagBuilder.Build(o.dag, o.beadsClient, o.projects, o.completed); err != nil {
		return err
	}

	return nil
}

// GetBeadFromFile reads a single bead from issues.jsonl
func (o *Overseer) getAllDescendants(nodeID string) []string {
	return o.dagNavigator.GetAllDescendants(nodeID)
}

// getReadyBeads retrieves beads that are ready to execute.
func (o *Overseer) getReadyBeads() ([]types.Bead, error) {
	o.workerMutex.RLock()
	activeWorkers := make(map[string]bool, len(o.activeWorkers))
	for id := range o.activeWorkers {
		activeWorkers[id] = true
	}
	o.workerMutex.RUnlock()
	return o.beadDiscovery.FindReadyBeads(activeWorkers)
}

// getReadyBeadsFromBR retrieves beads using br ready (legacy method)
// getReadyBeadsFromBR retrieves beads using br ready (legacy method).
func (o *Overseer) getReadyBeadsFromBR() ([]types.Bead, error) {
	o.workerMutex.RLock()
	activeWorkers := make(map[string]bool, len(o.activeWorkers))
	for id := range o.activeWorkers {
		activeWorkers[id] = true
	}
	o.workerMutex.RUnlock()
	return o.beadDiscovery.findFromBR(activeWorkers)
}

// getReadyBeadsFromBV retrieves beads using bv --robot-triage.
func (o *Overseer) getReadyBeadsFromBV() ([]types.Bead, error) {
	o.workerMutex.RLock()
	activeWorkers := make(map[string]bool, len(o.activeWorkers))
	for id := range o.activeWorkers {
		activeWorkers[id] = true
	}
	o.workerMutex.RUnlock()
	return o.beadDiscovery.findFromBV(activeWorkers)
}

// Falls back to br ready if bv is not available (fail hard if bv fails when enabled)
// getReadyBeadsFromBVPlan retrieves beads using bv --robot-plan.
func (o *Overseer) getReadyBeadsFromBVPlan() ([]types.Bead, error) {
	o.workerMutex.RLock()
	activeWorkers := make(map[string]bool, len(o.activeWorkers))
	for id := range o.activeWorkers {
		activeWorkers[id] = true
	}
	o.workerMutex.RUnlock()
	return o.beadDiscovery.findFromBVPlan(activeWorkers)
}

// critical-path awareness, and impact scoring (unblocks count).
// spawnWorker creates and starts a worker for a bead using the configured spawner
func (o *Overseer) spawnWorker(bead types.Bead) error {
	return o.workerSpawner.Spawn(bead)
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
	return o.workerSpawner.SpawnInternal(bead)
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
	return o.beadCloser.Close(worker, success)
}

// resolveRepoPath finds the repository path for a given project and repo.
func (o *Overseer) resolveRepoPath(projectName, repoName, projectPath string) string {
	return o.projectResolver.ResolveRepoPath(projectName, repoName, projectPath)
}

// resolveProjectPath determines the project path using multiple resolution strategies.
func (o *Overseer) resolveProjectPath(worker *types.Worker, projectName string) string {
	if path := o.projectResolver.ResolveProjectPathByWorker(worker, projectName); path != "" {
		return path
	}
	if o.config != nil {
		for _, proj := range o.config.Projects {
			if proj.Name == projectName {
				return proj.Path
			}
		}
	}
	log.Printf("[resolveProjectPath] ERROR: Project %s not found", projectName)
	return ""
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
	if o.scheduler != nil {
		o.scheduler.TriggerImmediateSchedule()
	}
}

// getAvailableWorkerSlots returns the number of available worker slots
func (o *Overseer) getAvailableWorkerSlots() int {
	if o.scheduler != nil {
		return o.scheduler.GetAvailableWorkerSlots()
	}

	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()
	return o.GetMaxWorkers() - activeCount
}

// getReadyNodes returns nodes that are ready for execution
func (o *Overseer) getReadyNodes() []*Node {
	if o.scheduler != nil {
		return o.scheduler.GetReadyNodes()
	}
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()
	return o.dag.GetReadyNodes()
}

// spawnWorkersForNodes spawns workers for the given nodes up to available slots
func (o *Overseer) spawnWorkersForNodes(nodes []*Node, maxSlots int) int {
	if o.scheduler != nil {
		return o.scheduler.SpawnWorkersForNodes(nodes, maxSlots)
	}

	spawned := 0
	for _, node := range nodes {
		o.workerMutex.RLock()
		currentActive := len(o.activeWorkers)
		o.workerMutex.RUnlock()
		maxWorkers := o.GetMaxWorkers()
		currentAvailable := maxWorkers - currentActive
		if maxWorkers == 0 {
			currentAvailable = 20
		}
		effectiveLimit := maxSlots
		if currentAvailable < effectiveLimit {
			effectiveLimit = currentAvailable
		}
		if spawned >= effectiveLimit {
			break
		}
		if !o.shouldSpawnWorker(node) {
			continue
		}
		if err := o.spawnWorker(node.Bead); err != nil {
			log.Printf("Failed to spawn worker for bead %s: %v", node.Bead.ID, err)
			continue
		}
		spawned++
	}
	return spawned
}

// shouldSpawnWorker determines if a worker should be spawned for a node
func (o *Overseer) shouldSpawnWorker(node *Node) bool {
	if o.scheduler != nil {
		return o.scheduler.ShouldSpawnWorker(node)
	}

	beadID := string(node.Bead.ID)
	if o.isBeadRunning(beadID) {
		return false
	}
	if node.Bead.Type == "approval-gate" || node.Bead.Type == "epic" {
		return false
	}
	if node.Bead.Type == "approval" && node.Bead.Status == types.StatusInProgress {
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
	return o.dagNavigator.GetReadyBeads(o.completed)
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
	return o.validationService.CloseBead(projectPath, repoName, beadID, reason)
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

// initializeMCP initializes the MCP client and reservation manager.
func (o *Overseer) initializeMCP() error {
	return o.mcpManager.Initialize(&o.config.MCP)
}

// StartMCPListener connects to the MCP server and starts listening for messages.
func (o *Overseer) StartMCPListener() error {
	return o.mcpManager.StartListener()
}

// StopMCP disconnects from the MCP server.
func (o *Overseer) StopMCP() error {
	return o.mcpManager.Stop()
}

// handleMCPMessage processes incoming MCP messages.
func (o *Overseer) handleMCPMessage(msg mcp.Message) {
	o.mcpManager.handleMessage(msg)
}

// HandleMCPMessage processes an MCP message (public API).
func (o *Overseer) HandleMCPMessage(msg *mcp.Message) error {
	return o.mcpManager.HandleMessage(msg)
}

// handleTaskCompleteMessage processes task completion messages from workers.
func (o *Overseer) handleTaskCompleteMessage(msg *mcp.Message) {
	o.mcpManager.handleTaskComplete(msg)
}

// submitExecutionResultToBatchRetention extracts execution result from MCP message.
func (o *Overseer) submitExecutionResultToBatchRetention(msg *mcp.Message) {
	o.mcpManager.submitExecutionResult(msg)
}

// handleTaskStartMessage processes task start messages.
func (o *Overseer) handleTaskStartMessage(msg *mcp.Message) {
	o.mcpManager.handleTaskStart(msg)
}

// handleLogMessage processes log messages from agents.
func (o *Overseer) handleLogMessage(msg *mcp.Message) {
	o.mcpManager.handleLog(msg)
}

// handleFileReservedMessage processes file reservation messages.
func (o *Overseer) handleFileReservedMessage(msg *mcp.Message) {
	o.mcpManager.handleFileReserved(msg)
}

// handleFileReleasedMessage processes file release messages.
func (o *Overseer) handleFileReleasedMessage(msg *mcp.Message) {
	o.mcpManager.handleFileReleased(msg)
}

// GetActiveReservations returns all active file reservations.
func (o *Overseer) GetActiveReservations() []*mcp.Reservation {
	return o.mcpManager.GetActiveReservations()
}

// ForceReleaseReservation forcibly releases a reservation by ID.
func (o *Overseer) ForceReleaseReservation(reservationID string) error {
	return o.mcpManager.ForceReleaseReservation(reservationID)
}

// BroadcastToWorkers sends a broadcast message to all connected workers.
func (o *Overseer) BroadcastToWorkers(msgType string, payload map[string]interface{}) error {
	return o.mcpManager.Broadcast(msgType, payload)
}

// SendLogToMCP sends a log message to the MCP server.
func (o *Overseer) SendLogToMCP(level, message string) error {
	return o.mcpManager.SendLog(level, message)
}

// GetMCPStatus returns the current MCP connection status.
func (o *Overseer) GetMCPStatus() mcp.ClientStatus {
	return o.mcpManager.Status()
}

// IsMCPConnected returns whether the MCP client is connected.
func (o *Overseer) IsMCPConnected() bool {
	return o.mcpManager.IsConnected()
}

// sendMCPWorkerCompletion sends a task completion message via MCP.
func (o *Overseer) sendMCPWorkerCompletion(worker *types.Worker, success bool, exitCode int, duration time.Duration) {
	o.mcpManager.SendWorkerCompletion(worker, success, exitCode, duration)
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

// spawnWorkerWithContextEnrichment spawns a worker with context enrichment
// (Legacy function - CASS memory integration removed, now just calls spawnWorkerInternal)
func (o *Overseer) spawnWorkerWithContextEnrichment(bead types.Bead) (*types.Worker, error) {
	// Call spawnWorkerInternal to create the worker
	return o.spawnWorkerInternal(bead)
}

// buildWorkerEnvironmentWithContext builds worker environment with context enrichment.
func (o *Overseer) buildWorkerEnvironmentWithContext(bead types.Bead) []string {
	return o.workerEnvBuilder.BuildForContextEnrichment(bead)
}

// runPiSetup initializes and runs the Pi ecosystem setup
func (o *Overseer) runPiSetup() error {
	return o.piSetupManager.RunSetup()
}


