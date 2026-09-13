package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/agents"
	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/internal/bv"
	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/internal/pi"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/internal/services"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// IntelligenceBundle, Pattern, ContextEntry, ImpactAnalysis, CallGraph, CallNode, CallEdge
// moved to pkg/types/intelligence.go

// Retry configuration constants
const (
	defaultMaxRetries    = 3
	defaultBaseDelay     = 5 * time.Second
	defaultMaxDelay      = 1 * time.Hour
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
	UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error
	UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error
}

// Overseer is the main scheduler daemon
type Overseer struct {
	config          *Config
	projects        []types.Project
	userContexts    map[string]*UserContext // Multi-user contexts (username -> context)
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
	// These clients remain direct fields until IntelligenceHub fully exposes their APIs.
	gptCache              *GPTCacheClient                 // GPTCache client for caching
	hindsightClient       HindsightClient                 // Hindsight client for pre-execution context retrieval
	lastSpawnBlockedEvent time.Time                       // throttle for spawn_blocked events (scheduler)
	providerPool          *ProviderPool                   // nil-safe; shared provider concurrency budget (Shepherd B3)
	poolReleases          sync.Map                        // workerID -> func() releasing its provider-pool slot
	batchRetentionSvc     *services.BatchRetentionService // Batch retention service for efficient memory storage
	hindsightLifecycle    *services.HindsightLifecycle    // Hindsight lifecycle manager for memory management
	intelMutex            sync.RWMutex                    // Mutex for intelligence clients
	bvClient              *bv.Client                      // BV client for graph analytics
	settledStateSvc       *SettledStateService            // Settled-state detection service

	// MCP manager for Model Context Protocol operations
	mcpManager *MCPManager

	// Resource monitoring for adaptive scaling
	resourceMonitor *ResourceMonitor // Monitors CPU/Memory usage

	// Orphan process janitor for cleaning up orphaned worker processes
	orphanJanitor *Charon // Cleans up orphaned worker processes

	// Worker registry (daemon-owned SQLite truth) and Pan, the shepherd that
	// decides the ambiguous tail of worker states. Both fail-open: a registry
	// open failure or a disabled Pan leaves deterministic rules in force.
	registryStore *registry.Store
	panShepherd   *pan.Shepherd

	// Chain completion detector for end-of-chain pattern extraction
	chainCompletionDetector *ChainCompletionDetector // Detects chain completion and triggers pattern extraction
	chainRegistrar          *ChainRegistrar          // Registers bead chains from issues.jsonl
	chainsRegistered        bool                     // Track if chains have been registered (only once at startup)

	// DAG builder for dependency graph construction
	dagBuilder *DAGBuilder // Builds DAG from project beads

	// Impact analyzer for TLDR-based impact analysis

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

	// Epic scanner for periodic epic auto-close
	epicScanner *EpicScanner // Scans and closes epics when all tasks complete

	// Closed-loop compliance cycle manager
	complianceCycle *ComplianceCycleManager

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
	providerBreaker  *ProviderBreaker
	// ConcurrencyGovernor throttles effective concurrency based on observed
	// provider health. Nil when disabled.
	concurrencyGovernor *ConcurrencyGovernor

	// BeadDiscovery finds beads ready to execute from various sources
	beadDiscovery *BeadDiscovery
	storeGuard    *BeadStoreGuard // corruption detector; nil when disabled

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
	beadCloser      *BeadCloser
	worktreeManager *WorktreeManager // per-chain git isolation; nil = disabled
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
	o.hindsightClient = o.intelligenceHub.Hindsight()

	// Initialize config manager
	o.configManager = NewConfigManager(o)

	// Initialize DAG builder
	o.dagBuilder = NewDAGBuilder(o.errorLogger)

	// Initialize impact analyzer

	// Initialize worker environment builder + provider failover breaker
	// (rate-limit on the primary provider -> fallback provider/model).
	o.workerEnvBuilder = NewWorkerEnvBuilder(config)
	o.providerBreaker = NewProviderBreaker(
		config.Agents.Pi.Provider, config.Agents.Pi.Model,
		config.Agents.Pi.FallbackProvider, config.Agents.Pi.FallbackModel,
	)
	o.workerEnvBuilder.SetProviderBreaker(o.providerBreaker)

	if config.Overseer.ConcurrencyGovernor.Enabled {
		o.concurrencyGovernor = NewConcurrencyGovernor(config.Overseer.MaxWorkers, config.Overseer.ConcurrencyGovernor)
		// Log the governor's RESOLVED settings, not the raw config block. The
		// constructor applies defaults to its own by-value copy, so a config
		// carrying only `enabled: true` logged "window=0, decrease=0.000,
		// cooldown=0s" — which reads exactly like a governor that can never act
		// (or one about to divide by zero) while the real instance was correctly
		// running window=8 at 0.5/0.125. A status line that misrepresents live
		// state is worse than no status line.
		log.Printf("[Governor] Initialized (%s)", o.concurrencyGovernor.Describe())
	}

	// Shared provider budget (Shepherd B3). Sized to the plan's concurrency, not
	// to max_workers: a worker is only one of four consumers of that budget.
	if config.Overseer.ProviderPool.Enabled {
		o.providerPool = NewProviderPool(config.Overseer.ProviderPool.Size, config.Overseer.ProviderPool)
		log.Printf("[ProviderPool] Initialized (%s)", o.providerPool.Describe())
	}

	o.beadDiscovery = NewBeadDiscovery(o.beadsClient, o.bvClient, o.projects, o.completed, o.dag, &o.dagMu)

	// Bead-store integrity guard (detect + self-heal): tracks per-project
	// open-bead counts across discovery ticks and fails scheduling closed on
	// anomalous collapse (0 or >80% in one tick). Restore verifies the effect
	// by re-listing, never the command.
	o.storeGuard = NewBeadStoreGuard(func(projectPath string) (int, error) {
		beads, err := o.beadsClient.ListBeads(projectPath, "")
		if err != nil {
			return 0, err
		}
		open := 0
		for _, b := range beads {
			if !b.Status.IsTerminal() {
				open++
			}
		}
		return open, nil
	})
	o.storeGuard.SetEventSink(NewEventSink(DefaultEventsDir()))
	o.beadDiscovery.SetStoreGuard(o.storeGuard)

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
	o.orphanJanitor.SetMaxBeadTokens(config.Overseer.MaxBeadTokens)
	o.orphanJanitor.SetRetryManager(o.retryManager)
	o.orphanJanitor.SetProjectsProvider(o.GetProjects)
	// Inject access to the in-memory activeWorkers map so Charon can clean up
	// stale placeholder entries (PID 0) that would otherwise block new spawns
	// for the same bead. The snapshot is taken under the worker mutex by the
	// callback to avoid racing with concurrent spawns.
	o.orphanJanitor.SetActiveWorkersSnapshot(func() map[string]*types.Worker {
		o.workerMutex.RLock()
		defer o.workerMutex.RUnlock()
		snapshot := make(map[string]*types.Worker, len(o.activeWorkers))
		for id, w := range o.activeWorkers {
			if w == nil {
				continue
			}
			snapshot[id] = w
		}
		return snapshot
	})
	// Map active beads to their chain worktree keys so the worktree sweeps
	// never integrate/remove a live chain worktree (member bead ID ≠ chain
	// root key — gybo4, 2026-09-10).
	o.orphanJanitor.SetChainKeyForBeadID(o.chainKeyForBeadID)
	// When Charon detects a stale PID-0 entry, it invokes this callback to
	// actually delete the entry from the in-memory map (since Charon does not
	// own the map).
	o.orphanJanitor.SetOnStaleActiveWorker(o.removeActiveWorker)

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
		o.concurrencyGovernor,
	)
	o.workerLifecycle.providerBreaker = o.providerBreaker
	o.workerLifecycle.releaseProviderSlot = o.releaseProviderSlot
	o.workerLifecycle.workerTimeout = config.Overseer.WorkerTimeout
	// Park-aware timeout: read the worker's live state from the registry so
	// parked (congestion) time never counts against the wall clock.
	o.workerLifecycle.parkedLookup = func(beadID string) bool {
		if o.registryStore == nil {
			return false
		}
		rec, ok, err := o.registryStore.GetWorker(beadID)
		return err == nil && ok && rec.State == registry.StateParked
	}
	// Zero-token deaths are never task faults: read tokens_so_far from the
	// registry so HandleFailure can classify them as infrastructure. A miss
	// returns -1 (not zero) so lookup failures never reclassify real faults.
	o.workerLifecycle.tokensLookup = func(beadID string) int64 {
		if o.registryStore == nil {
			return -1
		}
		rec, ok, err := o.registryStore.GetWorker(beadID)
		if err != nil || !ok {
			return -1
		}
		return rec.TokensSoFar
	}

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
		nil, // userContexts set after discovery
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
	o.beadCloser = NewBeadCloser(o.beadsClient, o.intelligenceHub, o.projectResolver, o.validationService, config)

	// Daemon gate runner (FR-002): attach to the close path and recover any
	// artifacts the previous daemon incarnation died inside (running + dead
	// PID -> interrupted, never trusted). Fail-open: recovery errors are
	// logged, never gate startup.
	if home := os.Getenv("HYDRA_HOME"); home != "" {
		o.beadCloser.SetGateRunner(NewGateRunner(home))
		if n, err := FinalizeStaleGateArtifacts(home); err != nil {
			log.Printf("[DaemonGates] stale artifact recovery failed (continuing): %v", err)
		} else if n > 0 {
			log.Printf("[DaemonGates] recovered %d interrupted gate artifact(s) from previous run", n)
		}
	}
	// Structured decision stream (Shepherd C1/C2): central day-files plus
	// per-repo .hydra/events.jsonl mirrors.
	o.beadCloser.SetEventSink(NewEventSink(DefaultEventsDir()))
	o.providerPool.SetEventSink(NewEventSink(DefaultEventsDir()))
	o.orphanJanitor.SetEventSink(NewEventSink(DefaultEventsDir()))
	retainer := NewVerdictRetainer(o.hindsightClient)
	o.beadCloser.SetVerdictRetainer(retainer)

	// Wire retry/spawn state purgers into the close path so verified closes
	// clear retry and spawn-failure residue for the bead.
	o.beadCloser.SetRetryManager(o.retryManager)
	o.beadCloser.SetWorkerSpawner(o.workerSpawner)

	// Per-chain worktree isolation (opt-in via overseer.worktree_isolation).
	// Wires the manager into spawn (run worker in the chain's worktree) and
	// close (commit its work); integration happens on chain completion.
	if config.Overseer.WorktreeIsolation {
		o.worktreeManager = NewWorktreeManager()
		o.workerSpawner.SetWorktreeManager(o.worktreeManager, o.chainKeyForBead)
		o.beadCloser.SetWorktreeManager(o.worktreeManager, o.chainKeyForBead, o.isChainMember)
		log.Printf("[Overseer] Worktree isolation ENABLED")
	}

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
	// Initialize closed-loop compliance cycle manager
	o.complianceCycle = NewComplianceCycleManager(o, config.ComplianceCycle)
	if config.ComplianceCycle.Enabled {
		log.Printf("[ComplianceCycle] Initialized with max_cycles=%d, poll_interval=%s", config.ComplianceCycle.MaxCycles, config.ComplianceCycle.PollInterval)
	} else {
		log.Println("[ComplianceCycle] Disabled")
	}
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

