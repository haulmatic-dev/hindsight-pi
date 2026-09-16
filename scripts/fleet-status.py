#!/usr/bin/env python3
"""fleet-status.py — one-glance hydra fleet health.

Consolidates the probes we run repeatedly (bead boards, worker registry,
llm-router, Pan activity, recent errors) into a single compact report.

Usage: python3 scripts/fleet-status.py [--errors-since 15m]

Reads: /etc/hydra/hydra.yaml (projects), /opt/dev-tools/var/hydra/.hydra/registry.db,
/var/log/hydra/hydra-error.log, llm-router /metrics + /capacity.
Run as root (log/registry reads need it). br store reads run as each repo's owner.
"""

import json
import re
import subprocess
import sys
import urllib.request
from collections import Counter

HYDRA_HOME = "/opt/dev-tools/var/hydra"
REGISTRY = f"{HYDRA_HOME}/.hydra/registry.db"
LOG = "/var/log/hydra/hydra-error.log"
ROUTER = "http://127.0.0.1:9900"


def sh(cmd, user=None):
    full = ["sudo", "-u", user] + cmd if user else cmd
    try:
        return subprocess.run(full, capture_output=True, text=True, timeout=60).stdout
    except Exception as e:
        return f"ERR: {e}"


def section(title):
    print(f"\n== {title} ==")


def systemd(*units):
    out = sh(["systemctl", "is-active", *units]).split()
    return "  ".join(f"{u}:{s}" for u, s in zip(units, out)) or "unavailable"


def recent_log_lines(since_minutes):
    """Anchored tail: drops worker-echo lines (the [Worker:<bead>] [stderr]
    prefix contamination class) and slices by log timestamp."""
    out = sh(["tail", "-n", "30000", LOG])
    # log clock runs ahead of UTC; slice generously and filter by pattern count
    return [l for l in out.splitlines() if l and not re.match(r"^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} \[Worker:", l)]


def boards():
    section("boards")
    try:
        import yaml
        cfg = yaml.safe_load(open("/etc/hydra/hydra.yaml"))
        projects = cfg.get("projects", {}).get("list", []) or []
    except Exception:
        projects = []
    seen = set()
    for p in projects:
        name, path = p.get("name", "?"), p.get("path", "")
        if not path or (name, path) in seen:
            continue
        seen.add((name, path))
        owner = path.split("/")[2] if path.startswith("/home/") else "hydra"
        raw = sh(["bash", "-c", f"cd '{path}' && br list --all --json 2>/dev/null"], user=owner)
        try:
            items = json.loads(raw)
            items = items if isinstance(items, list) else items.get("issues", [])
        except Exception:
            print(f"  {name:<22} {path}  (store unreadable)")
            continue
        c = Counter(b.get("status") for b in items)
        closes = [str(b.get("closed_at") or "")[:16] for b in items if b.get("status") == "closed" and b.get("closed_at")]
        latest = max(closes) if closes else "-"
        print(f"  {name:<22} open:{c.get('open', 0):<3} inprog:{c.get('in_progress', 0):<3} "
              f"closed:{c.get('closed', 0):<4} latest-close:{latest}  ({owner})")


def workers():
    section("workers (registry)")
    q = ("SELECT state, COUNT(*) FROM workers WHERE state NOT IN ('completed') GROUP BY state")
    out = sh(["sqlite3", REGISTRY, q]).strip()
    print("  active: " + (out.replace("\n", ", ") if out else "none"))
    q2 = ("SELECT bead_id, state, tokens_so_far/1000 FROM workers "
          "ORDER BY started_at DESC LIMIT 6")
    rows = [r.split("|") for r in sh(["sqlite3", REGISTRY, q2]).strip().splitlines() if r]
    for bead, state, tk in rows:
        print(f"  {bead:<24} {state:<10} {tk}K")


def router():
    section("llm-router")
    try:
        cap = json.load(urllib.request.urlopen(f"{ROUTER}/capacity", timeout=3))
        print(f"  capacity: free_lanes={cap.get('free_lanes')}")
        metrics = urllib.request.urlopen(f"{ROUTER}/metrics", timeout=3).read().decode()
        for pat, label in [
            (r'credits_burned\{account="([^"]+)"\} ([\d.eE+]+)', "credits"),
            (r'affinity_hit_total\{result="([^"]+)"\} (\d+)', "affinity"),
            (r'lease_denied_total\{account="([^"]+)",reason="([^"]+)"\} (\d+)', "denied"),
        ]:
            for m in re.finditer(pat, metrics):
                if label == "denied":
                    print(f"  denied {m.group(2)}: {m.group(3)} ({m.group(1)})")
                else:
                    print(f"  {label} {m.group(1)}: {m.group(2)}")
    except Exception as e:
        print(f"  unavailable: {e}")


def pan(lines):
    section("pan (last ~30k log lines)")
    pats = {
        "judgments": r"\[Pan\] \d+ candidate",
        "repairs": r"PanRepair.*\"result\":\"ok\"",
        "repair-refused": r"repair .* refused",
        "park events": r"PARKED \(",
        "abandon audits": r"abandon-with-summary audit",
    }
    for label, pat in pats.items():
        n = sum(1 for l in lines if re.search(pat, l))
        print(f"  {label:<16} {n}")


def errors(lines, since_label):
    section(f"errors/degraded ({since_label})")
    pats = {
        "DEGRADED": r"\[DEGRADED\]",
        "FAIL/error": r"\bFAIL(?:ED)?\b|❌",
        "hard-parks": r"hard-parked",
        "push pending": r"push pending|PUSH-PENDING",
    }
    for label, pat in pats.items():
        hits = [l for l in lines if re.search(pat, l)]
        print(f"  {label:<14} {len(hits)}")
        for l in hits[-2:]:
            print(f"      {l[:150]}")


def main():
    since = "15m"
    args = sys.argv[1:]
    if "--errors-since" in args:
        since = args[args.index("--errors-since") + 1]
    print(f"fleet-status  {sh(['date', '-u']).strip()}  UTC")
    print("services: " + systemd("hydra", "llm-router"))
    boards()
    workers()
    router()
    lines = recent_log_lines(30)
    pan(lines)
    errors(lines, f"tail slice (~{since} at busy rates)")


if __name__ == "__main__":
    main()
