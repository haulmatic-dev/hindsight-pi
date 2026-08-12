package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// RED: pi's RPC stdout can stall while the agent loop stays healthy (chain
// walk srv-smart-engine-3a4: session events flowed until 75s before the
// kill, yet the watchdog reported 10m of silence). The session file is
// ground truth for liveness — a fresh mtime must prevent the timeout even
// when no stdout/stderr line arrived.
func TestActivityMonitorSessionDirLivenessPreventsTimeout(t *testing.T) {
	dir := t.TempDir()
	am := NewActivityMonitor(60 * time.Millisecond)
	am.SetSessionDir(dir)

	// Force lastActivity into the past so stdout-only logic would time out.
	am.mu.Lock()
	am.lastActivity = time.Now().Add(-time.Hour)
	am.mu.Unlock()

	// pi keeps persisting events: a file with a fresh mtime appears.
	sessionFile := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if am.IsTimedOut() {
		t.Fatal("timed out despite fresh session file — RPC-stall blindness still present")
	}
}

// A stale session dir (no writes) must NOT mask a real timeout.
func TestActivityMonitorStaleSessionDirStillTimesOut(t *testing.T) {
	dir := t.TempDir()
	am := NewActivityMonitor(60 * time.Millisecond)
	am.SetSessionDir(dir)

	stale := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(stale, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	am.mu.Lock()
	am.lastActivity = time.Now().Add(-time.Hour)
	am.mu.Unlock()

	if !am.IsTimedOut() {
		t.Fatal("stale session dir masked a real timeout")
	}
}
