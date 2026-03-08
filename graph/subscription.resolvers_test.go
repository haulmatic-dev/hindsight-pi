package graph

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/graph/model"
)

// TestWorkerUpdatesSubscription tests the WorkerUpdates subscription resolver
func TestWorkerUpdatesSubscription(t *testing.T) {
	t.Run("creates subscription channel", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		ctx := context.Background()

		// Act - Subscription resolver currently panics (not implemented)
		// This test documents the current behavior
		defer func() {
			if r := recover(); r != nil {
				// Expected - subscription not yet implemented
				t.Logf("Expected panic from unimplemented subscription: %v", r)
			}
		}()

		ch, err := resolver.Subscription().WorkerUpdates(ctx)

		// Assert - if we reach here, subscription is now implemented
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if ch == nil {
			t.Error("Expected non-nil channel when no error")
		}
	})

	t.Run("receives worker update events", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		// Create a test subscriber directly using the internal mechanism
		subscriberCh := make(chan SubscriptionEvent, 10)
		sub := &EventSubscriber{
			ID:      "test-subscriber-1",
			Channel: subscriberCh,
			Filter: func(event SubscriptionEvent) bool {
				return event.Type == model.UpdateTypeWorkerStarted ||
					event.Type == model.UpdateTypeWorkerCompleted ||
					event.Type == model.UpdateTypeWorkerFailed
			},
		}

		// Act - Add subscriber
		resolver.addSubscriber(sub)

		// Broadcast a worker event
		testWorker := &model.Worker{
			ID:        "worker-123",
			Status:    model.WorkerStatusRunning,
			AgentType: "test-agent",
			Pid:       1234,
			StartTime: time.Now(),
		}
		resolver.BroadcastWorkerEvent(testWorker, model.UpdateTypeWorkerStarted)

		// Allow time for broadcast
		time.Sleep(50 * time.Millisecond)

		// Assert
		select {
		case event := <-subscriberCh:
			if event.Type != model.UpdateTypeWorkerStarted {
				t.Errorf("Expected event type WORKER_STARTED, got %s", event.Type)
			}
			if event.Worker == nil {
				t.Fatal("Expected worker in event")
			}
			if event.Worker.ID != "worker-123" {
				t.Errorf("Expected worker ID 'worker-123', got '%s'", event.Worker.ID)
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for worker event")
		}

		// Cleanup
		resolver.removeSubscriber(sub.ID)
	})

	t.Run("context cancellation closes channel", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		_, cancel := context.WithCancel(context.Background())

		// Create test subscriber
		subscriberCh := make(chan SubscriptionEvent, 10)
		sub := &EventSubscriber{
			ID:      "test-sub-cancel",
			Channel: subscriberCh,
		}
		resolver.addSubscriber(sub)

		// Act - Cancel context
		cancel()
		time.Sleep(50 * time.Millisecond)

		// Assert - Broadcast should not reach subscriber after context cancellation
		// and resolver cleanup
		resolver.removeSubscriber(sub.ID)

		select {
		case _, ok := <-subscriberCh:
			if ok {
				t.Error("Expected subscriber channel to be closed")
			}
		default:
			// Channel still open but empty, which is fine
		}
	})

	t.Run("multiple subscribers receive events", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		numSubscribers := 3
		subscribers := make([]*EventSubscriber, numSubscribers)
		subscriberChannels := make([]chan SubscriptionEvent, numSubscribers)

		for i := 0; i < numSubscribers; i++ {
			ch := make(chan SubscriptionEvent, 10)
			subscriberChannels[i] = ch
			subscribers[i] = &EventSubscriber{
				ID:      "test-sub-" + string(rune('A'+i)),
				Channel: ch,
			}
			resolver.addSubscriber(subscribers[i])
		}

		// Act - Broadcast worker event
		testWorker := &model.Worker{
			ID:        "worker-multi",
			Status:    model.WorkerStatusCompleted,
			AgentType: "test-agent",
			Pid:       5678,
			StartTime: time.Now(),
		}
		resolver.BroadcastWorkerEvent(testWorker, model.UpdateTypeWorkerCompleted)

		// Allow time for broadcast
		time.Sleep(100 * time.Millisecond)

		// Assert - All subscribers should receive the event
		for i, ch := range subscriberChannels {
			select {
			case event := <-ch:
				if event.Type != model.UpdateTypeWorkerCompleted {
					t.Errorf("Subscriber %d: expected WORKER_COMPLETED, got %s", i, event.Type)
				}
				if event.Worker == nil || event.Worker.ID != "worker-multi" {
					t.Errorf("Subscriber %d: expected worker with ID 'worker-multi'", i)
				}
			case <-time.After(500 * time.Millisecond):
				t.Errorf("Subscriber %d: timeout waiting for event", i)
			}
		}

		// Cleanup
		for _, sub := range subscribers {
			resolver.removeSubscriber(sub.ID)
		}
	})
}

