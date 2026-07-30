#!/usr/bin/env bash
# testbed-gate.sh — deploy-time check against the nightly testbed verdict (D2).
#
# Reads the verdict written by eval/testbed-matrix.sh and decides whether a
# deploy should proceed. The rules encode which failures are the deploy's
# problem and which are not:
#
#   pass          → allow.
#   fail          → block. The nightly known-answer run detected a behavioural
#                   regression; shipping on top of it compounds the problem.
#   inconclusive  → warn, allow. Usually a provider-starved night or an ops
#                   gap in the testbed itself; blocking would punish the deploy
#                   for something it cannot fix.
#   stale/missing → warn, allow. Absence of evidence is not evidence of a
#                   regression, and a hard block here would make the very first
#                   deploy after installing this gate impossible.
#
# Override with HYDRA_SKIP_TESTBED_GATE=1 (mirrors HYDRA_SKIP_PREPUSH).
#
# Exit 0 = proceed, exit 1 = blocked.

set -uo pipefail

HYDRA_HOME="${HYDRA_HOME:-/opt/hydra}"
VERDICT="${HYDRA_TESTBED_VERDICT:-$HYDRA_HOME/shepherd/testbed-verdict.json}"
MAX_AGE_HOURS="${HYDRA_TESTBED_MAX_AGE_HOURS:-36}"

if [ "${HYDRA_SKIP_TESTBED_GATE:-0}" = "1" ]; then
  echo "testbed-gate: skipped (HYDRA_SKIP_TESTBED_GATE=1)"
  exit 0
fi

if [ ! -f "$VERDICT" ]; then
  echo "testbed-gate: WARN no verdict at $VERDICT — nightly matrix has not run yet; proceeding"
  exit 0
fi

field() { grep -oE "\"$1\":\"?[^,\"}]*\"?" "$VERDICT" | head -1 | cut -d: -f2- | tr -d '"'; }

RESULT="$(field verdict)"
DETAIL="$(field detail)"
WHEN="$(field time)"
PASSED="$(field passed)"
TOTAL="$(field total)"

AGE_HOURS=999
if [ -n "$WHEN" ]; then
  WHEN_EPOCH="$(date -u -d "$WHEN" +%s 2>/dev/null || echo 0)"
  [ "$WHEN_EPOCH" -gt 0 ] && AGE_HOURS=$(( ( $(date +%s) - WHEN_EPOCH ) / 3600 ))
fi

echo "testbed-gate: verdict=$RESULT (${PASSED:-?}/${TOTAL:-?} assertions, ${AGE_HOURS}h old) — $DETAIL"

if [ "$AGE_HOURS" -gt "$MAX_AGE_HOURS" ]; then
  echo "testbed-gate: WARN verdict is older than ${MAX_AGE_HOURS}h — treating as no evidence; proceeding"
  exit 0
fi

case "$RESULT" in
  pass)
    echo "testbed-gate: OK"
    exit 0
    ;;
  fail)
    echo "testbed-gate: BLOCKED — the nightly known-answer run found a regression:"
    grep -oE '"failures":\[[^]]*\]' "$VERDICT" | sed 's/"failures":\[//;s/\]$//;s/","/\n  /g;s/^"/  /;s/"$//'
    echo "testbed-gate: fix the regression, or override with HYDRA_SKIP_TESTBED_GATE=1 if you accept it"
    exit 1
    ;;
  *)
    echo "testbed-gate: WARN verdict is '$RESULT' — no regression evidence either way; proceeding"
    exit 0
    ;;
esac
