// Package services provides batch memory retention services for Hydra.
// It implements asynchronous batching of execution memories to reduce
// concurrent load on the Hindsight memory system.
package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MemoryBatchSize is the target number of memories per batch (10-50 range)
const (
	MemoryBatchSize    = 30
	BatchFlushInterval = 30 * time.Second
	MaxQueueSize       = 1000
)

// ExecutionResult represents the result of a worker execution
type ExecutionResult struct {
	BeadID    types.BeadID
	Success   bool
	ExitCode  int
	Output    string
	Error     string
	Duration  time.Duration
	Attempts  int
	AgentType string
	Timestamp time.Time
}

// MemoryQueueItem represents an item waiting to be retained
type MemoryQueueItem struct {
	Memory    *hindsight.ExecutionMemory
	Submitted time.Time
}

// RetentionClient defines the interface for retaining memories
type RetentionClient interface {
	Retain(memory *hindsight.ExecutionMemory) (*hindsight.RetentionResult, error)
	IsEnabled() bool
}

// BatchRetentionService manages asynchronous batch retention of execution memories.
// It collects memories from workers and flushes them to Hindsight in batches
// to reduce concurrent request load.
type BatchRetentionService struct {
	client      RetentionClient
	queue       chan MemoryQueueItem
	batch       []*hindsight.ExecutionMemory
	batchMu     sync.Mutex
	flushTicker *time.Ticker
	stopChan    chan struct{}
	wg          sync.WaitGroup
	stats       RetentionStats
	statsMu     sync.RWMutex
}

// RetentionStats tracks retention performance metrics
type RetentionStats struct {
	TotalSubmitted int64
	TotalRetained  int64
	TotalFailed    int64
	BatchesSent    int64
	AvgBatchSize   float64
	LastFlushTime  time.Time
	QueueDepth     int
}

// NewBatchRetentionService creates a new batch retention service.
// The service starts a background goroutine that periodically flushes batches.
func NewBatchRetentionService(client RetentionClient) *BatchRetentionService {
	s := &BatchRetentionService{
		client:      client,
		queue:       make(chan MemoryQueueItem, MaxQueueSize),
		batch:       make([]*hindsight.ExecutionMemory, 0, MemoryBatchSize),
		flushTicker: time.NewTicker(BatchFlushInterval),
		stopChan:    make(chan struct{}),
	}

	// Start background batch processor
	s.wg.Add(1)
	go s.processBatches()

	return s
}

// SubmitMemory submits an execution memory for batch retention.
// This method is non-blocking and returns immediately.
// If the queue is full, the memory is dropped and an error is logged.
func (s *BatchRetentionService) SubmitMemory(memory *hindsight.ExecutionMemory) error {
	if memory == nil {
		return fmt.Errorf("cannot submit nil memory")
	}

	item := MemoryQueueItem{
		Memory:    memory,
		Submitted: time.Now(),
	}

	select {
	case s.queue <- item:
		s.statsMu.Lock()
		s.stats.TotalSubmitted++
		s.stats.QueueDepth = len(s.queue)
		s.statsMu.Unlock()
		return nil
	default:
		// Queue is full, drop the memory
		s.statsMu.Lock()
		s.stats.TotalFailed++
		s.statsMu.Unlock()
		return fmt.Errorf("retention queue full (max %d), memory dropped", MaxQueueSize)
	}
}

// SubmitExecutionResult submits an execution result to be converted to a memory and retained.
// This is a convenience method that wraps the result in an ExecutionMemory.
func (s *BatchRetentionService) SubmitExecutionResult(
	bead types.Bead,
	result ExecutionResult,
	workerID string,
) error {
	memory := hindsight.NewExecutionMemory(bead, workerID).
		WithAgentType(result.AgentType).
		WithOutcome(
			result.Success,
			result.ExitCode,
			result.Output,
			result.Error,
			result.Duration,
			result.Attempts,
		)

	return s.SubmitMemory(memory)
}

// processBatches is the background goroutine that processes the queue and flushes batches.
func (s *BatchRetentionService) processBatches() {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopChan:
			// Flush remaining batch before stopping
			s.flushBatch()
			return

		case <-s.flushTicker.C:
			// Periodic flush
			s.flushBatch()

		case item := <-s.queue:
			// Add to batch
			s.batchMu.Lock()
			s.batch = append(s.batch, item.Memory)
			shouldFlush := len(s.batch) >= MemoryBatchSize
			s.batchMu.Unlock()

			// Flush if batch is full
			if shouldFlush {
				s.flushBatch()
			}
		}
	}
}

// flushBatch sends the current batch to Hindsight.
// This method is thread-safe and can be called concurrently.
func (s *BatchRetentionService) flushBatch() {
	s.batchMu.Lock()
	if len(s.batch) == 0 {
		s.batchMu.Unlock()
		return
	}

	// Copy batch and reset
	batch := make([]*hindsight.ExecutionMemory, len(s.batch))
	copy(batch, s.batch)
	s.batch = s.batch[:0]
	s.batchMu.Unlock()

	// Send batch to Hindsight
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := s.retainBatch(ctx, batch)
	if err != nil {
		log.Printf("[BatchRetention] Failed to retain batch of %d memories: %v", len(batch), err)
		s.statsMu.Lock()
		s.stats.TotalFailed += int64(len(batch))
		s.statsMu.Unlock()
	} else {
		log.Printf("[BatchRetention] Successfully retained batch of %d memories", len(batch))
		s.statsMu.Lock()
		s.stats.TotalRetained += int64(len(batch))
		s.stats.BatchesSent++
		s.stats.LastFlushTime = time.Now()
		// Update average batch size
		total := s.stats.BatchesSent
		if total == 1 {
			s.stats.AvgBatchSize = float64(len(batch))
		} else {
			s.stats.AvgBatchSize = (s.stats.AvgBatchSize*float64(total-1) + float64(len(batch))) / float64(total)
		}
		s.statsMu.Unlock()
	}
}

// retainBatch sends a batch of memories to Hindsight.
func (s *BatchRetentionService) retainBatch(ctx context.Context, memories []*hindsight.ExecutionMemory) error {
	if len(memories) == 0 {
		return nil
	}

	// For now, retain each memory individually
	// TODO: Update hindsight client to support batch retain with multiple items
	for _, memory := range memories {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, err := s.client.Retain(memory)
		if err != nil {
			log.Printf("[BatchRetention] Failed to retain memory for bead %s: %v", memory.BeadID, err)
			// Continue with others even if one fails
		}
	}

	return nil
}

// Stop gracefully shuts down the batch retention service.
// It flushes any pending memories before stopping.
func (s *BatchRetentionService) Stop() {
	log.Println("[BatchRetention] Stopping service...")

	// Signal stop
	close(s.stopChan)

	// Stop ticker
	s.flushTicker.Stop()

	// Wait for processor to finish
	s.wg.Wait()

	log.Println("[BatchRetention] Service stopped")
}

// GetStats returns current retention statistics.
func (s *BatchRetentionService) GetStats() RetentionStats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()

	// Copy stats
	stats := s.stats
	stats.QueueDepth = len(s.queue)
	return stats
}

// ForceFlush immediately flushes the current batch.
func (s *BatchRetentionService) ForceFlush() {
	s.flushBatch()
}

// IsHealthy returns true if the service is operating normally.
func (s *BatchRetentionService) IsHealthy() bool {
	s.statsMu.RLock()
	queueDepth := s.stats.QueueDepth
	s.statsMu.RUnlock()

	// Consider unhealthy if queue is more than 80% full
	return queueDepth < int(float64(MaxQueueSize)*0.8)
}
