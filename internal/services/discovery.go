// Package services provides continuous discovery services for Hydra.
// It enables asynchronous knowledge discovery that runs in parallel with
// bead execution without blocking the critical path.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Default configuration values for discovery operations.
const (
	// ConfidenceThreshold is the minimum confidence level required before
	// triggering automatic discovery.
	ConfidenceThreshold = 0.40

	// defaultDiscoveryTimeout is the timeout for discovery operations.
	defaultDiscoveryTimeout = 5 * time.Minute

	// defaultMinRelevance is the minimum relevance score for findings.
	defaultMinRelevance = 0.5
)

// DiscoveryResult represents the findings from a discovery operation.
type DiscoveryResult struct {
	BeadID      types.BeadID  `json:"bead_id"`
	Query       string        `json:"query"`
	Findings    []Finding     `json:"findings"`
	Confidence  float64       `json:"confidence"`
	CompletedAt time.Time     `json:"completed_at"`
	Duration    time.Duration `json:"duration"`
}

// Finding represents a single discovery finding.
type Finding struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	Relevance   float64 `json:"relevance"`
	Content     string  `json:"content,omitempty"`
}

// KnowledgeSource represents a source of knowledge for discovery.
type KnowledgeSource interface {
	// Query searches the knowledge source and returns relevant findings.
	Query(ctx context.Context, query string, minRelevance float64) ([]Finding, error)
	// IsAvailable returns true if the knowledge source is available.
	IsAvailable() bool
}

// Service provides continuous discovery capabilities.
type Service struct {
	hindsightClient     *hindsight.Client
	sources             []KnowledgeSource
	confidenceThreshold float64
	statusMap           map[types.BeadID]*DiscoveryStatus
	statusMutex         sync.RWMutex
}

// ServiceOption is a functional option for configuring the Service.
type ServiceOption func(*Service)

// WithHindsightClient sets the Hindsight client for storing findings.
func WithHindsightClient(client *hindsight.Client) ServiceOption {
	return func(s *Service) {
		s.hindsightClient = client
	}
}

// WithKnowledgeSources sets the knowledge sources for discovery.
func WithKnowledgeSources(sources ...KnowledgeSource) ServiceOption {
	return func(s *Service) {
		s.sources = sources
	}
}

// WithConfidenceThreshold sets the confidence threshold for triggering discovery.
func WithConfidenceThreshold(threshold float64) ServiceOption {
	return func(s *Service) {
		s.confidenceThreshold = threshold
	}
}

