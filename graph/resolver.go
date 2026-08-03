package graph

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haulmatic-dev/hydra/graph/model"
	"github.com/haulmatic-dev/hydra/internal/core"
)

// Resolver is the root resolver for GraphQL
// It manages event broadcasting for real-time subscriptions

type Resolver struct {
	Overseer *core.Overseer

	// Event broadcasting system for subscriptions
	eventChannel chan SubscriptionEvent

	// Subscriber management
	subscribers   map[string]*EventSubscriber
	subscribersMu sync.RWMutex

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// SubscriptionEvent represents a generic event that can be broadcast to subscribers
type SubscriptionEvent struct {
	Type      model.UpdateType
	Worker    *model.Worker
	Bead      *model.Bead
	LogEntry  *model.LogEntry
	Timestamp time.Time
}

// EventSubscriber represents a subscriber to a specific event channel
type EventSubscriber struct {
	ID       string
	Channel  chan SubscriptionEvent
	Filter   func(event SubscriptionEvent) bool
	WorkerID *string // For log stream filtering

	mu     sync.Mutex
	closed bool
}

// close marks the subscriber closed and closes its channel. Idempotent — safe
// to call from both the broadcaster's shutdown path and removeSubscriber.
func (s *EventSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.Channel)
	}
}

// send delivers an event to the subscriber with a timeout. It is a no-op once
// the subscriber is closed, so a send racing shutdown never panics.
func (s *EventSubscriber) send(event SubscriptionEvent, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.Channel <- event:
	case <-time.After(timeout):
		// Channel is full or subscriber is slow, drop the event
	}
}

// NewResolver creates a new Resolver with the given dependencies.
// It initializes the event broadcasting system for subscriptions.
func NewResolver(overseer *core.Overseer) *Resolver {
	ctx, cancel := context.WithCancel(context.Background())

	r := &Resolver{
		Overseer:     overseer,
		eventChannel: make(chan SubscriptionEvent, 100), // Buffered channel
		subscribers:  make(map[string]*EventSubscriber),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start the event broadcaster
	r.wg.Add(1)
	go r.eventBroadcaster()

	return r
}

// Stop gracefully shuts down the resolver and all subscriptions
func (r *Resolver) Stop() {
	r.cancel()
	r.wg.Wait()
	// eventChannel is deliberately NOT closed: Broadcast* calls select-send and
	// a concurrent Stop would otherwise make a send panic on a closed channel.
	// Once the broadcaster has exited (wg.Wait), no one reads it — GC reclaims.
}

// eventBroadcaster listens for events and broadcasts them to all subscribers
func (r *Resolver) eventBroadcaster() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			// Close all subscriber channels on shutdown
			r.subscribersMu.Lock()
			for _, sub := range r.subscribers {
				sub.close()
			}
			r.subscribers = make(map[string]*EventSubscriber)
			r.subscribersMu.Unlock()
			return

		case event := <-r.eventChannel:
			r.broadcastEvent(event)
		}
	}
}

// broadcastEvent sends an event to all matching subscribers
func (r *Resolver) broadcastEvent(event SubscriptionEvent) {
	r.subscribersMu.RLock()
	subscribers := make([]*EventSubscriber, 0, len(r.subscribers))
	for _, sub := range r.subscribers {
		subscribers = append(subscribers, sub)
	}
	r.subscribersMu.RUnlock()

	// Send to each subscriber in parallel using goroutines
	for _, sub := range subscribers {
		go func(sub *EventSubscriber) {
			// Check if subscriber has a filter and if the event passes
			if sub.Filter != nil && !sub.Filter(event) {
				return
			}
			sub.send(event, 100*time.Millisecond)
		}(sub)
	}
}

// addSubscriber registers a new event subscriber
func (r *Resolver) addSubscriber(sub *EventSubscriber) {
	r.subscribersMu.Lock()
	defer r.subscribersMu.Unlock()
	r.subscribers[sub.ID] = sub
}

// removeSubscriber unregisters an event subscriber and closes its channel
func (r *Resolver) removeSubscriber(id string) {
	r.subscribersMu.Lock()
	defer r.subscribersMu.Unlock()

	if sub, exists := r.subscribers[id]; exists {
		sub.close()
		delete(r.subscribers, id)
	}
}

// BroadcastWorkerEvent broadcasts a worker-related event to all subscribers
func (r *Resolver) BroadcastWorkerEvent(worker *model.Worker, eventType model.UpdateType) {
	if r == nil {
		return
	}

	event := SubscriptionEvent{
		Type:      eventType,
		Worker:    worker,
		Timestamp: time.Now(),
	}

	select {
	case r.eventChannel <- event:
	default:
		// Channel is full, drop the event
	}
}

// BroadcastBeadEvent broadcasts a bead-related event to all subscribers
func (r *Resolver) BroadcastBeadEvent(bead *model.Bead, eventType model.UpdateType) {
	if r == nil {
		return
	}

	event := SubscriptionEvent{
		Type:      eventType,
		Bead:      bead,
		Timestamp: time.Now(),
	}

	select {
	case r.eventChannel <- event:
	default:
		// Channel is full, drop the event
	}
}

// BroadcastLogEntry broadcasts a log entry to all subscribers
func (r *Resolver) BroadcastLogEntry(logEntry *model.LogEntry) {
	if r == nil {
		return
	}

	event := SubscriptionEvent{
		Type:      model.UpdateTypeBeadUpdated, // Re-use existing type for logging
		LogEntry:  logEntry,
		Timestamp: time.Now(),
	}

	select {
	case r.eventChannel <- event:
	default:
		// Channel is full, drop the event
	}
}

// generateSubscriberID generates a unique subscriber ID
func generateSubscriberID() string {
	return uuid.New().String()
}

// matchesBeadFilter checks if a bead matches the given filter criteria
func matchesBeadFilter(bead *model.Bead, filter *model.BeadFilter) bool {
	if filter == nil {
		return true
	}

	// Check status filter
	if len(filter.Status) > 0 {
		matched := false
		for _, status := range filter.Status {
			if bead.Status == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check type filter
	if len(filter.Type) > 0 {
		matched := false
		for _, t := range filter.Type {
			if bead.Type == t {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check project filter
	if filter.Project != nil && bead.Project != *filter.Project {
		return false
	}

	// Check priority range
	if filter.PriorityMin != nil && bead.Priority < *filter.PriorityMin {
		return false
	}
	if filter.PriorityMax != nil && bead.Priority > *filter.PriorityMax {
		return false
	}

	// Check labels filter (bead must have all specified labels)
	if len(filter.Labels) > 0 {
		for _, filterLabel := range filter.Labels {
			found := false
			for _, beadLabel := range bead.Labels {
				if filterLabel == beadLabel {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	// Check created after filter
	if filter.CreatedAfter != nil && bead.CreatedAt.Before(*filter.CreatedAfter) {
		return false
	}

	// Check created before filter
	if filter.CreatedBefore != nil && bead.CreatedAt.After(*filter.CreatedBefore) {
		return false
	}

	return true
}

// Bead returns the BeadResolver for field-level resolution.
// This enables lazy loading of dependencies, dependents, workers, patterns, and impact analysis.
func (r *Resolver) Bead() BeadResolver {
	return &beadResolver{r}
}

// Worker returns the WorkerResolver for field-level resolution.
// This enables lazy loading of bead and logs.
func (r *Resolver) Worker() WorkerResolver {
	return &workerResolver{r}
}