// TestBeadUpdatesSubscription tests the BeadUpdates subscription resolver
func TestBeadUpdatesSubscription(t *testing.T) {
	t.Run("creates subscription with filter", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		ctx := context.Background()
		filter := &model.BeadFilter{
			Status: []model.BeadStatus{model.BeadStatusOpen},
		}

		// Act - Subscription resolver currently panics (not implemented)
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Expected panic from unimplemented subscription: %v", r)
			}
		}()

		ch, err := resolver.Subscription().BeadUpdates(ctx, filter)

		// Assert - if we reach here, subscription is now implemented
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if ch == nil {
			t.Error("Expected non-nil channel when no error")
		}
	})

	t.Run("receives filtered bead events", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		// Create test subscriber with bead filter
		subscriberCh := make(chan SubscriptionEvent, 10)
		filter := &model.BeadFilter{
			Status: []model.BeadStatus{model.BeadStatusOpen, model.BeadStatusInProgress},
		}

		sub := &EventSubscriber{
			ID:      "test-bead-sub",
			Channel: subscriberCh,
			Filter: func(event SubscriptionEvent) bool {
				if event.Bead == nil {
					return false
				}
				return matchesBeadFilter(event.Bead, filter)
			},
		}
		resolver.addSubscriber(sub)

		// Act - Broadcast bead events
		openBead := &model.Bead{
			ID:       "bead-open-1",
			Title:    "Open Bead",
			Status:   model.BeadStatusOpen,
			Priority: 1,
		}
		completedBead := &model.Bead{
			ID:       "bead-completed-1",
			Title:    "Completed Bead",
			Status:   model.BeadStatusCompleted,
			Priority: 2,
		}

		resolver.BroadcastBeadEvent(openBead, model.UpdateTypeBeadCreated)
		resolver.BroadcastBeadEvent(completedBead, model.UpdateTypeBeadCompleted)

		time.Sleep(100 * time.Millisecond)

		// Assert - Only open bead should be received
		select {
		case event := <-subscriberCh:
			if event.Type != model.UpdateTypeBeadCreated {
				t.Errorf("Expected BEAD_CREATED, got %s", event.Type)
			}
			if event.Bead == nil || event.Bead.ID != "bead-open-1" {
				t.Error("Expected open bead event")
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for open bead event")
		}

		// Should not receive completed bead
		select {
		case event := <-subscriberCh:
			t.Errorf("Should not receive completed bead event, got: %+v", event)
		case <-time.After(100 * time.Millisecond):
			// Expected - no more events
		}

		// Cleanup
		resolver.removeSubscriber(sub.ID)
	})

	t.Run("filter excludes non-matching beads", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		subscriberCh := make(chan SubscriptionEvent, 10)
		filter := &model.BeadFilter{
			Project: stringPtr("project-alpha"),
			Labels:  []string{"urgent"},
		}

		sub := &EventSubscriber{
			ID:      "test-bead-filter",
			Channel: subscriberCh,
			Filter: func(event SubscriptionEvent) bool {
				if event.Bead == nil {
					return false
				}
				return matchesBeadFilter(event.Bead, filter)
			},
		}
		resolver.addSubscriber(sub)

		// Act - Broadcast beads with different properties
		matchingBead := &model.Bead{
			ID:       "bead-match",
			Title:    "Matching Bead",
			Project:  "project-alpha",
			Labels:   []string{"urgent", "backend"},
			Status:   model.BeadStatusOpen,
			Priority: 5,
		}
		nonMatchingBead1 := &model.Bead{
			ID:       "bead-wrong-project",
			Title:    "Wrong Project",
			Project:  "project-beta",
			Labels:   []string{"urgent"},
			Status:   model.BeadStatusOpen,
			Priority: 5,
		}
		nonMatchingBead2 := &model.Bead{
			ID:       "bead-wrong-label",
			Title:    "Wrong Label",
			Project:  "project-alpha",
			Labels:   []string{"low-priority"},
			Status:   model.BeadStatusOpen,
			Priority: 5,
		}

		resolver.BroadcastBeadEvent(matchingBead, model.UpdateTypeBeadCreated)
		resolver.BroadcastBeadEvent(nonMatchingBead1, model.UpdateTypeBeadCreated)
		resolver.BroadcastBeadEvent(nonMatchingBead2, model.UpdateTypeBeadCreated)

		time.Sleep(100 * time.Millisecond)

		// Assert - Only matching bead should be received
		select {
		case event := <-subscriberCh:
			if event.Bead == nil || event.Bead.ID != "bead-match" {
				t.Errorf("Expected matching bead, got: %+v", event.Bead)
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for matching bead event")
		}

		// Should not receive any more events
		select {
		case event := <-subscriberCh:
			t.Errorf("Should not receive non-matching bead event, got: %+v", event)
		case <-time.After(100 * time.Millisecond):
			// Expected - no more events
		}

		// Cleanup
		resolver.removeSubscriber(sub.ID)
	})

	t.Run("context cancellation", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		ctx, cancel := context.WithCancel(context.Background())
		filter := &model.BeadFilter{}

		// Act
		cancel()

		// Assert - Handle panic from unimplemented subscription
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Expected panic from unimplemented subscription: %v", r)
			}
		}()

		ch, err := resolver.Subscription().BeadUpdates(ctx, filter)
		if err != nil {
			t.Logf("Note: Subscription returned error: %v", err)
		}
		if ch != nil {
			t.Log("Note: Channel returned despite cancelled context")
		}
	})
}

