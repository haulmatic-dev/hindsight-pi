package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// enrichEnforceEnv is the RED gate. By default the contract tests skip loudly
// (suite green pre-GREEN); ENRICH_ENFORCE=1 proves RED.
const enrichEnforceEnv = "ENRICH_ENFORCE"

func requireEnrichImpl(t *testing.T) {
	t.Helper()
	if enrichmentImplemented {
		return
	}
	if os.Getenv(enrichEnforceEnv) == "1" {
		t.Fatalf("RED: brownfield enrichment contract not yet implemented (set ENRICH_ENFORCE=1 to fail; enrichmentImplemented=false)")
	}
	t.Skipf("RED: brownfield enrichment contract not yet implemented (set ENRICH_ENFORCE=1 to enforce)")
}

// enrichFixture builds a git repo at a temp dir. When commitTarget is true the
// target files are committed on the checkout branch (== base for a remote-less
// repo, per discovery A5) → brownfield. When false the files exist only on
// disk, not on the base → greenfield. worktreePath == projectPath == repo dir.
func enrichFixture(t *testing.T, commitTarget bool, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "development")
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if commitTarget && len(files) > 0 {
		git("add", "-A")
		git("commit", "-qm", "base")
	}
	return dir
}

func enrichBead(id types.BeadID, files string) types.Bead {
	return types.Bead{
		ID:       id,
		Title:    "GREEN: Implement",
		Metadata: map[string]string{"target_files": files},
	}
}

// fake output used by most tests: two claims + summary lines
func enrichAgentOut(t *testing.T, calls *int) enrichAgentRunner {
	return func(worktreePath, prompt string) (string, error) {
		*calls++
		return "claim: ServiceX uses a connection pool (verify: grep -r pool src)\n" +
			"claim: ServiceX retries requests 3 times (verify: grep -r retry src)\n" +
			"- ServiceX uses a connection pool\n" +
			"- ServiceX has an unclaimed fact\n", nil
	}
}

// AC1: brownfield trigger — all target files exist on base → agent runs and
// claims/summary are returned.
func TestEnrichBeadClaims_BrownfieldRunsAgent(t *testing.T) {
	requireEnrichImpl(t)
	dir := enrichFixture(t, true, "src/alpha.go", "src/beta.go")
	calls := 0
	run := func(w, p string) (string, error) { calls++; return "claim: c1 (verify: v1)\n- summary one\n", nil }
	claims, summary, err := enrichBeadClaims(enrichBead("proj-000", "src/alpha.go,src/beta.go"), dir, dir, run)
	if err != nil {
		t.Fatalf("enrichBeadClaims: %v", err)
	}
	if calls != 1 {
		t.Fatalf("agent calls = %d, want 1", calls)
	}
	if len(claims) != 1 || !strings.Contains(claims[0], "c1") {
		t.Fatalf("claims = %v, want c1", claims)
	}
	if !strings.Contains(summary, "summary one") {
		t.Fatalf("summary = %q, want it to contain 'summary one'", summary)
	}
}

