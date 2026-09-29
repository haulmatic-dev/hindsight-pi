# Integration-exhausted shared wedge — triage (hydra-d71g)

Date: 2026-09-29 · Projects: hydra, ajantha-srv-telematics

## Symptom

21+ beads hard-parked `integration-exhausted` across both stores within minutes
of each other — fleet-level wedge suspected.

## Root cause (VERIFIED)

**Phase 1 — the mass storm (pre-16:57 UTC): stale daemon binary.** 1,380
`already exists` failures on 2026-09-29. The publisher kept failing
`gh pr create` with "a pull request for branch ... already exists" instead of
reusing the open PR. The reuse fixes (b5eacb0, 76bf0c5, ecc6cb5) had landed on
`origin/development`, but the deployed daemon binary predated them. Binary was
rebuilt and redeployed at 16:56:58 UTC from the reconciled checkout (9a905d3).

**Phase 1 verification:**
- `grep 'already exists' events/2026-09-29.jsonl` → 1380 hits, **zero after
  16:57**.
- `/var/log/hydra/hydra-error.log` shows the fix in action:
  `[Publish] PR for dogfood/ajantha-srv-telematics-5y3v already exists —
  reusing PR from create error: https://github.com/haulmatic-dev/srv-t...`
  (17:27:47, plus df0w ×2, 4p14). The parked-by-storm beads are landing via
  reused PRs.

**Phase 2 — residual integration-failed events (post-restart): duplicate
implementations of the same fix colliding textually.** Branches
o0cn/pmed/4axh/7ywm/xxq5 carry d6c5752 (6zwl PR-reuse reconciliation, adds
`prURLFromError` fast-path); `origin/development` carries ecc6cb5 (the
fail-closed resolver restore, landed via PR #393). Same semantic contract,
different text → rebase conflicts in `internal/core/publisher.go` for all of
them. The reconciled combination **already exists** as the local checkout tip
9a905d3 (= ecc6cb5 base + d6c5752), builds clean, and is what the running
daemon executes. Every conflicted branch is therefore a superseded iteration:
its unique code delta is already in production.

## Current park inventory (VERIFIED via store SQLite)

- srv-telematics: **132** beads carry `integration-exhausted` — **all closed**;
  zero open. Nothing to batch-clear.
- hydra: **one** open bead with the label — `hydra-15zh`, itself a pan-filed
  meta bead tracking this very wedge, gate-failed on the (now-landed) dedupe.

## Disposition

1. **No code change.** The cause (stale binary) is fixed and deployed; the
   textual collisions are superseded duplicates of landed work. Tier B agentic
   conflict resolution remains the separate, still-open proposal (qzxv/xlkw).
2. **No batch park-clear needed** — no parked work beads remain in either
   store; parks were consumed by closure waves and the deployed reuse fix.
3. **Hygiene:** hydra-15zh closed as superseded (live store). srv-telematics
   wedge-tracker meta beads remain for their owners; the surviving canonical
   trackers are xlkw (srv) and qzxv (hydra).

## Open edges

- Tier B (agentic conflict resolution) still unimplemented — the honest fix
  for the residual duplicate-iteration conflicts; do not force-clear those
  branches.
- `dogfood/publish-hydra` PR #394 pending: landing 9a905d3's reconciliation
  would make future wt-branch rebases conflict-free for the 6zwl lineage.
- srv-telematics rebase warnings `skipped previously applied commit 99ec632c`
  on many branches — cosmetic, but inflates every retry log; consider
  `--reapply-cherry-picks` no, `--fork-point` reset in rebaseBranchOntoBase.
