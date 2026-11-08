# task handoff (task-v4s5)

## What changed (files + one line each)
- internal/core/pan_filed_promotion.go (new): `codeChangeMarker` ("code change suggested", case-insensitive, title+description), `isCodeChangeProposal`, and `PanFiledPromoter` — retroactive reconciler (5m tick + startup scan) that strips `pan-filed` from non-terminal task-type beads whose text carries the marker, emitting typed `pan_filed_promoted`; human-triage filings (no marker) stay excluded; non-task types skipped (promoting an epic would put an unspawnable bead into discovery); fail-open on list/label errors.
- internal/core/pan_health.go: filing-time routing — `filePanBead` files code-change proposals as normal task beads with NO `pan-filed` label (routed: "code-change" in the `pan_bead_filed` event + log), so discovery filterByDAG and the spawn preflight admit them; the per-project cap still applies — the cap counting loop now counts proposal filings (no pan-filed label) via `isCodeChangeProposal(b.Title, b.Description)` as well as labeled beads.
- internal/core/scheduler.go: retry-spawn container-type filter (approval-gate/approval/epic/hold, ~line 648) now emits `⏸️  Skipping container-type bead in retry path ...` via extracted `logContainerTypeSkip` helper — the silent `continue` that made the 2026-10-21 05:37–05:52 freeze undiagnosable.
- internal/core/overseer.go: `panPromoter` field + construction (`NewPanFiledPromoter(o.beadsClient, o.GetProjects, o.eventSink)`).
- internal/core/service_starter.go: Start/Stop wiring alongside escalationTTL.
- internal/core/pan_filed_promotion_test.go (new): 8 tests — marker matrix, promoted (label stripped), no-marker stays, non-task stays, terminal+unlabeled skipped, `pan_filed_promoted` event read back from real EventSink JSONL, Start/Stop loop promotes, nil inert, log emission pinned.
- internal/core/pan_filer_test.go (+1): `TestFilePanBead_CodeChangeProposalRoutedAsTask` — proposal files unlabeled + type task, triage filing keeps label, both count toward cap (cap reached asserted).
- CHANGELOG.md: 2 entries under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestFilePanBead|TestPanFiledPromoter|TestIsCodeChangeProposal|TestLogContainerTypeSkip' -count=1` — PASS, 22 tests, 0 FAIL.
- `go test ./internal/pan/ -count=1` — PASS (filer contract unchanged for label-carrying path).
- `go build ./...` — PASS ("Build successful").
- `go vet ./internal/core/` — clean; gofmt clean on touched files.
- Broader scoped runs PASS: `TestCollectHealthDigest|TestEscalationTTL|TestOrphan|TestFilterByDAG` (ok 30.2s), `TestTick|TestSpawn|TestSkipNonExecutable` (ok), `TestScheduler|TestDiscovery` (ok).
- FULL `go test ./internal/core/` — FAIL, NOT caused by this change: (a) `TestBeadDiscovery_RealMultiRepo` / `TestRefreshDiscoveredProjects_RealReposMidRun` fail identically with my diff stashed (baseline re-run: same `br [init] ... failed: exit status 127` — `br` lives at /opt/dev-tools/config/pi/bin, not on the test PATH; env-coupled, pre-existing); (b) the second run crashed in `runtime/cgo: pthread_create failed: Resource temporarily unavailable` (host PID/thread exhaustion — the same EAGAIN hitting this session's own shell) inside pre-existing `TestPerformCleanup_ReapsGhostRegistryRows`. VERIFIED pre-existing/env.
- LIVE promotion observation: NOT DONE this run — requires a deployed daemon rebuild+restart; the mechanism is pinned by tests (marker→promoted+event, cap shared).

## Open edges for the next phase
- Live verification: rebuild daemon from this strand, restart, seed a pan-filed code-change bead, confirm `pan_filed_promoted` + spawn within ~5m.
- Marker is prose-matched; if Pan's judgment prompt drifts from "code change suggested", promotion misses — consider teaching pan.go's prompt to emit the exact marker (separate bead).
- Cap counting now relies on description text of unlabeled beads; a code-change proposal closed then re-filed inside the 1h signature window still dedupes (unchanged behavior).
