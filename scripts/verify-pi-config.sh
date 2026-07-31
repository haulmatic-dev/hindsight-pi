#!/usr/bin/env bash
# verify-pi-config.sh — assert the Pi config is single and shared.
#
# Hydra workers and interactive shells must resolve the SAME pi config, so a
# skill or extension added once is used by both. Until 2026-07-31 they did not:
# /opt/dev-tools/config/pi-worker was a full 278 MB copy of
# /opt/dev-tools/config/pi, tracked in the dev-tools repo, and it had drifted to
# 239 npm packages against 242. Nothing detected that, because nothing looked.
#
# The split turned out to be accidental rather than designed: the only config
# file that differed was settings.json, on one line — lastChangelogVersion,
# which pi rewrites itself. APPEND_SYSTEM.md and auth.json were byte-identical
# and the skills trees were identical.
#
# Checks, in order of what actually breaks:
#   1. worker_config_dir == config_dir            (workers and shells agree)
#   2. the dir is a Pi package root               (else "No packages installed")
#   3. every allowlisted extension resolves       (else workers run with none)
#   4. ~/.pi/agent points at it for real users    (else shells diverge)
#   5. no retired copy is still referenced        (else drift returns)
#
# Exit 0 = healthy, 1 = a check failed. Safe to run any time; reads only.

set -uo pipefail

CONFIG="${HYDRA_CONFIG:-/etc/hydra/hydra.yaml}"
FAILED=0

ok()   { printf '  \033[0;32m✓\033[0m %s\n' "$*"; }
bad()  { printf '  \033[0;31m✗\033[0m %s\n' "$*"; FAILED=1; }
warn() { printf '  \033[1;33m⚠\033[0m %s\n' "$*"; }

echo "verify-pi-config: reading $CONFIG"
[ -f "$CONFIG" ] || { bad "config not found: $CONFIG"; exit 1; }

# yq is not installed on the box; python3 is, and it is already a hard
# dependency of the seeding path.
read -r CFG_DIR WORKER_DIR < <(python3 - "$CONFIG" <<'PY'
import sys, yaml
d = yaml.safe_load(open(sys.argv[1])) or {}
pi = d.get("pi") or {}
print(pi.get("config_dir", "") or "-", pi.get("worker_config_dir", "") or "-")
PY
)

echo "  config_dir        = $CFG_DIR"
echo "  worker_config_dir = $WORKER_DIR"

# 1. One directory, not two.
if [ "$CFG_DIR" = "-" ]; then
	bad "pi.config_dir is unset"
elif [ "$CFG_DIR" = "$WORKER_DIR" ]; then
	ok "workers and interactive shells share one config dir"
else
	bad "config_dir and worker_config_dir differ — they will drift (this is the 2026-07-31 regression)"
fi

# 2. Pi must recognise it as a package root, or it reports "No packages
#    installed" and every worker runs with zero extensions.
if [ -d "$CFG_DIR/npm/node_modules" ] || [ -f "$CFG_DIR/settings.json" ]; then
	ok "$CFG_DIR is a Pi package root"
else
	bad "$CFG_DIR has neither npm/node_modules nor settings.json — Pi will load no extensions"
fi

# 3. Extensions hydra asks for must actually be present. Names come from the
#    allowlist; the alias map lives in cmd/worker/pi_extensions.go and is
#    mirrored here deliberately, so a rename shows up as a failure rather than a
#    silently skipped extension.
declare -A ALIAS=(
	[rtk]=pi-rtk-optimizer
	[caveman]=@tomooshi/caveman-milk-pi
	[context-mode]=context-mode
	[guardrails]=@aliou/pi-guardrails
	[gograph]=pi-gograph
	[subagents]=pi-subagents
	[web-access]=pi-web-access
)

WANTED=$(python3 - "$CONFIG" <<'PY'
import sys, yaml
d = yaml.safe_load(open(sys.argv[1])) or {}
pi = ((d.get("agents") or {}).get("pi")) or {}
names = pi.get("extensions")
if names is None:
    names = ["rtk", "context-mode"]  # defaultPiExtensions
for phase in (pi.get("phase_extensions") or {}).values():
    names = list(names) + list(phase or [])
print(" ".join(sorted(set(n for n in names if n))))
PY
)

if [ -z "$WANTED" ]; then
	warn "no extensions requested — workers will run with --no-extensions and nothing loaded"
else
	for name in $WANTED; do
		pkg="${ALIAS[$name]:-$name}"
		if [ -d "$CFG_DIR/npm/node_modules/$pkg" ]; then
			ok "extension $name -> $pkg"
		else
			bad "extension $name -> $pkg NOT installed under $CFG_DIR/npm/node_modules"
		fi
	done
fi

# 4. The interactive side. A user whose ~/.pi/agent points elsewhere is running
#    a different skill set from the fleet, which is the whole problem.
for home in /root /home/*; do
	[ -d "$home" ] || continue
	link="$home/.pi/agent"
	[ -e "$link" ] || continue
	target="$(readlink -f "$link" 2>/dev/null)"
	if [ "$target" = "$(readlink -f "$CFG_DIR")" ]; then
		ok "$link -> shared config"
	else
		bad "$link -> $target (not the shared config)"
	fi
done

# 5. A retired copy left on disk is fine; a retired copy still referenced is
#    how the drift comes back.
for stale in /opt/dev-tools/config/pi-worker /opt/hydra/pi-config /opt/hydra/.agents/skills; do
	if [ -e "$stale" ]; then
		if grep -qs -- "$stale" "$CONFIG"; then
			bad "$stale still exists AND is referenced by $CONFIG"
		else
			warn "$stale still exists but nothing references it (safe to remove)"
		fi
	fi
done

if [ "$FAILED" -eq 0 ]; then
	echo "verify-pi-config: OK"
else
	echo "verify-pi-config: FAILED"
fi
exit "$FAILED"
