#!/usr/bin/env bash
# br-shim-test.sh — regression tests for packaging/br.
#
# The shim stands between every `br` call and the real binary, for humans and for
# the daemon. If it is not perfectly transparent it breaks everything that uses
# beads, so the transparency properties are pinned here rather than assumed.
#
# Self-contained: builds a fake `br` in a temp dir, no real beads needed.
# Run:  packaging/br-shim-test.sh

set -uo pipefail

SHIM="$(cd "$(dirname "$0")" && pwd)/br"
[ -x "$SHIM" ] || { echo "not executable: $SHIM" >&2; exit 2; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

pass=0 fail=0
ok()   { printf '  \033[0;32m✓\033[0m %s\n' "$*"; pass=$((pass+1)); }
bad()  { printf '  \033[0;31m✗\033[0m %s\n' "$*"; fail=$((fail+1)); }
check() { # description, expected, actual
	if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (want '$2', got '$3')"; fi
}

mode_of() { stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1"; }

# A fake `br`: echoes its arguments to both streams, writes a 0600 issues.jsonl
# on anything that is not `list`, and exits with $FAKE_EXIT.
mkdir -p "$T/fakebin"
cat > "$T/fakebin/br" <<'FAKE'
#!/usr/bin/env bash
echo "STDOUT:$*"
echo "STDERR:$*" >&2
if [ "${1:-}" != "list" ]; then
  d="$(pwd)/.beads"; mkdir -p "$d"
  printf '{"id":"x"}\n' > "$d/issues.jsonl"; chmod 600 "$d/issues.jsonl"
fi
exit "${FAKE_EXIT:-0}"
FAKE
chmod +x "$T/fakebin/br"

mkdir -p "$T/proj/.beads"
cd "$T/proj" || exit 2
export PATH="$T/fakebin:$PATH"
# The group is whatever this user actually belongs to; chgrp to `beads` would
# fail on a dev machine and the shim (correctly) ignores that.
export BR_BEADS_GROUP="$(id -gn)"

echo "br-shim-test: transparency"

"$SHIM" update x --status open >/dev/null 2>&1
check "exit status 0 forwarded" "0" "$?"

FAKE_EXIT=42 "$SHIM" update x >/dev/null 2>&1
check "non-zero exit status forwarded" "42" "$?"

check "stdout forwarded verbatim" "STDOUT:update abc --status open" \
	"$("$SHIM" update abc --status open 2>/dev/null)"
check "stderr forwarded verbatim" "STDERR:update abc --status open" \
	"$("$SHIM" update abc --status open 2>&1 >/dev/null)"

# stdin must reach the real br untouched.
cat > "$T/fakebin/br-stdin" <<'FAKE'
#!/usr/bin/env bash
cat
FAKE
chmod +x "$T/fakebin/br-stdin"
check "stdin passed through" "hello" "$(printf 'hello' | BR_REAL="$T/fakebin/br-stdin" "$SHIM" update x)"

echo "br-shim-test: permission repair"

chmod 600 .beads/issues.jsonl
"$SHIM" update x >/dev/null 2>&1
check "group gains rw after a mutating call" "660" "$(mode_of .beads/issues.jsonl)"

# The security property: bead text must never become world-readable.
m="$(mode_of .beads/issues.jsonl)"
check "world bits stay off" "0" "$(( m % 10 ))"

chmod 600 .beads/issues.jsonl
"$SHIM" list >/dev/null 2>&1
check "read-only subcommand repairs a broken store" "660" "$(mode_of .beads/issues.jsonl)"

# ...but a conforming store costs the read-only path nothing (no chmod churn).
"$SHIM" list >/dev/null 2>&1
check "read-only on conforming store leaves modes alone" "660" "$(mode_of .beads/issues.jsonl)"

# A `br` that leaves a 0600 file behind on failure must still be repaired: the
# daemon has to be able to read the state of a failed run.
chmod 600 .beads/issues.jsonl
FAKE_EXIT=1 "$SHIM" update x >/dev/null 2>&1
check "repair happens even when br fails" "660" "$(mode_of .beads/issues.jsonl)"

echo "br-shim-test: robustness"

# The shim must never invoke itself, however PATH is ordered.
mkdir -p "$T/shimbin" && cp "$SHIM" "$T/shimbin/br"
PATH="$T/shimbin:$T/fakebin:$PATH" timeout 10 "$T/shimbin/br" update x >/dev/null 2>&1
rc=$?
if [ "$rc" = 124 ]; then bad "self-recursion: shim invoked itself (timed out)"; else ok "no self-recursion when the shim is first on PATH"; fi

# No .beads anywhere: must still forward cleanly rather than erroring.
mkdir -p "$T/bare" && (cd "$T/bare" && "$SHIM" update x >/dev/null 2>&1)
check "no .beads present is not an error" "0" "$?"

# Missing real br is a clear failure, not a silent success.
(PATH="$T/empty" "$SHIM" list >/dev/null 2>&1)
check "missing real br exits 127" "127" "$?"

# BR_SHIM_OFF must be a pure passthrough.
chmod 600 .beads/issues.jsonl
BR_SHIM_OFF=1 "$SHIM" update x >/dev/null 2>&1
check "BR_SHIM_OFF disables normalisation" "600" "$(mode_of .beads/issues.jsonl)"

echo
echo "br-shim-test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
