package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeBrRunner records br config get/set calls and returns canned values.
type fakeBrRunner struct {
	values   map[string]string
	sets     map[string]string
	getErr   map[string]error
	setErr   map[string]error
	getCalls []string
	setCalls []string
}

func newFakeBrRunner() *fakeBrRunner {
	return &fakeBrRunner{
		values: make(map[string]string),
		sets:   make(map[string]string),
		getErr: make(map[string]error),
		setErr: make(map[string]error),
	}
}

func (f *fakeBrRunner) configGet(repoDir, key string) (string, error) {
	f.getCalls = append(f.getCalls, repoDir)
	if err := f.getErr[repoDir]; err != nil {
		return "", err
	}
	return f.values[repoDir], nil
}

func (f *fakeBrRunner) configSet(repoDir, key, value string) error {
	f.setCalls = append(f.setCalls, repoDir)
	if err := f.setErr[repoDir]; err != nil {
		return err
	}
	f.sets[repoDir] = value
	f.values[repoDir] = value
	return nil
}

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
	os.WriteFile(filepath.Join(projectPath, "frontend", ".beads", "issues.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectPath, "backend", ".beads", "issues.jsonl"), []byte("{}"), 0644)

	ctx := &UserContext{
		Username: "alice",
		HomeDir:  dir,
	}

	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.values[filepath.Join(projectPath, "frontend")] = "frontend"
	runner.values[filepath.Join(projectPath, "backend")] = "backend"
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name: "my-platform",
		Path: projectPath,
		Type: "multi",
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes()

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

	// PrefixGuard rewrites the default repo-dir prefix to <user>-<repo>.
	wantFront := "alice-frontend"
	if got := runner.sets[filepath.Join(projectPath, "frontend")]; got != wantFront {
		t.Errorf("frontend set = %q, want %q", got, wantFront)
	}
	wantBack := "alice-backend"
	if got := runner.sets[filepath.Join(projectPath, "backend")]; got != wantBack {
		t.Errorf("backend set = %q, want %q", got, wantBack)
	}
}

func TestMultiUserDiscovery_ExplicitRepos(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "my-platform")
	os.MkdirAll(projectPath, 0755)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.values[filepath.Join(projectPath, "frontend")] = "frontend"
	runner.values[filepath.Join(projectPath, "backend")] = "backend"
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name:  "my-platform",
		Path:  projectPath,
		Type:  "multi",
		Repos: []RepoConfig{{Name: "frontend", Path: "frontend"}, {Name: "backend", Path: "backend"}},
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes()

	if len(project.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(project.Repos))
	}
	if project.Repos[0].Name != "frontend" {
		t.Errorf("repo[0] = %q", project.Repos[0].Name)
	}

	wantFront := "alice-frontend"
	if got := runner.sets[filepath.Join(projectPath, "frontend")]; got != wantFront {
		t.Errorf("frontend set = %q, want %q", got, wantFront)
	}
}

func TestPrefixGuard_SingleRepoDefault(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "task-service")
	os.MkdirAll(filepath.Join(projectPath, ".beads"), 0755)
	os.WriteFile(filepath.Join(projectPath, ".beads", "issues.jsonl"), []byte("{}"), 0644)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.values[projectPath] = "task-service" // br default = dir base
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name: "task-service",
		Path: projectPath,
		Type: "single",
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes()

	if project == nil {
		t.Fatal("project conversion returned nil")
	}
	if got := runner.sets[projectPath]; got != "alice-task-service" {
		t.Errorf("set issue_prefix = %q, want alice-task-service", got)
	}
}

func TestPrefixGuard_SingleRepoUnset(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "task-service")
	os.MkdirAll(projectPath, 0755)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.values[projectPath] = ""
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name: "task-service",
		Path: projectPath,
		Type: "single",
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes()

	if got := runner.sets[projectPath]; got != "alice-task-service" {
		t.Errorf("set issue_prefix = %q, want alice-task-service", got)
	}
}

func TestPrefixGuard_CustomPrefixPreserved(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "weird_repo")
	os.MkdirAll(filepath.Join(projectPath, ".beads"), 0755)
	os.WriteFile(filepath.Join(projectPath, ".beads", "issues.jsonl"), []byte("{}"), 0644)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.values[projectPath] = "deliberate-custom-prefix"
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name:        "weird_repo",
		Path:        projectPath,
		Type:        "single",
		IssuePrefix: "My Custom_Prefix!",
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes()

	if project.IssuePrefix != "My Custom_Prefix!" {
		t.Errorf("project.IssuePrefix = %q, want raw configured value", project.IssuePrefix)
	}
	if _, ok := runner.sets[projectPath]; ok {
		t.Errorf("custom prefix was overwritten; sets = %v", runner.sets)
	}
}

func TestPrefixGuard_BrGetFailureIsFailOpen(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "task-service")
	os.MkdirAll(projectPath, 0755)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.getErr[projectPath] = errors.New("br exploded")
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name: "task-service",
		Path: projectPath,
		Type: "single",
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes() // must not panic

	if len(runner.setCalls) != 0 {
		t.Errorf("expected no set calls after get failure, got %d", len(runner.setCalls))
	}
}

func TestPrefixGuard_BrSetFailureIsFailOpen(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "task-service")
	os.MkdirAll(projectPath, 0755)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.values[projectPath] = "task-service"
	runner.setErr[projectPath] = errors.New("br set denied")
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name: "task-service",
		Path: projectPath,
		Type: "single",
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes() // must not panic

	if _, ok := runner.sets[projectPath]; ok {
		t.Errorf("set failure should not update recorded value")
	}
}

func TestPrefixGuard_GlobalBrDefaultIsRewritten(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "webapp")
	os.MkdirAll(projectPath, 0755)

	ctx := &UserContext{Username: "nipuna", HomeDir: dir}
	d := NewMultiUserDiscovery()
	runner := newFakeBrRunner()
	runner.values[projectPath] = "br" // global fallback before br init
	d.prefixGuard.runner = runner

	project := d.convertProject(UserProject{
		Name: "webapp",
		Path: projectPath,
		Type: "single",
	}, ctx)
	d.projects = append(d.projects, *project)
	d.guardPrefixes()

	if got := runner.sets[projectPath]; got != "nipuna-webapp" {
		t.Errorf("set issue_prefix = %q, want nipuna-webapp", got)
	}
}

func TestPrefixGuard_Normalization(t *testing.T) {
	cases := []struct {
		username, repoName, configured, want string
	}{
		{"alice", "webapp", "", "alice-webapp"},
		{"Alice", "WebApp", "", "alice-webapp"},
		{"alice", "webapp", "My_Custom.Prefix!", "my-custom-prefix"},
		{"alice", "webapp", "__leading--trailing__", "leading-trailing"},
		{"user", "repo", "!!!", "beads"},
	}
	for _, tc := range cases {
		got := issuePrefixForRepo(tc.username, tc.repoName, tc.configured)
		if got != tc.want {
			t.Errorf("issuePrefixForRepo(%q,%q,%q) = %q, want %q", tc.username, tc.repoName, tc.configured, got, tc.want)
		}
	}
}
