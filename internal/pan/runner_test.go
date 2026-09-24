package pan

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestRunPiDisablesExtensions pins the turn-economy contract: Pan judgments
// must invoke pi with --no-extensions. Workers measured 48,315 vs 12,059
// tokens/call (full interactive load vs allowlist); Pan needs zero extensions
// (self-contained prompt, JSON out) and was paying the full load every tick.
func TestRunPiDisablesExtensions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "pi-stub")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argsFile + "\"\necho '[]'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYDRA_PI_PATH", stub)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := RunPi(ctx, Config{Provider: "kimi-router", Model: "glm-5.3-flash"}, "prompt"); err != nil {
		t.Fatalf("RunPi: %v", err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("stub did not record args: %v", err)
	}
	found := false
	for _, a := range splitLines(string(got)) {
		if a == "--no-extensions" {
			found = true
		}
	}
	if !found {
		t.Errorf("pi invoked without --no-extensions; args:\n%s", got)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
