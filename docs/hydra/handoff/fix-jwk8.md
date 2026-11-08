# fix handoff (fix-jwk8)

## What changed (files + one line each)
- internal/registry/token_usage_daily.go (new): durable `token_usage_daily` table (day+project PK, additive merge), `mergeWorkerUsage`, `SnapshotAndRollup`, `GetDailyRollup` (median recomputed from stored per-bead token list), reap-candidate capture helpers.
- internal/registry/registry.go: `ReapStale`/`ReapGhosts` persist doomed rows' token usage into the daily table before delete; `UpsertWorker` records a rerun when a worker re-registers over a completed row; `migrateRegistry` creates the rollup table.
- internal/core/token_usage_rollup.go (new): `TokenUsageRollupJob` — fires at next 00:05 UTC, snapshots live rows into the daily table, emits typed `token_usage_daily` event per (day, project). Fail-open, nil-safe.
- internal/core/overseer.go: `tokenUsageRollup` field.
- internal/core/service_starter.go: job constructed with lazy registry accessor after registry open, Start on start, Stop on stop.
- internal/registry/token_usage_daily_test.go, internal/core/token_usage_rollup_test.go: tests — reap persistence (Stale + Ghosts), aggregation (completions/sums/median/max/zero-token), rerun bump, runOnce persist+event, nil-store no-op, nextRollupTime boundaries.
- CHANGELOG.md, docs/architecture/shepherd.md: operator-facing entry + section.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/registry/ ./internal/core/` — PASS (clean).
- `gofmt -l internal/registry internal/core` — clean.
- `go test ./internal/registry/ -count=1` — PASS.
- `go test ./internal/core/ -run 'TestTokenUsageRollup|TestNextRollupTime' -count=1` — PASS.

## Open edges for the next phase
- Rerun count is only durable from deploy onward (no historical backfill); pre-existing pruned completions are unrecoverable.
- `SnapshotAndRollup` merges every call (00:05 job + none others today); repeated same-day snapshots are additive over the same live rows — but the job fires once per day per day-key boundary, and reap paths persist only deleted rows, so live rows are merged once at rollup. A manual/extra runOnce would double-count that day's live rows; acceptable, but a `recorded_at` idempotence guard could be added later.
- Median per bead uses tokens_so_far of every merged non-zero row (working rows included).
