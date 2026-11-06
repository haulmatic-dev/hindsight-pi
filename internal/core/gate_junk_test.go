package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// gateJunkEnforceEnv gates the RED-phase proof: with GATE_JUNK_ENFORCE=1 the
// contract below FAILS (proving the gate is implemented nowhere), matching
// the established GATE_DEPS_ENFORCE pattern.
const gateJunkEnforceEnv = "GATE_JUNK_ENFORCE"

func gateJunkRequireImpl(t *testing.T) {
	t.Helper()
	if gateJunkImplemented {
		return // implemented
	}
	msg := "RED: close-time junk gate contract not yet implemented"
	if enforce := strings.TrimSpace(strings.ToLower(os.Getenv(gateJunkEnforceEnv))); enforce == "1" || enforce == "true" {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

// junkGit is a GUARDED git helper with explicit error propagation — the
// established fail-loud test utility shape (initGitRepo/writeAndCommit).
func junkGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func junkCommitPaths(t *testing.T, repo string, paths ...string) {
	t.Helper()
	junkGit(t, repo, append([]string{"add"}, paths...)...)
	junkGit(t, repo, "commit", "-q", "-m", "chain work")
}

// junkOnDiskFile returns the on-disk content of path inside repo.
func junkOnDiskFile(t *testing.T, repo, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, path))
	if err != nil {
		t.Fatalf("path %q gone from disk: %v", path, err)
	}
	return string(b)
}

// junkTracked reports whether path is present in the committed index (HEAD).
func junkTracked(t *testing.T, repo, path string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "ls-tree", "-r", "--name-only", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-tree: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == path {
			return true
		}
	}
	return false
}

func junkStagedPaths(t *testing.T, repo string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "diff", "--cached", "--name-only")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff --cached --name-only: %v", err)
	}
	var paths []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	return paths
}

func junkCommitCount(t *testing.T, repo string) int {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-list", "--count", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-list --count HEAD: %v", err)
	}
	return len(strings.Fields(string(out)))
}

// junkGateFixtureBead returns a close-candidate bead whose changed set
// consists of the extra reportFiles the caller commits on top of the fixture
// repo's initial real commit.
func junkGateCloserFor(t *testing.T, repo string) (*BeadCloser, *mockBeadsClient, string) {
	t.Helper()
	return hardGateCloser(t, hardGateConfig(3), repo)
}

// --- wiring smoke: inert/fail-open path covered with or without a real gate

func TestGateJunk_CleanWorktreeFailOpenCoverage(t *testing.T) {
	// Unconditional (not require-gated): exercises the gate entry point on a
	// clean worktree in both RED (inert stub) and GREEN (real gate, clean set
	// → pass). Gives the wiring + fail-open branch measurable coverage.
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "src/thread-cache/service.ts", "export class ThreadCache {}\n")

	c, _, sinkDir := junkGateCloserFor(t, repo)
	bead := &types.Bead{
		ID:          "proj-junk0",
		Title:       "GREEN: Implement thread cache",
		Project:     filepath.Base(repo),
		Status:      types.StatusInProgress,
		Description: "chain work",
	}
	if err := c.gateJunkArtifacts(bead, repo, "", repo); err != nil {
		t.Fatalf("clean worktree must pass the junk gate fail-open: %v", err)
	}
	for _, ev := range readEventsOfType(t, NewEventSink(sinkDir), junkDropEventType) {
		t.Errorf("no close_gate_junk_dropped event expected on a clean worktree, got: %+v", ev)
	}
}

// --- AC1: committed junk is auto-dropped exactly once, close proceeds ---

func TestGateJunk_CommittedJunkDroppedOnceCloseProceeds(t *testing.T) {
	gateJunkRequireImpl(t)
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "src/thread-cache/service.ts", "export class ThreadCache {}\n") // base (real work)

	c, _, sinkDir := junkGateCloserFor(t, repo)
	bead := &types.Bead{
		ID:          "proj-junk1",
		Title:       "GREEN: Implement thread cache",
		Project:     filepath.Base(repo),
		Status:      types.StatusInProgress,
		Description: "chain work\n**Verification Command:**\n```\nexit 0\n```\n",
	}
	before := junkCommitCount(t, repo)

	junkCommitPaths(t, repo, ".tmp-jest/x", "src/thread-cache/service.ts")

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("committed junk path must be auto-dropped once and let the close proceed: %v", err)
	}
	// Removal commit exists (one drop commit), file gone from the index.
	if got := junkCommitCount(t, repo); got != before+1 {
		t.Errorf("expected exactly one removal commit (before %d, after %d)", before, got)
	}
	if junkTracked(t, repo, ".tmp-jest/x") {
		t.Error("junk path .tmp-jest/x must be gone from the index after the drop")
	}
	if onDisk := junkOnDiskFile(t, repo, ".tmp-jest/x"); strings.TrimSpace(onDisk) == "" {
		t.Error("junk file must still be on disk after the drop (untracked, ignored)")
	}
	// A typed drop event names the junk path.
	evs := readEventsOfType(t, NewEventSink(sinkDir), junkDropEventType)
	if len(evs) != 1 {
		t.Fatalf("expected exactly one close_gate_junk_dropped event, got %d", len(evs))
	}
	if !strings.Contains(evs[0].Detail, ".tmp-jest/x") {
		t.Errorf("drop event must name the junk path verbatim, got: %q", evs[0].Detail)
	}
}

// --- AC2: staged-only junk is unstaged (no commit) and close proceeds ---

