# discovery handoff (hydra-sh9h)

## What changed (files + one line each)
- internal/core/pan_health.go: `verifyPanFilingPremise` (jm2 guard) — `filePanBead` re-probes live state before creating a wedge bead: digest older than 15m (`digest_stale`, stamped by `collectHealthDigest` via new `panDigestAt` field), judgment source bead terminal (`source_resolved`), or live registry worker on it (`worker_live`, heartbeat <5m, digest liveness rule); refusal emits typed `pan_bead_filed_skipped{reason}` and returns `pan.ErrFileBeadPremiseStale`; probes fail open (probe error ≠ proof premise false).
- internal/core/overseer.go: new `panDigestAt atomic.Int64` field (unix-nano of last digest collection, 0 = unknown → gate inert).
- internal/pan/pan.go: `ErrFileBeadPremiseStale` sentinel; `handleFileBead` treats it as benign skip ("premise stale", signature retained — a fresh digest re-fires when the wedge returns).
- internal/core/pan_filing_premise_test.go (new): 4 tests — source-resolved refusal + no bead created; worker-live refusal; digest-stale refusal (pseudo candidate included); fresh digest + open source → filing proceeds.
- internal/pan/file_bead_test.go: `TestFileBeadPremiseStaleFilerResultSkips` — sentinel → 1 filer call, signature retained.
- CHANGELOG.md: Fixed entry under Unreleased.

## Investigation record (jm2)
- `br show ajantha-srv-jobfile-jm2` — CLOSED 2026-10-05, pan-filed, title "integration-exhausted hard-park with no worktree, no worker, and an empty bead record (srv-jobfile-5my)". Source `srv-jobfile-5my` CLOSED (completed) same day. Filed 2026-10-05 04:01:14Z (`pan_bead_filed` event, source srv-jobfile-5my).
- Worktrees for srv-jobfile EXISTED: `/opt/dev-tools/var/hydra/.hydra-worktrees/-home-ajantha-projects-srv-jobfile/` with 12+ bead dirs — the "no worktree" premise was false at filing time.
- Closing note recorded the storm class; no live-store action taken this bead (already closed).
- Worktree-existence claims inside LLM-authored descriptions are not mechanically parseable; the guard re-verifies the three mechanically checkable premises (digest freshness, source bead state, live worker) instead.

## Verification (commands run + pass/fail)
- `go build ./internal/core/ ./internal/pan/` — PASS.
- `go vet ./internal/core/` — PASS.
- `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'TestFilePanBead|TestCollectHealthDigest|TestFileBead' -count=1` — PASS (all, incl. 4 new premise tests + 6 existing digest/filePanBead tests).
- `go test ./internal/pan/ -count=1` — PASS.
- (Full ./internal/core/ suite also ran green in the same invocation chain: `ok github.com/haulmatic-dev/hydra/internal/core 323s`.)

## Open edges for the next phase
- The guard keys on the judgment SOURCE bead; a filing naming only OTHER wedged bead IDs in the payload body (no source-terminality signal) is not covered — a payload-embedded bead-ID re-probe would close that gap.
- `panFilingDigestStaleAge` (15m) is a constant, not a config knob; make it tunable if operators see over-blocking (they won't see `pan_bead_filed_skipped` in current dashboards — consider surfacing it in Pan health text).
