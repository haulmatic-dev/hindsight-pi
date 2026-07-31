package core

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// PoolConfig configures the shared provider concurrency pool (Shepherd B3).
type PoolConfig struct {
	Enabled bool `yaml:"enabled"`
	// Size is the provider's concurrent-request budget. Default 4.
	Size int `yaml:"size"`
	// AcquireTimeoutSeconds bounds how long a caller waits for a slot. Default 120.
	AcquireTimeoutSeconds int `yaml:"acquire_timeout_seconds"`
	// Weights maps a consumer name to the number of slots it occupies. Config
	// rather than constants because a worker's real stream count has to be
	// measured, not guessed — see the pool_saturated events.
	Weights map[string]int `yaml:"weights"`
	// SaturationThresholdSeconds is how long a wait must last before it is
	// reported as saturation. Default 5.
	SaturationThresholdSeconds int `yaml:"saturation_threshold_seconds"`
}

func applyPoolDefaults(cfg *PoolConfig) {
	if cfg.Size <= 0 {
		cfg.Size = 4
	}
	if cfg.AcquireTimeoutSeconds <= 0 {
		cfg.AcquireTimeoutSeconds = 120
	}
	if cfg.SaturationThresholdSeconds <= 0 {
		cfg.SaturationThresholdSeconds = 5
	}
}

// PoolStats is a point-in-time view of pool occupancy, for logging and events.
type PoolStats struct {
	Size       int
	InUse      int
	Waiting    int
	ByConsumer map[string]int
}

// poolWaiter is one queued Acquire call. Waiters are granted strictly in
// arrival order.
type poolWaiter struct {
	consumer string
	weight   int
	ready    chan struct{}
	// granted guards against a race between a Release granting this waiter and
	// the waiter giving up on timeout: exactly one of the two must win.
	granted bool
}

// ProviderPool bounds the number of concurrent provider requests across every
// consumer in the daemon — worker spawns, the gap adjudicator, and validation.
//
// The problem it solves: the provider plan allows a fixed number of concurrent
// requests, and hydra had four independent consumers of that budget with no
// mutual awareness. Each fired whenever it wanted, the provider refused the
// overflow, and the refusals surfaced as worker failures. Measured 2026-07-30:
// 41 refusals and zero successful completions in 90 minutes while the AIMD
// governor walked the worker limit down to 1 chasing a ceiling whose cause it
// could not see. The fleet was competing with hydra's own judgment calls.
//
// Capping max_workers cannot fix that, because it gates one consumer against a
// budget shared by four. This pool gates all of them, and turns contention into
// waiting (measurable) instead of refusals (misread as provider failure).
//
// Fairness is strict FIFO. Priority deliberately does not live here: bv and the
// DAG already decide which work matters, and a second ordering scheme in the
// pool would silently reorder those decisions.
//
// A nil *ProviderPool is a usable no-op, so callers never nil-check.
type ProviderPool struct {
	mu         sync.Mutex
	size       int
	inUse      int
	byConsumer map[string]int
	queue      []*poolWaiter

	timeout       time.Duration
	saturationAt  time.Duration
	weights       map[string]int
	now           func() time.Time
	events        *EventSink
	lastEventPer  map[string]time.Time
	eventThrottle time.Duration
}

// NewProviderPool creates a pool sized to the provider's concurrency budget.
// size <= 0 yields a disabled pool whose Acquire never blocks.
func NewProviderPool(size int, cfg PoolConfig) *ProviderPool {
	applyPoolDefaults(&cfg)
	if size <= 0 {
		size = cfg.Size
	}
	return &ProviderPool{
		size:          size,
		byConsumer:    map[string]int{},
		timeout:       time.Duration(cfg.AcquireTimeoutSeconds) * time.Second,
		saturationAt:  time.Duration(cfg.SaturationThresholdSeconds) * time.Second,
		weights:       cfg.Weights,
		now:           time.Now,
		lastEventPer:  map[string]time.Time{},
		eventThrottle: time.Minute,
	}
}

// SetEventSink attaches the structured event stream. Nil stays a no-op.
func (p *ProviderPool) SetEventSink(s *EventSink) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = s
}

