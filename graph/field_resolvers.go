package graph

// This file contains field resolvers for GraphQL types.
// Field resolvers handle lazy loading of related data and complex field computations.

import (
	"context"
	"fmt"

	"github.com/haulmatic-dev/hydra/graph/model"
)

// BeadResolver resolves fields for the Bead type.
// These field resolvers provide lazy loading of related data.
type BeadResolver interface {
	Dependencies(ctx context.Context, obj *model.Bead) ([]*model.Bead, error)
	Dependents(ctx context.Context, obj *model.Bead) ([]*model.Bead, error)
	Worker(ctx context.Context, obj *model.Bead) (*model.Worker, error)
	Patterns(ctx context.Context, obj *model.Bead) ([]*model.Pattern, error)
	Impact(ctx context.Context, obj *model.Bead) (*model.ImpactAnalysis, error)
}

// WorkerResolver resolves fields for the Worker type.
// These field resolvers provide lazy loading of related data.
type WorkerResolver interface {
	Bead(ctx context.Context, obj *model.Worker) (*model.Bead, error)
	Logs(ctx context.Context, obj *model.Worker) ([]*model.LogEntry, error)
}

// beadResolver handles field resolution for the Bead type.
// It provides lazy loading of dependencies, dependents, workers, patterns, and impact analysis.
type beadResolver struct {
	*Resolver
}

// Dependencies resolves the dependencies field for a Bead.
// It fetches dependencies from the overseer's DAG.
func (r *beadResolver) Dependencies(ctx context.Context, obj *model.Bead) ([]*model.Bead, error) {
	if r.Resolver.Overseer == nil {
		return []*model.Bead{}, nil
	}

	// Get dependency IDs from DAG via overseer
	depIDs, err := r.Resolver.Overseer.GetBeadDependencies(obj.ID)
	if err != nil {
		// Return empty slice if node not found or error occurs
		return []*model.Bead{}, nil
	}

	// Convert dependency IDs to Bead models
	dependencies := make([]*model.Bead, 0, len(depIDs))
	for _, depID := range depIDs {
		// Try to get the bead from overseer
		bead, err := r.Resolver.Overseer.GetBeadFromDAG(depID)
		if err != nil {
			continue
		}

		// Convert internal Bead type to GraphQL model using existing converter
		beadModel := convertTypesBeadToModel(&bead)
		dependencies = append(dependencies, beadModel)
	}

	return dependencies, nil
}

// Dependents resolves the dependents field for a Bead.
// It fetches beads that depend on this bead from the overseer's DAG.
func (r *beadResolver) Dependents(ctx context.Context, obj *model.Bead) ([]*model.Bead, error) {
	if r.Resolver.Overseer == nil {
		return []*model.Bead{}, nil
	}

	// Get dependent IDs from DAG via overseer
	depIDs, err := r.Resolver.Overseer.GetBeadDependents(obj.ID)
	if err != nil {
		// Return empty slice if node not found or error occurs
		return []*model.Bead{}, nil
	}

	// Convert dependent IDs to Bead models
	dependents := make([]*model.Bead, 0, len(depIDs))
	for _, depID := range depIDs {
		// Try to get the bead from overseer
		bead, err := r.Resolver.Overseer.GetBeadFromDAG(depID)
		if err != nil {
			continue
		}

		// Convert internal Bead type to GraphQL model using existing converter
		beadModel := convertTypesBeadToModel(&bead)
		dependents = append(dependents, beadModel)
	}

	return dependents, nil
}

// Worker resolves the worker field for a Bead.
// It finds the active worker currently processing this bead, if any.
func (r *beadResolver) Worker(ctx context.Context, obj *model.Bead) (*model.Worker, error) {
	if r.Resolver.Overseer == nil {
		return nil, nil
	}

	// Get active workers from overseer
	activeWorkers := r.Resolver.Overseer.GetActiveWorkers()

	// Find worker for this bead
	for _, worker := range activeWorkers {
		if worker.ID == obj.ID {
			return convertTypesWorkerToModel(&worker), nil
		}
	}

	// No active worker for this bead
	return nil, nil
}

