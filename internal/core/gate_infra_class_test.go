package core

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// GREEN-phase tests for infra classification at the executable-acceptance
// gate (bead hydra-b2qe). Contract:
//   - exit 127 (command not found, e.g. `sh: 1: jest: not found`) classifies
//     as infrastructure, not a project fault; exit 0 is a pass.
//   - An infra-shaped verification failure reopens the bead WITH a reason but
//     WITHOUT consuming a verification strike.
//   - The 4th consecutive infra rejection escalates with reason infra-stuck
//     and an escalation label, leaving the bead in_progress.
//   - Project-shaped failures (exit 1/2/3/...) keep the existing
//     verification-failed strike path.

func TestGateInfra_ClassificationTable(t *testing.T) {
	cases := []struct {
		name      string
		exitCode  int
		output    string
		wantInfra bool
	}{
		{"jest not found", 127, "sh: 1: jest: not found", true},
		{"command not found without output", 127, "", true},
		{"real test failure", 1, "--- FAIL: TestThing (0.01s)", false},
		{"shell exit 3", 3, "exit 3", false},
		{"pass is not a failure", 0, "--- PASS: TestThing (0.01s)", false},
		{"signal death", -1, "", true},
		{"operator kill", -15, "Terminated", true},
	}
	for _, tc := range cases {
		if got := isInfraGateFailure(tc.exitCode, tc.output); got != tc.wantInfra {
			t.Errorf("%s: isInfraGateFailure(%d, %q) = %v, want %v", tc.name, tc.exitCode, tc.output, got, tc.wantInfra)
		}
	}
}

func gateInfraBead(id, cmd string, labels []string) *types.Bead {
	desc := "Do the thing.\n**Verification Command:**\n```\n" + cmd + "\n```\n"
	return &types.Bead{
		ID:          types.BeadID(id),
		Title:       "GREEN: Implement activity log",
		Project:     "test-project",
		Status:      types.StatusInProgress,
		Labels:      labels,
		Description: desc,
	}
}

func TestGateInfra_CommandNotFoundReopensWithoutStrike(t *testing.T) {
	repo := initGitRepo(t)
	cfg := hardGateConfig(3)
	rm := NewRetryManager(time.Second, time.Minute)
	c, mock, sinkDir := hardGateCloser(t, cfg, repo)
	c.validationService = NewValidationService(cfg, rm, nil)
	bead := gateInfraBead("proj-ig1", "exit 127", nil)
	mock.beads["proj-ig1"] = *bead

	err := c.runCloseHardGates(bead, repo, "", repo)
	if err == nil {
		t.Fatal("exit-127 verification command must reject the close")
	}
	if !strings.Contains(err.Error(), "infra-failure") {
		t.Fatalf("rejection reason must be infra-failure, got: %v", err)
	}
	b := mock.beads["proj-ig1"]
	if b.Status != types.StatusOpen {
		t.Errorf("infra rejection must reopen the bead, status = %s", b.Status)
	}
	joined := strings.Join(b.Labels, ",")
	if !strings.Contains(joined, "infra-reject-1") {
		t.Errorf("bead must carry the durable infra counter label, got %v", b.Labels)
	}
	if strings.Contains(joined, "verified-close-reject-attempt-") {
		t.Errorf("infra rejection must not consume a verification strike, got %v", b.Labels)
	}
	if n := rm.GetValidationRetryCount("proj-ig1"); n != 0 {
		t.Errorf("infra rejection must not increment the retry manager, got %d", n)
	}
	evs := hardGateEvents(t, sinkDir, "close_gate_rejected")
	if len(evs) != 1 {
		t.Fatalf("expected exactly one close_gate_rejected event, got %d", len(evs))
	}
	if evs[0].Reason != "infra-failure" {
		t.Errorf("rejection event reason must be infra-failure, got %q", evs[0].Reason)
	}
	if !strings.Contains(evs[0].Detail, "127") {
		t.Errorf("rejection event must carry the exit-code evidence, got detail: %q", evs[0].Detail)
	}
}

