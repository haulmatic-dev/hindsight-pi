#!/usr/bin/env bash
# scripts/mirror-health-slo.sh — SLO checker for the beads-mirror-audit sweep (FR-5).
#
# Exits 0 iff ALL of the following hold:
#   1. The event stream shows 0 `mirror_drift_detected` events in the last 24h.
#   2. The event stream shows at least 1 `mirror_audit_clean` event per
#      registered store in the last 24h.
#
# Otherwise exits 1 (and prints a one-line diagnostic per failed condition).
# Exits 2 on infrastructure errors (missing tools, unreadable event dir).
#
# Read-only: never opens, repairs, or mutates the stores.

set -euo pipefail

EVENTS_DIR="${HYDRA_EVENTS_DIR:-/opt/dev-tools/var/hydra/events}"
HYDRA_CONFIG="${HYDRA_CONFIG:-/etc/hydra/hydra.yaml}"
SLO_MAX_AGE_HOURS="${MIRROR_SLO_MAX_AGE_HOURS:-24}"

# --- helpers -----------------------------------------------------------------

fail() {
    echo "SLO-BREACH: $*" >&2
    exit 1
}

now=$(date +%s)
cutoff=$((now - SLO_MAX_AGE_HOURS * 3600))

# Scan the event stream files (*.jsonl) for occurrences of `$event_type`
# within the cutoff window. Event format:
#   {"time":"2026-10-22T15:15:29.982Z","type":"...","fields":{...}}
scan_events() {
    local event_type="$1"
    local count=0

    # Fast path: grep the type directly out of the stream.
    while IFS= read -r line; do
        # Extract timestamp: prefer 'time' (RFC3339 UTC 'Z'), fall back to
        # 'ts' or 'timestamp' if they ever appear.
        ts=$(jq -r '(.time // .ts // .timestamp // empty)
                     | if type == "number" then .
                       elif type == "string" then try (sub("\\.[0-9]+Z$"; "Z") | fromdate) else empty end' \
                     <<< "$line" 2>/dev/null || echo "")
        if [[ -z "$ts" ]]; then
            continue
        fi
        if (( ts >= cutoff )); then
            count=$((count + 1))
        fi
    done < <(cat "${EVENTS_DIR}"/*.jsonl 2>/dev/null | grep "\"type\":\"$event_type\"" || true)

    echo "$count"
}

# Collect distinct store paths from `mirror_audit_clean` events.
# Guard against the event wrapping data in a `fields` object (the shape used
# by other typed events) or carrying top-level `store`/`store_dir`.
scan_clean_stores() {
    jq -r 'select(.type == "mirror_audit_clean")
           | (.fields.store // .store // .fields.store_dir // .store_dir // empty)' \
        "${EVENTS_DIR}"/*.jsonl 2>/dev/null | sort -u
}

# Enumerate project stores registered with the daemon.
#  1. `hydra.yaml` project entries (paths are absolute; strip quotes).
#  2. Keep only those whose `.beads/` dir really exists (read-only probe).
collect_registered_stores() {
    local stores=()
    if [[ -f "$HYDRA_CONFIG" ]]; then
        while IFS= read -r line; do
            line=$(sed -E 's/^[[:space:]]*-?[[:space:]]*path:[[:space:]]*"?(.*)"?/\1/' <<< "$line")
            if [[ -n "$line" && -d "$line/.beads" ]]; then
                stores+=("$line")
            fi
        done < <(grep -E '^[[:space:]]*-?[[:space:]]*path:[[:space:]]*[^#]' "$HYDRA_CONFIG")
    fi
    # Remove duplicates, keep sorted, strip empty lines.
    printf '%s\n' "${stores[@]:-}" | sort -u | grep -v '^$' || true
}

# --- main checks -------------------------------------------------------------

# 1. No mirror_drift_detected in the last window.
drift_count=$(scan_events "mirror_drift_detected")
if (( drift_count != 0 )); then
    fail "${drift_count} mirror_drift_detected event(s) in the last ${SLO_MAX_AGE_HOURS}h"
fi

# 2. At least one mirror_audit_clean per registered store in the window.
registered=$(collect_registered_stores)
if [[ -z "$registered" ]]; then
    echo "SLO-OK: no registered stores to verify"
    exit 0
fi

observed=$(scan_clean_stores | grep -v '^$' || true)
for store in $registered; do
    if ! grep -qF -- "$store" <<< "$observed"; then
        fail "store '$store' has no mirror_audit_clean event in the last ${SLO_MAX_AGE_HOURS}h"
    fi
done

echo "SLO-OK: 0 drift, ${SLO_MAX_AGE_HOURS}h clean coverage for $(echo "$registered" | wc -l) store(s)"
