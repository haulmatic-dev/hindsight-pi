# Discovery: FR-004 — Pan filing for non-healable drift

**Bead:** hydra-wyel (discovery phase) · **PRD:** docs/PRDs/mirror-health-prd-20261022.md · **Date:** 2026-10-23

## Purpose

Pin the FR-4 contract (file one Pan triage event per divergent record the
guard will not heal) before GREEN writes the filing implementation, and
resolve the ambiguous semantics GREEN would otherwise guess at.

## Verified context (from repo, this session)

- FR-3 heal seam landed (`internal/core/overseer.go` `BrUpdater` seam +
  `mirror_health.auto_resync` knob, default false). Healing is guarded by
  three conditions: daemon-owned store, passing write probe, terminal drift
  status. FR-4 covers everything that fails those guards.
- The RED contract tests live in `internal/core/pan_mirror_health_test.go`
  under the `FR-4: Pan filing for non-healable drift (RED contract —
  hydra-wyel)` block; they are gated on `mirrorHealthRequireImpl(t)` and skip
  loudly until GREEN.
- Write-probe seam added this bead: `mirrorWriteProbe` in
  `internal/core/pan_mirror_health.go` (inert default: probe succeeds), plus
  the `mirrorHooks.writeProbe` test hook. GREEN must consult it on the
  auto-resync path; a probe failure downgrades a would-be heal into a filing.
- Filing sink precedent: `internal/core/pan_stale_filing.go` uses the
  pan-bead route through `beadsClient.CreateBead` with a disk-ledger dedup
  (in-process map + persist hook) so one record files once, and a *failed*
  filing is retried on a later tick because the ledger records successes
  only.

## Resolved design decisions (for GREEN to pin)

1. **Filing sink = `beadsClient.CreateBead`.** Same pan-bead route as
   stale filing. The RED tests assert on
   `mockBeadsClient.getCreateCalls()`; a second sink (EventSink-only) would
   break the contract. Bead title must contain the divergent record id; the
   description must contain the literal event name
   `mirror_drift_needs_human` plus store path, direction, and the suggested
   one-liner (`br sync --import-only` for jsonl_ahead-terminal,
   `br update <id> --status <same>` for db_ahead-ahead).
2. **Exactly-once semantics = per record, ledger-backed.** One divergent
   record files exactly one pan bead across repeated sweeps; the filing is
   retried only while the previous attempt errored (sink failure is not a
   strike and is not recorded as success). This mirrors the
   `pan_stale_filing.go` ledger pattern and is what the
   `FilingSinkFailureLoggedSweepContinues` test pins (two ticks: attempt,
   fail, retry, succeed).
3. **Fail-open, always.** A filing-sink error is logged via the EventSink
   stream and the sweep continues over remaining records; it never returns an
   error and never panics. A *failed* filing does not emit
   `mirror_drift_needs_human` (the event fires on the pan bead being
   accepted).
4. **Healable vs failable classification (owned by FR-3's guards, consumed
   here):** a record is *healed* only when daemon-owned + probe-ok +
   terminal-status drift + `auto_resync` enabled. Every other divergent
   record — user-owned store, non-terminal jsonl status, failed write probe,
   `auto_resync` disabled, 2-strike, daily cap — routes to this filing path.
   FR-4 does not re-evaluate the guards; it receives the non-healable
   residual from the FR-3 decision point.
5. **Event stream.** On a successful filing, emit one
   `mirror_drift_needs_human` event carrying store path, bead id, and
   direction (jsonl_ahead / db_ahead). Sweep-level summaries keep using the
   existing `mirror_audit` events.

## Files

- `internal/core/pan_mirror_health.go` — filing helper + ledger + event emit
  (GREEN target; this bead adds only the `mirrorWriteProbe` seam).
- `internal/core/pan_mirror_health_test.go` — RED contract tests (written).

## Verification

- `go test ./internal/core/ -run 'MirrorHealth' -count=1` exits 0 with all
  mirror-health contract tests skipping loudly (RED gate), suite green.
  Non-mirror-health suites unaffected.

## Handoff

`docs/hydra/handoff/discovery-wyel.md` records the same decisions for the
GREEN bead.
