package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/haulmatic-dev/hydra/internal/triage"
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
	RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error
	UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error
	UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error
	AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error
}

// Overseer is the main scheduler daemon
type Overseer struct {
	// beadSource maps bead ID -> the project path whose store listed it
	// (rebuilt every DAG build). The only unambiguous resolution when
	// several projects share a repo name (ajantha/webapp vs nipuna.j/webapp).
	beadSource *BeadProvenanceIndex
	// discoveryRefreshInterval gates how often multi-user discovery re-runs
	// inside tick() so edited ~/.hydra/config.yaml files and repos added
	// under a multi dir onboard without a daemon restart.
	discoveryRefreshInterval time.Duration
	lastDiscoveryRefresh     time.Time
	// discoveryHomeDir overrides the discovery scan root in tests.
	discoveryHomeDir string
	config           *Config
	projects         []types.Project
	userContexts     map[string]*UserContext // Multi-user contexts (username -> context)
	activeWorkers    map[string]*types.Worker
	workerMutex      sync.RWMutex
	stateController  *StateController // Runtime state (running, paused, maxWorkers, interval)
	configManager    *ConfigManager   // Dynamic configuration management
	beadsClient      BeadsClient
	// Pan file-bead signature window: recently filed signatures (normalized
	// title per project) are remembered for panFileSigTTL so a closed wedge
	// bead is not re-filed while the same root cause is still producing
	// blocked beads (2026-10-02: one spawn failure filed 15+ PROJECT-WEDGE
	// twins, each spawning its own diagnostic worker run).
	panSigMu         sync.Mutex
	recentPanSigs    map[string]time.Time
	workerSpawner    *WorkerSpawner
	pushPendingMu    sync.Mutex
	pushPending      map[string]*PushPendingEntry    // projectPath → stranded-push state
	publishBackoff   map[string]*publishBackoffEntry // projectPath → failing-publish backoff state (publish tick retry throttle)
	diskWatermarkPct int                             // host disk watermark (0 = default 85%)
	diskStatPath     string                          // statfs path ("" = "/")
	diskCleanup      *diskCleanupGuard               // watermark-breach cleanup (fail-open)
	dag              *DAG
	dagMu            sync.RWMutex
	epicCloseGuard   epicCloseGuard // store-verification refusals for epic auto-close
	dagNavigator     *DAGNavigator
	// panCapHits marks project paths whose pan-filed filing was refused at
	// the per-project cap (keyed by project path; cleared on a successful
	// filing). collectHealthDigest surfaces it as a stable anomaly so the
	// suppression stays visible to Pan.
	panCapHits      sync.Map
	completed       map[string]bool  // Track completed beads for this session
	retryManager    *RetryManager    // Retry and validation-retry tracking
	intelligenceHub *IntelligenceHub // Encapsulates TLDR, GPTCache, Hindsight clients
	// hardParkProbes tracks consecutive successful worktree probes per bead
	// for the hard-park reconciler (hysteresis x2 before clearing labels).
	hardParkProbes        map[string]int
	lastHardParkReconcile time.Time
	// These clients remain direct fields until IntelligenceHub fully exposes their APIs.
	gptCache              *GPTCacheClient                 // GPTCache client for caching
	hindsightClient       HindsightClient                 // Hindsight client for pre-execution context retrieval
	lastSpawnBlockedEvent time.Time                       // throttle for spawn_blocked events (scheduler)
	providerPool          *ProviderPool                   // nil-safe; shared provider concurrency budget (Shepherd B3)
	poolReleases          sync.Map                        // workerID -> func() releasing its provider-pool slot
	batchRetentionSvc     *services.BatchRetentionService // Batch retention service for efficient memory storage
	intelMutex            sync.RWMutex                    // Mutex for intelligence clients
	bvClient              *bv.Client                      // BV client for graph analytics
	settledStateSvc       *SettledStateService            // Settled-state detection service

	// Integration-retest stage: periodic mechanical recheck of
	// integration-parked beads; nil unless HYDRA_HOME is set.
	integrationRetest *IntegrationRetestStage

	// Quarantine reconciler: re-verdicts quarantined chains and re-integrates
	// them once no member carries closed-failed; nil without worktree isolation.
	quarantineReconciler *QuarantineReconciler

	// MCP manager for Model Context Protocol operations
	mcpManager *MCPManager

	// Resource monitoring for adaptive scaling
	resourceMonitor *ResourceMonitor // Monitors CPU/Memory usage

	// Shared structured event sink for daemon-wide decision events.
	eventSink *EventSink

	// Orphan process janitor for cleaning up orphaned worker processes
	orphanJanitor *Charon // Cleans up orphaned worker processes

	// Gate artifact root ($HYDRA_HOME); enables the fleet digest's
	// close-in-progress probe so beads mid-close are not counted as
	// orphaned claims. Empty = probe disabled.
	gateRoot string

	// Stale-escalation ager (nil when escalation_ttl <= 0)
	escalationTTL *EscalationTTLScanner
	panPromoter   *PanFiledPromoter

	// Stale integration-exhausted park sweeper (nil when disabled)
	integrationSweep *IntegrationExhaustedSweeper
	panDigestAt      atomic.Int64 // unix-nano of last collectHealthDigest (0 = unknown)
	evidenceSweep    *ToolHealthEvidenceScanner
	tokenUsageRollup *TokenUsageRollupJob

	// Waiting-on-human-merge PR ledger + weekly aging-digest scanner (nil when
	// disabled / no HYDRA_HOME).
	prWaiting *PRWaitingLedger
	prAging   *PRAgingScanner

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

	// Periodic LLM PR-triage pass (advice only: merge order / duplicate /
	// needs-human recommendations on overlapping dogfood/* PRs). Nil when
	// disabled.
	triageSvc *triage.Service

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

	// FR-031: tier-1 canary deploy orchestrator; nil when deploy.enabled=false
	tier1Deployer *Tier1Deployer
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

	// FR-031: tier-1 canary deploy with rollback (opt-in via deploy.enabled).
	if config.Deploy.Enabled {
		o.tier1Deployer = NewTier1Deployer(config.Deploy, o.eventSink,
			func() int { return o.GetActiveWorkerCount() },
			func(n int) {
				if n < 0 {
					n = config.Overseer.MaxWorkers
				}
				o.stateController.SetMaxWorkers(n)
			})
		log.Printf("[Deploy] tier-1 canary deploy enabled (repo=%s)", config.Deploy.RepoPath)
	}

	// Initialize DAG builder
	o.dagBuilder = NewDAGBuilder(o.errorLogger)
	o.dagBuilder.Sensor = NewMissingEdgeSensor(o.eventSink, func(projectPath, title, description string) error {
		return o.filePanBead(projectPath, "", title, description)
	})

	// Prefer bv --robot-graph for DAG edges: one call per repo replaces the
	// per-bead br show N+1 pattern (86 CLI calls per rebuild) that contended on
	// the br store and silently dropped gate edges under load (2026-09-25).
	if o.bvClient != nil {
		o.dagBuilder.EdgeFetcher = func(projectPath, repoName string) (map[string][]string, error) {
			dir := projectPath
			if repoName != "" {
				dir = projectPath + "/" + repoName
			}
			return o.bvClient.RobotGraphEdges(dir)
		}
	}

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
		o.concurrencyGovernor.SetEventSink(o.eventSink)
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
	minMemAvail := config.Overseer.ResourceMonitoring.MinMemAvailableMB
	if minMemAvail == 0 {
		minMemAvail = defaultMinMemAvailableMB
	}
	o.resourceMonitor.SetMinMemAvailableMB(minMemAvail)
	log.Printf("[ResourceMonitor] Initialized with CPU: %.1f%%, Memory: %.1f%% limits, MemAvailable floor %dMB (0=unlimited)", maxCPU, maxMemory, minMemAvail)

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
	if config.Overseer.EscalationTTL > 0 {
		o.escalationTTL = NewEscalationTTLScanner(o.beadsClient, o.GetProjects, config.Overseer.EscalationTTL, o.eventSink)
	}
	// Retroactive pan-filed promotion: pan-filed beads whose text names a
	// code change are work, not human triage; the reconciler strips the
	// label so discovery admits them (see pan_filed_promotion.go).
	o.panPromoter = NewPanFiledPromoter(o.beadsClient, o.GetProjects, o.eventSink)
	if config.Overseer.IntegrationExhaustedSweep > 0 {
		o.integrationSweep = NewIntegrationExhaustedSweeper(o.beadsClient, o.GetProjects, config.Overseer.IntegrationExhaustedSweep, o.eventSink)
	}
	if o.intelligenceHub != nil {
		evidenceScanner := NewToolHealthEvidenceScanner(o.GetProjects, o.eventSink)
		evidenceScanner.SetActivityProbe(o.recentWorkerActivityProbe())
		evidenceScanner.SetHindsightProbe(o.hindsightMemoriesProbe())
		o.evidenceSweep = evidenceScanner
	}
	// PR aging sweep (bead hydra-rrtq): record waiting-on-human-merge PRs at
	// close time and produce a weekly per-owner digest for PRs open past the
	// threshold. Ledger needs HYDRA_HOME; without it recording is inert.
	if home := os.Getenv("HYDRA_HOME"); home != "" {
		o.prWaiting = NewPRWaitingLedger(home)
		if config.Overseer.PRAgingDays > 0 {
			o.prAging = NewPRAgingScanner(o.prWaiting, o.GetProjects, config.IsPRAutoMergeRepo, config.Overseer.PRAgingDays, o.eventSink)
		}
		o.beadCloser.SetPRWaitingRecorder(o.recordPRWaiting)
	}
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
	// Guard stranded-work detection against in-progress chains.
	o.orphanJanitor.SetChainCompletionDetector(o.chainCompletionDetector)
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
	o.beadSource = &BeadProvenanceIndex{}
	o.beadAnalyzer = NewBeadAnalyzer(o.projects)
	o.beadAnalyzer.SetProvenanceIndex(o.beadSource)
	if o.dagBuilder != nil {
		o.dagBuilder.Provenance = o.beadSource
	}

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
	if o.escalationTTL != nil {
		o.escalationTTL.SetLiveWorkerFunc(o.workerSpawner.HasLiveRegistryWorker)
	}

	// Session-file resume pilot (Self-heal recovery A): quality-gated resume
	// of Pi sessions after infra-class orphan deaths.
	sessionResumePilot := NewSessionResumePilot(config.SelfHealing.SessionResume)
	o.workerSpawner.SetSessionResumePilot(sessionResumePilot)

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
	o.beadCloser.SetSourceIndex(o.beadSource)
	// Gate-doctor (feature-fva4): pi-based triage of verification RED on the
	// close path. Fail-open: with the doctor absent Triage escalates uncertain,
	// so wiring it here (not optional) is safe.
	o.beadCloser.SetGateDoctor(NewGateDoctor(DefaultGateDoctorConfig()))

	// Owner-identity publish (OPERATOR RULE 2026-09-29): project -> owning
	// user -> owner PAT, fail-closed for non-daemon-owned projects.
	SetPublishIdentityResolver(func(projectPath string) (PublishIdentity, error) {
		return ResolvePublishIdentity(projectPath, o.getUserContextForProject(projectPath))
	})
	SetDaemonOwnedChecker(func(projectPath string) bool {
		uc := o.getUserContextForProject(projectPath)
		return uc == nil || uc.Username == daemonOwnerName
	})
	SetPublishGitAuthorResolver(func(projectPath string) (string, string, bool) {
		uc := o.getUserContextForProject(projectPath)
		if uc != nil {
			name, email := uc.GitAuthor()
			return name, email, true
		}
		return "", "", false // resolveGitAuthorFor falls back to the service account
	})

	// Single-source daemon credential (2026-10-01 drift incident): when
	// output.github_token_owner_user is set, the daemon's GitHub identity
	// comes from that user's ~/.hydra/.github-token — never the private
	// ~/.git-credentials copy.
	if config.Output.GithubTokenOwnerUser != "" {
		SetDaemonTokenOwnerUser(config.Output.GithubTokenOwnerUser)
		log.Printf("[PublishIdentity] daemon GitHub token owner user: %s (per-user PAT file is the single source of truth)", config.Output.GithubTokenOwnerUser)
	}

	// Daemon gate runner (FR-002): attach to the close path and recover any
	// artifacts the previous daemon incarnation died inside (running + dead
	// PID -> interrupted, never trusted). Fail-open: recovery errors are
	// logged, never gate startup.
	if home := os.Getenv("HYDRA_HOME"); home != "" {
		o.beadCloser.SetGateRunner(NewGateRunner(home))
		o.orphanJanitor.SetGateRoot(home)
		o.gateRoot = home
		if n, err := FinalizeStaleGateArtifacts(home); err != nil {
			log.Printf("[DaemonGates] stale artifact recovery failed (continuing): %v", err)
		} else if n > 0 {
			log.Printf("[DaemonGates] recovered %d interrupted gate artifact(s) from previous run", n)
		}
	}
	// Structured decision stream (Shepherd C1/C2): central day-files plus
	// per-repo .hydra/events.jsonl mirrors.
	events := NewEventSink(DefaultEventsDir())
	o.eventSink = events
	if gidx := o.intelligenceHub.GraphifyIndexer(); gidx != nil {
		gidx.SetEventSink(events)
	}
	SetPublishEventSink(events)
	o.beadCloser.SetEventSink(events)
	o.providerPool.SetEventSink(events)
	o.orphanJanitor.SetEventSink(events)
	o.concurrencyGovernor.SetEventSink(events)
	o.diskCleanup = &diskCleanupGuard{events: events}
	// Integration-retest stage (Tier A): periodic mechanical recheck of
	// integration-failed/exhausted beads. Constructed whenever HYDRA_HOME is
	// available; the stage is inert unless integration_retest.enabled is set.
	// Fail-open by construction: Run isolates per-project/panics.
	if home := os.Getenv("HYDRA_HOME"); home != "" {
		o.integrationRetest = NewIntegrationRetestStage(config.IntegrationRetest, config, o.beadsClient, events, NewGateRunner(home), home, o.chainKeyForBeadID)
		if o.integrationRetest.Enabled() {
			log.Printf("[IntegrationRetest] stage armed (max_attempts=%d retry_interval=%s)",
				o.integrationRetest.cfg.MaxAttempts, o.integrationRetest.cfg.RetryInterval)
		}
		// Fast-path absorption: when a gated integration succeeds, parked
		// siblings on the same repo retest immediately (interval bypass inside
		// a short window) instead of waiting out the hourly RetryInterval.
		SetIntegrationOpenNotifier(func(projectPath string) {
			o.integrationRetest.NotifyRepoOpen(projectPath, o.projects)
		})
	}
	retainer := NewVerdictRetainer(o.hindsightClient)
	o.beadCloser.SetVerdictRetainer(retainer)

	// Wire retry/spawn state purgers into the close path so verified closes
	// clear retry and spawn-failure residue for the bead.
	o.beadCloser.SetRetryManager(o.retryManager)
	o.beadCloser.SetWorkerSpawner(o.workerSpawner)
	o.beadCloser.SetSessionResumePilot(sessionResumePilot)

	// Per-chain worktree isolation (opt-in via overseer.worktree_isolation).
	// Wires the manager into spawn (run worker in the chain's worktree) and
	// close (commit its work); integration happens on chain completion.
	if config.Overseer.WorktreeIsolation {
		o.worktreeManager = NewWorktreeManager()
		o.workerSpawner.SetWorktreeManager(o.worktreeManager, o.chainKeyForBead)
		o.beadCloser.SetWorktreeManager(o.worktreeManager, o.chainKeyForBead, o.isChainMember)
		// The quarantine reconciler is constructed in ServiceStarter — it needs
		// beadFileReader, which does not exist yet at this point.
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
	// Periodic LLM PR-triage pass (advice only; hydra-muvh FR-030). Nil when
	// disabled — ServiceStarter starts it only if non-nil.
	if config.Triage.Enabled {
		interval := time.Duration(config.Triage.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = time.Hour
		}
		o.triageSvc = triage.New(interval, triage.JudgeConfig{
			Provider:   config.Triage.Provider,
			Model:      config.Triage.Model,
			Thinking:   config.Triage.Thinking,
			APIKeyName: config.Triage.APIKeyName,
		}, func() []triage.Project {
			// Read live at tick time: multi-user discovery appends projects
			// after NewOverseer returns.
			projects := make([]triage.Project, 0, len(o.projects))
			for _, p := range o.projects {
				if p.Type == "multi" {
					for _, r := range p.Repos {
						projects = append(projects, triage.Project{Name: p.Name + "/" + r.Name, Path: resolveRepoPath(p, r)})
					}
					continue
				}
				projects = append(projects, triage.Project{Name: p.Name, Path: p.Path})
			}
			return projects
		}, triageStatePath())
		log.Printf("[Triage] Initialized (interval %v, model %s/%s)", interval, config.Triage.Provider, config.Triage.Model)
	}
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
	target := normalizeProjectPath(projectPath)
	for _, ctx := range o.userContexts {
		for _, p := range ctx.Config.Projects {
			pp := normalizeProjectPath(p.Path)
			if pp == target {
				return ctx
			}
			// Multi projects: repos live as children under the configured
			// parent dir (e.g. ~/projects). A repo path inside the parent
			// belongs to this user — without this, per-owner publish identity
			// silently fell back to the daemon identity for every
			// auto-discovered repo (hydra-k3nb L2, exposed 2026-10-05).
			if p.Type == "multi" && strings.HasPrefix(target, pp+string(filepath.Separator)) {
				return ctx
			}
		}
	}
	// Attribution miss on a user-home path silently falls back to the daemon
	// identity (hydra-k3nb L2): surface it so a misconfigured/moved project
	// does not publish under the daemon owner's credentials.
	if strings.HasPrefix(target, "/home/") {
		log.Printf("[Overseer] WARN: no user context for project %s — publish identity falls back to daemon owner", target)
	}
	return nil
}

// normalizeProjectPath canonicalizes a project path for attribution matching:
// Clean removes trailing slashes, EvalSymlinks collapses compatibility links
// (best-effort; a missing path just skips the symlink step).
func normalizeProjectPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
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

// refreshDiscoveredProjects re-runs multi-user discovery and converges the
// overseer's project set: new (name,path) projects are appended, existing
// multi projects pick up newly discovered repos in place. Removal is
// conservative — vanished entries are left alone (they list empty and cost
// nothing) rather than risking active work. All downstream consumers
// (analyzer, resolver, file reader, discovery, spawner contexts) are
// re-pointed at the merged list.
func (o *Overseer) refreshDiscoveredProjects() {
	if o.config == nil || !o.config.MultiUserDiscovery {
		return
	}
	d := NewMultiUserDiscovery()
	d.HomeDir = o.discoveryHomeDir
	d.SetExclude(o.config.AutoDiscoverExclude)
	if err := d.Discover(); err != nil {
		log.Printf("[MultiUser] re-discovery error (keeping existing projects): %v", err)
		return
	}
	fresh := d.Projects()

	existing := make(map[string]bool, len(o.projects))
	for _, p := range o.projects {
		existing[p.Name+"|"+p.Path] = true
	}

	var added []types.Project
	for _, fp := range fresh {
		key := fp.Name + "|" + fp.Path
		if existing[key] {
			continue
		}
		existing[key] = true
		added = append(added, fp)
	}

	// Refresh repo lists of already-registered multi projects in place so
	// repos added under a multi dir start scheduling without a restart.
	for i := range o.projects {
		for _, fp := range fresh {
			if fp.Name == o.projects[i].Name && fp.Path == o.projects[i].Path && len(fp.Repos) > 0 {
				o.projects[i].Repos = fp.Repos
			}
		}
	}

	if len(added) > 0 {
		names := make([]string, 0, len(added))
		for _, p := range added {
			names = append(names, p.Name)
		}
		o.projects = append(o.projects, added...)
		log.Printf("[MultiUser] Dynamically added %d project(s): %v", len(added), names)
	}

	o.beadAnalyzer.SetProjects(o.projects)
	o.projectResolver = NewProjectResolver(o.projects)
	if o.beadFileReader != nil {
		o.beadFileReader.SetProjects(o.projects)
	}
	if o.beadDiscovery != nil {
		o.beadDiscovery.SetProjects(o.projects)
	}
	// Merge user contexts, never replace: a user whose config is temporarily
	// unreadable (or who lives outside the scan root, as in tests) must not
	// vanish mid-run — that would strand attribution and block their spawns.
	if o.userContexts == nil {
		o.userContexts = make(map[string]*UserContext)
	}
	for name, ctx := range d.Users() {
		o.userContexts[name] = ctx
	}
	if o.workerSpawner != nil {
		o.workerSpawner.SetUserContexts(o.userContexts)
	}
}

// tick performs one iteration of the scheduling loop.
func (o *Overseer) tick() error {
	// Periodic multi-user re-discovery: new users, edited configs, and repos
	// added under a multi dir onboard here — no daemon restart needed.
	interval := o.discoveryRefreshInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if time.Since(o.lastDiscoveryRefresh) >= interval {
		o.lastDiscoveryRefresh = time.Now()
		o.refreshDiscoveredProjects()
	}
	err := o.scheduler.Tick()
	// Fail-open: clear stale worktree-unavailable hard-park labels when the
	// worktree probe has recovered for two consecutive ticks.
	o.reconcileHardParks()
	// After scheduling, publish each project's working branch to its remote
	// (opt-in via output.push). This consolidates completed worktree commits
	// onto the working branch and pushes with the project owner's PAT, so by
	// the end of a run every repo's branch is on the remote for the developer
	// to pull/build locally. Fail-open: never affects the scheduling result.
	o.publishProjects()
	// After publishing, sweep integration-parked beads for provably-clean
	// retests (fail-open: never affects the scheduling result).
	if o.integrationRetest != nil {
		o.integrationRetest.Run(o.projects, o.activeWorkerKeys())
	}
	return err
}

// activeWorkerKeys returns the set of bead IDs with a live worker row in
// this daemon, so maintenance sweeps (publish reconcile, integration
// retest) never touch in-flight beads.
func (o *Overseer) activeWorkerKeys() map[string]bool {
	o.workerMutex.RLock()
	defer o.workerMutex.RUnlock()
	active := make(map[string]bool, len(o.activeWorkers))
	for beadID := range o.activeWorkers {
		active[beadID] = true
	}
	return active
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
		o.publishOneProject(project, active)
		if o.config != nil && o.config.Worktree.QuarantineAutoDrain {
			if _, _, err := DrainQuarantineBranches(project.Path, false); err != nil {
				log.Printf("[publishProjects] drain quarantine for %s: %v", project.Path, err)
			}
		}
	}
}

