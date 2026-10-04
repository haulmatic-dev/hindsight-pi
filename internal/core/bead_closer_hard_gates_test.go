package core

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED-phase tests for FR-002: Close-time hard gates in the bead closer
// (docs/PRDs/executable-acceptance-postclose-verification-20261002.md).
//
// Contract pinned here for GREEN to implement in internal/core/bead_closer.go:
//   - Entry point: (c *BeadCloser) runCloseHardGates(bead *types.Bead,
//     projectPath, repoName, worktreePath string) error — runs before the
//     VerifiedClose LLM judge round.
//   - Gate 1 (executable acceptance): a bead whose description carries a
//     machine-parseable "**Verification Command:**" fenced block gets that
//     command executed in its worktree; non-zero exit rejects the close
//     (reason "verification-failed") with the command output attached
//     verbatim to the rejection event. No command = fail-open, plus a typed
//     "verification_command_missing" event.
//   - Gate 2 (quarantine contradiction): a close is rejected with reason
//     "closed-vs-quarantined" when branch hydra/quarantine/<beadID> exists
//     and the bead carries no "integrated" label. Rejection event carries
//     the quarantine branch as evidence. Git evidence errors fail open.
//   - Gate 3 (dependency order): closing a chain member (RED/GREEN/REFACTOR/
//     SYNTAX title prefix) while a Dependencies target is non-terminal emits
//     "close_dep_open" and rejects with reason "dep-open"; the event carries
//     the open dependency IDs. Singletons warn-only (event, no rejection).
//   - Existing VerifiedClose round/escalation semantics are unchanged:
//     rejected → reopen → attempt cap → escalate.
//
// These tests fail at RED because runCloseHardGates does not exist yet.

func hardGateConfig(maxRounds int) *Config {
	cfg := DefaultConfig()
	cfg.Validation.MaxRounds = maxRounds
	return cfg
}

func hardGateCloser(t *testing.T, cfg *Config, repo string) (*BeadCloser, *mockBeadsClient, string) {
	t.Helper()
	mock := newMockBeadsClient()
	resolver := NewProjectResolver([]types.Project{
		{Name: "test-project", Path: repo, Type: types.RepoTypeSingle},
	})
	c := NewBeadCloser(mock, nil, resolver, nil, cfg)
	sinkDir := t.TempDir()
	c.SetEventSink(NewEventSink(sinkDir))
	return c, mock, sinkDir
}

func hardGateEvents(t *testing.T, sinkDir, evType string) []Event {
	t.Helper()
	return readEventsOfType(t, NewEventSink(sinkDir), evType)
}

func gitBranch(t *testing.T, repo, name string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "branch", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch %s: %v\n%s", name, err, out)
	}
}

// --- Gate 1: executable acceptance gate ---

func TestCloseGate_VerificationCommandFailingRejectsWithVerbatimOutput(t *testing.T) {
	repo := initGitRepo(t)
	c, mock, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	bead := &types.Bead{
		ID:          "proj-vg1",
		Title:       "GREEN: Implement activity log",
		Project:     "test-project",
		Status:      types.StatusInProgress,
		Description: "Do the thing.\n**Verification Command:**\n```\necho 'verification blew up' && exit 3\n```\n",
	}
	mock.beads["proj-vg1"] = *bead

	err := c.runCloseHardGates(bead, repo, "", repo)
	if err == nil {
		t.Fatal("failing verification command must reject the close")
	}
	if !strings.Contains(err.Error(), "verification-failed") {
		t.Fatalf("rejection reason must be verification-failed, got: %v", err)
	}
	b := mock.beads["proj-vg1"]
	if b.Status != types.StatusOpen {
		t.Errorf("bead must be reopened on rejection, status = %s", b.Status)
	}
	evs := hardGateEvents(t, sinkDir, "close_gate_rejected")
	if len(evs) != 1 {
		t.Fatalf("expected exactly one close_gate_rejected event, got %d", len(evs))
	}
	if !strings.Contains(evs[0].Detail, "verification blew up") {
		t.Errorf("rejection event must carry the verbatim command output, got detail: %q", evs[0].Detail)
	}
}

func TestCloseGate_VerificationCommandPassingAllowsClose(t *testing.T) {
	repo := initGitRepo(t)
	c, _, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	bead := &types.Bead{
		ID:          "proj-vg2",
		Title:       "GREEN: Implement activity log",
		Project:     "test-project",
		Status:      types.StatusInProgress,
		Description: "Do the thing.\n**Verification Command:**\n```\nexit 0\n```\n",
	}

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("passing verification command must not reject the close: %v", err)
	}
	if evs := hardGateEvents(t, sinkDir, "close_gate_rejected"); len(evs) != 0 {
		t.Errorf("no rejection event expected on pass, got %v", evs)
	}
}

