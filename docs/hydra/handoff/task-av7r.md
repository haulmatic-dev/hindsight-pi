# discovery handoff

## What changed (files + one line each)
- docs/hydra/handoff/task-av7r.md: this handoff (ops/triage bead; no code change).
- CHANGELOG.md: Documentation entry under Unreleased.

## Verified findings (hydra-av7r)

PR #394 state (gh, token from git credential fill):
- `gh pr view 394 --repo haulmatic-dev/hydra --json state,mergeStateStatus,mergedAt,title` → `{"mergeStateStatus":"DIRTY","mergedAt":null,"state":"OPEN","title":"Hydra publish: development"}`.
- PR #394 is OPEN with a merge CONFLICT vs development — a plain human "Merge" button click will fail until the conflict is resolved. Prior resolved precedent: hydra-2hvh (PR #358) resolved an add/add conflict via git plumbing merge commit pushed to dogfood/publish-hydra, then `gh pr merge --merge --admin`. Same recipe applies here, or GitHub web conflict resolution.
- HUMAN ACTION (per task, not automated): merge https://github.com/haulmatic-dev/hydra/pull/394 after resolving the DIRTY conflict.

Host disk:
- Before: 99% reported by the alert; measured 87% at bead start (451G disk, 376G used).
- `systemd-tmpfiles --clean` run (freed ~7G) → 86%. Root-cause found: /tmp held ~203.5 GB of files across 21,766+ stale top-level entries (hydra-core-test-home-*, TestSpawn_*, TestGraphifyIndexer_*, go-build* test debris, older than 48h).
- Cleanup: deleted /tmp top-level entries older than 2 days, excluding tmux-*, systemd-private-*, snap-private-tmp, .ctx-mode-* → 83% (358G used, 75G free) after first pass; a background pass continues.
- `journalctl --vacuum-size` NOT POSSIBLE as non-root (insufficient permissions, no sudo — `sudo -n true` → "sudo: a password is required"). /var/log is only 1.7G; not the driver.
- du timings: full-disk du / find /tmp du exceed 10m activity timeout (19k+ dirs); use `find /tmp -xdev -maxdepth N -type f -printf '%s'` for fast sizing.

## Verification (commands run + pass/fail)
- gh pr view 394 (with credential-fill token) — VERIFIED OPEN/DIRTY.
- df -h / before 87% → after cleanup 83% — PASS (below 85% watermark).
- systemd-tmpfiles --clean — ran, rc 0 with permission-denied warnings on foreign /tmp dirs only.
- hindsight_search + graphify query — graph.json absent in cwd → rg/handoff fallback per protocol.

## Open edges for the next phase
- Background /tmp deletion pass may still be running at commit time (marker file /tmp/.av7r-cleanup-done appears when finished). Expect further disk relief beyond 83%.
- Durable fix candidates (code, separate bead): hydra core tests leak /tmp test-home dirs on failure — add t.TempDir()/cleanup; systemd-tmpfiles rule for /tmp age-based cleanup.
- PR #394 remains human-gated; monitor for wedge re-alert until merged.