// reconcileHardParks runs the worktree-unavailable label reconciler at most
// once per hardParkReconcileInterval. Fail-open: any error inside the
// reconciler is logged there and never affects the scheduling tick.
func (o *Overseer) reconcileHardParks() {
	if time.Since(o.lastHardParkReconcile) < hardParkReconcileInterval {
		return
	}
	o.lastHardParkReconcile = time.Now()
	if o.hardParkProbes == nil {
		o.hardParkProbes = map[string]int{}
	}
	ReconcileHardParkedWorktrees(o.beadsClient, o.projects, nil, o.hardParkProbes, o.eventSink)
}

// PushPendingEntry records a project whose integration/working branch holds
// commits that have not landed on origin. publishProjects previously
// discarded PublishRepo's (pushed, reason) result, so a failing push retried
// silently forever — the "commits not pushed properly" class ajantha reported
// (2026-09-13). The entry feeds Pan as a PUSH-PENDING candidate.
type PushPendingEntry struct {
	ProjectPath string
	ProjectName string
	Since       time.Time
	Reason      string
	// Failures tracks consecutive non-benign publish failures for the
	// project (failure state tracking for backoff/escalation logic).
	Failures int
	// LastClass records the publish failure classification of the most
	// recent failure (ClassifyPushFailure result, e.g. TRANSIENT,
	// CREDENTIAL) so backoff logic can distinguish transient vs permanent.
	LastClass string
}

