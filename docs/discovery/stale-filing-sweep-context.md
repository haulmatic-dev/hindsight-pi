# Discovery: Stale-filing sweep covers in-progress filings with cleared premises

**Bead type:** FR (discovery phase) · **Date:** 2026-10-22

## Summary

Hydra already has *creation-time* staleness gates on pan filing (premise
re-verified before a wedge bead is created), and *cohort-level* deterministic
sweeps for worktree/spawn wedges. What is still missing is the *closure-side*
mirror: a periodic sweep that closes pan-filed beads whose premise has
**cleared** — including beads that are currently `in_progress` (claimed by a
worker) — without violating the fail-open or anti-loop contracts established
by earlier fixes.

## Existing mechanisms (verified on this branch)

### 1. Premise gate at file time (`internal/core/pan_health.go`)

`verifyPanFilingPremise` re-probes live state before `filePanBead` creates a
wedge bead. Refusal reasons (each emits typed `pan_bead_filed_skipped{reason}`):

| Reason            | Check                                                          |
|-------------------|----------------------------------------------------------------|
| `digest_stale`    | Digest older than `panFilingDigestStaleAge` (15m) or never collected (`panDigestAt == 0`) |
| `source_resolved` | Judgment source bead is terminal — the wedge premise no longer holds |
| `worker_live`     | Live registry worker (heartbeat < 5m) already on the target bead |

Probes **fail open**: a probe error is never treated as proof the premise is
false. On refusal, `internal/pan/pan.go` treats `pan.ErrFileBeadPremiseStale`
as a **benign skip** — the filer's signature is retained so a fresh digest
re-fires when the wedge returns.

### 2. Deterministic cohort sweep (`internal/pan/prune_sweep.go`)

`PrunableWedgeSweep` groups BLOCKED candidates sharing a worktree/registration
or spawn wedge key and runs ONE project-scoped batch repair
(`prune-worktree-registration` or `recover-spawn-cohort`) per cohort once
`prunableSweepThreshold` (2) is reached. Fail-open by contract; explicit
`SweepReport{Successes, Cleared, Residuals}` for logging. Deliberately
conservative: worktree/spawn wedges only — human escalations and integration
wedges are **never** swept (wrong-tool history 2026-09-14).

### 3. Integration-exhausted stale park surfacing

`IntegrationExhaustedSweeper` (`overseer.integration_exhausted_sweep`, default
48h) surfaces stale parks for human labeling via `integration_exhausted_stale`
events. Uses `UpdatedAt` as age proxy; never clears the park itself.

## What is missing (the gap this FR closes)

The premise gate protects **creation**; nothing sweeps **existing** filings:

- A pan-filed bead whose source bead has since closed, or whose wedge
  self-resolved (worker exited cleanly, gate re-ran green), stays open and
  occupies one of the two `MaxOpenFiledBeadsPerProject` slots until a human
  or Pan judgment closes it.
- A pan-filed bead that has been *claimed* (`in_progress`) is even harder to
  retire: today's cap and dedupe counts non-terminal beads
  (`b.Status.IsTerminal()` skip) — an in-progress filed bead holds its slot,
  which is correct for real work but wrong for a filing whose premise already
  cleared before the worker started.

## Design constraints for the implementation phase (from prior incidents)

1. **Fail-open, always.** Every sweep mutation must be nil-sink-safe and
   never block the digest or filer on error (pattern: `PrunableWedgeSweep`,
   `postclose_sweep.go`).
2. **Never self-fire.** The digest excludes pan-filed beads from
   `InProgress` counting (qqtl fix) and from orphan claims (completed-row
   skip, xcye fix) precisely to stop the self-perpetuating P0 filing loop.
   The closure sweep must not re-create that loop: closing a stale filing
   must not re-file a twin about the closure itself.
3. **Reuse premise probes verbatim.** The sweep's staleness check should
   call the same `verifyPanFilingPremise`-family probes (source terminal,
   digest freshness, live registry worker) rather than duplicating the
   logic — a divergence here is how the 2026-10-02 orphan-claim churn
   started.
4. **Respect in-progress filings.** A filing that is genuinely being worked
   (fresh registry heartbeat) must NOT be closed by the sweep — only filings
   whose premise cleared *and* which are not under live worker control.
5. **Typed events.** Emit a distinct event (e.g. `pan_filing_closed_stale`)
   per closure, with the cleared-premise reason, for the audit trail —
   matching the `pan_bead_filed_skipped` / `empty_diff_completed` pattern.
6. **Dedupe/cap interplay.** Closing a stale filing frees a cap slot
   (correct), but the closure must not increment any retry or escalation
   counter that would re-trigger the filer for the same signature while
   `panFileSigTTL` is still warm.

## Relevant files

| File | Role |
|------|------|
| `internal/core/pan_health.go` | Digest collection, premise gate, `filePanBead` cap/dedupe |
| `internal/core/pan_filing_premise_test.go` | Premise-gate tests (source-resolved, worker-live, digest-stale, fresh-digest-proceeds) |
| `internal/pan/pan.go` | `ErrFileBeadPremiseStale`, filer, `PrunableWedgeSweep` dispatch |
| `internal/pan/prune_sweep.go` | Deterministic cohort sweep (pattern for a fail-open sweep) |
| `internal/core/overseer.go` | `panDigestAt` stamping, sweep service wiring |
| `internal/core/service_starter.go` | Where a new closure sweep would be started/stopped |

## Next phase

Implementation: add a periodic stale-filing closure sweep reusing the
premise probes, gated on (a) premise cleared, (b) no live worker, (c)
digest fresh — emitting `pan_filing_closed_stale` and keeping the digest
exclusions intact.