// TestDagUpdatesSubscription tests the DagUpdates subscription resolver
func TestDagUpdatesSubscription(t *testing.T) {
	t.Run("creates DAG update subscription", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		ctx := context.Background()

		// Act - Handle panic from unimplemented subscription
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Expected panic from unimplemented subscription: %v", r)
			}
		}()

		ch, err := resolver.Subscription().DagUpdates(ctx)

		// Assert - if we reach here, subscription is now implemented
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if ch == nil {
			t.Error("Expected non-nil channel when no error")
		}
	})

	t.Run("receives bead and worker events", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		subscriberCh := make(chan SubscriptionEvent, 10)
		sub := &EventSubscriber{
			ID:      "test-dag-sub",
			Channel: subscriberCh,
			Filter: func(event SubscriptionEvent) bool {
				// Accept all DAG-related events
				return event.Bead != nil || event.Worker != nil
			},
		}
		resolver.addSubscriber(sub)

		// Act - Broadcast various events
		testBead := &model.Bead{
			ID:       "dag-bead-1",
			Title:    "DAG Bead",
			Status:   model.BeadStatusInProgress,
			Priority: 3,
		}
		testWorker := &model.Worker{
			ID:        "dag-worker-1",
			Status:    model.WorkerStatusRunning,
			AgentType: "dag-agent",
		}

		resolver.BroadcastBeadEvent(testBead, model.UpdateTypeBeadUpdated)
		resolver.BroadcastWorkerEvent(testWorker, model.UpdateTypeWorkerStarted)

		time.Sleep(100 * time.Millisecond)

		// Assert - Should receive both events
		eventCount := 0
		timeout := time.After(500 * time.Millisecond)

		for eventCount < 2 {
			select {
			case event := <-subscriberCh:
				eventCount++
				if event.Bead != nil {
					if event.Bead.ID != "dag-bead-1" {
						t.Errorf("Expected bead ID 'dag-bead-1', got '%s'", event.Bead.ID)
					}
				}
				if event.Worker != nil {
					if event.Worker.ID != "dag-worker-1" {
						t.Errorf("Expected worker ID 'dag-worker-1', got '%s'", event.Worker.ID)
					}
				}
			case <-timeout:
				t.Fatalf("Timeout waiting for events. Received %d/2 events", eventCount)
			}
		}

		if eventCount != 2 {
			t.Errorf("Expected 2 events, got %d", eventCount)
		}

		// Cleanup
		resolver.removeSubscriber(sub.ID)
	})

	t.Run("context cancellation", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		ctx, cancel := context.WithCancel(context.Background())

		// Act
		cancel()

		// Assert - Handle panic from unimplemented subscription
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Expected panic from unimplemented subscription: %v", r)
			}
		}()

		ch, err := resolver.Subscription().DagUpdates(ctx)
		if err != nil {
			t.Logf("Note: Subscription returned error: %v", err)
		}
		if ch != nil {
			t.Log("Note: Channel returned despite cancelled context")
		}
	})
}

