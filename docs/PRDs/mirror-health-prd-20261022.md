# PRD: Mirror Health — Beads JSONL/DB Drift Detection + Guarded Healing

**Version:** 1.0 · **Date:** 2026-10-22 · **System Type:** library · **Repo:** components/hydra

## Problem

The `issues.jsonl` mirror and `beads.db` diverge whenever a write path bypasses
br's dirty-hash export tracker. Measured today (2026-10-22):

- `ajantha-srv-jobfile-7db`: `jsonl=in_progress` while beads.db says CLOSED
  since 2026-10-05 — caused by direct-sqlite surgery recorded in the bead's own
  triage note ("cleared manually schema-11, br unusable").
- 10 further `jsonl=<terminal> db=<missing>` records across 5 finished repos
  (harbour `q2q`/`bv8`, smart-engine `h4a`/`wq2`, telematics `muy`/`nub`,
  fms-frontend `h4t`/`u3r`, webapp `9n0`/`ume`) — db rows never imported.
- `br sync` reports "JSONL is current (hash unchanged since last import)" and
  `br sync --flush-only` reports "Nothing to export (no dirty issues)" on the
  diverged jobfile store — **both primitives are blind to this drift class**.
  The only proven re-sync is an idempotent `br update <id> --status <same>`
  which rewrites the mirror.

Impact is cosmetic for scheduling (the daemon reads db via br), but every
file-based report inherits phantom beads, and the drift class recurs whenever
an emergency bypasses br.

## Verified Facts (measured 2026-10-22)

- Detection logic exists as CLI: `tools/beads-mirror-audit/main.go` walks the
  jsonl and compares against `br list --json --all` (verify:
  `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads`
  → `mirror clean (49 beads)` after today's manual fix).
- Proven fix primitive: `br update ajantha-srv-jobfile-7db --status closed`
  (idempotent same-value update) flushes the mirror; direct jsonl edits are
  rejected by br's schema validation (exit 7) — fail-closed, keep it that way.
- Daemon uid can read user stores: `sudo -u hydra test -r <jsonl>` passes.
  Write access is untested and must be probed, not assumed.
- Periodic-sweep skeleton exists: `internal/core/pan_stale_filing.go`
  (interval + disk-persisted ledger + typed events + fail-open guards).
- Adversarial review (2026-10-22, this session) fixed the design: audit-only
  by default; auto-fix restricted to terminal statuses, daemon-owned stores,
  write-probe, 2-strike, daily cap. Auto-importing non-terminal records can
  resurrect stale claims — forbidden.

## Goals

### FR-1: Shared audit package (single source of truth)

Create `internal/mirroraudit` package: `Audit(storePath string) (Report, error)`
classifying each record as `clean | db_ahead | jsonl_ahead` with per-record
id/status detail. Port the walk-and-compare from
`tools/beads-mirror-audit/main.go`; refactor that CLI to import the package
(two implementations drifting is the disease this PRD treats). Unit tests with
fixture stores (temp dir: jsonl + `br`-free stub via the package's `Comparer`
interface so tests never shell out).

- Fail-closed: malformed jsonl line → record classified `jsonl_ahead`,
  never panics.
- Files: `internal/mirroraudit/audit.go`, `internal/mirroraudit/audit_test.go`,
  `tools/beads-mirror-audit/main.go`.

### FR-2: Periodic read-only audit sweep in the daemon

New `internal/core/pan_mirror_health.go`, modeled on `pan_stale_filing.go`:
every `mirror_health.audit_interval` (default 30m, 0 = disabled), audit every
registered project store; emit `mirror_drift_detected` (per store, with counts
by direction) and `mirror_audit_clean`. Persist a ledger at
`$HYDRA_HOME/mirror-health-ledger.json` (records: store, direction, bead ids,
attempt counts, outcomes) that survives restarts.

- Guards: skip a store if its jsonl mtime is < 2m old or if any worker for
  that project is live (never race a br write).
- Fail-open: any audit error logs `mirror_audit_error` and continues; the
  sweep never blocks scheduling, filing, or closes.
- Files: `internal/core/pan_mirror_health.go`,
  `internal/core/pan_mirror_health_test.go`, `internal/core/config.go`
  (knobs: `mirror_health.enabled`, `mirror_health.audit_interval`).

### FR-3: Guarded auto-resync (OFF by default)

When `mirror_health.auto_resync` is true (default false), the sweep may heal a
divergent record only if ALL hold: (a) the project store is daemon-owned
(projects whose owner resolves to the hydra uid), (b) the record's jsonl
status is terminal (`closed`, `completed`, `tombstone`), (c) no live worker for
the store, (d) a write-probe to the store directory succeeds, (e) the record
has not already failed resync twice (2-strike), (f) the store is under 3
auto-resync attempts/day. Healing action = idempotent
`br update <id> --status <current-jsonl-or-db-terminal-status>`; verify by
re-audit; emit `mirror_resynced` or `mirror_resync_failed`.

- Forbidden: `br sync --flush-only` (proven no-op on this drift class — see
  Verified Facts), direct jsonl edits, importing any non-terminal record.
- Fail-closed on the heal decision: any guard that cannot be evaluated
  (e.g. ownership unknown) means do not heal — file instead.
- Files: `internal/core/pan_mirror_health.go`,
  `internal/core/pan_mirror_health_test.go`.

### FR-4: Pan filing for non-healable drift

Every divergent record that FR-3 will not heal (user-owned store,
non-terminal status, write-probe failure, 2-strike, daily cap exceeded) files
one Pan triage event `mirror_drift_needs_human` with store path, record id,
direction, and suggested one-liner (`br sync --import-only` for
jsonl_ahead-terminal, `br update <id> --status <same>` for db_ahead).
Fail-open: filing errors are logged, never propagated.

- Files: `internal/core/pan_mirror_health.go`,
  `internal/core/pan_mirror_health_test.go`.

### FR-5: Config docs, runbook line, SLO check

Document the knobs in `docs/` (mirror-health section beside the stale-filing
sweep docs) and `AGENTS.md` operating table; add the post-emergency-surgery
checklist line to the runbook: after any direct-sqlite repair, run
`br sync --import-only` in that store. Provide
`scripts/mirror-health-slo.sh`: exits 0 iff the event stream shows 0
`mirror_drift_detected` in the last 24h AND ≥1 `mirror_audit_clean` per
registered store.

- Files: `docs/architecture/shepherd.md`, `AGENTS.md`, `CHANGELOG.md`,
  `scripts/mirror-health-slo.sh`.

## Non-Goals

- Healing user-owned stores automatically (humans own their stores; filing only).
- Replacing br's hash tracker (third-party; we work around it, not fix it).
- Any scheduling/closing behavior change — the daemon keeps reading db via br.

## Acceptance

- `go test ./internal/mirroraudit/... ./internal/core/...` green.
- `go run ./tools/beads-mirror-audit --store <any>` output unchanged for users.
- Daemon with default config runs the audit sweep and files Pan events; with
  `auto_resync: true` heals a fixture db_ahead record in a daemon-owned temp
  store via idempotent br update; refuses a non-terminal fixture (asserts
  filing instead).