// NewService creates a new discovery service with the specified options.
func NewService(opts ...ServiceOption) *Service {
	s := &Service{
		sources:             make([]KnowledgeSource, 0),
		confidenceThreshold: ConfidenceThreshold,
		statusMap:           make(map[types.BeadID]*DiscoveryStatus),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ShouldTriggerDiscovery returns true if discovery should be triggered based on
// the current knowledge confidence level.
func (s *Service) ShouldTriggerDiscovery(confidence float64) bool {
	return confidence < s.confidenceThreshold
}

// TriggerDiscoveryAsync initiates asynchronous discovery for a bead.
// This method is non-blocking and returns immediately.
func (s *Service) TriggerDiscoveryAsync(ctx context.Context, bead types.Bead, query string) error {
	s.markDiscoveryRunning(bead.ID)

	go s.runDiscoveryAsync(bead, query)

	return nil
}

// runDiscoveryAsync performs discovery asynchronously and updates status.
func (s *Service) runDiscoveryAsync(bead types.Bead, query string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDiscoveryTimeout)
	defer cancel()

	result, err := s.RunDiscovery(ctx, bead, query)
	if err != nil {
		s.setDiscoveryStatus(&DiscoveryStatus{
			BeadID:    bead.ID,
			IsRunning: false,
			Completed: false,
			Error:     err.Error(),
		})
		return
	}

	if err := s.StoreFindings(ctx, result); err != nil {
		s.setDiscoveryStatus(&DiscoveryStatus{
			BeadID:     bead.ID,
			IsRunning:  false,
			Completed:  false,
			Confidence: result.Confidence,
			Error:      fmt.Sprintf("failed to store findings: %v", err),
		})
		return
	}

	s.setDiscoveryStatus(&DiscoveryStatus{
		BeadID:     bead.ID,
		IsRunning:  false,
		Completed:  true,
		Confidence: result.Confidence,
	})
}

// RunDiscovery performs synchronous discovery for a bead.
// This is primarily used for testing; production code should use TriggerDiscoveryAsync.
func (s *Service) RunDiscovery(ctx context.Context, bead types.Bead, query string) (*DiscoveryResult, error) {
	startTime := time.Now()
	result := &DiscoveryResult{
		BeadID: bead.ID,
		Query:  query,
	}

	findings := s.queryKnowledgeSources(ctx, query)
	result.Findings = findings
	result.Confidence = calculateConfidence(findings)
	result.CompletedAt = time.Now()
	result.Duration = time.Since(startTime)

	return result, nil
}

// queryKnowledgeSources queries all available sources and aggregates findings.
func (s *Service) queryKnowledgeSources(ctx context.Context, query string) []Finding {
	var allFindings []Finding

	for _, source := range s.sources {
		if !source.IsAvailable() {
			continue
		}

		findings, err := source.Query(ctx, query, defaultMinRelevance)
		if err != nil {
			// Continue with other sources on error
			continue
		}

		allFindings = append(allFindings, findings...)
	}

	return allFindings
}

// calculateConfidence computes the average relevance of findings.
func calculateConfidence(findings []Finding) float64 {
	if len(findings) == 0 {
		return 0.0
	}

	var totalRelevance float64
	for _, finding := range findings {
		totalRelevance += finding.Relevance
	}

	return totalRelevance / float64(len(findings))
}

// StoreFindings stores discovery results in Hindsight for future queries.
// The findings are stored with type "discovery" to distinguish them from
// regular execution memories.
func (s *Service) StoreFindings(ctx context.Context, result *DiscoveryResult) error {
	if s.hindsightClient == nil {
		return fmt.Errorf("hindsight client not configured")
	}

	if result == nil {
		return fmt.Errorf("discovery result cannot be nil")
	}

	// Convert findings to JSON for storage
	findingsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		return fmt.Errorf("failed to marshal findings: %w", err)
	}

	// Create execution memory with discovery type
	memory := &hindsight.ExecutionMemory{
		BeadID: result.BeadID,
		BeadMetadata: hindsight.BeadMetadata{
			Title:       fmt.Sprintf("Discovery: %s", result.Query),
			Description: "Automated knowledge discovery findings",
			Type:        "discovery", // Store with discovery type as required
			Labels:      []string{"discovery", "automated", "continuous-discovery"},
		},
		ExecutionResult: hindsight.ExecutionOutcome{
			Success:  true,
			Output:   string(findingsJSON),
			Duration: result.Duration,
		},
		RecordedAt: result.CompletedAt,
		WorkerID:   "discovery-service",
		Version:    hindsight.SchemaVersion,
	}

	// Retain the memory in Hindsight
	retentionResult, err := s.hindsightClient.Retain(memory)
	if err != nil {
		return fmt.Errorf("failed to retain discovery findings: %w", err)
	}

	if !retentionResult.Success {
		return fmt.Errorf("retention failed: %s", retentionResult.Error)
	}

	return nil
}

// GetDiscoveryStatus returns the current discovery status for a bead.
func (s *Service) GetDiscoveryStatus(beadID types.BeadID) *DiscoveryStatus {
	s.statusMutex.RLock()
	defer s.statusMutex.RUnlock()

	if status, ok := s.statusMap[beadID]; ok {
		// Return a copy to avoid external modification
		return &DiscoveryStatus{
			BeadID:     status.BeadID,
			IsRunning:  status.IsRunning,
			Completed:  status.Completed,
			Confidence: status.Confidence,
			Error:      status.Error,
		}
	}

	return nil
}

