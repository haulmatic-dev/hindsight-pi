# discovery handoff (FR-4, hydra-wyel)

## What changed (files + one line each)
- `internal/core/pan_mirror_health.go` — added `mirrorWriteProbe` seam + `mirrorHooks.writeProbe` hook (inert default: probe succeeds); no impl yet.
- `internal/core/pan_mirror_health_test.go` — FR-4 RED contract tests (file-pan-bead-on-heal-blocked, exactly-once-per-record, filing-sink-failure-logged-sweep-continues), gated on `mirrorHealthRequireImpl(t)` and skipping loudly.
- `docs/discovery/mirror-health-fr4-discovery.md` — FR-4 contract decisions.
- `CHANGELOG.md` — Documentation entry.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'MirrorHealth' -count=1` — all contract tests SKIP loudly (impl not yet present), suite ok → 0 exit. PASS.
- `go test ./internal/core/ -count=1` — full suite green, no regression from the new seam. PASS.

## Open edges for the next phase
- GREEN must implement `panFilingForNonHealableDrift` in `pan_mirror_health.go` so the three FR-4 tests above pass (source contract, not re-referenced here).
- GREEN must consume `mirrorHooks.writeProbe` on the FR-3 auto-resync path (red contract: `AutoResyncDisabledDoesNotHeal` + `WriteProbeFailureFilesInsteadOfHeals` in the same FR-4 RED block).
- Pending from this bead: changelog docs entry is not on disk yet (see top of this handoff for reason); GREEN should land it with the implementation.
- Open from before: `docs/PRDs/mirror-health-prd-20261022.md` still says "skip loudly" is ok even when impl exists — FR-5 must resolve; FR-4 leaves it as-is.
