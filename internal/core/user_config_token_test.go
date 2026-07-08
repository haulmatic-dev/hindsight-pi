package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadUserConfig_NoToken_LoadsForLocalWork verifies a user config with no
// git token still loads (previously any token-resolve failure skipped the
// entire user, silently disabling all their projects).
func TestLoadUserConfig_NoToken_LoadsForLocalWork(t *testing.T) {
	home := t.TempDir()
	writeUserConfig(t, home, `user:
  name: Test User
  email: t@example.com
projects:
  - name: proj
    path: ~/projects/proj
    type: single
`)

	ctx, err := LoadUserConfig(home)
	if err != nil {
		t.Fatalf("expected user to load without a token, got: %v", err)
	}
	if ctx.Token != "" {
		t.Fatalf("expected empty token, got %q", ctx.Token)
	}
	if len(ctx.Config.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(ctx.Config.Projects))
	}
}

// TestLoadUserConfig_UnreadableToken_FailsSoft verifies a token_file that
// cannot be read (missing / permission denied) is non-fatal: the user still
// loads for local work with an empty token.
func TestLoadUserConfig_UnreadableToken_FailsSoft(t *testing.T) {
	home := t.TempDir()
	writeUserConfig(t, home, `git:
  provider: github
  token_file: ~/.hydra/.missing-token
projects:
  - name: proj
    path: ~/projects/proj
    type: single
`)

	ctx, err := LoadUserConfig(home)
	if err != nil {
		t.Fatalf("expected fail-soft on unreadable token, got: %v", err)
	}
	if ctx.Token != "" {
		t.Fatalf("expected empty token, got %q", ctx.Token)
	}
}

func writeUserConfig(t *testing.T, home, body string) {
	t.Helper()
	hydraDir := filepath.Join(home, ".hydra")
	if err := os.MkdirAll(hydraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hydraDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
