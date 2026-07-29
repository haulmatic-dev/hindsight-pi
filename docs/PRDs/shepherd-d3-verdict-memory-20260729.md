# PRD: Verdicts Feed Memory (Shepherd D3)

## 1. Overview

**Problem:** Hydra's most expensive judgments evaporate. The gap adjudicator (k3-256k, max thinking) decides `real | false_positive | ambiguous` with written reasoning for every gap — persisted only to `.hydra/gap-adjudications.json`, used only to suppress the same fingerprint within the same project. Failed closes now carry an extracted failure cause (Shepherd A2) — but it flows only into the fix bead's description. Neither verdict ever reaches Hindsight, so the next run, the next project, and the adjudicator itself all start from zero. This is the standing leak in the learning loop, and the direct cost is gap cycles: the same class of false-positive gap gets re-adjudicated from scratch, and the same class of real failure gets rediscovered by workers with no memory of it.

**Solution:** Retain both verdict kinds as structured Hindsight memories at the moment they are made, and inject prior verdicts back into the adjudication prompt. Fail-open everywhere: memory is an observer of the pipeline, never a gate on it.

**Success Metrics:**
- After one compliance cycle with gaps, Hindsight `Search` returns at least one memory whose content names the gap category and verdict.
- A failed close produces a retained memory containing the extracted failure cause.
- The adjudication prompt for a gap includes prior verdicts when Hindsight holds any for a matching query; with Hindsight disabled or down, prompts and pipeline behaviour are byte-identical to today.

---

## 0. Scope Profile
- **System Type:** library (internal Go, daemon-side)
- **Interfaces:** internal Go API; existing Hindsight HTTP client (`internal/hindsight`)
- **Surfaces Explicitly Excluded:** CASS playbook writes (no write API from this repo — workers consume CASS via the pi intelligence stack; revisit when a write path exists), the Hindsight service itself, worker binary, gap detection/analyzer, the adjudicator's decision logic and model
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: The system remembers its own judgments — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Verdict Memory Builder
- FR-002: Retain At The Two Verdict Sites
- FR-003: Prior Verdicts In The Adjudication Prompt

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... ./internal/hindsight/...` exits 0
- With a stub Hindsight server recording requests, one adjudicated gap and one failed close produce exactly two retain calls whose bodies contain the verdict/cause text
- With no Hindsight client configured, both sites are silent no-ops and the compliance cycle completes unchanged
- No mocking of internal dependencies: the integration test drives the real builder and the real HTTP client against an httptest server

---

## 3. Functional Requirements

### FR-001: Verdict Memory Builder
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/verdict_memory.go`, `internal/core/verdict_memory_test.go`

Add pure constructors that convert the two verdict kinds into `hindsight.ExecutionMemory` records, plus a nil-safe retainer.

