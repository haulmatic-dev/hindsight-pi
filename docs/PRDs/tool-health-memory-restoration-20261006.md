# PRD: Tool-Health Monitoring & Memory-Plane Restoration

## 1. Overview

**Problem:** Hydra's intelligence tooling (hindsight, CASS, graphify) fails silently — the hindsight-pi extension has been dead since the server moved to 0.10.1 (removed `/profile` endpoints), graphify refreshes churn on moved project paths every 30m tick, and workers in chain worktrees can never resolve `graphify query` — while nothing detects, reports, or repairs any of it.

**Solution:** A tool-health probe suite with typed degradation/recovery events, failure-signature classification feeding Pan's existing fix-bead pipeline, closed-loop re-probe at fix-bead close, plus the two structural fixes (project-path guard, worktree graph symlink) that remove today's known-broken states.

**Success Metrics:**
- `tool_degraded` event fires within one sweep interval (10m) of a probeable outage — verified by induced-failure test
- Zero `[Graphify] update failed ... chdir` log lines after path guard lands
- `graphify query` exits 0 inside a chain worktree (today: "Graph missing → rg fallback" on every worker)
- A tool-repair bead close re-runs its probe; a still-broken tool rejects the close
- Time-to-detect and time-to-recover per tool derivable from the event stream (`tool_degraded` → `tool_recovered` delta)

---

## 2. User Stories

### US-001: Operator visibility into tool health
**As a** hydra operator
**I want** typed events and auto-filed fix beads when fleet-critical tooling degrades
**So that** the hindsight-pi outage class (dead for days, unnoticed) becomes impossible

**Type:** us-heavy
**Priority:** P1

**Target Files:** `internal/core/tool_health.go`

**Acceptance Criteria:**
- A probe failure emits a `tool_degraded` event with `tool`, `probe`, `detail` fields
- A recovered probe emits `tool_recovered` with the same `tool` field
- A semantic-class failure files exactly one fix bead per failure signature (Pan fix-cause dedup)

**Aggregates:**
- FR-001: Project-path existence guard
- FR-002: Worktree graphify-out symlink
- FR-003: Tool-health probe framework
- FR-004: Evidence-of-use probes
- FR-005: Signature classification + closed-loop repair

### US-INT-01: End-to-end degradation drill — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Project-path existence guard
- FR-002: Worktree graphify-out symlink
- FR-003: Tool-health probe framework
- FR-004: Evidence-of-use probes
- FR-005: Signature classification + closed-loop repair

**Acceptance Criteria:**
- `go test ./internal/core/ -run 'TestToolHealth' -count=1` exits 0
- Inducing a probe failure in the test harness (stubbed probe returning error) produces a `tool_degraded` event and exactly one fix bead; restoring it produces `tool_recovered`
- No mocking of the event sink or the bead-filing dedup store in the integration test

---

## 3. Functional Requirements

### FR-001: Project-path existence guard for graphify refresh
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/graphify_indexer.go:Refresh()`, `internal/core/multi_user_discovery.go`

The graphify indexer currently attempts refresh on every enumerated project path every interval and logs `update failed ... chdir: no such file or directory` forever when a project moves (srv-jobfile, fms-frontend — 2026-10-06). Add an existence guard: `os.Stat` the path before claiming/building; on missing, emit a `project_path_missing` event (deduplicated: at most once per path per 24h via an in-memory map with timestamp) and skip. Missing paths must not consume the `max_concurrent` semaphore or enter the claim set.

**Acceptance Criteria:**
- `Refresh("/nonexistent/path")` returns `false` and does not spawn a build goroutine
- First miss for a path emits exactly one `project_path_missing` event with `path` field; a second miss within 24h emits none
- A path that reappears (Stat succeeds on a later tick) refreshes normally
- `go test ./internal/core/ -run 'TestGraphifyPathGuard' -count=1` exits 0
- `go build -buildvcs=false ./...` exits 0

**Depends On:** —

---

### FR-002: Worktree graphify-out symlink
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/worktree_manager.go`

Workers run in chain worktrees, but `graphify-out/` is gitignored and never copied — every worker's mandatory `graphify query` falls back to `rg` (observed live: `Graph missing → rg fallback`, 2026-10-06). On worktree creation/reuse (`EnsureWorktree`), create a symlink `<worktree>/graphify-out` → `<mainCheckout>/graphify-out` when the target directory exists. The repo's `.gitignore` already contains `graphify-out/` (assert this; add via existing `ensureGitignoreLine` if absent) so the symlink is never committed. Failure to create the symlink is fail-open (log only) — graph access is an optimization.

**Acceptance Criteria:**
- After `EnsureWorktree` on a repo whose main checkout has `graphify-out/graph.json`, `os.Lstat(<worktree>/graphify-out)` reports a symlink and reading `<worktree>/graphify-out/graph.json` succeeds
- The worktree's `.gitignore` contains the literal line `graphify-out/`
- `git -C <worktree> status --porcelain` shows no `graphify-out` entry after symlink creation
- When the main checkout lacks `graphify-out`, no symlink is created and no error is returned
- `go test ./internal/core/ -run 'TestWorktreeGraphifySymlink' -count=1` exits 0
- `go build -buildvcs=false ./...` exits 0

