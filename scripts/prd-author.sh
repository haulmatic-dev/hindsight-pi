#!/usr/bin/env bash
# prd-author.sh — author a PRD on the VPS using the strongest available model.
#
# PRD authoring is the highest-leverage decision in Hydra: the PRD is compiler
# input, and a vague PRD cannot be rescued by any downstream model. It runs
# ONCE per feature, so the strong model costs almost nothing in aggregate —
# unlike the hundreds of build beads, which stay on the cheap model by design.
#
# This is the VPS equivalent of authoring in Claude Code (which is not
# installed there). Explicitly user-initiated: Hydra itself never calls this.
#
# Usage:
#   scripts/prd-author.sh "<feature description>"           # K3, max effort
#   PRD_MODEL=k3 scripts/prd-author.sh "..."                # 1M context (2x quota)
#   PRD_EFFORT=high scripts/prd-author.sh "..."             # cheaper effort
#
# Defaults: k3-256k at max effort. k3-256k gives identical results to k3 within
# 256k of context at HALF the quota (per Kimi's model docs), and no PRD needs
# 1M context. Never set effort to none/off — the endpoint silently reroutes
# K3/K2.7 to the older K2.6 model when thinking is disabled.
set -euo pipefail

MODEL="${PRD_MODEL:-k3-256k}"
EFFORT="${PRD_EFFORT:-max}"
PROVIDER="${PRD_PROVIDER:-kimi-coding}"
SKILL_DIR="${PRD_SKILL_DIR:-/opt/dev-tools/config/pi/skills/create-prd}"

if [ $# -lt 1 ]; then
  cat >&2 <<USAGE
usage: prd-author.sh "<feature description>"
  env: PRD_MODEL (default k3-256k) PRD_EFFORT (default max)
       PRD_PROVIDER (default kimi-coding) PRD_SKILL_DIR
USAGE
  exit 2
fi

case "$(printf '%s' "$EFFORT" | tr '[:upper:]' '[:lower:]')" in
  none|off|disabled)
    echo "prd-author: refusing effort '$EFFORT' — disabling thinking reroutes the request to K2.6. Use low/high/max." >&2
    exit 2
    ;;
esac

[ -f "$SKILL_DIR/SKILL.md" ] || { echo "prd-author: create-prd skill not found at $SKILL_DIR" >&2; exit 2; }

echo "prd-author: model=$MODEL effort=$EFFORT skill=$SKILL_DIR" >&2

printf '%s\n\n%s\n' "$*" \
  "Author the PRD now following the create-prd skill in full, including doctrine.md and the mandatory Phase 3.5 adversarial review. Verify every code anchor against the current checkout before citing it. Save under docs/PRDs/." \
  | pi --no-session \
      --provider "$PROVIDER" \
      --model "$MODEL" \
      --thinking-level "$EFFORT" \
      --skill "$SKILL_DIR"
