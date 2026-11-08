package core

import (
	"os"
	"testing"
	"time"
)

// TestGateRunner_ArtifactRecordsChildPID: the running marker and the final
// artifact must carry the gate child's PID, not the daemon's. A daemon-PID
// marker stays "alive" while the daemon lives, so a stale running artifact
// (crash mid-gate before the final write) permanently fooled the
// close-in-progress probe and blocked orphan recovery — the root cause of
// the persisting orphanclaims=6 digest anomaly (hydra 2026-10-06).
func TestGateRunner_ArtifactRecordsChildPID(t *testing.T) {
	root := t.TempDir()
	r := NewGateRunner(root)
	a, err := r.RunGate(GateRequest{
		Project: "proj", BeadID: "proj-pid", Gate: "lint",
		Spec:        GateSpec{Cmd: "echo ok", Parser: "exit-code"},
		ProjectPath: t.TempDir(), WorktreePath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("RunGate: %v", err)
	}
	if a.PID == 0 {
		t.Fatal("artifact PID = 0, want the gate child PID")
	}
	if a.PID == os.Getpid() {
		t.Errorf("artifact PID = %d (daemon), want the gate child process", a.PID)
	}
	loaded, err := LoadGateArtifact(root, "proj", "proj-pid", "lint")
	if err != nil {
		t.Fatalf("LoadGateArtifact: %v", err)
	}
	if loaded.PID != a.PID {
		t.Errorf("persisted PID = %d, want %d", loaded.PID, a.PID)
	}
}

func TestGateCloseInProgress_RunningAliveArtifact(t *testing.T) {
	root := t.TempDir()
	a := GateArtifact{Project: "proj", BeadID: "proj-b1", Gate: "test", Status: GateStatusRunning, PID: os.Getpid(), StartedAt: time.Now()}
	if err := WriteGateArtifact(root, a); err != nil {
		t.Fatalf("WriteGateArtifact: %v", err)
	}
	if !GateCloseInProgress(root, "proj", "proj-b1") {
		t.Error("GateCloseInProgress = false, want true for running artifact with live PID")
	}
}

func TestGateCloseInProgress_TerminalArtifact(t *testing.T) {
	root := t.TempDir()
	a := GateArtifact{Project: "proj", BeadID: "proj-b2", Gate: "test", Status: GateStatusFailed, PID: os.Getpid(), StartedAt: time.Now(), EndedAt: time.Now()}
	if err := WriteGateArtifact(root, a); err != nil {
		t.Fatalf("WriteGateArtifact: %v", err)
	}
	if GateCloseInProgress(root, "proj", "proj-b2") {
		t.Error("GateCloseInProgress = true, want false for terminal artifact")
	}
}

func TestGateCloseInProgress_DeadPIDAndStaleStart(t *testing.T) {
	root := t.TempDir()
	dead := GateArtifact{Project: "proj", BeadID: "proj-b3", Gate: "test", Status: GateStatusRunning, PID: 0, StartedAt: time.Now()}
	if err := WriteGateArtifact(root, dead); err != nil {
		t.Fatalf("WriteGateArtifact: %v", err)
	}
	if GateCloseInProgress(root, "proj", "proj-b3") {
		t.Error("GateCloseInProgress = true, want false for dead PID")
	}
	stale := GateArtifact{Project: "proj", BeadID: "proj-b3", Gate: "lint", Status: GateStatusRunning, PID: os.Getpid(), StartedAt: time.Now().Add(-time.Hour)}
	if err := WriteGateArtifact(root, stale); err != nil {
		t.Fatalf("WriteGateArtifact: %v", err)
	}
	if GateCloseInProgress(root, "proj", "proj-b3") {
		t.Error("GateCloseInProgress = true, want false for artifact older than 45m")
	}
	if GateCloseInProgress("", "proj", "proj-b3") {
		t.Error("empty gate root must disable the probe")
	}
	if GateCloseInProgress(root, "proj", "proj-nope") {
		t.Error("unknown bead must not report close in progress")
	}
}