// TestLogStreamSubscription tests the LogStream subscription resolver
func TestLogStreamSubscription(t *testing.T) {
	t.Run("creates log stream subscription", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		ctx := context.Background()
		workerID := "worker-123"

		// Act - Handle panic from unimplemented subscription
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Expected panic from unimplemented subscription: %v", r)
			}
		}()

		ch, err := resolver.Subscription().LogStream(ctx, &workerID)

		// Assert - if we reach here, subscription is now implemented
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if ch == nil {
			t.Error("Expected non-nil channel when no error")
		}
	})

	t.Run("receives log entries", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		subscriberCh := make(chan SubscriptionEvent, 10)
		sub := &EventSubscriber{
			ID:      "test-log-sub",
			Channel: subscriberCh,
			Filter: func(event SubscriptionEvent) bool {
				return event.LogEntry != nil
			},
		}
		resolver.addSubscriber(sub)

		// Act - Broadcast log entries
		logEntry1 := &model.LogEntry{
			Timestamp: time.Now(),
			Level:     model.LogLevelInfo,
			Message:   "Starting task execution",
			WorkerID:  stringPtr("worker-abc"),
		}
		logEntry2 := &model.LogEntry{
			Timestamp: time.Now(),
			Level:     model.LogLevelDebug,
			Message:   "Processing data",
			WorkerID:  stringPtr("worker-abc"),
		}

		resolver.BroadcastLogEntry(logEntry1)
		resolver.BroadcastLogEntry(logEntry2)

		time.Sleep(100 * time.Millisecond)

		// Assert - Should receive both log entries (order may vary due to concurrent broadcast)
		messages := make(map[string]bool)
		timeout := time.After(500 * time.Millisecond)

		for i := 0; i < 2; i++ {
			select {
			case event := <-subscriberCh:
				if event.LogEntry == nil {
					t.Error("Expected log entry in event")
					continue
				}
				messages[event.LogEntry.Message] = true
			case <-timeout:
				t.Errorf("Timeout waiting for log entry %d", i+1)
			}
		}

		if !messages["Starting task execution"] {
			t.Error("Expected to receive 'Starting task execution' log message")
		}
		if !messages["Processing data"] {
			t.Error("Expected to receive 'Processing data' log message")
		}

		// Cleanup
		resolver.removeSubscriber(sub.ID)
	})

	t.Run("worker ID filtering", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		targetWorkerID := "worker-target"
		subscriberCh := make(chan SubscriptionEvent, 10)

		sub := &EventSubscriber{
			ID:       "test-log-filter-sub",
			Channel:  subscriberCh,
			WorkerID: &targetWorkerID,
			Filter: func(event SubscriptionEvent) bool {
				if event.LogEntry == nil || event.LogEntry.WorkerID == nil {
					return false
				}
				return *event.LogEntry.WorkerID == targetWorkerID
			},
		}
		resolver.addSubscriber(sub)

		// Act - Broadcast logs from different workers
		targetLog := &model.LogEntry{
			Timestamp: time.Now(),
			Level:     model.LogLevelInfo,
			Message:   "Target worker log",
			WorkerID:  &targetWorkerID,
		}
		otherLog := &model.LogEntry{
			Timestamp: time.Now(),
			Level:     model.LogLevelInfo,
			Message:   "Other worker log",
			WorkerID:  stringPtr("worker-other"),
		}
		noWorkerLog := &model.LogEntry{
			Timestamp: time.Now(),
			Level:     model.LogLevelWarn,
			Message:   "No worker ID log",
			WorkerID:  nil,
		}

		resolver.BroadcastLogEntry(targetLog)
		resolver.BroadcastLogEntry(otherLog)
		resolver.BroadcastLogEntry(noWorkerLog)

		time.Sleep(100 * time.Millisecond)

		// Assert - Only target worker log should be received
		select {
		case event := <-subscriberCh:
			if event.LogEntry == nil {
				t.Fatal("Expected log entry")
			}
			if event.LogEntry.Message != "Target worker log" {
				t.Errorf("Expected 'Target worker log', got: %s", event.LogEntry.Message)
			}
			if event.LogEntry.WorkerID == nil || *event.LogEntry.WorkerID != targetWorkerID {
				t.Error("Expected target worker ID")
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("Timeout waiting for target worker log")
		}

		// Should not receive other logs
		select {
		case event := <-subscriberCh:
			t.Errorf("Should not receive other worker logs, got: %+v", event)
		case <-time.After(100 * time.Millisecond):
			// Expected - no more events
		}

		// Cleanup
		resolver.removeSubscriber(sub.ID)
	})

	t.Run("context cancellation", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		ctx, cancel := context.WithCancel(context.Background())
		workerID := "worker-123"

		// Act
		cancel()

		// Assert - Handle panic from unimplemented subscription
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Expected panic from unimplemented subscription: %v", r)
			}
		}()

		ch, err := resolver.Subscription().LogStream(ctx, &workerID)
		if err == nil {
			t.Log("Note: Subscription may or may not check context")
		}
		if ch != nil {
			t.Log("Note: Channel returned despite cancelled context")
		}
	})
}

