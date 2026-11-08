package core

import (
	"os"

	"github.com/haulmatic-dev/hydra/pkg/types"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeVerdictLog writes a fake worker log containing the given
// HYDRA_VERDICT payload into an isolated HYDRA_HOME log dir.
func writeVerdictLog(t *testing.T, beadID, verdict string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "logs", "workers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYDRA_HOME", filepath.Dir(filepath.Dir(dir)))
	name := filepath.Join(dir, safeBeadID(beadID)+"-1.log")
	line := "2026-09-25T00:00:00Z [stdout] HYDRA_VERDICT: " + verdict + "\n"
	if err := os.WriteFile(name, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptedEmptyReason(t *testing.T) {
	cases := []struct {
		name    string
		verdict string
		changed []string
		want    string
	}{
		{"docs-only", `{"done":true,"summary":"nothing to do, changes were docs-only"}`, []string{"README.md", "docs/guide.md"}, acceptedEmptyDocsOnly},
		{"docs-only rejected with code changes", `{"done":true,"summary":"docs-only cleanup"}`, []string{"x/a.go"}, ""},
		{"chain-deferred", `{"done":true,"summary":"already handled, chain-deferred-to-root"}`, nil, acceptedEmptyChainDeferredRoot},
		{"chain-deferred rejected with changes", `{"done":true,"summary":"chain-deferred-to-root"}`, []string{"README.md"}, ""},
		{"superseded", `{"done":true,"summary":"bead superseded by PR #9"}`, nil, acceptedEmptySuperseded},
		{"failed verdict not honoured", `{"done":false,"summary":"docs-only but blocked"}`, []string{"README.md"}, ""},
		{"no reason", `{"done":true,"summary":"all acceptance criteria met"}`, nil, ""},
		{"nil verdict", ``, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parsed *emptyGateVerdict
			if tc.verdict != "" {
				parsed = parseEmptyGateVerdictForTest(t, tc.verdict)
			}
			if got := acceptedEmptyReason(parsed, tc.changed); got != tc.want {
				t.Fatalf("acceptedEmptyReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// parseEmptyGateVerdictForTest builds a verdict the same way the log parser
// does, without duplicating the JSON decoding.
func parseEmptyGateVerdictForTest(t *testing.T, raw string) *emptyGateVerdict {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs", "workers")
	os.MkdirAll(logs, 0o755)
	name := filepath.Join(logs, "vtest-1.log")
	os.WriteFile(name, []byte("x [stdout] HYDRA_VERDICT: "+raw+"\n"), 0o644)
	t.Setenv("HYDRA_HOME", dir)
	return extractEmptyGateVerdict(logs, "vtest")
}

func TestExtractEmptyGateVerdictLastWins(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs", "workers")
	os.MkdirAll(logs, 0o755)
	t.Setenv("HYDRA_HOME", dir)
	name := filepath.Join(logs, "last-bead-1.log")
	content := "x [stdout] HYDRA_VERDICT: {\"done\":false,\"summary\":\"first attempt\"}\n" +
		"x [stdout] HYDRA_VERDICT: {\"done\":true,\"summary\":\"final\"}\n"
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	v := extractEmptyGateVerdict(logs, "last-bead")
	if v == nil || !v.Done || v.Summary != "final" {
		t.Fatalf("expected last verdict done:true summary=final, got %+v", v)
	}
}

func TestVerifyBeforeClose_DocsOnlyAccepted(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	// Only docs changed; bead target x/a.go untouched.
	writeAndCommit(t, repo, "README.md", "# notes\n")
	writeVerdictLog(t, bead.ID.String(), `{"done":true,"summary":"ac met; work was docs-only"}`)
	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("docs-only accepted-empty close should pass, got: %v", err)
	}
}

// TestEmitEmptyDiffCompleted pins the mechanical backstop: a done verdict
// over an empty or target-missing diff emits empty_diff_completed before the
// close is rejected; a non-done verdict, nil sink, or on-target diff emits
// nothing.
func TestEmitEmptyDiffCompleted(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	central := t.TempDir()
	closer.SetEventSink(NewEventSink(central))

	count := func() int {
		t.Helper()
		entries, _ := os.ReadDir(central)
		n := 0
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonl") {
				n++
			}
		}
		return n
	}

	// done verdict + empty diff -> event emitted
	writeVerdictLog(t, bead.ID.String(), `{"done":true,"summary":"all ac met"}`)
	before := count()
	closer.emitEmptyDiffCompleted(bead, "test-project", repo, nil, []string{"x/a.go"})
	if count() != before+1 {
		t.Fatal("empty diff with done verdict must emit empty_diff_completed")
	}

	// non-done verdict -> no event
	writeVerdictLog(t, bead.ID.String(), `{"done":false,"summary":"blocked"}`)
	before = count()
	closer.emitEmptyDiffCompleted(bead, "test-project", repo, nil, []string{"x/a.go"})
	if count() != before {
		t.Fatal("non-done verdict must not emit")
	}

	// done verdict, diff touches target -> no event
	writeVerdictLog(t, bead.ID.String(), `{"done":true,"summary":"all ac met"}`)
	before = count()
	closer.emitEmptyDiffCompleted(bead, "test-project", repo, []string{"x/a.go"}, []string{"x/a.go"})
	if count() != before {
		t.Fatal("on-target diff must not emit")
	}
}

func TestVerifyBeforeClose_EmptyDiffNoReasonRejects(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	// No changes at all, no verdict reason -> must still reject.
	if err := closer.verifyBeforeClose(bead, repo, ""); err == nil {
		t.Fatal("empty diff without accepted reason must be rejected")
	}
}

func TestVerifyBeforeClose_EmptyDiffAcceptedReason(t *testing.T) {
	repo, closer, _ := setupScopeGateTest(t, "block")
	// E1 (2026-11-09): the token must NAME a chain root, and the daemon
	// verifies it exists and is open. Real-shape bead ID (E1 derives the
	// reference namespace from it); named root seeded OPEN so the
	// reference resolves.
	bead := &types.Bead{
		ID:       "scope-test-ab12",
		Title:    "Test bead",
		Project:  "test-project",
		Metadata: map[string]string{"files": "x/a.go"},
	}
	base, berr := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if berr != nil {
		closer.SetSessionBase(string(bead.ID), "HEAD")
	} else {
		closer.SetSessionBase(string(bead.ID), strings.TrimSpace(string(base)))
	}
	if mc, ok := closer.beadsClient.(*mockBeadsClient); ok {
		// The mock ListBeads filters by filepath.Base(projectPath); match it.
		mc.beads["scope-test-9zz9"] = types.Bead{ID: "scope-test-9zz9", Project: filepath.Base(repo), Status: types.StatusOpen}
	}
	writeVerdictLog(t, bead.ID.String(), `{"done":true,"summary":"chain-deferred-to-root: root scope-test-9zz9 owns the implementation"}`)
	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("accepted-empty reason close should pass, got: %v", err)
	}
}

// Non-hydra project reference (review-round-1 blocker regression guard).
func TestVerifyEmptyCloseReference_NonHydraProject(t *testing.T) {
	c := &BeadCloser{}
	// No resolver/client: verification must fail closed, never crash.
	if c.verifyEmptyCloseReference(&types.Bead{ID: "srv-telematics-ab12", Project: "srv-telematics"}, "repo", "superseded by srv-telematics-cd34", acceptedEmptySuperseded) {
		t.Error("nil client must fail closed, not accept")
	}
}

func TestAllDocsOnly(t *testing.T) {
	if allDocsOnly(nil) {
		t.Fatal("empty changed list is not docs-only")
	}
	if !allDocsOnly([]string{"a.md", "docs/b.txt"}) {
		t.Fatal("doc paths should be docs-only")
	}
	if allDocsOnly([]string{"a.md", "b.go"}) {
		t.Fatal("mixed list is not docs-only")
	}
}
