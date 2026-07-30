# Hydra System Loop — Process Flow & Loop-Closure Map

**Status:** living document · **Updated:** 2026-07-30

Hydra is three nested loops. Each subsection shows the flow, which component
implements each edge, and whether the edge is **automated**, **planned**, or
**deliberately human**. The compounding claim is only honest if every loop's
exit feeds a later iteration's input — the closure table at the end audits
exactly that.

Legend (all diagrams): solid = automated today · dashed = planned (PRD exists)
· thick = human-by-design.

Companion operational reference: [`shepherd.md`](shepherd.md) — the event
stream, quarantine, fix-bead lifecycle, concurrency governor, and verdict
memory that this page references.

---

## 1. Inner loop — Build (per PRD, bounded by `max_cycles`)

```mermaid
flowchart TD
    PRD["PRD authored<br/><i>create-prd skill (adversarial review)</i>"] --> TB["task-breakdown<br/><i>pi-config/skills/task-breakdown</i><br/>+ prd-lint preflight"]
    TB --> GATE["approval gate<br/><b>human close</b>"]
    GATE --> EXEC["bead execution<br/><i>worktree-isolated TDD chains<br/>RED→GREEN→REFACTOR→SYNTAX<br/>pi rpc + rtk/caveman/context-mode</i>"]
    EXEC --> GATES{"verified close<br/><i>bead_closer.go</i><br/>TDD validation · diff gate ·<br/>scope gate (worktree-aware)"}
    GATES -->|reject| RETRY["reopen + Reflexion retry<br/><i>≤3 rounds, then escalate</i>"]
    RETRY --> EXEC
    GATES -->|"failed close → closed-failed label<br/>+ cause extracted from worker log"| FIXB["fix bead — cap 1 per original<br/><i>carries '### What failed last time'</i><br/>dedup: open fix blocks duplicates"]
    FIXB --> EXEC
    GATES -->|accept| COMMIT["micro-commit<br/><i>BuildCommitMessage (conventional)</i>"]
    COMMIT --> CHAIN{"chain complete —<br/>any closed-failed member?"}
    CHAIN -->|clean| INTEG["integrate to working branch<br/><i>IntegrateAndRemove</i>"]
    CHAIN -->|failed| QUAR["quarantine<br/><i>hydra/quarantine/&lt;key&gt; branch</i><br/>work preserved, branch untouched"]
    INTEG --> DONE{"all beads closed?"}
    DONE -->|yes| ANALYZER["compliance analyzer<br/><i>hydra-compliance-analyzer</i><br/>placeholders · stubs · phantom files ·<br/>exports · routes · US-ACs · source-vs-go<br/>+ security-scan verifier"]
    ANALYZER -->|gaps| ADJ["gap adjudicator<br/><i>pi k3-256k, max thinking</i><br/>prompt includes prior verdicts (D3)"]
    ADJ -->|real gaps| G2P["gap-to-prd → mini-PRD<br/><i>cycle cap 2</i>"]
    G2P --> TB
    ADJ -->|clean| SHIP["ship-gate (optional)<br/><i>build/boot/e2e</i>"]
    SHIP --> APPROVED["PRD approved (sticky)"]

    style GATE stroke-width:4px
```

Bounded: the analyzer→gap-to-prd→task-breakdown cycle stops at `max_cycles`
(default 2) or when the gap set stops shrinking (progress-delta). Fix-bead
recursion caps at one level (`fix-recursion-capped` label marks the refusal).

**Known gap (2026-07-30, run D3 evidence):** the compliance gate closed clean
on a run whose working branch contained zero integrated code — the analyzer's
verdict is not yet derived from repo state. State-based gate truth is the next
A-track item.

---

## 2. Middle loop — Learning (per worker prompt, compounds across runs)

```mermaid
flowchart LR
    RUN["bead attempt<br/><i>rpc · per-turn token stats</i>"] -->|failure summary| REFLX["Reflexion<br/><i>HYDRA_PREV_FAILURE</i>"]
    RUN -->|"curated patterns<br/><i>pattern-quality-reviewer</i>"| HS["Hindsight memory<br/><i>hindsight-pi via pi</i>"]
    RUN -->|proven rules| CASS["CASS playbook<br/><i>cm context --json</i>"]
    GATES["gate & adjudicator verdicts<br/><i>VerdictRetainer (Shepherd D3)</i>"] -->|"adjudications + failed-close causes"| HS
    GR["graphify code graph<br/><i>per-project AST, no model call</i>"] -->|"blast radius / structure queries"| PROMPT
    REFLX --> PROMPT["next worker prompt<br/><i>+ per-phase doctrine ·<br/>DELIVERABLE CONTRACT ·<br/>stack-scoped skills</i>"]
    HS -->|top-k recall| PROMPT
    HS -->|"top-3 prior verdicts"| ADJP["adjudication prompt"]
    CASS -->|top-3 rules| PROMPT
    PROMPT --> RUN
```

Every edge automated. This is the per-run compounding: each attempt makes the
next attempt's prompt smarter, and — since D3 — each *judgment* (gate
rejection cause, adjudicator verdict with reasoning) is retained instead of
evaporating. `ambiguous` verdicts and cause-less failures are deliberately not
retained: an unsure judgment is noise, not memory.

**This loop fails silently, and that is its most dangerous property.** On
2026-07-27 a one-line change to Hindsight recall switched the entire read path
off for every worker, and nothing in the system noticed. The recall API returned
HTTP 200 with an empty result set, so workers logged the routine `No relevant
memories found` and carried on with no prior experience. Beads still closed,
gates still passed, metrics still looked healthy — the run simply stopped
compounding. It was caught only by comparing worker logs either side of the
deploy: every worker before it logged `Retrieved 5 relevant memories`, every
worker after logged none.