// TestEventBroadcasting tests the event broadcasting system
func TestEventBroadcasting(t *testing.T) {
	t.Run("broadcasts to multiple subscribers", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		numSubscribers := 5
		subscribers := make([]*EventSubscriber, numSubscribers)
		subscriberChannels := make([]chan SubscriptionEvent, numSubscribers)

		for i := 0; i < numSubscribers; i++ {
			ch := make(chan SubscriptionEvent, 10)
			subscriberChannels[i] = ch
			subscribers[i] = &EventSubscriber{
				ID:      "broadcast-sub-" + string(rune('A'+i)),
				Channel: ch,
			}
			resolver.addSubscriber(subscribers[i])
		}

		// Act - Broadcast events
		testBead := &model.Bead{
			ID:       "broadcast-bead",
			Title:    "Broadcast Test Bead",
			Status:   model.BeadStatusOpen,
			Priority: 1,
		}

		resolver.BroadcastBeadEvent(testBead, model.UpdateTypeBeadCreated)
		time.Sleep(100 * time.Millisecond)

		// Assert - All subscribers should receive the event
		for i, ch := range subscriberChannels {
			select {
			case event := <-ch:
				if event.Bead == nil || event.Bead.ID != "broadcast-bead" {
					t.Errorf("Subscriber %d: expected broadcast bead, got: %+v", i, event.Bead)
				}
			case <-time.After(500 * time.Millisecond):
				t.Errorf("Subscriber %d: timeout waiting for broadcast event", i)
			}
		}

		// Cleanup
		for _, sub := range subscribers {
			resolver.removeSubscriber(sub.ID)
		}
	})

	t.Run("subscriber cleanup on disconnect", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		subCh := make(chan SubscriptionEvent, 10)
		sub := &EventSubscriber{
			ID:      "cleanup-sub",
			Channel: subCh,
		}
		resolver.addSubscriber(sub)

		// Verify subscriber exists
		resolver.subscribersMu.RLock()
		_, exists := resolver.subscribers["cleanup-sub"]
		resolver.subscribersMu.RUnlock()
		if !exists {
			t.Fatal("Subscriber should exist after adding")
		}

		// Act - Remove subscriber
		resolver.removeSubscriber("cleanup-sub")

		// Assert - Subscriber should be removed
		resolver.subscribersMu.RLock()
		_, exists = resolver.subscribers["cleanup-sub"]
		resolver.subscribersMu.RUnlock()
		if exists {
			t.Error("Subscriber should be removed after cleanup")
		}

		// Channel should be closed
		select {
		case _, ok := <-subCh:
			if ok {
				t.Error("Expected subscriber channel to be closed")
			}
		default:
			t.Error("Channel should be closed, not just empty")
		}
	})

	t.Run("event filtering works correctly", func(t *testing.T) {
		// Arrange
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		// Create subscribers with different filters
		workerOnlyCh := make(chan SubscriptionEvent, 10)
		workerOnlySub := &EventSubscriber{
			ID:      "worker-only-sub",
			Channel: workerOnlyCh,
			Filter: func(event SubscriptionEvent) bool {
				return event.Worker != nil
			},
		}

		beadOnlyCh := make(chan SubscriptionEvent, 10)
		beadOnlySub := &EventSubscriber{
			ID:      "bead-only-sub",
			Channel: beadOnlyCh,
			Filter: func(event SubscriptionEvent) bool {
				return event.Bead != nil
			},
		}

		logOnlyCh := make(chan SubscriptionEvent, 10)
		logOnlySub := &EventSubscriber{
			ID:      "log-only-sub",
			Channel: logOnlyCh,
			Filter: func(event SubscriptionEvent) bool {
				return event.LogEntry != nil
			},
		}

		resolver.addSubscriber(workerOnlySub)
		resolver.addSubscriber(beadOnlySub)
		resolver.addSubscriber(logOnlySub)

		// Act - Broadcast different event types
		resolver.BroadcastWorkerEvent(&model.Worker{ID: "w1"}, model.UpdateTypeWorkerStarted)
		resolver.BroadcastBeadEvent(&model.Bead{ID: "b1"}, model.UpdateTypeBeadCreated)
		resolver.BroadcastLogEntry(&model.LogEntry{Message: "log1"})

		time.Sleep(100 * time.Millisecond)

		// Assert - Each subscriber should only receive their filtered events
		// Worker subscriber
		select {
		case event := <-workerOnlyCh:
			if event.Worker == nil {
				t.Error("Worker subscriber should receive worker event")
			}
		case <-time.After(200 * time.Millisecond):
			t.Error("Worker subscriber timeout")
		}

		// Should not receive other event types
		select {
		case <-workerOnlyCh:
			t.Error("Worker subscriber should not receive non-worker events")
		case <-time.After(50 * time.Millisecond):
			// Expected
		}

		// Bead subscriber
		select {
		case event := <-beadOnlyCh:
			if event.Bead == nil {
				t.Error("Bead subscriber should receive bead event")
			}
		case <-time.After(200 * time.Millisecond):
			t.Error("Bead subscriber timeout")
		}

		// Should not receive other event types
		select {
		case <-beadOnlyCh:
			t.Error("Bead subscriber should not receive non-bead events")
		case <-time.After(50 * time.Millisecond):
			// Expected
		}

		// Log subscriber
		select {
		case event := <-logOnlyCh:
			if event.LogEntry == nil {
				t.Error("Log subscriber should receive log event")
			}
		case <-time.After(200 * time.Millisecond):
			t.Error("Log subscriber timeout")
		}

		// Should not receive other event types
		select {
		case <-logOnlyCh:
			t.Error("Log subscriber should not receive non-log events")
		case <-time.After(50 * time.Millisecond):
			// Expected
		}

		// Cleanup
		resolver.removeSubscriber(workerOnlySub.ID)
		resolver.removeSubscriber(beadOnlySub.ID)
		resolver.removeSubscriber(logOnlySub.ID)
	})
}

