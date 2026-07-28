#!/usr/bin/env bash
# Install the repo's git hooks. Idempotent.
#
#   ./scripts/install-hooks.sh
#
# Hooks live in scripts/hooks/ so they are version-controlled and reviewable;
# .git/hooks is not. This uses core.hooksPath so a single setting picks up every
# hook in that directory and future ones need no re-install.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1
HOOKS_DIR="scripts/hooks"

if [ ! -d "$HOOKS_DIR" ]; then
	echo "ERROR: $HOOKS_DIR not found" >&2
	exit 1
fi

chmod +x "$HOOKS_DIR"/* 2>/dev/null || true
git config core.hooksPath "$HOOKS_DIR"

echo "installed: core.hooksPath = $(git config core.hooksPath)"
for h in "$HOOKS_DIR"/*; do
	[ -f "$h" ] && echo "  $(basename "$h") $([ -x "$h" ] && echo '(executable)' || echo '(NOT executable)')"
done
echo
echo "Bypass a single push with: HYDRA_SKIP_PREPUSH=1 git push"