// benignPublishReasons are PublishRepo results that mean "nothing stranded".
var benignPublishReasons = map[string]bool{
	"up to date":          true,
	"no branch":           true,
	"no worktree manager": true,
	"already integrated":  true, // branch tip already merged into origin base — no PR to make (hydra-ybhh)
}

// publishOneProject runs PublishRepo for a single project and tracks the
// push-pending state. Returns PublishRepo's result verbatim. Push
// authenticates with the bead owner's PAT (owner-identity publish,
// fail-closed via ResolvePublishIdentity; daemon-owned repos use the service
// account's own credential).
func (o *Overseer) publishOneProject(project types.Project, active map[string]bool) (bool, string) {
	if project.Type == types.RepoTypeMulti && len(project.Repos) > 0 {
		return o.publishOneProjectRepos(project, active)
	}
	return o.publishOneRepo(project, project.Path, active)
}

// publishOneProjectRepos publishes every repo of a multi-repo project: the
// parent directory is not a git repository (git rev-parse fatal: not a git
// repository), so publishing must run per repo (nipuna.j fms incident,
// 2026-09-30). Identity resolves once on the parent project path (where the
// user config matches); push-pending state is aggregated on the parent.
func (o *Overseer) publishOneProjectRepos(project types.Project, active map[string]bool) (bool, string) {
	if o.publishBackoffActive(project.Path) {
		return false, "publish backoff"
	}
	id, err := ResolvePublishIdentity(project.Path, o.getUserContextForProject(project.Path))
	token := ""
	if err != nil {
		log.Printf("[Publish] %s: %v (publish blocked, fail-closed)", project.Path, err)
		if o.eventSink != nil {
			o.eventSink.Emit(Event{Type: "publish_identity_missing", Project: project.Name, Reason: "owner_pat_unresolved", Detail: err.Error()})
		}
	} else {
		token = id.Token
		if o.eventSink != nil {
			o.eventSink.Emit(Event{Type: "publish_identity_resolved", Project: project.Name, Fields: map[string]any{"owner": id.Owner, "token_source": id.Source}})
		}
	}
	pushedAny := false
	lastReason := ""
	allBenign := true
	for _, repo := range project.Repos {
		repoPath := resolveRepoPath(project, repo)
		if repoPath == "" {
			continue
		}
		pushed, reason := PublishRepo(o.worktreeManager, repoPath, token, o.config.PRMergeEnabled(), active, o.terminalBeadChecker(repoPath))
		if pushed {
			pushedAny = true
		}
		if !benignPublishReasons[reason] {
			allBenign = false
			lastReason = reason
		}
	}
	if len(project.Repos) == 0 || pushedAny || (allBenign && lastReason == "") {
		o.clearPushPending(project.Path)
		o.clearPublishBackoff(project.Path)
	} else {
		o.notePushPending(project, lastReason)
		o.notePublishBackoff(project.Path, lastReason)
	}
	return pushedAny, lastReason
}