func TestGateJunk_StagedOnlyJunkUnstagedWithoutCommit(t *testing.T) {
	gateJunkRequireImpl(t)
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "src/thread-cache/service.ts", "export class ThreadCache {}\n")

	c, _, _ := junkGateCloserFor(t, repo)
	bead := &types.Bead{
		ID:          "proj-junk2",
		Title:       "GREEN: Implement thread cache",
		Project:     filepath.Base(repo),
		Status:      types.StatusInProgress,
		Description: "chain work",
	}
	before := junkCommitCount(t, repo)
	full := filepath.Join(repo, ".jest-cache", "transform")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	junkGit(t, repo, "add", ".jest-cache")

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("staged-only junk must be unstaged and let the close proceed: %v", err)
	}
	if got := junkCommitCount(t, repo); got != before {
		t.Errorf("a removal commit would die on a clean tree — no commit allowed, before %d after %d", before, got)
	}
	if paths := junkStagedPaths(t, repo); len(paths) != 0 {
		t.Errorf("junk path must be unstaged, still staged: %v", paths)
	}
	if _, err := os.ReadFile(full); err != nil {
		t.Error("staged junk file must remain on disk (only unstaged)")
	}
}

// --- AC3: junk re-added on a retry after drops is a distinct hard reject ---

func TestGateJunk_ReAddedAfterTwoDropsRejectsJunkArtifacts(t *testing.T) {
	gateJunkRequireImpl(t)
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "src/thread-cache/service.ts", "export class ThreadCache {}\n")

	c, mock, sinkDir := junkGateCloserFor(t, repo)
	bead := &types.Bead{
		ID:          "proj-junk3",
		Title:       "GREEN: Implement thread cache",
		Project:     filepath.Base(repo),
		Status:      types.StatusInProgress,
		Description: "chain work",
	}

	// Two junk generations already auto-dropped by the gate:
	junkCommitPaths(t, repo, ".tmp-jest/first")
	if err := c.gateJunkArtifacts(bead, repo, "", repo); err != nil {
		t.Fatalf("first junk drop must pass through: %v", err)
	}
	junkCommitPaths(t, repo, ".tmp-jest/second")
	if err := c.gateJunkArtifacts(bead, repo, "", repo); err != nil {
		t.Fatalf("second junk drop must still pass through: %v", err)
	}
	// ...and the junk is re-added / re-committed again on the third retry.
	junkCommitPaths(t, repo, ".tmp-jest/third")

	err := c.gateJunkArtifacts(bead, repo, "", repo)
	if err == nil {
		t.Fatal("junk re-added after two drops must be a hard rejection")
	}
	if !strings.Contains(err.Error(), "junk-artifacts") {
		t.Fatalf("rejection reason must be exactly junk-artifacts, got: %v", err)
	}
	labels := strings.Join(mock.beads["proj-junk3"].Labels, ",")
	if !strings.Contains(labels, "junk-artifacts") {
		t.Errorf("junk-artifacts label must be present on the bead, got labels: %q", labels)
	}
	if strings.Contains(labels, "escalated") {
		t.Errorf("junk rejection must leave the escalation ladder untouched, got labels: %q", labels)
	}
	for _, ev := range readEventsOfType(t, NewEventSink(sinkDir), closeGateRejectionEvent) {
		if ev.Reason == "junk-artifacts" {
			return // found the distinct rejection event
		}
	}
	t.Errorf("expected one close_gate_rejected event with reason junk-artifacts, got: %+v",
		readEventsOfType(t, NewEventSink(sinkDir), closeGateRejectionEvent))
}

// --- AC4: non-junk target-file changes are untouched ---

func TestGateJunk_ThreadCacheServiceFileNotTouched(t *testing.T) {
	gateJunkRequireImpl(t)
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "src/modules/agent/services/conversation/thread-cache.service.ts", "// base\n")

	c, _, sinkDir := junkGateCloserFor(t, repo)
	bead := &types.Bead{
		ID:          "proj-junk4",
		Title:       "GREEN: Implement thread cache",
		Project:     filepath.Base(repo),
		Status:      types.StatusInProgress,
		Description: "chain work",
	}
	junkCommitPaths(t, repo, "src/modules/agent/services/conversation/thread-cache.service.ts")

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("real source file touched in the chain must not hit the junk gate: %v", err)
	}
	for _, ev := range readEventsOfType(t, NewEventSink(sinkDir), closeGateRejectionEvent) {
		if ev.Reason == "junk-artifacts" {
			t.Errorf("junk-artifacts reason must never fire for real source paths, got: %+v", ev)
		}
	}
}

// --- AC5: non-junk off-scope files keep today's warn-only behavior ---

func TestGateJunk_NonJunkOffScopeWarnOnlyNoRejection(t *testing.T) {
	gateJunkRequireImpl(t)
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "src/thread-cache/service.ts", "export class ThreadCache {}\n")

	c, _, sinkDir := junkGateCloserFor(t, repo)
	bead := &types.Bead{
		ID:          "proj-junk5",
		Title:       "GREEN: Implement thread cache",
		Project:     filepath.Base(repo),
		Status:      types.StatusInProgress,
		Description: "chain work\n**Target Files:** src/thread-cache/service.ts",
	}
	junkCommitPaths(t, repo, "README.md")

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("non-junk off-scope file must stay warn-only (junk gate silent): %v", err)
	}
	for _, ev := range readEventsOfType(t, NewEventSink(sinkDir), closeGateRejectionEvent) {
		if ev.Reason == "junk-artifacts" {
			t.Errorf("off-scope non-junk edits must not be rejected by the junk gate, got: %+v", ev)
		}
	}
}