func TestCloseGate_VerificationCommandMissingFailsOpen(t *testing.T) {
	repo := initGitRepo(t)
	c, _, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	bead := &types.Bead{
		ID:          "proj-vg3",
		Title:       "GREEN: Legacy bead without a verification command",
		Project:     "test-project",
		Status:      types.StatusInProgress,
		Description: "Do the thing. No command block anywhere.",
	}

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("absence of a verification command must fail open (legacy beads): %v", err)
	}
	evs := hardGateEvents(t, sinkDir, "verification_command_missing")
	if len(evs) != 1 {
		t.Fatalf("expected exactly one verification_command_missing event, got %d", len(evs))
	}
	if evs[0].Bead != "proj-vg3" {
		t.Errorf("event must name the bead, got %q", evs[0].Bead)
	}
}

// --- Gate 2: quarantine contradiction gate ---

func TestQuarantineClose_RejectsWhileQuarantinedAndUnintegrated(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")
	gitBranch(t, repo, "hydra/quarantine/proj-qg1")
	c, mock, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	bead := &types.Bead{
		ID:      "proj-qg1",
		Title:   "GREEN: Implement activity log",
		Project: "test-project",
		Status:  types.StatusInProgress,
	}
	mock.beads["proj-qg1"] = *bead

	err := c.runCloseHardGates(bead, repo, "", repo)
	if err == nil {
		t.Fatal("close must be rejected while the worktree branch is quarantined and unintegrated")
	}
	if !strings.Contains(err.Error(), "closed-vs-quarantined") {
		t.Fatalf("rejection reason must be closed-vs-quarantined, got: %v", err)
	}
	evs := hardGateEvents(t, sinkDir, "close_gate_rejected")
	if len(evs) != 1 {
		t.Fatalf("expected exactly one close_gate_rejected event, got %d", len(evs))
	}
	if !strings.Contains(evs[0].Detail, "hydra/quarantine/proj-qg1") {
		t.Errorf("rejection event must carry the quarantine branch evidence, got detail: %q", evs[0].Detail)
	}
}

func TestQuarantineClose_PassesWhenIntegrated(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")
	gitBranch(t, repo, "hydra/quarantine/proj-qg2")
	c, _, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	bead := &types.Bead{
		ID:      "proj-qg2",
		Title:   "GREEN: Implement activity log",
		Project: "test-project",
		Status:  types.StatusInProgress,
		Labels:  []string{"integrated"},
	}

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("close must pass once the quarantine has an integration record: %v", err)
	}
	if evs := hardGateEvents(t, sinkDir, "close_gate_rejected"); len(evs) != 0 {
		t.Errorf("no rejection event expected, got %v", evs)
	}
}

func TestQuarantineClose_PassesWithoutQuarantineBranch(t *testing.T) {
	repo := initGitRepo(t)
	writeAndCommit(t, repo, "x/a.go", "package x\n")
	c, _, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	bead := &types.Bead{
		ID:      "proj-qg3",
		Title:   "GREEN: Implement activity log",
		Project: "test-project",
		Status:  types.StatusInProgress,
	}

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("no quarantine branch means no contradiction: %v", err)
	}
	if evs := hardGateEvents(t, sinkDir, "close_gate_rejected"); len(evs) != 0 {
		t.Errorf("no rejection event expected, got %v", evs)
	}
}

// --- Gate 3: dependency-order gate ---

func TestCloseDep_ChainMemberWithOpenDependencyRejects(t *testing.T) {
	repo := initGitRepo(t)
	c, mock, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	dep := &types.Bead{ID: "proj-dg0", Title: "RED: Write tests", Project: "test-project", Status: types.StatusInProgress}
	mock.beads["proj-dg0"] = *dep
	bead := &types.Bead{
		ID:           "proj-dg1",
		Title:        "GREEN: Implement activity log",
		Project:      "test-project",
		Status:       types.StatusInProgress,
		Dependencies: []types.BeadID{"proj-dg0"},
	}
	mock.beads["proj-dg1"] = *bead

	err := c.runCloseHardGates(bead, repo, "", repo)
	if err == nil {
		t.Fatal("chain member close with a non-terminal dependency must be rejected")
	}
	if !strings.Contains(err.Error(), "dep-open") {
		t.Fatalf("rejection reason must be dep-open, got: %v", err)
	}
	evs := hardGateEvents(t, sinkDir, "close_dep_open")
	if len(evs) == 0 {
		t.Fatal("expected a close_dep_open event")
	}
	if !strings.Contains(evs[0].Detail, "proj-dg0") {
		t.Errorf("dependency event must carry the open dependency evidence, got detail: %q", evs[0].Detail)
	}
}

