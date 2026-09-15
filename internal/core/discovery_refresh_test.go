package core

// Dynamic multi-user re-discovery (2026-09-15, fix #2): a user editing
// ~/.hydra/config.yaml — e.g. pointing a multi project at
// ~/projects/source-2-pay — must onboard without a daemon restart, and
// repos added under a multi dir must start scheduling as they appear.

import (
	"os"
	"path/filepath"
	"testing"
)

// writeDiscoveryUserConfig creates a fake home layout: <home>/<user>/.hydra/config.yaml
// plus a projects dir. Returns the user's projects dir.
func writeDiscoveryUserConfig(t *testing.T, home, user, configYAML string) string {
	t.Helper()
	projDir := filepath.Join(home, user, "projects")
	if err := os.MkdirAll(filepath.Join(home, user, ".hydra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, user, ".hydra", "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return projDir
}

// A user config that appears AFTER the overseer started must be picked up by
// refreshDiscoveredProjects without a restart, and a repo added under the
// multi dir later must attach to the existing project in place.
func TestRefreshDiscoveredProjectsOnboardsDynamically(t *testing.T) {
	home := t.TempDir()
	// Repo with a beads store, discovered via the multi scan.
	// Layout: ~/projects/source-2-pay/ (multi root) containing the repos.
	multiRoot := filepath.Join(home, "newuser", "projects", "source-2-pay")
	mustBeadsDB(t, filepath.Join(multiRoot, "source-2-pay-api"))
	projDir := writeDiscoveryUserConfig(t, home, "newuser", `
projects:
  - name: source-2-pay
    path: ~/projects/source-2-pay
    type: multi
`)

	o := &Overseer{
		config:           DefaultConfig(),
		projects:         nil,
		discoveryHomeDir: home,
	}
	o.config.MultiUserDiscovery = true
	o.beadAnalyzer = NewBeadAnalyzer(nil)
	o.beadFileReader = NewBeadFileReader(nil)

	o.refreshDiscoveredProjects()

	if len(o.projects) != 1 {
		t.Fatalf("projects after refresh = %d, want 1 (onboarded dynamically)", len(o.projects))
	}
	p := o.projects[0]
	if p.Name != "source-2-pay" {
		t.Errorf("project name = %q, want source-2-pay", p.Name)
	}
	if len(p.Repos) != 1 || p.Repos[0].Name != "source-2-pay-api" {
		t.Errorf("repos = %+v, want source-2-pay-api discovered under the multi dir", p.Repos)
	}

	// A second repo appears under the same multi dir: refresh must attach it
	// to the EXISTING project in place (no duplicate project).
	mustBeadsDB(t, filepath.Join(projDir, "source-2-pay", "source-2-pay-web"))
	o.refreshDiscoveredProjects()

	if len(o.projects) != 1 {
		t.Fatalf("projects after second refresh = %d, want 1 (in-place repo refresh)", len(o.projects))
	}
	if got := len(o.projects[0].Repos); got != 2 {
		t.Errorf("repos after second refresh = %d, want 2", got)
	}

	// Refresh is idempotent: a third run adds nothing.
	o.refreshDiscoveredProjects()
	if len(o.projects) != 1 || len(o.projects[0].Repos) != 2 {
		t.Errorf("refresh not idempotent: %+v", o.projects)
	}
}

// Multi-user discovery disabled: refresh is a no-op.
func TestRefreshDiscoveredProjectsDisabled(t *testing.T) {
	o := &Overseer{config: DefaultConfig(), discoveryHomeDir: t.TempDir()}
	o.config.MultiUserDiscovery = false
	o.refreshDiscoveredProjects() // must not panic, must not add
	if len(o.projects) != 0 {
		t.Errorf("projects = %d, want 0 with discovery disabled", len(o.projects))
	}
}

// mustBeadsDB creates <dir>/.beads/beads.db — discovery's repo criterion.
func mustBeadsDB(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "beads.db"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}
