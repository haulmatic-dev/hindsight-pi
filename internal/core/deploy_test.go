package core

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyTier1(t *testing.T) {
	cases := []struct {
		paths []string
		want  string
	}{
		{[]string{"README.md"}, "tier-1"},
		{[]string{"docs/architecture/shepherd.md", "config.yaml"}, "tier-1"},
		{[]string{"internal/hindsight/client_test.go"}, "tier-1"},
		{[]string{"internal/core/deploy.go"}, "tier-1g"},
		{[]string{"cmd/worker/main.go"}, "tier-1g"},
		{[]string{"scripts/verify-deploy.sh"}, "tier-1g"},
		{[]string{"README.md", "internal/core/deploy.go"}, "tier-1g"}, // any doubt -> human
		{[]string{""}, "tier-1g"},
		{[]string{"internal/hindsight/client.go"}, "tier-1"},
	}
	for _, c := range cases {
		if got := ClassifyTier1(c.paths); got != c.want {
			t.Errorf("ClassifyTier1(%v) = %q, want %q", c.paths, got, c.want)
		}
	}
}

func newTestDeployer(t *testing.T, dir string, workers int, verifyFail bool) *Tier1Deployer {
	t.Helper()
	bin := filepath.Join(dir, "bin", "hydra")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	verifyScript := filepath.Join(dir, "verify.sh")
	content := "#!/bin/sh\nexit 0\n"
	if verifyFail {
		content = "#!/bin/sh\necho boom >&2\nexit 1\n"
	}
	if err := os.WriteFile(verifyScript, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Tier1Deployer{
		cfg: DeployConfig{
			Enabled:      true,
			RepoPath:     dir,
			BinaryPath:   bin,
			VerifyScript: verifyScript,
			BuildTimeout: 60000000000,
		},
		workers:    func() int { return workers },
		pinWorkers: func(int) {},
		now:        time.Now,
	}
	// real runner replaced with a stub: "cp" works via os, restart is a no-op
	d.runner = func(name string, args ...string) error {
		if name == "cp" && len(args) == 2 {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			return os.WriteFile(args[1], data, 0o755)
		}
		if name == "systemctl" {
			return nil
		}
		return errors.New("unexpected command " + name)
	}
	return d
}

func TestDeploySkipsWorkersInFlight(t *testing.T) {
	dir := t.TempDir()
	d := newTestDeployer(t, dir, 2, false)
	called := 0
	d.workers = func() int { called++; return 1 }
	d.RunAfterMerge("feat")
	if called != 1 {
		t.Fatalf("worker probe called %d times, want 1", called)
	}
	// No swap must have happened.
	if _, err := os.Stat(filepath.Join(dir, "bin", "hydra.prev")); !os.IsNotExist(err) {
		t.Fatal("backup created despite in-flight workers")
	}
}

func TestDeploySucceedsAndSwapsBinary(t *testing.T) {
	dir := t.TempDir()
	d := newTestDeployer(t, dir, 0, false)
	// Make the build produce a new binary: build step writes binary.new.

	d.cfg.RepoPath = dir
	// Replace deploySteps' build via running it: simpler — pre-create .new? No:
	// deploySteps builds with go, which works in the repo dir (dir has no go
	// files). Override by making RepoPath point at this repo? Instead stub the
	// build through runner: our runner handles only cp/systemctl, so the build
	// would fail. Provide a build-friendly runner.
	d.runner = func(name string, args ...string) error {
		switch name {
		case "cp":
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			return os.WriteFile(args[1], data, 0o755)
		case "systemctl":
			return nil
		case "go":
			// emulate a successful build producing binary.new
			bin := d.cfg.BinaryPath
			return os.WriteFile(bin+".new", []byte("new-binary"), 0o755)
		case "bash":
			return nil // verify passes
		}
		return errors.New("unexpected command " + name)
	}
	// deploySteps builds with `go build` in RepoPath via exec.Command — stub it
	// by pointing RepoPath at this repo (real go build) and pre-creating a
	// go.mod-bearing dir is overkill: simplest is a temp module.
	mod := filepath.Join(dir, "go.mod")
	os.WriteFile(mod, []byte("module test\n\ngo 1.21\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "cmd", "hydra"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "hydra", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	d.RunAfterMerge("feat")
	bin := d.cfg.BinaryPath
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, []byte("old-binary")) {
		t.Fatal("binary not swapped (still old)")
	}
}

func TestDeployRollbackOnVerifyFailure(t *testing.T) {
	dir := t.TempDir()
	d := newTestDeployer(t, dir, 0, true)
	d.runner = func(name string, args ...string) error {
		switch name {
		case "cp":
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			return os.WriteFile(args[1], data, 0o755)
		case "systemctl":
			return nil
		case "go":
			return os.WriteFile(d.cfg.BinaryPath+".new", []byte("new-binary"), 0o755)
		case "bash":
			return errors.New("verify failed: exit 1: boom")
		}
		return errors.New("unexpected command " + name)
	}
	d.RunAfterMerge("feat")
	data, err := os.ReadFile(d.cfg.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old-binary" {
		t.Fatalf("rollback failed: binary = %q, want old-binary", string(data))
	}
}

func TestDeployDisabledIsInert(t *testing.T) {
	if NewTier1Deployer(DeployConfig{Enabled: false}, nil, nil, nil) != nil {
		t.Fatal("disabled deployer must be nil")
	}
	var nilD *Tier1Deployer
	nilD.RunAfterMerge("feat") // must not panic
}

func TestAutoMergeIfTier1FailClosed(t *testing.T) {
	origFiles, origChecks, origMerge := deployGhFiles, deployGhChecks, deployGhMerge
	defer func() { deployGhFiles, deployGhChecks, deployGhMerge = origFiles, origChecks, origMerge }()

	// tier-1g paths -> no merge attempt
	deployGhFiles = func(string, string) ([]string, error) { return []string{"internal/core/x.go"}, nil }
	deployGhChecks = func(string, string) (bool, error) { return true, nil }
	merged := false
	deployGhMerge = func(string, string) error { merged = true; return nil }
	ok, err := AutoMergeIfTier1("repo", "123")
	if ok || err != nil || merged {
		t.Fatalf("tier-1g PR must not merge: ok=%v err=%v merged=%v", ok, err, merged)
	}

	// tier-1 but red checks -> no merge
	deployGhFiles = func(string, string) ([]string, error) { return []string{"README.md"}, nil }
	deployGhChecks = func(string, string) (bool, error) { return false, errors.New("failing check") }
	ok, err = AutoMergeIfTier1("repo", "123")
	if ok || err == nil || merged {
		t.Fatalf("red-check PR must not merge: ok=%v err=%v merged=%v", ok, err, merged)
	}

	// tier-1 green -> merge
	deployGhChecks = func(string, string) (bool, error) { return true, nil }
	ok, err = AutoMergeIfTier1("repo", "123")
	if !ok || err != nil || !merged {
		t.Fatalf("tier-1 green PR must merge: ok=%v err=%v", ok, err)
	}
	if !strings.Contains("", "") {
		t.Fatal("unreachable")
	}
}