// WeightFor returns the configured slot cost for a consumer, defaulting to 1.
func (p *ProviderPool) WeightFor(consumer string) int {
	if p == nil {
		return 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if w, ok := p.weights[consumer]; ok && w > 0 {
		return w
	}
	return 1
}

// noopRelease is returned whenever no slots were actually taken, so callers can
// always defer the result without checking it for nil.
func noopRelease() {}

// Acquire blocks until weight slots are free, then returns a release closure.
// It honours ctx cancellation and the configured timeout: a caller that cannot
// get a slot is expected to give up and be retried by its own existing path
// (the scheduler's next tick, the compliance cycle's next pass) rather than
// block forever.
//
// The returned release is always non-nil when err is nil, and is idempotent —
// a leaked slot would be worse than a refused request.
func (p *ProviderPool) Acquire(ctx context.Context, consumer string, weight int) (func(), error) {
	if p == nil {
		return noopRelease, nil
	}
	if weight <= 0 {
		weight = 1
	}

	p.mu.Lock()
	if p.size <= 0 {
		p.mu.Unlock()
		return noopRelease, nil
	}
	// A weight larger than the whole pool would wait forever. Clamp it: the
	// caller still gets exclusive use, which is the closest honest answer.
	if weight > p.size {
		weight = p.size
	}
	// Fast path: only when nobody is queued, so a burst of light callers cannot
	// starve a heavier waiter that arrived first.
	if len(p.queue) == 0 && p.inUse+weight <= p.size {
		p.take(consumer, weight)
		p.mu.Unlock()
		return p.releaser(consumer, weight), nil
	}

	w := &poolWaiter{consumer: consumer, weight: weight, ready: make(chan struct{})}
	p.queue = append(p.queue, w)
	start := p.now()
	p.mu.Unlock()

	timer := time.NewTimer(p.timeout)
	defer timer.Stop()

	select {
	case <-w.ready:
		p.reportSaturation(consumer, p.now().Sub(start))
		return p.releaser(consumer, weight), nil
	case <-ctx.Done():
		return noopRelease, p.abandon(w, consumer, ctx.Err())
	case <-timer.C:
		return noopRelease, p.abandon(w, consumer,
			fmt.Errorf("provider pool: timed out after %s waiting for %d slot(s)", p.timeout, weight))
	}
}

// abandon removes a waiter that gave up. It must re-drain, because the departing
// waiter may have been the queue head blocking others behind it.
func (p *ProviderPool) abandon(w *poolWaiter, consumer string, cause error) error {
	p.mu.Lock()
	if w.granted {
		// Release granted the slots between our select firing and this lock —
		// we own them now, so hand them straight back rather than leaking.
		p.mu.Unlock()
		p.releaser(consumer, w.weight)()
		return nil
	}
	for i, q := range p.queue {
		if q == w {
			p.queue = append(p.queue[:i], p.queue[i+1:]...)
			break
		}
	}
	p.drainLocked()
	p.mu.Unlock()
	p.emit("pool_timeout", consumer, 0)
	return cause
}

// take records occupancy. Caller must hold the lock.
func (p *ProviderPool) take(consumer string, weight int) {
	p.inUse += weight
	p.byConsumer[consumer] += weight
}

// releaser builds an idempotent release closure for a held reservation.
func (p *ProviderPool) releaser(consumer string, weight int) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.inUse -= weight
			if p.inUse < 0 {
				p.inUse = 0
			}
			p.byConsumer[consumer] -= weight
			if p.byConsumer[consumer] <= 0 {
				delete(p.byConsumer, consumer)
			}
			p.drainLocked()
			p.mu.Unlock()
		})
	}
}

// drainLocked grants queued waiters in strict arrival order. It stops at the
// first waiter that does not fit — letting a lighter waiter behind it through
// would be the starvation the FIFO rule exists to prevent.
func (p *ProviderPool) drainLocked() {
	for len(p.queue) > 0 {
		head := p.queue[0]
		if p.inUse+head.weight > p.size {
			return
		}
		p.queue = p.queue[1:]
		p.take(head.consumer, head.weight)
		head.granted = true
		close(head.ready)
	}
}

// Stats reports current occupancy.
func (p *ProviderPool) Stats() PoolStats {
	if p == nil {
		return PoolStats{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	byConsumer := make(map[string]int, len(p.byConsumer))
	for k, v := range p.byConsumer {
		byConsumer[k] = v
	}
	return PoolStats{Size: p.size, InUse: p.inUse, Waiting: len(p.queue), ByConsumer: byConsumer}
}

// Describe renders occupancy for the scheduler's status line, so a human
// tailing the log sees saturation rather than having to infer it.
func (p *ProviderPool) Describe() string {
	if p == nil {
		return "disabled"
	}
	s := p.Stats()
	if s.Size <= 0 {
		return "disabled"
	}
	return fmt.Sprintf("pool %d/%d (%d waiting)", s.InUse, s.Size, s.Waiting)
}

// reportSaturation emits an event when a caller waited long enough to matter.
func (p *ProviderPool) reportSaturation(consumer string, waited time.Duration) {
	if waited < p.saturationAt {
		return
	}
	p.emit("pool_saturated", consumer, waited)
}

// emit publishes a pool event, throttled per consumer. The failure mode of this
// kind of signal is log spam that gets filtered and then ignored, so one event
// per consumer per minute is the ceiling.
func (p *ProviderPool) emit(kind, consumer string, waited time.Duration) {
	p.mu.Lock()
	sink := p.events
	key := kind + "/" + consumer
	if last, ok := p.lastEventPer[key]; ok && p.now().Sub(last) < p.eventThrottle {
		p.mu.Unlock()
		return
	}
	p.lastEventPer[key] = p.now()
	inUse, waiting, size := p.inUse, len(p.queue), p.size
	p.mu.Unlock()

	log.Printf("[ProviderPool] %s consumer=%s waited=%s pool=%d/%d waiting=%d",
		kind, consumer, waited.Round(time.Millisecond), inUse, size, waiting)
	sink.Emit(Event{Type: kind, Reason: consumer, Fields: map[string]any{
		"consumer": consumer, "waited_ms": waited.Milliseconds(),
		"in_use": inUse, "waiting": waiting, "size": size,
	}})
}
