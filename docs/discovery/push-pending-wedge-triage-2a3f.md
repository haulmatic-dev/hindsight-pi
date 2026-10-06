# Push-Pending Wedge Triage — hydra-2a3f (2026-10-06)

## Claim

Four beads BLOCKED with PUSH-PENDING (commits local, not on origin); pushing is human/pipeline
action; proposal: human/CI push + audit why multiple beads accumulated unpushed commits +
consider a project-scoped sweep batching push-pending reporting instead of per-bead BLOCKED states.

## Verification findings (all VERIFIED at 2026-10-06 ~11:30Z)

1. **Zero BLOCKED beads today.** Swept every live store on the host
   (/opt/dev-tools/components/{hydra,infra}, /home/ajantha/projects/{srv-jobfile,srv-smart-engine,
   srv-telematics,webapp}, /home/nipuna.j/projects/webapp, /home/yasanjith/projects/demo) with
   `br list --status blocked` — no results in any store. The "4 blocked beads" premise is stale;
   the accumulation class self-resolved (see 3).
2. **Hydra checkout fully published.** `git ls-remote origin development` = 3ac4484; local
   `/opt/dev-tools/components/hydra` HEAD = 3ac4484 (fast-forwarded during this bead);
   `git rev-list --left-right --count origin/development...HEAD` = `0 0`. No unpushed commits.
3. **Root cause of the accumulation, and its fix already landed.** PRs #238/#227 (and ~20 other
   open dogfood/* PRs) sat awaiting human merge; the publisher emitted
   `publish_push_rejected (pr_merge_failed)` on **every publish tick** while a standing-open PR
   waited — 114 events/day across 3 repos — and that failure-flavored churn made the Pan digest
   re-fire `pan_bead_filed` pseudo-beads round after round (hydra-hrdh 10-04, hydra-2a3f and
   hydra-quy0 10-05, all sourced `hydra-push-pending`). Remedy landed **today** as
   e17e8dc "fix(publish): idempotent waiting — standing-open PR stops the retry churn" (PR #250):
   first wait emits `publish_pr_waiting (pr_awaiting_human)` with the PR URL; later ticks skip
   while the branch tip is unchanged; new work re-publishes. Deployed binary
   `/opt/dev-tools/var/hydra/bin/hydra` contains the `publish_pr_waiting` symbol (strings hit) —
   the fix is **live**, not just merged. This IS the project-scoped batching the bead proposed.
4. **Residual live push-pending is real and conflict-bound.** Two standing PRs currently wait
   on human merge and cannot be admin-merged:
   - fms-frontend PR #144 (HMT-Dev-Teams/fms-frontend, `hydra/publish-fms-frontend` →
     `development`): `gh pr merge --admin` → "X Pull request #144 is not mergeable: the merge
     commit cannot be cleanly created." mergeStateStatus UNKNOWN/unmergeable.
   - srv-telematics PR #417 (haulmatic-dev/srv-telematics, `hydra/publish-srv-telematics` →
     `feat/intergration-test-plan`): same refusal; mergeStateStatus **DIRTY**.
   Both are the conflict class — per-bead repairs and direct pushes cannot resolve; requires
   rebase/conflict resolution (Tier B agentic resolution remains unimplemented; otherwise human).
   Token note: daemon + buddhi PATs authenticate as `haulmatic-devteams` and cannot see
   `haulmatic-dev/srv-telematics`; only ajantha's owner PAT (AjanthaHM) can.

## Disposition

- Bead body's four-BLOCKED-beads wedge: stale — no blocked beads exist in any store, and the
  hydra project has no unpushed commits.
- Audit answer: the accumulation was the publisher's per-tick rejected-event churn against a
  standing-open PR; fixed and deployed same-day (PR #250).
- Proposal (project-scoped batched push-pending reporting): already implemented as
  `publish_pr_waiting` + idempotent skip; no new code warranted.
- Residual: merge PRs #144 and #417 after conflict resolution (human or future Tier B bead).
