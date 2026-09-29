# Integration wedge triage — ajantha-srv-telematics shared hard-parks (hydra-b19n)

Date: 2026-09-29 · Bead: hydra-b19n (discovery)

## Incident

Beads hydra-ycmu, 5l11, 36ed, 04jq, 38ot, f10l, 0po4, nygw, 5gq were hard-parked with
`integration-exhausted` within minutes of each other, indicating a shared wedge rather than
per-bead integration failures.

## Root cause (verified)

**Poisoned br WAL index on the srv-telematics bead store.** Every `br show`/write against
`/home/ajantha/projects/srv-telematics/.beads/beads.db` failed with:

```
Error: Sync conflict: automatic WAL index recovery already failed for this exact database
family at stage live-recovery (Database error: database is busy (recovery in progress))
```

With the store unreadable, every integration/close attempt errored → `failIntegration`
counted consecutive failures → `integration-exhausted` hard-park. The parked beads listed
in the pan-filed wedge reports (mymr, 51r7, jo3u, qali, atr9, h2ll, 6iue, ukwe, pmgh) are
themselves CLOSED (quarantine / supersession dispositions from earlier triages) — the
*meta* beads tracking the wedge were the ones accumulating parks.

## Live-store remediation (applied, verified)

- `br doctor migrate-schema recover` — run **twice**; the first pass succeeded but was
  re-poisoned by a contending writer (same recurrence pattern recorded in prior triages:
  "cleared recurring WAL-index poison between write bursts").
- After the second pass: `br show` works for all previously-failing beads; store healthy.

## Duplication finding

The srv-telematics store now holds **10+ overlapping pan-filed meta-beads** tracking the
same wedge (oive, awtj, srch, m369, 5l11, 36ed, f10l, burr, otae, 0po4, nygw). The
`MaxOpenFiledBeadsPerProject=2` cap (hydra-7t1l work) limits *new* filings; the existing
duplicate backlog predates the cap. Human/Pan disposition: close duplicates as
`not_planned: duplicate wedge finding` pointing at the oldest open bead.

## Daemon-side change (this bead)

Pan health digest (`internal/core/pan_health.go`, `internal/pan/health.go`) now surfaces
the wedge pattern instead of letting parks accumulate silently:

- `ProjectHealth.IntegrationExhausted` — count of non-terminal beads labelled
  `integration-exhausted` per project (terminal beads with stale labels excluded).
- Digest text renders `intexh=N` per project line.
- At `sharedWedgeThreshold` (3) open parks, one anomaly is emitted:
  "shared integration wedge (one cause, not per-bead failures); triage the integration
  path, do not clear parks individually". Pan is woken by the anomaly (digest
  anomaly-wake path), giving fleet-level diagnosis without new filings.

## Per-bead disposition

Do **not** blanket `clear-hard-park`. The underlying store is now healthy, so one fair
scheduler retry per meta-bead is reasonable — but each meta-bead is a duplicate-class
finding; prefer closing duplicates (`not_planned`) over clearing parks. Human triage
decides which single bead remains open as the canonical wedge tracker.

## Open edges

- br WAL poison recurs under write contention (third recorded incident). Root fix belongs
  upstream in br (write-lock coordination), not in hydra.
- `integration_retest` recheck path will mechanically probe the parked beads once the
  store is healthy; if any remain truly conflicted with the working branch they stay
  parked (Tier B agentic resolution still unimplemented, deliberately).
