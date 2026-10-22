# Discovery/task handoff — bead qgvr: shared audit package (single source of truth)

## State at handoff
All three acceptance criteria verified green in this worktree:
- `go test ./internal/mirroraudit/ -count=1` — "Test Results: PASS: 1 passed".
- `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads` — "mirror clean (49 beads)", exit 0.
- `go build ./...` — exit 0 (verified with /usr/bin/go).

## Root causes fixed this session
1. `internal/core/pan_mirror_health.go` referenced undefined `o.brUpdater` field (BrUpdater interface existed locally but was never wired). Heal call routed through existing `o.beadsClient.UpdateBeadStatus(project.Path, types.BeadID(rec.BeadID), types.BeadStatus(status))` (internal/beads/client.go:343).
2. `br list --json` now returns an `{"issues":[...],"total":N}` envelope, not a bare array. `mirroraudit.parseBrList` accepts both shapes (audit.go).
3. Comparers now pass `--all --limit 0` to `br list --json`; default omits closed beads, which made live stores falsely classify every closed bead as `jsonl_ahead` drift. Note: `--limit 0` is what actually widens the set (the `--all` flag alone had no measureable effect on br v1.0.x output).

## Files changed
- internal/core/pan_mirror_health.go — compile fix via beadsClient route.
- internal/mirroraudit/audit.go — parseBrList envelope handling; comparer `--all --limit 0` args.
- tools/beads-mirror-audit/main.go — same arg fix in CLI path.
- CHANGELOG.md — Unreleased Fixed/Added entries.

## Single-source contract (unchanged)
- Production classification lives only in `internal/mirroraudit` (`Audit(storePath, nil)`).
- `tools/beads-mirror-audit/main.go` imports the shared package; `internal/mirroraudit/single_source_test.go` (MIRROR_AUDIT_SOT_ENFORCE=1) pins this.

## Open edges for next bead
- `dailyHealCount(storePath)` in pan_mirror_health.go returns 0 (TODO(GREEN): wire from ledger) — not an AC blocker.
- Sweep registration in service_starter.go and `mirror_health.enabled`/`mirror_health.audit_interval` knobs verified by prior beads (defaults true/30m, ledger at $HYDRA_HOME/mirror-health-ledger.json).
- Commit any untracked files in this worktree before integration (git status showed untracked mirroraudit files in a prior strand).
# Discovery handoff: hydra-qgvr — Shared audit package (single source of truth)

**Scope:** Fix compile-gate failure in `internal/core/pan_mirror_health.go`.

**Result:** Healing path now routes via `o.beadsClient.UpdateBeadStatus(project.Path, types.BeadID(rec.BeadID), types.BeadStatus(status))` (interface seam at internal/beads/client.go:343). The previously-referenced `o.brUpdater` field was never wired on `*Overseer`.

**Verification:**
- `go build ./...` — exit 0
- `go vet ./internal/core/` — clean
- `go test ./internal/core/` — PASS
- `go test ./internal/mirroraudit/ -count=1` — PASS (fixture: clean)
- `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads` — clean (49 beads, mirror clean)

**Landing:** commit e210b18 (fix(mirror-health)).
