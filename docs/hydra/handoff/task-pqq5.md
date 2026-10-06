# green handoff

## What changed (files + one line each)
- internal/core/gate_artifact.go: new `GateCloseInProgress(root, project, beadID)` — shared probe (running artifact + live writer PID + started <45m) extracted from Charon.
- internal/core/charon.go: `beadCloseInProgress` delegates to `GateCloseInProgress` (per project dir); dropped now-unused imports.
- internal/core/gate_runner.go: `RunGate` + `RunCoverageTruthGate` Start the gate child, rewrite the running artifact with `cmd.Process.Pid` (was `os.Getpid()` = daemon PID, always alive), then Wait.
- internal/core/overseer.go: new `gateRoot` field set from `$HYDRA_HOME` at startup.
- internal/core/pan_health.go: digest orphan-claim loop skips beads with a live running gate artifact (same probe Charon uses) — mid-close beads are not orphans.
- internal/core/gate_close_in_progress_test.go (new), internal/core/pan_health_test.go: 5 new tests.
- CHANGELOG.md, docs/architecture/shepherd.md: entries.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — PASS. `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'TestGateRunner_ArtifactRecordsChildPID|TestGateCloseInProgress|TestCollectHealthDigestCloseInProgressNotOrphan|TestCollectHealthDigestOrphanClaims|TestCharon|TestRecoverOrphan' -count=1` — PASS (ok 86.6s).

## Verified findings (live triage, 2026-10-06 12:2x–12:37 UTC)
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 4 live `working` rows (95ia, v5xq, hu12, pqq5), PIDs alive, heartbeats <1 min. Zero live orphans.
- All 13 in_progress beads: either pan-filed P0 twins (pqq5, hu12, v5xq, 0ndi, 2a3f, l5ug), human-escalated (q207, rxi6), or beads whose workers COMPLETED exit 0 minutes earlier (cw35, bldo, sh9h, nydy, lkbo, s8l7, lz3m, gcal) with close gates mid-run — Charon correctly logged "close in progress (gate artifact running); leaving in_progress" for every one and reported orphans_found=0.
- Root cause of the PERSISTING anomaly: digest false positive. Gate artifacts recorded the daemon PID (3172191) as writer; the digest lacked the close-in-progress probe, so mid-close beads counted as "in_progress with no live registry worker" → orphanclaims=6 → Pan filed a new P0 twin (pqq5/hu12/v5xq/0ndi) every round. The DAG was never wedged — Charon/scheduler reconciliation were working as designed.
- Secondary defect fixed: stale `running` artifacts with daemon PID could never be finalized as interrupted (`FinalizeStaleGateArtifacts` checks PID liveness); child PID now recorded.
- Disk: `df -h /` = 208G/451G = **49%** — the 99% claim in the bead body is STALE; no systemd-tmpfiles cleanup required (consistent with every prior triage of this recurring stale claim).

## Open edges for the next phase
- The digest fix requires a daemon rebuild + restart to take effect; until then Pan may file one more twin per round.
- Operator may close the duplicate P0 twins (hydra-hu12, hydra-v5xq, hydra-0ndi, hydra-2a3f, hydra-l5ug, plus this bead pqq5) as duplicate filings once their workers exit.
- cw35/bldo/lkbo/95ia legitimately loop gate-failed-test close rejections — that churn is the verified-close round machinery, not an orphan wedge; separate concern.
