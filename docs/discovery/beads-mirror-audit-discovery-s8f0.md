# FR-002 Discovery: Beads Mirror Audit Tool

## State (verified on this worktree)

`tools/beads-mirror-audit/` already exists as a RED-phase inert stub (commit-lineage on this branch):
- `main.go`: `Divergence{BeadID,JSONLStatus,DBStatus}`, `Report{Divergences,Checked}`, exit-code constants (0/1/2), `auditUnavailablePrefix = "audit unavailable: "`, `enforceEnv = "BEADS_MIRROR_AUDIT_ENFORCE"`, `auditImplemented = false`, injectable `brRunner` seam, inert `runAudit`, `main` exits 2.
- `main_test.go` (7.0 KB): pins the full contract (divergence line, exit 0/1/2, --json shape, fake br double + fixture jsonl).

This is the established RED-stub + ENFORCE-gate pattern (same as `bead_claims.go` / `postclose_sweep.go` chains). GREEN is the next phase's obligation.

## Contract GREEN must implement (pinned by main_test.go + AC)

- `runAudit(storeDir, jsonOut) (Report, string, int)` — reads `<storeDir>/issues.jsonl`, lists live beads via `brRunner(dir, args)`, compares per-bead status by ID.
- Divergence line format: `<bead-id> jsonl=<status> db=<status>`, one per divergence, in jsonl order.
- Clean: `mirror clean (N beads)` printed, exit 0.
- Drift: divergence lines + exit 1. **Never mutates either store** (read-only probe; `br list --json` only).
- `--json`: `{"divergences":[{"bead_id","jsonl","db"}...],"checked":N}`.
- `br` absent / nonzero exit / unparseable output → print `audit unavailable: <err>` (stdout), exit 2. Fail-open: never blocks a caller.
- `brRunner` invoked with cwd = storeDir so the live `br` binary resolves the store from cwd (matches `brListProbe` precedent in internal/core/spawn_preflight.go).

## Ambiguities resolved (design decisions)

1. **jsonl is the mirror; br is the truth.** Divergence rendered as `jsonl=<status> db=<status>` — db (br) is authoritative; jsonl is the possibly-stale file mirror.
2. **`br list --json`, not per-bead `br show`** — one listing call, O(N) not O(N×probe); WAL-poison (recurring "database is busy") affects any call, fewer calls = fewer fail-2 windows. `br show` fallback stays out of scope.
3. **Records present in br but absent from jsonl (and vice versa)** count as divergences (`jsonl=missing` / `db=missing` sentinels) — a truncated mirror is the failure mode this tool exists to catch.
4. **Decode failures on a single jsonl line**: skip silently? No — treat a malformed line as `jsonl=malformed` divergence (fail-closed on drift), design decision pinned in doc; GREEN pins exact behavior in tests.
5. **Exit 2 precedence**: if br fails AND jsonl missing → exit 2 (availability precedes auditability).
6. **No config/wiring**: standalone CLI under `tools/`, own go.mod sibling pattern like tools/prd-lint; Pan digest wiring explicitly out of scope (AC).

## AC mapping

| AC | Status |
|---|---|
| 1 compare + divergence line | contract pinned, GREEN pending |
| 2 exit 0/1, no mutation | contract pinned, GREEN pending |
| 3 `--json` | Report struct exists, GREEN pending |
| 4 exit-2 fail-open | contract pinned, GREEN pending |
| 5 unit test fake br | main_test.go already present |
| 6 `go test ./tools/beads-mirror-audit/ -count=1` | RED suite green pre-GREEN (stub path) |

## Open edge for GREEN

- Implement `runAudit` + flip `auditImplemented = true` (or remove the gate) + wire `brRunner` real implementation (`exec.Command("br", "list", "--json")`, `cmd.Dir = dir`).
- Keep stub's exported shapes byte-identical; tests already reference them.
