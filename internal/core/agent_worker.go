package core

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/internal/agents"
	"github.com/haulmatic-dev/hydra/internal/pi"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// spawnWorkerViaAgent creates a worker using the agent connector
// This replaces the old binary-spawning approach with proper agent session management
func (o *Overseer) spawnWorkerViaAgent(bead types.Bead) (*types.Worker, error) {
	beadIDStr := bead.ID.String()

	// Determine agent type
	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) == 0 {
		err := fmt.Errorf("no agent type found for bead type: %s", bead.Type)
		o.errorLogger.Error("spawnWorkerViaAgent", "agent_type", beadIDStr, err, "error")
		return nil, err
	}

	agentType := agentTypes[0]

	retryCount := o.retryManager.GetRetryCount(beadIDStr)
	if retryCount > 0 {
		log.Printf("[AgentWorker] Spawning retry worker for bead: %s (attempt %d, agent: %s)", beadIDStr, retryCount+1, agentType)
	} else {
		log.Printf("[AgentWorker] Spawning worker for bead: %s (agent: %s)", beadIDStr, agentType)
	}

	// Find project path
	projectPath := o.findProjectPath(&bead)
	if projectPath == "" {
		projectPath = bead.Project
	}

	// Mark bead as in_progress
	if o.beadsClient != nil && projectPath != "" {
		log.Printf("[Overseer] Marking bead %s as in_progress", beadIDStr)
		if err := o.beadsClient.UpdateBeadStatus(projectPath, bead.ID, types.StatusInProgress); err != nil {
			log.Printf("[Overseer] Warning: failed to mark bead %s as in_progress: %v", beadIDStr, err)
		}
	}

	// Create worker record
	worker := &types.Worker{
		ID:          beadIDStr,
		BeadID:      bead.ID,
		AgentType:   agentType,
		StartTime:   time.Now(),
		Status:      "starting",
		ProjectPath: projectPath,
	}

	// Start agent session
	o.agentConnMu.RLock()
	connector := o.agentConnector
	o.agentConnMu.RUnlock()

	if connector == nil {
		return nil, fmt.Errorf("agent connector not initialized")
	}

	// Start session
	opts := agents.AgentOptions{
		ProjectPath: projectPath,
		Timeout:     o.config.Overseer.WorkerTimeout,
		AgentType:   string(agentType),
		// Note: Patterns/AntiPatterns now accessed via cass-memory-insights skill on-demand
	}

	ctx := context.Background()
	session, err := connector.Start(ctx, bead, opts)
	if err != nil {
		o.errorLogger.Error("spawnWorkerViaAgent", "start_session", beadIDStr, err, "error")
		return nil, err
	}

	// Note: worker.ID must remain as beadIDStr to match the placeholder in activeWorkers
	// Session ID is available via the session parameter in monitorAgentSession

	worker.Status = "running"

	// Register with worker tracker for orphan/stuck worker detection by Charon
	// The PID will be populated by the connector after the process starts
	if o.workerTracker != nil {
		// Register with placeholder PID (0) - will be updated when process starts
		if err := o.workerTracker.TrackWorker(0, beadIDStr, projectPath); err != nil {
			log.Printf("[AgentWorker] Warning: failed to track worker in tracker: %v", err)
		}
	}

	// Monitor in goroutine
	go o.monitorAgentSession(worker, connector, session, bead)

	return worker, nil
}

// monitorAgentSession monitors an agent session until completion
func (o *Overseer) monitorAgentSession(worker *types.Worker, connector agents.AgentConnector, session *agents.AgentSession, bead types.Bead) {
	ctx := context.Background()
	startTime := time.Now()

	// Build prompt from bead
	prompt := o.buildAgentPrompt(bead)

	// Update worker tracker with actual PID and PGID now that session is started
	if session.PID > 0 && o.workerTracker != nil {
		pgid := session.PGID
		if pgid == 0 {
			pgid = session.PID // Fallback to PID if PGID not set
		}
		if err := o.workerTracker.UpdateWorkerPIDWithPGID(0, session.PID, pgid, string(worker.BeadID)); err != nil {
			log.Printf("[AgentWorker] Warning: Failed to update worker PID from 0 to %d: %v", session.PID, err)
		} else {
			log.Printf("[AgentWorker] Updated worker PID to %d (PGID %d) for bead %s", session.PID, pgid, worker.BeadID)
		}
	} else if session.PID == 0 {
		log.Printf("[AgentWorker] Warning: Session %s has PID 0, will be tracked as placeholder", session.ID)
	}

	// Create a context with timeout for the execution
	workerTimeout := o.config.Overseer.WorkerTimeout
	if workerTimeout == 0 {
		workerTimeout = 10 * time.Minute // Default 10 minute timeout
	}
	execCtx, cancel := context.WithTimeout(ctx, workerTimeout)
	defer cancel()

	// Start timeout monitor goroutine
	go func() {
		select {
		case <-execCtx.Done():
			if execCtx.Err() == context.DeadlineExceeded {
				log.Printf("[AgentWorker] Session %s exceeded timeout (%v), aborting", session.ID, workerTimeout)
				connector.Abort(ctx, session)
			}
		}
	}()

	// Execute
	result, err := connector.Execute(execCtx, session, prompt)

	duration := time.Since(startTime)

	var success bool
	var exitCode int

	if err != nil {
		log.Printf("[AgentWorker] Session %s failed: %v", session.ID, err)
		success = false
		exitCode = 1
	} else {
		success = result.Success
		exitCode = result.ExitCode
		log.Printf("[AgentWorker] Session %s completed: success=%v, exitCode=%d, duration=%v",
			session.ID, success, exitCode, duration)
	}

	// Handle completion
	if success {
		o.handleWorkerSuccess(worker, duration)
	} else {
		o.handleWorkerFailure(worker, exitCode, duration)
	}

	// Cleanup
	o.removeActiveWorker(worker.ID)
	connector.Cleanup(ctx, session)

	// Close bead
	o.closeBead(worker, success)

	// Record completion
	if o.settledStateSvc != nil {
		o.settledStateSvc.TrackBead(worker.BeadID)
	}
}

// buildAgentPrompt constructs the prompt for the agent using template loader
func (o *Overseer) buildAgentPrompt(bead types.Bead) string {
	// Determine config directory - use config or default to ./pi-config
	configDir := "./pi-config"
	if o.config != nil && o.config.Pi.ConfigDir != "" {
		configDir = o.config.Pi.ConfigDir
	}

	// Create prompt loader
	loader := pi.NewPromptLoader(configDir)

	// Build template data
	data := pi.TemplateData{
		Title:       bead.Title,
		Description: bead.Description,
		Type:        bead.Type,
		Project:     bead.Project,
		BeadID:      bead.ID.String(),
		// Add available skills (this could be enhanced to load actual skills)
		Skills: []pi.SkillInfo{
			{Name: "code-search-and-intelligence", Description: "Query institutional knowledge (cass_memory) and search code semantically (TLDR)"},
			{Name: "golang-pro", Description: "Go development patterns and best practices"},
			{Name: "golang-testing", Description: "Go testing patterns and strategies"},
			{Name: "test-driven-development", Description: "TDD methodology and practices"},
			{Name: "systematic-debugging", Description: "Scientific debugging methodology"},
		},
	}

	// Try to load and render the Pi agent template
	prompt, err := loader.GetPiAgentPrompt(data)
	if err != nil {
		log.Printf("[AgentWorker] Warning: Failed to load prompt template: %v. Using default.", err)
		// Fall back to default hardcoded prompt
		return pi.DefaultPiPrompt(data)
	}

	return prompt
}
