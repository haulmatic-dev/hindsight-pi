package core

// spawn_preflight_store_ownership_test.go — tests for the foreign-uid store
// spawn preflight (bug hydra-6g4u: daemon uid 999 writes .beads stores inside
// user-owned repos; br's fsqlite sidecars are uid-locked, so cross-uid access
// fails). The daemon stops managing user-owned repos by refusing to spawn
// into them. daemonUID is a test seam (production: os.Geteuid()).

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestSpawnPreflightStoreOwnership_ForeignOwnerSuppressed(t *testing.T) {
	oldUID := daemonUID
	daemonUID = func() int { return 999 }
	defer func() { daemonUID = oldUID }()

	// Test tmp dirs are owned by the test's own uid — which differs from the
	// stubbed daemon uid 999 (unless the test itself runs as uid 999, in which
	// case skip the mismatch test: the guard correctly passes).
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, ".beads"))
	owner := int(st.Sys().(*syscall.Stat_t).Uid)
	if owner == 999 {
		t.Skip("test process runs as uid 999; cannot create a foreign-owned store")
	}

	o := &Overseer{}
	err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, dir)
	if err == nil {
		t.Fatal("want SpawnSuppressedError for foreign-uid store, got nil")
	}
	if _, ok := err.(*SpawnSuppressedError); !ok {
		t.Fatalf("want *SpawnSuppressedError, got %T", err)
	}
}

func TestSpawnPreflightStoreOwnership_MissingStorePasses(t *testing.T) {
	o := &Overseer{}
	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, t.TempDir()); err != nil {
		t.Fatalf("missing .beads store must fail open, got %v", err)
	}
	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, ""); err != nil {
		t.Fatalf("empty projectPath must pass, got %v", err)
	}
}

func TestSpawnPreflightStoreOwnership_MatchingOwnerPasses(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, ".beads"))
	owner := int(st.Sys().(*syscall.Stat_t).Uid)
	oldUID := daemonUID
	daemonUID = func() int { return owner }
	defer func() { daemonUID = oldUID }()

	o := &Overseer{}
	if err := o.spawnPreflightStoreOwnership(types.Bead{ID: types.NewNumericBeadID("proj", "repo", 1)}, dir); err != nil {
		t.Fatalf("matching-uid store must pass, got %v", err)
	}
}
