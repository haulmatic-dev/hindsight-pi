# PRD: Fleet Loop Degradation — Context Starvation, Token-Blindness, and Close-Gate Burn

**Version:** 1.0 · **Date:** 2026-10-21 · **System Type:** library · **Repo:** components/hydra

## Problem

A 70-member chain (srv-data-harbour) wedged in a reject-respawn loop for 6+ hours
(2026-10-21, bead `ajantha-srv-data-harbour-8nz`). Root causes measured in
production logs, each compounding the next:

1. **Context starvation is self-inflicted.** TDDValidation creates a fix bead on
   every failed close (`13:10:41 [TDDValidation] Created fix bead ...bo1`); fix
   beads grow the chain past `maxPackChainMembers = 50` (`internal/core/worker_spawner.go:913`),
   so the ContextPack is skipped (`[ContextPack] chain has 70 members (> 50); skipping pack`),
   workers run unguided, close worse, and spawn more fix beads. The repair
   mechanism starves the very runs it repairs.
2. **Charon token-blindness for rpc workers.** PID 2353988 was flagged
   `zero-token stall ... tokens_so_far=0 for 26m` while fully productive
   (RTK extension notices flowing, completed `success=true` at 26m32s). False
   stall classification pushes Pan toward `kill-stalled-worker` on healthy runs.
3. **Partial closes burn gate strikes.** A worker whose own verdict says
   `HYDRA_VERDICT: {"done": false ...}` still closes the bead
   (`reason: completed`); CloseGate rejects (`verification-failed`), and three
   quick rejections escalate — observed 11:36 and 11:42, six minutes apart.
4. **Ownership preflight is noisy after healing.** `spawn_warn_store_ownership`
   still fired at 16:13 although `br show` as the daemon user succeeds; the
   warning cannot distinguish functional access from the residual uid-2008
   `.br_history/` files.

## Verified Facts (measured 2026-10-21)

- `internal/core/worker_spawner.go:913` declares `const maxPackChainMembers = 50` (verify: `rg -n 'maxPackChainMembers' internal/core/worker_spawner.go`).
- Pack writes failed on a mixed-ownership store: `pack_created write failed ... br update ... exit status 8` (verify: `grep 'pack_created write failed' /var/log/hydra/hydra-error.log | tail`).
- Fix-bead creation on failed validation grows chains (verify: `grep 'Created fix bead' /var/log/hydra/hydra-error.log | tail`).
- `cmd/worker/reporter.go` exposes `SetTokens`/`SetTokenBreakdown`; comment at `cmd/worker/main.go:3262` documents a prior tokens_so_far=0 bug of this class (verify: `rg -n 'TokensSoFar' cmd/worker/`).
- Worker close path logs at `cmd/worker/main.go:3040` `Closing bead from worker: %s (reason: %s)`; an auto-close guard exists at `:526` (verify: `rg -n 'Closing bead|Auto-close guard' cmd/worker/main.go`).
- CloseGate extracts the verification command from the bead description: `internal/core/bead_closer_hard_gates.go:69 extractVerificationCommand` (verify: `rg -n 'extractVerificationCommand' internal/core/bead_closer_hard_gates.go`).
- The br shim with `normalise()` is deployed and functional; `br show` as uid 999 from the project cwd works (verify: `grep -c normalise /opt/dev-tools/bin/br`).
- Residual mixed ownership exists in `.beads/.br_history/**` (uid 2008) — a host-side `chown` op, out of scope for hydra code (verify: `find <project>/.beads -uid 2008 | head`).

## Assumptions

- The 26-minute zero-token window on PID 2353988 is a reporting gap (rpc usage
  events not reaching `SetTokens`), not a model outage — RTK notices prove an
  active session. RED test must reproduce the gap before any fix.
- Pan consume-side changes for FR-006 are prompt-level only; no new repair verbs.

## Out of Scope