// publishOneRepo runs PublishRepo for one repo path and tracks the push-
// pending state. Returns PublishRepo's result verbatim. Push
// authenticates with the bead owner's PAT (owner-identity publish,
// fail-closed via ResolvePublishIdentity; daemon-owned repos use the service
// account's own credential).
func (o *Overseer) publishOneRepo(project types.Project, projectPath string, active map[string]bool) (bool, string) {
	if o.publishBackoffActive(projectPath) {
		return false, "publish backoff"
	}
	id, err := ResolvePublishIdentity(project.Path, o.getUserContextForProject(project.Path))
	token := ""
	if err != nil {
		log.Printf("[Publish] %s: %v (publish blocked, fail-closed)", project.Path, err)
		if o.eventSink != nil {
			o.eventSink.Emit(Event{Type: "publish_identity_missing", Project: project.Name, Reason: "owner_pat_unresolved", Detail: err.Error()})
		}
	} else {
		token = id.Token
		if o.eventSink != nil {
			o.eventSink.Emit(Event{Type: "publish_identity_resolved", Project: project.Name, Fields: map[string]any{"owner": id.Owner, "token_source": id.Source}})
		}
	}
	pushed, reason := PublishRepo(o.worktreeManager, project.Path, token, o.config.PRMergeEnabled(), active, o.terminalBeadChecker(project.Path))
	if pushed && o.tier1Deployer != nil && o.tier1Deployer.cfg.Enabled &&
		o.tier1Deployer.cfg.RepoPath != "" && project.Path == o.tier1Deployer.cfg.RepoPath {
		go o.tier1Deployer.RunAfterMerge(filepath.Base(project.Path))
	}
	if pushed || benignPublishReasons[reason] {
		o.clearPushPending(project.Path)
		o.clearPublishBackoff(projectPath)
	} else {
		o.notePushPending(project, reason)
		o.notePublishBackoff(projectPath, reason)
	}
	return pushed, reason
}

