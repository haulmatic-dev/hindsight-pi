# Hydra System Loop — Process Flow & Loop-Closure Map

**Status:** living document · **Updated:** 2026-07-24

Hydra is three nested loops. Each subsection shows the flow, which component
implements each edge, and whether the edge is **automated**, **planned**, or
**deliberately human**. The compounding claim is only honest if every loop's
exit feeds a later iteration's input — the closure table at the end audits
exactly that.

Legend (all diagrams): solid = automated today · dashed = planned (PRD exists)
· thick = human-by-design.

---

## 1. Inner loop — Build (per PRD, bounded by `max_cycles`)

```mermaid
flowchart TD
    PRD["PRD authored<br/><i>create-prd skill (adversarial review)</i>"] --> TB["task-breakdown<br/><i>pi-config/skills/task-breakdown</i>"]
    TB -.->|"planned: prd-lint preflight"| TB
    TB --> GATE["approval gate<br/><b>human close</b>"]
    GATE --> EXEC["bead execution<br/><i>worktree-isolated TDD chains<br/>RED→GREEN→REFACTOR→SYNTAX</i>"]
    EXEC --> GATES{"verified close<br/><i>bead_closer.go</i><br/>TDD validation · diff gate ·<br/>scope gate (worktree-aware)"}
    GATES -->|reject| RETRY["reopen + Reflexion retry<br/><i>≤3 rounds, then escalate</i>"]
    RETRY --> EXEC
    GATES -->|accept| COMMIT["micro-commit<br/><i>BuildCommitMessage (conventional)</i>"]
    COMMIT --> INTEG["integrate to working branch<br/><i>IntegrateAndRemove / reconciler</i>"]
    INTEG --> PUB["publish branch to remote<br/><i>PublishRepo + per-user PAT</i><br/>(built, opt-in via output.push)"]
    INTEG --> DONE{"all beads closed?"}
    DONE -->|yes| ANALYZER["compliance analyzer<br/><i>hydra-compliance-analyzer</i><br/>placeholders · stubs · phantom files ·<br/>exports · routes · US-ACs · source-vs-go"]
    ANALYZER -.->|"planned: security-scan verifier<br/>(gitleaks + govulncheck)"| ANALYZER
    ANALYZER -->|gaps| ADJ["gap adjudicator<br/><i>pi, false-positive filter</i>"]
    ADJ -->|real gaps| G2P["gap-to-prd → mini-PRD<br/><i>(doctrine fix PRD pending)</i>"]
    G2P --> TB
    ADJ -->|clean| SHIP["ship-gate (optional)<br/><i>build/boot/e2e</i>"]
    SHIP --> APPROVED["PRD approved (sticky)"]

    style GATE stroke-width:4px
    style PUB stroke-dasharray: 5 5
```

Bounded: the analyzer→gap-to-prd→task-breakdown cycle stops at `max_cycles`
or when the gap set stops shrinking (progress-delta).

---

## 2. Middle loop — Learning (per worker prompt, compounds across runs)

```mermaid
flowchart LR
    RUN["bead attempt"] -->|failure summary| REFLX["Reflexion<br/><i>HYDRA_PREV_FAILURE</i>"]
    RUN -->|"curated patterns<br/><i>pattern-quality-reviewer</i>"| HS["Hindsight memory<br/><i>hindsight-pi via pi</i>"]
    RUN -->|proven rules| CASS["CASS playbook<br/><i>cm context --json</i>"]
    REFLX --> PROMPT["next worker prompt<br/><i>+ per-phase doctrine ·<br/>DELIVERABLE CONTRACT ·<br/>stack-scoped skills</i>"]
    HS -->|top-k recall| PROMPT
    CASS -->|top-3 rules| PROMPT
    PROMPT --> RUN
```

Every edge automated. This is the per-run compounding: each attempt makes the
next attempt's prompt smarter.

**Known leak:** gate and adjudicator *decisions* (why closes were rejected,
which gaps were false positives) are not fed back into CASS/Hindsight — only
worker-level patterns are. The richest judgment data in the system currently
evaporates.

---

## 3. Outer loop — Meta (self-improvement; the loop that builds Hydra)

```mermaid
flowchart TD
    RUNS["scored dogfood runs<br/><i>eval/run.sh</i>"] --> METRICS["metrics JSON<br/><i>tools/hydra-eval</i><br/>accepts · one-shot rate · stuck kills"]
    METRICS --> DETECT["detect anomalies"]
    LOGS["daemon log signatures"] --> DETECT
    DETECT -.->|"planned: tools/hydra-observer<br/>(PRD exists — Phase 1)"| BACKLOG["ranked backlog<br/>+ draft finding"]
    DETECT ==>|"today: manual log forensics<br/>(Claude/human)"| BACKLOG
    BACKLOG ==>|"today: human/Claude authors PRD<br/><i>create-prd + adversarial review</i>"| NEWPRD["improvement PRD"]
    BACKLOG -.->|"Phase 2: Proposer (roadmap)"| NEWPRD
    NEWPRD --> DOGFOOD["dogfood run<br/><i>hydra builds the fix into itself</i>"]
    DOGFOOD --> PRQ["PR to development"]
    PRQ ==>|"human merge<br/><b>by design, stays</b>"| MERGE["merged"]
    MERGE ==>|"deploy (make build + restart)<br/><b>human today; Phase 3 canary+rollback</b>"| DEPLOY["daemon updated"]
    DEPLOY --> RUNS

    style PRQ stroke-width:4px
    style MERGE stroke-width:4px
```

---

## Loop-closure audit (is it a closed, compounding system?)

| Loop | Closed? | Compounds via | Manual links | Mechanization plan |
|---|---|---|---|---|
| Inner (build) | ✅ closed, bounded | gap beads fix the same PRD's deficits within the run | approval gate (by design) | prd-lint preflight + security-scan verifier + gap-to-prd doctrine (PRDs exist) harden edges |
| Middle (learning) | ✅ closed | CASS rules + Hindsight memories + Reflexion persist across runs | none | leak to fix: feed gate/adjudicator verdicts into CASS/Hindsight |
| Outer (meta) | ✅ topology closed; cadence human | every merged improvement changes the daemon that runs the next loop | detect/diagnose · PRD authoring · merge+deploy | Observer (PRD exists) → Proposer (roadmap) → merge stays human; deploy gains canary+rollback (roadmap Phase 3) |

**Resilience note (2026-07-27):** a daemon restart used to orphan in-flight
beads permanently — Charon's recovery was unreachable whenever no workers were
tracked, which is precisely when orphans exist. That is fixed: recovery now
runs every cleanup cycle and **reopens** orphaned beads (it previously
force-closed them, silently unblocking dependents on unfinished work). A
mid-run restart is therefore survivable, and worth testing deliberately.

**Verdict:** the system is a closed compounding loop at the topology level —
each loop's output verifiably feeds a later iteration's input. Cadence of the
outer loop is currently human-limited at three links; the Observer and
Proposer phases exist precisely to mechanize the first two. The merge link is
*kept* human as the safety invariant (roadmap: `self-improvement-loop.md`).

## Evidence that compounding is real (measured, not asserted)
- Gate fix (#65): empty-diff rejections 21 → 0, one-shot accepts 0 → 14 —
  detected by outer loop, fixed via inner loop, verified by `hydra-eval`.
- Anti-consultant doctrine (#56): 0 accepts → 13 accepts between consecutive
  scored runs of the same PRD.
- 9 → 3 divergent implementations across dogfood runs 4→6 after
  Reflexion + CASS injection (middle loop).
