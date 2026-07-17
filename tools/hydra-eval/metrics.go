package main

import (
	"time"
)

// Metrics is the structured JSON report produced by hydra-eval.
type Metrics struct {
	TotalBeads       int            `json:"total_beads"`
	ClosedBeads      int            `json:"closed_beads"`
	VerifiedAccepts  int            `json:"verified_accepts"`
	FailedCloses     int            `json:"failed_closes"`
	OneShotAccepts   int            `json:"one_shot_accepts"`
	OneShotRate      float64        `json:"one_shot_rate"`
	StuckWorkerKills int            `json:"stuck_worker_kills"`
	ScopeWarnings    int            `json:"scope_warnings"`
	WallClockSeconds float64        `json:"wall_clock_seconds"`
	BeadsByType      map[string]int `json:"beads_by_type"`
}

// ComputeMetrics derives a Metrics report from the parsed artifacts and
// the pre-counted log markers.
func ComputeMetrics(beads []BeadRecord, events []CloseEvent, stuckKills, scopeWarnings, failedCloses int) Metrics {
	m := Metrics{
		BeadsByType:      make(map[string]int),
		StuckWorkerKills: stuckKills,
		ScopeWarnings:    scopeWarnings,
		FailedCloses:     failedCloses,
	}

	m.TotalBeads = len(beads)
	for _, b := range beads {
		if b.Status == "closed" {
			m.ClosedBeads++
		}
		m.BeadsByType[b.IssueType]++
	}

	for _, ev := range events {
		if ev.Verdict == "accepted" {
			m.VerifiedAccepts++
			if ev.Attempt == 0 {
				m.OneShotAccepts++
			}
		}
	}

	if m.VerifiedAccepts > 0 {
		m.OneShotRate = float64(m.OneShotAccepts) / float64(m.VerifiedAccepts)
	}

	m.WallClockSeconds = wallClockSeconds(beads)
	return m
}

func wallClockSeconds(beads []BeadRecord) float64 {
	var earliestCreated time.Time
	var latestClosed time.Time
	haveCreated := false
	haveClosed := false

	for _, b := range beads {
		if b.CreatedAt != "" {
			t, ok := parseRFC3339OrNano(b.CreatedAt)
			if ok {
				if !haveCreated || t.Before(earliestCreated) {
					earliestCreated = t
					haveCreated = true
				}
			}
		}
		if b.ClosedAt != "" {
			t, ok := parseRFC3339OrNano(b.ClosedAt)
			if ok {
				if !haveClosed || t.After(latestClosed) {
					latestClosed = t
					haveClosed = true
				}
			}
		}
	}

	if !haveCreated || !haveClosed {
		return 0
	}
	return latestClosed.Sub(earliestCreated).Seconds()
}

func parseRFC3339OrNano(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}
