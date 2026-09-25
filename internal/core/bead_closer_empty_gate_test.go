package core

import (
	"os"
	"path/filepath"
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

func TestVerifyBeforeClose_EmptyDiffNoReasonRejects(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	// No changes at all, no verdict reason -> must still reject.
	if err := closer.verifyBeforeClose(bead, repo, ""); err == nil {
		t.Fatal("empty diff without accepted reason must be rejected")
	}
}

func TestVerifyBeforeClose_EmptyDiffAcceptedReason(t *testing.T) {
	repo, closer, bead := setupScopeGateTest(t, "block")
	writeVerdictLog(t, bead.ID.String(), `{"done":true,"summary":"no code change required; chain-deferred-to-root"}`)
	if err := closer.verifyBeforeClose(bead, repo, ""); err != nil {
		t.Fatalf("accepted-empty reason close should pass, got: %v", err)
	}
}

func TestIsDocPath(t *testing.T) {
	yes := []string{"README.md", "docs/x/y.txt", "a/b.NOTES.mdx"}
	no := []string{"x/a.go", "docs.md.go", "main.go.bak", "mdd.tx"}
	for _, p := range yes {
		if !isDocPath(p) {
			t.Errorf("isDocPath(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if isDocPath(p) {
			t.Errorf("isDocPath(%q) = true, want false", p)
		}
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