func TestGateInfra_FourthConsecutiveInfraRejectEscalates(t *testing.T) {
	repo := initGitRepo(t)
	cfg := hardGateConfig(3)
	rm := NewRetryManager(time.Second, time.Minute)
	c, mock, sinkDir := hardGateCloser(t, cfg, repo)
	c.validationService = NewValidationService(cfg, rm, nil)
	bead := gateInfraBead("proj-ig2", "exit 127", []string{"infra-reject-3"})
	mock.beads["proj-ig2"] = *bead

	err := c.runCloseHardGates(bead, repo, "", repo)
	if err == nil {
		t.Fatal("4th consecutive infra rejection must escalate, not pass")
	}
	if !strings.Contains(err.Error(), "infra-stuck") {
		t.Fatalf("escalation reason must be infra-stuck, got: %v", err)
	}
	b := mock.beads["proj-ig2"]
	if b.Status != types.StatusInProgress {
		t.Errorf("escalated bead must be left in_progress (no reopen loop), status = %s", b.Status)
	}
	joined := strings.Join(b.Labels, ",")
	for _, want := range []string{"needs-human", "escalated", "infra-stuck"} {
		if !strings.Contains(joined, want) {
			t.Errorf("escalated bead must carry %q label, got %v", want, b.Labels)
		}
	}
	if n := rm.GetValidationRetryCount("proj-ig2"); n != 0 {
		t.Errorf("infra escalation must not consume verification strikes, got %d", n)
	}
	evs := hardGateEvents(t, sinkDir, "bead.escalated")
	if len(evs) != 1 {
		t.Fatalf("expected exactly one bead.escalated event, got %d", len(evs))
	}
	if evs[0].Reason != "infra-stuck" {
		t.Errorf("escalation event reason must be infra-stuck, got %q", evs[0].Reason)
	}
}

func TestGateInfra_SuccessClearsInfraCounter(t *testing.T) {
	repo := initGitRepo(t)
	c, mock, sinkDir := hardGateCloser(t, hardGateConfig(3), repo)
	bead := gateInfraBead("proj-ig3", "exit 0", []string{"infra-reject-2"})
	mock.beads["proj-ig3"] = *bead

	if err := c.runCloseHardGates(bead, repo, "", repo); err != nil {
		t.Fatalf("passing verification command must not reject the close: %v", err)
	}
	b := mock.beads["proj-ig3"]
	for _, l := range b.Labels {
		if strings.HasPrefix(l, "infra-reject-") {
			t.Errorf("passing gate must clear stale infra counter labels, still has %v", b.Labels)
		}
	}
	if evs := hardGateEvents(t, sinkDir, "close_gate_rejected"); len(evs) != 0 {
		t.Errorf("no rejection event expected on pass, got %v", evs)
	}
}

func TestGateInfra_ProjectFailureStaysVerificationFailed(t *testing.T) {
	repo := initGitRepo(t)
	cfg := hardGateConfig(3)
	rm := NewRetryManager(time.Second, time.Minute)
	c, mock, sinkDir := hardGateCloser(t, cfg, repo)
	c.validationService = NewValidationService(cfg, rm, nil)
	bead := gateInfraBead("proj-ig4", "echo 'test blew up' && exit 1", nil)
	mock.beads["proj-ig4"] = *bead

	err := c.runCloseHardGates(bead, repo, "", repo)
	if err == nil {
		t.Fatal("project failure must reject the close")
	}
	if !strings.Contains(err.Error(), "verification-failed") {
		t.Fatalf("project failure reason must stay verification-failed, got: %v", err)
	}
	b := mock.beads["proj-ig4"]
	joined := strings.Join(b.Labels, ",")
	if strings.Contains(joined, "infra-reject-") {
		t.Errorf("project failure must not touch the infra counter, got %v", b.Labels)
	}
	if n := rm.GetValidationRetryCount("proj-ig4"); n != 1 {
		t.Errorf("project failure must consume exactly one verification strike, got %d", n)
	}
	evs := hardGateEvents(t, sinkDir, "close_gate_rejected")
	if len(evs) != 1 || evs[0].Reason != "verification-failed" {
		t.Errorf("project rejection event must carry reason verification-failed, got %+v", evs)
	}
}
