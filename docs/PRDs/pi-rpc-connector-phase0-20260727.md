# PRD: Pi RPC Connector — Phase 0 (Correctness + Provable Liveness)

## 1. Overview

**Problem:** Hydra's Pi RPC connector is instantiated but routed around for all real work because it "returned not-Success in the daemon context". Root cause, verified at HEAD: the connector handles only four event types (`agent_start`, `agent_end`, `error`, `message_update`) and has **zero** handling of `extension_ui_request` — so when an installed extension (e.g. `@aliou/pi-guardrails`, present in the worker package set) issues a blocking `confirm`/`select`/`input` ask, nobody ever replies, Pi waits forever, the connector's silence timeout fires, and the run is reported as a failure. Consequently Hydra runs Pi in piped text mode, where **no** extension initialises at all (verified across three controlled runs: no rtk rewriting, no Hindsight, zero `ctx_*` calls in ~4,100 worker logs).
**Solution:** Make the connector protocol-correct (answer blocking UI asks deny-by-default, ignore notifications, count every event as liveness), add a session-stats query that exposes real per-session token counts, and expose a self-test entrypoint that proves the connector works in-process — all with **no change to any live execution path**.
**Success Metrics:**
- `hydra --rpc-selftest "<prompt>"` completes a turn through the connector and prints real token counts.
- A blocking UI ask no longer hangs the session: it is answered and logged.
- No behaviour change for workers or the adjudicator (both remain on text mode).

---

## 0. Scope Profile
- **System Type:** library (internal Go packages) + cli-tool (self-test entrypoint)
- **Interfaces:** internal Go API (`agents.PiConnector`), Pi's JSONL rpc protocol over stdin/stdout, one daemon CLI flag
- **Surfaces Explicitly Excluded:** worker execution path, gap adjudicator, `abort`/`follow_up`/`cycle_model` adoption, making rpc the default
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: The rpc connector is provably correct before anything depends on it — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Protocol Correctness and Session Stats
- FR-002: RPC Mode Configuration
- FR-003: Self-Test Entrypoint

**Acceptance Criteria:**
- `go build ./...` exits 0 and `go test ./internal/agents/... ./internal/core/...` exits 0
- `UIResponseFor` returns a `cancelled` reply for a `select` ask and no reply for a `setStatus` notification (asserted in one test that exercises both)
- With `agents.pi.mode` unset, `RPCEnabled()` returns `false` — the default remains text mode
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Protocol Correctness and Session Stats
**Type:** fr
**Priority:** P1

**Target Files:** `internal/agents/pi.go`, `internal/agents/pi_rpc_test.go`

Three same-file concerns, kept in one FR because splitting them would collide on `internal/agents/pi.go`:

1. **Answer blocking UI asks.** Add `func UIRequestNeedsReply(method string) bool` returning true only for `select`, `confirm`, `input`, `editor`; and `func UIResponseFor(id, method string, autoConfirm bool) ([]byte, bool)` returning the JSON reply plus `false` when the method is a notification (`notify`, `setStatus`, `setWidget`, `setTitle`, `set_editor_text`) that must NOT be answered. Reply shape per Pi's protocol: `{"type":"extension_ui_response","id":"<id>","confirmed":<bool>}` for `confirm`, and `{"type":"extension_ui_response","id":"<id>","cancelled":true}` for `select`/`input`/`editor`. In the connector's event loop add `case "extension_ui_request":` which, for blocking methods, writes the reply to Pi's stdin and logs `[PiConnector] answered blocking UI ask` plus the method — a headless session has no human to ask, so the ask itself is the anomaly and must be visible. Notifications are logged at debug level only and never answered. Deny-by-default (`confirmed: false`, `cancelled: true`) is **fail-closed**: auto-approving an unreviewed action is the worse failure.
2. **Every event is liveness.** The loop currently refreshes `lastProgressActivity` only inside `case "message_update"` (`pi.go:~535`). Refresh it for **every** decoded event, so a session that is emitting tool-execution and extension events — but not text deltas — is not killed as silent. The existing `activityTimeout`/`progressTimeout`/`maxDuration` limits stay as backstops with their current values.
3. **Session stats.** Add `func (c *PiConnector) SessionStats(ctx context.Context, session *AgentSession) (SessionStats, error)` plus `type SessionStats struct { Input, Output, CacheRead, Total int }` (JSON tags `input`, `output`, `cacheRead`, `total`), which sends `{"type":"get_session_stats"}` and parses the `data.tokens` object of the matching `{"type":"response","command":"get_session_stats"}` line. This is the first real per-session token measurement in Hydra — `tools/hydra-eval/metrics.go` currently has **no** token fields at all.

