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
