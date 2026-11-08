package core

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// This file gives the AIMD governor a memory feedback loop. Historical gap
// (2026-10-15): the slice hit memory.high 16G with 25.4G swapped and workers
// visibly throttled, yet zero OOM kills fired — memory.high reclaims WITHOUT
// killing — so a governor keyed only on worker outcomes saw a healthy fleet
// and redlined at max_workers. The signals here detect the throttle regime
// directly: cgroup PSI (memory.pressure avg10) and the memory.current vs
// memory.high ratio. Both fail open: an unreadable cgroup (v1, container
// without delegation, host without the service slice) reports no pressure and
// the governor keeps its outcome-driven behavior.

const (
	// governorPSIPath is the daemon's own cgroup PSI file on a systemd v2 host.
	governorPSIPath = "/sys/fs/cgroup/system.slice/hydra.service/memory.pressure"
	// governorMemCurrentPath / governorMemHighPath are the daemon cgroup's
	// current usage and throttle limit.
	governorMemCurrentPath = "/sys/fs/cgroup/system.slice/hydra.service/memory.current"
	governorMemHighPath    = "/sys/fs/cgroup/system.slice/hydra.service/memory.high"
)

// parsePSIAvg10 extracts the "some" line's avg10 percentage from a
// memory.pressure file body. Returns 0 on malformed content (fail open).
func parsePSIAvg10(content string) float64 {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if !strings.HasPrefix(field, "avg10=") {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimPrefix(field, "avg10="), 64)
			if err != nil {
				return 0
			}
			return v
		}
	}
	return 0
}

// parseMemBytes parses a cgroup memory value; "max" (unlimited) returns 0.
func parseMemBytes(content string) int64 {
	s := strings.TrimSpace(content)
	if s == "" || s == "max" {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// memoryThrottlePressure returns the effective throttle signal as a 0-100
// percentage: the cgroup PSI "some" avg10, overridden to 100 when
// memory.current has reached memory.high (at/over the throttle boundary —
// reclaim is mandatory from that point on regardless of what PSI averages).
// Missing or unreadable files are fail-open (no pressure signal).
func memoryThrottlePressure(psiPath, curPath, highPath string) float64 {
	psi := 0.0
	if b, err := os.ReadFile(psiPath); err == nil {
		psi = parsePSIAvg10(string(b))
	}
	cur := int64(0)
	if b, err := os.ReadFile(curPath); err == nil {
		cur = parseMemBytes(string(b))
	}
	high := int64(0)
	if b, err := os.ReadFile(highPath); err == nil {
		high = parseMemBytes(string(b))
	}
	if high > 0 && cur >= high {
		return 100
	}
	if psi > 100 {
		psi = 100
	}
	return psi
}

// MaybeScaleDownForMemoryPressure probes the daemon cgroup's memory throttle
// regime and, when it crosses the configured threshold, multiplicatively
// halves the concurrency limit (the MD in AIMD) and arms lastFailure so
// MaybeRecover cannot climb straight back while workers are still being
// throttled. Safe on a nil receiver; returns true when the limit changed.
//
// A negative MemoryPressureThreshold disables the memory feedback entirely;
// a zero value takes the default (50% PSI avg10).
func (g *ConcurrencyGovernor) MaybeScaleDownForMemoryPressure() bool {
	if g == nil || g.maxWorkers <= 0 || g.memThreshold < 0 {
		return false
	}
	psiPath, curPath, highPath := g.pressurePaths()
	pressure := memoryThrottlePressure(psiPath, curPath, highPath)
	if pressure < g.memThreshold {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	old := g.limit
	newLimit := old / 2
	if newLimit < 1 {
		newLimit = 1
	}
	if newLimit == old {
		return false
	}
	g.limit = newLimit
	now := g.now()
	g.lastChange = now
	g.lastFailure = now
	g.window = g.window[:0]
	log.Printf("[Governor] memory throttle %.0f%% >= %.0f%% — scaling down", pressure, g.memThreshold)
	g.emitChangeLocked(old, newLimit, "memory_throttle")
	return true
}

// pressurePaths returns the cgroup paths probed for the throttle signal,
// overridable in tests.
func (g *ConcurrencyGovernor) pressurePaths() (psi, cur, high string) {
	if g.psiPath != "" {
		return g.psiPath, g.curPath, g.highPath
	}
	return governorPSIPath, governorMemCurrentPath, governorMemHighPath
}

// SetPressurePaths overrides the cgroup probe paths (used by tests). Nil-safe.
func (g *ConcurrencyGovernor) SetPressurePaths(psi, cur, high string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.psiPath = psi
	g.curPath = cur
	g.highPath = high
}
