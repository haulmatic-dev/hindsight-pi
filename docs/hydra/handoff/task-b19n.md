# discovery handoff

## What changed (files + one line each)
- internal/core/pan_health.go: health digest counts non-terminal beads labelled `integration-exhausted` per project; emits single "shared integration wedge" anomaly at ≥3 open parks (`sharedWedgeThreshold = 3`).
- internal/pan/health.go: `ProjectHealth.IntegrationExhausted` field + `intexh=N` in digest text.
- internal/core/pan_health_test.go: TestCollectHealthDigestIntegrationExhaustedWedge (count, terminal-exclusion, threshold, anomaly, text).
- docs/discovery/integration-wedge-srv-telematics-b19n.md: full triage report.
- docs/architecture/shepherd.md: shared-wedge anomaly documented (Pan judgment section).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/ ./internal/pan/` — PASS.
- `go test ./internal/core/ -run TestCollectHealthDigestIntegrationExhaustedWedge -count=1` — PASS.
- `go test ./internal/core/ -run TestCollectHealthDigest -count=1` — PASS.
- `go test ./internal/pan/ -count=1` — PASS.
- Live store `/home/ajantha/projects/srv-telematics/.beads`: poisoned WAL index ("database is busy (recovery in progress)") cleared with `br doctor migrate-schema recover` ×2 (first pass re-poisoned by contending writer); all previously-failing `br show` calls now succeed — VERIFIED.

## Findings (for human triage)
- Root cause of the 9-bead integration-exhausted cluster: br WAL-index poison on srv-telematics store, not integration capacity. Underlying work-beads (mymr, 51r7, jo3u, qali, atr9, h2ll, 6iue, ukwe, pmgh) are CLOSED (quarantine/supersession); the meta-beads were accumulating parks.
- 10+ duplicate pan-filed meta-beads (oive, awtj, srch, m369, 5l11, 36ed, f10l, burr, otae, 0po4, nygw) — close duplicates as `not_planned`, keep one canonical wedge tracker. Do NOT blanket clear-hard-park.

## Open edges for the next phase
- br WAL poison recurs under write contention (3rd recorded incident) — upstream fix belongs in br.
- Tier B agentic conflict resolution still unimplemented (deliberate).
