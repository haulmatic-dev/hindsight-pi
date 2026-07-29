package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestComplianceCycleManager_areAllBeadsClosed(t *testing.T) {
	tests := []struct {
		name       string
		beads      map[string]types.Bead
		wantClosed bool
		wantOpen   int
	}{
		{
			name: "all closed",
			beads: map[string]types.Bead{
				"b1": {ID: "b1", Project: "test", Status: types.StatusClosed},
				"b2": {ID: "b2", Project: "test", Status: types.StatusClosed},
			},
			wantClosed: true,
			wantOpen:   0,
		},
		{
			name: "one open",
			beads: map[string]types.Bead{
				"b1": {ID: "b1", Project: "test", Status: types.StatusClosed},
				"b2": {ID: "b2", Project: "test", Status: types.StatusOpen},
			},
			wantClosed: false,
			wantOpen:   1,
		},
		{
			name: "all open",
			beads: map[string]types.Bead{
				"b1": {ID: "b1", Project: "test", Status: types.StatusOpen},
			},
			wantClosed: false,
			wantOpen:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockBeadsClient()
			for k, v := range tt.beads {
				client.beads[k] = v
			}

			overseer := NewOverseer(DefaultConfig())
			overseer.beadsClient = client
			overseer.projects = []types.Project{{Name: "test", Path: "/tmp/test"}}

			cm := NewComplianceCycleManager(overseer, ComplianceCycleConfig{
				Enabled:      true,
				MaxCycles:    5,
				PollInterval: 1 * time.Second,
			})

			closed, open, err := cm.areAllBeadsClosed()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if closed != tt.wantClosed {
				t.Errorf("closed = %v, want %v", closed, tt.wantClosed)
			}
			if open != tt.wantOpen {
				t.Errorf("open = %d, want %d", open, tt.wantOpen)
			}
		})
	}
}

func TestComplianceCycleManager_hasGaps(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with gaps
	gapReport := map[string]interface{}{
		"passed": false,
		"gaps": []map[string]string{
			{"id": "gap-1", "category": "route"},
		},
	}
	gapPath := filepath.Join(tmpDir, "gaps.json")
	data, _ := json.Marshal(gapReport)
	os.WriteFile(gapPath, data, 0644)

	cm := &ComplianceCycleManager{config: ComplianceCycleConfig{ProjectDir: tmpDir}}
	hasGaps, err := cm.hasGaps(gapPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasGaps {
		t.Error("expected hasGaps = true")
	}

	// Test without gaps
	noGapReport := map[string]interface{}{"passed": true}
	noGapPath := filepath.Join(tmpDir, "no-gaps.json")
	data, _ = json.Marshal(noGapReport)
	os.WriteFile(noGapPath, data, 0644)

	hasGaps, err = cm.hasGaps(noGapPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasGaps {
		t.Error("expected hasGaps = false")
	}
}

func TestComplianceCycleManager_runGapToPRD(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a simple gap report
	gapReport := map[string]interface{}{
		"passed":   false,
		"prd_path": "/tmp/test-prd.md",
		"cycles":   0,
		"summary":  "1 gap found",
		"checks":   []map[string]interface{}{{"name": "verify-routes", "passed": false}},
		"gaps": []map[string]interface{}{
			{
				"id":             "route-get-status",
				"category":       "route",
				"severity":       "critical",
				"description":    "GET /status not wired",
				"target_files":   []string{"server.go"},
				"suggested_bead": "WIRE: GET /status",
			},
		},
	}
	gapPath := filepath.Join(tmpDir, "gap-report.json")
	data, _ := json.Marshal(gapReport)
	os.WriteFile(gapPath, data, 0644)

	// Create gaps directory (gap-to-prd writes there)
	os.MkdirAll(filepath.Join(tmpDir, "gaps"), 0755)

	// Build gap-to-prd
	gapToPRDBin := filepath.Join("..", "..", "..", "..", "outreach-workflow", "tools", "gap-to-prd", "gap-to-prd")
	if _, err := os.Stat(gapToPRDBin); os.IsNotExist(err) {
		gapToPRDBin = "/opt/outreach-workflow/tools/gap-to-prd/gap-to-prd"
	}

	cm := &ComplianceCycleManager{
		config: ComplianceCycleConfig{
			ProjectDir:   tmpDir,
			GapToPRDPath: gapToPRDBin,
		},
	}

	prdPath, err := cm.runGapToPRD(gapPath)
	if err != nil {
		t.Fatalf("runGapToPRD failed: %v", err)
	}

	content, err := os.ReadFile(prdPath)
	if err != nil {
		t.Fatalf("read gap PRD: %v", err)
	}

	if len(content) == 0 {
		t.Error("gap PRD is empty")
	}

	// Verify it contains the route gap
	contentStr := string(content)
	if !contains(contentStr, "WIRE: GET /status") {
		t.Error("gap PRD missing expected bead suggestion")
	}
	if !contains(contentStr, "## FR-001: Route") {
		t.Error("gap PRD missing Route FR section")
	}
}

func TestComplianceCycleManager_cycleExhaustion(t *testing.T) {
	client := newMockBeadsClient()
	overseer := NewOverseer(DefaultConfig())
	overseer.beadsClient = client
	overseer.projects = []types.Project{{Name: "test", Path: "."}}
	overseer.stateController.SetRunning(true)

	cm := NewComplianceCycleManager(overseer, ComplianceCycleConfig{
		Enabled:      true,
		MaxCycles:    2,
		PollInterval: 1 * time.Second,
	})
	cm.cycles = 2 // Already at max

	// Start should stop immediately due to max cycles
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cm.Start(ctx)

	if cm.cycles != 2 {
		t.Errorf("cycles = %d, want 2", cm.cycles)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
