#!/usr/bin/env bash
# git-ownership-guard.sh — fleet-wide sweep for root-owned files in the
# git databases of every repo the hydra daemon works in. Runs as ROOT from
# a systemd timer (15 min, cloned from hydra-onboard-sweep.timer).
#
# WHY: any operator running git as root (deploy ritual, root shells) can
# leave root-owned entries in any repo's .git; the daemon then fails
# `git add` with "insufficient permission for adding an object to
# repository database" and bead integration wedges (2026-11-08: 64
# root-owned entries in the monorepo module db wedged 4 beads; 6th
# recurrence of the class). The daemon cannot chown — only a root-side
# sweep repairs. This guard is the net that catches every operator, every
# repo; scripts/fix-git-ownership.sh is the shared per-repo repair.
#
# ENUMERATION (adversarial review 2026-11-08):
#   - Config-driven first: `path:` entries from /etc/hydra/hydra.yaml
#     (the authoritative repo set).
#   - Plus /home/*/projects/* (flat glob — `~*` tilde expansion is a
#     silent no-op as root; reviewed blocker #1).
#   - The monorepo itself (this script's checkout) and its parent .git
#     (shared modules db — reviewed blocker #2).
#
# SAFETY: per-repo repair delegates to fix-git-ownership.sh (find -P,
# per-entry chown, no -R, no deref, realpath guards). This enumerator
# only adds the symlink/containment check on candidate repos:
#   - candidate must be a real dir (no symlink repos)
#   - per-user candidates must resolve under /home/<same-user>/
# Fail-open per repo: errors log and continue.

set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
FIX="$HERE/fix-git-ownership.sh"
LOG_PREFIX="[GitOwnershipGuard]"
HYDRA_YAML="${HYDRA_YAML:-/etc/hydra/hydra.yaml}"

log() { echo "$LOG_PREFIX $*"; }

[ "$(id -u)" -eq 0 ] || { log "must run as root"; exit 1; }
[ -x "$FIX" ] || { log "missing $FIX"; exit 1; }

declare -A seen

add_candidate() {
    local path="$1"
    [ -n "$path" ] || return 0
    [ "${seen[$path]:-}" = "1" ] && return 0
    seen["$path"]=1
    [ -d "$path" ] || return 0
    # A candidate repo root must not itself be a symlink (TOCTOU guard).
    local real
    real=$(realpath -e "$path" 2>/dev/null) || return 0
    [ "$real" = "$path" ] || { log "skip symlink repo $path"; return 0; }
    "$FIX" "$path" || log "WARN: fix failed for $path (continuing)"
}

# 1) The monorepo checkout (script's own repo) + its parent repo.
add_candidate "$REPO"
add_candidate "/opt/dev-tools"

# 2) Config-driven project paths (authoritative set).
if [ -r "$HYDRA_YAML" ]; then
    while IFS= read -r p; do
        add_candidate "$p"
    done < <(sed -n 's/^[[:space:]]*-[[:space:]]*path:[[:space:]]*\(.*\)$/\1/p' "$HYDRA_YAML" | tr -d '"' | tr -d "'")
fi

# 3) Per-user project repos (flat /home glob; containment enforced).
shopt -s nullglob
for userrepo in /home/*/projects/*; do
    [ -d "$userrepo" ] || continue
    real=$(realpath -e "$userrepo" 2>/dev/null) || continue
    case "$real" in
        /home/*) add_candidate "$userrepo" ;;
        *) log "skip out-of-containment $userrepo -> $real" ;;
    esac
done
shopt -u nullglob

log "sweep complete (${#seen[@]} candidates)"
exit 0