1. `func buildAdjudicationMemory(projectName string, g gap, adj adjudication, fingerprint string) *hindsight.ExecutionMemory` — BeadID from `g.SuggestedBead` when set, otherwise the fingerprint; the memory content (via the struct's existing fields — use `BeadMetadata` for title/description and `ExecutionResult` for outcome text) must include, verbatim where available: gap category, severity, one-line description, the verdict slug, confidence, and the adjudicator's reasoning. Outcome is success=true for `false_positive` (the code was fine), success=false for `real`; `ambiguous` verdicts return nil — an unsure judgment is noise, not memory.
2. `func buildFailedCloseMemory(projectName string, bead *types.Bead, cause string, exitCode int) *hindsight.ExecutionMemory` — nil when `bead` is nil or `cause` is empty (a cause-less failure teaches nothing); content includes bead title, exit code, and the extracted cause block verbatim.
3. `type VerdictRetainer struct` with constructor `func NewVerdictRetainer(client *hindsight.Client) *VerdictRetainer` and method `Retain(mem *hindsight.ExecutionMemory)`. Nil receiver, nil client, disabled client (`IsEnabled() == false`), or nil memory: silent no-op. Retention runs synchronously with the client's own timeout; any error is logged at most and never returned — the pipeline must be unable to fail because memory hiccuped.

**Acceptance Criteria:**
- `buildAdjudicationMemory` with verdict `real` yields a memory whose serialized retain request contains the category, the reasoning text, and marks failure
- Verdict `false_positive` marks success; verdict `ambiguous` returns nil
- `buildFailedCloseMemory` with an empty cause returns nil; with a cause it embeds the cause verbatim and the exit code
- A nil `*VerdictRetainer` and a `VerdictRetainer` over a nil client both tolerate `Retain` without panicking
- Against an httptest stub, `Retain` POSTs to the retain endpoint once per non-nil memory; a 500 response is swallowed (no panic, no returned error)

### FR-002: Retain At The Two Verdict Sites
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/gap_adjudicator.go`, `internal/core/bead_closer.go`, `internal/core/overseer.go`, `internal/core/verdict_sites_test.go`

1. Construct one `VerdictRetainer` in the overseer next to the existing Hindsight client wiring and hand it to the compliance cycle manager and the bead closer (both keep a nil-safe field, matching the `events` field pattern in `bead_closer.go`).
2. In `gap_adjudicator.go`, immediately after a fresh adjudication is parsed and saved: build the memory via FR-001 and retain it. Already-cached verdicts (fingerprint present in `gap-adjudications.json`) are NOT re-retained — one memory per judgment, not per re-encounter.
3. In `bead_closer.go`, on the `reason == "failed"` path where the failure cause has been extracted (the A2 site): build and retain the failed-close memory. Reuse the already-extracted cause string — do not read the worker log twice.
4. Both sites emit an event `verdict_retained` (Fields: `kind` = `adjudication` | `failed_close`, plus the verdict slug or exit code) through the existing `EventSink`, only when a retain call was actually made.

**Acceptance Criteria:**
- Driving a compliance cycle with a stub adjudicator output and an httptest Hindsight server produces one retain call; running the same gap through a second cycle (cached verdict) produces zero additional retain calls
- A failed close with an extracted cause produces one retain call containing that cause
- With the retainer nil (Hindsight absent from config), both paths complete with zero retain calls and no panic
- `verdict_retained` events appear in the event stream once per actual retain
- `go vet ./internal/core/...` exits 0

### FR-003: Prior Verdicts In The Adjudication Prompt
**Type:** fr
**Priority:** P2

**Target Files:** `internal/core/gap_adjudicator.go`, `internal/core/gap_adjudicator_test.go`

Close the loop for the judge itself: before adjudicating a fresh gap, query Hindsight and show the adjudicator what was decided before.

1. Query via the existing `Search` API using the gap's category plus the first sentence of its description; take at most 3 results.
2. Append a clearly delimited section to the prompt built by `buildAdjudicationPrompt`: `## Prior verdicts on similar gaps` followed by one line per result. Content is advisory; the existing instructions and output contract are unchanged.
3. Any search error, timeout, or empty result set yields exactly today's prompt. The search must respect the client's existing timeout so a slow Hindsight cannot stall the compliance cycle beyond it.
4. When the retainer/client is nil, `buildAdjudicationPrompt` is not called differently and no search is attempted.

**Acceptance Criteria:**
- With a stub Hindsight returning 2 results, the adjudication prompt contains the `Prior verdicts` section with 2 lines
- With a stub returning an error or zero results, the prompt is byte-identical to today's
- With no Hindsight client, no HTTP request is made
- Prompt-section formatting is pinned by a golden assertion in the test

---

## 4. Non-Functional Requirements

### NFR-001: Memory observes, never gates
**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/verdict_memory.go`

A Hindsight outage must cost memories, not runs.

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... -run 'Verdict' -count=1` exits 0
- A test drives the failed-close path and one adjudication against a Hindsight stub that always returns 500, and asserts the close result and the adjudication verdict are identical to runs with no client at all

**Verifies:**
- FR-001: Verdict Memory Builder
- FR-002: Retain At The Two Verdict Sites

---

## 5. Out of Scope
- CASS playbook writes (no write API available from this repo today)
- Changing what the adjudicator decides, its model, or its output contract
- Retaining `ambiguous` verdicts or cause-less failures
- Cross-project verdict sharing policy (Hindsight's own scoping applies as-is)
- Worker-prompt recall of these memories (the existing recall path picks them up for free once retained)

---

## 6. Adversarial Review

**Reviewed by:** Claude Fable 5 (independent pass after drafting)

- **"Retaining every failed close will flood Hindsight with noise."** Only causes survive: cause-less failures return nil, `ambiguous` returns nil, cached verdicts never re-retain, and the fix-recursion cap (A2) already bounds failed closes per original to one fix attempt. Volume is bounded by judgments made, not retries attempted.
- **"FR-003 could bias the adjudicator into rubber-stamping its own past mistakes."** The section is advisory, capped at 3 lines, and the output contract is unchanged; a wrong past verdict is exactly the thing a better-informed later pass can overturn, and today it silently repeats instead. Net risk decreases.
- **"Synchronous retain adds latency to the close path."** Bounded by the client's existing timeout (10s default), on a path that just ran a multi-minute worker; and only when a cause was extracted. Acceptable; an async queue would add failure modes for negligible gain.
- **"Search query built from free-text descriptions may recall irrelevant memories."** Worst case is 3 irrelevant advisory lines in a 256k-context prompt — the failure mode is today's behaviour plus noise, never a blocked cycle.
