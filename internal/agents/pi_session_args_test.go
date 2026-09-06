package agents

import (
	"log"
	"testing"
)

// TestPiConnector_RpcArgs_NamedSession verifies the cache-hardening fix: the
// RPC path must run pi under a named per-bead session (--session-id
// hydra-<bead>) instead of --no-session, so the transcript is resumable and
// prompt-cache prefixes stay warm across respawns (2026-09-06).
func TestPiConnector_RpcArgs_NamedSession(t *testing.T) {
	c := NewPiConnector(PiConfig{Provider: "kimi-coding", Model: "kimi-for-coding"}, log.Default())

	args := c.rpcArgs("hydra-srv-x-1")

	if len(args) == 0 || args[0] != "--offline" {
		t.Fatalf("expected --offline first, got %v", args)
	}
	found := false
	for i, a := range args {
		if a == "--session-id" && i+1 < len(args) && args[i+1] == "hydra-srv-x-1" {
			found = true
		}
		if a == "--no-session" {
			t.Fatalf("--no-session must be gone once sessions are named: %v", args)
		}
	}
	if !found {
		t.Errorf("args missing --session-id hydra-srv-x-1: %v", args)
	}
}
