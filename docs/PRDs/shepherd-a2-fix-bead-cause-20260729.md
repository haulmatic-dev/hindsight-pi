# PRD: Cause-Aware Fix Beads (Shepherd A2)

## 1. Overview

**Problem:** When a bead closes as failed, the auto-created fix bead's description says, verbatim: "Re-run the original task and ensure it closes as `completed`." The worker that picks it up receives no information about *why* the original failed — no failing test name, no error text, no exit classification. Redoing failed work with the same information fails the same way; the 2026-07-28 known-answer run confirmed the pattern, and separately produced **three duplicate fix beads for the same original** because creation is not deduplicated.

The failure evidence already exists on disk: every worker writes a per-bead log to `logs/workers/<bead-id>-<pid>.log` (see `openWorkerLogFile` in `internal/core/worker_spawner.go`). Nothing reads it back.

**Solution:** A small, deterministic failure-cause extractor that reads the failed bead's newest worker log and distils a compact cause block; wire it into fix-bead creation, and deduplicate so one original can never accumulate more than one open fix bead.

**Success Metrics:**
- A fix bead's description contains the original's close reason, exit code, and a cause block extracted from its worker log.
- Creating a fix bead for an original that already has one (open or in progress) is a no-op.
- No model calls: extraction is pure log parsing.

---

## 0. Scope Profile
- **System Type:** library (internal Go package, daemon-side)
- **Interfaces:** internal Go API only
- **Surfaces Explicitly Excluded:** the worker binary (`cmd/worker`), prompt templates, TDD validation's own fix-bead path (`validation_service.go`), the recursion cap itself (unchanged), any retry-policy change
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: A fix bead tells its worker what went wrong — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Failure Cause Extraction
- FR-002: Cause-Aware, Deduplicated Fix Bead Creation

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0
- Given a fake worker log containing a Go test failure, the created fix bead's description contains the failing test's name
- Calling fix-bead creation twice for the same original produces exactly one fix bead
- No mocking of internal dependencies in the integration test: it drives the real extractor against a real log file on disk and the real (mock-client-backed) creation path

---

## 3. Functional Requirements

