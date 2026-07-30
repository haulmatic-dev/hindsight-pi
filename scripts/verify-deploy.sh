#!/usr/bin/env bash
# verify-deploy.sh — prove the RUNNING daemon and worker contain the change.
#
# A deploy can succeed and change nothing. systemd runs /opt/hydra/bin/hydra, a
# symlink into the repo's bin/, while `command -v hydra` resolves to
# /usr/local/bin/hydra — a path nothing executes. On 2026-07-30 a full day of
# deploys landed there: the AIMD governor, failed-chain quarantine and
# verdict-memory tracks all sat inert while being reported as live, and two
# absent signals (zero verdict_retained events, no quarantine branches) were
# rationalised as "nothing has triggered it yet" rather than checked.
#
# Three assertions, cheapest first:
#   1. The daemon's live exe resolves to the path we deployed to.
#   2. Both binaries are newer than the deploy started.
#   3. Optional: a caller-supplied marker string is present in the binary.
#
# Usage:
#   scripts/verify-deploy.sh [since-epoch] [marker-string] [marker-target]
#     since-epoch    binaries must be newer than this (default: 10 minutes ago)
#     marker-string   a string unique to the change, e.g. "concurrent request limit"
#     marker-target   worker|daemon|both  (default: both)
#
# Exit 0 = the running code is what we deployed. Exit 1 = it is not; the caller
# must treat the deploy as failed rather than assume success.

set -uo pipefail

SINCE="${1:-$(( $(date +%s) - 600 ))}"
MARKER="${2:-}"
MARKER_TARGET="${3:-both}"

HYDRA_HOME="${HYDRA_HOME:-/opt/hydra}"
DAEMON_LINK="$HYDRA_HOME/bin/hydra"
WORKER_LINK="$HYDRA_HOME/bin/worker"

fail=0
ok()   { printf '  ok   %s\n' "$*"; }
bad()  { printf '  FAIL %s\n' "$*"; fail=1; }

echo "verify-deploy: asserting the running code is the deployed code"

# --- 1. Is the daemon executing the binary we think we deployed? -------------
pid="$(systemctl show hydra -p MainPID --value 2>/dev/null || echo 0)"
if [ -z "$pid" ] || [ "$pid" = "0" ]; then
  bad "hydra is not running (MainPID=0) — nothing to verify"
else
  live_exe="$(readlink -f "/proc/$pid/exe" 2>/dev/null || echo unknown)"
  want_exe="$(readlink -f "$DAEMON_LINK" 2>/dev/null || echo missing)"
  if [ "$live_exe" = "$want_exe" ]; then
    ok "daemon exe is $live_exe"
  else
    bad "daemon is running $live_exe but the deploy target is $want_exe"
  fi
  # A daemon started before the binary was replaced is running the old code
  # from an unlinked inode — the file on disk looks fresh and the process is
  # stale, which is the failure this catches.
  started="$(date -u -d "$(systemctl show hydra -p ActiveEnterTimestamp --value)" +%s 2>/dev/null || echo 0)"
  if [ "$started" -gt 0 ] && [ "$started" -lt "$SINCE" ]; then
    bad "daemon started $(( (SINCE - started) / 60 ))m before this deploy — it did not pick up the new binary (restart it)"
  else
    ok "daemon restarted after the deploy"
  fi
fi

# --- 2. Are the binaries actually new? --------------------------------------
for pair in "daemon:$DAEMON_LINK" "worker:$WORKER_LINK"; do
  name="${pair%%:*}"; path="${pair#*:}"
  if [ ! -e "$path" ]; then
    bad "$name binary missing at $path"
    continue
  fi
  mtime="$(stat -Lc %Y "$path" 2>/dev/null || echo 0)"
  if [ "$mtime" -ge "$SINCE" ]; then
    ok "$name binary is fresh ($(date -u -d "@$mtime" '+%H:%M:%S'))"
  else
    bad "$name binary at $path is stale ($(date -u -d "@$mtime" '+%Y-%m-%d %H:%M:%S')) — the install went somewhere else"
  fi
done

# --- 3. Does the binary contain the change? ---------------------------------
# The cheapest possible proof that specific code is on the box. Skipped when no
# marker is supplied, because a generic freshness check cannot tell a rebuilt
# binary from a rebuilt-with-the-fix binary.
if [ -n "$MARKER" ]; then
  # grep -c, never grep -q: with `set -o pipefail`, grep -q exits on the first
  # match, `strings` dies of SIGPIPE, and the pipeline reports failure for a
  # marker that IS present. Counting reads the whole stream, so the exit status
  # reflects the match rather than the plumbing.
  marker_hits() {
    strings "$1" 2>/dev/null | grep -cF -- "$MARKER" || true
  }
  check_marker() {
    local name="$1" path="$2"
    local resolved; resolved="$(readlink -f "$path" 2>/dev/null)"
    [ -n "$resolved" ] || { bad "$name binary unresolvable for marker check"; return; }
    if [ "$(marker_hits "$resolved")" -gt 0 ]; then
      ok "$name binary contains marker: $MARKER"
    else
      bad "$name binary does NOT contain marker: $MARKER"
    fi
  }
  case "$MARKER_TARGET" in
    worker) check_marker worker "$WORKER_LINK" ;;
    daemon) check_marker daemon "$DAEMON_LINK" ;;
    *)      # Present in either binary is enough: most changes live in exactly one.
            d_hits="$(marker_hits "$(readlink -f "$DAEMON_LINK")")"
            w_hits="$(marker_hits "$(readlink -f "$WORKER_LINK")")"
            if [ "$d_hits" -gt 0 ] || [ "$w_hits" -gt 0 ]; then
              ok "marker present (daemon=$d_hits worker=$w_hits): $MARKER"
            else
              bad "marker absent from BOTH binaries: $MARKER"
            fi ;;
  esac
else
  echo "  note no marker supplied — freshness only, cannot prove the change itself is present"
fi

if [ "$fail" -ne 0 ]; then
  echo "verify-deploy: FAILED — treat this deploy as not applied"
  exit 1
fi
echo "verify-deploy: OK"
exit 0
