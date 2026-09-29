#!/bin/sh
# Cache cap guard (hydra-rsyw followup): keeps regenerable caches under the
# host disk-watermark budget so the daemon's rebuild loops cannot drift / back
# over the 85% watermark. Runs from hydra's crontab (no sudo required).
# Purges, oldest-first by atime/mtime, until df used-ratio <= 85% or nothing left.

CACHE_ROOT=/opt/dev-tools/var/hydra
TARGET=85

ratio() { df / | awk 'NR==2 {printf "%d", $5}'; }

purge_dir() {
  # delete whole directory if it exists (all listed paths are regenerable)
  [ -e "$1" ] && rm -rf "$1"
}

purge_oldest() {
  # delete oldest entries one at a time under $1 (cache with many entries)
  find "$1" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' 2>/dev/null \
    | sort -n | head -1 | cut -d' ' -f2-
}

while [ "$(ratio)" -gt "$TARGET" ]; do
  freed=0
  # Tier 1: whole regenerable caches
  for d in \
    "$CACHE_ROOT/.cache/go-build" \
    "$CACHE_ROOT/.npm/_npx" \
    "$CACHE_ROOT/.cache/pnpm" \
    "$CACHE_ROOT/.cache/ms-playwright" \
    "$CACHE_ROOT/.cache/puppeteer" \
    "$CACHE_ROOT/.cache/mongodb-binaries" \
    "$CACHE_ROOT/.cache/huggingface" \
    "$CACHE_ROOT/go/pkg/mod"; do
    [ "$(ratio)" -le "$TARGET" ] && break
    purge_dir "$d" && freed=1
  done
  # Tier 2: stale hydra tmp work dirs (>2 days)
  [ "$(ratio)" -le "$TARGET" ] && break
  find "$CACHE_ROOT/tmp" -mindepth 1 -maxdepth 1 -type d -mtime +2 -exec rm -rf {} + 2>/dev/null && freed=1
  # Tier 3: stale worktrees of CLOSED beads only (hydra cleans these too;
  # only remove if the matching br entry is terminal-closed)
  [ "$(ratio)" -le "$TARGET" ] && break
  for w in "$CACHE_ROOT"/.hydra-worktrees/*/; do
    [ "$(ratio)" -le "$TARGET" ] && break
    for b in "$w"*/; do
      id=$(basename "$b")
      st=$(br show "$id" 2>/dev/null | head -1)
      case "$st" in *CLOSED*) rm -rf "$b";; esac
    done
  done
  [ "$freed" = 0 ] && break   # nothing left to purge; fail-open, exit
done
exit 0
