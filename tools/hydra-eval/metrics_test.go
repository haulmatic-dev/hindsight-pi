package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeMetrics_CountsBeads(t *testing.T) {
	beads := []BeadRecord{
		{ID: "1", Status: "closed", IssueType: "task"},
		{ID: "2", Status: "closed", IssueType: "task"},
		{ID: "3", Status: "open", IssueType: "epic"},
	}
	metrics := ComputeMetrics(beads, nil, 0, 0, 0)

	assert.Equal(t, 3, metrics.TotalBeads)
	assert.Equal(t, 2, metrics.ClosedBeads)
	assert.Equal(t, 2, metrics.BeadsByType["task"])
	assert.Equal(t, 1, metrics.BeadsByType["epic"])
}

func TestComputeMetrics_CountsEventsAndOneShotRate(t *testing.T) {
	events := []CloseEvent{
		{BeadID: "1", Verdict: "accepted", Attempt: 0},
		{BeadID: "2", Verdict: "accepted", Attempt: 2},
		{BeadID: "3", Verdict: "rejected", Attempt: 0},
	}
	metrics := ComputeMetrics(nil, events, 0, 0, 0)

	assert.Equal(t, 2, metrics.VerifiedAccepts)
	assert.Equal(t, 1, metrics.OneShotAccepts)
	assert.InDelta(t, 0.5, metrics.OneShotRate, 0.0001)
}

func TestComputeMetrics_OneShotRateZeroWhenNoAccepts(t *testing.T) {
	events := []CloseEvent{
		{BeadID: "1", Verdict: "rejected", Attempt: 0},
	}
	metrics := ComputeMetrics(nil, events, 0, 0, 0)

	assert.Equal(t, 0, metrics.VerifiedAccepts)
	assert.Equal(t, 0, metrics.OneShotAccepts)
	assert.InDelta(t, 0.0, metrics.OneShotRate, 0.0001)
}

func TestComputeMetrics_WallClockSeconds(t *testing.T) {
	beads := []BeadRecord{
		{ID: "1", CreatedAt: "2026-07-14T10:00:00Z", ClosedAt: ""},
		{ID: "2", CreatedAt: "", ClosedAt: "2026-07-14T11:30:00Z"},
	}
	metrics := ComputeMetrics(beads, nil, 0, 0, 0)

	assert.InDelta(t, 5400.0, metrics.WallClockSeconds, 0.0001)
}

func TestComputeMetrics_WallClockSecondsNanoFallback(t *testing.T) {
	beads := []BeadRecord{
		{ID: "1", CreatedAt: "2026-07-14T10:00:00.000000000Z", ClosedAt: ""},
		{ID: "2", CreatedAt: "", ClosedAt: "2026-07-14T10:00:01.000000000Z"},
	}
	metrics := ComputeMetrics(beads, nil, 0, 0, 0)

	assert.InDelta(t, 1.0, metrics.WallClockSeconds, 0.0001)
}

func TestComputeMetrics_UnparseableTimestampsIgnored(t *testing.T) {
	beads := []BeadRecord{
		{ID: "1", CreatedAt: "not-a-time", ClosedAt: ""},
		{ID: "2", CreatedAt: "", ClosedAt: "also-not-a-time"},
	}
	metrics := ComputeMetrics(beads, nil, 0, 0, 0)

	assert.InDelta(t, 0.0, metrics.WallClockSeconds, 0.0001)
}

func TestComputeMetrics_PassesThroughMarkers(t *testing.T) {
	metrics := ComputeMetrics(nil, nil, 5, 7, 3)

	assert.Equal(t, 5, metrics.StuckWorkerKills)
	assert.Equal(t, 7, metrics.ScopeWarnings)
	assert.Equal(t, 3, metrics.FailedCloses)
}
