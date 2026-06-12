#!/bin/bash
# Creates a lightweight Pi config directory for Hydra workers.
# The worker config is a copy of the main Pi config with context-mode
# removed, so each worker starts ~5-10x faster.

set -e

PI_CONFIG_DIR="${PI_CONFIG_DIR:-/opt/dev-tools/config/pi}"
WORKER_CONFIG_DIR="${WORKER_CONFIG_DIR:-/opt/dev-tools/config/pi-worker}"

if [ ! -d "$PI_CONFIG_DIR" ]; then
    echo "Error: Pi config directory not found: $PI_CONFIG_DIR" >&2
    exit 1
fi

echo "Setting up worker Pi config at $WORKER_CONFIG_DIR"

mkdir -p "$WORKER_CONFIG_DIR"

# Copy main config, excluding per-session/state directories and VCS metadata.
rsync -a \
    --exclude='.git' \
    --exclude='sessions' \
    --exclude='*.log' \
    "$PI_CONFIG_DIR/" "$WORKER_CONFIG_DIR/"

# Sessions are kept separate so concurrent workers don't fight over locks.
mkdir -p "$WORKER_CONFIG_DIR/agent/sessions"

# Remove context-mode from packages so workers skip the MCP bridge startup.
python3 - "$WORKER_CONFIG_DIR/settings.json" <<'PY'
import json, sys
path = sys.argv[1]
with open(path) as f:
    data = json.load(f)
if 'packages' in data:
    before = len(data['packages'])
    data['packages'] = [p for p in data['packages'] if p != 'npm:context-mode']
    after = len(data['packages'])
    if before != after:
        print(f'Removed npm:context-mode from {path}')
with open(path, 'w') as f:
    json.dump(data, f, indent=2)
PY

# Allow all users in the 'pi' group to read/update shared Pi config.
chown -R root:pi "$WORKER_CONFIG_DIR"
find "$WORKER_CONFIG_DIR" -type d -exec chmod g+ws {} \;
find "$WORKER_CONFIG_DIR" -type f -exec chmod g+rw {} \;

echo "Done. Add this to your Hydra config:"
echo "  pi:"
echo "    config_dir: $PI_CONFIG_DIR"
echo "    worker_config_dir: $WORKER_CONFIG_DIR"