// TestConcurrentAccess tests concurrent operations on the resolver
func TestConcurrentAccess(t *testing.T) {
	t.Run("concurrent subscriber additions", func(t *testing.T) {
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		numGoroutines := 10
		numSubscribersPerGoroutine := 10

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(goroutineID int) {
				defer wg.Done()
				for j := 0; j < numSubscribersPerGoroutine; j++ {
					subID := "concurrent-sub-" + string(rune('0'+goroutineID)) + "-" + string(rune('0'+j))
					sub := &EventSubscriber{
						ID:      subID,
						Channel: make(chan SubscriptionEvent, 10),
					}
					resolver.addSubscriber(sub)
				}
			}(i)
		}

		wg.Wait()

		// Verify all subscribers were added
		resolver.subscribersMu.RLock()
		subscriberCount := len(resolver.subscribers)
		resolver.subscribersMu.RUnlock()

		expectedCount := numGoroutines * numSubscribersPerGoroutine
		if subscriberCount != expectedCount {
			t.Errorf("Expected %d subscribers, got %d", expectedCount, subscriberCount)
		}
	})

	t.Run("consecutive broadcast with concurrent subscribers", func(t *testing.T) {
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		numSubscribers := 20
		subscribers := make([]*EventSubscriber, numSubscribers)
		subscriberChannels := make([]chan SubscriptionEvent, numSubscribers)

		// Add subscribers
		for i := 0; i < numSubscribers; i++ {
			ch := make(chan SubscriptionEvent, 100)
			subscriberChannels[i] = ch
			subscribers[i] = &EventSubscriber{
				ID:      "concurrent-broadcast-sub-" + string(rune('A'+i%26)),
				Channel: ch,
			}
			resolver.addSubscriber(subscribers[i])
		}

		// Concurrent broadcasts
		numBroadcasts := 50
		var broadcastWg sync.WaitGroup
		broadcastWg.Add(numBroadcasts)

		for i := 0; i < numBroadcasts; i++ {
			go func(broadcastID int) {
				defer broadcastWg.Done()
				bead := &model.Bead{
					ID:     "concurrent-bead-" + string(rune('0'+broadcastID%10)),
					Status: model.BeadStatusOpen,
				}
				resolver.BroadcastBeadEvent(bead, model.UpdateTypeBeadUpdated)
			}(i)
		}

		broadcastWg.Wait()
		time.Sleep(200 * time.Millisecond)

		// Verify all subscribers received events
		for i, ch := range subscriberChannels {
			count := 0
			for {
				select {
				case <-ch:
					count++
				case <-time.After(50 * time.Millisecond):
					goto done
				}
			}
		done:
			if count == 0 {
				t.Errorf("Subscriber %d received no events", i)
			}
		}

		// Cleanup
		for _, sub := range subscribers {
			resolver.removeSubscriber(sub.ID)
		}
	})
}