// publishBackoffEntry records the exponential retry cooldown for a
// project whose PublishRepo keeps failing. Without it, a persistent
// failure (stale token, PR waiting on human merge, missing remote)
// re-attempted the full publish every scheduler tick: 2,709 classified
// failures + 6,721 identity resolutions in 12h across three projects
// (2026-10-02 fleet digest). Base 2m, doubling, capped 15m; success or a
// benign result clears it.
type publishBackoffEntry struct {
	attempt int
	next    time.Time
}

const (
	publishBackoffBase = 2 * time.Minute
	publishBackoffMax  = 15 * time.Minute
)

// publishBackoffActive reports whether the project's publish tick is
// currently in a failure cooldown. Backoff is per process (in-memory):
// a daemon restart gives every project one immediate fair retry, which
// is the desired fail-open behavior.
func (o *Overseer) publishBackoffActive(projectPath string) bool {
	if o == nil {
		return false
	}
	o.pushPendingMu.Lock()
	defer o.pushPendingMu.Unlock()
	e, ok := o.publishBackoff[projectPath]
	return ok && time.Now().Before(e.next)
}

// notePublishBackoff extends the project's publish cooldown exponentially
// after another non-benign publish failure.
func (o *Overseer) notePublishBackoff(projectPath, reason string) {
	if o == nil {
		return
	}
	o.pushPendingMu.Lock()
	defer o.pushPendingMu.Unlock()
	if o.publishBackoff == nil {
		o.publishBackoff = make(map[string]*publishBackoffEntry)
	}
	e, ok := o.publishBackoff[projectPath]
	if !ok {
		e = &publishBackoffEntry{}
		o.publishBackoff[projectPath] = e
	}
	e.attempt++
	d := publishBackoffBase << uint(min(e.attempt-1, 20))
	if d > publishBackoffMax || d <= 0 {
		d = publishBackoffMax
	}
	e.next = time.Now().Add(d)
	log.Printf("[Publish] backoff %s: attempt %d, next publish eligible in %v (%s)", projectPath, e.attempt, d, reason)
}

