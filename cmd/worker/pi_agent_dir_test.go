package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func mkFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestResolvePiAgentDir_VPSLayout is the regression that matters: packages live
// in the config dir itself, and the old code appended "agent" and landed on an
// empty directory. Pi then reported "No packages installed", so every worker ran
// with no rtk, no context-mode, no caveman — in text mode and rpc mode alike.
func TestResolvePiAgentDir_VPSLayout(t *testing.T) {
	cfg := t.TempDir()
	mkFile(t, filepath.Join(cfg, "settings.json"))
	if err := os.MkdirAll(filepath.Join(cfg, "npm", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	// An empty agent/ subdir exists, exactly as on the VPS — its presence must
	// not win, because it holds no package set.
	if err := os.MkdirAll(filepath.Join(cfg, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolvePiAgentDir(cfg)
	want := absOrSelf(cfg)
	if got != want {
		t.Errorf("resolvePiAgentDir = %q, want %q (the dir that actually holds settings.json/npm)", got, want)
	}
}

// TestResolvePiAgentDir_DeveloperLayout: on a developer machine the package set
// lives under agent/ (~/.pi/agent/...). That layout must keep working unchanged.
func TestResolvePiAgentDir_DeveloperLayout(t *testing.T) {
	cfg := t.TempDir()
	mkFile(t, filepath.Join(cfg, "agent", "settings.json"))

	got := resolvePiAgentDir(cfg)
	want := absOrSelf(filepath.Join(cfg, "agent"))
	if got != want {
		t.Errorf("resolvePiAgentDir = %q, want %q", got, want)
	}
}

// TestResolvePiAgentDir_PrefersAgentWhenBothValid pins the probe order:
// agent/ is checked first, so a machine with both keeps its historical value.
func TestResolvePiAgentDir_PrefersAgentWhenBothValid(t *testing.T) {
	cfg := t.TempDir()
	mkFile(t, filepath.Join(cfg, "settings.json"))
	mkFile(t, filepath.Join(cfg, "agent", "settings.json"))

	got := resolvePiAgentDir(cfg)
	want := absOrSelf(filepath.Join(cfg, "agent"))
	if got != want {
		t.Errorf("resolvePiAgentDir = %q, want %q (agent/ is probed first)", got, want)
	}
}

// TestResolvePiAgentDir_NpmOnlyCountsAsPackageRoot: a dir with installed packages
// but no settings.json is still a package root.
func TestResolvePiAgentDir_NpmOnlyCountsAsPackageRoot(t *testing.T) {
	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "npm", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := resolvePiAgentDir(cfg), absOrSelf(cfg); got != want {
		t.Errorf("resolvePiAgentDir = %q, want %q", got, want)
	}
}

// TestResolvePiAgentDir_UnknownLayoutKeepsOldBehaviour: when neither candidate
// holds a package set, fall back to the historical <cfg>/agent so this cannot
// change behaviour in setups it does not understand.
func TestResolvePiAgentDir_UnknownLayoutKeepsOldBehaviour(t *testing.T) {
	cfg := t.TempDir()
	got := resolvePiAgentDir(cfg)
	want := absOrSelf(filepath.Join(cfg, "agent"))
	if got != want {
		t.Errorf("resolvePiAgentDir = %q, want %q", got, want)
	}
}

func TestResolvePiAgentDir_EmptyConfigDir(t *testing.T) {
	if got := resolvePiAgentDir(""); got != "" {
		t.Errorf("resolvePiAgentDir(\"\") = %q, want \"\" so no env var is exported", got)
	}
}

// TestBuildPiCommand_ExportsResolvedAgentDir checks the value actually reaches
// Pi's environment, since that is the only thing that matters.
func TestBuildPiCommand_ExportsResolvedAgentDir(t *testing.T) {
	cfg := t.TempDir()
	mkFile(t, filepath.Join(cfg, "settings.json"))
	if err := os.MkdirAll(filepath.Join(cfg, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := buildPiCommand(types.Bead{ID: types.BeadID("b1"), Title: "t", Type: "task"}, PiConfig{ConfigDir: cfg, Provider: "p", Model: "m"}, t.TempDir(), nil)

	want := "PI_CODING_AGENT_DIR=" + absOrSelf(cfg)
	var found bool
	for _, e := range cmd.Env {
		if e == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q in Pi's env; got PI_CODING_AGENT_DIR entries: %v", want, grepEnv(cmd.Env, "PI_CODING_AGENT_DIR"))
	}
}

func grepEnv(env []string, prefix string) []string {
	var out []string
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			out = append(out, e)
		}
	}
	return out
}
