# PRD: Autonomous Resolution Layer (Containment → Resolution)

## 1. Overview

**Problem:** Hydra detects, classifies, parks, caps, and quarantines failures
reliably — but every exit from a contained state requires a human: 570
integrate failures from 2 beads, 86 quarantined branches, a fleet idle 5h on a
mechanical merge conflict, and a daemon hindsight-injection path dead at 0%
for weeks with no telemetry. Hydra is containment-complete and
resolution-absent.

**Solution:** Four cooperating layers — (1) prevent merge conflicts at spawn,
(2) make the PR the integration surface with mechanical rebase recovery,
(3) turn Pan into a diagnostician that files high-priority beads instead of
repairing state itself, (4) automate low-risk hydra-repo merge+deploy behind a
tier classifier, with degradation telemetry on every fail-open path.

**Success Metrics:**
- Integrate/merge failures per week: 570-class events → near zero (spawn-side
  prevention), measured from daemon log `CONFLICT (content)` lines.
- Time-to-resolution for quarantined work: human-hours → zero-touch for
  mechanical classes (rebase beads, signature-clustered meta-beads).
- Time-to-live for merged hydra dogfood fixes: hours → <30 minutes (tier-1
  auto-merge + canary deploy).
- Fail-open subsystems: 100% emit degradation counters into the Pan digest
  (0%-for-24h becomes an anomaly, not a surprise).
- Operator interventions per week drop to genuinely-human classes only:
  credentials, GitHub rulesets, HOLD decisions, novel failure classes.

**Agreed operator decisions (2026-09-28):**
1. Instant daemon-side repair verbs are kept ONLY for state hygiene:
   `reap-placeholder`, `prune-worktree-registration`, `reset-retries`,
   `repair-bead-store`. Everything touching repo content, merges, or code
   becomes a bead.
2. Singleton local integrate is DEMOTED to a no-remote fallback, not deleted.
3. Rebase beads auto-spawn on PR conflict (product repos included), capped at
   2 attempts per PR, then human.
4. Pan-filed beads are P1 — never outranking operator-explicit P0 — deduped by
   failure signature, capped at 2 open `pan-identified` beads per project.
   Pan keeps campaign-park as the emergency brake (containment) while P1 beads
   execute (resolution).

---

## 2. User Stories

### US-001: Spawn-Side Conflict Prevention
**As an** operator
**I want** parallel beads to never write the same file
**So that** integration merges are a formality instead of the dominant failure
class (570 failures from 2 beads)

**Type:** us-heavy
**Priority:** P1

**Acceptance Criteria:**
- A fleet run of the srv-telematics coverage campaign produces zero
  `CONFLICT (content)` lines attributable to handoff docs or shared test
  infrastructure.
- Beads whose target scope overlaps a running sibling are serialized at spawn
  (second bead waits), not at close.

**Aggregates:**
- FR-001: Per-Bead Handoff Fragments
- FR-002: Shared-Infra Spawn Exclusivity Lock
- FR-003: Scope Overlap Serialization at Spawn

### US-002: PR-First Integration
**As an** operator
**I want** every bead's deliverable to be a pushed feature branch with an open
PR against development and green gates
**So that** integration conflicts surface as GitHub PR state with mechanical
rebase recovery, not as local-merge quarantine pileups

**Type:** us-heavy
**Priority:** P1

**Acceptance Criteria:**
- Bead close succeeds when: branch pushed to origin, PR open against the
  development branch, configured gates green.
- Product repositories stop at PR (human merges). Hydra repository may proceed
  per US-004 tiers.
- A PR that becomes conflicting (`mergeable=false`) auto-spawns a rebase bead;
  after 2 failed rebase attempts the PR escalates to human.

**Aggregates:**
- FR-010: PR-First Close Contract
- FR-011: Automatic Rebase Beads

### US-003: Pan as Diagnostician and Bead Author
**As an** operator
**I want** Pan to file P1 beads with diagnosis and evidence when it identifies
an issue requiring repo content, merge, or code changes
**So that** resolution flows through the verified worker pipeline (gates, PR,
changelog) instead of bespoke daemon repair code paths

**Type:** us-heavy
**Priority:** P2

**Acceptance Criteria:**
- Pan's instant repair verbs shrink to the hygiene set (operator decision 1).
- A detected issue class produces exactly one P1 bead (dedupe by failure
  signature), carrying diagnosis, evidence, and suggested fix.
- Quarantine-labeled open beads surface as Pan judgment candidates; the
  stranded-work repair accepts open-labeled beads.
- N beads failing with the same integrate/close error signature produce ONE
  meta fix bead; the remaining beads are parked with a reference to it.

**Aggregates:**
- FR-020: Failure-Signature Clustering in Fix-Bead Creation
- FR-021: Quarantine-to-Pan Candidate Connection
- FR-022: Pan Bead-Filing Actuator

### US-004: Tiered Autonomy for Hydra Self-Repo
**As an** operator
**I want** low-risk hydra-repo fixes to merge and deploy themselves, with
core-loop changes and all product repos still requiring my merge
**So that** self-improvement latency drops from hours to minutes without me
losing control of the blast radius