Two structural lessons for anyone touching this loop:

1. **An empty recall is indistinguishable from a working recall** unless someone
   compares across runs. There is no gate on "did the worker actually receive
   memories," the way `RequireDiffOnClose` gates real code changes. Until there
   is, a recall regression can survive indefinitely.
2. **Correcting a wrong parameter name is not a safe rename.** The old code sent
   the bead type as `type`, which the API ignored; renaming it to the real
   `types` turned an inert parameter into an enforced filter, and bead types
   match none of Hindsight's memory kinds (`experience`, `observation`, `world`).
   The behaviour depended on being ignored.

Recall latency is 3.4–4.0s measured against the live service, so any client
built without an explicit timeout above that fails 100% of the time — also
silently. The default is now 10s for that reason.

---

## 3. Outer loop — Meta (self-improvement; the loop that builds Hydra)

```mermaid
flowchart TD
    RUNS["scored dogfood runs<br/><i>eval/run.sh</i><br/>+ testbed known-answer runs"] --> METRICS["metrics JSON<br/><i>tools/hydra-eval</i><br/>accepts · one-shot rate · stuck kills<br/>(passthrough-honest since 07-28)"]
    METRICS --> DETECT["detect anomalies"]
    LOGS["daemon log signatures<br/>+ typed event stream<br/><i>central day-files + per-repo<br/>.hydra/events.jsonl (C1/C2)</i>"] --> DETECT
    DETECT -->|"tools/hydra-observer Phase 1"| BACKLOG["ranked backlog<br/>+ draft finding"]
    DETECT ==>|"manual log forensics<br/>(Claude/human)"| BACKLOG
    BACKLOG ==>|"human/Claude authors PRD<br/><i>create-prd + adversarial review</i>"| NEWPRD["improvement PRD"]
    BACKLOG -.->|"Phase 2: Proposer (roadmap)"| NEWPRD
    NEWPRD --> DOGFOOD["dogfood run<br/><i>hydra builds the fix into itself</i><br/>fleet self-governs concurrency (AIMD, B1)"]
    DOGFOOD --> PRQ["PR to development<br/><i>CI + pre-push guards</i>"]
    PRQ ==>|"human merge<br/><b>by design, stays</b>"| MERGE["merged"]
    MERGE --> TBED["testbed validation<br/><i>seeded-failure scenario<br/>docs/ASSERTIONS.md</i>"]
    TBED -.->|"planned: nightly matrix cron (D2)"| TBED
    TBED ==>|"deploy<br/><b>human today; Phase 3 canary+rollback</b>"| DEPLOY["daemon updated"]
    DEPLOY --> RUNS

    style PRQ stroke-width:4px
    style MERGE stroke-width:4px
```

Since 2026-07-29 the working practice is a **single long-lived branch**
(`feat/shepherd` era): work is committed and deployed from the branch, and PRs
are raised from it when a unit completes — review does not block the loop,
because prod is ahead of the PR, not behind it.

---

## Loop-closure audit (is it a closed, compounding system?)

| Loop | Closed? | Compounds via | Manual links | Mechanization plan |
|---|---|---|---|---|
| Inner (build) | ✅ closed, bounded (cycles 2, fix-cap 1) | gap beads fix the same PRD's deficits within the run | approval gate (by design) | ✅ A1 quarantine + A2 cause-aware fix beads shipped 07-29 · next: state-based gate truth, pathology brake (B2) |
| Middle (learning) | ✅ closed, verified live | CASS rules + Hindsight memories + Reflexion + code graph persist across runs; ✅ verdicts retained since 07-30 (D3) | none | recall-liveness gate (the silent-failure class) |
| Outer (meta) | ✅ topology closed; cadence human | every merged improvement changes the daemon that runs the next loop | PRD authoring · merge (by design) | Observer shipped · Proposer (roadmap) · shepherd cron (D1) · nightly matrix (D2) |

**Resilience note (2026-07-27):** a daemon restart used to orphan in-flight
beads permanently — Charon's recovery was unreachable whenever no workers were
tracked, which is precisely when orphans exist. That is fixed: recovery now
runs every cleanup cycle and **reopens** orphaned beads. A mid-run restart is
survivable and has been survived four times in production since.

**Verdict:** the system is a closed compounding loop at the topology level —
each loop's output verifiably feeds a later iteration's input, including (since
D3) its own judgments. Cadence of the outer loop is human-limited at two links;
merge is *kept* human as the safety invariant (roadmap:
`self-improvement-loop.md`).

## Evidence that compounding is real (measured, not asserted)
- Gate fix (#65): empty-diff rejections 21 → 0, one-shot accepts 0 → 14 —
  detected by outer loop, fixed via inner loop, verified by `hydra-eval`.
- Anti-consultant doctrine (#56): 0 accepts → 13 accepts between consecutive
  scored runs of the same PRD.
- 9 → 3 divergent implementations across dogfood runs 4→6 after
  Reflexion + CASS injection (middle loop).
- Fix-bead churn against a seeded guaranteed failure: 158 → 25, zero nested
  (was 3-deep), refusals labelled (07-28, testbed).
- Fleet freeze on transient 429s: 15m → 0 across a 219-hang provider storm
  (exit-7 classification, 07-28).
- Hydra built its own AIMD concurrency governor in one dogfood run (B1: 36
  beads, 3 gates closed, harvested and deployed same day, 07-29).
- 26/26 fix beads in run B1 carried their predecessor's failure cause (A2).