// clearPublishBackoff resets the project's publish cooldown after a
// successful or benign publish tick.
func (o *Overseer) clearPublishBackoff(projectPath string) {
	if o == nil {
		return
	}
	o.pushPendingMu.Lock()
	defer o.pushPendingMu.Unlock()
	if _, ok := o.publishBackoff[projectPath]; ok {
		delete(o.publishBackoff, projectPath)
	}
}

func (o *Overseer) notePushPending(project types.Project, reason string) {
	o.pushPendingMu.Lock()
	defer o.pushPendingMu.Unlock()
	if o.pushPending == nil {
		o.pushPending = make(map[string]*PushPendingEntry)
	}
	cls := string(ClassifyPushFailure(reason))
	if e, ok := o.pushPending[project.Path]; ok {
		e.Reason = reason
		e.Failures++
		e.LastClass = cls
		return
	}
	o.pushPending[project.Path] = &PushPendingEntry{
		ProjectPath: project.Path,
		ProjectName: project.Name,
		Since:       time.Now(),
		Reason:      reason,
		Failures:    1,
		LastClass:   cls,
	}
	log.Printf("[Publish] push pending for %s: %s (surfaced to Pan as PUSH-PENDING)", project.Path, reason)
}

func (o *Overseer) clearPushPending(projectPath string) {
	o.pushPendingMu.Lock()
	defer o.pushPendingMu.Unlock()
	if _, ok := o.pushPending[projectPath]; ok {
		log.Printf("[Publish] push pending cleared for %s", projectPath)
		delete(o.pushPending, projectPath)
	}
}

