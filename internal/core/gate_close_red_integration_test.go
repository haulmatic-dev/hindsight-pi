package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// stubGateRunnerForRed returns a GateRunner whose test gate always produces
// the given artifact. We cannot easily stub GateRunner (concrete type), so
// these tests exercise the decision functions with controlled artifacts and
// verify the matrix through the exported helpers.

func TestRunDaemonGates_RedContractMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("integration-style: needs git worktree")
	}

	dir := t.TempDir()
	// Minimal git worktree so changedFilePaths can resolve a diff.
	run := func(args ...string) {
		c := fmt.Sprintf("cd %s && git %s", dir, strings.Join(args, " "))
		if out, err := execCommand(c); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		_ = os.Stderr
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	// Base commit on development.
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	run("checkout", "-q", "-b", "development")
	run("add", ".")
	run("commit", "-q -m base")
	// Branch with a change (simulates the RED bead's test file).
	run("checkout", "-q", "-b", "feature")
	fooDir := filepath.Join(dir, "internal", "foo")
	if err := os.MkdirAll(fooDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fooDir, "x_test.go"), []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q -m \"red test\"")

	artifacts := map[string]GateArtifact{
		// RED + FAIL + own pkg failing → contract satisfied.
		"red_own_fail": {
			Status:   GateStatusFailed,
			ExitCode: 1,
			Summary:  "--- FAIL: TestNew\nFAIL\texample.com/m/internal/foo\t0.01s",
			RawLog:   "/tmp/nonexistent.log",
		},
		// RED + FAIL + all foreign → contract violated.
		"red_foreign_fail": {
			Status:   GateStatusFailed,
			ExitCode: 1,
			Summary:  "FAIL\texample.com/m/internal/bar\t0.01s",
			RawLog:   "/tmp/nonexistent.log",
		},
	}

	c := &BeadCloser{}
	redBead := &types.Bead{Title: "RED: Write tests"}

	// ownFilesInFailingPackages with real worktree: own pkg failing → true.
	if !c.ownFilesInFailingPackages(redBead, dir, artifacts["red_own_fail"]) {
		t.Error("RED own-package failure should satisfy contract (want true)")
	}
	// all-foreign failure → false.
	if c.ownFilesInFailingPackages(redBead, dir, artifacts["red_foreign_fail"]) {
		t.Error("RED foreign-only failure should violate contract (want false)")
	}

	// isForeignTestFailure for a GREEN bead on the same artifacts:
	// own-package failure → NOT foreign (blocks close as before).
	greenBead := &types.Bead{Title: "GREEN: Implement"}
	if c.isForeignTestFailure(greenBead, dir, artifacts["red_own_fail"]) {
		t.Error("own-package failure should not be foreign for GREEN")
	}
}

func execCommand(cmd string) (string, error) {
	out, err := exec.Command("bash", "-c", cmd).CombinedOutput()
	return string(out), err
}
