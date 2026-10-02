# task handoff (hydra-i1kt)

## What changed (files + one line each)
- docs/hydra/handoff/task-i1kt.md: this handoff (verify/triage bead; no code change — both P0 claims in the bead body verified stale/resolved).
- CHANGELOG.md: Documentation entry recording the verified disposition.

## Verified outcome (PR #165 publisher wedge)
Task: human merge required on PR #165 — locally-merged work unpublished; disk 99% on /.

Disposition: NO ACTION NEEDED — PR #165 already MERGED; disk below watermark.

- `gh pr view 165 -R haulmatic-devteams/hydra` (daemon PAT from $HYDRA_HOME/.git-credentials) →
  `{"state":"MERGED","mergedAt":"2026-10-02T09:44:27Z","headRefName":"dogfood/publish-hydra","baseRefName":"development"}`. PASS.
- `git fetch origin development` + `git log --oneline -1 origin/development` → `b13bfd2 Merge pull request #165 from haulmatic-devteams/dogfood/publish-hydra` — the PR merge commit IS the development branch tip. Work published. PASS.
- Local `refs/remotes/origin/development` (a36ee76) was stale; fetch healed it. No fast-forward action needed by the daemon beyond its normal tick.

## Disk watermark
- `df -h /` → 193G/451G = **45%**, well below the 85% watermark. No systemd-tmpfiles cleanup, no cache cap, no I/O-error-28 risk. The 99% claim in the bead body is stale.

## Verification (commands run + pass/fail)
- `df -h /` → 45%. PASS (claim stale).
- `gh pr view 165 --json state,mergedAt,mergeable` → MERGED. PASS.
- `git ls-remote … refs/heads/development` → b13bfd2 == PR #165 merge commit. PASS.
- hindsight_search — not connected at bead start (skipped per discovery protocol).

## Open edges for the next phase
- None on this wedge. Precedent pattern stands: publisher-wedge beads whose PR is already merged close verify-first with no code change.
- Recurring observation: disk-99% and PR-pending claims keep arriving stale — the fleet digest produces them hours late; treat such claims as unverified until df/gh checked.
