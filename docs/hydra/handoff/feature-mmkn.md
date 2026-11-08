# feature handoff

## What changed (files + one line each)
- internal/core/events.go: `SensorCorrelation(sensor, object)` — deterministic fire/resolve correlation ID helper (`<sensor>:<object>`).
- internal/core/bead_closer.go: `verified_close_rejected` fire on every reject/escalate round (both paths in rejectClose) + `verified_close_passed` resolve on verified successful close, both carrying correlation `verified-close:<bead>`.
- internal/core/charon.go: `charon_orphan_reopened` fire now carries correlation `orphan-retry:<bead>` and registers in new `orphanWatch` map (mutex-guarded); each `recoverOrphanInProgressBeads` sweep checks watched beads via GetBead — terminal status → `orphan_recovered` resolve under the same correlation, watch entry cleared.
- internal/core/worktree_reconciler.go: `ReconcileOrphanedWorktrees` gains `events *EventSink` param; quarantine path emits `worktree_quarantined` fire (correlation `worktree-reconcile:<key>`); both production callers (publisher PublishRepo, Charon sweep) updated, all 10 test call sites pass nil.
- internal/core/quarantine_reattempt.go: clean requeue additionally emits `quarantine_restored` resolve with the reconciler's correlation ID.
- internal/core/publisher.go: `publishWithRebaseRetry` — `publish_rebase_attempted`/`publish_rebase_exhausted` fires now carry correlation `publish-retry:<projectPath>`; successful publish emits `publish_succeeded` resolve (only when url != "" and err == nil; exhaustion emits no resolve).
- internal/core/concurrency_governor.go: governor gains `SetEventSink`; `emitChangeLocked` emits `aimd_scale_up` (additive_increase/quiet_recovery) and `aimd_scale_down` (additive_decrease) under correlation `aimd:governor` — the AIMD sensor is now visible.
- internal/core/overseer.go: governor wired to `o.eventSink` in both construction paths.
- internal/core/sensor_symmetry.go (new): `wrapRepairHandlers` — wraps every Pan repair verb with `repair_failed` fire / `repair_succeeded` resolve under correlation `pan-repair:<verb>:<bead>`; error passthrough preserved, nil-sink-safe.
- internal/core/service_starter.go: repair handlers registered through `wrapRepairHandlers`.
- scripts/sensor-rollup.sh (new): daily rollup — per-sensor fire/resolve counts and paired-correlation resolve rate from $HYDRA_HOME/events/*.jsonl.
- internal/core/sensor_symmetry_test.go (new): 8 tests — verified-close fire+resolve, orphan fire+resolve+watch-clear, worktree quarantine fire, quarantine-restored correlation, AIMD down/up pair, publish retry resolve/exhaustion-no-resolve, pan repair success/failure/nil-sink, correlation helper.
- CHANGELOG.md, docs/architecture/shepherd.md: Added entry + 4 new event-pair rows in the event-stream table.

## Rollup demo
`HYDRA_HOME= bash scripts/sensor-rollup.sh /tmp/ev-test.jsonl` with one fire+one resolve → `verified_close_rejected/verified_close_passed fires=1 resolves=1 paired=1/1 rate=100.0%` (VERIFIED). On historical streams the new sensors show 0 fires until the rebuilt binary is live — that is the before/after: pre-change streams have fire types (publish_rebase_attempted etc.) with 0 correlation_id-bearing resolves; post-change both sides carry `fields.correlation_id`.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on all touched files — clean.
- `go test ./internal/core/ -run 'TestRecoverOrphan|TestWorktreeReconcile|TestQuarantineReattempt|TestGovernor|TestConcurrencyGovernor|TestBeadCloser_VerifiedClose|TestRejectClose|TestIsPRAlreadyExists|TestPublishWithRebaseRetry|TestSensorSymmetry' -count=1` — PASS (37 tests incl. 8 new).

## Open edges for the next phase
- Operator: rebuild + restart daemon so the new events are live; historical streams stay fire-only.
- orphanWatch is in-memory: daemon restart loses watched entries (beads still recover — only the resolve event for a pre-restart fire is lost).
- AIMD correlation is a single global ID (`aimd:governor`); if per-window pairing is ever needed, derive from `old`/`new`.
