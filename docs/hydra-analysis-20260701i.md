# Hydra Analysis — 2026-07-01

## 1. Ultimate goals of Hydra

Hydra is intended to be a closed-loop, multi-project agent orchestration system for software engineering. Its stated goals are:

1. **PRD-driven delivery.** Read a Product Requirements Document (PRD), decompose it into beads (tasks), spawn the right agents, and drive each bead to completion.
2. **Closed-loop compliance.** After a run finishes, automatically run verifier tools (language drift detectors, export/route checkers, stub detectors), convert any gaps into a new mini-PRD, and iterate until zero gaps or max cycles.
3. **Language-agnostic migration.** Support Java → Go, Python → Go, TypeScript → Go, etc. by comparing the source project against the target Go project and surfacing behavioural drift.
4. **Multi-project, multi-user scheduling.** Run continuously as a system service, discover ready beads across projects, schedule workers within resource limits, and survive reboots.
5. **Autonomous yet observable.** Track every bead, every worker run, every retry, and every gap in a durable database (Beads) so operators can inspect state and history.
6. **No premature closure.** Ensure that beads are only closed when the actual work is done, not when the agent merely claims it is done.

---

## 2. Findings from the srv-file-handler Java → Go migration

### 2.1 What was achieved

| Area | Result |
|------|--------|
| PRD generation | `create-PRD` and `gap-to-prd` were improved to emit concrete acceptance criteria with source evidence and Go evidence. |
| Task breakdown | `task-breakdown` runner was fixed to auto-approve its own approval gate instead of blocking on "Expected approval gate to be ready". |
| Configuration | `hydra.yaml` was corrected to invoke the actual `cli.mjs` file instead of the stale `pi` binary. |
| Permissions | `.beads/` directory ACLs were fixed so the `hydra` user can read/write the daemon JSONL. |
| Worker execution | After MiniMax credits were replenished, workers began succeeding instead of failing with 429. |
| Gap reduction | `verify-source-vs-go` went from 3 gaps to 2 warning-level gaps. `retry-attempts` was resolved. |
| Code generated | A partial Go port was produced: handlers, services, Temporal worker, extraction package, tests, and documentation. |
| Bead count | 885 of 886 beads are closed. |

### 2.2 Critical failures

| Failure | Evidence | Impact |
|---------|----------|--------|
| **Agent self-report** | Workers closed gap beads as `completed` while the Go files still contained the original gap. The verifier re-detected the same gap on the next cycle. | Compliance cycled without making real progress. |
| **Rate-limit storm** | 2,683 of 2,941 worker executions (91%) failed with `429 Token Plan usage limit reached`. | Workers could not execute; the system looked idle while burning API quota. |
| **Fix-bead explosion** | 316 deeply nested `Fix: Fix: Fix: ...` beads existed before the max-depth guard. | Bead database grew ~5× from 121 to 886. Most fix beads were noise. |
| **Approval gate blockage** | `task-breakdown` required the gap-PRD approval gate to be ready before the verify step. | Direct PRD runs failed; the compliance cycle stalled. |
| **Untracked artifact noise** | ~50 untracked files (test binaries, coverage files, `drift_test/`, `docs/discovery/`, duplicate packages). | The repo became hard to review; generated artifacts were not cleaned up. |
| **Config rot** | `task_breakdown_path: pi` pointed to a dead binary. | `node pi` failed, producing opaque `exit status 1` errors. |
| **Bead daemon lock** | `br` CLI returned `exit status 8` (daemon lock) during concurrent closes. | Fix-bead creation failed, which cascaded into more fix-bead creation. |

### 2.3 Root causes

1. **Trust the agent's self-report.** The system closes a bead when the worker exits with `success=true` or when the agent reports completion. There is no mandatory re-verification of the actual file changes before closure.
2. **No distinction between infrastructure and code failures.** Any non-zero exit spawned a fix bead, so rate limits and daemon locks generated hundreds of meaningless beads.
3. **Weak acceptance criteria in gap-PRDs.** Early gap-PRDs said "Verify X is intentional in Go," which agents interpreted as "do nothing."
4. **Single-model dependency.** All workers used `MiniMax-M3`. A single rate limit or outage stopped all execution.
5. **No cleanup contract.** Workers are allowed to write arbitrary files into the repo; there is no enforced cleanup or commit policy.
6. **Daemon concurrency assumptions.** The system assumes the beads daemon is always available; it does not gracefully serialize or back off on transient locks.

### 2.4 Current state (as of 2026-07-01)

