package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Retry configuration constants
const (
	defaultMaxRetries    = 3
	defaultBaseDelay     = 5 * time.Second
	defaultMaxDelay      = 5 * time.Minute
	defaultRetryCooldown = 10 * time.Second
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

// BeadsClient defines the interface for beads operations
// This allows for mocking in tests
type BeadsClient interface {
	ListBeads(projectPath, repoName string) ([]types.Bead, error)
	GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error)
	CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error
	GetReadyBeads(projectPath, repoName string) ([]types.Bead, error)
}

// Overseer is the main scheduler daemon
type Overseer struct {
	config        *Config
	projects      []types.Project
	activeWorkers map[string]*types.Worker
	workerMutex   sync.RWMutex
	running       bool
	paused        bool
	pauseMutex    sync.RWMutex
	pollInterval  time.Duration
	maxWorkers    int
	maxWorkersMux sync.RWMutex
	beadsClient   BeadsClient
	dag           *DAG
	dagMu         sync.RWMutex
	completed     map[string]bool           // Track completed beads for this session
	retries       map[string]*beadRetryInfo // Track retry state for failed beads
	retriesMu     sync.RWMutex              // Mutex for retries map
	baseDelay     time.Duration             // Base delay for exponential backoff
	maxDelay      time.Duration             // Maximum delay for exponential backoff
}

