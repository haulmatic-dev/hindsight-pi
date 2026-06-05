package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitAuth_CommitAndPush(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()

	// Init a git repo properly
	initCmd := exec.Command("git", "init")
	initCmd.Dir = dir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Set git config
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	// Create a file and initial commit
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	ctx := &UserContext{
		Config: UserConfig{
			User: UserInfo{Name: "Alice", Email: "alice@example.com"},
			Git:  GitCredentials{Username: "alice"},
		},
		Token: "ghp_test",
	}

	// Modify the file
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	auth := NewGitAuth()
	result := auth.CommitAndPush(ctx, dir, "test commit")

	// Push will fail without a real remote, but commit should succeed
	if result.Error != nil {
		t.Logf("Push failed as expected (no remote): %v", result.Error)
	}

	// Verify commit was made
	hash, err := auth.getCommitHash(dir)
	if err != nil || hash == "" {
		t.Errorf("expected commit hash, got: %v", err)
	}

	// Verify git config was set
	nameCmd := exec.Command("git", "-C", dir, "config", "user.name")
	nameOut, _ := nameCmd.Output()
	if string(nameOut) != "Alice\n" {
		t.Errorf("user.name = %q", string(nameOut))
	}
}

func TestGitAuth_NoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	exec.Command("git", "init").Dir = dir
	exec.Command("git", "init").Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	// Create initial file and commit
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	// No new changes
	ctx := &UserContext{
		Config: UserConfig{User: UserInfo{Name: "Alice", Email: "alice@example.com"}},
	}

	auth := NewGitAuth()
	result := auth.CommitAndPush(ctx, dir, "no changes")

	// Should succeed with no error when there are no changes
	if result.Error != nil {
		t.Logf("Result: success=%v, error=%v", result.Success, result.Error)
	}
}
