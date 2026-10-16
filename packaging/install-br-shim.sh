#!/usr/bin/env bash
# install-br-shim — make the packaging/br shim win on every PATH that can run br.
#
# WHY: the shim (packaging/br) repairs .beads store perms inside the same br
# invocation. That only works when `br` on a given PATH IS the shim. The
# 2026-10-15 lockout incident was caused by a raw beads-rust ELF shadowing the
# shim at /opt/dev-tools/config/pi/bin/br (owner-session PATH), so every owner
# write re-locked the store to 0600 with an empty ACL mask and the daemon
# (uid 999) lost access until root repaired it.
#
# WHAT IT DOES: for every directory that appears before the beads-rust release
# dir on PATH and contains a br that is NOT the shim, replace it with a
# hardlink/symlink to the shim. Never touches the release binary itself.
#
# MUST run as root when the target dirs are root-owned (they usually are).
#
# Usage: sudo packaging/install-br-shim.sh [path-spec]
#   path-spec defaults to the daemon unit's PATH ordering + the interactive
#   PATH, colon-separated, deduped in order.
set -euo pipefail

SHIM="$(cd "$(dirname "$0")" && pwd)/br"
RELEASE_DIR="/opt/dev-tools/components/infra/services/beads-rust/beads_rust/target/release"

DEFAULT_PATH="/opt/dev-tools/config/pi/bin:/opt/dev-tools/bin:$RELEASE_DIR:/usr/local/bin:/usr/bin:/bin"
SPEC="${1:-${PATH:-$DEFAULT_PATH}}"

if [ ! -f "$SHIM" ]; then
	echo "error: shim not found at $SHIM" >&2
	exit 1
fi

declare -A seen=""
first_raw=""
order=""

IFS=: read -ra DIRS <<<"$SPEC"
for dir in "${DIRS[@]}"; do
	[ -n "$dir" ] || continue
	[ -d "$dir" ] || continue
	[ -n "${seen[$dir]:-}" ] && continue
	seen[$dir]=1
	order="$order$dir:"
	[ "$dir" = "$RELEASE_DIR" ] && break # the real binary stays put
done

for dir in ${order%:}; do
	target="$dir/br"
	if [ ! -e "$target" ]; then
		continue
	fi
	# Identify the shim by its shebang; the raw binary is an ELF.
	if head -c 2 "$target" | grep -q '#!'; then
		echo "ok (already shim): $target"
		continue
	fi
	if ln -sf "$SHIM" "$target.tmp" && mv -f "$target.tmp" "$target"; then
		echo "shimmed (was raw binary): $target"
	else
		echo "FAILED to shim $target (root-owned? run with sudo)" >&2
		first_raw="$first_raw $target"
	fi
done

if [ -n "$first_raw" ]; then
	echo "still raw after run:$first_raw" >&2
	echo "these will keep re-locking .beads stores on owner writes" >&2
	exit 1
fi

echo "verify: for d in \$(echo \"\$PATH\" | tr : ' '); do [ -x \"\$d/br\" ] && head -c2 \"\$d/br\" | grep -q '#!' && echo shim: \$d/br; done"
