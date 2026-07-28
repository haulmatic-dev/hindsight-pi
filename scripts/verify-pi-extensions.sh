#!/usr/bin/env bash
# Verify whether Pi extensions actually ACTIVATE, and in which mode.
#
# Why this script exists: extensions being *installed* and *discoverable* is not
# the same as them doing anything. On the VPS all 13 packages were installed and
# rtk still had not rewritten a single command since 06-26. The only trustworthy
# signal is rtk's own counter, which is external to Pi and to Hydra.
#
# Run this on a machine with a WORKING KIMI_API_KEY in the environment. It makes
# real model calls (3 short turns).
#
#   ./scripts/verify-pi-extensions.sh
#
# It answers three questions:
#   1. Does rtk activate in text mode with extension discovery on?
#   2. Does rtk activate in text mode when the extension is named explicitly (-e)?
#   3. Does rtk activate in rpc mode?
set -uo pipefail

PI=${PI:-pi}
PROVIDER=${PROVIDER:-kimi-coding-plan}
MODEL=${MODEL:-kimi-k2.7-code}
# A prompt that forces several shell commands — rtk only rewrites commands, so a
# turn that runs none proves nothing either way.
PROMPT=${PROMPT:-"Run these shell commands one at a time: 'git status --short', then 'ls -la', then 'grep -rn TODO . | head -5'. Then reply DONE."}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK" || exit 1
git init -q .
printf 'package main\n\n// TODO: something\nfunc main() {}\n' > main.go
git add -A && git -c user.email=t@t -c user.name=t commit -qm init

if ! command -v rtk >/dev/null 2>&1; then
  echo "rtk not on PATH — this script needs it as the ground-truth counter." >&2
  exit 1
fi

# rtk's counter is the ground truth: it is incremented by rtk itself, so it cannot
# be faked by a log line or a plausible-looking event.
counter() { rtk gain 2>/dev/null | grep -aiE "total commands" | grep -aoE "[0-9]+" | head -1; }
newest()  { rtk gain --history 2>/dev/null | head -2 | tail -1 | cut -c1-60; }

report() { # $1=label  $2=before  $3=after
  local delta=$(( ${3:-0} - ${2:-0} ))
  if [ "$delta" -gt 0 ]; then
    printf '  %-34s rtk rewrote %d command(s)  ACTIVE\n' "$1" "$delta"
  else
    printf '  %-34s rtk counter unchanged      NOT ACTIVE\n' "$1"
  fi
}

echo "pi:       $(command -v $PI)"
echo "provider: $PROVIDER / $MODEL"
echo "rtk:      $(rtk --version 2>&1 | head -1)"
echo "packages installed:"
$PI list 2>&1 | grep -E "rtk|caveman|context-mode|guardrails" | sed 's/^/  /'
echo

echo "=== 1. text mode, discovery on ==="
b=$(counter)
timeout 240 $PI --offline --no-session --provider "$PROVIDER" --model "$MODEL" "$PROMPT" >"$WORK/t1.log" 2>&1
report "text (discovery)" "$b" "$(counter)"

echo "=== 2. text mode, extension named explicitly (-e) ==="
ENTRY=$($PI list 2>/dev/null | grep -A1 "rtk" | tail -1 | tr -d ' ')
if [ -n "$ENTRY" ] && [ -d "$ENTRY" ]; then
  MAIN=$(python3 -c "
import json,os,sys
p=sys.argv[1]
try:
    d=json.load(open(os.path.join(p,'package.json')))
except Exception:
    print(''); raise SystemExit
ex=(d.get('pi') or {}).get('extensions') or []
print(os.path.normpath(os.path.join(p, ex[0] if ex else (d.get('main') or 'index.js'))))
" "$ENTRY" 2>/dev/null)
  echo "  entry: ${MAIN:-<not resolved>}"
  if [ -n "${MAIN:-}" ] && [ -f "$MAIN" ]; then
    b=$(counter)
    timeout 240 $PI --offline --no-session --provider "$PROVIDER" --model "$MODEL" \
      -e "$MAIN" "$PROMPT" >"$WORK/t2.log" 2>&1
    report "text (-e explicit)" "$b" "$(counter)"
  else
    echo "  skipped — could not resolve the extension entry file"
  fi
else
  echo "  skipped — rtk package path not found in 'pi list'"
fi

echo "=== 3. rpc mode (via tools/pi-rpc) ==="
RPC=${RPC:-./tools/pi-rpc}
if [ -d "$OLDPWD/tools/pi-rpc" ]; then
  ( cd "$OLDPWD/tools/pi-rpc" && go build -o "$WORK/pi-rpc" . ) 2>/dev/null
fi
if [ -x "$WORK/pi-rpc" ]; then
  b=$(counter)
  timeout 240 "$WORK/pi-rpc" --prompt "$PROMPT" --provider "$PROVIDER" --model "$MODEL" \
    --auto-confirm >"$WORK/t3.log" 2>&1
  report "rpc" "$b" "$(counter)"
else
  echo "  skipped — build tools/pi-rpc first (go build ./tools/pi-rpc)"
fi

echo
echo "newest rtk history entry now: $(newest)"
echo
echo "Interpretation:"
echo "  Any line saying ACTIVE means that mode really does run extensions —"
echo "  then the same should hold for caveman/context-mode, worth confirming once."
echo "  All three NOT ACTIVE means the extensions are inert regardless of mode,"
echo "  and rpc buys nothing until that is fixed."