// Patterns resolves the patterns field for a Bead.
// Returns empty patterns as cass_memory has been replaced by Hindsight.
func (r *beadResolver) Patterns(ctx context.Context, obj *model.Bead) ([]*model.Pattern, error) {
	// Cass_memory has been removed in favor of Hindsight
	// Return empty slice for backward compatibility
	return []*model.Pattern{}, nil
}

// Impact resolves the impact field for a Bead.
// It runs TLDR impact analysis on files mentioned in the bead, with caching.
func (r *beadResolver) Impact(ctx context.Context, obj *model.Bead) (*model.ImpactAnalysis, error) {
	// Impact analysis is unwired. It was backed by llm-tldr, which has been
	// removed (unmaintained; zero semantic units on Go; its service was not
	// running in production, so this field already returned nil there).
	//
	// The schema field is kept and nullable, so no API contract changes. Wiring
	// it to `graphify affected` is a follow-up: that is a real reverse-traversal
	// query, not a rename, and deserves its own tests.
	return nil, nil
}

// workerResolver handles field resolution for the Worker type.
type workerResolver struct {
	*Resolver
}

// Bead resolves the bead field for a Worker.
// It returns the bead that this worker is processing.
func (r *workerResolver) Bead(ctx context.Context, obj *model.Worker) (*model.Bead, error) {
	if r.Resolver.Overseer == nil {
		return nil, fmt.Errorf("overseer not available")
	}

	// Get the bead ID from the worker
	beadID := obj.ID

	// Try to get the bead from overseer's DAG
	bead, err := r.Resolver.Overseer.GetBeadFromDAG(beadID)
	if err != nil {
		// Return nil if bead not found in DAG
		return nil, nil
	}

	return convertTypesBeadToModel(&bead), nil
}

// Logs resolves the logs field for a Worker.
// It returns recent log entries for the worker (in-memory only for now).
func (r *workerResolver) Logs(ctx context.Context, obj *model.Worker) ([]*model.LogEntry, error) {
	// For now, return empty logs since we don't have a log storage mechanism yet
	// TODO: Implement log storage and retrieval when available
	return []*model.LogEntry{}, nil
}

// extractTargetFiles extracts file paths from a bead model.
func extractTargetFiles(obj *model.Bead) []string {
	files := make([]string, 0)
	seen := make(map[string]bool)

	// Extract from metadata if available
	// Note: The GraphQL model doesn't have metadata field, so we check description
	if obj.Description != nil {
		// Simple extraction - look for file-like patterns in description
		words := splitWords(*obj.Description)
		for _, word := range words {
			if looksLikeFile(word) && !seen[word] {
				files = append(files, word)
				seen[word] = true
			}
		}
	}

	return files
}

// splitWords splits a string into words.
func splitWords(s string) []string {
	words := make([]string, 0)
	current := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == ',' || r == ';' {
			if current != "" {
				words = append(words, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

// looksLikeFile checks if a string looks like a file path.
func looksLikeFile(s string) bool {
	// Simple heuristic: contains / or has a common extension
	if len(s) < 2 {
		return false
	}

	// Check for path separator
	if containsAny(s, "/\\") {
		return true
	}

	// Check for common file extensions
	extensions := []string{".go", ".js", ".ts", ".py", ".java", ".rb", ".rs", ".c", ".cpp", ".h", ".hpp", ".md", ".json", ".yaml", ".yml", ".toml"}
	for _, ext := range extensions {
		if len(s) > len(ext) && s[len(s)-len(ext):] == ext {
			return true
		}
	}

	return false
}

// containsAny checks if a string contains any of the given characters.
func containsAny(s string, chars string) bool {
	for _, c := range chars {
		for _, r := range s {
			if r == c {
				return true
			}
		}
	}
	return false
}
