package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

func testPool(t *testing.T, size int) *ProviderPool {
	t.Helper()
	return NewProviderPool(size, PoolConfig{Enabled: true, AcquireTimeoutSeconds: 2})
}

func TestProviderPool_AdmitsUpToSizeThenBlocks(t *testing.T) {
	p := testPool(t, 2)
	ctx := context.Background()

	r1, err := p.Acquire(ctx, "worker", 1)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	r2, err := p.Acquire(ctx, "worker", 1)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if got := p.Stats().InUse; got != 2 {
		t.Fatalf("InUse = %d, want 2", got)
	}

	// Third must block until a slot frees.
	admitted := make(chan struct{})
	go func() {
		r3, err := p.Acquire(ctx, "adjudicator", 1)
		if err == nil {
			close(admitted)
			r3()
		}
	}()

	select {
	case <-admitted:
		t.Fatal("third acquire was admitted while the pool was full")
	case <-time.After(150 * time.Millisecond):
	}

	r1()
	select {
	case <-admitted:
	case <-time.After(2 * time.Second):
		t.Fatal("third acquire never admitted after a release")
	}
	r2()
}

// TestProviderPool_FIFOUnderWeight is the fairness rule: a heavy waiter that
// arrived first must not be overtaken by a lighter one behind it. Without this,
// a stream of single-slot worker spawns would starve the adjudicator forever.
func TestProviderPool_FIFOUnderWeight(t *testing.T) {
	p := testPool(t, 2)
	ctx := context.Background()

	hold, err := p.Acquire(ctx, "worker", 1) // 1 of 2 in use
	if err != nil {
		t.Fatalf("hold: %v", err)
	}

	order := make(chan string, 2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // heavy waiter, needs both slots, arrives first
		defer wg.Done()
		r, err := p.Acquire(ctx, "adjudicator", 2)
		if err != nil {
			return
		}
		order <- "heavy"
		r()
	}()

	// Let the heavy waiter enqueue before the light one arrives.
	waitForWaiting(t, p, 1)

	wg.Add(1)
	go func() { // light waiter, would fit right now, but must not jump the queue
		defer wg.Done()
		r, err := p.Acquire(ctx, "worker", 1)
		if err != nil {
			return
		}
		order <- "light"
		r()
	}()
	waitForWaiting(t, p, 2)

	hold() // frees 1 slot: not enough for heavy, and light must not overtake
	wg.Wait()

	if len(order) != 2 {
		t.Fatalf("expected both waiters to complete, got %d", len(order))
	}
	if first := <-order; first != "heavy" {
		t.Errorf("light waiter overtook the heavy one that arrived first (got %q first)", first)
	}
}

func waitForWaiting(t *testing.T, p *ProviderPool, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Stats().Waiting >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d queued waiters (have %d)", n, p.Stats().Waiting)
}

func TestProviderPool_CancelledContextAcquiresNothing(t *testing.T) {
	p := testPool(t, 1)
	hold, _ := p.Acquire(context.Background(), "worker", 1)
	defer hold()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Acquire(ctx, "adjudicator", 1); err == nil {
		t.Fatal("acquire with a cancelled context should fail")
	}
	if got := p.Stats().InUse; got != 1 {
		t.Errorf("InUse = %d after a failed acquire, want 1 (nothing extra taken)", got)
	}
}

func TestProviderPool_AcquireTimesOutAndLeavesNoSlotHeld(t *testing.T) {
	p := NewProviderPool(1, PoolConfig{Enabled: true, AcquireTimeoutSeconds: 1})
	hold, _ := p.Acquire(context.Background(), "worker", 1)

	start := time.Now()
	if _, err := p.Acquire(context.Background(), "adjudicator", 1); err == nil {
		t.Fatal("acquire should have timed out")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("acquire blocked %s, well past the 1s timeout", elapsed)
	}
	if got := p.Stats().InUse; got != 1 {
		t.Errorf("InUse = %d after timeout, want 1", got)
	}
	hold()
	if got := p.Stats().InUse; got != 0 {
		t.Errorf("InUse = %d after release, want 0", got)
	}
}

