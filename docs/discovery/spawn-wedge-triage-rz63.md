# Spawn-failed wedge triage — project hydra (hydra-rz63)

Date: 2026-10-06 ~13:41–13:45 UTC
Bead: hydra-rz63 "Project-scoped spawn-failed wedge in project hydra" (pan-filed)

## Task premise

Three beads blocked by "spawn failed" — a shared cohort wedge; the deterministic
PrunableWedgeSweep should run one project-scoped repair per sweep tick; bead flags it
persisting. Proposal: verify sweep firing + investigate underlying spawn failure cause.

## Verification (evidence)

1. **Sweep IS firing for this project.** `/var/log/hydra/hydra-error.log`
   (log.Printf output of `internal/pan/prune_sweep.go`):
   - 13:32:55 → 13:40:06 (5 ticks): `[PruneSweep] residual: spawn failed — cohort of 1
     below sweep threshold 2; left to normal judgment`
   - 13:40:57: `[PruneSweep] cohort "spawn failed" (2 beads): prune-worktree-registration
     succeeded for hydra-bldo — cleared in batch`; `done: 1 cohort(s) pruned, 0 residual(s)`
   - Note: the log line prints the legacy verb name; the actual verb chosen for a
     spawn-class cohort key is `recover-spawn-cohort` (prune_sweep.go verb selection).
2. **Repair executed with the correct verb:**
   `[PanRepair] recover-spawn-cohort cleared 2 spawn failure(s) in
   /opt/dev-tools/components/hydra`; `{"action":"recover-spawn-cohort","bead_id":"hydra-bldo","result":"ok"}`.
   `[WorkerSpawner] cleared spawn failure for hydra-rz63` + `for hydra-bldo` at 13:40:57.
3. **Cohort members:** hydra-rz63 (this bead) + hydra-bldo. The third reported bead was a
   below-threshold residual of 1 (threshold = 2, `prunableSweepThreshold`); single-bead
   wedges are deliberately left to normal judgment flow per sweep contract.
4. **Underlying spawn-failure cause (registry/registration class):** NOT a worktree or
   registry-registration fault. Live log 13:39–13:40 shows the classic duplicate-check
   race: `[spawnWorker] duplicate_check issue | bead=hydra-rz63 | error=spawn already in
   flight for bead hydra-rz63` → retry counted as a spawn failure entry
   (`[Retry] Bead hydra-rz63 failure recorded (faults=1 ...): spawn already in flight`).
   The in-flight guard is correct behavior; the spawn-failure entry it records is the
   wedge artifact, and recover-spawn-cohort is the designed purge for exactly this.
5. **Wedge self-resolved post-repair:** 13:41:14 `Immediate scheduling complete: spawned
   2 workers` — including this bead's own worker. Daemon PID 3172191 (started 12:16:28,
   binary mtime 12:16:29) carries `recover-spawn-cohort` in the deployed binary
   (strings ×7). Deploy current.
6. **No code defect:** stale-entry purge (fmsfe-npa), threshold-2 cohort rule, and the
   recover-spawn-cohort verb all behaved as designed; nothing persisted.

## Disposition

No code change. The "persisting wedge" premise was a below-threshold residual plus a
transient duplicate-check spawn-failure cohort; one recover-spawn-cohort tick cleared it
and both beads respawned. `prunableWedgeKey` correctly routes spawn-class cohorts to
`recover-spawn-cohort` rather than `prune-worktree-registration`.

## Open edges (observations, not defects)

- `[PruneSweep]` success log hardcodes the string "prune-worktree-registration
  succeeded" even when the verb was recover-spawn-cohort — cosmetic log-accuracy nit;
  the structured `[PanRepair]` event carries the true verb.
- Repeated cohort-of-1 residuals every ~3 min for the same reason could prefix-flap
  below threshold; acceptable per design (single bead = not a shared wedge).
