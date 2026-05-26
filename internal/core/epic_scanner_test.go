package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestEpicScannerCreation verifies that EpicScanner can be created with proper configuration
func TestEpicScannerCreation(t *testing.T) {
	scanner := NewEpicScanner(5*time.Minute, nil)

	if scanner == nil {
		t.Fatal("NewEpicScanner should not return nil")
	}

	if scanner.interval != 5*time.Minute {
		t.Errorf("interval = %v, want %v", scanner.interval, 5*time.Minute)
	}
}

// TestGetEpicForReq verifies the req-XXX to epic mapping
func TestGetEpicForReq(t *testing.T) {
	tests := []struct {
		reqNum   int
		wantEpic string
	}{
		{1, "hydra-31h"},  // Phase 0 start
		{20, "hydra-31h"}, // Phase 0 end
		{21, "hydra-2qk"}, // Phase 1 start
		{40, "hydra-2qk"}, // Phase 1 end
		{41, "hydra-33k"}, // Phase 2 start
		{60, "hydra-33k"}, // Phase 2 end
		{61, "hydra-itm"}, // Phase 3 start
		{79, "hydra-itm"}, // Phase 3 end
		{0, ""},           // Invalid: too low
		{80, ""},          // Invalid: too high
		{100, ""},         // Invalid: way too high
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("req-%03d", tt.reqNum), func(t *testing.T) {
			got := getEpicForReq(tt.reqNum)
			if got != tt.wantEpic {
				t.Errorf("getEpicForReq(%d) = %q, want %q", tt.reqNum, got, tt.wantEpic)
			}
		})
	}
}

// TestExtractReqNumber verifies extraction of req number from label
func TestExtractReqNumber(t *testing.T) {
	tests := []struct {
		label   string
		wantNum int
		wantOk  bool
	}{
		{"req-001", 1, true},
		{"req-020", 20, true},
		{"req-079", 79, true},
		{"req-100", 100, true},
		{"REQ-001", 0, false},         // Case sensitive
		{"req001", 0, false},          // Missing hyphen
		{"req-", 0, false},            // Missing number
		{"requirement-001", 0, false}, // Wrong prefix
		{"other-label", 0, false},     // Not a req label
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			gotNum, gotOk := extractReqNumber(tt.label)
			if gotNum != tt.wantNum || gotOk != tt.wantOk {
				t.Errorf("extractReqNumber(%q) = (%d, %v), want (%d, %v)",
					tt.label, gotNum, gotOk, tt.wantNum, tt.wantOk)
			}
		})
	}
}

// TestEpicScannerScanWithMockClient verifies scanning logic with mock client
func TestEpicScannerScanWithMockClient(t *testing.T) {
	// Create mock client
	mockClient := newMockBeadsClient()

	// Add an epic bead
	epicBead := types.Bead{
		ID:     "hydra-31h",
		Type:   "epic",
		Status: types.StatusOpen,
		Labels: []string{"prd:phase-0", "type:epic"},
	}
	mockClient.beads["hydra-31h"] = epicBead

	// Add task beads with req-001 to req-003 (should map to Phase 0 epic)
	for i := 1; i <= 3; i++ {
		beadID := fmt.Sprintf("hydra-task-%d", i)
		mockClient.beads[beadID] = types.Bead{
			ID:     types.BeadID(beadID),
			Type:   "task",
			Status: types.StatusOpen,
			Labels: []string{fmt.Sprintf("req-%03d", i), "implementation"},
		}
	}

	// Create scanner
	scanner := NewEpicScanner(5*time.Minute, mockClient)
	scanner.SetMockBeads(mockClient.beads)

	// Run scan
	scanner.scanAndCloseEpics()

	// Epic should NOT be closed because tasks are still open
	if len(mockClient.closeCalls) > 0 {
		t.Errorf("Epic should not be closed when tasks are open, got %d close calls", len(mockClient.closeCalls))
	}
}

// TestEpicScannerClosesEpicWhenAllTasksComplete verifies epic closes when all children done
func TestEpicScannerClosesEpicWhenAllTasksComplete(t *testing.T) {
	// Create mock client
	mockClient := newMockBeadsClient()

	// Add an epic bead
	epicBead := types.Bead{
		ID:     "hydra-31h",
		Type:   "epic",
		Status: types.StatusOpen,
		Labels: []string{"prd:phase-0", "type:epic"},
	}
	mockClient.beads["hydra-31h"] = epicBead

	// Add COMPLETED task beads with req-001 to req-003
	for i := 1; i <= 3; i++ {
		beadID := fmt.Sprintf("hydra-task-%d", i)
		mockClient.beads[beadID] = types.Bead{
			ID:     types.BeadID(beadID),
			Type:   "task",
			Status: types.StatusCompleted,
			Labels: []string{fmt.Sprintf("req-%03d", i), "implementation"},
		}
	}

	// Create scanner
	scanner := NewEpicScanner(5*time.Minute, mockClient)
	scanner.SetMockBeads(mockClient.beads)

	// Run scan
	scanner.scanAndCloseEpics()

	// Epic SHOULD be closed because all tasks are completed
	found := false
	for _, call := range scanner.GetCloseCalls() {
		if call.beadID.String() == "hydra-31h" {
			found = true
			if call.reason != "all dependencies resolved" {
				t.Errorf("Close reason = %q, want %q", call.reason, "all dependencies resolved")
			}
			break
		}
	}

	if !found {
		t.Error("Epic should be closed when all tasks are completed")
	}
}

// TestEpicScannerStartStop verifies scanner can be started and stopped
func TestEpicScannerStartStop(t *testing.T) {
	mockClient := newMockBeadsClient()
	scanner := NewEpicScanner(100*time.Millisecond, mockClient)

	// Start scanner
	scanner.Start()

	// Let it run for a bit
	time.Sleep(150 * time.Millisecond)

	// Stop scanner
	scanner.Stop()

	// Verify it stopped cleanly (no panic)
	select {
	case <-scanner.stopChan:
		// Good, channel is closed
	default:
		t.Error("Stop channel should be closed after Stop()")
	}
}
