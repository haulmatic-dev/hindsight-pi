# refactor handoff

## What changed
- Gate-failure fix (test-only): `internal/core/verdict_memory_test.go` — `TestVerdictRetainer_RetainPOSTsOnce` expected a hard-coded `/v1/default/banks/hydra/memories` path; commit `1e74202f` ("replace hard-coded 'hydra' bank literals with BankID") centralized the bank to `hindsight.BankID` (`coding-brain`) but missed this test literal, so the suite gate failed with:
  `verdict_memory_test.go:182: Path = "/v1/default/banks/coding-brain/memories", want /v1/default/banks/hydra/memories`
  The test now asserts against `hindsight.BankID` so it tracks the constant.
- No product-code change; prior REFACTOR work (8vds: `gateFingerprintCounts` dedupe, dead-param removal in `pan_decision_packet.go`) unchanged.

## Verification
- Baseline (pre-edit): `go test ./internal/core/ -run 'TestDecisionPacket_|TestVerdictRetainer_' -count=1` → FAIL (only `TestVerdictRetainer_RetainPOSTsOnce`); reproduced locally with same error string.
- Post-edit: same command → `ok github.com/haulmatic-dev/hydra/internal/core 0.025s`.
- `go vet ./internal/core/` clean; `gofmt -l internal/core/verdict_memory_test.go` empty.
- Gate-log evidence: `/opt/dev-tools/var/hydra/gates/hydra/hydra-uoxk/test.log` lines 4153/4710 — sole `--- FAIL` in `internal/core` was `TestVerdictRetainer_RetainPOSTsOnce`.

## Open edges
- Full suite NOT run locally (daemon authority). The `src/x/a.spec.ts` FAIL line at log line 3135 is inside a test fixture string, not a real failure.
- `hindsight_search` not connected on this host (known); skipped per rule.
