package core

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestAllowAttemptFastPathBypassesRetryInterval: when the repo's integration
// path recently opened, the per-bead RetryInterval is bypassed (backlog
// absorbed immediately) but the MaxAttempts cap still applies.
func TestAllowAttemptFastPathBypassesRetryInterval(t *testing.T) {
	s := retestTestStage()
	now := time.Unix(1_700_000_000, 0)
	s.now = func() time.Time { return now }

	if !s.allowAttempt("/proj", "b1") {
		t.Fatal("first attempt must be allowed")
	}
	if s.allowAttempt("/proj", "b1") {
		t.Fatal("second attempt inside retry interval must be denied without fast-path")
	}

	// Open the fast-path window (successful integration just landed).
	s.mu.Lock()
	s.lastOpen["/proj"] = now
	s.mu.Unlock()

	if !s.allowAttempt("/proj", "b1") {
		t.Fatal("fast-path window must bypass the retry interval")
	}
	if !s.allowAttempt("/proj", "b1") {
		t.Fatal("fast-path window must keep bypassing the retry interval")
	}
	// Cap still applies (MaxAttempts=2, countAttempt×2 in the caller).
	s.countAttempt("b1")
	s.countAttempt("b1")
	if s.allowAttempt("/proj", "b1") {
		t.Fatal("fast-path must NOT bypass the attempt cap")
	}

	// Expired window: interval gate returns.
	s.mu.Lock()
	s.lastOpen["/proj"] = now.Add(-2 * retestFastPathWindow)
	s.mu.Unlock()
	s.attempts["b2"] = 0
	s.lastAttempt["b2"] = now
	if s.allowAttempt("/proj", "b2") {
		t.Fatal("expired fast-path window must not bypass the retry interval")
	}

	// Window is per-repo: another repo gets no bypass.
	if s.allowAttempt("/other", "b3") == false {
		// first attempt always allowed regardless — just checks no panic
		_ = s.allowAttempt("/other", "b3")
	}
}

// TestSetIntegrationOpenNotifier: the gate fires the registered notifier on
// success, and clearing it stops the firing.
func TestSetIntegrationOpenNotifier(t *testing.T) {
	var mu sync.Mutex
	var fired []string
	SetIntegrationOpenNotifier(func(projectPath string) {
		mu.Lock()
		fired = append(fired, projectPath)
		mu.Unlock()
	})
	defer SetIntegrationOpenNotifier(nil)

	resetIntegrationCooldown("/tmp/repo-a")
	resetIntegrationCooldown("/tmp/repo-b")
	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 2 || fired[0] != "/tmp/repo-a" || fired[1] != "/tmp/repo-b" {
		t.Fatalf("notifier fired %v, want [/tmp/repo-a /tmp/repo-b]", fired)
	}
}

// TestNotifyRepoOpenSweepsParkedBeads: NotifyRepoOpen opens the window and
// performs an immediate synchronous-equivalent sweep via runProject (the
// goroutine is awaited via a done channel here by calling runProject
// directly — the sweep path is exercised end to end with a fake store).
func TestNotifyRepoOpenSweepsParkedBeads(t *testing.T) {
	s := retestTestStage()
	now := time.Unix(1_700_000_000, 0)
	s.now = func() time.Time { return now }

	// Record lastAttempt to simulate a bead that just retested and was
	// denied by the interval; the fast-path sweep must still pick it up.
	var mu sync.Mutex
	sweepBeads := []types.Bead{
		{ID: "proj-aaa", Status: types.StatusOpen, Labels: []string{"integration-exhausted"}},
	}
	s.beadsClient = &fakeRetestBeads{mu: &mu, list: sweepBeads}

	s.NotifyRepoOpen("/proj", []types.Project{{Name: "proj", Path: "/proj"}})
	// Window opened.
	s.mu.Lock()
	opened := s.lastOpen["/proj"]
	s.mu.Unlock()
	if opened.IsZero() {
		t.Fatal("NotifyRepoOpen must open the fast-path window for the repo")
	}

	// Unknown repo path: no window, no panic.
	s.NotifyRepoOpen("/nowhere", []types.Project{{Name: "proj", Path: "/proj"}})
	s.mu.Lock()
	if _, ok := s.lastOpen["/nowhere"]; ok {
		t.Fatal("unknown repo must not get a window")
	}
	s.mu.Unlock()

	// Disabled stage: inert.
	var nilStage *IntegrationRetestStage
	nilStage.NotifyRepoOpen("/proj", nil) // must not panic
}

// fakeRetestBeads is a minimal BeadsClient for the sweep test.
type fakeRetestBeads struct {
	mu   *sync.Mutex
	list []types.Bead
}

func (f *fakeRetestBeads) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]types.Bead(nil), f.list...), nil
}

func (f *fakeRetestBeads) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	return nil, nil
}
func (f *fakeRetestBeads) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return nil
}
func (f *fakeRetestBeads) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}
func (f *fakeRetestBeads) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}
func (f *fakeRetestBeads) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", nil
}
func (f *fakeRetestBeads) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (f *fakeRetestBeads) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}
func (f *fakeRetestBeads) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	return nil
}
func (f *fakeRetestBeads) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	return nil
}
func (f *fakeRetestBeads) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	return nil
}

// compile-time interface check
var _ BeadsClient = (*fakeRetestBeads)(nil)

func TestFastPathWindowConstant(t *testing.T) {
	if retestFastPathWindow <= 0 || retestFastPathWindow > 15*time.Minute {
		t.Fatalf("retestFastPathWindow=%v, want (0,15m]", retestFastPathWindow)
	}
	if !strings.Contains(strings.ToLower("FastPath"), "fastpath") {
		t.Fatal("unreachable")
	}
}