- Hydra service is running and stable.
- `srv-file-handler` has 886 beads, 885 closed, 1 open approval gate.
- `verify-source-vs-go` reports **2 warning-level gaps**:
  - `task-queue-default` — Java uses `document-ingestion-queue`, Go uses `document-extraction-queue`.
  - `error-message-quoted` — error message string mismatch between Java and Go.
- The compliance cycle is waiting for the approval gate to close before it can run again.
- Many generated Go files and tests are still untracked in the working tree.

---

## 3. Potential fixes

### 3.1 Immediate (already implemented or partially implemented)

- [x] **Concrete gap-PRD acceptance criteria.** Replace vague "verify" instructions with source evidence, Go evidence, and explicit "change the file or document the divergence" instructions.
- [x] **Auto-approve gap-PRD approval gates.** Move the approval step inside the `task-breakdown` verify phase so the runner does not block on itself.
- [x] **Max-depth guard for fix beads.** Cap fix-bead recursion at 3 levels.
- [x] **Infrastructure-failure guard.** Worker exit codes classify failures; rate limits/timeouts do not create fix beads and leave the bead open for retry.
- [x] **Hourly retry for exhausted infra beads.** After max retries, infra-related beads retry every hour instead of giving up.
- [x] **ACL fix for `.beads/` directory.** Ensure the `hydra` user can read/write the daemon JSONL.
- [x] **Config path fix.** Use the full `cli.mjs` path in `hydra.yaml`.

### 3.2 Short-term (next 1–2 weeks)

- [ ] **Verifier-after-close gate.** Before any bead can be closed as `completed`, re-run the relevant verifier (e.g., `verify-source-vs-go`) and confirm the gap is gone. If the gap persists, the close must be rejected and the bead reopened or turned into a fix bead.
- [ ] **Diff-based completion check.** For code beads, require that the target file(s) changed on disk before the worker can close the bead. Empty diffs should fail the close.
- [ ] **Model fallback / circuit breaker.** If MiniMax returns 429, pause worker scheduling for N minutes or fall back to another provider, instead of spawning doomed workers.
- [ ] **Artifact cleanup policy.** Workers should write to a temporary workspace; only explicitly named deliverables should be copied into the repo. Test binaries and coverage files should be in `.gitignore` or auto-removed.
- [ ] **Daemon lock serialization.** Retry `br` commands with exponential backoff on `exit status 8` instead of treating the lock as a fatal failure.
- [ ] **Approval-gate automation.** Either close all gap-PRD approval gates automatically or remove them for machine-generated gap PRDs.

### 3.3 Medium-term (next 1–2 months)

- [ ] **Structured verification state.** Store the last verifier output in the bead metadata. The compliance cycle should compare the previous and current gap reports to prove progress, not just run a fixed number of cycles.
- [ ] **Per-bead verification contracts.** Each bead should declare which verifier command(s) must pass before it can close. The close path enforces this.
- [ ] **Worker commit discipline.** After a GREEN bead closes, the worker should commit its changes with a descriptive message. The operator reviews commits, not untracked files.
- [ ] **Retry budget visibility.** Surface infra-exhausted beads in the UI/log with a clear "will retry at HH:MM" message.
- [ ] **Multi-model worker pool.** Allow different worker types to use different models/providers based on cost/availability.

### 3.4 Long-term architecture

- [ ] **Two-phase close: claim → verify → close.** A worker may claim completion, but the bead is only closed after an independent verification phase confirms the claim. This removes the self-report problem entirely.
- [ ] **Deterministic planning.** Instead of letting the agent interpret the PRD, generate a deterministic execution plan (which files to edit, which tests to add) and validate the agent's output against it.
- [ ] **Sandboxed execution.** Run workers in isolated filesystem sandboxes so failed or noisy workers cannot pollute the repo.
- [ ] **Human-in-the-loop escalation.** Auto-approve low-risk beads but escalate high-risk or ambiguous ones (e.g., cross-project API changes) to a human.

---

## 4. Verdict

Hydra is a **promising research prototype** but **not a production-ready autonomous delivery system** today. It can assist with structured PRDs, task decomposition, and partial code generation, but it requires significant human oversight, guardrails, and cleanup.

The srv-file-handler migration demonstrates that Hydra can get close to the finish line, but the last 2 warning gaps and the open approval gate show that the system cannot yet reliably close itself out. The dominant risk is **agent self-report**: the system trusts the agent's claim of completion more than the actual file system state.

The highest-value next fix is **mandatory verification before close**. Until that exists, Hydra will continue to cycle on gaps that the agent claims to have fixed but did not.