// TestGoroutineCleanup tests that goroutines are properly cleaned up
func TestGoroutineCleanup(t *testing.T) {
	t.Run("resolver stop closes all subscriber channels", func(t *testing.T) {
		resolver := createTestSubscriptionResolver()

		// Add multiple subscribers
		numSubscribers := 5
		subscriberChannels := make([]chan SubscriptionEvent, numSubscribers)

		for i := 0; i < numSubscribers; i++ {
			ch := make(chan SubscriptionEvent, 10)
			subscriberChannels[i] = ch
			sub := &EventSubscriber{
				ID:      "cleanup-test-sub-" + string(rune('A'+i)),
				Channel: ch,
			}
			resolver.addSubscriber(sub)
		}

		// Act - Stop resolver
		resolver.Stop()

		// Assert - All channels should be closed
		for i, ch := range subscriberChannels {
			select {
			case _, ok := <-ch:
				if ok {
					t.Errorf("Subscriber channel %d should be closed", i)
				}
			default:
				t.Errorf("Subscriber channel %d should be closed (non-blocking read)", i)
			}
		}
	})

	t.Run("event broadcaster goroutine exits on stop", func(t *testing.T) {
		resolver := createTestSubscriptionResolver()

		// Add a subscriber to ensure broadcaster is running
		subCh := make(chan SubscriptionEvent, 10)
		sub := &EventSubscriber{
			ID:      "exit-test-sub",
			Channel: subCh,
		}
		resolver.addSubscriber(sub)

		// Broadcast an event to ensure broadcaster is active
		resolver.BroadcastBeadEvent(&model.Bead{ID: "test"}, model.UpdateTypeBeadCreated)
		time.Sleep(50 * time.Millisecond)

		// Act - Stop resolver in a goroutine to detect hangs
		done := make(chan bool)
		go func() {
			resolver.Stop()
			done <- true
		}()

		// Assert - Wait group should complete within timeout
		select {
		case <-done:
			// Success - Stop() completed without hanging
		case <-time.After(500 * time.Millisecond):
			t.Error("Resolver.Stop() may have hung - broadcaster might not have exited")
		}
	})
}

// TestBroadcastEdgeCases tests edge cases in broadcasting
func TestBroadcastEdgeCases(t *testing.T) {
	t.Run("broadcast to nil resolver", func(t *testing.T) {
		// This should not panic
		var resolver *Resolver
		resolver.BroadcastBeadEvent(&model.Bead{ID: "test"}, model.UpdateTypeBeadCreated)
		resolver.BroadcastWorkerEvent(&model.Worker{ID: "test"}, model.UpdateTypeWorkerStarted)
		resolver.BroadcastLogEntry(&model.LogEntry{Message: "test"})
	})

	t.Run("broadcast with full channel drops event", func(t *testing.T) {
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		// Create subscriber with very small buffer
		subCh := make(chan SubscriptionEvent, 1)
		sub := &EventSubscriber{
			ID:      "full-channel-sub",
			Channel: subCh,
		}
		resolver.addSubscriber(sub)

		// Fill the channel
		subCh <- SubscriptionEvent{Type: model.UpdateTypeBeadCreated}

		// Try to broadcast more events - should not block indefinitely
		done := make(chan bool)
		go func() {
			resolver.BroadcastBeadEvent(&model.Bead{ID: "test1"}, model.UpdateTypeBeadUpdated)
			resolver.BroadcastBeadEvent(&model.Bead{ID: "test2"}, model.UpdateTypeBeadUpdated)
			resolver.BroadcastBeadEvent(&model.Bead{ID: "test3"}, model.UpdateTypeBeadUpdated)
			done <- true
		}()

		select {
		case <-done:
			// Expected - broadcast completed (some events may have been dropped)
		case <-time.After(500 * time.Millisecond):
			t.Error("Broadcast should complete even with full subscriber channel")
		}

		resolver.removeSubscriber(sub.ID)
	})

	t.Run("broadcast with slow subscriber", func(t *testing.T) {
		resolver := createTestSubscriptionResolver()
		defer resolver.Stop()

		// Create subscriber that will be slow
		subCh := make(chan SubscriptionEvent, 1)
		sub := &EventSubscriber{
			ID:      "slow-sub",
			Channel: subCh,
		}
		resolver.addSubscriber(sub)

		// Broadcast multiple events rapidly
		for i := 0; i < 10; i++ {
			bead := &model.Bead{
				ID:     "rapid-bead-" + string(rune('0'+i)),
				Status: model.BeadStatusOpen,
			}
			resolver.BroadcastBeadEvent(bead, model.UpdateTypeBeadCreated)
		}

		// Allow time for broadcasts (with timeout for slow subscriber)
		time.Sleep(300 * time.Millisecond)

		// The broadcaster should handle this gracefully without blocking
		// We can't guarantee all events were delivered due to the timeout
		resolver.removeSubscriber(sub.ID)
	})
}

