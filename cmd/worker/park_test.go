package main

import (
	"strings"
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
