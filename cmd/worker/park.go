package main

import (
	"io"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// parkState tracks Tier A provider-capacity parking for one pi session (PRD
// worker-park-and-resume Part 2). A parked worker keeps pi alive, waits out
// congestion with exponential backoff, and re-prompts the same session.
// Parked time is excluded from the max-total-runtime budget; the park cap is
// the monotonic bound.
type parkState struct {
	mu    sync.Mutex
	cap   time.Duration
	count int           // park episodes so far (drives backoff)
	total time.Duration // accumulated parked time
	since time.Time    // zero when not parked
	wake  time.Time
	reason string
}

func newParkState(cap time.Duration) *parkState {
	if cap <= 0 {
		cap = defaultParkCap()
	}
	return &parkState{cap: cap}
}

// defaultParkCap reads HYDRA_MAX_PARK_MINUTES (default 60).
func defaultParkCap() time.Duration {
	if v := os.Getenv("HYDRA_MAX_PARK_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return 60 * time.Minute
}

// nextWait returns the backoff for the next park episode, or false when the
// park cap is (or would be) exceeded.
func (ps *parkState) nextWait(reason string) (time.Duration, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	backoff := 30 * time.Second * time.Duration(1<<uint(ps.count))
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	jitter := time.Duration(float64(backoff) * 0.25 * (2*rand.Float64() - 1))
	backoff += jitter
	if backoff < 5*time.Second {
		backoff = 5 * time.Second
	}
	if ps.total+backoff > ps.cap {
		return 0, false
	}
	return backoff, true
}

func (ps *parkState) begin(wait time.Duration, reason string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.count++
	ps.since = time.Now()
	ps.wake = ps.since.Add(wait)
	ps.reason = reason
}

func (ps *parkState) end() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if !ps.since.IsZero() {
		ps.total += time.Since(ps.since)
	}
	ps.since = time.Time{}
	ps.wake = time.Time{}
	ps.reason = ""
}

func (ps *parkState) isActive() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return !ps.since.IsZero()
}

// totalParked returns accumulated plus currently-elapsing parked time.
func (ps *parkState) totalParked() time.Duration {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	total := ps.total
	if !ps.since.IsZero() {
		total += time.Since(ps.since)
	}
	return total
}

// parkInfo exposes the current park window for reporting.
func (ps *parkState) parkInfo() (since, wake time.Time, reason string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.since, ps.wake, ps.reason
}

// decideParkClass classifies a turn failure: park (Tier A provider capacity)
// or not. Ordering matters, learned the expensive way from live Kimi
// payloads: concurrency-limit must be checked before quota (they share the
// 403/access_terminated envelope), and quota before the generic rate-limit
// markers (quota payloads carry "limit" language).
func decideParkClass(errMsg string) (park bool, reason string) {
	s := strings.ToLower(errMsg)
	if isConcurrencyLimit(s) {
		return true, "provider-capacity: concurrency limit"
	}
	if isQuotaError(s) {
		return false, ""
	}
	if isTransientOverload(s) {
		return true, "provider-capacity: transient overload"
	}
	// Plain 429/rate-limit with none of the above markers: throughput
	// ceiling, clears on its own — parkable.
	if strings.Contains(s, "429") || strings.Contains(s, "rate limit") || strings.Contains(s, "rate_limit") {
		return true, "provider-capacity: rate limit"
	}
	return false, ""
}

// parkSession parks the live pi session: reports the parked state, waits out
// the backoff without blocking the scanner, then re-prompts the same session.
// If pi died while parked, the reprompt write fails and the turn is finished
// so the normal failure path takes over (zombie-park guard).
//
// At wake the worker drains shepherd commands queued while it was parked
// (PRD Part 3): steer/continue reshape the wake prompt, abandon-with-summary
// closes the turn and the session instead of resuming.
func parkSession(ps *parkState, wait time.Duration, reason string, stdin io.WriteCloser, turn *rpcTurn, reprompt string) {
	ps.begin(wait, reason)
	since, wake, parkReason := ps.parkInfo()
	if activeReporter != nil {
		activeReporter.SetState("parked")
		activeReporter.SetPark(since, wake, parkReason)
		activeReporter.SetError(0, "", "")
	}
	log.Printf("[Worker:rpc] ⏸️  PARKED (%s) — waiting %v, keeping pi session alive (total parked %v/%v)",
		reason, wait, ps.totalParked(), ps.cap)

	go func() {
		time.Sleep(wait)
		ps.end()
		if activeReporter != nil {
			activeReporter.SetState("working")
			activeReporter.SetPark(time.Time{}, time.Time{}, "")
			// Honor shepherd commands queued while parked.
			for _, qc := range activeReporter.DrainCommands() {
				switch qc.Kind {
				case registry.CmdSteer:
					reprompt = "Shepherd steering instruction (follow it, then continue your task): " + qc.Payload + "\n\n" + reprompt
					log.Printf("[Worker:rpc] honoring steer command: %s", truncateLog(qc.Payload, 120))
				case registry.CmdContinue, registry.CmdCompactAndContinue:
					if qc.Payload != "" {
						reprompt += "\n\nShepherd note: " + qc.Payload
					}
					log.Printf("[Worker:rpc] honoring %s command", qc.Kind)
				case registry.CmdAbandonWithSummary:
					log.Printf("[Worker:rpc] 🛑 abandon-with-summary from shepherd: %s", truncateLog(qc.Payload, 200))
					turn.abandon("abandoned by shepherd: "+qc.Payload, stdin)
					return
				default:
					log.Printf("[Worker:rpc] ignoring unknown command kind %q", qc.Kind)
				}
			}
		}
		line, err := rpcPromptLine(reprompt)
		if err != nil {
			log.Printf("[Worker:rpc] park wake: prompt encode failed: %v", err)
			return
		}
		if _, err := stdin.Write(append(line, '\n')); err != nil {
			// Zombie park: pi died while we slept. Do not reset the turn —
			// the scanner's pipe-error path will finish it.
			log.Printf("[Worker:rpc] park wake: pi unreachable (died while parked): %v", err)
			return
		}
		log.Printf("[Worker:rpc] ▶️  park wake after %v — resuming session", wait)
		turn.reset()
	}()
}
