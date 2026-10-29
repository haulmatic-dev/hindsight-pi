# green handoff

Bead: hydra-xchh — GREEN: Implement Campaign-wide park via label sweep
Phase: green (parent phase: tdd)

## What changed (files + one line each)

- `internal/core/campaign_park.go` — real `sweepCampaignParkLabels`: parked sweep applies `park-gate` to non-terminal campaign members (idempotent skip of already-gated; `campaign_park_leak` `{bead, project}` event per in_progress member, deduped once-per-bead ledger), unpark removes the label from every non-terminal member; nil list seam fails open with (0, nil), list error surfaces as error; `wireCampaignParkSweep(o)` installs client.ListBeads/UpdateBeadLabels/RemoveBeadLabels + nil-guarded event emit; `campaignParkSweepBead(bead)` fail-open per-bead sweep via `o.beadAnalyzer.FindProjectPath`; `campaignParkSweepImplemented = true`.
- `internal/core/dag_navigator.go` — `GetReadyBeads` skips beads carrying `park-gate` (`campaignParkGateActive`, case-insensitive) before the escalated-label check.
- `internal/core/scheduler.go` — both `campaignParked` consult sites (retry-queue and DAG-node loops) now call the fail-open per-bead sweep so park becomes durable at bead level.
- `internal/core/overseer.go` — `wireCampaignParkSweep(o)` installed in NewOverseer after `wireStaleFilingProbes(o)`; the overseer-side `campaignParked` site wired with the sweep call.
- `CHANGELOG.md` — Unreleased/Added entry for the campaign-park sweep.

Parent ACs covered: hydra-gcip leak fixture honored (park-gate only on unrelated bead; members swept by Project only), cross-campaign dependents never parked (membership by `bead.Project == key` only), closing PARK-GATE removes labels and restores readiness, in_progress member emits exactly one deduped `campaign_park_leak` event.

## Verification (commands run + pass/fail)

- `go test ./internal/core/ -run 'TestCampaignPark_' -count=1 -v` → PASS: all 9 tests `--- PASS`, no skips; raw line `ok github.com/haulmatic-dev/hydra/internal/core 0.040s`, exit 0. (RTK wrapper's "Test Results:" banner miscounts on this suite — raw redirected output is authoritative.)
- `go build ./...` → exit 0 (`build_rc=0`).
- `go vet ./internal/core/` → exit 0 (`vet_rc=0`).
- Placeholder grep (`grep -nE 'TODO|FIXME|// Placeholder'` over all four impl files) → 0 hits.
- Coverage (`go test ... -coverprofile` + `go tool cover -func`), new implementation code:
  - `sweepCampaignParkLabels` 83.3%
  - `campaignParkNoteLeak` 100%
  - `resetCampaignParkLeakLedger` 100%
  - `campaignParkGateActive` 100%
  - `campaignParkMember` 100%
  - `wireCampaignParkSweep` 0%, `campaignParkSweepBead` 0% — production wiring paths not exercised by the 9 contract tests (test seams bypass them by design).

## Open edges for the next phase

- `wireCampaignParkSweep`/`campaignParkSweepBead` have no direct tests; wiring regressions would only surface at scheduler/overseer integration level.
- List error path is surfaced as an error but callers log-and-continue (fail-open by design); no retry/backoff on a failing store.
- Full `go test ./...` suite intentionally not run in this bead — daemon verify-loop owns it.
