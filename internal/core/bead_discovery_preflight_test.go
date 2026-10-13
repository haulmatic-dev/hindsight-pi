package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestFindFromBR_PreflightBlocksMissingIssuesJSONL verifies the startup
// preflight guard: when .beads/issues.jsonl is missing but .br_history has
// snapshots, the project is refused scheduling and the store guard blocks it.
// This is the exact signature of the srv-smart-engine 2026-09-11 corruption.
func TestFindFromBR_PreflightBlocksMissingIssuesJSONL(t *testing.T) {
	mock := newMockBeadsClient()
	projectDir := t.TempDir()
	beadsDir := filepath.Join(projectDir, ".beads")
	histDir := filepath.Join(beadsDir, ".br_history")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		t.Fatalf("mkdir history: %v", err)
	}
	// Any snapshot file is enough for the preflight to treat the store as corrupt.
	snapshot := filepath.Join(histDir, "issues.2026-09-11T091150.jsonl")
	if err := os.WriteFile(snapshot, []byte(`{}`), 0644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	projects := []types.Project{{Name: "proj", Path: projectDir, Type: types.RepoTypeSingle}}
	var mu sync.RWMutex
	guard := NewBeadStoreGuard(func(path string) (int, error) { return 0, nil })
	d := NewBeadDiscovery(mock, nil, projects, map[string]bool{}, nil, &mu)
	d.SetStoreGuard(guard)

	beads, err := d.findFromBR(map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(beads) != 0 {
		t.Fatalf("expected no beads from a corrupt store, got %d", len(beads))
	}
	if !guard.IsBlocked(projectDir) {
		t.Fatal("expected project blocked by preflight missing-issues.jsonl guard")
	}
}
