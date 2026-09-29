#!/usr/bin/env bash
# cache-cap-guard.sh — hourly cache-retention guard for the hydra runtime user.
# Installed as a crontab entry (17 * * * *). Keeps the host under the disk
# watermark by capping the caches the daemon itself creates; system-level
# caps (journald SystemMaxUse) remain operator/root actions.
#
# 1. /tmp/hydra-executions: execution-output JSON, already harvested into
#    events/ — retention 24h. This directory re-accumulates at ~6G/day.
# 2. Go build cache: capped by age (7d) — rebuilds are cheap.
# 3. systemd-tmpfiles clean (user level).
set -u

EXEC_DIR=/tmp/hydra-executions
LOG_TAG=cache-cap-guard

log() { echo "$(date -u +%FT%TZ) $LOG_TAG: $*"; }

# 1. Old execution outputs.
if [ -d "$EXEC_DIR" ]; then
  before=$(df -k / | awk 'NR==2{print $3}')
  find "$EXEC_DIR" -maxdepth 1 -mtime +1 -print0 2>/dev/null | xargs -0 -r rm -rf
  after=$(df -k / | awk 'NR==2{print $3}')
  log "executions pruned: $(( (before - after) / 1024 ))MiB freed"
fi

# 2. Go build cache older than 7 days (GOCACHE-aware).
if command -v go >/dev/null 2>&1; then
  gc="$(go env GOCACHE 2>/dev/null || true)"
  if [ -n "$gc" ] && [ -d "$gc" ]; then
    find "$gc" -type f -mtime +7 -print0 2>/dev/null | xargs -0 -r rm -f
  fi
fi

# 3. User tmpfiles cleanup (includes ~/.config/user-tmpfiles.d rules).
systemd-tmpfiles --user --clean >/dev/null 2>&1 || true

# 4. Watermark report (fail-open: never gate on this).
df -h / | awk 'NR==2{print "'"$LOG_TAG"': / usage " $5 " (" $4 " free)"}'
