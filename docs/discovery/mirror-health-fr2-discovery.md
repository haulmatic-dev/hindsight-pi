# Discovery: FR-002 — Periodic read-only audit sweep in the daemon

**Bead:** FR-002 (discovery phase) · **PRD:** docs/PRDs/mirror-health-prd-20261022.md · **Date:** 2026-10-22

## Purpose

Resolve design ambiguities for FR-2 (periodic read-only mirror-drift audit
sweep in the daemon) before the RED bead writes contract tests, so GREEN has a
pinned API surface instead of inventing one.

## Verified context (from repo, this session)

- The audit logic itself exists as `tools/beads-mirror-audit` (walk jsonl vs
  `br list --json --all`); FR-1 (shared `internal/mirroraudit` package) is a
  sibling deliverable — FR-2 should consume that package, not re-implement the
  compare. If FR-1 has not landed when FR-2's GREEN starts, the sweep must
  call the tool's exported function or the CLI; do not fork a second parser
  (explicit prior-chain rule: "Reuse the existing target-file parsing").
- The periodic-sweep skeleton to model is `internal/core/pan_stale_filing.go`:
  package-level probe seams, interval-throttled tick inside
  `Overseer.tick()` (`overseer.go` ~977), fail-open guards, typed events via
  EventSink, disk ledger pattern (in-process map + persist hook).
- Config knob precedent: `ChainDetectionConfig{Enabled, Interval time.Duration
  yaml:"interval"}` and `RetryIntervalDuration()` parse helpers in
  `internal/core/config.go`.

## Proposed design (for RED/GREEN to pin)

- New file `internal/core/pan_mirror_health.go`, wire-up in
  `internal/core/overseer.go` (throttle field `lastMirrorAuditSweep` + tick
  branch) and `internal/core/service_starter.go` (Start/Stop if the loop is
  goroutine-shaped rather than tick-shaped; either is acceptable — pick the
  one `pan_stale_filing` uses so a reviewer reads one pattern).
- Knobs (FR-2 scope): `mirror_health.enabled` (bool, default true per
  PRD "audit-only by default"), `mirror_health.audit_interval` (duration,
  default 30m, 0 = disabled). Note: FR-3/FR-4 knobs (`auto_resync`) are OUT of
  this bead's scope but the config struct should leave room for them.
- Per sweep: for every registered project store — skip if jsonl mtime < 2m
  (quiescence guard), skip if any live worker for the project; audit via the
  mirroraudit package; emit `mirror_drift_detected` (per-divergence record:
  store, id, jsonl status, db status) and `mirror_audit_clean` (per store with
  count). Persist a ledger at `$HYDRA_HOME/mirror-health-ledger.json`
  (store → last audit time, direction, bead ids, attempt counts) that
  survives restarts. Fail-open throughout: any error logs `mirror_audit_error`
  and continues; the sweep never gates scheduling, filing, or closes.

## Acceptance-criteria mapping (from bead FR-002)

| AC | Evidence the GREEN bead must produce |
|---|---|
| Unit tests cover interval firing | Test that a sweep does not run before `audit_interval` elapses and runs after (throttle field / injected clock) |
| mtime-quiescence skip | Temp-dir fixture whose jsonl mtime is fresh → zero audit invocations, no events |
| live-worker skip | Fixture with a live worker for the project → zero audit invocations |
| Config knobs parse | Parse `mirror_health.enabled` / `mirror_health.audit_interval` from yaml into typed fields; default + 0-disable cases |
| `go test ./internal/core/ -run 'MirrorHealth' -count=1` exits 0 | RED suite greens loudly via the ENFORCE-gate pattern (`MIRROR_HEALTH_ENFORCE=1` proves the contract is unimplemented pre-GREEN, matching the `pan_stale_filing` / `bead_claims` precedent) |
| "sweep" (trailing AC text) | Green sweep implementation wired on the overseer tick |

## Ambiguities resolved for GREEN

1. **Audit source:** reuse the existing `tools/beads-mirror-audit` parsing —
   either import the FR-1 package or shell the CLI. Decision left to whichever
   lands first; the sweep's contract tests should inject an `audit func(store)
   (report, error)` seam so tests never depend on br.
2. **Ledger scope:** per-store last-audit + per-record attempt counts; restarts
   must not reset strike counts (disk-persisted, like the stale-filing closed
   ledger's intent).
3. **"Live worker" definition:** any worker with a heartbeat for that project
   within its liveness window — reuse whatever the overseer already computes
   (`activeWorkerKeys`); do not invent a second liveness source.
4. **No auto-heal in this bead:** healing is FR-3/FR-4. The sweep is
   strictly read-only here; ledger fields for outcomes exist for later phases.

## Test plan (RED bead contract)

`internal/core/pan_mirror_health_test.go`:
- Config parse: yaml with both knobs → fields set; missing knobs → defaults;
  `audit_interval: 0` disables.
- Interval firing: injected clock before interval → no sweep; at interval →
  sweep runs once, throttle resets.
- mtime quiescence: fresh jsonl → skip (verified via injected audit seam call
  counter).
- Live-worker skip: project with active worker → skip.
- Fail-open: audit seam returns error → sweep logs `mirror_audit_error`, no
  panic, next store still audited.
- Typed events: `mirror_drift_detected` and `mirror_audit_clean` emitted with
  store/id/count fields; clean store emits only clean event.

## Open edges

- FR-1 (`internal/mirroraudit`) sequencing: if not landed, GREEN may inject a
  stub seam and wire to the package in a follow-up — flag which path was taken
  in the GREEN handoff.
- `mirror_audit_error` should be throttled per store to avoid event spam if a
  store is persistently unreadable (mirror the stale-filing rate-limit shape).
