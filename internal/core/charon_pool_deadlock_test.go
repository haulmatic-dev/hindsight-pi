package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// freePIDs returns n consecutive PIDs that do not exist in /proc.
func freePIDs(t *testing.T, n int) []int {
	t.Helper()
	pids := make([]int, 0, n)
	for pid := 4194000; pid < 4194300 && len(pids) < n; pid++ {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
			pids = append(pids, pid)
		}
	}
	if len(pids) < n {
		t.Skip("not enough free PID slots for test")
	}
	return pids
}

// writePlaceholderWorkers seeds the tracker with dead-PID placeholder records
// at a specific age. Distinct PIDs are required because the tracker indexes by
// PID, so two placeholders cannot share PID 0.
func writePlaceholderWorkers(t *testing.T, dir string, age time.Duration, count int) *WorkerTracker {
	t.Helper()
	pids := freePIDs(t, count)
	hydraDir := filepath.Join(dir, ".hydra")
	if err := os.MkdirAll(hydraDir, 0o755); err != nil {
		t.Fatalf("mkdir .hydra: %v", err)
	}
	started := time.Now().Add(-age)
	records := make([]*WorkerRecord, 0, count)
	for i := 0; i < count; i++ {
		records = append(records, &WorkerRecord{
			PID:         pids[i],
			PGID:        0,
			BeadID:      "phantom-" + string(rune('a'+i)),
			ProjectPath: dir,
			StartedAt:   started,
			Status:      "running",
		})
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatalf("marshal workers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hydraDir, "workers.json"), data, 0o644); err != nil {
		t.Fatalf("write workers.json: %v", err)
	}
	wt, err := NewWorkerTracker(dir)
	if err != nil {
		t.Fatalf("NewWorkerTracker: %v", err)
	}
	return wt
}

func TestCharon_PoolDeadlock_ReapsPhantomSlotsAfterTwoScans(t *testing.T) {
	dir := t.TempDir()
	wt := writePlaceholderWorkers(t, dir, 11*time.Minute, 2)

	eventsDir := t.TempDir()
	sink := NewEventSink(eventsDir)

	j := NewCharon(nil, wt, 0)
	j.SetMaxWorkers(2)
	j.SetEventSink(sink)

	// First scan: condition detected, but needs a second consecutive scan.
	j.checkAndReapPoolDeadlock()
	if running, _ := wt.GetStats(); running != 2 {
		t.Fatalf("after first scan expected 2 placeholders, got %d", running)
	}

	// Second scan: reaps both phantom slots.
	reaped := j.checkAndReapPoolDeadlock()
	if reaped != 2 {
		t.Fatalf("expected 2 phantom slots reaped, got %d", reaped)
	}

	running, _ := wt.GetStats()
	if running != 0 {
		t.Fatalf("expected 0 running after reap, got %d", running)
	}

	evs, err := ReadEvents(filepath.Join(eventsDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].Type != "charon_pool_deadlock" || evs[0].Reason != "phantom_slots" {
		t.Fatalf("unexpected event: %+v", evs[0])
	}
}

func TestCharon_PoolDeadlock_NoReapWhenLiveProcess(t *testing.T) {
	dir := t.TempDir()
	wt := writePlaceholderWorkers(t, dir, 11*time.Minute, 2)

	// Start a real process so there is a live PID tracked alongside the phantoms.
	proc := exec.Command("sleep", "120")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer proc.Process.Kill()

	if err := wt.TrackWorkerWithPGID(proc.Process.Pid, proc.Process.Pid, "live-bead", dir); err != nil {
		t.Fatalf("TrackWorkerWithPGID: %v", err)
	}

	j := NewCharon(nil, wt, 0)
	j.SetMaxWorkers(2)

	// Condition must not fire while a live process exists.
	for i := 0; i < 3; i++ {
		if got := j.checkAndReapPoolDeadlock(); got != 0 {
			t.Fatalf("scan %d: expected no reap with live PID, got %d", i+1, got)
		}
	}

	running, _ := wt.GetStats()
	if running != 3 {
		t.Fatalf("expected 3 running records, got %d", running)
	}
}

func TestCharon_PoolDeadlock_NoReapWhenYoungPlaceholder(t *testing.T) {
	dir := t.TempDir()
	// Young placeholders are protected by the 10-minute double-spawn guard.
	wt := writePlaceholderWorkers(t, dir, 30*time.Second, 2)

	j := NewCharon(nil, wt, 0)
	j.SetMaxWorkers(2)

	for i := 0; i < 3; i++ {
		if got := j.checkAndReapPoolDeadlock(); got != 0 {
			t.Fatalf("scan %d: expected no reap for young placeholders, got %d", i+1, got)
		}
	}

	running, _ := wt.GetStats()
	if running != 2 {
		t.Fatalf("expected 2 young placeholders still running, got %d", running)
	}
}
