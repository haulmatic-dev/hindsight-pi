# Hydra Execution Plan — 2026-07-02

**Goal:** Hydra builds complete, shippable, end-to-end-tested products from a PRD with minimal human intervention — cheap models do the bite-sized implementation work inside deterministic gates, high-IQ models handle PRD/architecture/adjudication/review, and a working memory loop makes agents measurably better every day.

This doc = **verified current state** (code + live VPS `<prod-vps>`, inspected 2026-07-01/02) + **8 work orders** written so a kimi-k2.7-coding-class model can execute them: exact files, numbered steps, testable acceptance criteria.

## Verified current state

**What works:**
- Go daemon (`cmd/hydra/main.go`) discovers beads across projects (`br` CLI + SQLite), spawns pi-agent workers, drives a PRD→beads→verify→gap-PRD compliance loop (`compliance_cycle.go`, MaxCycles=5). Runs as systemd service, survives reboots.
- TDD phase chains (RED/GREEN/REFACTOR via bead labels), DAG dependencies, infra-failure classification, fix-bead max-depth guard, worktree/rollback machinery in the task-breakdown skill.
- **Daemon-side Hindsight wiring runs on every worker**: config from `/etc/hydra/hydra.yaml:59-61` (`enabled: true, base_url: http://127.0.0.1:8888`), worker queries Hindsight for context before work and retains execution memory after (visible throughout worker stderr).
- `hindsight-api` live on `:8888` + Postgres + local embeddings (`BAAI/bge-small-en-v1.5`) + reranker. `.beads` cross-user ACLs in place (`drwxrwsr-x+`, `group:hydra rwx, group:pi rwx`, setgid).
- Prod models: 5 models / 2 providers (`kimi-k2.7-code` default, kimi-k2.6, MiniMax-M3/M2.7/M2.7-Highspeed).
- Overseer God-object refactor ~90% done (`REFACTOR-TRACKING.md`); remaining: ChainOrchestrator, ResourceGovernor, ValidationEngine.

**What's broken (each maps to a work order):**

