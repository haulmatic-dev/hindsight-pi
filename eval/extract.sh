#!/usr/bin/env bash
# extract.sh — pull a scope-clean patch out of a hydra dogfood run.
#
# After a run, the implemented work sits UNCOMMITTED in the target clone's
# shared checkout (chain integration is not yet reliable). This scripts the
# extraction recipe proven on dogfood runs 1-2: stage only the PRD's declared
# Target Files (plus their tests), emit a patch, and leave everything else
# (worker sprawl) behind.
#
# Usage: extract.sh <clone-dir> <prd-file> [out.patch]
#   <clone-dir>  the dogfood target checkout hydra worked in
#   <prd-file>   the PRD markdown containing "**Target Files:**" lines
#   [out.patch]  output path (default: ./scope-extract.patch)
#
# The patch applies to the clone's base branch with `git apply`.
set -euo pipefail

CLONE="${1:?clone dir required}"
PRD="${2:?prd file required}"
OUT="${3:-scope-extract.patch}"

cd "$CLONE"

# Collect declared target files: "**Target Files:** `path`, `path`" lines and
# bulleted "- `path`" continuations under them. Strip :symbol suffixes.
mapfile -t TARGETS < <(
  grep -oE '`[^`]+`' <(grep -A3 -i 'Target Files' "$PRD") |
    tr -d '`' | sed 's/:[A-Za-z_].*$//' | sort -u |
    while read -r f; do [ -e "$f" ] || [ -e "${f%.go}_test.go" ] && echo "$f"; done
)

if [ "${#TARGETS[@]}" -eq 0 ]; then
  echo "no existing target files parsed from $PRD" >&2
  exit 1
fi

STAGE=()
for t in "${TARGETS[@]}"; do
  [ -e "$t" ] && STAGE+=("$t")
  # companion tests in the same dir
  base="${t%.go}"
  [ -e "${base}_test.go" ] && STAGE+=("${base}_test.go")
done

git reset -q
git add -- "${STAGE[@]}"
git diff --cached > "$OUT"
git reset -q

echo "targets: ${TARGETS[*]}"
echo "patch: $OUT ($(grep -c '^diff --git' "$OUT") files, $(wc -l < "$OUT") lines)"
echo "left behind (sprawl, review manually):"
git status --short | head -20
