#!/usr/bin/env bash
# fix-git-ownership.sh — repair root-owned files in a git repository's
# object database left behind by root-run git operations.
#
# WHY: operators run git as root against repos the daemon (user `hydra`)
# works in; root-owned objects/refs/index entries then make daemon-side
# `git add` fail with "insufficient permission for adding an object to
# repository database", wedging bead integration (integration-pending
# parks, retest budget burns). Pan cannot repair filesystem permissions
# (root-only). This script is the root-side repair (2026-11-08, 6th
# recurrence of the class; hindsight 2026-10-28 rule "never run git as
# root" — rules don't catch mistakes, sweeps do).
#
# USAGE (as root): fix-git-ownership.sh <checkout-path>
#   <checkout-path> may be a submodule checkout (.git is a FILE pointing
#   into the parent's modules dir) — the script resolves the real git
#   common dir via `git rev-parse --git-common-dir` so the actual object
#   database is swept, not the pointer.
#
# SAFETY (adversarial review 2026-11-08 round 1):
#   - Ownership is restored to the CHECKOUT OWNER (stat of the checkout
#     root) — never hardcoded: user repos belong to their users.
#   - Daemon access restored after chown in daemon-worked repos via ACL
#     (u:hydra:rwx), mirroring the onboard-sweep ACL pattern — plain
#     chown-to-user would revoke hydra's write into those dirs and wedge
#     the daemon a second way.
#   - No `chown -R`, no symlink following: `find -P` restricted to
#     (-type f -o -type d), each entry chowned individually (TOCTOU-safe
#     against symlink swaps).
#   - Fail-open: repairs that fail log a warning and the script still
#     exits 0 — a guard must never fail a deploy (EXIT-trap caller).

set -u

LOG_PREFIX="[GitOwnershipFix]"
fixed=0

log() { echo "$LOG_PREFIX $*"; }

# resolve_git_common_dir <checkout> — prints the real git dir (absolute),
# or empty. Handles both normal repos (.git dir) and submodule/worktree
# checkouts (.git FILE with gitdir: pointer).
resolve_git_common_dir() {
    local checkout="$1" gitdir
    [ -d "$checkout" ] || return 0
    gitdir=$(git -C "$checkout" rev-parse --git-common-dir 2>/dev/null) || return 0
    [ -n "$gitdir" ] || return 0
    # rev-parse may return a relative path (e.g. ../../.git/modules/x)
    case "$gitdir" in
        /*) printf '%s\n' "$gitdir" ;;
        *)  printf '%s\n' "$(cd "$checkout" && cd "$gitdir" 2>/dev/null && pwd)" ;;
    esac
}

# fix_repo <checkout> — sweep one checkout's git common dir.
fix_repo() {
    local checkout="$1"
    [ -d "$checkout" ] || return 0

    local gitdir
    gitdir=$(resolve_git_common_dir "$checkout")
    [ -n "$gitdir" ] && [ -d "$gitdir" ] || { log "skip $checkout: no resolvable git dir"; return 0; }

    # TOCTOU guard: the resolved git dir must not be a symlink itself.
    local real_gitdir
    real_gitdir=$(realpath -e "$gitdir" 2>/dev/null) || { log "skip $checkout: gitdir realpath failed"; return 0; }
    [ "$real_gitdir" = "$gitdir" ] || { log "skip $checkout: gitdir is a symlink ($gitdir -> $real_gitdir)"; return 0; }

    local owner_group owner
    owner_group=$(stat -c '%U:%G' "$checkout" 2>/dev/null) || { log "skip $checkout: stat failed"; return 0; }
    owner=${owner_group%%:*}
    # Deleted-uid owners surface as numeric — still a valid chown target.
    [ -n "$owner" ] || { log "skip $checkout: no owner resolved"; return 0; }

    local count
    count=$(find -P "$gitdir" -maxdepth 6 \( -type f -o -type d \) -user root 2>/dev/null | wc -l)
    [ "$count" -gt 0 ] || return 0

    log "fixing $count root-owned entries in $gitdir -> $owner_group"
    find -P "$gitdir" -maxdepth 6 \( -type f -o -type d \) -user root -print0 2>/dev/null |
    while IFS= read -r -d '' entry; do
        chown "$owner_group" "$entry" 2>/dev/null || log "WARN: chown failed on $entry"
    done

    # Restore daemon write access in dirs it must write into (objects,
    # refs, worktrees metadata). setfacl is absent on some hosts — the
    # chown above already fixed the primary wedge; ACL is the belt for
    # daemon-worked user repos.
    if command -v setfacl >/dev/null 2>&1; then
        find -P "$gitdir" -maxdepth 6 -type d \( -name objects -o -name refs -o -name worktrees \) -print0 2>/dev/null |
        while IFS= read -r -d '' d; do
            setfacl -m "u:hydra:rwx" "$d" 2>/dev/null || true
        done
    fi
    fixed=$((fixed + count))
    log "done: $count entries -> $owner_group (+hydra ACL on objects/refs/worktrees dirs)"
}

main() {
    if [ "$(id -u)" -ne 0 ]; then
        log "must run as root (chown required); current uid=$(id -u)"
        exit 1
    fi
    local target="${1:-}"
    [ -n "$target" ] || { log "usage: $0 <checkout-path>"; exit 1; }

    fix_repo "$target"

    # Submodule checkouts: the parent monorepo's .git holds the shared
    # object databases (modules/*). If the checkout's git common dir lives
    # under a parent repo, sweep the parent .git too — resolving the
    # common dir alone misses sibling module dbs and the parent objects.
    local gitdir parent_git
    gitdir=$(resolve_git_common_dir "$target")
    case "$gitdir" in
        */.git/modules/*)
            parent_git="${gitdir%%/.git/modules/*}/.git"
            if [ -d "$parent_git" ] && [ "$parent_git" != "$gitdir" ]; then
                fix_repo_from_gitdir "$parent_git" "$target"
            fi
            ;;
    esac

    [ "$fixed" -eq 0 ] || log "summary: $fixed entries repaired"
    exit 0
}

# fix_repo_from_gitdir <gitdir> <owner-checkout> — sweep an explicit git
# dir (not resolved from the checkout), owned by the checkout's owner.
fix_repo_from_gitdir() {
    local gitdir="$1" checkout="$2"
    [ -d "$gitdir" ] || return 0
    local real_gitdir
    real_gitdir=$(realpath -e "$gitdir" 2>/dev/null) || return 0
    [ "$real_gitdir" = "$gitdir" ] || { log "skip $gitdir: symlink"; return 0; }
    local owner_group count
    owner_group=$(stat -c '%U:%G' "$checkout" 2>/dev/null) || return 0
    count=$(find -P "$gitdir" -maxdepth 6 \( -type f -o -type d \) -user root 2>/dev/null | wc -l)
    [ "$count" -gt 0 ] || return 0
    log "fixing $count root-owned entries in parent $gitdir -> $owner_group"
    find -P "$gitdir" -maxdepth 6 \( -type f -o -type d \) -user root -print0 2>/dev/null |
    while IFS= read -r -d '' entry; do
        chown "$owner_group" "$entry" 2>/dev/null || log "WARN: chown failed on $entry"
    done
    if command -v setfacl >/dev/null 2>&1; then
        find -P "$gitdir" -maxdepth 6 -type d \( -name objects -o -name refs -o -name worktrees \) -print0 2>/dev/null |
        while IFS= read -r -d '' d; do
            setfacl -m "u:hydra:rwx" "$d" 2>/dev/null || true
        done
    fi
    fixed=$((fixed + count))
}

main "$@"
