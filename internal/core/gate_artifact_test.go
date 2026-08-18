package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The artifact is the source of truth for gate outcomes: written atomically
// BEFORE any LLM involvement, so no LLM timeout/crash can lose verification
// state, and the close path consumes it with zero LLM calls. Raw logs live
// outside the project repo (the srv-smart-engine test-log pollution pattern).

func TestGateArtifact_RoundTrip(t *testing.T) {
	root := t.TempDir()
	a := GateArtifact{
		Project:   "proj",
		BeadID:    "proj-abc1",
		Gate:      "test",
		Status:    GateStatusFailed,
		ExitCode:  1,
		Summary:   "FAIL src/x.spec.ts\ncoverage 72.5% < threshold 80%",
		RawLog:    filepath.Join(root, "gates", "proj", "proj-abc1", "test.log"),
		PID:       1234,
		StartedAt: time.Now().Add(-time.Minute),
		EndedAt:   time.Now(),
	}
	if err := WriteGateArtifact(root, a); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadGateArtifact(root, "proj", "proj-abc1", "test")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Status != GateStatusFailed || got.ExitCode != 1 || got.Summary != a.Summary {
		t.Errorf("round trip mismatch: %+v", got)
	}
	// raw log path must be under the gates root, not the project repo
	if !strings.HasPrefix(got.RawLog, filepath.Join(root, "gates")) {
		t.Errorf("raw log outside artifact root: %s", got.RawLog)
	}
}

func TestGateArtifact_FinalizeStaleRunning(t *testing.T) {
	root := t.TempDir()
	// A running artifact with an impossible PID (daemon crashed mid-gate).
	a := GateArtifact{
		Project: "proj", BeadID: "proj-abc2", Gate: "lint",
		Status: GateStatusRunning, PID: 99999999, StartedAt: time.Now().Add(-time.Hour),
	}
	if err := WriteGateArtifact(root, a); err != nil {
		t.Fatalf("write: %v", err)
	}
	finalized, err := FinalizeStaleGateArtifacts(root)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized != 1 {
		t.Errorf("finalized = %d, want 1", finalized)
	}
	got, _ := LoadGateArtifact(root, "proj", "proj-abc2", "lint")
	if got.Status != GateStatusInterrupted {
		t.Errorf("status = %s, want interrupted", got.Status)
	}
}

func TestGateArtifact_FinalizeKeepsLivePID(t *testing.T) {
	root := t.TempDir()
	a := GateArtifact{
		Project: "proj", BeadID: "proj-abc3", Gate: "build",
		Status: GateStatusRunning, PID: os.Getpid(), StartedAt: time.Now(),
	}
	if err := WriteGateArtifact(root, a); err != nil {
		t.Fatalf("write: %v", err)
	}
	finalized, err := FinalizeStaleGateArtifacts(root)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized != 0 {
		t.Errorf("finalized = %d, want 0 (live PID must not be interrupted)", finalized)
	}
}
