package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBeadClaimsDefaultPlanVerifyRunner exercises the default runner
// implementation (triggered by newBeadClaimChecker when the runner is nil).
// This is the actual production execution path: real shell commands are run
// to validate the runner's handling of tooling failures vs. claim evidence.
func TestBeadClaimsDefaultPlanVerifyRunner(t *testing.T) {
	if !availableShellCheck(t) {
		t.Skip("sh is unavailable on this platform")
	}
	if err := os.Chmod(t.TempDir(), 0o700); err != nil {
		t.Errorf("setup temp dir: %v", err)
	}
	file := filepath.Join(t.TempDir(), "marker.txt")
	if err := os.WriteFile(file, []byte("seeded"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	t.Run("non-zero exit is claim evidence (not tooling)", func(t *testing.T) {
		out, err := defaultClaimRun("exit 3", time.Second)
		if err == nil {
			t.Fatalf("expected non-nil error for exit 3")
		}
		if isClaimToolingFailure(err) {
			t.Errorf("isClaimToolingFailure(exit 3) = true, want false: %v", err)
		}
		_ = out
	})

	t.Run("timeout is classified tooling failure", func(t *testing.T) {
		_, err := defaultClaimRun("sleep 5", 300*time.Millisecond)
		if err == nil {
			t.Fatalf("expected timeout error")
		}
		if !isClaimToolingFailure(err) {
			t.Errorf("isClaimToolingFailure(timeout) = false, want true: %v", err)
		}
	})

	t.Run("stdout is captured and trimmed", func(t *testing.T) {
		got, err := defaultClaimRun("echo 'hello world'", time.Second)
		if err != nil {
			t.Fatalf("echo failed: %v", err)
		}
		if got != "hello world" {
			t.Errorf("output = %q, want %q", got, "hello world")
		}
	})

	t.Run("stderr rides back in error path", func(t *testing.T) {
		_, err := defaultClaimRun("echo 'to stderr' >&2; exit 1", time.Second)
		if err == nil {
			t.Fatalf("expected error from stderr+exit 1")
		}
		if isClaimToolingFailure(err) {
			t.Errorf("isClaimToolingFailure(stderr+exit 1) = true, want false: %v", err)
		}
	})

	t.Run("removed file tooling failure", func(t *testing.T) {
		_ = os.Remove(file)
		_, err := defaultClaimRun("cat "+file, time.Second)
		if err == nil {
			t.Fatalf("expected error from missing marker")
		}
		if isClaimToolingFailure(err) {
			t.Errorf("isClaimToolingFailure(missing marker) = true, want false: %v", err)
		}
	})

	_ = context.Background
}

// availableShellCheck guards tests against environments where sh is not on PATH.
func availableShellCheck(t *testing.T) bool {
	t.Helper()
	if _, err := defaultClaimRun("true", time.Second); err != nil {
		if isClaimToolingFailure(err) {
			return false
		}
	}
	return true
}
