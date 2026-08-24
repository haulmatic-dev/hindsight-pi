package core

import (
	"github.com/haulmatic-dev/hydra/pkg/types"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Preflight design (bead hydra-code-jwrh): after EnsureWorktree, before the
// worker spawns, the worktree must have resolvable dependencies. Missing
// node_modules or broken symlinks (e.g. @haulmatic/common -> ../../../lib-nestjs)
// burn a whole worker lifetime on npm install. Fail early, deterministically.

func writePkg(t *testing.T, wt string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wt, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightSkipsWhenSentinelFresh(t *testing.T) {
	wt := t.TempDir()
	writePkg(t, wt)
	m := &WorktreeManager{}
	called := 0
	restore := stubInstallRunner(&called, nil)
	defer restore()

	if err := m.Preflight(wt); err != nil {
		t.Fatalf("first preflight: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected one install, got %d", called)
	}
	if err := m.Preflight(wt); err != nil {
		t.Fatalf("second preflight: %v", err)
	}
	if called != 1 {
		t.Fatalf("sentinel ignored — install ran again (%d)", called)
	}
}

func TestPreflightInstallsWhenNodeModulesMissing(t *testing.T) {
	wt := t.TempDir()
	writePkg(t, wt)
	m := &WorktreeManager{}
	called := 0
	restore := stubInstallRunner(&called, nil)
	defer restore()

	if err := m.Preflight(wt); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if called != 1 {
		t.Fatalf("install not run for missing node_modules")
	}
}

func TestPreflightDetectsBrokenSymlinkAfterInstall(t *testing.T) {
	wt := t.TempDir()
	writePkg(t, wt)
	m := &WorktreeManager{}
	called := 0
	restore := stubInstallRunner(&called, func(wtPath string) {
		// installer "succeeds" but leaves a broken scoped symlink
		dir := filepath.Join(wtPath, "node_modules", "@haulmatic")
		os.MkdirAll(dir, 0o755)
		os.Symlink("../../../does-not-exist", filepath.Join(dir, "common"))
	})
	defer restore()

	err := m.Preflight(wt)
	if err == nil {
		t.Fatal("broken scoped symlink not detected")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error should name broken links: %v", err)
	}
}

func TestPreflightFailsClosedOnInstallError(t *testing.T) {
	wt := t.TempDir()
	writePkg(t, wt)
	m := &WorktreeManager{}
	called := 0
	restore := stubInstallRunner(&called, nil)
	defer restore()
	installRunner = func(ctx context.Context, wtPath, manager string) (string, error) {
		return "npm ERR! code EACCES\nnpm ERR! permission denied", errors.New("exit status 1")
	}

	err := m.Preflight(wt)
	if err == nil {
		t.Fatal("install failure must fail the preflight (fail-closed)")
	}
	if !strings.Contains(err.Error(), "EACCES") {
		t.Fatalf("error must quote installer stderr tail: %v", err)
	}
}

func TestPreflightNoopForNonNodeProject(t *testing.T) {
	wt := t.TempDir() // no package.json
	m := &WorktreeManager{}
	called := 0
	restore := stubInstallRunner(&called, nil)
	defer restore()
	if err := m.Preflight(wt); err != nil {
		t.Fatalf("non-node project must be a no-op: %v", err)
	}
	if called != 0 {
		t.Fatal("installer ran for non-node project")
	}
}

func stubInstallRunner(called *int, effect func(string)) func() {
	orig := installRunner
	installRunner = func(ctx context.Context, wtPath, manager string) (string, error) {
		*called++
		if effect != nil {
			effect(wtPath)
		} else {
			os.MkdirAll(filepath.Join(wtPath, "node_modules"), 0o755)
		}
		return "", nil
	}
	return func() { installRunner = orig }
}

// 1xs7z (2026-08-23, 51.6M tokens burned): a scheduler-path bead has an
// empty Project field; FindProjectPath matched nothing, the spawner fell
// back to the empty field, and git worktree add ran in the DAEMON'S CWD —
// creating a dev-tools checkout as the "srv-smart-engine" chain worktree.
// Three layers must hold: analyzer derives from the bead ID, spawner
// fallback uses it too, and EnsureWorktree refuses an empty project path.
func TestFindProjectPath_EmptyProjectDerivesFromID(t *testing.T) {
	ba := NewBeadAnalyzer([]types.Project{{Name: "srv-smart-engine", Path: "/srv", Type: types.RepoTypeSingle}})
	bead := &types.Bead{ID: "srv-smart-engine-1xs7z"} // Project empty, as from the scheduler
	if got := ba.FindProjectPath(bead); got != "/srv" {
		t.Errorf("FindProjectPath = %q, want /srv (derive project from bead ID)", got)
	}
}

func TestEnsureWorktree_EmptyPathRefused(t *testing.T) {
	m := NewWorktreeManager()
	if _, err := m.EnsureWorktree("", "proj-x1"); err == nil {
		t.Fatal("empty project path must fail closed — never worktree the daemon cwd")
	}
}
