package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDiscover_BlockedOnUnreadableConfig verifies that a user whose config
// exists but is unreadable (permission denied) is recorded as blocked rather
// than silently skipped. Uses a 0000-permission config file to simulate the
// traverse/permission gap without needing multiple OS users.
func TestDiscover_BlockedOnUnreadableConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions; cannot simulate EACCES")
	}

	base := t.TempDir()

	// Reachable user: valid config.
	okDir := filepath.Join(base, "okuser", ".hydra")
	if err := os.MkdirAll(okDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(okDir, "config.yaml"), []byte("projects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Blocked user: config exists but is unreadable.
	badDir := filepath.Join(base, "baduser", ".hydra")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badCfg := filepath.Join(badDir, "config.yaml")
	if err := os.WriteFile(badCfg, []byte("projects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(badCfg, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badCfg, 0o644) })

	d := NewMultiUserDiscovery()
	d.HomeDir = base
	if err := d.Discover(); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if _, ok := d.Users()["okuser"]; !ok {
		t.Error("okuser should have been discovered")
	}
	blocked := d.Blocked()
	if len(blocked) != 1 || blocked[0] != "baduser" {
		t.Errorf("expected blocked == [baduser], got %v", blocked)
	}
	if _, ok := d.Users()["baduser"]; ok {
		t.Error("baduser must not be in Users() when its config is unreadable")
	}
}
