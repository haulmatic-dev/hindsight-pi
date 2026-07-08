package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserConfig(t *testing.T) {
	dir := t.TempDir()

	// Create config.yaml inside .hydra dir
	hydraDir := filepath.Join(dir, ".hydra")
	os.MkdirAll(hydraDir, 0755)
	configYAML := `
user:
  name: Alice Smith
  email: alice@example.com
git:
  provider: github
  username: alice
  token_file: ~/.hydra/.github-token
projects:
  - name: task-service
    path: ~/projects/task-service
    type: single
`
	os.WriteFile(filepath.Join(hydraDir, "config.yaml"), []byte(configYAML), 0644)

	// Create token file
	os.WriteFile(filepath.Join(hydraDir, ".github-token"), []byte("ghp_testtoken123\n"), 0600)

	ctx, err := LoadUserConfig(dir)
	if err != nil {
		t.Fatalf("LoadUserConfig failed: %v", err)
	}

	if ctx.Username != filepath.Base(dir) {
		t.Errorf("username = %q, want %q", ctx.Username, filepath.Base(dir))
	}
	if ctx.Config.User.Name != "Alice Smith" {
		t.Errorf("user.name = %q", ctx.Config.User.Name)
	}
	if ctx.Config.Git.Username != "alice" {
		t.Errorf("git.username = %q", ctx.Config.Git.Username)
	}
	if ctx.Token != "ghp_testtoken123" {
		t.Errorf("token = %q", ctx.Token)
	}
	if len(ctx.Config.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(ctx.Config.Projects))
	}

	// Verify path expansion
	expectedPath := filepath.Join(dir, "projects", "task-service")
	if ctx.Config.Projects[0].Path != expectedPath {
		t.Errorf("project path = %q, want %q", ctx.Config.Projects[0].Path, expectedPath)
	}

	// Verify defaults
	if ctx.Config.Projects[0].PRDPath != "docs/PRDs/current.md" {
		t.Errorf("default prd_path = %q", ctx.Config.Projects[0].PRDPath)
	}
	if ctx.Config.Projects[0].Branch != "main" {
		t.Errorf("default branch = %q", ctx.Config.Projects[0].Branch)
	}
}

func TestLoadUserConfig_TokenEnv(t *testing.T) {
	dir := t.TempDir()

	hydraDir := filepath.Join(dir, ".hydra")
	os.MkdirAll(hydraDir, 0755)
	configYAML := `
user:
  name: Bob
git:
  provider: github
  username: bob
  token_env: TEST_GITHUB_TOKEN
projects: []
`
	os.WriteFile(filepath.Join(hydraDir, "config.yaml"), []byte(configYAML), 0644)
	os.Setenv("TEST_GITHUB_TOKEN", "ghp_envtoken456")
	defer os.Unsetenv("TEST_GITHUB_TOKEN")

	ctx, err := LoadUserConfig(dir)
	if err != nil {
		t.Fatalf("LoadUserConfig failed: %v", err)
	}
	if ctx.Token != "ghp_envtoken456" {
		t.Errorf("token from env = %q", ctx.Token)
	}
}

func TestLoadUserConfig_NoToken(t *testing.T) {
	dir := t.TempDir()

	hydraDir := filepath.Join(dir, ".hydra")
	os.MkdirAll(hydraDir, 0755)
	configYAML := `
user:
  name: Charlie
git:
  provider: github
  username: charlie
projects: []
`
	os.WriteFile(filepath.Join(hydraDir, "config.yaml"), []byte(configYAML), 0644)

	// A missing git token is non-fatal: the user loads for local work with an
	// empty token (remote git ops surface the failure at use-time). This
	// replaces the old hard-fail that silently skipped the entire user.
	ctx, err := LoadUserConfig(dir)
	if err != nil {
		t.Fatalf("expected fail-soft load with no token, got error: %v", err)
	}
	if ctx.Token != "" {
		t.Fatalf("expected empty token, got %q", ctx.Token)
	}
}

func TestGitAuthor(t *testing.T) {
	ctx := &UserContext{
		Config: UserConfig{
			User: UserInfo{Name: "Alice Smith", Email: "alice@example.com"},
			Git:  GitCredentials{Username: "alice"},
		},
	}
	name, email := ctx.GitAuthor()
	if name != "Alice Smith" {
		t.Errorf("name = %q", name)
	}
	if email != "alice@example.com" {
		t.Errorf("email = %q", email)
	}

	// Fallback to git username when user name is empty
	ctx2 := &UserContext{
		Config: UserConfig{
			Git: GitCredentials{Username: "bob"},
		},
	}
	name2, email2 := ctx2.GitAuthor()
	if name2 != "bob" {
		t.Errorf("fallback name = %q", name2)
	}
	if email2 == "" {
		t.Error("fallback email should not be empty")
	}
}

func TestGitRemoteURL(t *testing.T) {
	ctx := &UserContext{
		Config: UserConfig{Git: GitCredentials{Username: "alice"}},
		Token:  "ghp_xxx",
	}
	url := ctx.GitRemoteURL("https://github.com/alice/repo.git")
	if url != "https://alice:ghp_xxx@github.com/alice/repo.git" {
		t.Errorf("remote URL = %q", url)
	}
}
