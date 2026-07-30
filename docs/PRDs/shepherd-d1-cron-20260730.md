# PRD: Shepherd Cron — The Never-Sleeping Engineer (Shepherd D1)

## 1. Overview

**Problem:** Hydra's failures are now *recorded* (typed event stream, labels, quarantine branches) but nobody *watches*. Every pathology this week was caught by a human happening to look: the hindsight-api leak blocked all spawns for 2.5 hours with the only signal being per-bead log spam; run-H worktrees sat stale for days; Hindsight recall died silently twice and was caught only by diffing worker logs across deploys. The event stream (C1/C2) was built to be watched — the watcher does not exist.

**Solution:** A shepherd binary run by cron (every 10 minutes) that reads the central event day-files and cheap system probes, detects a fixed set of pathologies, and escalates two ways: machine/human-readable findings appended to each affected repo's `.hydra/escalations.jsonl` (the user's responsibility to act on), and a central findings file (our analysis DB feed). No model calls; deterministic; observes and reports, never mutates project state — with one deliberate exception: pruning provably-dead worktrees.

**Success Metrics:**
- Replaying the 2026-07-30 morning window (spawn_blocked events + high hindsight RSS) produces a `fleet_stalled` finding within one cron tick.
- A repo accumulating 3+ fix beads for one original in an hour produces a `fix_storm` finding naming the original bead.
- Two consecutive runs with recall injections at zero while Hindsight is enabled produce a `recall_dead` finding (the silent-failure class, mechanized).
- Stale worktrees whose chains are fully terminal and integrated/quarantined are pruned; nothing else is touched.

---

## 0. Scope Profile
- **System Type:** cli (new `tools/hydra-shepherd`, single static binary) + ops (cron unit)
- **Interfaces:** reads `$HYDRA_HOME/events/*.jsonl`, `/proc` + `systemctl show` probes, `git worktree list`; writes `.hydra/escalations.jsonl` (per repo) and `$HYDRA_HOME/shepherd/findings-YYYY-MM-DD.jsonl` (central)
- **Surfaces Explicitly Excluded:** the daemon (no daemon code changes in this PRD), model calls, auto-remediation beyond worktree pruning, notification transports (Slack/email — findings files only for now)
- **Deployment Model:** cron-invoked binary (systemd timer), stateless between runs except a small cursor file

---

## 2. User Stories

### US-001: Pathologies surface without a human watching — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Event & Probe Collection
- FR-002: Pathology Detectors
- FR-003: Two-Tier Escalation Writer
- FR-004: Stale Worktree Pruning

**Acceptance Criteria:**
- `go build ./tools/hydra-shepherd` exits 0
- `go test ./tools/hydra-shepherd/...` exits 0
- Driving the binary against a fixture events dir reproducing the 07-30 stall yields a `fleet_stalled` finding in both the central file and the repo escalation file
- A second run over the same events produces zero duplicate findings (cursor)
- No mocking of internal dependencies: fixtures are real JSONL files and a real temp git repo

---

## 3. Functional Requirements

### FR-001: Event & Probe Collection
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-shepherd/collect.go`, `tools/hydra-shepherd/collect_test.go`, `tools/hydra-shepherd/main.go`, `tools/hydra-shepherd/go.mod`

1. Read events from `$HYDRA_HOME/events/` day-files for a lookback window (default 6h, flag `--window`), tolerating torn lines (reuse the parsing approach of `core.ReadEvents` — the shepherd is a separate module, so it carries a small local copy with the same torn-line contract, pinned by a shared fixture).
2. Maintain a cursor file `$HYDRA_HOME/shepherd/cursor.json` recording the last-processed event timestamp per day-file; a finding is only raised for events after the cursor, and the cursor advances only after findings are written.
3. Cheap probes, each fail-open (a probe error yields "unknown", never a crash): hydra service active + MainPID RSS, hindsight service RSS (via `/proc/<pid>/status` VmRSS), worker process count, per-project `git worktree list` against `hydra/wt-*` branches.
4. `--home` flag overrides `$HYDRA_HOME`; `--dry-run` prints findings to stdout without writing.

**Acceptance Criteria:**
- A fixture dir with two day-files and a torn final line parses all intact records
- The cursor prevents re-processing: two consecutive runs over static fixtures yield findings once
- A missing events dir, missing cursor, or unreadable /proc path yields empty collections and exit 0
- `--dry-run` writes no files

### FR-002: Pathology Detectors
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-shepherd/detect.go`, `tools/hydra-shepherd/detect_test.go`

Pure functions from collected state to findings. Each finding: `{time, kind, severity, project, subject, evidence, suggestion}`. Detectors, with thresholds as constants:

1. `fleet_stalled` (critical): any `spawn_blocked` event in the window, or (hydra active AND worker count 0 AND open-bead work implied by ≥1 `bead_closed`/`fix_created` event in the prior window but none in this one). Evidence includes the probe snapshot.
2. `fix_storm` (warn): ≥3 `fix_created` events for the same `fields.original` within any 60-minute span.
3. `quarantine_repeat` (warn): ≥2 `chain_quarantined` events for the same chain key in the window — the fix loop is not converging.
4. `memory_pressure` (warn / critical at 85%): hindsight or hydra RSS exceeding 4 GiB (warn) / system memory above 85% (critical). The 07-30 leak reached 20 GiB with no signal.
5. `recall_dead` (critical): Hindsight service active but zero `verdict_retained` events AND zero recall evidence across two consecutive shepherd windows that contain ≥5 `bead_closed` events (i.e., the fleet worked, memory saw nothing). Uses a two-window state carried in the cursor file.
6. `provider_storm` (info): ≥10 `bead_closed` events with `fields.exit_code` in {2,7} in the window — context for the governor's behaviour, not an action item.

