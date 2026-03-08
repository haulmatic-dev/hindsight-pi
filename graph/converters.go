package graph

import (
	"github.com/haulmatic-dev/hydra/graph/model"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// convertTypesBeadToModel converts a types.Bead to a model.Bead
func convertTypesBeadToModel(bead *types.Bead) *model.Bead {
	if bead == nil {
		return nil
	}

	mBead := &model.Bead{
		ID:           string(bead.ID),
		Title:        bead.Title,
		Type:         bead.Type,
		Status:       model.BeadStatus(bead.Status),
		Priority:     int32(bead.Priority),
		Project:      bead.Project,
		Labels:       bead.Labels,
		CreatedAt:    bead.CreatedAt,
		UpdatedAt:    bead.UpdatedAt,
		Patterns:     []*model.Pattern{},
		Dependencies: []*model.Bead{},
		Dependents:   []*model.Bead{},
	}

	// Set optional fields
	if bead.Description != "" {
		desc := bead.Description
		mBead.Description = &desc
	}

	if bead.Repo != "" {
		mBead.Repo = &bead.Repo
	}

	if bead.CompletedAt != nil {
		mBead.CompletedAt = bead.CompletedAt
	}

	return mBead
}

// convertTypesBeadsToModel converts a slice of types.Bead to model.Bead pointers
func convertTypesBeadsToModel(beads []types.Bead) []*model.Bead {
	result := make([]*model.Bead, len(beads))
	for i := range beads {
		result[i] = convertTypesBeadToModel(&beads[i])
	}
	return result
}

// convertTypesWorkerToModel converts a types.Worker to a model.Worker
func convertTypesWorkerToModel(worker *types.Worker) *model.Worker {
	if worker == nil {
		return nil
	}

	return &model.Worker{
		ID:         worker.ID,
		AgentType:  string(worker.AgentType),
		Status:     model.WorkerStatus(worker.Status),
		Pid:        int32(worker.PID),
		StartTime:  worker.StartTime,
		RetryCount: 0, // Retry count is tracked separately in the overseer
		Logs:       []*model.LogEntry{},
	}
}

// convertTypesWorkersToModel converts a slice of types.Worker to model.Worker pointers
func convertTypesWorkersToModel(workers []types.Worker) []*model.Worker {
	result := make([]*model.Worker, len(workers))
	for i := range workers {
		result[i] = convertTypesWorkerToModel(&workers[i])
	}
	return result
}