- Host ownership repair (`chown -R` on `.beads`) — ops task, recorded separately.
- Escalated-bead respawn gating — institutional memory (#296→#304) fixed this
  fail-open deliberately; a hard gate would re-wedge prod.
- The br shim ACL fix — already deployed and working.

---

### FR-001: ContextPack chain scan excludes terminal fix-beads; cap becomes config

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/worker_spawner.go`, `internal/core/config.go`, `internal/core/worker_spawner_test.go`
**Reads From:** `internal/core/context_pack.go`
**Test Command:** `go test ./internal/core/ -run 'ContextPack|PackChain' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- Chain member scan (`FindChainByBead` result fed to the pack decision) excludes beads with status `completed` or `closed` from the member count; a chain of 70 with 25 terminal beads counts as 45 and gets a pack.
- New config field `context_pack.max_chain_members` (int, default 50, yaml `context_pack.max_chain_members`) bounds the count; `0` keeps the default.
- A chain whose non-terminal count exceeds the bound still logs the skip line, now reporting both numbers: `[ContextPack] chain has %d members (%d non-terminal > %d); skipping pack for %s`.
- `go test ./internal/core/ -run 'ContextPack|PackChain' -count=1` exits 0.

### FR-002: Token-budgeted pack digests

**Type:** fr
**Priority:** P0
**System Type:** library
**Depends On:** FR-001
**Target Files:** `internal/core/context_pack.go`, `internal/core/config.go`, `internal/core/context_pack_test.go`
**Test Command:** `go test ./internal/core/ -run 'ContextPack' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- New config field `context_pack.max_pack_tokens` (int, default 40000, yaml `context_pack.max_pack_tokens`).
- Pack assembly trims member digests oldest-first until the pack's estimated token size (chars/4) fits the budget; the pack footer lists elided bead IDs as `elided: id1, id2, ...`.
- The current bead's own digest is never elided.
- Budget hit emits one typed event `pack_trimmed` with fields `{bead, elided_count, budget}`.
- A chain of 120 one-line digests under the default budget produces a pack with zero elisions (guard against over-eager trimming).
- `go test ./internal/core/ -run 'ContextPack' -count=1` exits 0.

### FR-003: rpc-mode token reporting closes the zero-token blind spot

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `cmd/worker/reporter.go`, `cmd/worker/main.go`, `internal/core/charon.go`
**Reads From:** `internal/core/worker_registry.go`
**Test Command:** `go test ./cmd/worker/ ./internal/core/ -run 'Token|ZeroToken|Stall' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- RED: a test that drives a fake rpc session emitting `turn_end` usage events (input>0 or output>0) through the reporter path asserts `TokensSoFar > 0` within one heartbeat interval — this test must fail on current main (reproduces PID 2353988 class).
- The rpc scanner goroutine (reporter.go:35 vicinity) forwards parsed usage totals to `SetTokenBreakdown` on every usage-bearing event, not only at exit.
- Charon's zero-token stall classifier (`internal/core/charon.go:456` vicinity) additionally requires **no rpc activity signal** (extension notices or turn events in the last 5m) before flagging; an active-but-zero-token worker logs at most one info line `[Charon] rpc-active but token-silent: <bead>` and is excluded from `kill-stalled-worker` surfacing.
- `go test ./cmd/worker/ ./internal/core/ -run 'Token|ZeroToken|Stall' -count=1` exits 0.

### FR-004: Bounded close-hold on self-reported `done:false`

**Type:** fr
**Priority:** P1
**System Type:** library
**Depends On:** FR-003
**Target Files:** `cmd/worker/main.go`, `cmd/worker/main_close_hold_test.go`
**Test Command:** `go test ./cmd/worker/ -run 'CloseHold' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- When the worker's own verdict JSON contains `"done": false`, the worker does NOT close the bead; it emits typed event `worker_close_held` with fields `{bead, hold_count, verdict_summary}` and continues or exits without closing.
- Hold count is persisted via the existing label mechanism (`close-hold-N`); at hold_count == 2 the worker closes anyway (fail-open path) and the event carries `forced: true`.
- Verdict JSON that fails to parse is ignored: close proceeds exactly as today (fail-open).
- No existing test asserting close-on-success changes behavior; `go test ./cmd/worker/ -run 'CloseHold' -count=1` exits 0 with the new tests and `go build ./...` exits 0.

### FR-005: Store-ownership preflight precision

**Type:** fr
**Priority:** P2
**System Type:** library
**Target Files:** `internal/core/bead_store_guard.go`, `internal/core/bead_store_guard_test.go`
**Test Command:** `go test ./internal/core/ -run 'StoreOwnership' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- The preflight warning names the actual offending paths (up to 3, e.g. `.beads/.br_history: uid 2008`) instead of the generic store-wide string.
- When a `br list --json --limit 1` probe in the project cwd exits 0, the preflight logs the ownership detail at debug level only (no `spawn_warn_store_ownership` event); a failing probe keeps today's warn + event.
- `go test ./internal/core/ -run 'StoreOwnership' -count=1` exits 0.

### FR-006: Same-fingerprint verification classifier

**Type:** fr
**Priority:** P2
**System Type:** library
**Depends On:** FR-004
**Target Files:** `internal/core/bead_closer_hard_gates.go`, `internal/pan/pan.go`
**Reads From:** `internal/core/service_starter.go`
**Test Command:** `go test ./internal/core/ ./internal/pan/ -run 'Fingerprint|VerifiedClose' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- On each CloseGate rejection, compute `sha256(bead_id + verification command + first stderr line)`; store via existing label `gate-fp-<first8>`.
- When the same fingerprint repeats on the same bead, emit typed event `verification_substantive` with `{bead, fingerprint, count}`.
- The Pan sweep prompt (internal/pan/pan.go) gains one sentence: beads carrying a `verification_substantive` event must NOT receive `repair:clear-escalation` — leave for human.
- No new repair verbs; no changes to the repair whitelist.
- `go test ./internal/core/ ./internal/pan/ -run 'Fingerprint|VerifiedClose' -count=1` exits 0.
