package main

import (
	"regexp"
	"testing"
)

func findRegression(t *testing.T, regressions []Regression, field string) *Regression {
	t.Helper()
	for i := range regressions {
		if regressions[i].Field == field {
			return &regressions[i]
		}
	}
	return nil
}

func TestCompare_OneShotRate_DropAboveThreshold(t *testing.T) {
	baseline := Metrics{OneShotRate: 0.8}
	current := Metrics{OneShotRate: 0.6}
	regs := Compare(baseline, current)

	if len(regs) != 1 {
		t.Fatalf("Compare(OneShotRate 0.8, 0.6) returned %d regressions; want 1", len(regs))
	}
	if regs[0].Field != "one_shot_rate" {
		t.Errorf("Regression.Field = %q; want %q", regs[0].Field, "one_shot_rate")
	}
}

func TestCompare_OneShotRate_DropWithinTolerance(t *testing.T) {
	baseline := Metrics{OneShotRate: 0.8}
	current := Metrics{OneShotRate: 0.7}
	regs := Compare(baseline, current)

	if r := findRegression(t, regs, "one_shot_rate"); r != nil {
		t.Errorf("Compare(OneShotRate 0.8, 0.7) returned unexpected one_shot_rate regression: %+v", *r)
	}
}

func TestCompare_StuckWorkerKills_Increase(t *testing.T) {
	baseline := Metrics{StuckWorkerKills: 0}
	current := Metrics{StuckWorkerKills: 1}
	regs := Compare(baseline, current)

	if len(regs) != 1 {
		t.Fatalf("Compare(StuckWorkerKills 0, 1) returned %d regressions; want 1", len(regs))
	}
	if regs[0].Field != "stuck_worker_kills" {
		t.Errorf("Regression.Field = %q; want %q", regs[0].Field, "stuck_worker_kills")
	}
}

func TestCompare_VerifiedAcceptsZero_WithTotalBeads(t *testing.T) {
	baseline := Metrics{}
	current := Metrics{VerifiedAccepts: 0, TotalBeads: 10}
	regs := Compare(baseline, current)

	hasCritical := false
	for _, r := range regs {
		if r.Severity == "critical" {
			hasCritical = true
			break
		}
	}
	if !hasCritical {
		t.Fatalf("Compare(VerifiedAccepts=0, TotalBeads=10) did not return a critical regression; got %+v", regs)
	}
}

func TestLoadMetrics_MissingPath(t *testing.T) {
	_, err := LoadMetrics("/nonexistent/path/metrics.json")
	if err == nil {
		t.Fatal("LoadMetrics on missing path returned nil error")
	}
	if matched, _ := regexp.MatchString(`no such file`, err.Error()); !matched {
		t.Errorf("error %q does not match /no such file/", err.Error())
	}
}
