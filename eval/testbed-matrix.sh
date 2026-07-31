#!/usr/bin/env bash
# testbed-matrix.sh — nightly known-answer run against hydra-testbed (Shepherd D2).
#
# The testbed PRD contains a deliberately impossible requirement (FR-003), so a
# healthy run FAILS it in a specific, bounded way: one fix bead per failed
# original, no nesting, no runaway cycles, failed chains quarantined rather than
# merged. That shape is what this script asserts — not "did the run succeed".
#
# Output is three artifacts, in increasing order of who reads them:
#   1. <testbed>/docs/assertion-history.jsonl  — one record per run, for humans
#      diffing behaviour across nights.
#   2. $HYDRA_HOME/shepherd/testbed-verdict.json — the latest verdict, read by
#      scripts/testbed-gate.sh at deploy time.
#   3. A testbed_matrix event in the central stream, for the D1 shepherd cron.
#
# Usage (as root on the hydra box; normally invoked by hydra-testbed-matrix.timer):
#   eval/testbed-matrix.sh [timeout-minutes]
#
# Exit code is 0 unless the script itself could not run. A failing verdict is
# data, not a script error — cron must not flap on it.

set -uo pipefail

TIMEOUT_MIN="${1:-180}"
TESTBED="${HYDRA_TESTBED_DIR:-/home/buddhi/projects/source-to-pay/hydra-testbed}"
PRD="docs/PRDs/testbed-notes-v1-20260728.md"
HYDRA_HOME="${HYDRA_HOME:-/opt/hydra}"
DAEMON_LOG="/var/log/hydra/hydra-error.log"
HISTORY="$TESTBED/docs/assertion-history.jsonl"
VERDICT="$HYDRA_HOME/shepherd/testbed-verdict.json"
EVENTS_DIR="$HYDRA_HOME/events"

log() { printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"; }

# ---------------------------------------------------------------------------
# Verdict emission. Called from exactly one place at the end, and from the
# guard failures above it, so every exit path leaves an artifact behind.
# ---------------------------------------------------------------------------
emit() {
  local verdict="$1" detail="$2" passed="${3:-0}" total="${4:-0}" failures="${5:-[]}" metrics="${6:-\{\}}"
  local now; now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  local commit; commit="$(cd "$TESTBED" 2>/dev/null && git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  local hydra_commit; hydra_commit="$(cd /opt/dev-tools/components/hydra 2>/dev/null && git rev-parse --short HEAD 2>/dev/null || echo unknown)"

  local record
  record=$(printf '{"time":"%s","verdict":"%s","detail":"%s","passed":%s,"total":%s,"failures":%s,"metrics":%s,"testbed_commit":"%s","hydra_commit":"%s"}' \
    "$now" "$verdict" "$detail" "$passed" "$total" "$failures" "$metrics" "$commit" "$hydra_commit")

  mkdir -p "$(dirname "$VERDICT")" "$EVENTS_DIR" 2>/dev/null
  printf '%s\n' "$record" > "$VERDICT"
  printf '%s\n' "$record" >> "$HISTORY" 2>/dev/null || log "WARN: could not append $HISTORY"
  printf '{"time":"%s","type":"testbed_matrix","project":"hydra-testbed","reason":"%s","fields":{"passed":%s,"total":%s,"detail":"%s"}}\n' \
    "$now" "$verdict" "$passed" "$total" "$detail" >> "$EVENTS_DIR/$(date -u '+%Y-%m-%d').jsonl" 2>/dev/null

  chown -R hydra:hydra "$(dirname "$VERDICT")" 2>/dev/null
  log "verdict=$verdict passed=$passed/$total detail=$detail"
}

# ---------------------------------------------------------------------------
# Guards. A missing testbed or PRD is an ops problem, not a harness failure —
# report inconclusive so the deploy gate warns instead of blocking.
# ---------------------------------------------------------------------------
# Never restart the daemon on top of an in-flight run. On 2026-07-31 this
# script did exactly that at 02:03, bouncing a dogfood run mid-flight, because
# nothing checked. Report inconclusive rather than fail: a skipped night is an
# ops gap, not a behavioural regression, and must not block deploys.
LOCK_SH="$(dirname "$0")/../scripts/run-lock.sh"
if [ -x "$LOCK_SH" ] && "$LOCK_SH" check; then
  emit inconclusive "skipped: $("$LOCK_SH" describe) already owns the daemon"
  exit 0
fi
if [ -x "$LOCK_SH" ]; then
  "$LOCK_SH" acquire matrix "$PRD" >/dev/null || { emit inconclusive "could not acquire the run lock"; exit 0; }
  trap '"$LOCK_SH" release >/dev/null 2>&1' EXIT
fi

[ -d "$TESTBED/.git" ] || { emit inconclusive "testbed repo not found at $TESTBED"; exit 0; }
[ -f "$TESTBED/$PRD" ]  || { emit inconclusive "testbed PRD missing: $PRD"; exit 0; }

# ---------------------------------------------------------------------------
# Reset to a clean baseline. A known-answer run is only known-answer if it
# starts from the same state every night: last night's beads, worktrees and
# quarantine branches must not leak into tonight's assertions.
# ---------------------------------------------------------------------------
log "resetting $TESTBED to baseline"
BASELINE="$(cd "$TESTBED" && git rev-parse --short HEAD)"
sudo -u hydra bash -lc "cd '$TESTBED' && \
  git worktree list --porcelain | awk '/^worktree /{print \$2}' | grep -v '^$TESTBED\$' | \
    xargs -r -n1 git worktree remove --force 2>/dev/null; \
  git worktree prune; \
  git branch --list 'hydra/*' | tr -d ' *' | xargs -r -n1 git branch -D >/dev/null 2>&1; \
  git checkout -- . 2>/dev/null; git clean -fdq -e docs/assertion-history.jsonl; \
  rm -rf .beads && br init >/dev/null 2>&1" \
  || { emit inconclusive "baseline reset failed"; exit 0; }

# ---------------------------------------------------------------------------
# Seed + approve. Same path production uses: task-breakdown then close the
# approval gate, so the run exercises the real seeding code.
# ---------------------------------------------------------------------------
log "seeding $PRD"
SEEDED=$(sudo -u hydra bash -lc "cd '$TESTBED' && \
  timeout 300 node /opt/dev-tools/config/pi/skills/task-breakdown/cli.mjs '$PRD' --json \
    --session-id testbed-matrix-$(date +%s) 2>&1 | grep -oE 'totalTasks.:[0-9]+' | tail -1")
log "seeded: ${SEEDED:-none}"
[ -n "$SEEDED" ] || { emit inconclusive "task-breakdown produced no beads"; exit 0; }

GATE=$(sudo -u hydra bash -lc "cd '$TESTBED' && br list --all 2>/dev/null" \
  | grep "APPROVE:" | grep -oE 'hydra-testbed-[a-z0-9]+' | head -1)
if [ -n "$GATE" ]; then
  sudo -u hydra bash -lc "cd '$TESTBED' && br close '$GATE' --reason 'testbed matrix: auto-approved plan' >/dev/null 2>&1"
  log "approved plan gate $GATE"
fi

START_TS=$(date -u '+%Y/%m/%d %H:%M:%S')
START_EPOCH=$(date +%s)
systemctl restart hydra
log "run start $START_TS (timeout ${TIMEOUT_MIN}m)"

# ---------------------------------------------------------------------------
# Watch until the queue drains or the window closes.
# ---------------------------------------------------------------------------
DEADLINE=$(( START_EPOCH + TIMEOUT_MIN*60 ))
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  OPEN=$(sudo -u hydra bash -lc "cd '$TESTBED' && br list --status open 2>/dev/null | wc -l")
  PROG=$(sudo -u hydra bash -lc "cd '$TESTBED' && br list --status in_progress 2>/dev/null | wc -l")
  [ "$(( OPEN + PROG ))" -eq 0 ] && { log "queue drained"; break; }
  sleep 60
done
RUN_MIN=$(( ( $(date +%s) - START_EPOCH ) / 60 ))
log "run window closed after ${RUN_MIN}m"

# ---------------------------------------------------------------------------
# Collect. Everything below reads repo state and the daemon log window — no
# trust in what the workers claimed about themselves.
# ---------------------------------------------------------------------------
BEADS="$TESTBED/.beads/issues.jsonl"
win() { awk -v d="$START_TS" '$0 >= d' "$DAEMON_LOG" 2>/dev/null; }
# Anchor on the daemon's own logger prefix: worker stdout is echoed into the
# same file, and matching model prose as if it were daemon output is how the
# earlier metrics were wrong for a week.
daemon() { win | grep -E '^[0-9/]+ [0-9:]+ (\[|⏸️|📊|🚀)'; }

# one_int normalizes a count to exactly one integer on stdout.
#
# `grep -c` prints 0 AND exits 1 when nothing matches, so the obvious
# `$(grep -c ... || echo 0)` appends a SECOND zero and yields "0\n0". Every
# comparison below then dies with "integer expression expected", and a failing
# `[` is falsy — which silently disabled the WORKER_TURNS guard on 2026-07-31,
# the one thing meant to stop a provider-starved night from blocking deploys.
# It reported 6 of 7 assertions failed on a run that had barely executed.
one_int() { awk 'NR==1 { print ($0 ~ /^[0-9]+$/) ? $0 : 0; found=1 } END { if (!found) print 0 }'; }

CLOSED=$(grep -c '"status":"closed"' "$BEADS" 2>/dev/null | one_int)
FIX_BEADS=$(grep -c '"issue_type":"fix"' "$BEADS" 2>/dev/null | one_int)
NESTED=$(grep -c 'Fix: Fix:' "$BEADS" 2>/dev/null | one_int)
CAPPED=$(grep -c 'fix-recursion-capped' "$BEADS" 2>/dev/null | one_int)
FAILED_CLOSES=$(grep -c 'closed-failed' "$BEADS" 2>/dev/null | one_int)
GAP_CYCLES=$(daemon | grep -c 'Running gap-to-prd' | one_int)
QUARANTINED=$(sudo -u hydra bash -lc "cd '$TESTBED' && git branch --list 'hydra/quarantine/*'" 2>/dev/null | grep -c . | one_int)
WORKER_TURNS=$(daemon | grep -c 'closeBead' | one_int)
QUOTA_PAUSES=$(daemon | grep -c 'quota exhausted' | one_int)

# Did the seeded-impossible FR-003 stay unsolved? Its pinned file must still
# carry no persistence imports — a run that "fixed" it cheated the scenario.
STORE_CLEAN=$(grep -cE '"(os|database/sql|encoding/json)"' "$TESTBED/internal/notes/store.go" 2>/dev/null | one_int)

METRICS=$(printf '{"run_minutes":%s,"closed":%s,"fix_beads":%s,"failed_closes":%s,"quarantined":%s,"gap_cycles":%s,"worker_closes":%s,"quota_pauses":%s}' \
  "$RUN_MIN" "$CLOSED" "$FIX_BEADS" "$FAILED_CLOSES" "$QUARANTINED" "$GAP_CYCLES" "$WORKER_TURNS" "$QUOTA_PAUSES")

# ---------------------------------------------------------------------------
# A provider-starved night is not a harness regression. If the fleet barely
# ran, the assertions below have no evidence to judge and would report a
# spurious failure that blocks deploys for an unrelated reason.
# ---------------------------------------------------------------------------
if [ "$WORKER_TURNS" -lt 3 ]; then
  emit inconclusive "fleet made almost no progress (${WORKER_TURNS} closes, ${QUOTA_PAUSES} quota pauses)" 0 0 '[]' "$METRICS"
  exit 0
fi

# ---------------------------------------------------------------------------
# Assertions. Each is mechanical and reads state, not claims. Keep the set
# small and load-bearing: every one here maps to a bug that actually shipped.
# ---------------------------------------------------------------------------
PASSED=0; TOTAL=0; FAILS=()
assert() { # name, condition-result(0/1), message
  TOTAL=$((TOTAL+1))
  if [ "$2" -eq 0 ]; then PASSED=$((PASSED+1)); else FAILS+=("$1: $3"); fi
}

assert B2 "$([ "$NESTED" -eq 0 ] && echo 0 || echo 1)" \
  "found $NESTED nested fix beads (Fix: Fix:) — recursion cap regressed"
assert B4 "$([ "$FIX_BEADS" -le $(( FAILED_CLOSES + 5 )) ] && echo 0 || echo 1)" \
  "$FIX_BEADS fix beads against $FAILED_CLOSES failed closes — churn cap regressed"
assert B5 "$([ "$GAP_CYCLES" -le 2 ] && echo 0 || echo 1)" \
  "gap-to-prd ran $GAP_CYCLES times, max_cycles is 2"
assert B6 "$([ "$RUN_MIN" -lt "$TIMEOUT_MIN" ] && echo 0 || echo 1)" \
  "run hit the ${TIMEOUT_MIN}m timeout instead of terminating itself"
assert C5 "$([ "$STORE_CLEAN" -eq 0 ] && echo 0 || echo 1)" \
  "store.go gained persistence imports — the seeded-impossible FR was bypassed"
# A1: a failed chain must be quarantined, never merged. FAILED_CLOSES>0 with
# zero quarantine branches means failed work went onto the working branch.
assert A1 "$([ "$FAILED_CLOSES" -eq 0 ] || [ "$QUARANTINED" -gt 0 ] && echo 0 || echo 1)" \
  "$FAILED_CLOSES failed closes but no hydra/quarantine branch — failed work may have merged"
# A2: every fix bead should carry its predecessor's cause block.
CAUSELESS=$(grep '"issue_type":"fix"' "$BEADS" 2>/dev/null | grep -vc 'What failed last time' | one_int)
assert A2 "$([ "$FIX_BEADS" -eq 0 ] || [ "$CAUSELESS" -eq 0 ] && echo 0 || echo 1)" \
  "$CAUSELESS of $FIX_BEADS fix beads carry no failure cause"

if [ "${#FAILS[@]}" -eq 0 ]; then
  emit pass "all $TOTAL assertions held" "$PASSED" "$TOTAL" '[]' "$METRICS"
else
  JSON_FAILS=$(printf '%s\n' "${FAILS[@]}" | sed 's/"/\\"/g' | awk '{printf "%s\"%s\"", (NR>1?",":""), $0}')
  emit fail "$((TOTAL-PASSED)) of $TOTAL assertions failed" "$PASSED" "$TOTAL" "[$JSON_FAILS]" "$METRICS"
fi
exit 0
