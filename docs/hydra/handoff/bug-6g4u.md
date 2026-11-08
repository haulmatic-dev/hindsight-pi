# bug handoff (hydra-6g4u)

## What changed (files + one line each)
- internal/core/postclose_sweep.go (gate-repair follow-up, same bead): `scan()` implemented per the RED-pinned contract — closed beads <24h with `metadata["test_command"]` re-run once (flake guard → `postclose-flake` label), persistent failure reopens with `postclose-reopened` + comment carrying BOTH failing-run outputs (`runOutput` falls back to `err.Error()` when output is empty, e.g. `false`), already-reopened second failure adds `needs-human` instead; hourly Start loop nil-safe; all errors fail open. NOT wired into overseer/service_starter yet — wiring needs concrete runCommand/addComment impls (open edge).
- CHANGELOG.md: Fixed entry for the sweep implementation.
- internal/core/spawn_preflight.go: new `daemonUID` test seam (production: `os.Geteuid()`), `storeOwnerUID` (stats `<projectPath>/.beads`; missing → not-applicable, fail-open), `spawnPreflightStoreOwnership` — suppresses spawn into any project whose `.beads` store is owned by a uid ≠ the daemon's, with typed `spawn_blocked_store_ownership` event and a remedy in the error reason; probe errors fail open.
- internal/core/overseer.go: preflight wired in `spawnWorker` between `spawnPreflightBead` and `spawnPreflightPublishIdentity` (single choke point for all spawn paths); comment updated.
- internal/core/spawn_preflight_store_ownership_test.go (new): 3 tests — foreign-uid store suppressed with *SpawnSuppressedError (skips when the test itself runs as uid 999), missing store + empty projectPath fail open, matching-uid store passes.
- CHANGELOG.md: Fixed entry under Unreleased.

## Root cause / defect (verified live, 2026-10-13)
- `/home/ajantha/projects/webapp/.beads`: `beads.db` owned by uid 999 (hydra, daemon), `beads.db-fsqlite-ns-gate`/`-ns-use` owned by root (migration ran as root 2026-10-03). Same for srv-smart-engine.
- br's fsqlite WAL layer refuses a namespace sidecar owned by a different uid → repo owner running br interactively hits `fsqlite refused its namespace sidecar ... owned by uid 999`. Enforced inside br's Rust fsqlite layer (strings `sidecar_mismatch` / `orphaned-sidecar` in the binary) — not fixable from hydra; the hydra-side remedy is the bead's option (a): daemon stops managing user-owned repos.
- Ajantha-side refusal NOT reproduced directly (no sudo; `setpriv --reuid` → "setresuid failed: Operation not permitted"); the defect condition (daemon-owned store in a user-owned repo) and the guard's live behavior were both verified instead.
- Owner separation verified: hydra's own store `/opt/dev-tools/components/hydra/.beads` = uid 999; ajantha's stores = uid 2008 (bead text said uid 1000; actual uid is 2008 — the lock complaint stands regardless).

## Verification (commands run + pass/fail)
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run TestSpawnPreflightStoreOwnership -count=1` — PASS (ok, 3 tests).
- Live probe (throwaway test, removed before commit): `/opt/dev-tools/components/hydra` → nil (pass); `/home/ajantha/projects/webapp` and `/srv-smart-engine` → `spawn suppressed: bead probe-1: store owned by foreign uid 2008 (remedy: chown the store to the daemon uid or manage the repo user-side)`. PASS.

## Open edges for the next phase
- Consequence of option (a): webapp and srv-smart-engine (ajantha-owned, explicitly registered with custom daemon-owned gates) stop receiving hydra workers until ownership is reconciled — operator must either `chown -R hydra:beads <repo>/.beads` (keeps daemon managing) or accept user-side-only management. Their beads will no longer be worked by the fleet in the meantime.
- Schema drift remains open: fleet stores are schema v11; br 0.7.0 cannot migrate them (supports 13+). 0.7.0 binary backup: /tmp/stale-beads-debris/br-0.7.0-backup.
- Option (b) (br gains multi-uid tolerance) remains a br-side change, out of this repo.
- If the suppression re-fires after a chown, the guard auto-resumes (probe per spawn, no sticky state).
