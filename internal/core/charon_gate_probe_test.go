package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBeadCloseInProgress(t *testing.T) {
	root := t.TempDir()
	j := &Charon{gateRoot: root}

	// No artifacts at all -> not in progress.
	if j.beadCloseInProgress("b1") {
		t.Fatal("no artifacts must not report in-progress")
	}

	// Running artifact, live PID (ours), fresh start -> in progress.
	dir := filepath.Join(root, "gates", "proj", "b2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteGateArtifact(root, GateArtifact{
		Project: "proj", BeadID: "b2", Gate: "test", Status: GateStatusRunning,
		PID: os.Getpid(), StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if !j.beadCloseInProgress("b2") {
		t.Fatal("running artifact with live PID must report in-progress")
	}

	// Passed artifact -> not in progress.
	if err := WriteGateArtifact(root, GateArtifact{
		Project: "proj", BeadID: "b2", Gate: "test", Status: GateStatusPassed,
		PID: os.Getpid(), StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if j.beadCloseInProgress("b2") {
		t.Fatal("passed artifact must not report in-progress")
	}

	// Running but stale (>45m) -> not in progress (recovery may proceed).
	if err := WriteGateArtifact(root, GateArtifact{
		Project: "proj", BeadID: "b3", Gate: "test", Status: GateStatusRunning,
		PID: os.Getpid(), StartedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if j.beadCloseInProgress("b3") {
		t.Fatal("stale running artifact must not block recovery")
	}

	// Running with a dead PID -> not in progress.
	if err := WriteGateArtifact(root, GateArtifact{
		Project: "proj", BeadID: "b4", Gate: "test", Status: GateStatusRunning,
		PID: 99999999, StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if j.beadCloseInProgress("b4") {
		t.Fatal("dead-PID artifact must not block recovery")
	}

	// Probe disabled (no root) -> never blocks.
	j2 := &Charon{}
	if j2.beadCloseInProgress("b2") {
		t.Fatal("empty gateRoot must disable the probe")
	}
}