**Type:** us-heavy
**Priority:** P2

**Acceptance Criteria:**
- A mechanical tier classifier assigns every hydra-repo PR to tier-1
  (auto-merge+deploy) or tier-1g (human merge); uncertainty fails CLOSED
  upward to human.
- Tier-1 deploys run drain → build → atomic binary swap → restart →
  verify-deploy with automatic rollback on failed verification.
- Product repositories never auto-merge.

**Aggregates:**
- FR-030: Autonomy Tier Classifier
- FR-031: Hydra Tier-1 Auto-Merge and Canary Deploy

### US-005: Degradation Telemetry on Fail-Open Paths
**As an** operator
**I want** every fail-open subsystem to emit a health counter into the Pan
digest
**So that** "0% for 24h" states (like the hindsight injection timeout) surface
as anomalies within a day instead of being found by manual audit weeks later

**Type:** us-light
**Priority:** P2

**Acceptance Criteria:**
- Hindsight context injection, retain path, graphify, settled-state, and any
  future fail-open subsystem each expose a success/attempt counter.
- The Pan health digest renders anomaly lines for counters stuck at 0% over a
  configurable window.

**Aggregates:**
- FR-032: Fail-Open Degradation Counters

### US-006: Operator State API
**As an** operator
**I want** park/unpark and quarantine inspection via the overseer API
**So that** operator actions don't require root sqlite access to registry.db

**Type:** us-light
**Priority:** P3

**Acceptance Criteria:**
- API endpoints cover: park project, unpark project, list quarantined
  beads/branches, clear quarantine label (audited).
- All mutations emit typed events.

**Aggregates:**
- FR-033: Operator Park/Quarantine API

---

## 3. Functional Requirements

### FR-001: Per-Bead Handoff Fragments
**Priority:** P1
**Target Files:** `cmd/worker/main.go` (prompt builder), `cmd/worker/chain_handoff.go`, `docs/` templates
- Discovery/handoff artifacts are written to
  `docs/hydra/handoff/<phase>-<bead-id>.md` instead of a shared per-phase file.
- Chain close assembles fragments into the canonical handoff document.
- Eliminates the 568-conflict `discovery.md` hotspot.

### FR-002: Shared-Infra Spawn Exclusivity Lock
**Priority:** P1
**Target Files:** `internal/core/scheduler.go`, `internal/core/worker_spawner.go`, project config schema
- Configurable per-project path list (e.g. `jest.config.ts`, `test/setup/**`,
  `eslint.config.mjs`) marks beads as exclusive.
- An exclusive bead spawns only when no other live worker holds any exclusive
  path; otherwise it is deferred to the next scheduler tick.
- Eliminates the 397-conflict test-infra hotspot.

### FR-003: Scope Overlap Serialization at Spawn
**Priority:** P1
**Target Files:** `internal/core/scope_gate.go`, `internal/core/scheduler.go`
- The existing target-scope guard moves from close-time to spawn-time: a bead
  whose declared targets intersect a live sibling's targets is deferred.
- Eliminates the 171-conflict overlapping-spec hotspot.

### FR-010: PR-First Close Contract
**Priority:** P1
**Target Files:** `internal/core/bead_closer.go`, `internal/core/publisher.go`, `internal/core/worktree_manager.go`
- Close flow: push `hydra/wt-<bead>` to origin → `gh pr create` against the
  repo's development branch → wait for configured gates → close bead.
- Singleton `git merge --no-ff` into a local working branch runs only when the
  project has no remote configured.
- Product repos stop at PR-open + gates-green; hydra repo proceeds per FR-031.

### FR-011: Automatic Rebase Beads
**Priority:** P2
**Target Files:** `internal/core/publisher.go` (PR watch), `internal/core/bead_discovery.go`
- A periodic PR watch detects `mergeable=false` on hydra-open PRs and files a
  rebase bead: rebase feature branch onto development, re-run gates,
  force-push.
- Max 2 rebase attempts per PR; third conflict escalates to human with the
  conflict summary.

### FR-020: Failure-Signature Clustering in Fix-Bead Creation
**Priority:** P1
**Target Files:** `internal/core/fix_cause.go`, `internal/core/bead_closer.go`
- Close-fail errors are normalized to a signature (operation + conflicted
  path set / error class, bead-id scrubbed).
- If an open meta fix bead with the same signature exists, the failing bead is
  parked referencing it instead of spawning a new per-bead fix bead.
- One signature = one meta bead; on meta-bead close, parked beads are released.

### FR-021: Quarantine-to-Pan Candidate Connection
**Priority:** P1
**Target Files:** `internal/pan/runner.go` (candidate collection), `internal/core/stranded_work.go`
- Open beads carrying `closed-failed`/`integration-failed`/
  `fix-recursion-capped` labels surface as Pan judgment candidates with their
  label history and last error.
- `DetectStrandedWork` accepts branches whose owning bead is quarantine-labeled
  (not only terminal/unknown).