// AC5: greenfield (target file missing on base) → skip with exactly one log
// line, zero agent calls (zero tokens).
func TestEnrichBeadClaims_GreenfieldSkipsZeroTokens(t *testing.T) {
	requireEnrichImpl(t)
	dir := enrichFixture(t, false, "src/unused.go")
	calls := 0
	run := func(w, p string) (string, error) { calls++; return "", nil }
	claims, summary, err := enrichBeadClaims(enrichBead("proj-001", "src/missing.go"), dir, dir, run)
	if err != nil {
		t.Fatalf("greenfield must not error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("agent called %d times on greenfield bead, want 0 (zero tokens)", calls)
	}
	if len(claims) != 0 || summary != "" {
		t.Fatalf("greenfield must return empty output, got claims=%v summary=%q", claims, summary)
	}
}

// AC2: anti-laundering gate — summary assertions with no matching claim are
// dropped; claimed assertions pass through.
func TestEnrichBeadClaims_AntiLaunderingDropsUnclaimedSummaryLines(t *testing.T) {
	requireEnrichImpl(t)
	dir := enrichFixture(t, true, "src/alpha.go")
	calls := 0
	claims, summary, err := enrichBeadClaims(enrichBead("proj-002", "src/alpha.go"), dir, dir, enrichAgentOut(t, &calls))
	if err != nil {
		t.Fatalf("enrichBeadClaims: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("claims = %v, want 2", claims)
	}
	if !strings.Contains(summary, "ServiceX uses a connection pool") {
		t.Fatalf("summary dropped a CLAIMED assertion: %q", summary)
	}
	if strings.Contains(summary, "unclaimed fact") {
		t.Fatalf("summary kept an UNCLAIMED assertion (laundering): %q", summary)
	}
}

// AC3: budget — summary over budget elides oldest assertion lines first and
// appends footer `elided: <count>`.
func TestEnrichBeadClaims_BudgetElidesOldestWithFooter(t *testing.T) {
	requireEnrichImpl(t)
	dir := enrichFixture(t, true, "src/alpha.go")
	run := func(w, p string) (string, error) {
		var b strings.Builder
		for i := 0; i < 200; i++ {
			b.WriteString("claim: assertion number " + itoa(i) + " (verify: v)\n")
		}
		for i := 0; i < 200; i++ {
			b.WriteString("- assertion number " + itoa(i) + "\n")
		}
		return b.String(), nil
	}
	_, summary, err := enrichBeadClaims(enrichBead("proj-003", "src/alpha.go"), dir, dir, run)
	if err != nil {
		t.Fatalf("enrichBeadClaims: %v", err)
	}
	if !strings.Contains(summary, "elided:") {
		t.Fatalf("overflow summary missing `elided: <n>` footer: tail=%q", tailTrim(summary))
	}
	// oldest (assertion number 0) must be gone; newest must remain
	if strings.Contains(summary, "assertion number 0\n") {
		t.Fatalf("oldest line not elided: %q", tailTrim(summary))
	}
}

// default budget is context_pack.max_pack_tokens (DefaultMaxPackTokens, 40000).
func TestEnrichBeadClaims_DefaultBudgetIsMaxPackTokens(t *testing.T) {
	requireEnrichImpl(t)
	dir := enrichFixture(t, true, "src/alpha.go")
	run := func(w, p string) (string, error) {
		var b strings.Builder
		for i := 0; i < 500; i++ { // ~30 chars/line * 500 = 15000 chars < 40000*4 — fits
			b.WriteString("claim: filler assertion " + itoa(i) + " (verify: v)\n- filler assertion " + itoa(i) + "\n")
		}
		return b.String(), nil
	}
	_, summary, err := enrichBeadClaims(enrichBead("proj-004", "src/alpha.go"), dir, dir, run)
	if err != nil {
		t.Fatalf("enrichBeadClaims: %v", err)
	}
	if strings.Contains(summary, "elided:") {
		t.Fatalf("default budget must be 40000 tokens (DefaultMaxPackTokens); 16000-char output should not elide: %q", tailTrim(summary))
	}
}

// AC4: enrichment output committed as docs/hydra/handoff/enrich-<bead>.md in
// the worktree so loadChainContext picks it up.
func TestEnrichBeadClaims_WritesHandoffDoc(t *testing.T) {
	requireEnrichImpl(t)
	dir := enrichFixture(t, true, "src/alpha.go")
	calls := 0
	_, _, err := enrichBeadClaims(enrichBead("proj-005", "src/alpha.go"), dir, dir, enrichAgentOut(t, &calls))
	if err != nil {
		t.Fatalf("enrichBeadClaims: %v", err)
	}
	doc := filepath.Join(dir, "docs", "hydra", "handoff", "enrich-proj-005.md")
	data, rerr := os.ReadFile(doc)
	if rerr != nil {
		t.Fatalf("enrich handoff doc not written at %s: %v", doc, rerr)
	}
	if !strings.Contains(string(data), "ServiceX uses a connection pool") {
		t.Fatalf("handoff doc missing claimed summary: %q", data)
	}
}

// A2 memoization: once-per-bead — existing enrich doc skips the agent.
func TestEnrichBeadClaims_SkipsWhenDocAlreadyExists(t *testing.T) {
	requireEnrichImpl(t)
	dir := enrichFixture(t, true, "src/alpha.go")
	docDir := filepath.Join(dir, "docs", "hydra", "handoff")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "enrich-proj-006.md"), []byte("prior enrichment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	run := func(w, p string) (string, error) { calls++; return "", nil }
	if _, _, err := enrichBeadClaims(enrichBead("proj-006", "src/alpha.go"), dir, dir, run); err != nil {
		t.Fatalf("enrichBeadClaims: %v", err)
	}
	if calls != 0 {
		t.Fatalf("doc exists but agent still called %d times, want 0", calls)
	}
}

// DefaultAgentRunner must be a detached text-mode invocation, never rpc mode
// (discovery decision 1: pan.RunPi pattern, --no-extensions --no-session).
func TestEnrichDefaultAgentRunner_DetachedNotRPC(t *testing.T) {
	requireEnrichImpl(t)
	if enrichDefaultAgentRunner == nil {
		t.Fatal("enrichDefaultAgentRunner must be set (detached text-mode runner)")
	}
	argv := enrichAgentArgv("srcx fake prompt")
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "--mode rpc") || strings.Contains(joined, "rpc") {
		t.Fatalf("runner must be detached text-mode, not rpc: argv=%q", argv)
	}
	if !strings.Contains(joined, "--no-extensions") || !strings.Contains(joined, "--no-session") {
		t.Fatalf("runner must pass --no-extensions --no-session: argv=%q", argv)
	}
}

// Ac budget library reuse: budget constant must come from
// internal/core.DefaultMaxPackTokens (reuse contract, not a redefinition).
func TestEnrichBudgetReusesContextPackDefault(t *testing.T) {
	requireEnrichImpl(t)
	if enrichDefaultBudgetTokens() != core.DefaultMaxPackTokens {
		t.Fatalf("enrichDefaultBudgetTokens() = %d, want internal/core.DefaultMaxPackTokens = %d", enrichDefaultBudgetTokens(), core.DefaultMaxPackTokens)
	}
}

func itoa(i int) string {
	return fmtInt(i)
}

func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

func tailTrim(s string) string {
	if len(s) > 120 {
		return s[len(s)-120:]
	}
	return s
}
