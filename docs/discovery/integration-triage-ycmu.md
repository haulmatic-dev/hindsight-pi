# Triage: hydra-ycmu — escalation-aging policy branch (integration-exhausted)

**Disposition: ABANDON-WITH-SUMMARY. Do NOT rebase. Do NOT clear the hard-park.**

## Summary

`hydra-ycmu` carries one unique commit, `7be95d1 feat(core): escalation-aging policy — re-notify,
channel, expire-to-abandon stale human escalations` (internal/core/escalation_aging.go + config
knobs + tests, 481 insertions). The bead's own body proposes exactly this: an escalation-aging
policy so stale human escalations cannot wedge projects.

The problem it addresses **already has a landed fix** on `origin/development`:

- `2ee7449 feat: escalation TTL — age stale human escalations back to ready queue`
  (`internal/core/escalation_ttl.go`, knob `overseer.escalation_ttl`, default 72h) — landed
  2026-09-29, plus the Pan `clear-escalation` repair (see handoff `task-5xvh`-family green phase).

## Evidence

- `git cat-file -e origin/development:internal/core/escalation_aging.go` → does NOT exist: ycmu's
  implementation never landed.
- `git log origin/development..hydra/wt-hydra-ycmu` → 1 commit (7be95d1) + integration commit e4fe245.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-ycmu` → CONFLICT in
  `internal/core/overseer.go`, `internal/core/service_starter.go`, `docs/operations/host-setup.md`.
- `git diff origin/development..hydra/wt-hydra-ycmu -- internal/core/escalation_ttl.go` → branch
  **DELETES** the landed TTL scanner entirely (-139 lines). Full diff vs origin: 78 files,
  +539/−3998 — the branch also deletes `internal/triage/*` and other newer landed work (it predates
  them). Rebasing would revert newer code. Cherry-picking `7be95d1` alone would install a duplicate
  escalation scanner alongside `escalation_ttl.go` — two sweepers racing the same label.
- Fleet-level bead hydra-10ka (shared integration-exhausted cluster) is CLOSED (PR #364 landed);
  per-bead disposition pattern (abandon-with-summary) already established by
  `docs/discovery/integration-wedge-triage-10ka.md` and `integration-triage-iojl-shared-wedge.md`.

## Why abandon, not integrate

1. Landed `escalation_ttl` (72h expire-to-ready) covers the core failure mode ycmu's proposal
   targets; the extra stages (re-notify, channel) are unproven additions racing an existing scanner.
2. The branch is a stale snapshot: it deletes newer landed code (triage package, TTL scanner).
   Rebase = mass revert; cherry-pick = duplicate scanner + conflicts in overseer.go /
   service_starter.go.
3. Established per-bead disposition for this cluster is abandon-with-summary (10ka, d778, 014y,
   242b, q5ey, 9ydi all handled the same way).

## Actions for human/Pan

- `br close hydra-ycmu -r "not_planned: superseded by landed escalation_ttl (2ee7449) + Pan
  clear-escalation repair; see docs/discovery/integration-triage-ycmu.md"`.
- Do NOT clear integration-exhausted / integration-failed labels; do NOT rebase or cherry-pick
  `hydra/wt-hydra-ycmu`; branch may be deleted after closure.
- If the richer stages (re-notify → channel → expire) are ever wanted, design them as an extension
  of `escalation_ttl.go`, not a second scanner.