**Acceptance Criteria:**
- `UIRequestNeedsReply("confirm")` returns `true`; `UIRequestNeedsReply("setStatus")` returns `false`
- `UIResponseFor("abc", "confirm", false)` returns `ok == true` and JSON that unmarshals to a map with `type` equal to `"extension_ui_response"`, `id` equal to `"abc"`, and `confirmed` equal to `false`
- `UIResponseFor("abc", "confirm", true)` returns JSON whose `confirmed` is `true`
- `UIResponseFor("abc", "select", false)` returns JSON whose `cancelled` is `true` and which has no `confirmed` key
- `UIResponseFor("abc", "setStatus", false)` returns `ok == false` (notifications are never answered)
- Parsing the line `{"type":"response","command":"get_session_stats","data":{"tokens":{"input":17474,"output":27,"cacheRead":256,"total":17757}},"success":true}` yields `SessionStats{Input:17474, Output:27, CacheRead:256, Total:17757}`
- Parsing a `get_session_stats` response with no `tokens` object returns a zero-valued `SessionStats` and a non-nil error
- `go build ./...` exits 0
- `go vet ./internal/agents/...` exits 0
- `go test ./internal/agents/...` exits 0

**Depends On:** —

---

### FR-002: RPC Mode Configuration
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/config.go`, `internal/core/config_rpc_test.go`

Add to the existing `PiConfig` struct (the `agents.pi` block): `Mode string \`yaml:"mode"\`` and `RPCAutoConfirm bool \`yaml:"rpc_auto_confirm"\``. Add methods `func (p PiConfig) RPCEnabled() bool` returning true only when `strings.ToLower(strings.TrimSpace(p.Mode)) == "rpc"`, and `func (p PiConfig) AutoConfirm() bool` returning `p.RPCAutoConfirm`. Absent or unrecognised `mode` values mean text mode — **the default must remain text**, because text mode is what currently ships successfully and rpc is unproven in the daemon.

**Acceptance Criteria:**
- `PiConfig{}.RPCEnabled()` returns `false` (unset default is text mode)
- `PiConfig{Mode:"rpc"}.RPCEnabled()` returns `true`; `PiConfig{Mode:"RPC"}.RPCEnabled()` returns `true`; `PiConfig{Mode:" rpc "}.RPCEnabled()` returns `true`
- `PiConfig{Mode:"text"}.RPCEnabled()` returns `false`; `PiConfig{Mode:"banana"}.RPCEnabled()` returns `false`
- `PiConfig{RPCAutoConfirm:true}.AutoConfirm()` returns `true`; the zero value returns `false`
- A yaml config containing `agents:\n  pi:\n    mode: rpc\n    rpc_auto_confirm: true` parses with `Mode == "rpc"` and `RPCAutoConfirm == true`
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:** —

---

### FR-003: Self-Test Entrypoint
**Type:** fr
**Priority:** P1

**Target Files:** `cmd/hydra/main.go`

**Reads From:**
- FR-001: Protocol Correctness and Session Stats
- FR-002: RPC Mode Configuration

Add a `--rpc-selftest <prompt>` string flag to the daemon binary (`cmd/hydra/main.go`, alongside the existing `-config`/`-debug` flags). When non-empty, the process does NOT start the daemon: it builds a `PiConnector` from the loaded config exactly as `overseer.go:~341-348` does, starts a session, executes the prompt, queries `SessionStats`, prints a one-line summary in the exact form `rpc-selftest: success=<bool> tokens=<total> duration=<duration>` followed by the assistant output, and exits 0 on `result.Success` or 1 otherwise. This proves the connector works **in-process, in the daemon's own configuration**, without altering a single execution path — the missing evidence that made the connector untrustworthy.

**Acceptance Criteria:**
- `go build ./cmd/hydra` exits 0
- Running the built binary with `--rpc-selftest ""` (empty) starts the daemon as before — the flag is inert when unset (verified by the flag defaulting to the empty string and the daemon path being unchanged)
- Running with `--rpc-selftest "Reply with exactly: ok"` against a reachable provider prints a line matching `/^rpc-selftest: success=(true|false) tokens=\d+ duration=/` and exits 0 when `success=true`
- The self-test path never calls `core.NewOverseer(...).Start(...)` (asserted by inspection of the added branch returning before daemon startup)
- `go vet ./cmd/hydra/...` exits 0

**Depends On:**
- FR-001: Protocol Correctness and Session Stats
- FR-002: RPC Mode Configuration

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Root `go build ./...` exits 0
- `go test ./internal/agents/... ./internal/core/...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `internal/agents/pi.go`, `internal/core/config.go` and `cmd/hydra/main.go` all compile as part of their packages

**Verifies:**
- FR-001: Protocol Correctness and Session Stats
- FR-002: RPC Mode Configuration
- FR-003: Self-Test Entrypoint

---

## 5. Technical Considerations

### Architecture
- **Correctness before adoption.** This PRD makes the connector right and provable; it deliberately does not migrate workers or the adjudicator. Those are separate, measurable decisions gated on this evidence.
- **Fail-closed on UI asks, fail-open on the feature.** An unanswered ask is a hang, so we always answer — but we answer *deny/cancel*, because a headless agent asking for approval must not be auto-approved. Meanwhile the whole rpc path stays behind a flag defaulting to text, so a protocol regression cannot reach production.
- **Liveness is semantic, not textual.** Refreshing the activity timer on every event fixes a real misdiagnosis class: a worker executing a long tool emits execution and extension events but no text deltas, and the current loop treats that as silence.

### Technology
- Go standard library only. UI-reply construction, notification classification and stats parsing are pure functions tested without spawning Pi; the self-test exercises the real subprocess.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/agents/pi.go`, `pi_rpc_test.go` | fr | — | — |
| FR-002 | `internal/core/config.go`, `config_rpc_test.go` | fr | — | — |
| FR-003 | `cmd/hydra/main.go` | fr | Reads From FR-001, FR-002 | — |
| US-001 | — | us-integration | FR-001..003 | — |
| NFR-SYS | — | nfr-verification | FR-001..003 | — |