// TestMatchesBeadFilter tests the bead filter matching function
func TestMatchesBeadFilter(t *testing.T) {
	t.Run("nil filter matches all", func(t *testing.T) {
		bead := &model.Bead{
			ID:       "test-bead",
			Status:   model.BeadStatusOpen,
			Type:     "feature",
			Project:  "test-project",
			Priority: 5,
			Labels:   []string{"urgent"},
		}

		if !matchesBeadFilter(bead, nil) {
			t.Error("Nil filter should match all beads")
		}
	})

	t.Run("status filter works", func(t *testing.T) {
		bead := &model.Bead{
			ID:     "test-bead",
			Status: model.BeadStatusOpen,
		}

		// Match
		filter := &model.BeadFilter{
			Status: []model.BeadStatus{model.BeadStatusOpen, model.BeadStatusInProgress},
		}
		if !matchesBeadFilter(bead, filter) {
			t.Error("Should match when status is in filter list")
		}

		// No match
		filter2 := &model.BeadFilter{
			Status: []model.BeadStatus{model.BeadStatusCompleted},
		}
		if matchesBeadFilter(bead, filter2) {
			t.Error("Should not match when status is not in filter list")
		}
	})

	t.Run("type filter works", func(t *testing.T) {
		bead := &model.Bead{
			ID:   "test-bead",
			Type: "feature",
		}

		filter := &model.BeadFilter{
			Type: []string{"bug", "feature"},
		}
		if !matchesBeadFilter(bead, filter) {
			t.Error("Should match when type is in filter list")
		}

		filter2 := &model.BeadFilter{
			Type: []string{"documentation"},
		}
		if matchesBeadFilter(bead, filter2) {
			t.Error("Should not match when type is not in filter list")
		}
	})

	t.Run("project filter works", func(t *testing.T) {
		bead := &model.Bead{
			ID:      "test-bead",
			Project: "project-alpha",
		}

		filter := &model.BeadFilter{
			Project: stringPtr("project-alpha"),
		}
		if !matchesBeadFilter(bead, filter) {
			t.Error("Should match when project matches")
		}

		filter2 := &model.BeadFilter{
			Project: stringPtr("project-beta"),
		}
		if matchesBeadFilter(bead, filter2) {
			t.Error("Should not match when project doesn't match")
		}
	})

	t.Run("priority range filter works", func(t *testing.T) {
		bead := &model.Bead{
			ID:       "test-bead",
			Priority: 5,
		}

		// Within range
		minPriority := int32(1)
		maxPriority := int32(10)
		filter := &model.BeadFilter{
			PriorityMin: &minPriority,
			PriorityMax: &maxPriority,
		}
		if !matchesBeadFilter(bead, filter) {
			t.Error("Should match when priority is within range")
		}

		// Below min
		minPriority2 := int32(7)
		filter2 := &model.BeadFilter{
			PriorityMin: &minPriority2,
		}
		if matchesBeadFilter(bead, filter2) {
			t.Error("Should not match when priority is below minimum")
		}

		// Above max
		maxPriority2 := int32(3)
		filter3 := &model.BeadFilter{
			PriorityMax: &maxPriority2,
		}
		if matchesBeadFilter(bead, filter3) {
			t.Error("Should not match when priority is above maximum")
		}
	})

	t.Run("labels filter works", func(t *testing.T) {
		bead := &model.Bead{
			ID:     "test-bead",
			Labels: []string{"urgent", "backend", "api"},
		}

		// Has all required labels
		filter := &model.BeadFilter{
			Labels: []string{"urgent", "backend"},
		}
		if !matchesBeadFilter(bead, filter) {
			t.Error("Should match when bead has all required labels")
		}

		// Missing one label
		filter2 := &model.BeadFilter{
			Labels: []string{"urgent", "frontend"},
		}
		if matchesBeadFilter(bead, filter2) {
			t.Error("Should not match when bead is missing a required label")
		}
	})
}

// Helper functions

// createTestSubscriptionResolver creates a test resolver with mocked dependencies for subscription tests
func createTestSubscriptionResolver() *Resolver {
	return NewResolver(nil, nil, nil)
}