**Depends On:** —

---

### FR-003: Tool-health probe framework + contract probes
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/tool_health.go`, `internal/core/config.go`, `internal/core/service_starter.go`

New file `tool_health.go`: a `ToolHealthMonitor` running a probe set on an interval from a goroutine started in `service_starter.go` (mirroring the credential-recovery sweep). Config under `shepherd.tool_health`: `enabled` (default true), `interval` (default 10m), `deep_check_interval` (default 1h), `auto_remediate` (default empty list). All probes fail-open with hard timeouts ≤10s and never gate the bead pipeline.

Contract probes (every `interval`):
- `hindsight`: GET `{hindsight.base_url}/health` → 200 with `"healthy"` in body; POST `.../v1/default/banks/hydra/memories/recall` with a fixed probe query → 200 and parseable JSON (asserts contract, not content)
- `cass`: run `cm context "tool-health probe" --json` → exit 0 and `success:true` in parsed output
- `graphify`: per project from the overseer's project list, `graphify-out/graph.json` exists and mtime < 2× `intelligence.graphify.refresh_interval`
- `llm-router`: GET the router health endpoint → 200

State machine per tool: healthy → degraded (emit `tool_degraded{tool, probe, detail}` once per degradation episode) → healthy (emit `tool_recovered{tool}`). Probe failure while already degraded emits nothing (no spam; the episode is one event pair).

**Acceptance Criteria:**
- With all probe targets stubbed healthy, a sweep emits zero events
- Stubbing the hindsight probe to fail emits exactly one `tool_degraded` event with `tool:"hindsight"` across three consecutive sweeps, and one `tool_recovered` when it passes again
- Each probe respects a 10s timeout (a hanging stub does not block the sweep goroutine beyond timeout)
- `shepherd.tool_health.enabled: false` disables the sweep goroutine entirely
- `go test ./internal/core/ -run 'TestToolHealthContract' -count=1` exits 0
- `go build -buildvcs=false ./...` exits 0

**Depends On:** —

---

### FR-004: Evidence-of-use probes (deep tier)
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/tool_health.go`, `cmd/worker/main.go`

Endpoint-alive is not feature-working (the `agent_verification_enabled: true` with zero verdicts class). Add hourly evidence probes that assert the fleet actually exercised each tool. Workers already `streamLog` over MCP; add a counters path: worker emits `intel_usage` stream events (`hindsight_memories:N`, `cass_bullets:N`, `graphify_fallback:true|false`) at execution start; the daemon-side monitor tallies per hour. Evidence probes (every `deep_check_interval`): hindsight memories injected > 0 when ≥1 worker ran; CASS bullets injected count present; graphify fallback rate reported. Zero-usage while workers ran emits `tool_degraded{tool, probe:"evidence"}`. Periods with zero workers are skipped (no false degradation).

**Acceptance Criteria:**
- Worker execution emits an `intel_usage` stream event containing hindsight, CASS, and graphify-fallback fields before the agent invocation
- A sweep with recorded worker activity but zero `hindsight_memories` emits `tool_degraded` with `probe:"evidence"`
- A sweep with zero worker activity emits no evidence-tier events
- `go test ./internal/core/ -run 'TestToolHealthEvidence' -count=1` exits 0
- `go build -buildvcs=false ./...` exits 0

**Depends On:**
- FR-003: Tool-health probe framework

---

