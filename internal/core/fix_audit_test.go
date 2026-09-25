package core

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Closure-audit verifier (jx78, 2026-09-25): the original verdict's own
// evidence is re-run cheaply in the inherited worktree before Pan trusts a
// recorded done:true — the jx78 verdict was hollow (suites failed at load,
// 0 tests ran).

func setupFixAuditOverseer(t *testing.T, bead types.Bead, gateCmd string) *Overseer {
	t.Helper()
	o, _ := setupSatisfiedOverseer(t, bead, gateCmd)
	return o
}

func TestBuildFixVerifierGreen(t *testing.T) {
	orig := types.Bead{
		ID:          "test-project-abc",
		Title:       "original work",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusClosed,
		Type:        "implement",
	}
	o := setupFixAuditOverseer(t, orig, "exit 0 <scope>")

	ver := BuildFixVerifier(o)("fx-1", "test-project-abc")
	if ver.Status != "verified" {
		t.Errorf("status = %q (%s), want verified", ver.Status, ver.Detail)
	}
}

func TestBuildFixVerifierRedContradicts(t *testing.T) {
	orig := types.Bead{
		ID:          "test-project-abc",
		Title:       "original work",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusClosed,
		Type:        "implement",
	}
	o := setupFixAuditOverseer(t, orig, "exit 3 <scope>")

	ver := BuildFixVerifier(o)("fx-1", "test-project-abc")
	if ver.Status != "contradicted" {
		t.Errorf("status = %q (%s), want contradicted", ver.Status, ver.Detail)
	}
	if !strings.Contains(ver.Detail, "exit 3") {
		t.Errorf("detail %q missing exit code", ver.Detail)
	}
}

func TestBuildFixVerifierUnknownOnDoubt(t *testing.T) {
	orig := types.Bead{
		ID:          "test-project-abc",
		Title:       "original work",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusClosed,
	}
	o := setupFixAuditOverseer(t, orig, "exit 0 <scope>")
	o.beadCloser.gateRunner = nil

	if ver := BuildFixVerifier(o)("fx-1", "test-project-abc"); ver.Status != "unknown" {
		t.Errorf("status = %q, want unknown (no gate runner)", ver.Status)
	}

	// Unresolvable original: unknown, never a false verified/contradicted.
	if ver := BuildFixVerifier(o)("fx-1", "does-not-exist-xyz"); ver.Status != "unknown" {
		t.Errorf("status = %q, want unknown (unresolved original)", ver.Status)
	}
}

func TestFixAuditExtraction(t *testing.T) {
	desc := "Auto-created fix bead because the original closed as `failed`.\n\n" +
		"Original bead: abc\nClose reason: failed (worker exited non-zero)\n\n" +
		"Recorded verdict: HYDRA_VERDICT: {\"done\": true, \"ac_met\": [\"x\"]}"
	if got := fixCloseReason(desc); !strings.Contains(got, "worker exited non-zero") {
		t.Errorf("fixCloseReason = %q", got)
	}
	v := recordedVerdictSummary(desc)
	if !strings.Contains(v, `"done": true`) {
		t.Errorf("recordedVerdictSummary = %q", v)
	}
	if !hollowVerdict(v) {
		t.Error("hollowVerdict = false for done:true verdict")
	}
	if got := recordedVerdictSummary("no verdict here"); got != "" {
		t.Errorf("recordedVerdictSummary(no verdict) = %q, want empty", got)
	}
}