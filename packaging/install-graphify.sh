#!/usr/bin/env bash
# Install graphify (the code knowledge graph) system-wide and wire it into the
# Pi configs Hydra workers use. Idempotent: safe to re-run.
#
#   sudo ./packaging/install-graphify.sh
#
# Why a venv rather than `pip install --user` or system pip:
#   - Ubuntu 24.04 marks system Python as externally managed (PEP 668), so a
#     plain `pip install` refuses or needs --break-system-packages.
#   - The daemon runs as `hydra`, not as whoever ran the installer, so a
#     per-user install is invisible to it. A venv plus symlinks into
#     /usr/local/bin is reachable by every user.
#
# Why the PyPI name differs from the command: the package is published as
# `graphifyy` while upstream reclaims `graphify`; the CLI is still `graphify`.
# Worth knowing before anyone installs a name-squat by guessing.
set -uo pipefail

VENV="${GRAPHIFY_VENV:-/opt/graphify}"
PI_CONFIG_DIR="${PI_CONFIG_DIR:-/opt/dev-tools/config/pi}"
DAEMON_USER="${HYDRA_USER:-hydra}"
PKG="graphifyy"

log() { printf '  %s\n' "$*"; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || fail "run as root (writes $VENV and /usr/local/bin)"
command -v python3 >/dev/null || fail "python3 not found"

echo "==> venv at $VENV"
if [ ! -x "$VENV/bin/python" ]; then
	python3 -m venv "$VENV" || fail "venv creation failed"
	log "created"
else
	log "already present"
fi

echo "==> install/upgrade $PKG"
"$VENV/bin/pip" install --quiet --upgrade pip || log "pip self-upgrade failed (continuing)"
"$VENV/bin/pip" install --quiet --upgrade "$PKG" || fail "pip install $PKG failed"
log "version: $("$VENV/bin/graphify" --version 2>&1 | head -1)"

echo "==> symlink into /usr/local/bin"
for bin in graphify graphify-mcp; do
	if [ -x "$VENV/bin/$bin" ]; then
		ln -sf "$VENV/bin/$bin" "/usr/local/bin/$bin"
		log "$bin -> $VENV/bin/$bin"
	fi
done

# The daemon user must be able to execute out of the venv.
echo "==> make readable for all users"
chmod -R a+rX "$VENV"

echo "==> install the pi skill into the canonical config"
# Agents reach graphify through this skill. It costs only its frontmatter
# (~380 B) in the prompt because skill bodies load on demand — which is why this
# is a skill and not an MCP server, whose tool definitions would be in every
# bead's prompt.
#
# `graphify install --platform pi` writes to ~/.pi/agent/skills. On the hydra
# hosts ~/.pi/agent is a SYMLINK to the canonical config (deliberately: the pi
# agent is also used standalone), so this lands in the shared config. Never
# `rm -rf` the destination first — through that symlink the destination can be
# the source.
if [ -d "$PI_CONFIG_DIR/skills" ]; then
	PI_OFFLINE=1 graphify install --platform pi >/dev/null 2>&1 || log "skill install reported an error (continuing)"
	if [ -f "$PI_CONFIG_DIR/skills/graphify/SKILL.md" ]; then
		chmod -R a+rX "$PI_CONFIG_DIR/skills/graphify"
		chgrp -R pi "$PI_CONFIG_DIR/skills/graphify" 2>/dev/null || true
		log "skill at $PI_CONFIG_DIR/skills/graphify"
	else
		log "⚠️  skill not found at $PI_CONFIG_DIR/skills/graphify — check where graphify install wrote it"
	fi
else
	log "⚠️  $PI_CONFIG_DIR/skills missing — skipping skill install"
fi

# No propagation step: workers read the canonical config directly. There used
# to be a generated rsync copy at /opt/dev-tools/config/pi-worker that every
# skill install had to be pushed into; it was retired on 2026-07-31 after it
# drifted behind the canonical tree unnoticed. Installing into
# $PI_CONFIG_DIR/skills is now the whole job.

echo "==> verify as the daemon user ($DAEMON_USER)"
# This is the check that matters: the daemon, not root, runs graphify inside
# repositories.
if id "$DAEMON_USER" >/dev/null 2>&1; then
	if sudo -u "$DAEMON_USER" bash -lc 'command -v graphify >/dev/null'; then
		log "graphify on PATH: $(sudo -u "$DAEMON_USER" bash -lc 'command -v graphify')"
		log "version: $(sudo -u "$DAEMON_USER" bash -lc 'graphify --version' 2>&1 | head -1)"
	else
		fail "graphify is NOT on ${DAEMON_USER}'s PATH — the daemon cannot build graphs"
	fi
else
	log "user $DAEMON_USER does not exist — skipping daemon check"
fi

cat <<'NOTE'

==> done. Remaining step, and it is not optional:

    Add an `intelligence.graphify` block to /etc/hydra/hydra.yaml.

    Hydra's config loader assigns `config.Intelligence = raw.Intelligence`
    wholesale, so a config with an `intelligence:` section but NO `graphify:`
    key does not inherit the built-in default — graphify ends up disabled and
    nothing builds a graph. The daemon logs a warning for exactly this case.

      intelligence:
        graphify:
          enabled: true
          binary: graphify
          refresh_interval: 30m
          timeout: 10m
          max_concurrent: 1

NOTE
