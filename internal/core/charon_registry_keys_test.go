package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// 2026-09-16 webapp-zpf husk incident: a chain timeout unregistered the
// chain, the reconciler's in-memory protection set dropped the key, and the
// sweep deleted a live worker's worktree 8 minutes later. The registry's
// durable worktree_key must protect the sandbox independent of chain state.

func TestWorktreeActiveKeys_IncludesRegistryWorktreeKey(t *testing.T) {
	c, st := registryCharon(t)

	rec := registry.WorkerRecord{
		BeadID:        "webapp-9ah",
		PID:           1,
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-30 * time.Minute), // past spawn grace: only the registry leg can protect
		LastHeartbeat: time.Now(),
		WorktreeKey:   "webapp-zpf",
	}
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	active := c.worktreeActiveKeys()
	if !active["webapp-zpf"] {
		t.Fatalf("registry worktree key not protected: active=%v", active)
	}
	if !active["webapp-9ah"] {
		t.Fatalf("registry bead ID not protected: active=%v", active)
	}
}

func TestWorktreeActiveKeys_StaleHeartbeatRowDoesNotProtect(t *testing.T) {
	c, st := registryCharon(t)
	c.heartbeatHungAfter = 5 * time.Minute

	rec := registry.WorkerRecord{
		BeadID:        "webapp-9ah",
		PID:           1,
		State:         registry.StateWorking,
		StartedAt:     time.Now().Add(-time.Hour),
		LastHeartbeat: time.Now().Add(-30 * time.Minute), // stale: worker presumed dead
		WorktreeKey:   "webapp-zpf",
	}
	if err := st.UpsertWorker(rec); err != nil {
		t.Fatalf("UpsertWorker: %v", err)
	}

	active := c.worktreeActiveKeys()
	if active["webapp-zpf"] || active["webapp-9ah"] {
		t.Fatalf("stale row must not protect: active=%v", active)
	}
}
