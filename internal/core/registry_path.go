package core

import (
	"os"
	"path/filepath"
)

// registryPath resolves the daemon-owned worker registry database. With
// HYDRA_HOME set (production daemon) it lives beside the rest of the runtime
// state; otherwise it falls back to the working directory, matching
// WorkerTracker's behavior for dev runs and tests.
func registryPath() string {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = "."
	}
	dir := filepath.Join(base, ".hydra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Fail-open: the caller logs the Open error and runs without a registry.
		return filepath.Join(base, "registry.db")
	}
	return filepath.Join(dir, "registry.db")
}

// RegistryPath returns the resolved path to the daemon-owned worker registry
// database. It mirrors registryPath() for use by subcommands and tools outside
// this package.
func RegistryPath() string {
	return registryPath()
}

// triageStatePath resolves the PR-triage rate-limit state file (one triage
// comment per PR per 24h; survives daemon restarts). Same runtime-state
// discipline as registryPath: HYDRA_HOME/.hydra, never inside a checkout.
func triageStatePath() string {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = "."
	}
	return filepath.Join(base, ".hydra", "pr-triage-state.json")
}

// TriageStatePath returns the resolved PR-triage rate-limit state file path.
// Exported mirror of triageStatePath for subcommands.
func TriageStatePath() string {
	return triageStatePath()
}
