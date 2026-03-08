package graph

import (
	"time"

	"github.com/haulmatic-dev/hydra/graph/model"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ToGraphQLBead converts internal Bead to GraphQL model
func ToGraphQLBead(bead types.Bead) *model.Bead {
	var description *string
	if bead.Description != "" {
		description = &bead.Description
	}

	var repo *string
	if bead.Repo != "" {
		repo = &bead.Repo
	}

	var completedAt *time.Time
	if bead.CompletedAt != nil {
		completedAt = bead.CompletedAt
	}

	return &model.Bead{
		ID:          bead.ID.String(),
		Title:       bead.Title,
		Description: description,
		Type:        bead.Type,
		Status:      ToGraphQLBeadStatus(bead.Status),
		Priority:    int32(bead.Priority),
		Project:     bead.Project,
		Repo:        repo,
		Labels:      bead.Labels,
		CreatedAt:   bead.CreatedAt,
		UpdatedAt:   bead.UpdatedAt,
		CompletedAt: completedAt,
	}
}

// ToGraphQLBeadStatus converts internal BeadStatus to GraphQL BeadStatus
func ToGraphQLBeadStatus(status types.BeadStatus) model.BeadStatus {
	switch status {
	case types.StatusOpen:
		return model.BeadStatusOpen
	case types.StatusInProgress:
		return model.BeadStatusInProgress
	case types.StatusCompleted:
		return model.BeadStatusCompleted
	case types.StatusFailed:
		return model.BeadStatusFailed
	case types.StatusBlocked:
		return model.BeadStatusBlocked
	default:
		return model.BeadStatusOpen
	}
}

// ToGraphQLWorker converts internal Worker to GraphQL model
func ToGraphQLWorker(worker *types.Worker) *model.Worker {
	if worker == nil {
		return nil
	}

	return &model.Worker{
		ID:        worker.ID,
		AgentType: string(worker.AgentType),
		Status:    ToGraphQLWorkerStatus(worker.Status),
		Pid:       int32(worker.PID),
		StartTime: worker.StartTime,
		// RetryCount is not available in types.Worker yet - using 0 as default
		RetryCount: 0,
		// Logs would be populated separately if available
		Logs: []*model.LogEntry{},
	}
}

// ToGraphQLWorkerStatus converts internal worker status string to GraphQL WorkerStatus
func ToGraphQLWorkerStatus(status string) model.WorkerStatus {
	switch status {
	case "running":
		return model.WorkerStatusRunning
	case "completed":
		return model.WorkerStatusCompleted
	case "failed":
		return model.WorkerStatusFailed
	case "cancelled":
		return model.WorkerStatusCancelled
	case "retry_pending":
		// Retry pending is not a terminal state, map to pending
		return model.WorkerStatusPending
	default:
		return model.WorkerStatusPending
	}
}

// ToGraphQLDAGStats converts DAG stats to GraphQL model
func ToGraphQLDAGStats(dag *core.DAG, maxWorkers int) *model.DAGStats {
	if dag == nil {
		return &model.DAGStats{
			TotalNodes:     0,
			ReadyNodes:     0,
			BlockedNodes:   0,
			CompletedNodes: 0,
			FailedNodes:    0,
			ActiveWorkers:  0,
			MaxWorkers:     int32(maxWorkers),
		}
	}

	return &model.DAGStats{
		TotalNodes:     int32(dag.GetNodeCount()),
		ReadyNodes:     int32(len(dag.GetReadyNodes())),
		BlockedNodes:   int32(dag.GetBlockedCount()),
		CompletedNodes: int32(dag.GetCompletedCount()),
		FailedNodes:    int32(dag.GetFailedCount()),
		ActiveWorkers:  0, // Would be populated separately from overseer
		MaxWorkers:     int32(maxWorkers),
	}
}

// ToGraphQLSystemStats converts overseer stats to GraphQL model
func ToGraphQLSystemStats(overseer *core.Overseer) *model.SystemStats {
	if overseer == nil {
		return &model.SystemStats{
			Uptime:          0,
			TotalProjects:   0,
			OverseerRunning: false,
			Paused:          false,
		}
	}

	return &model.SystemStats{
		Uptime:          int32(time.Since(time.Now().Add(-time.Hour)).Seconds()), // Placeholder - real uptime tracking needed
		TotalProjects:   int32(len(overseer.GetProjects())),
		OverseerRunning: overseer.IsRunning(),
		Paused:          overseer.IsPaused(),
	}
}

// ToGraphQLConfig converts config to GraphQL model
func ToGraphQLConfig(config *core.Config) *model.Config {
	if config == nil {
		return &model.Config{
			MaxWorkers:    0,
			PollInterval:  0,
			RetryDelay:    0,
			WorkerTimeout: 0,
			APIPort:       0,
		}
	}

	return &model.Config{
		MaxWorkers:    int32(config.Overseer.MaxWorkers),
		PollInterval:  int32(config.Overseer.PollInterval.Seconds()),
		RetryDelay:    30, // Default retry delay in seconds - not directly in config
		WorkerTimeout: int32(config.Overseer.WorkerTimeout.Seconds()),
		APIPort:       int32(config.API.Port),
	}
}

// ToGraphQLPattern converts cass_memory bullet to GraphQL model
func ToGraphQLPattern(bullet intelligence.Bullet) *model.Pattern {
	score := bullet.Score
	return &model.Pattern{
		ID:       bullet.ID,
		Content:  bullet.Content,
		Category: bullet.Category,
		Score:    &score,
	}
}

// ToGraphQLPatterns converts a slice of cass_memory bullets to GraphQL models
func ToGraphQLPatterns(bullets []intelligence.Bullet) []*model.Pattern {
	patterns := make([]*model.Pattern, len(bullets))
	for i, bullet := range bullets {
		patterns[i] = ToGraphQLPattern(bullet)
	}
	return patterns
}

// ToGraphQLImpactAnalysis converts TLDR impact to GraphQL model
func ToGraphQLImpactAnalysis(impact *intelligence.TLDRImpactResponse) *model.ImpactAnalysis {
	if impact == nil {
		return &model.ImpactAnalysis{
			Files:    0,
			Tokens:   0,
			Exceeded: false,
			Callers:  []*model.Dependency{},
			Callees:  []*model.Dependency{},
		}
	}

	// Convert callers
	callers := make([]*model.Dependency, len(impact.Impact.Callers))
	for i, caller := range impact.Impact.Callers {
		callers[i] = ToGraphQLDependency(caller)
	}

	// Convert callees
	callees := make([]*model.Dependency, len(impact.Impact.Callees))
	for i, callee := range impact.Impact.Callees {
		callees[i] = ToGraphQLDependency(callee)
	}

	return &model.ImpactAnalysis{
		Files:    int32(impact.Scope.Files),
		Tokens:   int32(impact.Scope.Tokens),
		Exceeded: impact.Scope.Exceeded,
		Callers:  callers,
		Callees:  callees,
	}
}

// ToGraphQLDependency converts internal TLDR Dependency to GraphQL model
func ToGraphQLDependency(dep intelligence.Dependency) *model.Dependency {
	function := dep.Function
	line := int32(dep.Line)

	return &model.Dependency{
		File:     dep.File,
		Function: &function,
		Line:     &line,
	}
}

// ToGraphQLHealth returns health status
func ToGraphQLHealth() *model.Health {
	return &model.Health{
		Status:    "healthy",
		Version:   "1.0.0",
		Timestamp: time.Now(),
	}
}

// ToGraphQLBeads converts a slice of internal beads to GraphQL models
func ToGraphQLBeads(beads []types.Bead) []*model.Bead {
	result := make([]*model.Bead, len(beads))
	for i, bead := range beads {
		result[i] = ToGraphQLBead(bead)
	}
	return result
}

// ToGraphQLWorkers converts a slice of internal workers to GraphQL models
func ToGraphQLWorkers(workers []types.Worker) []*model.Worker {
	result := make([]*model.Worker, len(workers))
	for i := range workers {
		result[i] = ToGraphQLWorker(&workers[i])
	}
	return result
}

// ToGraphQLLogLevel converts a log level string to GraphQL LogLevel
func ToGraphQLLogLevel(level string) model.LogLevel {
	switch level {
	case "DEBUG":
		return model.LogLevelDebug
	case "INFO":
		return model.LogLevelInfo
	case "WARN", "WARNING":
		return model.LogLevelWarn
	case "ERROR":
		return model.LogLevelError
	default:
		return model.LogLevelInfo
	}
}

// ToGraphQLLogEntry converts internal log data to GraphQL model
func ToGraphQLLogEntry(timestamp time.Time, level, message, workerID string) *model.LogEntry {
	var workerIDPtr *string
	if workerID != "" {
		workerIDPtr = &workerID
	}

	return &model.LogEntry{
		Timestamp: timestamp,
		Level:     ToGraphQLLogLevel(level),
		Message:   message,
		WorkerID:  workerIDPtr,
	}
}