- `repair:integrate-stranded` is replaced by bead filing per US-003 (rebase or
  meta-fix bead), not daemon-side merging.

### FR-022: Pan Bead-Filing Actuator
**Priority:** P2
**Target Files:** `internal/pan/pan.go`, `internal/pan/runner.go`, `internal/beads/client.go`
- `repair:log-improvement` upgraded: files a real P1 bead on the hydra repo
  with label `pan-identified`, description = diagnosis + evidence + suggested
  fix.
- Dedupe by failure signature; cap 2 open `pan-identified` beads per project.
- Instant repair verbs shrink to: `reap-placeholder`,
  `prune-worktree-registration`, `reset-retries`, `repair-bead-store`.
- Campaign park/unpark verbs remain (containment brake).

### FR-030: Autonomy Tier Classifier
**Priority:** P2
**Target Files:** `internal/core/publisher.go` (or new `internal/core/tier.go`)
- Classifies hydra-repo PRs: tier-1 (docs, config knobs, telemetry, tests,
  non-core packages) vs tier-1g (scheduler, closer, integrate, worktree, Pan,
  registry, api).
- Inputs: paths touched, owning subsystem, test coverage delta. Any doubt →
  tier-1g (fail closed toward human).

### FR-031: Hydra Tier-1 Auto-Merge and Canary Deploy
**Priority:** P2
**Target Files:** `internal/core/publisher.go`, new deploy orchestration (daemon-side), `scripts/verify-deploy.sh`
- Tier-1 PR with green gates: squash-merge via admin token, then deploy:
  drain (registry park / max-workers API) → build → atomic binary swap →
  restart → `verify-deploy.sh` → automatic rollback to previous binary on
  verification failure.
- Every step emits typed events; deploys are announced in the fleet digest.
- Never runs with workers in-flight (drain precondition).

### FR-032: Fail-Open Degradation Counters
**Priority:** P2
**Target Files:** `agents/context-processor.go`, `cmd/worker/retain.go`, `internal/core/pan_health.go`
- Each fail-open subsystem records attempt/success counters (in-memory +
  digest snapshot).
- Pan digest renders an anomaly line when a subsystem is at 0% success over
  the configured window (default 24h).
- Includes the hindsight injection path (see bead hydra-bye7 for the 500ms
  timeout fix this counter would have caught).

### FR-033: Operator Park/Quarantine API
**Priority:** P3
**Target Files:** `internal/api/server.go`, `internal/registry/registry.go`
- Endpoints: park/unpark project, list quarantined beads and branches, clear
  quarantine label (with audit event).
- Replaces direct sqlite manipulation of `registry.db`.

---

## 4. Non-Functional Requirements

### NFR-001: Human Boundary (hard)
Product-repo merges, credentials, GitHub rulesets, HOLD-gate decisions, and
novel (unclassified) failure classes remain human-only. The tier classifier
fails CLOSED toward human on any doubt.

### NFR-002: Observability
Every new actuator (bead filing, rebase beads, tier classification, auto-merge,
deploy steps) emits a typed event on the shepherd stream. Fail-open subsystems
emit degradation counters per FR-032.

### NFR-003: Priority Discipline
Pan-filed beads are P1; they never preempt operator-explicit P0 work. Caps:
2 open `pan-identified` per project; 2 rebase attempts per PR.

### NFR-004: Rollback
Tier-1 deploys keep the previous binary and roll back automatically on failed
verification. All new behavior ships behind config flags defaulting to the
current behavior where feasible.

---

## 5. Rollout Order

1. **Set 1 (FR-001..003)** — unblocks the srv-telematics campaign (570-failure
   class) at the spawn side.
2. **Set 2 (FR-010, FR-011)** — PR-first close + rebase recovery; removes the
   quarantine pileup mechanism.
3. **Set 3 (FR-020..022)** — resolution layer: clustering, quarantine→Pan,
   Pan bead filing.
4. **Set 4 (FR-030..033)** — tiered auto-merge/deploy, telemetry, operator API.

Each set lands independently behind flags; sets 2–4 degrade gracefully to
today's behavior when disabled.

## 6. Non-Goals

- Auto-merging product-repo PRs (Tier 2 stays human-merge).
- Rotating credentials, changing GitHub rulesets, or releasing HOLD gates.
- Replacing hindsight or other infrastructure (investigated 2026-09-28;
  engine is competitive, wiring was the bug).
- Deleting the singleton local-integrate path (demoted to no-remote fallback
  per operator decision 2).

## 7. Related

- Roadmap: `docs/roadmap/cass-knowledge-federation-20260928.md` (knowledge
  locality — orthogonal).
- Bead hydra-bye7: daemon hindsight `MaxQueryLatency` 500ms fix (motivating
  case for FR-032).
- Evidence log: 570 `[Integrate] FAILED` events from beads pmgh/5gq;
  conflict distribution `discovery.md` 568 / test-infra 397 / overlapping
  specs 171; 86 `hydra/quarantine/*` branches; daemon injection 0% hit rate
  until 2026-09-28 audit.
