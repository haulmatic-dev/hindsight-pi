package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDecideParkTierA(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{"concurrency limit parks", `403 {"error":{"type":"permission_error","message":"You've reached your concurrent request limit"}}`, true},
		{"engine overload parks", `429 {"error":{"type":"rate_limit_error","message":"The engine is currently overloaded, please try again later"}}`, true},
		{"plain 429 parks", `429 {"error":{"type":"rate_limit_error","message":"rate limit exceeded"}}`, true},
		{"quota does not park", `403 {"error":{"type":"access_terminated_error","message":"usage limit for the billing cycle reached"}}`, false},
		{"auth does not park", `401 {"error":{"type":"authentication_error","message":"invalid api key"}}`, false},
		{"code failure does not park", `build failed: undefined: Foo`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			park, _ := decideParkClass(tc.errMsg)
			if park != tc.want {
				t.Errorf("park = %v, want %v", park, tc.want)
			}
		})
	}
}

func TestParkBackoffGrowth(t *testing.T) {
	ps := newParkState(60 * time.Minute)
	w1, ok1 := ps.nextWait("r")
	ps.count++
	w2, ok2 := ps.nextWait("r")
	if !ok1 || !ok2 {
		t.Fatal("waits should be granted")
	}
	if w2 <= w1 {
		t.Errorf("backoff should grow: %v then %v", w1, w2)
	}
	if w1 < 20*time.Second || w1 > 45*time.Second {
		t.Errorf("first wait out of expected range: %v", w1)
	}
}

func TestParkCapExhaustion(t *testing.T) {
	ps := newParkState(1 * time.Minute)
	ps.total = 55 * time.Second
	if _, ok := ps.nextWait("r"); ok {
		t.Errorf("park cap exhausted — wait should be denied")
	}
}

func TestParkBeginEndAccounting(t *testing.T) {
	ps := newParkState(60 * time.Minute)
	ps.begin(50*time.Millisecond, "test")
	if !ps.isActive() {
		t.Fatal("park should be active")
	}
	time.Sleep(60 * time.Millisecond)
	ps.end()
	if ps.isActive() {
		t.Fatal("park should be inactive after end")
	}
	if ps.totalParked() < 50*time.Millisecond {
		t.Errorf("parked time not accumulated: %v", ps.totalParked())
	}
}

func TestParkReasonClassification(t *testing.T) {
	_, reason := decideParkClass(`403 concurrent request limit`)
	if !strings.Contains(reason, "provider-capacity") {
		t.Errorf("reason should classify the tier: %q", reason)
	}
}

// safeBuffer wraps bytes.Buffer with a mutex so goroutine writes and test
// reads don't race. parkSession writes the abort synchronously and the wake
// reprompt from a goroutine, so the tests need synchronized access.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *safeBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

// recordingWriteCloser is a safeBuffer that also implements io.WriteCloser.
type recordingWriteCloser struct {
	safeBuffer
}

func (*recordingWriteCloser) Close() error { return nil }

func TestParkAbortsInFlightTurnBeforeSleep(t *testing.T) {
	ps := newParkState(0)
	stdin := &recordingWriteCloser{}
	turn := newRPCTurn(false)

	// Simulate the 403 concurrency-limit event that triggers the park.
	if done := turn.handleLine(`{"type":"error","error":{"errorMessage":"403 You've reached your concurrent request limit"}}`, nil); !done {
		t.Fatal("error event must end the turn")
	}
	if !turn.Failed() {
		t.Fatal("turn should be failed before park")
	}

	parkSession(ps, 20*time.Millisecond, "provider-capacity: concurrency limit", stdin, turn,
		"The provider rejected the previous request for capacity reasons. It should accept requests again now — continue your task from where you stopped.")

	// Abort must be written synchronously, before the sleep goroutine wakes.
	if !strings.Contains(stdin.String(), `{"type":"abort"}`) {
		t.Errorf("park must abort the in-flight pi turn before sleeping, got writes: %q", stdin.String())
	}

	// The abort cleanup events that pi emits must not re-fail the turn or
	// overwrite the original park-triggering error.
	if done := turn.handleLine(`{"type":"agent_end","messages":[{"stopReason":"aborted"}]}`, nil); done {
		t.Error("abort cleanup agent_end must not close the turn")
	}
	if !strings.Contains(turn.ErrMsg(), "concurrent request limit") {
		t.Errorf("original park reason must survive abort cleanup, got err=%q", turn.ErrMsg())
	}

	// Wait for wake and verify reset + reprompt.
	time.Sleep(200 * time.Millisecond)
	if turn.Failed() {
		t.Errorf("turn should be reset after wake, still failed with %q", turn.ErrMsg())
	}
	if !strings.Contains(stdin.String(), "continue your task from where you stopped") {
		t.Errorf("wake must reprompt with the resume message, got: %q", stdin.String())
	}
}

// routerCapacityFull: the park-wake probe. A parked worker must hold its
// park while the llm-router pool is saturated (waking burns a denied
// request and re-parks for another full backoff). Any probe failure must
// fail OPEN — never hold a park because the probe itself broke.
func TestRouterCapacityFull(t *testing.T) {
	// No URL configured → feature off → never full.
	t.Setenv("LLM_ROUTER_CAPACITY_URL", "")
	if routerCapacityFull() {
		t.Error("no capacity URL configured: routerCapacityFull = true, want false (feature off)")
	}

	// Saturated pool → full.
	saturated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"free_lanes": 0}`))
	}))
	defer saturated.Close()
	t.Setenv("LLM_ROUTER_CAPACITY_URL", saturated.URL)
	if !routerCapacityFull() {
		t.Error("free_lanes=0: routerCapacityFull = false, want true")
	}

	// Free lanes → not full.
	free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"free_lanes": 3}`))
	}))
	defer free.Close()
	t.Setenv("LLM_ROUTER_CAPACITY_URL", free.URL)
	if routerCapacityFull() {
		t.Error("free_lanes=3: routerCapacityFull = true, want false")
	}

	// Unreachable router → fail open.
	t.Setenv("LLM_ROUTER_CAPACITY_URL", "http://127.0.0.1:1/nope")
	if routerCapacityFull() {
		t.Error("unreachable probe: routerCapacityFull = true, want false (fail open)")
	}
}