| # | Finding | Evidence |
|---|---|---|
| 1 | **Hindsight retrieval returns nothing despite a full DB.** Postgres: `memory_units=4209`, `memory_links=148993`, `entities=4644`, `banks=5` — yet every worker recall logs `No relevant memories found`. Retain is async (`async_operations=5667`; empty `memory_id` in logs is normal). Retention lands; **retrieval is severed** — suspect bank mismatch or query mismatch. Nothing compounds until fixed. | worker stderr in `/var/log/hydra/hydra-error.log`; `pg_stat_user_tables` |
| 2 | **Beads close on exit code alone.** `worker_lifecycle.go:88` `success := exitCode == 0` → `bead_closer.go:114` closes unconditionally. TDD validation is label-gated and **non-blocking** — failure logs and falls through (`bead_closer.go:85-87`). No diff check. Prod `agent_verification_enabled: false`. Agents "verified intentional" without editing files and closed GREEN (commit `502a059` documents this happening). | `internal/core/bead_closer.go`, `/etc/hydra/hydra.yaml:97` |
| 3 | **False-positive gaps burn full cycles.** Compliance loop iterates a fixed count with no progress check (`hasGaps` reads only the current report's `passed` bool; per-cycle reports never diffed). Detector false positives (cross-layer token matches) spawn full PRD→beads→TDD chains that conclude "no change needed" — then get re-detected next cycle because no verdict is remembered. | `compliance_cycle.go:114,221`; worker discovery docs for FR-001/FR-003 |
| 4 | **The prompt-based CASS+tldr protocol is dead.** `APPEND_SYSTEM.md` mandates `cm context` / `hydra-llm-tldr search` etc., but `cm`/`cass`/`hydra-cass-memory`/`hydra-llm-tldr` don't resolve for the daemon user, and worker logs show **zero** invocations ever. The real code-intel CLI on the box is `llm-tldr`/`tldr` (AST tool: search/impact/calls/arch) — unused. Three memory systems on paper (CASS, Hindsight, context-mode); one wired. | `command -v` as user `hydra`; 235MB log grep |
| 5 | **Runaway retry loop.** `[Retry] Could not fetch bead … not found in DAG` repeats 651×/bead, every tick, still growing while the daemon is idle → `hydra-error.log` = 235 MB, unrotated. | `retry_manager.go`; `/var/log/hydra/` |
| 6 | **Secrets + env hygiene.** A live LLM API key (`<redacted>`) is printed in `journalctl -u hydra` — systemd rejects the `export`-prefixed lines in `/etc/hydra/api-keys.env` sourcing chain (`/etc/profile.d/hydra-api-keys.sh`) and echoes each value while complaining (~25 "Ignoring invalid environment assignment" lines per start). | journal, unit file |
| 7 | **No model tiering, no product gate.** All beads use one implementation model; no high-IQ adjudication/review tier; nothing builds/boots/e2e-tests the assembled product before declaring a PRD complete. Dev/prod config drift (local pi `defaultModel: MiniMax-M3` vs prod `kimi-k2.7-code`). | `hydra.yaml` agents block; `settings.json` local vs prod |

## Model-tier contract

| Tier | Model class | Cost stance |
|---|---|---|
| PRD, architecture, gap adjudication, product review | High-IQ, max effort (Opus-class / kimi-k2.7 max thinking) | Don't optimize cost here |
| Implementation beads, mechanical fixes | Cheap coding models (kimi-k2.7-code, GLM-5.2, MiniMax) | Bulk tier; viable **only if** bead instructions are bite-sized + precise (file paths, exact ACs, evidence) |

---

## Work orders

> **Executor note:** file:line references are anchors from HEAD `dc88af9` (2026-06-26). Re-locate each anchor by searching for the quoted code/log strings before editing — do not trust raw line numbers.

### WO-1 — Fix Hindsight retrieval (P0 — the compounding-breaker)

**Why:** 4,209 memory units stored; every worker recall returns "No relevant memories found". Until retrieval works, agents relearn everything from zero on every bead.

**Files:** `cmd/worker/main.go` (Hindsight query/retain call sites — search `Querying Hindsight for context` and `Retaining execution memory`), `internal/hindsight/client.go`, `internal/services/hindsight_lifecycle.go`. Server: `hindsight-api` on `127.0.0.1:8888` (VPS), Postgres DB `hindsight`.

**Steps:**
1. Read the recall call in `cmd/worker/main.go`: extract the exact endpoint, bank name, and query payload. Do the same for retain.
2. Compare banks: `psql -d hindsight -c "SELECT * FROM banks;"` — list the 5 banks; verify retain-bank == recall-bank. Mismatch = likely root cause (a `HYDRA_BANK`-style env default may differ per code path).
3. Manual round-trip on the live API: retain a marker memory ("HYDRA-RETRIEVAL-TEST-2026") into the bank the worker recalls from, then recall with a related query. If the marker isn't returned, capture raw HTTP request/response of both calls.
4. Fix the mismatch found (bank name, query params, score threshold, async-indexing delay). If retain is async, confirm indexing completes — check `async_operations` for stuck rows.
5. Add integration test `internal/hindsight/roundtrip_test.go`: retain → poll until indexed → recall returns the retained unit. Skip when `HINDSIGHT_BASE_URL` unset.

**Accept:** (a) roundtrip test green against a live server; (b) on the VPS, a worker-context query for a topic present in the DB logs non-empty context; (c) root cause documented in the PR description.

### WO-2 — VerifiedClose gate (P0)

**Why:** Beads close on `exitCode==0` alone; validation failures fall through and close anyway; empty-diff "fixes" pass.

**Files:** `internal/core/bead_closer.go` (close path ~lines 75–120; fallthrough at ~85–87), `internal/core/validation_service.go`, `internal/beads/client.go`.

**Prerequisite audit (30 min, read-only):** confirm whether concurrent workers get isolated checkouts (worktree orchestrator) or share one working tree. If shared, two beads editing the same files race — which corrupts this WO's diff check. Until isolation lands, make the scheduler serialize beads whose `Files:` overlap (DAG-level constraint).

**Steps:**
1. Add `func (c *BeadCloser) verifiedClose(bead Bead, workDir string) error`, called before any `CloseBead(...completed...)`:
   a. **Diff check:** `git -C <workDir> status --porcelain` + `git diff HEAD --stat` scoped to the bead's target files (bead metadata `Files:`; fall back to whole repo). Empty ⇒ reject **unless** bead metadata carries `adjudication: false-positive` (set by WO-3).
   b. **Blocking validation:** replace the log-and-fallthrough at `bead_closer.go:85-87` — on TDD-validation failure return an error instead of proceeding.
   c. **Gap beads:** if the bead carries the gap-fix label/title-prefix emitted by gap-to-prd, run the project verifier (`verify-source-vs-go … --json`) and require `passed: true` for that gap ID.
2. On rejection: reopen the bead with `reopen_reason`, and retain an antipattern to Hindsight via the existing client ("closed-without-diff", "validation-failed-close-attempt") — failures become guardrails future agents recall.
3. **Max-rejection guard (anti-wedge):** track `close_rejections` in `HYDRA_METADATA`; after 3 rejections, stop retrying and escalate to human via the existing notification path (strong gates + incapable agent must not loop forever). Note: the diff scoping already blocks trivial-diff gaming for gap/TDD beads — the semantic backstop is the verifier/tests, not the diff itself.
4. **Commit on verified close:** after `verifiedClose` passes, commit the bead's changes (`git add <plan/Files paths> && git commit -m "bead(<id>): <title>"`) and warn on any untracked files the worker left outside its scope — operators review commits, not stray files. Before the worker starts, snapshot `git status --porcelain`; at close, only stage paths the bead touched — never sweep pre-existing dirt into the bead's commit.
5. **Structured close events:** emit one JSON log line per close attempt `{bead_id, verdict, reason, attempt, model, tokens?}` (tokens when the pi run reports usage) — makes the one-shot-rate dashboard and tier-cost tuning a query, not a project.
6. Unit tests in `internal/core/bead_closer_test.go`: empty-diff reject; empty-diff + adjudicated pass; validation-fail reject; gap-bead verifier-fail reject; 3rd-rejection escalation; happy path + commit created.

**Accept:** all tests green; a bead with no file changes and no adjudication verdict cannot reach `completed`; a bead rejected 3× lands in the human queue, not a loop.

**Deploy coupling:** ship WO-2 and WO-3 together, or feature-flag the empty-diff rejection until WO-3 lands — otherwise legitimate false-positive gap beads (no diff possible, no verdict mechanism yet) wedge against the gate.

### WO-3 — Adjudicate before decompose (P0)

**Why:** Detector false positives currently spawn full PRD→beads→TDD chains that conclude "no change needed", then reappear next cycle. Settle each gap once with one high-IQ call **before** decomposition.

**Files:** `internal/core/compliance_cycle.go` (between gap detection ~line 130 and `runGapToPRD` ~line 144), new `internal/core/gap_adjudicator.go`.

**Steps:**
1. New step after gap detection: for each gap, invoke the pi CLI once with a **high-IQ model override** (config key `compliance.adjudicator_model`) and a two-sided prompt: "Independently argue this gap is REAL, then argue it is FALSE-POSITIVE; verdict = {real|false_positive|ambiguous} + confidence + file:line evidence on both source and Go sides (same layer, same symbol required)". Parse structured JSON output.
2. Route: `real` → gap-to-PRD as today; `false_positive` → write verdict to `<project>/.hydra/gap-adjudications.json` (fingerprint = detector ID + normalized evidence) and exclude; `ambiguous` → exclude + notify human. **Verify the notification path actually reaches a person** (chat/email/push — whatever hydra's notifier targets), not only a log line; adjudicators are confidently wrong sometimes, and the human escape hatch is the backstop.
3. Suppression: before adjudicating, skip gaps whose fingerprint already holds a `false_positive` verdict. Retain each verdict to Hindsight too.
4. Progress delta: if the surviving-gap set is identical to the previous cycle's, halt the cycle and notify instead of burning remaining cycles.
5. Tests: adjudicator behind an interface + stub → false-positive gap produces zero beads; suppression honored across cycles; identical-gap-set halts.

**Accept:** compliance on srv-file-handler adjudicates the 2 known warning gaps once, creates beads only for `real` verdicts, and a second run creates none.

### WO-4 — One memory: retire the dead CASS shell protocol (P1)

**Why:** Prompts mandate memory CLIs that don't resolve and were never invoked; memory query/save already happens daemon-side (Go). Prose-mandated protocols are unenforceable — delete them.

**Files:** `/opt/dev-tools/config/pi/APPEND_SYSTEM.md`, `config/pi/skills/code-search-and-intelligence/SKILL.md`, `config/pi/skills/code-discovery/SKILL.md`, hydra repo `pi-config/prompts/*.md`.

**Steps:**
1. Delete all `cm …` / `hydra-cass-memory …` instructions from the files above.
2. tldr: keep as an *optional* code-search aid; correct every reference to the resolvable name `llm-tldr` (verify with `command -v llm-tldr` as user `hydra`); drop "MANDATORY" framing.
3. Keep the `pattern-quality-reviewer` skill as the quality gate wording for what the Go side retains (distilled insights only, no raw transcripts).
4. Grep-verify: zero remaining references to `cm `, `hydra-cass-memory`, `hydra-llm-tldr` in pi config or hydra prompts.

**Accept:** step-4 grep returns zero hits; a sample worker prompt contains injected Hindsight context and no dead-command instructions.

### WO-5 — Kill the runaway retry loop (P0)

**Why:** 235 MB error log from one repeated line; retry manager re-processes beads that no longer exist in the DAG, every tick, forever.

**Files:** `internal/core/retry_manager.go` (the loop emitting `[Retry] Could not fetch bead … not found in DAG`).

**Steps:**
1. On "bead not in DAG": **remove** the bead from the retry-tracking map (deleted/completed elsewhere) instead of leaving it for the next tick.
2. Log once per bead at removal, not per tick.
3. Unit test: bead in retry map + absent from DAG ⇒ removed after one tick, single log line.

**Accept:** test green; after deploy, `hydra-error.log` stops growing when no beads are active.

### WO-6 — Ops hygiene (P0 — human/ops runbook, not a model task)

1. **Rotate** the LLM API key currently readable in `journalctl -u hydra` (`HINDSIGHT_API_LLM_API_KEY=<redacted>`); revoke the old one.
2. Give systemd a proper env file: `EnvironmentFile` syntax is `KEY=value` — no `export`, no `${…}` interpolation. Keep `/etc/profile.d/hydra-api-keys.sh` for interactive shells only. Removes the ~25 "Ignoring invalid environment assignment" lines and the value-echo leak.
3. Add `/etc/logrotate.d/hydra`: `/var/log/hydra/*.log` weekly, 4 rotations, compress, copytruncate. Truncate the current 235 MB file after WO-5 deploys.
4. Clear the stuck approval-gate bead blocking srv-file-handler's cycle (`br list --json | grep -i approval` on the project; close `srv-file-handler-37oo` if still open) and confirm no other gate beads are blocking.

**Accept:** new key live, old revoked; `systemctl restart hydra` yields zero "Ignoring invalid environment" lines; `logrotate -d /etc/logrotate.d/hydra` passes; no open approval gates blocking any project.

### WO-7 — Explicit model tiering + provider failover (P1)

**Why:** The tier contract must be config, not convention. Today all beads run one implementation model; no adjudication/review tier exists; local and prod pi configs drift. And a proven failure mode has no defense: a historical rate-limit storm produced 6,017 `429` errors and a 91% worker-failure wave — with a second provider already configured and idle.

**Files:** `/etc/hydra/hydra.yaml` (agent blocks ~lines 125–140), `internal/core/worker_spawner.go` (model selection → pi `--model` flag), config structs, `/opt/dev-tools/config/pi/models.json`.

**Steps:**
1. Per-role model config: `agents.implementation.model` (kimi-k2.7-code / GLM-5.2 / MiniMax), `agents.adjudicator.model` and `agents.reviewer.model` (high-IQ, max thinking). Route by bead label/type: `gap-adjudication`, `product-review` → high tier; everything else → implementation tier.
2. Ensure all referenced models exist in `models.json`; add missing high-tier entries. Sync the drifted local `config/pi` (`defaultModel: MiniMax-M3` → match prod `kimi-k2.7-code`).
3. Bead-instruction precision (what makes cheap models viable): every bead must carry `Files:`, exact acceptance criteria, and evidence links — verify `gap-to-prd`/`task-breakdown` templates emit these; tighten any template that can produce a bead without `Files:`.
4. **Provider failover:** each tier gets an ordered model list (`model: [primary, fallback]`), providers differing between the two. When a worker fails with the rate-limit classification (exit code 2 from `classifyFailure`), the retry respawns on the fallback; ≥3 rate-limit failures on a provider within 10 min → circuit-open that provider for 15 min (all tiers route to fallback), log the flip. No doomed-worker storms. Thresholds are config values, initial guesses — revisit after the first real rate-limit wave.

**Accept:** spawner unit test proves label→model routing and rate-limit→fallback respawn; circuit-breaker test (3 failures → provider skipped, recovers after window); a sampled implementation bead prompt contains Files + ACs; adjudicator runs show the high-tier model in pi run history.

### WO-8 — Product-level e2e ship-gate (P1)

**Why:** "All beads green" ≠ shippable product. Nothing currently builds, boots, or exercises the assembled result before a PRD is declared complete.

**Files:** `internal/core/compliance_cycle.go` (after zero-gaps, before declaring complete), new `internal/core/ship_gate.go`, per-project config block.

**Steps:**
1. Per-project config: `ship_gate: {build_cmd, boot_cmd, health_url, e2e_cmd, timeout}` (srv-file-handler example: `go build ./...`; start worker+api binaries; hit health endpoint; run the e2e test tag derived from PRD acceptance criteria).
2. Gate: compliance reports "complete" only after all four pass; any failure emits a `ship-gate` gap into the normal gap flow (adjudicated by WO-3 like any other gap).
3. Tests with a stub project config (commands = `true`/`false` binaries): pass, build-fail, boot-timeout, e2e-fail.

**Accept:** stub tests green; srv-file-handler `ship_gate` config exists and runs end-to-end on the VPS.

---

## Sequencing

**WO-6 (ops, today) → WO-5 → WO-1 → WO-2 + WO-3 (deploy together — see WO-2 coupling note) → WO-7 → WO-8 → WO-4.**
Stop the bleeding (6, 5) → make memory real (1, likely a small config-level fix — cheap unlock that improves every later run) → make closes honest and settle gaps once (2+3, coupled) → tier the models (7) → gate the product (8) → clean the prompts (4, cosmetic, last).

**Parked (P2, revisit after the above):** labeled eval corpus for detector precision/recall; one-shot-rate metric dashboard (% beads closed first attempt — the number that proves compounding; WO-2's close events supply the data); full per-worker worktree isolation (the audit + interim same-`Files:` serialization is a WO-2 prerequisite; this is the complete fix); false-negative product sweep (high-IQ recall pass over the finished product).

---

# Phase 2 — intelligence upgrades

**Entry condition:** WO-1…WO-8 deployed, one real PRD run end-to-end, one-shot-rate baseline measured. Phase 1 fixes the plumbing; Phase 2 attacks the four residual walls: no oracle for "done", cheap-model one-shot rate, memory quality, silent false negatives.

### WO-9 — Executable acceptance criteria: the PRD becomes the oracle (P1)

**Why:** Today "complete" is defined implicitly by detectors + a ship-gate config. For greenfield work there is no oracle at all. Fix: at PRD time, a high-IQ pass compiles every acceptance criterion into a **failing e2e test** — before any implementation exists. Product-level TDD: "complete" is defined on day zero, and WO-8's gate gets a principled suite to run.

**Files:** `config/pi/skills/create-prd/SKILL.md` (AC format contract), new pi skill `config/pi/skills/compile-acceptance-tests/`, `internal/core/compliance_cycle.go` (PRD-ingest hook, before task-breakdown), WO-8's `ship_gate` config (`e2e_cmd` points at the compiled suite).

**Steps:**
1. Tighten the `create-prd` AC contract: every AC must be given/when/then with observable outcome (endpoint + expected status/body, CLI + expected output, state change + how to read it). Reject/flag ACs that aren't machine-checkable ("user-friendly", "robust") — those go to a separate `non_functional` section reviewed by the human.
2. New `compile-acceptance-tests` skill (high-IQ tier via WO-7 routing): input = PRD; output = one e2e test file per FR under `<project>/e2e/prd/`, tagged (Go: `//go:build prd_e2e`), each test failing against an empty project and containing the AC text as a comment. Tests must exercise the product from outside (HTTP/CLI/process), never internal packages.
3. Hook in `compliance_cycle.go` (or the PRD-ingest path): after PRD accepted, before task-breakdown, spawn one bead `compile-acceptance-tests` (high tier). Its VerifiedClose criterion: test files exist, compile, and **fail** (red) against current HEAD.
4. Wire `ship_gate.e2e_cmd` to run the tagged suite (`go test -tags prd_e2e ./e2e/prd/...`). Completion = the suite that started all-red is now all-green. Opt-out decision rule: **default enabled, always.** Disabled only by an explicit `compile_acs: false` in the PRD frontmatter, set by the human during `create-prd` (machine-generated gap-PRDs can never set it). Never for greenfield — greenfield has no other oracle; migrations at least have the source to diff against.
5. Stub-project test: 2-AC toy PRD → skill emits 2 failing tests → implementing the toy makes them pass → ship-gate flips.

**Accept:** toy-PRD round trip green; on a real PRD, `e2e/prd/` exists before the first implementation bead spawns, all tests red at start; ship-gate runs that exact suite.

### WO-10 — Plan-then-execute: high-IQ plans, cheap hands (P1)

**Why:** Cheap-model one-shot rate is capped by how much *deciding* the bead asks of it. Split: a high-IQ planner emits a per-bead micro-plan (exact files, signatures, test names, edge cases); the cheap model only implements it; the close gate checks actual-vs-planned. Precision instructions taken to the limit.

**Files:** `config/pi/skills/task-breakdown/` (planner stage — `analyzer.mjs`/`dag-builder.mjs` emit plan metadata), `internal/core/worker_spawner.go` (inject plan into worker prompt), `internal/core/bead_closer.go` (extend WO-2's `verifiedClose`).

**Steps:**
1. Add a planning pass to task-breakdown: for each *implementation* bead, one high-IQ call (WO-7 `adjudicator`-tier model) produces a `plan` block in bead metadata: `{files: [...], new_functions: [name+signature], tests: [test names + what each asserts], steps: [...], out_of_scope: [...]}`. Skip for trivial beads (label `no-plan`).
2. `worker_spawner.go`: render the plan block at the top of the worker prompt as the contract ("implement exactly this; if the plan is wrong, stop and report instead of improvising").
3. Extend `verifiedClose` (WO-2): changed files must be ⊆ `plan.files` (+ their test files). Unplanned file touched ⇒ reject with reason `unplanned-change` (reopens with the diff attached so the planner can revise). Planned test names must exist and pass.
4. Escape hatch: worker can return `plan_defect: <reason>` instead of code; bead reopens to the planner tier, not the implementation tier. **Revision budget:** max 2 plan revisions per bead; the 3rd `plan_defect` escalates to human (same anti-wedge rule as WO-2's rejection guard).
5. Metrics: tag beads `planned`/`unplanned`; compare one-shot rates (this is the A/B that justifies the extra high-IQ call).
6. Tests: spawner injects plan; closer rejects out-of-plan diff; `plan_defect` routes to planner; no-plan label bypasses.

**Accept:** unit tests green; on a live run, a planned bead's prompt shows the plan block, an out-of-plan edit is rejected, and the planned/unplanned one-shot comparison is queryable.

### WO-11 — Outcome-graded memory: recall what actually helps (P1)

**Why:** WO-1 makes recall work; nothing makes it *good*. If retained content is noise, agents recall noise forever. Grade each memory by whether recalling it preceded success, prune what never helps — the bank becomes a self-curating playbook.

**Files:** `cmd/worker/main.go` (recall site — it knows which memory IDs were injected), `internal/core/bead_closer.go` (close outcome), `internal/hindsight/client.go`, new `internal/hindsight/grading.go`.

**Steps:**
1. Inspect `hindsight-api` for a feedback/weight endpoint (`docs/hindsight-api-notes.md` in the hindsight-pi repo; else the REST surface directly). If native feedback exists, use it; else sidecar table `memory_grades (memory_id, bead_id, outcome, ts)` in the existing Postgres.
2. Recall site: record injected memory IDs into bead metadata (`HYDRA_METADATA.recalled_memories`).
3. On close (WO-2 path): for each recalled ID write outcome — `+1` one-shot success, `0` success-with-retries, `-1` failure/rejected-close.
4. Scoring job (daemon tick or nightly): memory score = decayed mean of outcomes. Score < threshold after ≥N exposures → down-rank (native weight if supported, else exclude at recall time via grade filter). Top-scoring memories get injected first when recall over-returns. **Cold start:** memories with <N exposures rank neutral — grading shapes recall only once evidence accumulates; expect the first weeks ungraded.
5. Retention hygiene at the source: retain only distilled insight (the `pattern-quality-reviewer` contract), never raw transcripts — add a length + "contains file:line or rule" check before retain.
6. Tests: grade write on close; low-score exclusion at recall; decay math.

**Accept:** after a batch of runs, `memory_grades` populates; a deliberately-planted junk memory stops being recalled after N failures; recall ordering reflects scores.

### Phase-2 backlog (pick up after WO-9…11 prove out)

- **Structured memory schema** — `{type: pattern|antipattern|decision, scope, trigger-context, action, evidence}` instead of prose blobs; retrieval matches trigger-context. (Pairs with WO-11.)
- **Cross-project promotion** — pattern seen in ≥2 projects → global bank → seeded into new projects' first cycle. Fleet-level compounding.
- **Prompt/template evolution** — same failure pattern ≥3× → high-IQ optimizer rewrites the bead template/skill prompt → A/B by one-shot rate (branch `feature/prompt-optimizer` is the natural home).
- **Learned difficulty router** — predict bead difficulty from beads-DB history; easy → cheapest model, hard → higher tier or further decomposition.
- **Exploratory QA agent** — post-ship-gate, high-IQ agent drives the built product trying to break it; findings enter the normal gap flow. (False-negative catcher.)
- **Offline replay harness** — replay past beads against new prompts/models before deploying them; makes the two items above safe to iterate.
- **Human corrections as gold** — every human override/escalation retained as max-weight memory.

**Phase-2 order:** WO-9 → WO-10 → WO-11 (oracle → one-shot → compounding quality), backlog after — each item amplifies those three.