### FR-005: Failure-signature classification + closed-loop repair
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/tool_health.go`, `internal/core/bead_closer.go`

Classify degradation by failure signature, not by tool: each probe maps its failure output to a signature class — `mechanical` (path missing, service down, stale artifact: deterministic-playbook candidates) or `semantic` (HTTP 410/404 contract drift, parse failures: agent-fix candidates). `semantic` failures file a fix bead through Pan's existing `file-bead` path (inheriting fix-cause dedup and recursion caps) with the probe diagnosis in the bead description (failing endpoint, expected vs actual, suspect files) and label `tool-repair`. `mechanical` failures only emit events while `shepherd.tool_health.auto_remediate` does not list their signature; listed signatures run the registered playbook (ships with zero registered playbooks — promotion requires two successful agent fixes of the same signature, counted from the event stream).

Closed-loop verification: `bead_closer.go` gains a check in `verifyBeforeClose` — for beads labeled `tool-repair`, re-run the originating probe; still failing → reject the close with reason `tool-still-degraded`; passing → allow and emit `tool_recovered` if not already emitted. The label→probe mapping is carried in the bead description as `tool_health_probe: <probe-name>`.

**Acceptance Criteria:**
- A semantic-class stub failure files one fix bead carrying label `tool-repair`, the diagnosis text, and a `tool_health_probe:` line; a repeat of the same signature files no second bead
- A mechanical-class failure with empty `auto_remediate` emits `tool_degraded` and files no bead
- Closing a `tool-repair`-labeled bead while its probe still fails rejects the close with reason `tool-still-degraded`; with the probe passing, the close proceeds
- `go test ./internal/core/ -run 'TestToolHealthRepair' -count=1` exits 0
- `go build -buildvcs=false ./...` exits 0

**Depends On:**
- FR-004: Evidence-of-use probes

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- `go build -buildvcs=false ./...` exits 0
- `go test ./internal/core/ -count=1` exits 0
- `go vet ./internal/core/` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- CHANGELOG.md contains an Unreleased entry mentioning `tool_degraded`

**Verifies:**
- FR-001: Project-path existence guard
- FR-002: Worktree graphify-out symlink
- FR-003: Tool-health probe framework
- FR-004: Evidence-of-use probes
- FR-005: Signature classification + closed-loop repair
- US-001: Operator visibility into tool health
- US-INT-01: End-to-end degradation drill

---

## 5. Technical Considerations

### Architecture
- Probe sweep lives in `service_starter.go` (mirrors the credential-recovery sweep pattern — proven this session), NOT inside Pan's health loop; bead filing calls Pan's existing `file-bead` action to inherit dedup/caps.
- Event stream is the observability layer: `tool_degraded`/`tool_recovered` pairs yield time-to-detect/time-to-recover per tool for free.
- Tier promotion is data-driven: a mechanical signature earns a playbook only after two successful agent fixes (counted from events). Ships with `auto_remediate: []`.

### Alternatives considered and rejected
- **Endpoint-only probing**: repeats the AgentVerifier sin (endpoint alive, feature dead) — evidence tier added.
- **Fixing graphify paths by config edit alone**: drift recurs on the next project move — structural guard instead.
- **Capability-adaptive hindsight-pi init (F1)**: out of scope for this PRD — different repo (`haulmatic-dev/hindsight-pi` checkout at `/opt/dev-tools/config/pi/git/...`), not a hydra project, push blocked on PAT. Tracked as out-of-band item; local edit deploys immediately when made.

### Technology
- Go 1.25, existing `internal/core` patterns (EventSink, Pan repair actions, BeadCloser gates)

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/graphify_indexer.go`, `internal/core/multi_user_discovery.go` | fr | — | — |
| FR-002 | `internal/core/worktree_manager.go` | fr | — | — |
| FR-003 | `internal/core/tool_health.go`, `internal/core/config.go`, `internal/core/service_starter.go` | fr | — | 1 of 3 |
| FR-004 | `internal/core/tool_health.go`, `cmd/worker/main.go` | fr | FR-003 | 2 of 3 |
| FR-005 | `internal/core/tool_health.go`, `internal/core/bead_closer.go` | fr | FR-004 | 3 of 3 |
| US-001 | `internal/core/tool_health.go` | us-heavy | FR-001..FR-005 | — |
| US-INT-01 | — | us-integration | FR-001..FR-005 | — |
| NFR-SYS | — | nfr-verification | All FRs + USs | — |

### Edge Cases
- Probe target absent by config (hindsight disabled) → probe skips, no degradation
- Zero-worker hour → evidence tier silent (no false positives)
- Daemon restart mid-degradation → state rebuilt from fresh probe sweep; a second `tool_degraded` for the same episode is acceptable once per restart (documented)

### Constraints
- All probes fail-open; none may gate the bead pipeline
- Probe timeouts ≤10s hard
- No auto-remediation playbooks ship in this PRD

---

## 6. MoSCoW

**Must Have:** FR-001..FR-005, event pairs, closed-loop re-probe, bead dedup
**Should Have:** llm-router probe, evidence tier
**Could Have:** playbook registry scaffolding (config field only)
**Won't Have:** F1 hindsight-pi extension capability-adaptive init (different repo, out-of-band); any registered auto-remediation playbook; llm-router lane rebalancing

---

## 7. Research Insights

### Codebase Findings
- Credential-recovery sweep (service_starter.go, 2026-10-06) is the proven placement pattern for periodic probes
- Pan `file-bead` action + `fix_cause.go` provide dedup and recursion caps already
- `bead_closer.go verifyBeforeClose` is the gate chain where tool-repair re-probe hooks in
- `graphify_indexer.go:160` writes `graphify-out/` inside the repo and gitignores it — worktrees therefore never have it
- Worker CASS injection is deterministic (`cmd/worker/main.go:1323-1350`); hindsight enrichment is `EnrichBeadContext` (main.go:2555+)

### Live Verification (2026-10-06)
- Hindsight 0.10.1: `/health` 200, `POST /v1/default/banks/hydra/memories/recall` 200 in 3.7s, `/profile` 410 Gone
- pi extension dies at init on the removed profile endpoint — zero recall/retain since upgrade
- Graphify: hydra graph fresh (30m interval); srv-jobfile/fms-frontend churn on moved paths every tick
- CASS 0.2.10 healthy
