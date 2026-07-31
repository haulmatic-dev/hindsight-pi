#!/usr/bin/env bash
# run-lock.sh — declare that a run owns the daemon, so nothing bounces it mid-flight.
#
# A restart is not a free operation on this system. It kills every in-flight
# worker, and the work they were doing is redone from scratch by the next
# attempt. On 2026-07-30 more than ten daemon restarts (deploys, config edits,
# knob tuning) coincided with a dogfood run closing 33 beads in 21 hours where a
# comparable run had closed 36 in 4 — and D2's nightly matrix restarted the
# daemon at 02:03 directly on top of that in-flight run, because nothing checked.
#
# The lock is advisory and self-healing: it records the owning PID, and any
# reader treats a lock whose PID is gone as stale. That matters because the
# failure mode of a sticky lock is a system that refuses to ever deploy again.
#
# Usage:
#   run-lock.sh acquire <kind> <detail>   # kind: dogfood | matrix
#   run-lock.sh release
#   run-lock.sh check                     # exit 0 = a run is active, 1 = free
#   run-lock.sh describe                  # human-readable one-liner
#
# Intended shape in a caller:
#   scripts/run-lock.sh acquire dogfood "$PRD" || exit 0
#   trap 'scripts/run-lock.sh release' EXIT

set -uo pipefail

HYDRA_HOME="${HYDRA_HOME:-/opt/hydra}"
LOCK="${HYDRA_RUN_LOCK:-$HYDRA_HOME/run.lock}"
ACTION="${1:-check}"
# The owner is the CALLER, not this script: $$ here is run-lock.sh's own PID,
# which dies the instant acquire returns — recording it made every lock read as
# stale immediately. Callers may override explicitly; default to the parent.
OWNER_PID="${HYDRA_RUN_LOCK_PID:-$PPID}"

# is_stale reports whether the recorded owner is gone. A lock left behind by a
# killed run must never block the next one.
is_stale() {
	[ -f "$LOCK" ] || return 0
	local pid
	pid="$(sed -n 's/^pid=//p' "$LOCK" 2>/dev/null | head -1)"
	[ -n "$pid" ] || return 0
	kill -0 "$pid" 2>/dev/null && return 1 || return 0
}

case "$ACTION" in
acquire)
	KIND="${2:?kind required (dogfood|matrix)}"
	DETAIL="${3:-}"
	if [ -f "$LOCK" ] && ! is_stale; then
		echo "run-lock: a $(sed -n 's/^kind=//p' "$LOCK" | head -1) run is already active — refusing to start"
		sed 's/^/  /' "$LOCK"
		exit 1
	fi
	if [ -f "$LOCK" ]; then
		echo "run-lock: clearing a stale lock (owner PID is gone)"
	fi
	mkdir -p "$(dirname "$LOCK")" 2>/dev/null
	{
		printf 'pid=%s\n' "$OWNER_PID"
		printf 'kind=%s\n' "$KIND"
		printf 'detail=%s\n' "$DETAIL"
		printf 'started_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	} >"$LOCK"
	chown hydra:hydra "$LOCK" 2>/dev/null
	echo "run-lock: acquired for $KIND (owner pid $OWNER_PID)"
	;;
release)
	# Only the owner clears it, so a concurrent run's lock is never stolen.
	if [ -f "$LOCK" ]; then
		owner="$(sed -n 's/^pid=//p' "$LOCK" | head -1)"
		if [ "$owner" = "$OWNER_PID" ] || is_stale; then
			rm -f "$LOCK"
			echo "run-lock: released"
		else
			echo "run-lock: not the owner (lock held by pid $owner) — leaving it alone"
		fi
	fi
	;;
check)
	if [ -f "$LOCK" ] && ! is_stale; then
		exit 0
	fi
	exit 1
	;;
describe)
	if [ -f "$LOCK" ] && ! is_stale; then
		printf '%s run started %s (%s)\n' \
			"$(sed -n 's/^kind=//p' "$LOCK" | head -1)" \
			"$(sed -n 's/^started_at=//p' "$LOCK" | head -1)" \
			"$(sed -n 's/^detail=//p' "$LOCK" | head -1)"
	else
		echo "no active run"
	fi
	;;
*)
	echo "usage: run-lock.sh acquire <kind> <detail> | release | check | describe" >&2
	exit 2
	;;
esac
