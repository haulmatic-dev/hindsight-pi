# PRD: Autonomy Hygiene — Stale-Filing Self-Resolution Coverage + Beads Mirror Audit

**Version:** 1.0 · **Date:** 2026-10-22 · **System Type:** library · **Repo:** components/hydra

## Problem

Two deterministic decisions are sitting in the human triage queue that no human
judgment adds value to:

1. **Stale Pan filings linger when their premise clears.** `ajantha-srv-smart-engine-f1y5`
   ("spawn-failed wedge blocks 15") was correctly self-resolved 2026-10-16:
   *"Stale Pan filing: spawn wedge was the npm-401 wedge — root cause fixed
   2026-10-14; all 15 beads completed."* But its twin `ajantha-srv-smart-engine-c00v`
   ("3 beads blocked spawn failed", filed 2026-10-13, same wedge era) remains
   `in_progress` + `pan-filed` for 9+ days, eating one of 4 pan-filing slots,
   even though its premise is provably clear today: zero smart-engine spawn
   failures, chains completing, cohort empty. The stale-premise re-verification
   in the sweep does not cover in_progress-status filings whose blocked cohort
   has emptied.

2. **The `issues.jsonl` mirror lags `beads.db`.** `ajantha-srv-jobfile-7db` reads
   `in_progress` in the jsonl mirror but `CLOSED (not-needed) 2026-10-05` via
   `br show` (beads.db). Any reporting that parses the file directly inherits
   phantom beads and phantom "stuck" work.

## Verified Facts (measured 2026-10-22)

- Stale-premise re-verification lives in `internal/core/pan_health.go` (comment near line 541: "re-verified stale premise. Fail-open: a broken sink never blocks filing") (verify: `rg -n 'stale premise' internal/core/pan_health.go`).
- f1y5 auto-closure text quoted above is in the bead record (verify: `cd /home/ajantha/projects/srv-smart-engine && br show ajantha-srv-smart-engine-f1y5 | head -6`).
- c00v still open/in_progress with `pan-filed` label while smart-engine has zero spawn failures today and only 2 non-closed records (verify: `br show ajantha-srv-smart-engine-c00v`; `grep -c 'smart-engine.*spawn.*fail' /var/log/hydra/hydra-error.log` for 2026-10-22 = 0).
- 7db status divergence between `br show` (closed) and `issues.jsonl` (in_progress) reproduced 2026-10-22 (verify: `br show ajantha-srv-jobfile-7db | head -2` vs `grep 7db /home/ajantha/projects/srv-jobfile/.beads/issues.jsonl | tail -1`).
- Human-only filing class exists and must stay human: filings whose reason names branch rules, approvals, or permissions (verify: `rg -n 'campaign-anomaly' internal/pan/pan.go | head -3` — the never-automate contract).

## Assumptions

- Spawn-success/failure counters are already queryable by the sweep (Charon/preflight emit spawn_blocked and worker completion events); FR-001 may consume them read-only.

## Out of Scope

- Changing the pan-filing cap (4) itself.
- Any auto-close of filings that name human-only actions (branch rules, approvals, permissions, repo-content changes) — those remain human by contract.

---

### FR-001: Stale-filing sweep covers in-progress filings with cleared premises

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/pan_health.go`, `internal/core/pan_health_test.go`
**Test Command:** `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- The stale-premise re-verification evaluates `pan-filed` beads regardless of open/in_progress status when the filing's premise is a spawn-wedge/cohort claim.
- Premise-cleared probe (all three, all deterministic): (a) filing older than 48h; (b) the blocked cohort the filing names has zero members still blocked with that reason today; (c) the project recorded at least one successful spawn in the last 24h. All three true → filing closes with reason `stale pan filing: premise cleared (cohort empty, spawns healthy)` mirroring the f1y5 closure style.
- Guardrail: a filing whose reason text matches `/(branch protection|required approval|permissions|GH013|rules URL)/` is NEVER auto-closed by this path — it is left for human triage (existing contract, pan.go campaign-anomaly rules).
- Guardrail: at most one such auto-close per project per sweep tick (rate limit), with typed event `pan_filing_stale_closed` `{bead, project, cohort_named, cohort_now, last_spawn_ok}`.
- RED test: fixture filing with in_progress status + empty cohort + recent spawn success must be closed; same fixture with reason naming "branch protection" must NOT be closed; same fixture with cohort still blocked must NOT be closed.
- `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1` exits 0.

### FR-002: Beads mirror audit tool

**Type:** fr
**Priority:** P2
**System Type:** cli
**Target Files:** `tools/beads-mirror-audit/main.go`, `tools/beads-mirror-audit/main_test.go`
**Test Command:** `go test ./tools/beads-mirror-audit/ -count=1`
**Build Command:** `go build ./tools/beads-mirror-audit/`

**Acceptance Criteria:**
- `tools/beads-mirror-audit --store <dir>` compares every bead's status/id in `<dir>/issues.jsonl` against `br show` (or `br list --json`) for the same store and prints one line per divergence: `<bead-id> jsonl=<status> db=<status>`.
- Exit 0 with `mirror clean (N beads)` when no divergence; exit 1 with the divergence list otherwise (audit gate, fail-closed on drift by exit code, but never mutates either store).
- `--json` flag emits `{"divergences":[...],"checked":N}` for machine consumption; a future consumer is the Pan health digest, wiring is out of scope.
- Handles `br` absence or `br` failure by printing `audit unavailable: <err>` and exiting 2 (fail-open reporting, never blocks anything).
- Unit test uses a fake `br` double and fixture jsonl with one divergent record: asserts the divergence line, exit codes 0/1/2 paths.
- `go test ./tools/beads-mirror-audit/ -count=1` exits 0.
