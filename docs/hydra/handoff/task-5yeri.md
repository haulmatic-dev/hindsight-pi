# REFACTOR handoff (task-5yeri — completion-time scar scrub FR-001)

## What changed (files + one line each)
- internal/core/quarantine_scar.go: behavior-preserving readability pass — chain key resolution (`WorktreeKey` else `RootBeadID`) deduped into `chainKeyOf` and reused in `run`/`quarantineNow`/`scarLegacyQuarantine`/`scarAfterScrub`; `launchScarScrub`'s duplicate-completion guard collapsed from `scarScrubActive`-check + double `Store` into one `scarClaim` LoadOrStore claim (single goroutine owns the marker from claim); garbled duplicated doc comment on `launchScarScrub` and the stale RED-phase file header corrected; h.quarantine wrapper retained (signature differs from `scarQuarantineChain`). No signatures changed; no behavior change.
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit baseline): `go test ./internal/core/ -run 'ScarScrub' -count=1` — ok 0.947s, all pass.
- AFTER: `go build ./internal/core/` PASS; `go vet ./internal/core/` clean; `gofmt -l internal/core/quarantine_scar.go` clean; `go test ./internal/core/ -run 'ScarScrub' -count=1 -v` — PASS (6/6 incl. panic/cap/restamp/restart/launch); `go test ./internal/core/ -run 'ScarScrub|Quarantine|ChainCompletion' -count=1` — ok 2.244s.
- graphify affected: no node match (graph.json predates GREEN); grep confirmed all scarScrubActive/scarMarkerGuarded call sites are in the test file with unchanged signatures.
- Commit e1378509 verified via `git show --stat` (2 files, +40/−41).
- One compile iteration: direct `h.quarantine = scarQuarantineChain` assignment rejected by vet (extra `*Overseer` receiver) — reverted to the thin wrapper.

## Open edges for the next phase
- Known accepted limitation unchanged: `scarProbeLast` shared-context seam can mis-attribute under two concurrent chains (wrong = fail-closed, verdict stands); documented in `wireScarScrubHooks` comments.
- No benchmarks exist for the scrub path; none added.
