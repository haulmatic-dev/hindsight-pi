#!/usr/bin/env bash
# escape-rate-weekly.sh — NFR-001 escape-rate regression budget, weekly query.
#
# Escapes: beads that reached closed-completed while their declared
# verification command (metadata test_command) fails. The daemon-side
# PostCloseSweeper flags them with the durable `postclose-escape` label and a
# typed `postclose_escape` event; this query turns that into the weekly
# escape rate over the store and event stream.
#
# Usage:
#   escape-rate-weekly.sh <project-path> [days]     # store query (br)
#   escape-rate-weekly.sh --events <events-dir>     # event-stream query
#
# Output: human summary + machine-readable lines.

set -euo pipefail

mode="${1:-}"
if [ "$mode" = "--events" ]; then
    dir="${2:?events dir required}"
    escaped=0
    [ -d "$dir" ] && escaped=$(cat "$dir"/*.jsonl 2>/dev/null | grep -c '"postclose_escape"' || true)
    printf 'event-stream escapes: %d (all-time window in %s)\n' "$escaped" "$dir"
    exit 0
fi

project="$1"
days="${2:-7}"
since="$(date -u -d "-${days} days" +%Y-%m-%d)"

cd "$project"

rows="$(br list --status closed --json 2>/dev/null | jq -r --arg since "$since" '
    .issues[]
    | select((.closed_at // "") >= $since or (.updated_at // "") >= $since)
    | "\(.id)\t\(.labels | join(","))"' 2>/dev/null || true)"

total=0
escaped=0
while IFS=$'\t' read -r id labels; do
    [ -n "$id" ] || continue
    total=$((total + 1))
    case ",$labels," in
        *,postclose-escape,*) escaped=$((escaped + 1)) ;;
    esac
done <<<"$rows"

printf 'escape-rate window: last %s days (since %s UTC)\n' "$days" "$since"
printf 'closed-completed beads in window: %s\n' "$total"
printf 'verification escapes (postclose-escape): %s\n' "$escaped"
if [ "$total" -gt 0 ]; then
    awk -v e="$escaped" -v t="$total" 'BEGIN { printf "escape rate: %.1f%%\n", 100*e/t }'
else
    echo "escape rate: n/a (no closed beads in window)"
fi
