package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestMultiUser_EndToEnd verifies the full pipeline from user config discovery
// through project discovery, bead loading, scheduling, and worker spawn attribution.
func TestMultiUser_EndToEnd(t *testing.T) {
	baseDir := t.TempDir()

	// ── Create user "alice" ─────────────────────────────────
	aliceHome := filepath.Join(baseDir, "alice")
	aliceHydra := filepath.Join(aliceHome, ".hydra")
	os.MkdirAll(aliceHydra, 0755)

	configYAML := `
user:
  name: Alice Smith
  email: alice@example.com
git:
  provider: github
  username: alice
  token: ghp_alice_secret
projects:
  - name: tiny-api
    path: ~/projects/tiny-api
    type: single
`
	os.WriteFile(filepath.Join(aliceHydra, "config.yaml"), []byte(configYAML), 0644)

	// ── Create tiny-api project ──────────────────────────────
	projectDir := filepath.Join(aliceHome, "projects", "tiny-api")
	os.MkdirAll(filepath.Join(projectDir, ".beads"), 0755)
	os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module tiny-api\n\ngo 1.21\n"), 0644)

	// ── Run multi-user discovery ─────────────────────────────
	discovery := NewMultiUserDiscovery()
	discovery.HomeDir = baseDir
	if err := discovery.Discover(); err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
	if len(discovery.Users()) != 1 {
		t.Fatalf("expected 1 user, got %d", len(discovery.Users()))
	}
	if len(discovery.Projects()) != 1 {
		t.Fatalf("expected 1 project, got %d", len(discovery.Projects()))
	}

	project := discovery.Projects()[0]
	if project.Path != projectDir {
		t.Fatalf("project path = %q, want %q", project.Path, projectDir)
	}

	// ── Set up overseer with recording spawner ───────────────
	config := DefaultConfig()
	config.Overseer.MaxWorkers = 5
	config.Overseer.BeadDiscoveryInterval = 100 * time.Millisecond
	config.Overseer.DefaultRetries = 1
	config.Overseer.WorkerTimeout = 10 * time.Second
	config.API.Enabled = false
	config.MCP.Enabled = false
	config.Intelligence.Graphify.Enabled = false
	config.Intelligence.GPTCache.Enabled = false
	config.ComplianceCycle.Enabled = false
	config.Overseer.ResourceMonitoring.MaxMemoryPercent = 100.0

	overseer := NewOverseer(config)

	// Inject discovered projects + user contexts
	overseer.projects = discovery.Projects()
	overseer.userContexts = discovery.Users()
	overseer.beadAnalyzer.SetProjects(overseer.projects)
	overseer.beadDiscovery.SetProjects(overseer.projects)
	overseer.workerSpawner.SetUserContexts(overseer.userContexts)

	// Set up mock beads with 3 open beads
	mockBeads := newMockBeadsClient()
	for _, b := range []types.Bead{
		{ID: types.BeadID("FR-001"), Title: "Add greeting", Type: "feature", Status: types.StatusOpen, Project: "tiny-api"},
		{ID: types.BeadID("FR-002"), Title: "Add health", Type: "feature", Status: types.StatusOpen, Project: "tiny-api"},
		{ID: types.BeadID("NFR-001"), Title: "Coverage", Type: "quality", Status: types.StatusOpen, Project: "tiny-api"},
	} {
		mockBeads.beads[string(b.ID)] = b
	}
	overseer.beadsClient = mockBeads
	overseer.beadDiscovery.beadsClient = mockBeads // ensure bead discovery uses mock too

	// Create a fake worker binary that sleeps briefly then exits 0
	// This gives us time to inspect activeWorkers before it completes
	fakeWorker := filepath.Join(baseDir, "fake-worker")
	os.WriteFile(fakeWorker, []byte("#!/bin/sh\nsleep 1\nexit 0\n"), 0755)
	os.Setenv("HYDRA_WORKER_BINARY", fakeWorker)
	defer os.Unsetenv("HYDRA_WORKER_BINARY")

	// ── Manually init (avoid serviceStarter overwriting our state) ─
	overseer.beadDiscovery.SetProjects(overseer.projects)
	overseer.beadFileReader = NewBeadFileReader(overseer.projects)
	if overseer.orphanJanitor != nil {
		overseer.orphanJanitor.Start()
		defer overseer.orphanJanitor.Stop()
	}
	overseer.stateController.SetRunning(true)

	// Skip chain registration (would require full service starter init)
	overseer.chainsRegistered = true

	// ── Build DAG ────────────────────────────────────────────
	if err := overseer.buildDAG(); err != nil {
		t.Fatalf("buildDAG failed: %v", err)
	}
	overseer.dagMu.RLock()
	readyNodes := overseer.dag.GetReadyNodes()
	overseer.dagMu.RUnlock()
	t.Logf("DAG ready nodes: %d", len(readyNodes))
	for _, n := range readyNodes {
		t.Logf("  ready: %s (status=%s)", n.Bead.ID, n.Bead.Status)
	}

	// ── Trigger scheduling ───────────────────────────────────
	if err := overseer.tick(); err != nil {
		t.Fatalf("tick failed: %v", err)
	}

	// Give scheduler time to spawn workers before fake worker completes
	time.Sleep(200 * time.Millisecond)

	// ── Verify workers were spawned ──────────────────────────
	overseer.workerMutex.RLock()
	activeCount := len(overseer.activeWorkers)
	overseer.workerMutex.RUnlock()

	if activeCount == 0 {
		t.Fatalf("expected workers to be spawned, got 0 (DAG ready=%d)", len(readyNodes))
	}

	// ── Verify worker attribution ────────────────────────────
	overseer.workerMutex.RLock()
	for id, worker := range overseer.activeWorkers {
		if worker.Username != "alice" {
			t.Errorf("worker %s: username = %q, want alice", id, worker.Username)
		}
		if worker.ProjectPath != projectDir {
			t.Errorf("worker %s: projectPath = %q, want %s", id, worker.ProjectPath, projectDir)
		}
		t.Logf("✅ worker %s: bead=%s user=%s project=%s", id, worker.BeadID, worker.Username, worker.ProjectPath)
	}
	overseer.workerMutex.RUnlock()

	t.Logf("✅ End-to-end test passed: %d workers spawned for alice's project", activeCount)
}
