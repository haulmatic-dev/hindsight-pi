package beads

import (
	"sync"
	"testing"
	"time"
)

// concurrencyTrackingExecutor records the maximum number of concurrent
// invocations per directory and globally. It implements the ExecuteInDir
// hook that execInDir prefers, so it intercepts serialized br calls.
type concurrencyTrackingExecutor struct {
	mu   sync.Mutex
	cur  map[string]int
	max  map[string]int
	gcur int
	gmax int
}

func newConcurrencyTrackingExecutor() *concurrencyTrackingExecutor {
	return &concurrencyTrackingExecutor{
		cur: make(map[string]int),
		max: make(map[string]int),
	}
}

func (e *concurrencyTrackingExecutor) Execute(name string, arg ...string) ([]byte, error) {
	return []byte{}, nil
}

func (e *concurrencyTrackingExecutor) ExecuteInDir(dir string, name string, arg ...string) ([]byte, error) {
	e.mu.Lock()
	e.cur[dir]++
	if e.cur[dir] > e.max[dir] {
		e.max[dir] = e.cur[dir]
	}
	e.gcur++
	if e.gcur > e.gmax {
		e.gmax = e.gcur
	}
	e.mu.Unlock()

	time.Sleep(5 * time.Millisecond)

	e.mu.Lock()
	e.cur[dir]--
	e.gcur--
	e.mu.Unlock()
	return []byte{}, nil
}

func (e *concurrencyTrackingExecutor) maxConcurrent(dir string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.max[dir]
}

func (e *concurrencyTrackingExecutor) globalMax() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.gmax
}

// TestExecInDir_SerializesBRCallsPerStore verifies that 10 parallel br
// invocations against the same .beads directory never overlap. The
// srv-smart-engine 2026-09-11 incident emptied the store under concurrent
// br access; this is the daemon-side guard.
func TestExecInDir_SerializesBRCallsPerStore(t *testing.T) {
	exec := newConcurrencyTrackingExecutor()
	client := NewClientWithExecutor(exec)
	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.execInDir(dir, "br", "update", "b", "--status", "in_progress"); err != nil {
				t.Errorf("execInDir: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := exec.maxConcurrent(dir); got > 1 {
		t.Fatalf("expected at most 1 concurrent br call per store, got %d", got)
	}
}

// TestExecInDir_ParallelAcrossStores verifies that serialization is per
// store: two independent directories can execute br calls concurrently.
func TestExecInDir_ParallelAcrossStores(t *testing.T) {
	exec := newConcurrencyTrackingExecutor()
	client := NewClientWithExecutor(exec)
	dirA := t.TempDir()
	dirB := t.TempDir()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := client.execInDir(dirA, "br", "list", "--json"); err != nil {
			t.Errorf("execInDir A: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := client.execInDir(dirB, "br", "list", "--json"); err != nil {
			t.Errorf("execInDir B: %v", err)
		}
	}()
	wg.Wait()

	if exec.maxConcurrent(dirA) > 1 || exec.maxConcurrent(dirB) > 1 {
		t.Fatal("per-store concurrency exceeded 1")
	}
	if exec.globalMax() < 2 {
		t.Fatalf("expected concurrent br calls across independent stores, got global max %d", exec.globalMax())
	}
}
