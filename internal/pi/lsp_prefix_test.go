package pi

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setHydraHomeTemp points HYDRA_HOME at a fresh temp dir so prefix lookups
// cannot see real daemon-installed binaries from the surrounding environment.
func setHydraHomeTemp(t *testing.T) string {
	t.Helper()
	orig, hadOrig := os.LookupEnv("HYDRA_HOME")
	tmp := t.TempDir()
	os.Setenv("HYDRA_HOME", tmp)
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("HYDRA_HOME", orig)
		} else {
			os.Unsetenv("HYDRA_HOME")
		}
	})
	return tmp
}

func TestLspNPMPrefix_UsesHydraHome(t *testing.T) {
	home := setHydraHomeTemp(t)

	got := lspNPMPrefix()
	want := filepath.Join(home, "npm")
	if got != want {
		t.Fatalf("lspNPMPrefix() = %q, want %q", got, want)
	}
}

func TestLspNPMPrefix_FallsBackWithoutHydraHome(t *testing.T) {
	orig, hadOrig := os.LookupEnv("HYDRA_HOME")
	os.Unsetenv("HYDRA_HOME")
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("HYDRA_HOME", orig)
		}
	})

	got := lspNPMPrefix()
	if !strings.Contains(got, string(filepath.Separator)) || got == "" {
		t.Fatalf("lspNPMPrefix() without HYDRA_HOME returned unusable path %q", got)
	}
}

func TestNpmInstallPrefixArgs_NeverGlobal(t *testing.T) {
	home := setHydraHomeTemp(t)

	args := npmInstallPrefixArgs("lsp-client")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--prefix") {
		t.Fatalf("install args missing --prefix: %v", args)
	}
	if !strings.Contains(joined, filepath.Join(home, "npm")) {
		t.Fatalf("install args missing hydra prefix: %v", args)
	}
	if !strings.HasPrefix(joined, "install -g ") {
		t.Fatalf("install args malformed: %v", args)
	}
	// The whole point: no global install, which rewrites
	// /usr/lib/node_modules and /usr/bin and kills pi spawns mid-swap.
	for _, a := range args {
		if a == "/usr" || strings.HasPrefix(a, "/usr/") {
			t.Fatalf("install args reference global prefix: %v", args)
		}
	}
}

func TestLspCommandPath_PrefersPrefixBinary(t *testing.T) {
	home := setHydraHomeTemp(t)

	name := "fake-lsp-probe"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binDir := filepath.Join(home, "npm", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, name)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := "fake-lsp-probe"
	if got := lspCommandPath(cmd); got != binPath {
		t.Fatalf("lspCommandPath() = %q, want prefix binary %q", got, binPath)
	}
}

func TestLspCommandPath_FallsBackToPathLookup(t *testing.T) {
	setHydraHomeTemp(t)

	cmd := "definitely-not-installed-anywhere-xyz"
	if got := lspCommandPath(cmd); got != cmd {
		t.Fatalf("lspCommandPath() = %q, want bare fallback %q", got, cmd)
	}
}

func TestLspCommandPath_SkipsNonExecutable(t *testing.T) {
	home := setHydraHomeTemp(t)

	name := "fake-lsp-notexec"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binDir := filepath.Join(home, "npm", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 0o644: present but not executable — must NOT be picked up on unix.
	if err := os.WriteFile(filepath.Join(binDir, name), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := "fake-lsp-notexec"
	if runtime.GOOS == "windows" {
		if got := lspCommandPath(cmd); got == cmd {
			t.Fatalf("lspCommandPath() should accept %q on windows regardless of mode", name)
		}
		return
	}
	if got := lspCommandPath(cmd); got != cmd {
		t.Fatalf("lspCommandPath() = %q, want bare fallback for non-executable", got)
	}
}