// setDiscoveryStatus updates the discovery status for a bead (thread-safe).
func (s *Service) setDiscoveryStatus(status *DiscoveryStatus) {
	s.statusMutex.Lock()
	defer s.statusMutex.Unlock()

	s.statusMap[status.BeadID] = status
}

// Status helper methods for cleaner async operation handling.

func (s *Service) markDiscoveryRunning(beadID types.BeadID) {
	s.setDiscoveryStatus(&DiscoveryStatus{
		BeadID:    beadID,
		IsRunning: true,
		Completed: false,
	})
}

func (s *Service) markDiscoveryFailed(beadID types.BeadID, err error) {
	s.setDiscoveryStatus(&DiscoveryStatus{
		BeadID:    beadID,
		IsRunning: false,
		Completed: false,
		Error:     err.Error(),
	})
}

func (s *Service) markDiscoveryStoreFailed(beadID types.BeadID, confidence float64, err error) {
	s.setDiscoveryStatus(&DiscoveryStatus{
		BeadID:     beadID,
		IsRunning:  false,
		Completed:  false,
		Confidence: confidence,
		Error:      fmt.Sprintf("failed to store findings: %v", err),
	})
}

func (s *Service) markDiscoveryCompleted(beadID types.BeadID, confidence float64) {
	s.setDiscoveryStatus(&DiscoveryStatus{
		BeadID:     beadID,
		IsRunning:  false,
		Completed:  true,
		Confidence: confidence,
	})
}

// DiscoveryStatus represents the status of a discovery operation.
type DiscoveryStatus struct {
	BeadID     types.BeadID `json:"bead_id"`
	IsRunning  bool         `json:"is_running"`
	Completed  bool         `json:"completed"`
	Confidence float64      `json:"confidence"`
	Error      string       `json:"error,omitempty"`
}

// HindsightKnowledgeSource implements KnowledgeSource using Hindsight memories.
type HindsightKnowledgeSource struct {
	client       *hindsight.Client
	minScore     float64
	defaultLimit int
}

// NewHindsightKnowledgeSource creates a new Hindsight-based knowledge source.
func NewHindsightKnowledgeSource(client *hindsight.Client, minScore float64, limit int) *HindsightKnowledgeSource {
	return &HindsightKnowledgeSource{
		client:       client,
		minScore:     minScore,
		defaultLimit: limit,
	}
}

// Query searches Hindsight for relevant memories and converts them to findings.
func (h *HindsightKnowledgeSource) Query(ctx context.Context, query string, minRelevance float64) ([]Finding, error) {
	if h.client == nil || !h.client.IsEnabled() {
		return nil, fmt.Errorf("hindsight client not available")
	}

	limit := h.defaultLimit
	if limit <= 0 {
		limit = 10
	}

	response, err := h.client.Search(query, "discovery", limit)
	if err != nil {
		return nil, fmt.Errorf("hindsight search failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("hindsight search unsuccessful: %s", response.Error)
	}

	var findings []Finding
	for _, memory := range response.Memories {
		// Filter by minimum relevance
		if memory.Relevance < minRelevance && memory.Score < minRelevance {
			continue
		}

		finding := Finding{
			ID:          memory.ID,
			Title:       memory.Title,
			Description: memory.Content,
			Source:      "hindsight",
			Relevance:   max(memory.Relevance, memory.Score),
			Content:     memory.Content,
		}
		findings = append(findings, finding)
	}

	return findings, nil
}

// IsAvailable returns true if the Hindsight client is available.
func (h *HindsightKnowledgeSource) IsAvailable() bool {
	return h.client != nil && h.client.IsEnabled()
}

// max returns the larger of two float64 values.
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