### FR-001: Failure Cause Extraction
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/fix_cause.go`, `internal/core/fix_cause_test.go`

Add a deterministic extractor that turns a failed bead's worker log into a compact cause block.

1. Add `func workersLogDir() string` returning the same directory `openWorkerLogFile` in `internal/core/worker_spawner.go` writes to, by extracting the existing path construction into this shared helper so the two cannot drift. `openWorkerLogFile` must call it.
2. Add `func ExtractFailureCause(logDir, beadID string, maxBytes int) string`:
   - Find the newest file in `logDir` matching `<beadID>-*.log`. If none exists, return `""` — a missing log must never block fix-bead creation.
   - Scan the file and collect, in order of priority: lines containing `model call FAILED`; lines containing `--- FAIL:`; lines containing `FAIL` followed by a tab (package-level Go test failure); lines containing `Error:` or `error:`; the final 10 lines of the file as a fallback when none of the above matched.
   - Skip lines that are worker-passthrough JSONL (lines whose payload after the timestamp prefix starts with `{`) **except** when they contain `model call FAILED` — raw event noise must not fill the block, but the one JSONL line that names the model failure is the most valuable line in the file.
   - Truncate the assembled block to at most `maxBytes` bytes (callers pass 2048), keeping the **end** when truncating a single overlong line and the **highest-priority lines** when dropping whole lines.
3. The function is pure with respect to its inputs: no globals besides the filesystem read, no time dependence, no network, no model calls.

**Acceptance Criteria:**
- `ExtractFailureCause(dir, "missing-bead", 2048)` returns `""` and does not error or panic
- Given a log containing `--- FAIL: TestNotesSurviveRestart (0.00s)`, the returned block contains `TestNotesSurviveRestart`
- Given a log containing a `model call FAILED: 429 ...` line, the returned block contains `model call FAILED` and `429`
- Given a log whose only content is 50 ordinary lines, the returned block contains the last lines of the file and is non-empty
- Given `maxBytes` of 100 and a 5000-byte candidate block, the returned block is at most 100 bytes
- A line of worker-passthrough JSONL (`{"type":"message_update"...}`) with no `model call FAILED` marker does not appear in the block
- When two log files match the bead, the newer one (by modification time) is used

### FR-002: Cause-Aware, Deduplicated Fix Bead Creation
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/bead_closer.go`, `internal/core/bead_closer_fix_cause_test.go`

Wire the extractor into `createFixBeadForFailedClose` and stop duplicate creation.

1. **Dedup before create.** Before creating, list the project's beads via the existing `BeadsClient` and skip creation when any bead of type `fix` already exists whose status is not closed and whose `Dependencies` include the original bead's ID. Log the skip with both IDs. This closes the observed defect of three identical `Fix: REFACTOR: Optimize Delete Endpoint` beads: the current "already exists" check only inspects the create error string, and the beads client assigns fresh IDs, so it never fires.
2. **Cause in the description.** Build the description as: the existing two lines naming the original bead and close reason; then the exit code; then, when `ExtractFailureCause(workersLogDir(), <original bead id>, 2048)` returns non-empty, a section headed `### What failed last time` containing the cause block verbatim inside a fenced code block; then the existing instruction line, reworded to: "Address the failure above, then re-run the original task and ensure it closes as `completed`."
3. A missing or empty cause block degrades to today's description plus the exit code — never an error, never a skipped fix bead.
4. The recursion cap, its label, and the nil-bead fallback path are unchanged in behaviour; the fallback path (bead lookup failed) skips dedup (it has no dependencies to key on) and skips the cause section only if extraction returns empty — it still attempts extraction using the worker bead ID it does have.

**Acceptance Criteria:**
- Creating a fix bead for an original whose worker log contains `--- FAIL: TestX` yields a description containing both `What failed last time` and `TestX`
- Creating a fix bead twice for the same original results in exactly one fix bead in the store, and the second call returns nil error
- An original with an existing but **closed** fix bead gets a new fix bead (only open/in-progress fix beads block creation)
- With no worker log on disk, the fix bead is still created and its description contains the exit code
- Existing tests in `internal/core/bead_closer_fix_recursion_test.go` still pass unmodified
- `go vet ./internal/core/...` exits 0

---

## 4. Non-Functional Requirements

### NFR-001: The fix-bead path stays deterministic and offline
**Type:** nfr-verification
**Priority:** P2

**Target Files:** `internal/core/fix_cause.go`

Cause extraction is log parsing, not intelligence: it must not call a model, the network, or any subprocess.

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... -run 'FixCause|CreateFixBead' -count=1` exits 0
- `internal/core/fix_cause.go` contains neither `exec.Command` nor `http.` nor any import of `net/http`

**Verifies:**
- FR-001: Failure Cause Extraction

---

## 5. Out of Scope
- Changing what the *worker* does with the cause (prompt templates are untouched; the description reaches the worker through the existing bead-description path)
- TDD validation's separate fix-bead creation in `validation_service.go` (same idea, separate change once this one is measured)
- Any retry-count or recursion-cap change
- Feeding causes into CASS/Hindsight (Shepherd D3)

---

## 6. Adversarial Review

**Reviewed by:** Claude Opus 5 (independent pass after drafting)

- **"The worker log may contain the model's own text mentioning FAIL, polluting the block."** True and accounted for: passthrough JSONL lines are excluded except the `model call FAILED` marker line, and the block is capped at 2 KB with priority ordering, so pollution is bounded and the highest-signal lines win.
- **"Dedup by dependencies assumes fix beads depend on their original."** They do — `createFixBeadForFailedClose` already sets `Dependencies: [original]`; the dedup reads the same field it writes. The fallback path (nil bead) cannot dedup and is documented as such.
- **"Reading the log at close time races the streaming goroutine still writing it."** Acceptable by design: the extractor reads whatever is flushed at that moment, and a partially-written tail still names the failure in practice; the fallback (last 10 lines) tolerates truncation. No locking is added — a fix bead with a slightly short cause beats a close path that blocks on log writers.
- **"2 KB may be too small for a compile-error wall."** Deliberate: the description feeds a prompt; the worker needs the failure's *name and shape*, not the full wall. The cap keeps fix beads from becoming the new token sink.
