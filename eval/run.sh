#!/usr/bin/env bash
# run.sh — one-command scored dogfood run (VPS-side).
#
# Clones hydra, seeds a PRD via task-breakdown, approves the plan gate,
# waits for the run to finish, then scores it with tools/hydra-eval scoped
# to this run's start timestamp. The metrics JSON is the artifact.
#
# Usage (as root on the hydra box):
#   eval/run.sh <prd-path-in-repo> [work-branch] [timeout-minutes]
#     prd-path-in-repo  e.g. docs/PRDs/foo-20260717.md (must exist on BRANCH)
#     work-branch       branch to clone (default: development)
#     timeout-minutes   watch timeout (default: 240)
#
# Assumptions (documented, not automated):
#   - target dir $TARGET is registered in the hydra user-config discovery
#     (~buddhi/.hydra/config.yaml) as name hydra-dogfood, type single
#   - daemon reads /etc/hydra/hydra.yaml; hydra-eval builds from the clone
set -euo pipefail

PRD="${1:?prd path required}"
BRANCH="${2:-development}"
TIMEOUT_MIN="${3:-240}"
TARGET="/home/buddhi/projects/source-to-pay/hydra-dogfood"
REPO="https://github.com/haulmatic-dev/hydra.git"
DAEMON_LOG="/var/log/hydra/hydra-error.log"

echo "== clone $BRANCH =="
rm -rf "$TARGET"
git clone --quiet --branch "$BRANCH" "$REPO" "$TARGET"
cd "$TARGET"
git checkout -q -b "eval-run-$(date +%Y%m%d-%H%M%S)"
chown -R hydra:beads "$TARGET"
sudo -u hydra git config --global --add safe.directory "$TARGET" 2>/dev/null || true
git config --global --add safe.directory "$TARGET" 2>/dev/null || true

[ -f "$TARGET/$PRD" ] || { echo "PRD $PRD not found on $BRANCH" >&2; exit 1; }

echo "== seed =="
sudo -u hydra bash -lc "cd $TARGET && br init >/dev/null 2>&1 && \
  timeout 180 node /opt/dev-tools/config/pi/skills/task-breakdown/cli.mjs '$PRD' --json --session-id eval-run-$(date +%s) 2>&1 | grep -oE 'totalTasks.:[0-9]+' | tail -1"

GATE=""
for i in 1 2 3 4 5; do
  GATE=$(sudo -u hydra bash -lc "cd $TARGET && br list --all 2>/dev/null" | grep "APPROVE:" | grep -oE "hydra-dogfood-[a-z0-9]+" | head -1)
  [ -n "$GATE" ] && break
  sleep 5
done
[ -n "$GATE" ] || { echo "no approval gate found after retries" >&2; exit 1; }
sudo -u hydra bash -lc "cd $TARGET && br close $GATE -r approved-eval-run --force" >/dev/null 2>&1 || true
echo "gate $GATE approved"

START_TS=$(date -u "+%Y/%m/%d %H:%M:%S")
echo "== run start $START_TS (restarting daemon) =="
systemctl restart hydra

echo "== watch (timeout ${TIMEOUT_MIN}m) =="
DEADLINE=$(( $(date +%s) + TIMEOUT_MIN*60 ))
while :; do
  sleep 60
  OPEN=$(sudo -u hydra bash -lc "cd $TARGET && br list 2>/dev/null | grep -cE '◐|○'" || echo "?")
  echo "$(date -u +%H:%M:%S) open+inprog=$OPEN"
  [ "$OPEN" = "0" ] && break
  [ "$(date +%s)" -ge "$DEADLINE" ] && { echo "TIMEOUT after ${TIMEOUT_MIN}m"; break; }
done

echo "== score =="
OUT="/tmp/eval-run-$(date +%Y%m%d-%H%M%S).json"
sudo -u hydra bash -lc "cd $TARGET && go build -o /tmp/hydra-eval ./tools/hydra-eval"
/tmp/hydra-eval --beads-jsonl "$TARGET/.beads/issues.jsonl" \
  --daemon-log "$DAEMON_LOG" --since "$START_TS" --out "$OUT" || \
  /tmp/hydra-eval --beads-jsonl "$TARGET/.beads/issues.jsonl" \
    --daemon-log "$DAEMON_LOG" --out "$OUT"   # fallback: --since not built yet
echo "metrics: $OUT"
