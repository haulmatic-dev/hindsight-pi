package main

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// gybo4 (2026-09-10): a worktree removed between spawn and exec made Go's
// fork/exec report the chdir ENOENT against the pi binary path
// ("fork/exec /usr/bin/pi: no such file or directory"), costing an hour of
// false "provider failure" diagnosis. The worker must stat cmd.Dir before
// exec and report the directory as the true cause, classified as
// infrastructure (exit 4), never as a pi/binary or code failure.
func TestExecuteWithPi_MissingWorktreeDirReportsDirNotPi(t *testing.T) {
	t.Setenv("HYDRA_HOME", t.TempDir())

	missingDir := t.TempDir() + "/does-not-exist"
	bead := types.Bead{ID: "hydra-test-missingwt", Title: "FIX: probe", Project: missingDir}

	result, err := executeWithPi(bead, PiConfig{}, nil, missingDir, nil, promptExtras{})
	if err == nil {
		t.Fatal("executeWithPi with missing worktree dir must fail")
	}
	if !strings.Contains(err.Error(), "worktree dir missing: "+missingDir) {
		t.Fatalf("error must name the missing dir, got: %v", err)
	}
	if strings.Contains(err.Error(), "fork/exec") {
		t.Fatalf("error must not be a misattributed fork/exec binary error: %v", err)
	}
	if result == nil {
		t.Fatal("result must be non-nil so the failure is classified and recorded")
	}
	if !strings.Contains(result.Error, "worktree dir missing: "+missingDir) {
		t.Fatalf("result.Error must carry the true cause, got: %q", result.Error)
	}
	if got := classifyFailure(result); got != 4 {
		t.Fatalf("classifyFailure = %d, want 4 (infra) — a missing worktree dir is not a code failure", got)
	}
}