// findProjectPath finds the project path for a given bead.
// Falls back to beadAnalyzer if initialized, otherwise uses projectResolver.
func (o *Overseer) findProjectPath(bead *types.Bead) string {
	if o.beadAnalyzer != nil {
		return o.beadAnalyzer.FindProjectPath(bead)
	}
	return o.projectResolver.ResolveProjectPath(bead)
}

// getUserContextForProject returns the user context for a project path, if any.
func (o *Overseer) getUserContextForProject(projectPath string) *UserContext {
	for _, ctx := range o.userContexts {
		for _, p := range ctx.Config.Projects {
			if p.Path == projectPath {
				return ctx
			}
		}
	}
	return nil
}

// validateScope validates the scope of changes against configured limits
func (o *Overseer) validateScope(bundle *types.IntelligenceBundle, targetFiles []string) error {
	return o.scopeValidator.Validate(bundle, targetFiles)
}

// shouldRetry checks if a bead should be retried based on retry count and cooldown
func (o *Overseer) isIntelligenceEnabled() bool {
	return o.config.Intelligence.Graphify.Enabled || o.config.Intelligence.GPTCache.Enabled
}

// Start begins the overseer's main loop
func (o *Overseer) Start(ctx context.Context) error {
	log.Println("🚀 Starting Hydra overseer...")

	if err := o.serviceStarter.Start(ctx); err != nil {
		return err
	}

	// Start closed-loop compliance cycle manager
	if o.complianceCycle != nil && o.config.ComplianceCycle.Enabled {
		go o.complianceCycle.Start(ctx)
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
	err := o.scheduler.Tick()
	// After scheduling, publish each project's working branch to its remote
	// (opt-in via output.push). This consolidates completed worktree commits
	// onto the working branch and pushes with the project owner's PAT, so by
	// the end of a run every repo's branch is on the remote for the developer
	// to pull/build locally. Fail-open: never affects the scheduling result.
	o.publishProjects()
	return err
}

// publishProjects reconciles + pushes each project's working branch when
// output push is enabled. Push authenticates with the project owner's token
// (per-user PAT); projects without a resolvable user context fall back to a
// plain origin push. Only branches ahead of their remote are pushed.
func (o *Overseer) publishProjects() {
	if o.config == nil || !o.config.OutputPushEnabled() || o.worktreeManager == nil {
		return
	}
	// Worktree keys owned by live workers must not be reconciled mid-flight.
	active := make(map[string]bool, len(o.activeWorkers))
	for beadID := range o.activeWorkers {
		active[beadID] = true
	}
	for _, project := range o.projects {
		token := ""
		if uc := o.getUserContextForProject(project.Path); uc != nil {
			token = uc.Token
		}
		PublishRepo(o.worktreeManager, project.Path, token, o.config.PRMergeEnabled(), active, o.terminalBeadChecker(project.Path))
		if o.config != nil && o.config.Worktree.QuarantineAutoDrain {
			if _, _, err := DrainQuarantineBranches(project.Path, false); err != nil {
				log.Printf("[publishProjects] drain quarantine for %s: %v", project.Path, err)
			}
		}
	}
}

// terminalBeadChecker reports whether a worktree key's bead is terminal.
// Lookup errors fail closed (nil) — publish must never touch work it cannot
// prove is done.
func (o *Overseer) terminalBeadChecker(projectPath string) func(string) bool {
	if o.beadsClient == nil {
		return nil
	}
	return func(key string) bool {
		b, err := o.beadsClient.GetBead(projectPath, "", types.BeadID(key))
		return err == nil && b != nil && b.Status.IsTerminal()
	}
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

	// Auto-close epics whose dependencies AND task children are all done. Without
	// this step, an epic with all sub-beads closed would stay open forever and
	// block its downstream dependents (typically the next phase epic). The
	// close is persisted to the beads store after the DAG build returns so we
	// don't hold the DAG lock across a br CLI invocation.
	if closed := o.dag.CloseReadyEpics(); len(closed) > 0 {
		go o.persistClosedEpics(closed)
	}

	return nil
}

// persistClosedEpics writes the auto-close decision for organizational epics
// back to the beads store. Runs in a goroutine to avoid holding the DAG lock
// during the br CLI call.
func (o *Overseer) persistClosedEpics(beadIDs []string) {
	for _, id := range beadIDs {
		projectPath, _ := o.findProjectPathForBead(id)
		if projectPath == "" {
			log.Printf("[DAG] Could not find project path for epic %s; skipping persistence", id)
			continue
		}
		reason := "epic auto-close: all dependencies and children closed"
		if err := o.beadsClient.CloseBead(projectPath, "", types.BeadID(id), reason); err != nil {
			log.Printf("[DAG] Failed to persist close for epic %s: %v", id, err)
			continue
		}
		log.Printf("[DAG] Auto-closed epic %s (all dependencies and children done)", id)
		o.completed[id] = true
	}
}

// findProjectPathForBead returns the project path for a bead ID by matching the
// bead's project prefix (e.g. "srv-file-handler-xyz" -> project "srv-file-handler").
// Returns the first matching project's path.
func (o *Overseer) findProjectPathForBead(beadID string) (string, types.BeadID) {
	for _, project := range o.GetProjects() {
		if len(beadID) > len(project.Name) && beadID[:len(project.Name)+1] == project.Name+"-" {
			return project.Path, types.BeadID(beadID)
		}
	}
	return "", types.BeadID(beadID)
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
	beadIDStr := bead.ID.String()
	projectPath := o.beadAnalyzer.FindProjectPath(&bead)

	// Re-check the store at the last moment. In-memory retry state can outlive
	// the bead's real status; a bead closed by Pan, a human, or supersession
	// while it sat in the retry queue must not be resurrected (e2lqf, 2026-09-02).
	if projectPath != "" && o.beadsClient != nil {
		current, err := o.beadsClient.GetBead(projectPath, "", bead.ID)
		if err != nil {
			log.Printf("[Spawn] %s store status read failed: %v", beadIDStr, err)
		} else if current != nil && current.Status.IsTerminal() {
			// Drop the stale retry entry so the scheduler stops offering it.
			o.retryManager.ClearRetry(beadIDStr)
			o.emitSpawnSuppressedClosed(bead, projectPath, string(current.Status))
			return &SpawnSuppressedError{BeadID: beadIDStr, Status: current.Status}
		}
	}

	// Pre-spawn gate probe: a bead whose scoped gates already pass on main
	// closes at zero token cost instead of looping a worker through
	// empty-diff rejections (srv 65wa0: 7.9M tokens, 3 rounds, suite was
	// already green).
	if o.prespawnCloseIfGreen(bead) {
		return nil
	}
	return o.workerSpawner.Spawn(bead)
}

// emitSpawnSuppressedClosed writes a structured event when a spawn is aborted
// because the bead is already terminal. The event is mirrored into the repo so
// operators can see why a queued bead did not run.
func (o *Overseer) emitSpawnSuppressedClosed(bead types.Bead, projectPath, status string) {
	ev := Event{
		Type:    "spawn-suppressed-closed",
		Reason:  "already-terminal",
		Project: string(bead.Project),
		Bead:    bead.ID.String(),
		Detail:  fmt.Sprintf("Spawn suppressed for %s: bead is already %s", bead.ID, status),
		Fields:  map[string]any{"status": status},
	}
	if o.beadCloser != nil && o.beadCloser.events != nil {
		o.beadCloser.events.EmitRepo(projectPath, ev)
	} else {
		log.Printf("[Spawn] %s", ev.Detail)
	}
}

// holdProviderSlot records the pool reservation taken for a worker so it can be
// returned when that worker completes.
func (o *Overseer) holdProviderSlot(workerID string, release func()) {
	if release != nil {
		o.poolReleases.Store(workerID, release)
	}
}

// releaseProviderSlot returns a worker's pool reservation. Safe to call for an
// unknown worker and safe to call twice — the underlying release is idempotent.
func (o *Overseer) releaseProviderSlot(workerID string) {
	if v, ok := o.poolReleases.LoadAndDelete(workerID); ok {
		if release, ok := v.(func()); ok && release != nil {
			release()
		}
	}
}

// HindsightClient defines the interface for Hindsight memory operations
type HindsightClient interface {
	Search(query, beadType string, limit int) (*hindsight.SearchResponse, error)
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
func (o *Overseer) closeBead(worker *types.Worker, success bool, exitCode int) error {
	return o.beadCloser.Close(worker, success, exitCode)
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
	if node.Bead.Type == "approval" {
		return false
	}
	return true
}

// isBeadRunning checks if a bead is currently being executed by an active worker
func (o *Overseer) isBeadRunning(beadID string) bool {
	o.workerMutex.RLock()
	_, exists := o.activeWorkers[beadID]
	o.workerMutex.RUnlock()
	if exists {
		return true
	}
	if o.workerSpawner != nil {
		return o.workerSpawner.HasLiveRegistryWorker(beadID)
	}
	return false
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

// GetProjects returns the list of discovered projects. Falls back to the static
// config.Projects list when discovery has not yet populated o.projects (e.g.
// during early startup before projectScanner runs). This guarantees callers like
// the orphan janitor always see the configured projects even before the
// serviceStarter has finished its initial scan.
func (o *Overseer) GetProjects() []types.Project {
	if len(o.projects) > 0 {
		return o.projects
	}
	if o.config == nil {
		return nil
	}
	projects := make([]types.Project, 0, len(o.config.Projects))
	for _, pc := range o.config.Projects {
		projects = append(projects, types.Project{
			Name: pc.Name,
			Path: pc.Path,
			Type: types.RepoType(pc.Type),
		})
	}
	return projects
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
