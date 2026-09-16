#!/usr/bin/env python3
"""campaign-status.py — deep-dive on one project's campaign execution.

Answers the recurring forensics questions in one run:
  board state, worker sessions, close verdicts (accepted/rejected + reason),
  worktree wiring, integrate commits, and where the work actually landed.

Usage: python3 scripts/campaign-status.py /home/<user>/projects/<repo>
"""

import json
import os
import re
import subprocess
import sys
from collections import Counter

HYDRA_HOME = "/opt/dev-tools/var/hydra"
REGISTRY = f"{HYDRA_HOME}/.hydra/registry.db"
LOG = "/var/log/hydra/hydra-error.log"


def sh(cmd, user=None):
    full = ["sudo", "-u", user] + cmd if user else cmd
    try:
        return subprocess.run(full, capture_output=True, text=True, timeout=90).stdout
    except Exception as e:
        return f"ERR: {e}"


def main():
    if len(sys.argv) < 2:
        print("usage: campaign-status.py <repo-path>")
        sys.exit(1)
    repo = os.path.abspath(sys.argv[1])
    owner = repo.split("/")[2] if repo.startswith("/home/") else "hydra"
    enc = repo.replace("/", "-")
    wtroot = f"{HYDRA_HOME}/.hydra-worktrees/{enc}"
    prefix = os.path.basename(repo)  # bead ID prefix

    print(f"campaign-status  {repo}  (owner: {owner})")

    # 1. Board
    raw = sh(["bash", "-c", f"cd '{repo}' && br list --all --json 2>/dev/null"], user=owner)
    try:
        items = json.loads(raw)
        items = items if isinstance(items, list) else items.get("issues", [])
    except Exception:
        items = []
    c = Counter(b.get("status") for b in items)
    print(f"\n[board] open:{c.get('open', 0)} inprog:{c.get('in_progress', 0)} closed:{c.get('closed', 0)}")
    for b in items:
        if b.get("status") == "in_progress":
            print(f"  IN PROGRESS: {b['id']} {(b.get('title') or '')[:50]}")

    # 2. Workers (this project's beads, any state, recent first)
    rows = sh(["sqlite3", REGISTRY,
               "SELECT bead_id, state, tokens_so_far/1000, substr(started_at,1,16) FROM workers "
               f"WHERE bead_id LIKE '{prefix}-%' ORDER BY started_at DESC LIMIT 10"]).strip()
    print("\n[workers] (recent 10)")
    for line in rows.splitlines():
        if line:
            bead, state, tk, started = line.split("|")
            print(f"  {bead:<22} {state:<10} {tk:>8}K  since {started}")

    # 3. Close verdicts from the daemon log (anchored; worker-echo lines dropped)
    out = sh(["grep", "-a", f"CloseEvent.*{prefix}-", LOG]).strip()
    verdicts = re.findall(r'CloseEvent.*?"bead_id":"([^"]+)","verdict":"([^"]+)","reason":"([^"]+)"', out)
    print("\n[close events] (last 8)")
    for bead, verdict, reason in verdicts[-8:]:
        print(f"  {bead:<22} {verdict:<9} {reason}")

    # 4. Worktrees: dir, branch, prunable, HEAD vs campaign branch
    branch = sh(["bash", "-c", f"cd '{repo}' && git rev-parse --abbrev-ref HEAD 2>/dev/null"], user=owner).strip()
    head = sh(["bash", "-c", f"cd '{repo}' && git rev-parse --short HEAD 2>/dev/null"], user=owner).strip()
    print(f"\n[branch] {branch} @ {head}")
    integrates = sh(["bash", "-c", f"cd '{repo}' && git log --oneline -5 --grep='integrate' 2>/dev/null"], user=owner).strip()
    print(f"[integrate commits]\n  {integrates.replace(chr(10), chr(10)+'  ') if integrates else '(none)'}")
    print("[worktrees]")
    if os.path.isdir(wtroot):
        for d in sorted(os.listdir(wtroot)):
            info = sh(["bash", "-c", f"cd '{repo}' && git worktree list --porcelain 2>/dev/null | grep -A2 '{d}' | head -3"], user=owner).strip()
            wt_branch = ""
            for l in info.splitlines():
                m = re.search(r"branch refs/heads/(\S+)", l)
                if m:
                    wt_branch = m.group(1)
            prunable = "prunable" in info
            print(f"  {d:<24} branch:{wt_branch or '?':<28} {'PRUNABLE' if prunable else 'active'}")
    else:
        print("  (none)")

    # 5. Where the work landed: uncommitted + wt-branch diffs vs campaign branch
    dirty = sh(["bash", "-c", f"cd '{repo}' && git status --porcelain 2>/dev/null | head -5"], user=owner).strip()
    print(f"\n[main checkout dirt]\n  {dirty.replace(chr(10), chr(10)+'  ') if dirty else '(clean)'}")
    wtbranches = sh(["bash", "-c", f"cd '{repo}' && git branch --format='%(refname:short)' 'hydra/wt-*' 2>/dev/null"], user=owner).strip()
    for b in wtbranches.splitlines()[:5]:
        stat = sh(["bash", "-c", f"cd '{repo}' && git diff --stat {branch}..{b} 2>/dev/null | tail -1"], user=owner).strip()
        print(f"[{b}] vs {branch}: {stat or 'no diff / empty'}")


if __name__ == "__main__":
    main()