func TestCloseDep_ChainMemberPassesWhenDependenciesTerminal(t *testing.T) {
	repo := initGitRepo(t)
	c, mock, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	dep := &types.Bead{ID: "proj-dg2", Title: "RED: Write tests", Project: "test-project", Status: types.StatusCompleted}
	mock.beads["proj-dg2"] = *dep
	bead := &types.Bead{
		ID:           "proj-dg3",
		Title:        "GREEN: Implement activity log",
		Project:      "test-project",
		Status:       types.StatusInProgress,
		Dependencies: []types.BeadID{"proj-dg2"},
	}
	mock.beads["proj-dg3"] = *bead

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("terminal dependencies must not reject the close: %v", err)
	}
	if evs := hardGateEvents(t, sinkDir, "close_gate_rejected"); len(evs) != 0 {
		t.Errorf("no rejection event expected, got %v", evs)
	}
}

func TestCloseDep_SingletonWarnOnly(t *testing.T) {
	repo := initGitRepo(t)
	c, mock, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	dep := &types.Bead{ID: "proj-dg4", Title: "Add validation", Project: "test-project", Status: types.StatusInProgress}
	mock.beads["proj-dg4"] = *dep
	bead := &types.Bead{
		ID:           "proj-dg5",
		Title:        "Add validation coverage", // singleton: no chain-phase prefix
		Project:      "test-project",
		Status:       types.StatusInProgress,
		Dependencies: []types.BeadID{"proj-dg4"},
	}
	mock.beads["proj-dg5"] = *bead

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("singleton must close with open children (warn-only): %v", err)
	}
	if evs := hardGateEvents(t, sinkDir, "close_gate_rejected"); len(evs) != 0 {
		t.Errorf("singleton must not be rejected, got rejection events %v", evs)
	}
	evs := hardGateEvents(t, sinkDir, "close_dep_open")
	if len(evs) == 0 {
		t.Fatal("expected a close_dep_open warning event for the singleton")
	}
	if !strings.Contains(strings.ToLower(evs[0].Detail), "warn") {
		t.Errorf("singleton close_dep_open event must be marked as warning, got detail: %q", evs[0].Detail)
	}
}

// --- Existing VerifiedClose round/escalation semantics unchanged ---

func TestVerifiedClose_HardGateRejectionFollowsRoundCapAndEscalates(t *testing.T) {
	repo := initGitRepo(t)
	cfg := hardGateConfig(1)
	rm := NewRetryManager(time.Second, time.Minute)
	vs := NewValidationService(cfg, rm, nil)
	c, mock, sinkDir := hardGateCloser(t, cfg, repo)
	c.validationService = vs
	bead := &types.Bead{
		ID:          "proj-eg1",
		Title:       "GREEN: Implement activity log",
		Project:     "test-project",
		Status:      types.StatusInProgress,
		Description: "**Verification Command:**\n```\nexit 9\n```\n",
	}
	mock.beads["proj-eg1"] = *bead

	// Round 1: rejected and reopened.
	err1 := c.runCloseHardGates(bead, repo, "", repo)
	if err1 == nil || !strings.Contains(err1.Error(), "verification-failed") {
		t.Fatalf("first close must be rejected with verification-failed, got: %v", err1)
	}
	if got := mock.beads["proj-eg1"].Status; got != types.StatusOpen {
		t.Fatalf("round-1 rejection must reopen the bead, status = %s", got)
	}
	// The reject/reopen loop must have counted this attempt.
	if n := rm.GetValidationRetryCount("proj-eg1"); n != 1 {
		t.Fatalf("round-1 rejection must count one validation retry, got %d", n)
	}

	// Round 2: attempt cap reached → escalate to a human, not an infinite loop.
	err2 := c.runCloseHardGates(bead, repo, "", repo)
	if err2 == nil || !strings.Contains(err2.Error(), "escalated") {
		t.Fatalf("second close must escalate past the round cap, got: %v", err2)
	}
	b := mock.beads["proj-eg1"]
	joined := strings.Join(b.Labels, ",")
	if !strings.Contains(joined, "needs-human") || !strings.Contains(joined, "verified-close-escalated") {
		t.Errorf("escalated bead must carry needs-human + verified-close-escalated labels, got %v", b.Labels)
	}
	if evs := hardGateEvents(t, sinkDir, "bead.escalated"); len(evs) != 1 {
		t.Fatalf("expected exactly one bead.escalated event, got %d", len(evs))
	}
}
