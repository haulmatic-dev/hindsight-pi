package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestShouldSpawnWorker_SkipsEpics verifies that shouldSpawnWorker returns false for epic-type beads
func TestShouldSpawnWorker_SkipsEpics(t *testing.T) {
	// Create a mock DAG
	dag := NewDAG()

	// Create an epic bead
	epicBead := types.Bead{
		ID:     "hydra-test-epic",
		Type:   "epic",
		Status: types.StatusOpen,
	}

	// Add to DAG
	dag.AddNode(epicBead)

	// Get the node
	node, exists := dag.GetNode("hydra-test-epic")
	if !exists {
		t.Fatal("Node should exist in DAG")
	}

	// Create overseer
	o := &Overseer{
		dag:           dag,
		activeWorkers: make(map[string]*types.Worker),
	}

	// Test: shouldSpawnWorker should return false for epic
	result := o.shouldSpawnWorker(node)
	if result {
		t.Errorf("shouldSpawnWorker should return false for epic bead, got true")
	}
}

// TestShouldSpawnWorker_AllowsTasks verifies that shouldSpawnWorker returns true for task-type beads
func TestShouldSpawnWorker_AllowsTasks(t *testing.T) {
	// Create a mock DAG
	dag := NewDAG()

	// Create a task bead
	taskBead := types.Bead{
		ID:     "hydra-test-task",
		Type:   "task",
		Status: types.StatusOpen,
	}

	// Add to DAG
	dag.AddNode(taskBead)

	// Get the node
	node, exists := dag.GetNode("hydra-test-task")
	if !exists {
		t.Fatal("Node should exist in DAG")
	}

	// Create overseer
	o := &Overseer{
		dag:           dag,
		activeWorkers: make(map[string]*types.Worker),
	}

	// Test: shouldSpawnWorker should return true for task
	result := o.shouldSpawnWorker(node)
	if !result {
		t.Errorf("shouldSpawnWorker should return true for task bead, got false")
	}
}

// TestShouldSpawnWorker_SkipsRunningBeads verifies that shouldSpawnWorker returns false for already running beads
func TestShouldSpawnWorker_SkipsRunningBeads(t *testing.T) {
	// Create a mock DAG
	dag := NewDAG()

	// Create a task bead
	taskBead := types.Bead{
		ID:     "hydra-running-task",
		Type:   "task",
		Status: types.StatusOpen,
	}

	// Add to DAG
	dag.AddNode(taskBead)

	// Get the node
	node, exists := dag.GetNode("hydra-running-task")
	if !exists {
		t.Fatal("Node should exist in DAG")
	}

	// Create overseer with running worker
	o := &Overseer{
		dag:           dag,
		activeWorkers: make(map[string]*types.Worker),
	}
	o.activeWorkers["hydra-running-task"] = &types.Worker{
		ID:     "hydra-running-task",
		Status: "running",
	}

	// Test: shouldSpawnWorker should return false for running bead
	result := o.shouldSpawnWorker(node)
	if result {
		t.Errorf("shouldSpawnWorker should return false for running bead, got true")
	}
}

// TestShouldSpawnWorker_ExistingFilters verifies that existing filters still work
func TestShouldSpawnWorker_ExistingFilters(t *testing.T) {
	tests := []struct {
		name     string
		beadType string
		status   types.BeadStatus
		want     bool
	}{
		{
			name:     "skips approval-gate",
			beadType: "approval-gate",
			status:   types.StatusOpen,
			want:     false,
		},
		{
			name:     "skips approval in progress",
			beadType: "approval",
			status:   types.StatusInProgress,
			want:     false,
		},
		{
			name:     "allows approval open",
			beadType: "approval",
			status:   types.StatusOpen,
			want:     true,
		},
		{
			name:     "allows regular task",
			beadType: "task",
			status:   types.StatusOpen,
			want:     true,
		},
		{
			name:     "skips epic",
			beadType: "epic",
			status:   types.StatusOpen,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock DAG
			dag := NewDAG()

			// Create bead
			bead := types.Bead{
				ID:     "hydra-test-bead",
				Type:   tt.beadType,
				Status: tt.status,
			}

			// Add to DAG
			dag.AddNode(bead)

			// Get the node
			node, exists := dag.GetNode("hydra-test-bead")
			if !exists {
				t.Fatal("Node should exist in DAG")
			}

			// Create overseer
			o := &Overseer{
				dag:           dag,
				activeWorkers: make(map[string]*types.Worker),
			}

			// Test
			result := o.shouldSpawnWorker(node)
			if result != tt.want {
				t.Errorf("shouldSpawnWorker() = %v, want %v", result, tt.want)
			}
		})
	}
}
