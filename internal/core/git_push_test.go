package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushBranch(t *testing.T) {
	t.Parallel()

	t.Run("pushes local main to bare remote", func(t *testing.T) {
		t.Parallel()
		repo, remote := setupReachableRemote(t)
		commitFile(t, repo, "README.md", "hello")

		pushed, reason := PushBranch(repo, "origin", "main")
		if !pushed {
			t.Errorf("PushBranch(repo, \"origin\", \"main\") pushed = false; want true (reason: %q)", reason)
		}
		if reason != "" {
			t.Errorf("PushBranch(repo, \"origin\", \"main\") reason = %q; want %q", reason, "")
		}

		commit := headCommit(t, repo)
		if !remoteHas(t, remote, "main", commit) {
			t.Errorf("commit %s not found on remote main", commit)
		}
	})

	t.Run("empty remote returns not configured without invoking git", func(t *testing.T) {
		t.Parallel()
		repo := setupLocalRepo(t)

		pushed, reason := PushBranch(repo, "", "main")
		if pushed {
			t.Errorf("PushBranch(repo, \"\", \"main\") pushed = true; want false")
		}
		if reason != "not configured" {
			t.Errorf("PushBranch(repo, \"\", \"main\") reason = %q; want %q", reason, "not configured")
		}
	})

	t.Run("missing remote returns non-empty reason and does not panic", func(t *testing.T) {
		t.Parallel()
		repo := setupLocalRepo(t)
		nonexistent := filepath.Join(t.TempDir(), "nonexistent")
		git(t, repo, "remote", "add", "origin", nonexistent)

		pushed, reason := PushBranch(repo, "origin", "main")
		if pushed {
			t.Errorf("PushBranch(repo, \"origin\", \"main\") pushed = true; want false")
		}
		if reason == "" {
			t.Errorf("PushBranch(repo, \"origin\", \"main\") reason is empty; want non-empty reason")
		}
	})

	t.Run("pre-receive hook returns protected branch", func(t *testing.T) {
		t.Parallel()
		repo, remote := setupReachableRemote(t)
		writeHook(t, remote, "pre-receive", "#!/bin/sh\necho 'pre-receive hook declined' >&2\nexit 1\n")
		commitFile(t, repo, "file.txt", "content")

		pushed, reason := PushBranch(repo, "origin", "main")
		if pushed {
			t.Errorf("PushBranch(repo, \"origin\", \"main\") pushed = true; want false")
		}
		if reason != "protected branch" {
			t.Errorf("PushBranch(repo, \"origin\", \"main\") reason = %q; want %q", reason, "protected branch")
		}
	})
}

func setupReachableRemote(t *testing.T) (repo, remote string) {
	t.Helper()
	tmp := t.TempDir()
	remote = filepath.Join(tmp, "remote.git")
	repo = filepath.Join(tmp, "repo")

	initBareRemote(t, remote)
	initLocalRepo(t, repo)
	git(t, repo, "remote", "add", "origin", remote)
	return repo, remote
}

func setupLocalRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	initLocalRepo(t, repo)
	return repo
}

func initBareRemote(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	git(t, path, "init", "--bare")
	git(t, path, "symbolic-ref", "HEAD", "refs/heads/main")
}

func initLocalRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	git(t, path, "init")
	git(t, path, "config", "user.email", "test@example.com")
	git(t, path, "config", "user.name", "Test User")
	git(t, path, "checkout", "-b", "main")
}

func commitFile(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for file: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git(t, repo, "add", name)
	git(t, repo, "commit", "-m", "add "+name)
}

func writeHook(t *testing.T, remote, hook, script string) {
	t.Helper()
	path := filepath.Join(remote, "hooks", hook)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
}

func headCommit(t *testing.T, repo string) string {
	t.Helper()
	out := git(t, repo, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func remoteHas(t *testing.T, remote, branch, commit string) bool {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "--verify", "refs/heads/"+branch).CombinedOutput()
	if err != nil {
		return false
	}
	got := strings.TrimSpace(string(out))
	return strings.HasPrefix(got, commit) || strings.HasPrefix(commit, got)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func TestAuthenticatedURL(t *testing.T) {
	cases := []struct{ name, origin, token, want string }{
		{"ssh", "git@github.com:org/repo.git", "TOK", "https://x-access-token:TOK@github.com/org/repo.git"},
		{"https", "https://github.com/org/repo.git", "TOK", "https://x-access-token:TOK@github.com/org/repo.git"},
		{"https with userinfo", "https://old:cred@github.com/org/repo.git", "TOK", "https://x-access-token:TOK@github.com/org/repo.git"},
		{"gitlab nested", "git@gitlab.com:g/s/repo.git", "TOK", "https://x-access-token:TOK@gitlab.com/g/s/repo.git"},
		{"empty token", "git@github.com:org/repo.git", "", ""},
		{"empty origin", "", "TOK", ""},
		{"unknown scheme", "ftp://x/y", "TOK", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := authenticatedURL(c.origin, c.token); got != c.want {
				t.Errorf("authenticatedURL(%q,%q) = %q, want %q", c.origin, c.token, got, c.want)
			}
		})
	}
}

func TestRedactToken(t *testing.T) {
	if got := redactToken("fatal: auth for ghp_SECRET failed", "ghp_SECRET"); strings.Contains(got, "ghp_SECRET") {
		t.Errorf("token not redacted: %q", got)
	}
	if got := redactToken("no token here", ""); got != "no token here" {
		t.Errorf("empty token should passthrough, got %q", got)
	}
}

func TestPushBranchWithToken_EmptyBranchNotConfigured(t *testing.T) {
	if ok, reason := PushBranchWithToken(t.TempDir(), "", "TOK"); ok || reason != "not configured" {
		t.Errorf("empty branch: got (%v,%q), want (false,\"not configured\")", ok, reason)
	}
}

func TestPushBranchWithToken_NoRemote(t *testing.T) {
	repo := t.TempDir()
	// git init so it's a repo but has no origin remote
	exec.Command("git", "-C", repo, "init").Run()
	if ok, reason := PushBranchWithToken(repo, "main", "TOK"); ok || reason != "no remote" {
		t.Errorf("no origin: got (%v,%q), want (false,\"no remote\")", ok, reason)
	}
}
