package main

import (
	"encoding/json"
	"os"
)

// Metrics is the structured JSON report produced by hydra-eval.
// It is duplicated here because both tools are separate main packages.
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

// Regression reports one metric that has regressed against a baseline.
type Regression struct {
	Field    string  `json:"field"`
	Baseline float64 `json:"baseline"`
	Current  float64 `json:"current"`
	Severity string  `json:"severity"`
}

// Compare returns a Regression for each metric rule violated by current
// relative to baseline. A nil slice means no regressions.
func Compare(baseline, current Metrics) []Regression {
	const eps = 1e-9

	var regressions []Regression
	regressions = appendRegression(regressions, "verified_accepts",
		float64(baseline.VerifiedAccepts), float64(current.VerifiedAccepts),
		"critical",
		current.TotalBeads > 0 && current.VerifiedAccepts == 0)
	regressions = appendRegression(regressions, "one_shot_rate",
		baseline.OneShotRate, current.OneShotRate,
		"warning",
		baseline.OneShotRate-current.OneShotRate > 0.15+eps)
	regressions = appendRegression(regressions, "stuck_worker_kills",
		float64(baseline.StuckWorkerKills), float64(current.StuckWorkerKills),
		"warning",
		current.StuckWorkerKills > baseline.StuckWorkerKills)
	regressions = appendRegression(regressions, "failed_closes",
		float64(baseline.FailedCloses), float64(current.FailedCloses),
		"warning",
		current.FailedCloses-baseline.FailedCloses > 2)

	return regressions
}

// appendRegression adds a Regression for field when violated is true.
func appendRegression(regressions []Regression, field string, baseline, current float64, severity string, violated bool) []Regression {
	if violated {
		return append(regressions, Regression{
			Field:    field,
			Baseline: baseline,
			Current:  current,
			Severity: severity,
		})
	}
	return regressions
}

// LoadMetrics reads a Metrics JSON report from path.
func LoadMetrics(path string) (Metrics, error) {
	var m Metrics
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return m, err
	}
	return m, nil
}