// PushPending returns a snapshot of projects with stranded pushes.
func (o *Overseer) PushPending() []PushPendingEntry {
	o.pushPendingMu.Lock()
	defer o.pushPendingMu.Unlock()
	out := make([]PushPendingEntry, 0, len(o.pushPending))
	for _, e := range o.pushPending {
		out = append(out, *e)
	}
	return out
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

	o.beadSource.Reset()
	if o.dagBuilder != nil {
		o.dagBuilder.Provenance = o.beadSource
	}
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
// during the br CLI call; it re-acquires dagMu for its own mutations.
//
// Store-verification gate (regression 2026-09-30, fmsfe incident): the DAG
// build can miss open children (their repo's ListBeads fails and is skipped,
// or they are otherwise filtered from the build), so CloseReadyEpics sees a
// vacuously-empty child set and closes the epic over open work. Before
// closing, every project's bead stores are scanned for non-terminal
// dependents of the epic; any open dependent, or any listing error, refuses
// the close (fail-closed).
func (o *Overseer) persistClosedEpics(beadIDs []string) {
	o.dagMu.Lock()
	defer o.dagMu.Unlock()
	for _, id := range beadIDs {
		if o.epicCloseGuard.recentlyRefused(id, time.Now()) {
			continue
		}
		projectPath, _ := o.findProjectPathForBead(id)
		if projectPath == "" {
			log.Printf("[DAG] Could not find project path for epic %s; skipping persistence", id)
			o.epicCloseGuard.markRefused(id, time.Now())
			continue
		}
		if o.epicHasOpenDependent(id, projectPath) {
			log.Printf("[DAG] Epic %s has open dependents in the beads store; refusing auto-close (DAG view incomplete)", id)
			o.epicCloseGuard.markRefused(id, time.Now())
			if o.eventSink != nil {
				o.eventSink.Emit(Event{Type: "epic_close_refused", Bead: id,
					Reason: "open_dependents",
					Detail: "CloseReadyEpics proposed closing this epic but a bead-store scan found non-terminal dependents; close refused (fail-closed)."})
			}
			continue
		}
		reason := "epic auto-close: all dependencies and children closed"
		err := o.beadsClient.CloseBead(projectPath, "", types.BeadID(id), reason)
		if err != nil {
			// br itself refused the close because the epic still has open
			// children in its own parent-child graph (the ground truth the
			// DAG and the Dependencies-based store scan cannot see when
			// compile-time epic-child links failed). Classify it as a
			// refusal so the throttle applies and operators get a typed
			// event (srv-jobfile master epic: closed with 33 open children).
			msg := err.Error()
			if strings.Contains(msg, "open children") || strings.Contains(msg, "Skipped") {
				o.epicCloseGuard.markRefused(id, time.Now())
				log.Printf("[DAG] Epic %s auto-close refused by br (open children); throttled for re-check", id)
				if o.eventSink != nil {
					o.eventSink.Emit(Event{Type: "epic_close_refused", Bead: id,
						Reason: "open_children",
						Detail: msg})
				}
			} else {
				log.Printf("[DAG] Failed to persist close for epic %s: %v", id, err)
			}
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
	// Prefer the DAG's bead record: bead IDs may be user-namespaced
	// ("ajantha-srv-smart-engine-1u3") while project.Name is not
	// ("srv-smart-engine"), so the raw prefix match below never fires and
	// epic auto-close persistence silently no-ops forever (hydra-ysaz).
	// dag carries its own mutex, so this is safe under dagMu.
	if o.dag != nil {
		if node, ok := o.dag.GetNode(beadID); ok && node.Bead.Project != "" {
			for _, project := range o.GetProjects() {
				if project.Name == node.Bead.Project {
					if project.Type == types.RepoTypeMulti && len(project.Repos) > 0 {
						if dir := repoDirForBead(project, beadID, o.beadsClient); dir != "" {
							return dir, types.BeadID(beadID)
						}
					}
					return project.Path, types.BeadID(beadID)
				}
			}
		}
	}
	for _, project := range o.GetProjects() {
		if len(beadID) > len(project.Name) && beadID[:len(project.Name)+1] == project.Name+"-" {
			if project.Type == types.RepoTypeMulti && len(project.Repos) > 0 {
				// Multi-repo: the parent dir has no .beads store, so a close
				// there fails ("Could not find project path for epic"). Find
				// the repo whose bead store contains the exact ID
				// (nipuna.j fms incident, 2026-09-30).
				if dir := repoDirForBead(project, beadID, o.beadsClient); dir != "" {
					return dir, types.BeadID(beadID)
				}
			}
			return project.Path, types.BeadID(beadID)
		}
	}
	// Namespaced-ID fallback: strip the trailing "-<suffix>" and match the
	// project name as the whole namespace or a hyphen-delimited tail of it
	// ("ajantha-srv-smart-engine-1u3" -> namespace "ajantha-srv-smart-engine"
	// tail-matches "srv-smart-engine"). Longest name first so
	// "srv-smart-engine" beats "engine"; naive Contains("-"+name+"-")
	// substring-matches hyphenated names ("-engine-" inside "smart-engine-").
	ns := beadID
	if idx := strings.LastIndex(beadID, "-"); idx > 0 {
		ns = beadID[:idx]
	}
	projects := append([]types.Project(nil), o.GetProjects()...)
	sort.Slice(projects, func(i, j int) bool { return len(projects[i].Name) > len(projects[j].Name) })
	for _, project := range projects {
		if ns == project.Name || strings.HasSuffix(ns, "-"+project.Name) {
			if project.Type == types.RepoTypeMulti && len(project.Repos) > 0 {
				if dir := repoDirForBead(project, beadID, o.beadsClient); dir != "" {
					return dir, types.BeadID(beadID)
				}
			}
			return project.Path, types.BeadID(beadID)
		}
	}
	// Fallback: multi-discovered bead IDs carry the repo store's prefix
	// ("fmsfe-b9o" from repo fms-frontend), which matches no project name
	// ("fms"). Scan multi projects' repo stores for the exact ID.
	for _, project := range o.GetProjects() {
		if project.Type != types.RepoTypeMulti || len(project.Repos) == 0 {
			continue
		}
		if dir := repoDirForBead(project, beadID, o.beadsClient); dir != "" {
			return dir, types.BeadID(beadID)
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
		switch {
		case err != nil:
			log.Printf("[Spawn] %s store status read failed: %v", beadIDStr, err)
		case current != nil && current.Status.IsTerminal():
			// Drop the stale retry entry so the scheduler stops offering it.
			o.retryManager.ClearRetry(beadIDStr)
			o.emitSpawnSuppressedClosed(bead, projectPath, string(current.Status))
			return &SpawnSuppressedError{BeadID: beadIDStr, Status: current.Status}
		case current != nil:
			// fmsfe-a2u (2026-09-30): post-restart reconstructed beads can carry
			// an empty Type/Project, silently no-opping every scheduler type
			// filter. Re-read the metadata from store truth immediately before
			// spawning so the preflight below sees the real type.
			if strings.TrimSpace(bead.Type) == "" {
				bead.Type = current.Type
			}
			if bead.Project == "" {
				bead.Project = current.Project
			}
			// hydra-bldo: label truth matters at spawn time too — a pan-filed
			// (human-triage) label added by Pan while the stale in-memory copy
			// was queued must be seen by the preflight below.
			if len(current.Labels) > 0 {
				bead.Labels = current.Labels
			}
		}
	}

	// Fail-closed preflight (fmsfe-a2u, 2026-09-30): a bead whose Type cannot
	// be verified, or whose store Type is a human gate/container, must never
	// reach a worker. Also blocks on a foreign-uid .beads store (daemon stops
	// managing user-owned repos) and on an unresolved owner publish identity
	// (auto-resumes when the PAT lands).
	if err := o.spawnPreflightBead(bead, projectPath); err != nil {
		return err
	}
	if err := o.spawnPreflightStoreOwnership(bead, projectPath); err != nil {
		return err
	}
	if err := o.spawnPreflightGitAccess(bead, projectPath); err != nil {
		return err
	}
	if err := o.spawnPreflightPublishIdentity(bead, projectPath); err != nil {
		return err
	}
	if err := o.spawnPreflightBeadClaims(bead, projectPath); err != nil {
		return err
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
		if o.campaignParked(node.Bead) {
			log.Printf("⏸️  Skipping bead %s: campaign parked (Pan)", node.Bead.ID)
			continue
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
	if isNonExecutableBeadType(node.Bead.Type) {
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

// SetMaxWorkers updates the maximum number of concurrent workers. Both the
// state controller and the AIMD concurrency governor are updated: the governor
// keeps its own ceiling/limit, so without syncing it silently clamps any
// runtime increase to its construction-time value until daemon restart.
func (o *Overseer) SetMaxWorkers(count int) {
	o.stateController.SetMaxWorkers(count)
	o.concurrencyGovernor.SetMaxWorkers(count)
	log.Printf("🔧 Max workers set to %d", count)
}

// GetMaxWorkers returns the current maximum worker count
func (o *Overseer) GetMaxWorkers() int {
	return o.stateController.GetMaxWorkers()
}

// GetConcurrencyLimit returns the AIMD governor's current effective limit
// (0 when the governor is disabled/unlimited). Used by the API to expose and
// verify that runtime SetMaxWorkers updates reach the governor.
func (o *Overseer) GetConcurrencyLimit() int {
	return o.concurrencyGovernor.Limit()
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
	o.consultPackOnReservation(msg)
}

// consultPackOnReservation checks a worker-initiated file reservation
// against the bead's context-pack anchors. Hydra's reservation stream is
// informational (handleFileReserved only records it — worker-to-worker
// conflict enforcement lives in the MCP reservation client), so an
// off-anchor reservation emits an advisory pack_reservation_offscope event
// rather than blocking; the real enforcement remains the close-time scope
// gate, which consults the same anchors. Fail-open: any lookup failure
// returns silently.
func (o *Overseer) consultPackOnReservation(msg *mcp.Message) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ContextPack] consultPackOnReservation panicked (fail-open): %v", r)
		}
	}()
	beadID, _ := msg.Payload["bead_id"].(string)
	filePath, _ := msg.Payload["file_path"].(string)
	if beadID == "" || filePath == "" || o.beadsClient == nil {
		return
	}
	projectPath := o.beadSource.Lookup(beadID)
	if projectPath == "" {
		return
	}
	bead, err := o.beadsClient.GetBead(projectPath, "", types.BeadID(beadID))
	if err != nil || bead == nil {
		return
	}
	pack, err := ParseContextPack(bead.AgentContext)
	if err != nil || pack == nil || len(pack.Files) == 0 {
		return
	}
	anchorTargets := make([]string, 0, len(pack.Files))
	for _, a := range pack.Files {
		anchorTargets = append(anchorTargets, a.Path)
	}
	if violations := scopeViolations([]string{filePath}, anchorTargets); len(violations) == 0 {
		return
	}
	log.Printf("[ContextPack] bead %s reserved off-anchor file %s (anchors: %v)", beadID, filePath, anchorTargets)
	if o.eventSink != nil {
		o.eventSink.EmitRepo(projectPath, Event{Type: "pack_reservation_offscope", Project: bead.Project,
			Bead: beadID, Reason: "off_anchor_reservation",
			Detail: "reserved file outside context-pack anchors: " + filePath})
	}
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

// --- Operator API accessors (FR-033) ---

// RegistryStore exposes the worker registry store for the operator API. May be
// nil until the service starter opens the registry.
func (o *Overseer) RegistryStore() *registry.Store { return o.registryStore }

// EventSink exposes the shared structured event sink for operator-API audit
// events. Never nil after NewOverseer.
func (o *Overseer) EventSink() *EventSink { return o.eventSink }

// BeadsClient exposes the beads client for operator quarantine queries.
func (o *Overseer) BeadsClient() BeadsClient { return o.beadsClient }

// Projects exposes the configured project list for operator API scoping.
func (o *Overseer) Projects() []types.Project { return o.projects }
