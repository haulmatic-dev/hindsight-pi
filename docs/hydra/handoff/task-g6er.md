# refactor handoff

## What changed (files + one line each)
- internal/core/campaign_park.go: dedupe only — the parked/unpark branches inside `sweepCampaignParkLabels` extracted into per-bead handlers `campaignParkGateBead` (idempotent attach + once-per-bead `campaign_park_leak` note, returns wrote-bool) and `campaignParkUngateBead` (strip, returns wrote-bool); the sweep loop now selects the handler (`apply := campaignParkGateBead; if !parked { apply = campaignParkUngateBead }`) and counts/returns errors identically. Behavior, label names, event payloads, counts, and error semantics byte-identical. No other files touched (dag_navigator.go / scheduler.go / overseer.go consult sites already single-line and clean).

## Verification (commands run + pass/fail)
- BEFORE: `go test ./internal/core/ -run 'TestCampaignPark_' -count=1 -v` — PASS (11 passed wrapper count; 9 test funcs, all `--- PASS`, ok 0.007s).
- AFTER: same command — PASS 9/9 (`--- PASS` ×9, `ok github.com/haulmatic-dev/hydra/internal/core 0.007s`).
- `gofmt -l internal/core/campaign_park.go` — clean.
- `go vet ./internal/core/` — clean (no output).
- `go build ./internal/core/` — BUILD_OK.
- No benchmarks exist for this functionality (prior sibling-chain handoffs confirm); no perf check required.

## Open edges for the next phase
- None. The unpark path always counts a write per non-terminal member even when the listing lacks the label — pinned by TestCampaignPark_CloseGateRemovesLabelsRestoresReadiness and intentionally unchanged.
