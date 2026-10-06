# discovery handoff (hydra-rz63)

## What changed (files + one line each)
- docs/discovery/spawn-wedge-triage-rz63.md: triage — PrunableWedgeSweep verified firing for project hydra; the spawn-failed cohort was transient (recover-spawn-cohort cleared 2 entries at 13:40:57 UTC, both beads respawned 13:41:14); no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- hindsight_search at bead start — not connected (skip rule applied). PASS.
- graphify-out/graph.json absent in cwd → rg fallback per protocol; sweep code located via rg (internal/pan/prune_sweep.go, internal/pan/pan.go:769). PASS.
- `/var/log/hydra/hydra-error.log` — sweep residuals 13:32:55–13:40:06 ("cohort of 1 below sweep threshold 2"), then 13:40:57 `cohort "spawn failed" (2 beads): ... succeeded for hydra-bldo — cleared in batch`, `done: 1 cohort(s) pruned, 0 residual(s)`. PASS (sweep firing).
- `[PanRepair] recover-spawn-cohort cleared 2 spawn failure(s) in /opt/dev-tools/components/hydra` + `[WorkerSpawner] cleared spawn failure for hydra-rz63 / hydra-bldo` — correct verb for spawn-class cohort. PASS.
- Root cause evidence: `[spawnWorker] duplicate_check issue | bead=hydra-rz63 | error=spawn already in flight for bead hydra-rz63` → `[Retry] Bead hydra-rz63 failure recorded (faults=1, infra=0 ...): spawn already in flight` — duplicate-check race recording the wedge artifact, not a worktree/registry-registration fault. PASS.
- Deployed binary `/opt/dev-tools/var/hydra/bin/hydra` mtime 12:16:29 == daemon start 12:16:28 (PID 3172191, uptime verified via ps); `strings` → `recover-spawn-cohort` ×7 — current. PASS.
- 13:41:14 `Immediate scheduling complete: spawned 2 workers` — cohort members (incl. this bead) respawned; wedge self-resolved. PASS.
- `br doctor migrate-schema recover` run at bead start (store WAL hygiene, recurring poison). PASS.

## Open edges for the next phase
- Cosmetic: `[PruneSweep]` success log hardcodes "prune-worktree-registration succeeded" even when the verb was recover-spawn-cohort; structured `[PanRepair]` event carries the true verb. Log-accuracy nit, not fixed here (smallest-change discipline).
- No further action for the wedge: none persisting; store shows no open spawn-failed cohort.
