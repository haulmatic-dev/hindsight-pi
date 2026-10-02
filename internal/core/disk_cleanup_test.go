package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiskCleanupGuard_BreachRunsOncePerCooldown(t *testing.T) {
	var calls []string
	g := &diskCleanupGuard{
		minInterval: time.Hour,
		exec: func(name string, args ...string) error {
			calls = append(calls, name)
			return nil
		},
	}
	g.maybeCleanup("/", 99, 85)
	g.maybeCleanup("/", 99, 85) // cooldown: skipped
	if len(calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(calls))
	}
}

func TestDiskCleanupGuard_BelowWatermarkNoop(t *testing.T) {
	calls := 0
	g := &diskCleanupGuard{minInterval: time.Hour, exec: func(_ string, args ...string) error { calls++; return nil }}
	g.maybeCleanup("/", 50, 85)
	if calls != 0 {
		t.Fatalf("expected 0 calls below watermark, got %d", calls)
	}
}

func TestDiskCleanupGuard_ExecErrorFailOpen(t *testing.T) {
	calls := 0
	g := &diskCleanupGuard{
		minInterval: time.Hour,
		exec: func(_ string, args ...string) error {
			calls++
			return errors.New("systemd not running")
		},
	}
	g.maybeCleanup("/", 99, 85)
	if calls != len(diskCleanupCommands) {
		t.Fatalf("expected %d attempts, got %d", len(diskCleanupCommands), calls)
	}
}

func TestDiskCleanupGuard_NilReceiverInert(t *testing.T) {
	var g *diskCleanupGuard
	g.maybeCleanup("/", 99, 85) // must not panic
}

func TestDiskCleanupGuard_CooldownExpiryRunsAgain(t *testing.T) {
	calls := 0
	g := &diskCleanupGuard{minInterval: time.Hour, exec: func(_ string, args ...string) error { calls++; return nil }}
	g.maybeCleanup("/", 99, 85)
	g.mu.Lock()
	g.lastCleanup = time.Now().Add(-2 * time.Hour)
	g.mu.Unlock()
	g.maybeCleanup("/", 99, 85)
	if calls != 2 {
		t.Fatalf("expected 2 calls after cooldown expiry, got %d", calls)
	}
}

func TestDiskCleanupGuard_EventEmitted(t *testing.T) {
	dir := t.TempDir()
	sink := NewEventSink(dir)
	g := &diskCleanupGuard{minInterval: time.Hour, events: sink, exec: func(_ string, args ...string) error { return nil }}
	g.maybeCleanup("/", 99, 85)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no event files: %v", err)
	}
	evs, err := ReadEvents(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	found := false
	for _, e := range evs {
		if e.Type == "disk_cleanup_run" && strings.Contains(e.Detail, "99%") {
			found = true
		}
	}
	if !found {
		t.Fatalf("disk_cleanup_run event not found")
	}
}
