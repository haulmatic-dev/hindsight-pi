package core

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// The digest test builds a minimal Overseer (no NewOverseer side effects —
// IntelligenceHub/MCP init costs ~20s per test and pushed the internal/core
// package past the daemon gate's 15m `go test ./...` budget in bead 95ia).
func gateDeathOverseer(mock *mockBeadsClient) *Overseer {
	return &Overseer{
		projects:    []types.Project{{Name: "test-project", Path: "/test/test-project", Type: types.RepoTypeSingle}},
		beadsClient: mock,
	}
}

// TestCollectHealthDigest_GateDeathsAlert pins the digest visibility fix for
// ee5r/lkbo/ejsk (2026-10-04 15:45-16:17: three beads died at daemon gates
// and nobody was alerted): non-terminal gate-failed beads and recent
// closed-failed beads must surface as P0 campaign anomalies so Pan's judgment
// fires instead of the deaths sitting silent.
func TestCollectHealthDigest_GateDeathsAlert(t *testing.T) {
	mock := newMockBeadsClient()
	o := gateDeathOverseer(mock)

	old := time.Now().Add(-24 * time.Hour)
	recent := time.Now().Add(-10 * time.Minute)
	mock.beads["test-project-gaa"] = types.Bead{ID: "test-project-gaa", Project: "test-project", Status: types.StatusOpen, Type: "implement",
		Labels:    []string{"gate-failed", "gate-failed-test"},
		UpdatedAt: recent}
	// Already escalated: the escalation ladder owns it, digest must not
	// double-count.
	mock.beads["test-project-gbb"] = types.Bead{ID: "test-project-gbb", Project: "test-project", Status: types.StatusInProgress, Type: "implement",
		Labels:    []string{"gate-failed", "escalated", "needs-human"},
		UpdatedAt: recent}
	// Recent closed-failed death: terminal, invisible to every other counter.
	mock.beads["test-project-gcc"] = types.Bead{ID: "test-project-gcc", Project: "test-project", Status: types.StatusClosed, Type: "implement",
		Labels:    []string{"closed-failed"},
		UpdatedAt: recent}
	// Old closed-failed death: outside the window, no longer news.
	mock.beads["test-project-gdd"] = types.Bead{ID: "test-project-gdd", Project: "test-project", Status: types.StatusClosed, Type: "implement",
		Labels:    []string{"closed-failed"},
		UpdatedAt: old}

	d := o.collectHealthDigest()
	if len(d.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(d.Projects))
	}
	ph := d.Projects[0]
	if ph.GateFailedOpen != 1 {
		t.Errorf("GateFailedOpen = %d, want 1", ph.GateFailedOpen)
	}
	if ph.ClosedFailedRecent != 1 {
		t.Errorf("ClosedFailedRecent = %d, want 1", ph.ClosedFailedRecent)
	}
	joined := strings.Join(ph.Anomalies, "; ")
	if !strings.Contains(joined, "gate-failed with no escalation") {
		t.Errorf("missing gate-failed anomaly, got %v", ph.Anomalies)
	}
	if !strings.Contains(joined, "closed-failed in the last 4h") {
		t.Errorf("missing closed-failed anomaly, got %v", ph.Anomalies)
	}
	// The digest text carries both fields so a stable-digest diff sees them.
	text := d.Text()
	if !strings.Contains(text, "gatefailed=1") || !strings.Contains(text, "closedfail=1") {
		t.Errorf("digest text missing gate death fields: %q", text)
	}
}