// TestProviderPool_ReleaseIsIdempotent: a double release must not free slots it
// does not own, or the pool would over-admit and reintroduce the refusals it
// exists to prevent.
func TestProviderPool_ReleaseIsIdempotent(t *testing.T) {
	p := testPool(t, 2)
	r1, _ := p.Acquire(context.Background(), "worker", 1)
	r2, _ := p.Acquire(context.Background(), "worker", 1)

	r1()
	r1()
	r1()
	if got := p.Stats().InUse; got != 1 {
		t.Errorf("InUse = %d after three calls to one release, want 1", got)
	}
	r2()
	if got := p.Stats().InUse; got != 0 {
		t.Errorf("InUse = %d, want 0", got)
	}
}

func TestProviderPool_NilAndDisabledAreNoOps(t *testing.T) {
	var nilPool *ProviderPool
	rel, err := nilPool.Acquire(context.Background(), "worker", 3)
	if err != nil {
		t.Fatalf("nil pool Acquire returned an error: %v", err)
	}
	if rel == nil {
		t.Fatal("nil pool must return a usable no-op release, got nil")
	}
	rel() // must not panic
	if got := nilPool.Describe(); got != "disabled" {
		t.Errorf("nil Describe() = %q, want \"disabled\"", got)
	}
	if w := nilPool.WeightFor("worker"); w != 1 {
		t.Errorf("nil WeightFor = %d, want 1", w)
	}
}

// TestProviderPool_WeightLargerThanPoolDoesNotDeadlock: a misconfigured weight
// must degrade to exclusive use rather than hang the consumer forever.
func TestProviderPool_WeightLargerThanPoolDoesNotDeadlock(t *testing.T) {
	p := testPool(t, 2)
	done := make(chan struct{})
	go func() {
		r, err := p.Acquire(context.Background(), "worker", 99)
		if err == nil {
			r()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("an over-large weight deadlocked instead of clamping to the pool size")
	}
}

func TestProviderPool_WeightForUsesConfig(t *testing.T) {
	p := NewProviderPool(4, PoolConfig{Enabled: true, Weights: map[string]int{"worker": 2}})
	if got := p.WeightFor("worker"); got != 2 {
		t.Errorf("WeightFor(worker) = %d, want 2 from config", got)
	}
	if got := p.WeightFor("adjudicator"); got != 1 {
		t.Errorf("unconfigured consumer should default to 1, got %d", got)
	}
}

// TestProviderPool_ConcurrentAcquireNeverExceedsSize is the invariant the whole
// type exists for: under load, in-use must never exceed the provider's budget.
func TestProviderPool_ConcurrentAcquireNeverExceedsSize(t *testing.T) {
	p := testPool(t, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	maxSeen := 0

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			consumer := []string{"worker", "adjudicator", "validation"}[n%3]
			rel, err := p.Acquire(context.Background(), consumer, 1)
			if err != nil {
				return
			}
			mu.Lock()
			if u := p.Stats().InUse; u > maxSeen {
				maxSeen = u
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			rel()
		}(i)
	}
	wg.Wait()

	if maxSeen > 3 {
		t.Errorf("observed %d slots in use, pool size is 3 — the budget was exceeded", maxSeen)
	}
	if got := p.Stats().InUse; got != 0 {
		t.Errorf("InUse = %d after all releases, want 0 (slot leak)", got)
	}
}

// TestProviderPool_AllHoldersCancelledReturnsToZero is NFR-001: it must be
// impossible for the pool to hold a slot that no longer corresponds to live work.
func TestProviderPool_AllHoldersCancelledReturnsToZero(t *testing.T) {
	p := testPool(t, 3)
	releases := make([]func(), 0, 3)
	for i := 0; i < 3; i++ {
		r, err := p.Acquire(context.Background(), "worker", 1)
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		releases = append(releases, r)
	}
	if got := p.Stats().InUse; got != 3 {
		t.Fatalf("InUse = %d, want 3", got)
	}
	for _, r := range releases {
		r()
	}
	if got := p.Stats().InUse; got != 0 {
		t.Errorf("InUse = %d after every holder released, want 0", got)
	}
	if got := p.Stats().Waiting; got != 0 {
		t.Errorf("Waiting = %d, want 0", got)
	}
}

func TestProviderPool_DescribeReportsOccupancy(t *testing.T) {
	p := testPool(t, 4)
	r, _ := p.Acquire(context.Background(), "worker", 1)
	defer r()
	if got, want := p.Describe(), "pool 1/4 (0 waiting)"; got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
}
