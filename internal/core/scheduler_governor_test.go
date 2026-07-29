package core

import (
	"io"
	"log"
	"strconv"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// testGovernorConfig returns a governor configuration with a one-second cooldown.
// Recovery tests advance the clock via SetNow, so real sleeps are not needed.
func testGovernorConfig() GovernorConfig {
	return GovernorConfig{
		Enabled:           true,
		WindowSize:        8,
		DecreaseThreshold: 0.5,
		IncreaseThreshold: 0.125,
		CooldownSeconds:   1,
	}
}

// newTestScheduler builds a real Overseer and Scheduler with external services
// disabled so the integration test exercises only the governor and the slot
// arithmetic. No internal dependencies are mocked.
func newTestScheduler(t *testing.T, maxWorkers int, governorCfg GovernorConfig) (*Scheduler, *Overseer) {
	t.Helper()

	origLog := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(origLog) })

	cfg := DefaultConfig()
	cfg.Overseer.MaxWorkers = maxWorkers
	cfg.Overseer.ConcurrencyGovernor = governorCfg
	cfg.Overseer.ResourceMonitoring.Enabled = false
	cfg.Overseer.ChainDetection.Enabled = false
	cfg.Intelligence.Graphify.Enabled = false
	cfg.Intelligence.GPTCache.Enabled = false
	cfg.Intelligence.Hindsight.Enabled = false
	cfg.Intelligence.Patterns.Enabled = false
	cfg.Intelligence.SettledState.Enabled = false
	cfg.Intelligence.CollectiveUnconscious.Enabled = false
	cfg.MCP.Enabled = false
	cfg.API.Enabled = false
	cfg.Agents.Enabled = false

	o := NewOverseer(cfg)
	s := NewScheduler(o)
	return s, o
}

// addActiveWorkers inserts placeholder workers into the overseer's active map
// without spawning real processes.
func addActiveWorkers(t *testing.T, o *Overseer, count int) {
	t.Helper()
	o.workerMutex.Lock()
	defer o.workerMutex.Unlock()
	for i := 0; i < count; i++ {
		id := "w" + strconv.Itoa(i)
		bid := types.BeadID("b" + strconv.Itoa(i))
		o.activeWorkers[id] = &types.Worker{ID: id, BeadID: bid}
	}
}

func TestScheduler_GovernorEnabled_ShrinksAvailableSlots(t *testing.T) {
	s, o := newTestScheduler(t, 5, testGovernorConfig())

	if got := s.effectiveMaxWorkers(5); got != 5 {
		t.Fatalf("effective max before outcomes = %d, want 5", got)
	}

	// Record a full window of provider-shaped failures to trigger a decrease.
	for i := 0; i < 8; i++ {
		o.concurrencyGovernor.RecordOutcome(false)
	}
	if got := s.effectiveMaxWorkers(5); got != 3 {
		t.Fatalf("effective max after first decrease = %d, want 3", got)
	}

	addActiveWorkers(t, o, 1)
	if got := s.GetAvailableWorkerSlots(); got != 2 {
		t.Fatalf("available slots with limit 3 and 1 active = %d, want 2", got)
	}

	// Another full window of failures pushes the limit down to 2.
	for i := 0; i < 8; i++ {
		o.concurrencyGovernor.RecordOutcome(false)
	}
	if got := s.effectiveMaxWorkers(5); got != 2 {
		t.Fatalf("effective max after second decrease = %d, want 2", got)
	}
	if got := s.GetAvailableWorkerSlots(); got != 1 {
		t.Fatalf("available slots with limit 2 and 1 active = %d, want 1", got)
	}

	// A third window reaches the floor of 1.
	for i := 0; i < 8; i++ {
		o.concurrencyGovernor.RecordOutcome(false)
	}
	if got := s.effectiveMaxWorkers(5); got != 1 {
		t.Fatalf("effective max at floor = %d, want 1", got)
	}
	if got := s.GetAvailableWorkerSlots(); got != 0 {
		t.Fatalf("available slots with limit 1 and 1 active = %d, want 0", got)
	}
}

func TestScheduler_GovernorDisabled_BehaviourUnchanged(t *testing.T) {
	s, o := newTestScheduler(t, 5, GovernorConfig{Enabled: false})

	if o.concurrencyGovernor != nil {
		t.Fatal("governor must not be constructed when disabled")
	}
	if got := s.effectiveMaxWorkers(5); got != 5 {
		t.Fatalf("effective max with disabled governor = %d, want 5", got)
	}

	addActiveWorkers(t, o, 2)
	if got := s.GetAvailableWorkerSlots(); got != 3 {
		t.Fatalf("available slots with disabled governor = %d, want 3", got)
	}
}

func TestScheduler_GovernorEnabled_AbsentConfig_BehaviourUnchanged(t *testing.T) {
	// Default OverseerConfig has an empty ConcurrencyGovernor block, so the
	// governor is not constructed and slot arithmetic stays unchanged.
	s, o := newTestScheduler(t, 4, GovernorConfig{})

	if o.concurrencyGovernor != nil {
		t.Fatal("governor must not be constructed when the config block is empty")
	}
	if got := s.effectiveMaxWorkers(4); got != 4 {
		t.Fatalf("effective max with absent config = %d, want 4", got)
	}

	addActiveWorkers(t, o, 1)
	if got := s.GetAvailableWorkerSlots(); got != 3 {
		t.Fatalf("available slots with absent config = %d, want 3", got)
	}
}

func TestScheduler_GovernorRecoversOneSlotPerWindow(t *testing.T) {
	s, o := newTestScheduler(t, 5, testGovernorConfig())

	for i := 0; i < 8; i++ {
		o.concurrencyGovernor.RecordOutcome(false)
	}
	if got := s.effectiveMaxWorkers(5); got != 3 {
		t.Fatalf("effective max after decrease = %d, want 3", got)
	}

	// Advance past the cooldown so a full window of successes can raise the limit.
	o.concurrencyGovernor.SetNow(func() time.Time { return time.Now().Add(2 * time.Second) })
	for i := 0; i < 8; i++ {
		o.concurrencyGovernor.RecordOutcome(true)
	}
	if got := s.effectiveMaxWorkers(5); got != 4 {
		t.Fatalf("effective max after one recovery window = %d, want 4", got)
	}
	if got := s.GetAvailableWorkerSlots(); got != 4 {
		t.Fatalf("available slots after recovery to 4 = %d, want 4", got)
	}
}
