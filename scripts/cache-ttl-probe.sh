#!/usr/bin/env bash
# cache-ttl-probe.sh — measure whether the coding endpoint honors the
# Anthropic-style prompt-cache ttl:1h marker that PI_CACHE_RETENTION=long
# makes pi-ai send (pi-ai/dist/api/anthropic-messages.js).
#
# Background (verified 2026-09-06): with PI_CACHE_RETENTION=long, pi-ai marks
# cache blocks cache_control {type:ephemeral, ttl:"1h"} instead of the
# default 5m TTL. Kimi ignores markers for cache DECISIONS; whether it honors
# the ttl for cache EXPIRY is UNMEASURED — prior TTL measurements covered
# only [300s, 1800s) with default markers.
#
# What this does:
#   1. Warms a ~26K-token prefix (system + first user block both carry the
#      ttl:1h cache_control marker) and records cache_creation_input_tokens.
#   2. Probes cache_read at +35min and +65min by re-sending the identical
#      prefix with a tiny new user turn.
#   3. Prints HIT/MISS per probe.
#
# Usage:
#   KIMI_API_KEY=... ./scripts/cache-ttl-probe.sh [--model kimi-for-coding]
#     [--base-url https://api.kimi.com/anthropic] [--prefix-tokens 26000]
#     [--probe1 2100] [--probe2 3900]
#
#   probe1/probe2 are seconds after warm (default 35min/65min).
# Full run takes probe2 seconds + API latency — run under systemd-run or nohup
# on the box, not inside a worker turn.
#
# Exit codes: 0 = probe completed (regardless of HIT/MISS); 1 = setup error.

set -euo pipefail

MODEL="kimi-for-coding"
BASE_URL="${KIMI_BASE_URL:-https://api.kimi.com/anthropic}"
PREFIX_TOKENS=26000
PROBE1=2100   # 35 min
PROBE2=3900   # 65 min

while [[ $# -gt 0 ]]; do
	case "$1" in
		--model) MODEL="$2"; shift 2 ;;
		--base-url) BASE_URL="$2"; shift 2 ;;
		--prefix-tokens) PREFIX_TOKENS="$2"; shift 2 ;;
		--probe1) PROBE1="$2"; shift 2 ;;
		--probe2) PROBE2="$2"; shift 2 ;;
		*) echo "unknown arg: $1" >&2; exit 1 ;;
	esac
done

API_KEY="${KIMI_API_KEY:-${ANTHROPIC_API_KEY:-}}"
if [[ -z "$API_KEY" ]]; then
	echo "KIMI_API_KEY (or ANTHROPIC_API_KEY) required" >&2
	exit 1
fi

# ~4 chars/token rough English; build a prefix well above the provider's
# minimum cacheable block size with a unique nonce so the cache key cannot
# collide with anything else on the account.
NONCE="$(date +%s)-$$"
PAD_LEN=$((PREFIX_TOKENS * 4))
PAD="$(head -c "$PAD_LEN" /dev/zero | tr '\0' 'x')"
PREFIX_TEXT="cache-ttl-probe nonce=${NONCE}. ${PAD} end-of-pad."

# request <probe_index> : POST the shared prefix + a 1-line user turn.
# usage is printed as "fresh=<n> cache_read=<n> cache_create=<n>".
request() {
	local probe_index="$1"
	local body
	body=$(cat <<EOF
{
  "model": "${MODEL}",
  "max_tokens": 16,
  "system": [
    {"type": "text", "text": "${PREFIX_TEXT}",
     "cache_control": {"type": "ephemeral", "ttl": "1h"}}
  ],
  "messages": [
    {"role": "user", "content": [
      {"type": "text", "text": "probe ${probe_index} nonce=${NONCE}. Reply with the word ok."}
    ]}
  ]
}
EOF
)
	curl -fsS --max-time 300 "${BASE_URL}/v1/messages" \
		-H "x-api-key: ${API_KEY}" \
		-H "anthropic-version: 2023-06-01" \
		-H "content-type: application/json" \
		-d "$body"
}

usage_of() {
	echo "$1" | python3 -c '
import json,sys
u = json.load(sys.stdin).get("usage", {})
print(f"fresh={u.get(\"input_tokens\",0)} cache_read={u.get(\"cache_read_input_tokens\",0)} cache_create={u.get(\"cache_creation_input_tokens\",0)}")
'
}

echo "== cache-ttl-probe: model=${MODEL} base=${BASE_URL} prefix~${PREFIX_TOKENS}tok nonce=${NONCE}"
echo "-- warm (t=0)"
warm_json="$(request 0)"
echo "warm   $(usage_of "$warm_json")"

probe_at() {
	local label="$1" target="$2"
	local now elapsed
	now=$(date +%s)
	elapsed=$((now - T0))
	if (( elapsed < target )); then
		echo "-- ${label}: sleeping $((target - elapsed))s (until t=${target}s)"
		sleep $((target - elapsed))
	fi
	local json
	json="$(request "$label")"
	local line
	line="$(usage_of "$json")"
	local cache_read
	cache_read="$(echo "$line" | sed -n 's/.*cache_read=\([0-9]*\).*/\1/p')"
	if (( cache_read > 0 )); then
		echo "${label} HIT  ${line}"
	else
		echo "${label} MISS ${line}"
	fi
}

T0=$(date +%s)
probe_at probe1-35min "$PROBE1"
probe_at probe2-65min "$PROBE2"

echo "== done. Interpretation:"
echo "   probe1 HIT  -> ttl:1h marker honored past the default 5m TTL: PI_CACHE_RETENTION=long wins."
echo "   probe1 MISS -> provider expired the block before 35min; the env var stays (harmless) but do not rely on >5m cache."
echo "   probe2 result bounds the real retention window from above/below accordingly."
