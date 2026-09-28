# green handoff

## What changed (files + one line each)
- cmd/worker/main.go: added `billing_error`, `requires more credits`, `insufficient credits` to quotaErrorMarkers so OpenRouter HTTP 402 billing_error classifies as quota (exit 6) instead of code failure.
- cmd/worker/quota_classification_test.go: live 402 payload fixture + two regression tests (isQuotaError true; classifyFailure == 6).
- CHANGELOG.md, docs/operations/handoff-20260731.md: operator-facing entries.

## Verification (commands run + pass/fail)
- `go test ./cmd/worker/ -run 'TestIsQuotaError|TestClassifyFailure' -count=1` — PASS (11 tests).
- `go test ./cmd/worker/ -count=1` — PASS (full package).
- `go build ./...` / `go vet ./cmd/worker/` — PASS.

## Open edges for the next phase
- exit-6 → HandleFailure → RecordQuotaExhausted → ShouldPause wiring pre-existed and was not re-tested here; daemon gate will cover.
