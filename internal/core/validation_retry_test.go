package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestValidationRetryTracking(t *testing.T) {
	config := DefaultConfig()
	config.Validation.MaxRounds = 3

	o := NewOverseer(config)

	beadID := "test-bead-123"

	// Initially zero
	if count := o.getValidationRetryCount(beadID); count != 0 {
		t.Errorf("Expected initial count 0, got %d", count)
	}

	// Increment
	o.incrementValidationRetry(beadID)
	if count := o.getValidationRetryCount(beadID); count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Increment again
	o.incrementValidationRetry(beadID)
	if count := o.getValidationRetryCount(beadID); count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}

	// Clear
	o.clearValidationRetry(beadID)
	if count := o.getValidationRetryCount(beadID); count != 0 {
		t.Errorf("Expected count 0 after clear, got %d", count)
	}

	// Clear unknown bead (no panic)
	o.clearValidationRetry("unknown-bead")
}

func TestValidationRetryCount_MaxRounds(t *testing.T) {
	config := DefaultConfig()
	config.Validation.MaxRounds = 2

	o := NewOverseer(config)
	beadID := "test-bead-456"

	// Simulate exhausted retries
	o.incrementValidationRetry(beadID) // count = 1
	o.incrementValidationRetry(beadID) // count = 2

	if count := o.getValidationRetryCount(beadID); count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}

	// One more increment
	o.incrementValidationRetry(beadID) // count = 3

	if count := o.getValidationRetryCount(beadID); count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}
}

func TestValidationRetry_ConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	o := NewOverseer(config)
	beadID := "concurrent-bead"

	// Concurrent increments should not race
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			o.incrementValidationRetry(beadID)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if count := o.getValidationRetryCount(beadID); count != 10 {
		t.Errorf("Expected count 10 after concurrent increments, got %d", count)
	}
}

func TestFindProjectPathForBeadID(t *testing.T) {
	config := DefaultConfig()
	o := NewOverseer(config)

	o.projects = []types.Project{
		{Name: "hydra", Path: "/projects/hydra"},
		{Name: "myapp", Path: "/projects/myapp"},
	}

	tests := []struct {
		beadID   string
		expected string
	}{
		{"hydra-abc123", "/projects/hydra"},
		{"myapp-xyz789", "/projects/myapp"},
		{"unknown-proj-123", ""},
		{"hydra/backend-abc", "/projects/hydra"},
	}

	for _, tc := range tests {
		id, _ := types.ParseBeadID(tc.beadID)
		result := o.findProjectPathForBeadID(id)
		if result != tc.expected {
			t.Errorf("beadID %s: expected %q, got %q", tc.beadID, tc.expected, result)
		}
	}
}
