# red handoff (m1h9)

## What changed (files + one line each)
- tools/beads-mirror-audit/main.go (new): RED-phase inert stub — types Divergence{BeadID, JSONLStatus, DBStatus}/Report{Divergences, Checked} (--json shape), exit-code constants exitClean=0/exitDrift=1/exitUnavail=2, auditUnavailablePrefix "audit unavailable: ", enforceEnv BEADS_MIRROR_AUDIT_ENFORCE, auditImplemented=false const, injectable brRunner seam, runAudit (never runs, returns 2), main() stub exiting 2.
- tools/beads-mirror-audit/main_test.go (new): 7 tests pinning all parent ACs — divergence line `<id> jsonl=<s> db=<s>` + exit 1 on fixture with one divergent record, clean mirror exit 0 + `mirror clean (N beads)`, --json machine report `{divergences,checked}` parsed, br missing → exit 2 + unavailable line, br failure (fake br exit 3 on list) → exit 2, read-only guarantee (jsonl byte-identical after drift run), always-on compile gate.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./tools/beads-mirror-audit/ -count=1` — PASS (ok, exit 0; 6 behavior tests skip loudly "RED: beads mirror audit contract not yet implemented" so suite stays green pre-GREEN, per parent AC7).
- `BEADS_MIRROR_AUDIT_ENFORCE=1 go test ./tools/beads-mirror-audit/ -count=1` — FAIL 6/6, each `RED: beads mirror audit contract not yet implemented` — proves RED detects the missing contract (failure reason = missing feature, not compile error).
- `gofmt -l tools/beads-mirror-audit/` — clean. `go vet ./tools/beads-mirror-audit/` — clean.
- Coverage (RED stub): `go test . -count=1 -cover` → 0.0% of statements (expected pre-GREEN; GREEN obligation to measure new code).

## Contract GREEN must implement (pinned)
- `runAudit(storeDir string, jsonOut bool) (Report, string, int)` — reads `<storeDir>/issues.jsonl`, queries live store via `brRunner(dir, args)` (br list --json in store dir), prints per-divergence `<id> jsonl=<s> db=<s>` (exit 1), `mirror clean (N beads)` (exit 0), `audit unavailable: <err>` (exit 2) on br absence/failure. `--json` emits Report JSON only. No store mutations (read-only pinned by TestAudit_Readonly).
- Flip `auditImplemented = true` when implementing.

## Open edges for the next phase
- Fakes br with `br list --json` returning `[{"id":...,"status":...}]`; if real br's JSON field names differ (e.g. status casing), adjust the double or accept both shapes — divergence line format is pinned, not the br fetch command.
- Pan health digest wiring of --json output is explicitly out of scope.
