package core

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// countErrBeadsClient counts UpdateBeadAgentContext calls and can be flipped
// between failing and succeeding mid-test.
type countErrBeadsClient struct {
	*mockBeadsClient
	calls atomic.Int64
	fail  atomic.Bool
}

func (c *countErrBeadsClient) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	c.calls.Add(1)
	if c.fail.Load() {
		return errors.New("br update --agent-context: exit status 7")
	}
	return c.mockBeadsClient.UpdateBeadAgentContext(projectPath, repoName, beadID, ctxJSON)
}

func newBreakerSpawner(t *testing.T, client BeadsClient) *WorkerSpawner {
	t.Helper()
	s, _ := packTestSpawner(t, client, []types.BeadID{"root-1"}, t.TempDir())
	s.packBreakers = map[string]*packBreakerState{}
	return s
}

func TestPackBreaker_SkipsDuringBackoff(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "CreateShortlink handler"}
	client := &countErrBeadsClient{mockBeadsClient: mock}
	client.fail.Store(true)
	s := newBreakerSpawner(t, client)
	s.beadsClient = client
	bead := mock.beads["root-1"]

	// First failure schedules a 1m backoff: an immediate second call must not
	// reach the store (this is what damps the 2-3s hot-retry loop).
	s.ensureContextPack(bead, dir)
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("calls after first attempt = %d, want 1", got)
	}
	s.ensureContextPack(bead, dir)
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("backoff not honored: calls = %d, want 1", got)
	}

	// Once the backoff elapses, the breaker lets another pass through.
	future := time.Now().Add(2 * time.Minute)
	if !s.packBreakerAllowed(bead.Project, future) {
		t.Fatal("breaker still closed after backoff window elapsed")
	}
}

func TestPackBreaker_OpensWithTypedEvent(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "CreateShortlink handler"}
	client := &countErrBeadsClient{mockBeadsClient: mock}
	client.fail.Store(true)
	s := newBreakerSpawner(t, client)
	s.beadsClient = client
	bead := mock.beads["root-1"]

	sinkDir := t.TempDir()
	s.eventSink = NewEventSink(sinkDir)

	now := time.Now()
	for i := 0; i < packBreakerThreshold; i++ {
		s.packBreakerFailure(bead.Project, string(bead.ID), now.Add(time.Duration(i)*time.Second), errors.New("exit 7"))
	}
	st := s.packBreakers[bead.Project]
	if st == nil || !st.openNotified {
		t.Fatal("breaker did not open after threshold failures")
	}
	if st.openUntil.Before(now.Add(packBreakerBaseBackoff)) {
		t.Fatalf("openUntil %v earlier than first backoff", st.openUntil)
	}
	// openNotified set exactly once: further failures stay quiet.
	s.packBreakerFailure(bead.Project, string(bead.ID), now, errors.New("exit 7"))
	if s.packBreakers[bead.Project].openNotified != true {
		t.Fatal("state lost on repeat failure")
	}

	// Exactly one typed telemetry event on the fail-open stream.
	data, err := os.ReadFile(filepath.Join(sinkDir, now.UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("event file missing: %v", err)
	}
	if n := bytes.Count(data, []byte(PackBreakerOpenEventType)); n != 1 {
		t.Fatalf("pack_breaker_open events = %d, want 1 (file: %s)", n, data)
	}
}

func TestPackBreaker_SuccessResets(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "CreateShortlink handler"}
	client := &countErrBeadsClient{mockBeadsClient: mock}
	s := newBreakerSpawner(t, client)
	s.beadsClient = client
	bead := mock.beads["root-1"]

	client.fail.Store(true)
	for i := 0; i < packBreakerThreshold; i++ {
		s.packBreakerFailure(bead.Project, string(bead.ID), time.Now(), errors.New("exit 7"))
	}
	client.fail.Store(false)
	// Backoff still active; simulate expiry then a successful pass.
	s.packBreakerAllowed(bead.Project, time.Now().Add(time.Hour))
	if st := s.packBreakers[bead.Project]; st.failures == 0 {
		t.Fatal("precondition: breaker should hold failures")
	}
	s.packBreakerSuccess(bead.Project, time.Now())
	if st := s.packBreakers[bead.Project]; st.failures != 0 {
		t.Fatalf("success did not reset breaker: %+v", st)
	}
	// Fresh state: an immediate ensureContextPack reaches the store again.
	before := client.calls.Load()
	s.ensureContextPack(bead, dir)
	if client.calls.Load() <= before {
		t.Fatal("successful write pass did not reach the store after reset")
	}
}