// NewOverseer creates a new overseer instance
func NewOverseer(config *Config) *Overseer {
	maxRetries := config.Overseer.DefaultRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	o := &Overseer{
		config:        config,
		activeWorkers: make(map[string]*types.Worker),
		pollInterval:  config.Overseer.PollInterval,
		maxWorkers:    config.Overseer.MaxWorkers,
		beadsClient:   beads.NewClient(),
		dag:           NewDAG(),
		completed:     make(map[string]bool),
		retries:       make(map[string]*beadRetryInfo),
		baseDelay:     defaultBaseDelay,
		maxDelay:      defaultMaxDelay,
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

// Start begins the overseer's main loop
func (o *Overseer) Start(ctx context.Context) error {
	log.Println("🚀 Starting Hydra overseer...")

	// Discover projects
	if err := o.discoverProjects(); err != nil {
		return fmt.Errorf("failed to discover projects: %w", err)
	}

	log.Printf("📁 Discovered %d projects", len(o.projects))

	o.running = true
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			o.running = false
			return nil
		case <-ticker.C:
			if err := o.tick(); err != nil {
				log.Printf("Error during tick: %v", err)
			}
		}
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
	availableSlots := maxWorkers - activeCount
	if availableSlots <= 0 {
		return nil
	}

	// Build/refresh DAG from all beads
	if err := o.buildDAG(); err != nil {
		// Graceful degradation: log error and continue with existing DAG
		o.logGracefulDegradation("Overseer", "buildDAG", "using existing DAG", err)
		// Continue with existing DAG if rebuild fails
	}

	// Get ready beads from DAG (InDegree == 0)
	readyBeads, err := o.getReadyBeadsFromDAG()
	if err != nil {
		return fmt.Errorf("failed to get ready beads from DAG: %w", err)
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

	// Spawn workers for ready beads
	for _, bead := range retryBeads {
		if availableSlots <= 0 {
			break
		}

		beadIDStr := string(bead.ID)

		// Check if this is a retry and log appropriately
		if retryInfo, exists := o.retries[beadIDStr]; exists {
			log.Printf("[Retry] Attempting retry %d/%d for bead %s",
				retryInfo.count+1, o.config.Overseer.DefaultRetries, bead.ID)
		}

		// Skip approval gates (require manual br close)
		if bead.Type == "approval-gate" {
			log.Printf("⏸️  Skipping approval gate: %s", bead.ID)
			continue
		}

		if err := o.spawnWorker(bead); err != nil {
			// Record retry and log error
			o.recordRetry(beadIDStr, err)
			o.logError("Overseer", "spawnWorker", beadIDStr, err, "error")
			continue
		}

		// Clear retry state on successful spawn
		o.clearRetry(beadIDStr)
		availableSlots--
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

// getReadyBeadsFromDAG returns beads that have no incomplete dependencies (InDegree == 0)
func (o *Overseer) getReadyBeadsFromDAG() ([]types.Bead, error) {
	o.dagMu.RLock()
	defer o.dagMu.RUnlock()

	// Get all nodes with InDegree == 0
	nodes := o.dag.GetReadyNodes()

	// Filter and collect ready beads
	var readyBeads []types.Bead
	o.workerMutex.RLock()
	for _, node := range nodes {
		beadID := string(node.Bead.ID)

		// Filter: only StatusOpen
		if node.Bead.Status != types.StatusOpen {
			continue
		}

		// Filter: not already running
		if _, isRunning := o.activeWorkers[beadID]; isRunning {
			continue
		}

		// Filter: not already completed in this session
		if o.completed[beadID] {
			continue
		}

		// Skip approval gates (handled in tick, but also filter here)
		if node.Bead.Type == "approval-gate" {
			continue
		}

		readyBeads = append(readyBeads, node.Bead)
	}
	o.workerMutex.RUnlock()

	// Sort by priority (high to low)
	sort.Slice(readyBeads, func(i, j int) bool {
		return readyBeads[i].Priority > readyBeads[j].Priority
	})

	log.Printf("📋 Found %d ready beads from DAG (after filtering)", len(readyBeads))

	return readyBeads, nil
}

// getReadyBeads retrieves beads that are ready to execute
func (o *Overseer) getReadyBeads() ([]types.Bead, error) {
	var readyBeads []types.Bead

	for _, project := range o.projects {
		if project.Type == types.RepoTypeSingle {
			beads, err := o.beadsClient.GetReadyBeads(project.Path, "")
			if err != nil {
				log.Printf("⚠️  Could not get beads for project %s: %v", project.Name, err)
				continue
			}
			readyBeads = append(readyBeads, beads...)
		} else {
			// Multi-repo project
			for _, repo := range project.Repos {
				beads, err := o.beadsClient.GetReadyBeads(project.Path, repo.Name)
				if err != nil {
					log.Printf("⚠️  Could not get beads for repo %s/%s: %v", project.Name, repo.Name, err)
					continue
				}
				readyBeads = append(readyBeads, beads...)
			}
		}
	}

	return readyBeads, nil
}

// spawnWorker creates and starts a worker for a bead
func (o *Overseer) spawnWorker(bead types.Bead) error {
	beadIDStr := bead.ID.String()

	// Check if worker already exists
	o.workerMutex.RLock()
	if _, exists := o.activeWorkers[beadIDStr]; exists {
		o.workerMutex.RUnlock()
		o.logError("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
		return fmt.Errorf("worker already exists for bead %s", beadIDStr)
	}
	o.workerMutex.RUnlock()

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

	// Determine agent type
	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) == 0 {
		err := fmt.Errorf("no agent type found for bead type: %s", bead.Type)
		o.logError("spawnWorker", "agent_type", beadIDStr, err, "error")
		return err
	}

	agentType := agentTypes[0] // Use primary agent

	retryCount := o.getRetryCount(beadIDStr)
	if retryCount > 0 {
		log.Printf("[Worker] Spawning retry worker for bead: %s (attempt %d, agent: %s)", beadIDStr, retryCount+1, agentType)
	} else {
		log.Printf("[Worker] Spawning worker for bead: %s (agent: %s)", beadIDStr, agentType)
	}

	// Build worker command
	cmd := exec.Command("go", "run", "cmd/worker/main.go")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HYDRA_BEAD_ID=%s", beadIDStr),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
		fmt.Sprintf("HYDRA_RETRY_COUNT=%d", retryCount),
	)

	// Start process
	if err := cmd.Start(); err != nil {
		spawnErr := &WorkerSpawnError{
			BeadID:    beadIDStr,
			AgentType: string(agentType),
			Err:       err,
		}
		o.logError("spawnWorker", "process_start", beadIDStr, spawnErr, "error")
		return spawnErr
	}

	// Create worker record
	worker := &types.Worker{
		ID:        beadIDStr,
		BeadID:    bead.ID,
		PID:       cmd.Process.Pid,
		AgentType: agentType,
		StartTime: time.Now(),
		Status:    "running",
	}

	// Track worker
	o.workerMutex.Lock()
	o.activeWorkers[beadIDStr] = worker
	o.workerMutex.Unlock()

	// Monitor in goroutine
	go o.monitorWorker(worker, cmd)

	return nil
}

// monitorWorker watches a worker process until completion
func (o *Overseer) monitorWorker(worker *types.Worker, cmd *exec.Cmd) {
	// Wait for process to complete
	err := cmd.Wait()

	// Determine success/failure
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}

	success := exitCode == 0
	duration := time.Since(worker.StartTime)

	// Update DAG node status based on completion result
	var newlyReady []string
	if success {
		log.Printf("[Worker] Completed: %s (duration: %v)", worker.ID, duration)
		worker.Status = "completed"

		// Clear retry state on success
		o.clearRetry(worker.ID)

		// Mark as completed in DAG - this reduces InDegree of dependents
		o.dagMu.Lock()
		ready, err := o.dag.MarkCompleted(worker.ID)
		o.dagMu.Unlock()
		if err != nil {
			o.logError("DAG", "MarkCompleted", worker.ID, err, "warn")
		} else {
			newlyReady = ready
			if len(ready) > 0 {
				log.Printf("[DAG] %d dependents now ready after %s completion", len(ready), worker.ID)
			}
		}
	} else {
		// Log worker failure with structured error
		workerErr := fmt.Errorf("worker exited with code %d", exitCode)
		o.recordRetry(worker.ID, workerErr)

		retryCount := o.getRetryCount(worker.ID)
		maxRetries := o.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}

		if retryCount >= maxRetries {
			// Exhausted retries, log as error
			log.Printf("[Worker] Failed permanently: %s (exit code: %d, duration: %v, retries: %d/%d)",
				worker.ID, exitCode, duration, retryCount, maxRetries)
			worker.Status = "failed"
		} else {
			// Will retry, log as warning
			backoff := calculateBackoff(retryCount, o.baseDelay, o.maxDelay)
			log.Printf("[Worker] Failed but will retry: %s (exit code: %d, duration: %v, retry %d/%d, backoff: %v)",
				worker.ID, exitCode, duration, retryCount, maxRetries, backoff)
			worker.Status = "retry_pending"
		}

		// Mark as failed in DAG - dependents remain blocked
		o.dagMu.Lock()
		if err := o.dag.MarkFailed(worker.ID); err != nil {
			o.logError("DAG", "MarkFailed", worker.ID, err, "warn")
		}
		o.dagMu.Unlock()
	}

	// Remove from active workers
	o.workerMutex.Lock()
	delete(o.activeWorkers, worker.ID)
	o.workerMutex.Unlock()

	// Close bead via beadsClient
	if err := o.closeWorkerBead(worker, success); err != nil {
		o.logError("beadsClient", "CloseBead", worker.ID, err, "warn")
	}

	// If successful and new beads are ready, trigger immediate scheduling
	if success && len(newlyReady) > 0 {
		log.Printf("[Scheduler] Triggering immediate scheduling for %d newly ready beads", len(newlyReady))
		go o.triggerImmediateSchedule()
	}

	// If failed but retries remain, trigger scheduling for retry candidates
	if !success && o.getRetryCount(worker.ID) < o.config.Overseer.DefaultRetries {
		log.Printf("[Scheduler] Triggering scheduling to attempt retry for %s", worker.ID)
		go o.triggerImmediateSchedule()
	}

	// TODO: Send completion message via MCP
	// TODO: Record results to cass_memory
}

// closeWorkerBead closes the bead via beadsClient based on worker completion
func (o *Overseer) closeWorkerBead(worker *types.Worker, success bool) error {
	// Extract project and repo from BeadID
	projectName := worker.BeadID.Project()
	repoName := worker.BeadID.Repo()

	// Find project to get the full path
	var projectPath string
	for _, p := range o.projects {
		if p.Name == projectName {
			projectPath = p.Path
			break
		}
	}

	if projectPath == "" {
		return fmt.Errorf("project %s not found", projectName)
	}

	// Determine close reason based on success
	reason := "completed"
	if !success {
		reason = "failed"
	}

	return o.beadsClient.CloseBead(projectPath, repoName, worker.BeadID, reason)
}

// triggerImmediateSchedule spawns workers for ready beads without waiting for next poll
func (o *Overseer) triggerImmediateSchedule() {
	// Check if overseer is paused
	if o.IsPaused() {
		log.Println("⏸️  Immediate scheduling skipped: overseer is paused")
		return
	}

	// Get available worker slots
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	maxWorkers := o.GetMaxWorkers()
	availableSlots := maxWorkers - activeCount
	if availableSlots <= 0 {
		log.Printf("📊 Immediate scheduling skipped: no available slots (%d/%d workers active)", activeCount, maxWorkers)
		return
	}

	// Get ready beads from DAG
	o.dagMu.RLock()
	readyNodes := o.dag.GetReadyNodes()
	o.dagMu.RUnlock()

	if len(readyNodes) == 0 {
		return
	}

	log.Printf("📊 Immediate schedule: %d/%d workers active, %d ready beads in DAG", activeCount, maxWorkers, len(readyNodes))

	// Convert nodes to beads and filter out already running ones
	spawned := 0
	for _, node := range readyNodes {
		if availableSlots <= 0 {
			break
		}

		beadID := string(node.Bead.ID)

		// Skip if already running
		if o.isBeadRunning(beadID) {
			continue
		}

		// Skip approval gates (require manual br close)
		if node.Bead.Type == "approval-gate" {
			log.Printf("⏸️  Skipping approval gate: %s", beadID)
			continue
		}

		// Spawn worker for this bead
		if err := o.spawnWorker(node.Bead); err != nil {
			log.Printf("❌ Failed to spawn worker for bead %s: %v", beadID, err)
			continue
		}

		spawned++
		availableSlots--
	}

	if spawned > 0 {
		log.Printf("🚀 Immediate scheduling complete: spawned %d workers", spawned)
	}
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

// DAGStats represents statistics about the DAG
type DAGStats struct {
	TotalNodes     int `json:"total_nodes"`
	ReadyNodes     int `json:"ready_nodes"`
	BlockedNodes   int `json:"blocked_nodes"`
	CompletedNodes int `json:"completed_nodes"`
	FailedNodes    int `json:"failed_nodes"`
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