**Acceptance Criteria:**
- Table-driven tests cover each detector's fire and no-fire cases
- The 07-30 fixture (spawn_blocked + 91.8% memory) yields `fleet_stalled` (critical) and `memory_pressure` (critical)
- A window with 3 `fix_created` for one original fires `fix_storm`; 2 does not
- `recall_dead` requires both windows deficient — one quiet window does not fire
- Detectors are pure: no I/O in `detect.go` (enforced by the test importing it alone)

### FR-003: Two-Tier Escalation Writer
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-shepherd/escalate.go`, `tools/hydra-shepherd/escalate_test.go`

1. Central: append findings as JSONL to `$HYDRA_HOME/shepherd/findings-YYYY-MM-DD.jsonl`.
2. Per-repo: findings whose `project` resolves to a repo path (from the events' `project` field joined against a `--projects` flag or `$HYDRA_HOME/config` project list) are also appended to `<repo>/.hydra/escalations.jsonl`, and `.hydra/` is gitignored using the same idempotent line-append contract as the event sink. Findings without a resolvable repo go central-only.
3. Dedup: a finding with the same `(kind, subject)` as one already written today is skipped (read-back of today's central file), so a persisting condition produces one finding per day, not one per tick.
4. Writes are atomic appends; a write failure to one repo must not abort the others or the central write.

**Acceptance Criteria:**
- A finding for a known project lands in both tiers with identical content; unknown project lands central-only
- Same condition on the next run: no duplicate line (dedup by kind+subject per day)
- Repo `.gitignore` gains `.hydra/` exactly once across repeated runs
- A read-only repo directory does not prevent the central write (asserted with a chmod-0 dir)

### FR-004: Stale Worktree Pruning
**Type:** fr
**Priority:** P2

**Target Files:** `tools/hydra-shepherd/prune.go`, `tools/hydra-shepherd/prune_test.go`

The one mutation the shepherd may perform, because staleness is mechanical: for each project worktree under `$HYDRA_HOME/.hydra-worktrees/`, when the worktree's `hydra/wt-<key>` branch has **zero commits ahead** of the working branch AND no live worker owns the key (no process match, worktree mtime older than `--min-age`, default 24h), run `git worktree remove` + branch delete. Anything with commits ahead is left for the daemon's reconciler (gate-truth FR-002) — the shepherd never decides integrate-vs-quarantine.

**Acceptance Criteria:**
- A 25h-old worktree with zero commits ahead is removed, branch deleted, `worktree_pruned` finding written
- A worktree with one commit ahead is untouched regardless of age
- A worktree younger than min-age is untouched
- Prune errors on one worktree do not stop the sweep

---

## 4. Non-Functional Requirements

### NFR-001: The shepherd observes; it cannot break the flock
**Type:** nfr-verification
**Priority:** P1

**Target Files:** `tools/hydra-shepherd/main.go`

**Acceptance Criteria:**
- `go build ./tools/hydra-shepherd` exits 0
- `go test ./tools/hydra-shepherd/... -count=1` exits 0
- `main.go` contains no call that mutates beads, closes beads, or writes into a repo working tree (escalations/gitignore under `.hydra/` and worktree pruning excepted) — pinned by a test greping the package for `br `, `CloseBead`, and `git commit`
- Exit code is 0 on every path except flag-parse errors (cron must not flap on findings)

**Verifies:**
- FR-002: Pathology Detectors
- FR-003: Two-Tier Escalation Writer

---

## 5. Out of Scope
- Notification transports (Slack, email, GitHub issues) — findings files are the contract; transports read them later
- Model-based triage of findings (observer Phase 1b territory)
- Auto-remediation beyond zero-ahead worktree pruning (no service restarts, no bead mutations)
- Daemon changes (spawn_blocked event already shipped separately)
- The nightly testbed matrix (D2) — separate cron, separate PRD

---

## 6. Ops (post-merge, not part of the dogfood run)
- systemd service `hydra-shepherd.service` (oneshot) + `hydra-shepherd.timer` every 10 minutes, `User=hydra`, `HYDRA_HOME=/opt/hydra`
- Install binary via the existing packaging path

---

## 7. Adversarial Review

**Reviewed by:** Claude Fable 5 (independent pass after drafting)

- **"A cron watcher is a second brain that can drift from the daemon's truth."** It consumes only the daemon's own emitted events plus kernel-level probes, and its detectors are threshold arithmetic — there is no second model of bead state to drift.
- **"Dedup by kind+subject per day could suppress a re-occurrence after a fix."** Acceptable: findings are a day-granular digest, and the condition persisting after a fix produces the next day's finding. The alternative (per-tick findings) recreates log spam, which is the failure mode this exists to replace.
- **"Worktree pruning from cron races the live daemon."** Pruning requires zero commits ahead AND min-age 24h AND no owning process — a chain the daemon still cares about fails at least one of those. The destructive-decision cases (commits ahead) are explicitly left to the daemon's reconciler.
- **"recall_dead uses verdict_retained as its recall proxy, but a healthy run with zero failed closes and zero fresh adjudications emits none."** That is why it requires two consecutive deficient windows *with* ≥5 closes each and Hindsight active; a genuinely quiet-but-healthy fleet has few closes and does not qualify. False-positive cost is one finding line; false-negative cost is the 07-27 incident class persisting — the asymmetry favours firing.
- **"Why a separate module instead of importing internal/core?"** `tools/` binaries in this repo are standalone modules (hydra-eval precedent); a small copied reader with a shared fixture is cheaper than restructuring module boundaries, and the fixture pins the contract.
