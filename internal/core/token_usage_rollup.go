package core

import (
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// TokenUsageRollupJob snapshots the worker registry into the durable
// token_usage_daily table every day at 00:05 UTC and emits one
// token_usage_daily event per touched (day, project) row, so token/turn
// spend trends survive registry row pruning (ReapStale drops completed rows
// after 24h). Fail-open: registry or event failures are logged and retried
// on the next day's tick; the job never blocks scheduling.
type TokenUsageRollupJob struct {
	getStore func() *registry.Store // nil-safe: no store, no job work
	events   *EventSink             // nil-safe
	stopChan chan struct{}
}

// NewTokenUsageRollupJob builds the job. getStore must be lazy: the registry
// is opened in ServiceStarter.Start, after Overseer construction.
func NewTokenUsageRollupJob(getStore func() *registry.Store, events *EventSink) *TokenUsageRollupJob {
	return &TokenUsageRollupJob{getStore: getStore, events: events, stopChan: make(chan struct{})}
}

// Start launches the daily loop. No-op when there is no registry accessor.
func (j *TokenUsageRollupJob) Start() {
	if j == nil || j.getStore == nil {
		return
	}
	log.Printf("[TokenUsage] 📊 Daily token-usage rollup scheduled (00:05 UTC)")
	go j.run()
}

// Stop halts the daily loop.
func (j *TokenUsageRollupJob) Stop() {
	if j == nil {
		return
	}
	select {
	case <-j.stopChan:
	default:
		close(j.stopChan)
	}
}

func (j *TokenUsageRollupJob) run() {
	for {
		delay := time.Until(nextRollupTime(time.Now().UTC()))
		select {
		case <-time.After(delay):
			j.runOnce()
		case <-j.stopChan:
			return
		}
	}
}

// nextRollupTime returns the next 00:05 UTC boundary. If it is already past
// 00:05 today, the next day's 00:05 is returned.
func nextRollupTime(now time.Time) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, time.UTC)
	if !t.After(now) {
		t = t.Add(24 * time.Hour)
	}
	return t
}

// runOnce merges current worker rows into the durable daily table and emits
// a token_usage_daily event per touched rollup row.
func (j *TokenUsageRollupJob) runOnce() {
	store := j.getStore()
	if store == nil {
		return
	}
	rows, err := store.SnapshotAndRollup()
	if err != nil {
		log.Printf("[TokenUsage] rollup failed (fail open, retried next tick): %v", err)
		return
	}
	for _, r := range rows {
		j.events.Emit(Event{
			Type:    "token_usage_daily",
			Project: r.Project,
			Reason:  "daily_rollup",
			Detail:  "daily token-usage rollup persisted so registry pruning cannot erase spend trends",
			Fields: map[string]any{
				"day":              r.Day,
				"completions":      r.Completions,
				"tokens_fresh":     r.TokensFresh,
				"tokens_output":    r.TokensOutput,
				"tokens_cache":     r.TokensCache,
				"median_per_bead":  r.MedianPerBead,
				"max_per_bead":     r.MaxPerBead,
				"rerun_count":      r.RerunCount,
				"zero_token_count": r.ZeroTokenCount,
			},
		})
	}
	log.Printf("[TokenUsage] 📊 Persisted %d daily rollup row(s)", len(rows))
}
