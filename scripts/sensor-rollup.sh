#!/usr/bin/env bash
# sensor-rollup — daily per-sensor fire/resolve rates from the typed event
# stream ($HYDRA_HOME/events/<date>.jsonl, or a file list as arguments).
#
# Sensors are symmetric: each fire type pairs with a resolve type under a
# shared correlation_id field. The rollup reports fires, resolves, and the
# resolve rate per sensor — the fine-tuning evidence (e.g. judge max_rounds
# false-positive rate, orphan recovery rate) that was previously unmeasurable.
#
# Usage:
#   sensor-rollup.sh                     # today's central stream
#   sensor-rollup.sh /path/to/events/2026-10-02.jsonl
#
# Exit 0 always (fail-open; observability never gates the pipeline).
set -u

# fire type -> resolve type
declare -A RESOLVE=(
  [verified_close_rejected]=verified_close_passed
  [charon_orphan_reopened]=orphan_recovered
  [worktree_quarantined]=quarantine_restored
  [publish_rebase_attempted]=publish_succeeded
  [aimd_scale_down]=aimd_scale_up
  [repair_failed]=repair_succeeded
)

files=()
if [ $# -gt 0 ]; then files=("$@")
elif [ -n "${HYDRA_HOME:-}" ] && [ -d "$HYDRA_HOME/events" ]; then files=("$HYDRA_HOME"/events/*.jsonl)
else files=(./events/*.jsonl); fi

for fire in "${!RESOLVE[@]}"; do
  resolve="${RESOLVE[$fire]}"
  # Correlation IDs seen on each side (sets, deduped).
  fire_ids=$(jq -r --arg t "$fire" 'select(.type==$t) | .fields.correlation_id // empty' "${files[@]}" 2>/dev/null | sort -u | grep -c . )
  resolve_ids=$(jq -r --arg t "$resolve" 'select(.type==$t) | .fields.correlation_id // empty' "${files[@]}" 2>/dev/null | sort -u | grep -c .)
  fires=$(jq -r --arg t "$fire" 'select(.type==$t) | 1' "${files[@]}" 2>/dev/null | grep -c .)
  resolves=$(jq -r --arg t "$resolve" 'select(.type==$t) | 1' "${files[@]}" 2>/dev/null | grep -c .)
  if [ "${fires:-0}" = 0 ] && [ "${fire_ids:-0}" = 0 ]; then continue; fi
  rate="n/a"
  if [ "${fire_ids:-0}" -gt 0 ] 2>/dev/null; then rate="$(awk "BEGIN{printf \"%.1f%%\", 100*${resolve_ids}/(0+${fire_ids})}")"; fi
  printf '%-28s fires=%-6s resolves=%-6s paired=%-6s/%-6s rate=%s\n' \
    "$fire/$resolve" "$fires" "$resolves" "$resolve_ids" "$fire_ids" "$rate"
done
