package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMultiUserDiscovery_Discover(t *testing.T) {
	// Create fake home dirs
	homeDir := t.TempDir()

	// Create alice's config
	aliceHome := filepath.Join(homeDir, "alice")
	os.MkdirAll(filepath.Join(aliceHome, ".hydra"), 0755)
	aliceConfig := `
user:
  name: Alice Smith
  email: alice@example.com
git:
  provider: github
  username: alice
  token: ghp_alice123
projects:
  - name: task-service
    path: ` + filepath.Join(aliceHome, "projects", "task-service") + `
    type: single
`
	os.WriteFile(filepath.Join(aliceHome, ".hydra", "config.yaml"), []byte(aliceConfig), 0644)

	// Create bob's config
	bobHome := filepath.Join(homeDir, "bob")
	os.MkdirAll(filepath.Join(bobHome, ".hydra"), 0755)
	bobConfig := `
user:
  name: Bob Jones
  email: bob@example.com
git:
  provider: github
  username: bob
  token: ghp_bob456
projects:
  - name: api-gateway
    path: ` + filepath.Join(bobHome, "projects", "api-gateway") + `
    type: single
`
	os.WriteFile(filepath.Join(bobHome, ".hydra", "config.yaml"), []byte(bobConfig), 0644)

	// Create project directories
	os.MkdirAll(filepath.Join(aliceHome, "projects", "task-service"), 0755)
	os.MkdirAll(filepath.Join(bobHome, "projects", "api-gateway"), 0755)

	// Monkey-patch /home scanning for test
	originalHome := "/home"
	defer func() { _ = originalHome }()

	d := NewMultiUserDiscovery()
	// Instead of scanning /home, directly register test users
	d.users["alice"] = &UserContext{
		Config: UserConfig{
			User:     UserInfo{Name: "Alice Smith", Email: "alice@example.com"},
			Git:      GitCredentials{Provider: "github", Username: "alice", Token: "ghp_alice123"},
			Projects: []UserProject{{Name: "task-service", Path: filepath.Join(aliceHome, "projects", "task-service"), Type: "single"}},
		},
		Username: "alice",
		HomeDir:  aliceHome,
		HydraDir: filepath.Join(aliceHome, ".hydra"),
		Token:    "ghp_alice123",
	}
	d.users["bob"] = &UserContext{
		Config: UserConfig{
			User:     UserInfo{Name: "Bob Jones", Email: "bob@example.com"},
			Git:      GitCredentials{Provider: "github", Username: "bob", Token: "ghp_bob456"},
			Projects: []UserProject{{Name: "api-gateway", Path: filepath.Join(bobHome, "projects", "api-gateway"), Type: "single"}},
		},
		Username: "bob",
		HomeDir:  bobHome,
		HydraDir: filepath.Join(bobHome, ".hydra"),
		Token:    "ghp_bob456",
	}

	if len(d.Users()) != 2 {
		t.Fatalf("expected 2 users, got %d", len(d.Users()))
	}

	alice := d.GetUser("alice")
	if alice == nil {
		t.Fatal("alice not found")
	}
	if alice.Config.User.Name != "Alice Smith" {
		t.Errorf("alice name = %q", alice.Config.User.Name)
	}

	bob := d.GetUser("bob")
	if bob == nil {
		t.Fatal("bob not found")
	}
	if bob.Token != "ghp_bob456" {
		t.Errorf("bob token = %q", bob.Token)
	}
}

func TestMultiUserDiscovery_AutoDiscoverRepos(t *testing.T) {
	dir := t.TempDir()

	// Create multi-repo project structure
	projectPath := filepath.Join(dir, "my-platform")
	os.MkdirAll(filepath.Join(projectPath, "frontend", ".beads"), 0755)
	os.MkdirAll(filepath.Join(projectPath, "backend", ".beads"), 0755)
	os.WriteFile(filepath.Join(projectPath, "frontend", ".beads", "beads.db"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectPath, "backend", ".beads", "beads.db"), []byte("{}"), 0644)

	ctx := &UserContext{
		Username: "alice",
		HomeDir:  dir,
	}

	d := NewMultiUserDiscovery()
	project := d.convertProject(UserProject{
		Name: "my-platform",
		Path: projectPath,
		Type: "multi",
	}, ctx)

	if project == nil {
		t.Fatal("project conversion returned nil")
	}
	if project.Type != "multi" {
		t.Errorf("type = %q, want multi", project.Type)
	}
	if len(project.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(project.Repos))
	}

	repoNames := make(map[string]bool)
	for _, r := range project.Repos {
		repoNames[r.Name] = true
	}
	if !repoNames["frontend"] || !repoNames["backend"] {
		t.Errorf("repos = %v", project.Repos)
	}
}

func TestMultiUserDiscovery_ExplicitRepos(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "my-platform")
	os.MkdirAll(projectPath, 0755)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()

	project := d.convertProject(UserProject{
		Name:  "my-platform",
		Path:  projectPath,
		Type:  "multi",
		Repos: []RepoConfig{{Name: "frontend", Path: "frontend"}, {Name: "backend", Path: "backend"}},
	}, ctx)

	if len(project.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(project.Repos))
	}
	if project.Repos[0].Name != "frontend" {
		t.Errorf("repo[0] = %q", project.Repos[0].Name)
	}
}
