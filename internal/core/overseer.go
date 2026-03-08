package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/buddhi/hydra/internal/beads"
	"github.com/buddhi/hydra/pkg/types"
)

// Overseer is the main scheduler daemon
type Overseer struct {
	config        *Config
	projects      []types.Project
	activeWorkers map[string]*types.Worker
	workerMutex   sync.RWMutex
	running       bool
	pollInterval  time.Duration
	maxWorkers    int
	beadsClient   *beads.Client
}

// NewOverseer creates a new overseer instance
func NewOverseer(config *Config) *Overseer {
	return &Overseer{
		config:        config,
		activeWorkers: make(map[string]*types.Worker),
		pollInterval:  config.Overseer.PollInterval,
		maxWorkers:    config.Overseer.MaxWorkers,
		beadsClient:   beads.NewClient(),
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
	// Get active worker count
	o.workerMutex.RLock()
	activeCount := len(o.activeWorkers)
	o.workerMutex.RUnlock()

	availableSlots := o.maxWorkers - activeCount
	if availableSlots <= 0 {
		return nil
	}

	// Get ready beads from all projects
	readyBeads, err := o.getReadyBeads()
	if err != nil {
		return fmt.Errorf("failed to get ready beads: %w", err)
	}

	if len(readyBeads) == 0 {
		return nil
	}

	log.Printf("📊 Active workers: %d/%d, Ready beads: %d", activeCount, o.maxWorkers, len(readyBeads))

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

	// Determine success
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

	if success {
		log.Printf("✅ Worker completed: %s (duration: %v)", worker.ID, duration)
		worker.Status = "completed"
	} else {
		log.Printf("❌ Worker failed: %s (exit code: %d, duration: %v)", worker.ID, exitCode, duration)
		worker.Status = "failed"
	}

	// Remove from active workers
	o.workerMutex.Lock()
	delete(o.activeWorkers, worker.ID)
	o.workerMutex.Unlock()

	// TODO: Send completion message via MCP
	// TODO: Record results to cass_memory
}
