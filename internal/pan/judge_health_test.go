package pan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// forceReJudgment defeats the same-signature dedup window so each Tick in a
// test reaches the runner again with unchanged candidates.
func forceReJudgment(s *Shepherd) {
	s.dedupMu.Lock()
	s.lastJudgedAt = time.Now().Add(-2 * reJudgeInterval)
	s.dedupMu.Unlock()
}

func TestJudgeDegradedAlertAfterThreshold(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{parkedOverdue("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return "", fmt.Errorf("provider unreachable")
	}
	s := New(Config{Enabled: true}, store, runner)

	var alerts []int
	s.OnJudgeFailure = func(consecutive int, err error) { alerts = append(alerts, consecutive) }

	for i := 0; i < judgeDegradedThreshold+3; i++ {
		forceReJudgment(s)
		s.Tick(context.Background())
	}

	if len(alerts) != 1 {
		t.Fatalf("alerts %v, want exactly one at threshold %d", alerts, judgeDegradedThreshold)
	}
	if alerts[0] != judgeDegradedThreshold {
		t.Errorf("alert fired at consecutive=%d, want %d", alerts[0], judgeDegradedThreshold)
	}
}

func TestJudgeRecoveredAfterOutage(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{parkedOverdue("bead-1")}}
	fail := true
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		if fail {
			return "", fmt.Errorf("provider unreachable")
		}
		return `[{"bead_id": "bead-1", "action": "continue", "message": "ok"}]`, nil
	}
	s := New(Config{Enabled: true}, store, runner)

	var recovered []int
	s.OnJudgeRecovered = func(failures int) { recovered = append(recovered, failures) }

	for i := 0; i < judgeDegradedThreshold+1; i++ {
		forceReJudgment(s)
		s.Tick(context.Background())
	}
	fail = false
	forceReJudgment(s)
	s.Tick(context.Background())

	if len(recovered) != 1 || recovered[0] != judgeDegradedThreshold+1 {
		t.Fatalf("recovered %v, want one recovery reporting %d failures", recovered, judgeDegradedThreshold+1)
	}

	// A later sub-threshold blip must NOT fire a recovery event.
	fail = true
	for i := 0; i < judgeDegradedThreshold-1; i++ {
		forceReJudgment(s)
		s.Tick(context.Background())
	}
	fail = false
	forceReJudgment(s)
	s.Tick(context.Background())
	if len(recovered) != 1 {
		t.Fatalf("recovered %v, sub-threshold blip must not report recovery", recovered)
	}
}

func TestJudgeFailureBelowThresholdSilent(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{parkedOverdue("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return "", fmt.Errorf("provider unreachable")
	}
	s := New(Config{Enabled: true}, store, runner)

	calls := 0
	s.OnJudgeFailure = func(consecutive int, err error) { calls++ }

	for i := 0; i < judgeDegradedThreshold-1; i++ {
		forceReJudgment(s)
		s.Tick(context.Background())
	}
	if calls != 0 {
		t.Fatalf("OnJudgeFailure called %d times below threshold, want 0", calls)
	}
}
