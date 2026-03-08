package core

import (
	"context"
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
	beadsClient   *beads.Client
	dag           *DAG
	dagMu         sync.RWMutex
	completed     map[string]bool // Track completed beads for this session
}

// NewOverseer creates a new overseer instance
func NewOverseer(config *Config) *Overseer {
	return &Overseer{
		config:        config,
		activeWorkers: make(map[string]*types.Worker),
		pollInterval:  config.Overseer.PollInterval,
		maxWorkers:    config.Overseer.MaxWorkers,
		beadsClient:   beads.NewClient(),
		dag:           NewDAG(),
		completed:     make(map[string]bool),
	}
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
		log.Printf("⚠️  Failed to build DAG: %v", err)
		// Continue with existing DAG if rebuild fails
	}

	// Get ready beads from DAG (InDegree == 0)
	readyBeads, err := o.getReadyBeadsFromDAG()
	if err != nil {
		return fmt.Errorf("failed to get ready beads from DAG: %w", err)
	}

	if len(readyBeads) == 0 {
		return nil
	}

	log.Printf("📊 Active workers: %d/%d, Ready beads from DAG: %d", activeCount, maxWorkers, len(readyBeads))

	// Spawn workers for ready beads
	for _, bead := range readyBeads {
		if availableSlots <= 0 {
			break
		}

		// Skip approval gates (require manual br close)
		if bead.Type == "approval-gate" {
			log.Printf("⏸️  Skipping approval gate: %s", bead.ID)
			continue
		}

		if err := o.spawnWorker(bead); err != nil {
			log.Printf("❌ Failed to spawn worker for bead %s: %v", bead.ID, err)
			continue
		}

		availableSlots--
	}

	return nil
}

// buildDAG fetches all beads from all projects and builds/refreshs the DAG
func (o *Overseer) buildDAG() error {
	o.dagMu.Lock()
	defer o.dagMu.Unlock()

	// Clear existing DAG
	o.dag.Clear()

	var allBeads []types.Bead

	// Fetch all beads from all projects
	for _, project := range o.projects {
		if project.Type == types.RepoTypeSingle {
			beads, err := o.beadsClient.ListBeads(project.Path, "")
			if err != nil {
				log.Printf("⚠️  Could not list beads for project %s: %v", project.Name, err)
				continue
			}
			// Fetch full details for each bead to get dependencies
			for _, bead := range beads {
				fullBead, err := o.beadsClient.GetBead(project.Path, "", bead.ID)
				if err != nil {
					log.Printf("⚠️  Could not get full details for bead %s: %v", bead.ID, err)
					// Use the bead from list (may not have dependencies)
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
					log.Printf("⚠️  Could not list beads for repo %s/%s: %v", project.Name, repo.Name, err)
					continue
				}
				// Fetch full details for each bead to get dependencies
				for _, bead := range beads {
					fullBead, err := o.beadsClient.GetBead(project.Path, repo.Name, bead.ID)
					if err != nil {
						log.Printf("⚠️  Could not get full details for bead %s: %v", bead.ID, err)
						// Use the bead from list (may not have dependencies)
						allBeads = append(allBeads, bead)
					} else {
						allBeads = append(allBeads, *fullBead)
					}
				}
			}
		}
	}

	log.Printf("🔄 Building DAG with %d beads", len(allBeads))

	// Add all beads as nodes to the DAG
	for _, bead := range allBeads {
		if err := o.dag.AddNode(bead); err != nil {
			if err == ErrNodeAlreadyExists {
				log.Printf("⚠️  Duplicate bead in DAG: %s", bead.ID)
				continue
			}
			log.Printf("⚠️  Failed to add bead %s to DAG: %v", bead.ID, err)
		}
	}

	// Add edges for dependencies
	for _, bead := range allBeads {
		beadID := string(bead.ID)
		for _, depID := range bead.Dependencies {
			depIDStr := string(depID)
			if err := o.dag.AddEdge(beadID, depIDStr); err != nil {
				switch err {
				case ErrNodeNotFound:
					log.Printf("⚠️  Dependency %s not found for bead %s (may be in different project)", depIDStr, beadID)
				case ErrSelfReference:
					log.Printf("⚠️  Bead %s has self-reference dependency", beadID)
				case ErrEdgeCreatesCycle:
					log.Printf("❌ Adding dependency %s -> %s would create a cycle", depIDStr, beadID)
				default:
					log.Printf("⚠️  Failed to add edge %s -> %s: %v", depIDStr, beadID, err)
				}
				// Continue even if edge creation fails
			}
		}
	}

	// Detect and log cycles
	if cycle, err := o.dag.DetectCycle(); err != nil {
		log.Printf("❌ Error detecting cycles in DAG: %v", err)
	} else if cycle != nil {
		log.Printf("❌ Cycle detected in DAG: %v", cycle)
	}

	log.Printf("✅ DAG built: %d nodes, %d blocked", o.dag.GetNodeCount(), o.dag.GetBlockedCount())

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
		return fmt.Errorf("worker already exists for bead %s", beadIDStr)
	}
	o.workerMutex.RUnlock()

	// Determine agent type
	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) == 0 {
		return fmt.Errorf("no agent type found for bead type: %s", bead.Type)
	}

	agentType := agentTypes[0] // Use primary agent

	log.Printf("🚀 Spawning worker for bead: %s (agent: %s)", beadIDStr, agentType)

	// Build worker command
	cmd := exec.Command("go", "run", "cmd/worker/main.go")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HYDRA_BEAD_ID=%s", beadIDStr),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
	)

	// Start process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
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
		log.Printf("✅ Worker completed: %s (duration: %v)", worker.ID, duration)
		worker.Status = "completed"

		// Mark as completed in DAG - this reduces InDegree of dependents
		o.dagMu.Lock()
		ready, err := o.dag.MarkCompleted(worker.ID)
		o.dagMu.Unlock()
		if err != nil {
			log.Printf("⚠️  Failed to mark bead %s as completed in DAG: %v", worker.ID, err)
		} else {
			newlyReady = ready
			if len(ready) > 0 {
				log.Printf("📥 DAG: %d dependents now ready for bead %s", len(ready), worker.ID)
			}
		}
	} else {
		log.Printf("❌ Worker failed: %s (exit code: %d, duration: %v)", worker.ID, exitCode, duration)
		worker.Status = "failed"

		// Mark as failed in DAG - dependents remain blocked
		o.dagMu.Lock()
		if err := o.dag.MarkFailed(worker.ID); err != nil {
			log.Printf("⚠️  Failed to mark bead %s as failed in DAG: %v", worker.ID, err)
		}
		o.dagMu.Unlock()
	}

	// Remove from active workers
	o.workerMutex.Lock()
	delete(o.activeWorkers, worker.ID)
	o.workerMutex.Unlock()

	// Close bead via beadsClient
	if err := o.closeWorkerBead(worker, success); err != nil {
		log.Printf("⚠️  Failed to close bead %s: %v", worker.ID, err)
	}

	// If successful and new beads are ready, trigger immediate scheduling
	if success && len(newlyReady) > 0 {
		log.Printf("🚀 Triggering immediate scheduling for %d newly ready beads", len(newlyReady))
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
