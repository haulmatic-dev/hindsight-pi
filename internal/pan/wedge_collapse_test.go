package pan

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// The prompt's repair-name list and the parser whitelist must never drift
// apart: repair-bead-store sat whitelisted but unlisted in the prompt for
// weeks, so Pan could never choose it (2026-09-13 webapp store corruption
// #3 had to be repaired by hand).
func TestPromptListsEveryWhitelistedRepair(t *testing.T) {
	prompt := BuildPrompt(nil)
	for name := range repairWhitelist {
		if !strings.Contains(prompt, name) {
			t.Errorf("prompt does not list whitelisted repair %q", name)
		}
	}
}

// >=3 BLOCKED candidates with the same normalized reason collapse into one
// wedge candidate; smaller groups and mixed reasons pass through.
func TestCollapseWedges(t *testing.T) {
	mk := func(id, reason string) Candidate {
		return Candidate{Record: registry.WorkerRecord{BeadID: id}, Reason: reason}
	}
	in := []Candidate{
		mk("webapp-a1", "BLOCKED: spawn failed: preflight pnpm install"),
		mk("webapp-b2", "BLOCKED: spawn failed: preflight pnpm install"),
		mk("webapp-c3", "BLOCKED: spawn failed: preflight pnpm install"),
		mk("webapp-d4", "BLOCKED: spawn failed: preflight pnpm install"),
		mk("srv-x9", "BLOCKED: retry exhausted after 3 attempts: timeout"),
		mk("hydra-z1", "no heartbeat for 10m (threshold 5m)"),
	}
	out := collapseWedges(in)

	var wedges, passthrough int
	for _, c := range out {
		if strings.Contains(c.Reason, "PROJECT-WEDGE") {
			wedges++
			if !strings.Contains(c.Reason, "4 beads blocked") {
				t.Errorf("wedge reason missing count: %q", c.Reason)
			}
			if c.Record.BeadID != "webapp-a1" {
				t.Errorf("wedge keeps first bead ID, got %q", c.Record.BeadID)
			}
		} else {
			passthrough++
		}
	}
	if wedges != 1 {
		t.Errorf("wedges = %d, want 1", wedges)
	}
	if passthrough != 2 {
		t.Errorf("passthrough = %d, want 2 (srv-x9 below threshold + non-BLOCKED)", passthrough)
	}
}

// Project-scoped repairs rate-limit per project (not per bead), so one
// shared-wedge repair can clear a fleet-wide wedge without waiting an hour
// per bead.
func TestProjectScopedRepairRateLimit(t *testing.T) {
	s := New(Config{}, nil, nil)
	s.RepairHandlers = map[string]func(string) error{
		"prune-worktree-registration": func(string) error { return nil },
	}
	s.ProjectScope = func(string) string { return "/repo/webapp" }

	var events []string
	s.OnRepairEvent = func(_, _, result, _ string) { events = append(events, result) }

	s.handleRepair("webapp-a1", "prune-worktree-registration", "")
	s.handleRepair("webapp-b2", "prune-worktree-registration", "") // same project → limited
	s.handleRepair("webapp-a1", "reset-retries", "")               // per-bead action → allowed

	if len(events) != 3 {
		t.Fatalf("events = %v, want 3", events)
	}
	if events[0] != "ok" {
		t.Errorf("first repair = %q, want ok", events[0])
	}
	if events[1] != "error" {
		t.Errorf("second repair (same project) = %q, want error (rate limited)", events[1])
	}
	if events[2] != "error" {
		t.Errorf("third repair = %q, want error (no handler configured)", events[2])
	}

	// Backdating the project key past the 30m window unblocks the next repair.
	s.repairMu.Lock()
	for k := range s.repairLimits {
		if strings.Contains(k, "prune-worktree-registration") {
			s.repairLimits[k] = time.Now().Add(-31 * time.Minute)
		}
	}
	s.repairMu.Unlock()
	s.handleRepair("webapp-c3", "prune-worktree-registration", "")
	if events[3] != "ok" {
		t.Errorf("post-window repair = %q, want ok", events[3])
	}
}