### Edge Cases
- Unknown UI method (a future Pi version adds one): treated as a notification — never answered, logged — so an unrecognised ask cannot be silently mis-answered. It may hang; the timeout backstop covers it, and the log names the method.
- `get_session_stats` unsupported by the running Pi: returns an error; the self-test still reports the turn result and prints `tokens=0`.
- Multiple `extension_ui_request` events with the same id: each is answered independently; Pi de-duplicates by id.
- Provider unreachable: the self-test exits 1 with the model error surfaced (Pi carries model failures inside the message as `stopReason: "error"`, which the connector already inspects at `pi.go:~516-529`).

### Constraints
- Default behaviour must be byte-identical to today when `agents.pi.mode` is unset.
- No changes to `cmd/worker` or `internal/core/gap_adjudicator.go` in this PRD.
- Never auto-approve a `confirm` unless `rpc_auto_confirm` is explicitly set.

---

## 6. MoSCoW

**Must Have:** UI-ask handling, every-event liveness, `RPCEnabled()` defaulting to text, self-test entrypoint.
**Should Have:** `SessionStats` token parsing; `rpc_auto_confirm`.
**Could Have:** Recording self-test token counts into `hydra-eval` metrics.
**Won't Have:** Worker or adjudicator migration; `abort`/`follow_up`/`cycle_model` adoption; making rpc the default; `fork`/`clone`.

---

## 7. Research Insights

### Codebase Findings
- The connector's event loop handles exactly four cases — `agent_start`, `agent_end`, `error`, `message_update` (verified: `grep -oE 'case "[a-z_]+"' internal/agents/pi.go`) — and `extension_ui_request` appears **zero** times in the file. That is the unhandled blocking path.
- `@aliou/pi-guardrails` is present in the worker package set (`/opt/dev-tools/config/pi-worker/settings.json`), a plausible source of `confirm` asks in the daemon context.
- `agent_end` handling already inspects `messages[].stopReason == "error"|"aborted"` and copies `errorMessage` (`pi.go:~516-529`), so model-level failure detection is NOT the defect — an earlier hypothesis of mine that the code disproved.
- `lastProgressActivity` is initialised at `pi.go:426` and refreshed only under `case "message_update"`; the three limits (`activityTimeout` 15m, `progressTimeout`, `maxDuration`) are defined at `pi.go:~429-432`.
- `NewPiConnector` is already instantiated at `internal/core/overseer.go:348` from a config literal built at `:341-347` (Provider, Model, ThinkingLevel, Timeout, ApiKeyName, ConfigDir) — the self-test reuses that exact construction.
- `AgentResult` (`internal/agents/connector.go:71-78`) carries `Success`, `ExitCode`, `Output`, `Error`, `Duration`, `Metadata`.
- Protocol facts verified live against Pi 0.82.1 (`tools/pi-rpc`, PR #74): completion is `agent_end`; `extension_ui_response` accepts `value`/`confirmed`/`cancelled`; `get_session_stats` returns `data.tokens{input,output,cacheRead,total}` (observed `input:17474, output:27, cacheRead:256, total:17757`); rtk rewriting is live in rpc mode (`RTK rewrite: git status --short -> rtk git status --short`).

### Historical Patterns
- The abandonment is recorded in `internal/core/gap_adjudicator.go`: the adjudicator invokes Pi "in text mode … rather than the RPC connector, which returned not-Success in the daemon context and forced every gap to real."
- `tools/hydra-eval/metrics.go` has zero token/cost fields; the hydra-eval PRD listed token metrics as *Won't Have* pending "the future event ledger". `get_session_stats` is that ledger.

---
*Adversarial review: 1 round, 4 lenses, approved 2026-07-27. Findings folded in: the test adversary noted that an implementation answering ALL `extension_ui_request` events would pass a naive "no hang" AC while corrupting the stream, so an explicit `UIResponseFor("setStatus") → ok == false` AC was added; the implementability lens forced the unknown-method policy to be specified (treat as notification, log, rely on the timeout) rather than inferred; the scope adversary confined all three same-file concerns to FR-001 and fenced worker/adjudicator migration plus abort/follow_up/cycle_model into `Won't Have`; the grounding auditor verified every cited line at HEAD and corrected the author's earlier assumption that `stopReason` handling was missing — the code already has it.*
