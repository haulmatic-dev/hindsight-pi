package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestMultiUserDiscovery_Integration(t *testing.T) {
	// Build a fake /home directory
	baseDir := t.TempDir()

	// ── User: alice ─────────────────────────────────────────
	aliceHome := filepath.Join(baseDir, "alice")
	aliceHydra := filepath.Join(aliceHome, ".hydra")
	os.MkdirAll(aliceHydra, 0755)

	aliceConfig := `
user:
  name: Alice Smith
  email: alice@example.com
git:
  provider: github
  username: alice
  token: ghp_alice_secret_123
projects:
  - name: task-service
    path: ` + filepath.Join(aliceHome, "projects", "task-service") + `
    type: single
  - name: platform
    path: ` + filepath.Join(aliceHome, "projects", "platform") + `
    type: multi
`
	os.WriteFile(filepath.Join(aliceHydra, "config.yaml"), []byte(aliceConfig), 0644)

	// Alice's single-repo project
	os.MkdirAll(filepath.Join(aliceHome, "projects", "task-service", ".beads"), 0755)
	os.WriteFile(filepath.Join(aliceHome, "projects", "task-service", ".beads", "issues.jsonl"), []byte("{}"), 0644)

	// Alice's multi-repo project (auto-discover: frontend + backend)
	alicePlatform := filepath.Join(aliceHome, "projects", "platform")
	os.MkdirAll(filepath.Join(alicePlatform, "frontend", ".beads"), 0755)
	os.MkdirAll(filepath.Join(alicePlatform, "backend", ".beads"), 0755)
	os.WriteFile(filepath.Join(alicePlatform, "frontend", ".beads", "issues.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(alicePlatform, "backend", ".beads", "issues.jsonl"), []byte("{}"), 0644)

	// ── User: bob ──────────────────────────────────────────
	bobHome := filepath.Join(baseDir, "bob")
	bobHydra := filepath.Join(bobHome, ".hydra")
	os.MkdirAll(bobHydra, 0755)

	bobConfig := `
user:
  name: Bob Jones
  email: bob@example.com
git:
  provider: github
  username: bob
  token: ghp_bob_secret_456
projects:
  - name: api-gateway
    path: ` + filepath.Join(bobHome, "projects", "api-gateway") + `
    type: single
`
	os.WriteFile(filepath.Join(bobHydra, "config.yaml"), []byte(bobConfig), 0644)

	os.MkdirAll(filepath.Join(bobHome, "projects", "api-gateway", ".beads"), 0755)
	os.WriteFile(filepath.Join(bobHome, "projects", "api-gateway", ".beads", "issues.jsonl"), []byte("{}"), 0644)

	// ── Noise: system user ─────────────────────────────────
	os.MkdirAll(filepath.Join(baseDir, "_deploy"), 0755)
	os.WriteFile(filepath.Join(baseDir, "_deploy", "noise.txt"), []byte("ignore me"), 0644)

	// ── Run discovery ──────────────────────────────────────
	d := NewMultiUserDiscovery()
	d.HomeDir = baseDir

	if err := d.Discover(); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// ── Assertions ─────────────────────────────────────────
	if len(d.Users()) != 2 {
		t.Errorf("expected 2 users, got %d", len(d.Users()))
	}
	if len(d.Projects()) != 3 {
		t.Errorf("expected 3 projects, got %d", len(d.Projects()))
	}

	// Check alice
	alice := d.GetUser("alice")
	if alice == nil {
		t.Fatal("alice not found")
	}
	if alice.Config.User.Name != "Alice Smith" {
		t.Errorf("alice name = %q", alice.Config.User.Name)
	}
	if alice.Token != "ghp_alice_secret_123" {
		t.Errorf("alice token = %q", alice.Token)
	}
	if alice.Config.Git.Username != "alice" {
		t.Errorf("alice git username = %q", alice.Config.Git.Username)
	}

	// Check bob
	bob := d.GetUser("bob")
	if bob == nil {
		t.Fatal("bob not found")
	}
	if bob.Config.User.Email != "bob@example.com" {
		t.Errorf("bob email = %q", bob.Config.User.Email)
	}

	// Check project list
	projectNames := make(map[string]string) // name -> owner
	for _, p := range d.Projects() {
		projectNames[p.Name] = ""
	}
	if _, ok := projectNames["task-service"]; !ok {
		t.Error("missing task-service project")
	}
	if _, ok := projectNames["api-gateway"]; !ok {
		t.Error("missing api-gateway project")
	}
	if _, ok := projectNames["platform"]; !ok {
		t.Error("missing platform project")
	}

	// Check alice's multi-repo auto-discovery
	var platformProject *types.Project
	for _, p := range d.Projects() {
		if p.Name == "platform" {
			platformProject = &p
			break
		}
	}
	if platformProject == nil {
		t.Fatal("platform project not found")
	}
	if platformProject.Type != "multi" {
		t.Errorf("platform type = %q, want multi", platformProject.Type)
	}
	if len(platformProject.Repos) != 2 {
		t.Fatalf("expected 2 repos in platform, got %d", len(platformProject.Repos))
	}
	repoNames := make(map[string]bool)
	for _, r := range platformProject.Repos {
		repoNames[r.Name] = true
	}
	if !repoNames["frontend"] || !repoNames["backend"] {
		t.Errorf("platform repos = %v", platformProject.Repos)
	}

	// Check alice's single-repo project
	var taskProject *types.Project
	for _, p := range d.Projects() {
		if p.Name == "task-service" {
			taskProject = &p
			break
		}
	}
	if taskProject == nil {
		t.Fatal("task-service project not found")
	}
	if taskProject.Type != "single" {
		t.Errorf("task-service type = %q, want single", taskProject.Type)
	}
	if len(taskProject.Repos) != 0 {
		t.Errorf("task-service repos = %v, want empty", taskProject.Repos)
	}
}
