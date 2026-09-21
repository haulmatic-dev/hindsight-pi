#!/usr/bin/env python3
"""turn_economy.py — turn-economy measurement harness over Hydra session JSONL.

Reads pi session transcripts at {sessions-dir}/<bead-id>/*.jsonl and produces
per-bead, per-chain, and per-campaign aggregates for turn-economy work
(context pack, prompt-tail, turn budgets, RTK keep/fix/remove decisions):

  - turns, input/output/cache token sums (usage fields)
  - zero-action turn rate (assistant turns with no toolCall / total)
  - repeat-read rate and command-rerun rate (per chain)
  - RTK recovery events (bash reruns; full-output re-opens after a
    "[RTK compacted output" tool result)
  - thinking vs answer character split
  - first-8-turns tool-mix histogram
  - per-phase (RED/GREEN/REFACTOR/SYNTAX/GATE) turn distributions p25/p50/p75/max

Guardrails (computed per campaign; block success claims when regressed):
  - gate first-try pass rate — must not fall vs the before cohort
  - empty-patch rate (sessions with no successful edit/write) — must not rise

Campaigns are time windows over session start timestamps:

  python3 scripts/turn_economy.py \
    --sessions-dir /opt/dev-tools/var/hydra/sessions \
    --campaign pre1=2026-08-01..2026-08-20 \
    --campaign pre2=2026-08-21..2026-08-31 \
    --campaign post1=2026-09-01..2026-09-15 \
    --compare-before pre1,pre2 --compare-after post1 \
    --out-json /tmp/te.json --out-md /tmp/te.md

Gate command prefixes come from HYDRA_GATE_CMDS (same JSON-array convention as
cmd/worker/session_stats.go) or --gate-cmds. Exit code 2 when a guardrail
regresses; 1 on usage/IO errors.

Fail-open by design (daemon telemetry, not a product gate): malformed lines
are skipped, unreadable files are logged to stderr and skipped.
"""

import argparse
import json
import os
import re
import statistics
import sys
from collections import Counter, defaultdict

MAX_LINE_BYTES = 50 * 1024 * 1024  # mirror cmd/worker/session_stats.go cap
PHASE_RE = re.compile(r"^\s*(RED|GREEN|REFACTOR|SYNTAX|GATE)\s*[:\-]", re.IGNORECASE)
RTK_MARKER = "[RTK compacted output"
EDIT_TOOLS = {"edit", "write", "multiedit", "apply_patch", "str_replace"}
READ_TOOLS = {"read", "read_file"}


def iter_entries(path):
    """Yield parsed JSONL entries, skipping malformed lines (fail-open)."""
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            for line in f:
                if len(line) > MAX_LINE_BYTES:
                    continue
                try:
                    yield json.loads(line)
                except json.JSONDecodeError:
                    continue
    except OSError as e:
        print(f"[turn_economy] skipping unreadable {path}: {e}", file=sys.stderr)


def _text_of(content):
    """Concatenate text payloads from a message content list."""
    if not isinstance(content, list):
        return ""
    out = []
    for block in content:
        if isinstance(block, dict) and block.get("type") == "text":
            out.append(block.get("text", ""))
    return "\n".join(out)


def parse_context_block(first_user_text):
    """Extract the daemon-injected Context JSON from the first user message.

    Reuses the same shape the worker consumes (task/target/criteria); the
    target.files list drives the target-files cohort split. Returns {} when
    the block is absent or malformed (fail-open).
    """
    m = re.search(r"```json\s*(\{.*?\})\s*```", first_user_text, re.DOTALL)
    if not m:
        return {}
    try:
        ctx = json.loads(m.group(1))
        return ctx if isinstance(ctx, dict) else {}
    except json.JSONDecodeError:
        return {}


def norm_cmd(cmd):
    """Normalize a bash command for rerun detection (whitespace-collapse)."""
    return re.sub(r"\s+", " ", cmd or "").strip()


class SessionMetrics:
    def __init__(self):
        self.turns = 0
        self.zero_action_turns = 0
        self.tokens = Counter()  # input/output/cacheRead/cacheWrite
        self.thinking_chars = 0
        self.answer_chars = 0
        self.tool_calls = Counter()
        self.first8_tool_mix = Counter()
        self.bash_cmds = []           # normalized, in order
        self.read_paths = []          # in order
        self.edit_successes = 0
        self.gate_attempts = []       # (prefix, is_error) in order
        self.rtk_compacted = 0
        self.rtk_reopens = 0
        self.error_turns = 0

    def to_dict(self):
        return {
            "turns": self.turns,
            "zero_action_turns": self.zero_action_turns,
            "tokens": dict(self.tokens),
            "thinking_chars": self.thinking_chars,
            "answer_chars": self.answer_chars,
            "tool_calls": dict(self.tool_calls),
            "first8_tool_mix": dict(self.first8_tool_mix),
            "bash_cmds": self.bash_cmds,
            "read_paths": self.read_paths,
            "edit_successes": self.edit_successes,
            "gate_attempts": self.gate_attempts,
            "rtk_compacted": self.rtk_compacted,
            "rtk_reopens": self.rtk_reopens,
            "error_turns": self.error_turns,
        }


def gate_prefixes_from_env():
    raw = os.environ.get("HYDRA_GATE_CMDS", "")
    if not raw:
        return []
    try:
        cmds = json.loads(raw)
    except json.JSONDecodeError:
        return []
    return [gate_prefix(c) for c in cmds if isinstance(c, str) and c.strip()]


def gate_prefix(cmd):
    """Mirror cmd/worker/session_stats.go gateCmdPrefix: command up to the
    first flag/placeholder token, whitespace-normalized."""
    tokens = [t for t in norm_cmd(cmd).split(" ") if t]
    kept = []
    for t in tokens:
        if t.startswith("-") or "./" in t or "..." in t or "{{" in t:
            break
        kept.append(t)
    return " ".join(kept)


def match_gate_prefix(cmd, prefixes):
    for p in prefixes:
        if cmd == p or cmd.startswith(p + " ") or cmd.startswith(p + " ./"):
            return p
    return None


def analyze_session(path, gate_prefixes=None):
    """Analyze one session JSONL file into (SessionMetrics, meta) or None."""
    gate_prefixes = gate_prefixes or []
    m = SessionMetrics()
    meta = {"bead_id": "", "started_at": "", "phase": "", "chain_subject": "",
            "target_files": [], "title": ""}

    pending_calls = {}   # toolCallId -> (name, args)
    compacted_paths = set()
    assistant_idx = 0

    for ent in iter_entries(path):
        etype = ent.get("type")
        if etype == "session":
            meta["bead_id"] = ent.get("id", "")
            meta["started_at"] = ent.get("timestamp", "")
        elif etype == "message":
            msg = ent.get("message", {})
            role = msg.get("role")
            if role == "user" and not meta["title"]:
                text = _text_of(msg.get("content"))
                meta["title"] = text.split("\n", 1)[0][:200]
                ctx = parse_context_block(text)
                tgt = ctx.get("target") or {}
                files = tgt.get("files") or []
                if isinstance(files, list):
                    meta["target_files"] = [f for f in files if isinstance(f, str)]
                task = ctx.get("task") or {}
                # Phase from the daemon's "task: <PHASE>: <subject>" first
                # line, else the Context task.phase field, else unprefixed.
                line = re.sub(r"^\s*task\s*:\s*", "", meta["title"],
                              flags=re.IGNORECASE)
                pm = PHASE_RE.match(line)
                if pm:
                    meta["phase"] = pm.group(1).upper()
                    meta["chain_subject"] = re.sub(
                        r"^\s*" + re.escape(pm.group(1)) + r"\s*[:\-]\s*", "", line,
                        flags=re.IGNORECASE).strip().lower()
                else:
                    ph = task.get("phase")
                    if isinstance(ph, str) and ph.strip():
                        meta["phase"] = ph.strip().upper()
                    meta["chain_subject"] = (task.get("title") or line).strip().lower()
            elif role == "assistant":
                m.turns += 1
                assistant_idx += 1
                if msg.get("errorMessage") or msg.get("stopReason") == "error":
                    m.error_turns += 1
                usage = msg.get("usage") or {}
                for k in ("input", "output", "cacheRead", "cacheWrite"):
                    v = usage.get(k)
                    if isinstance(v, (int, float)):
                        m.tokens[k] += int(v)
                content = msg.get("content") or []
                if not isinstance(content, list):
                    content = []
                n_calls = 0
                for block in content:
                    if not isinstance(block, dict):
                        continue
                    bt = block.get("type")
                    if bt == "thinking":
                        m.thinking_chars += len(
                            block.get("thinking") or block.get("text") or "")
                    elif bt == "text":
                        m.answer_chars += len(block.get("text", "") or "")
                    elif bt == "toolCall":
                        n_calls += 1
                        name = block.get("name", "?")
                        m.tool_calls[name] += 1
                        if assistant_idx <= 8:
                            m.first8_tool_mix[name] += 1
                        args = block.get("arguments") or {}
                        cid = block.get("id")
                        if isinstance(cid, str):
                            pending_calls[cid] = (name, args)
                        if name == "bash":
                            cmd = norm_cmd(args.get("command", ""))
                            m.bash_cmds.append(cmd)
                        elif name in READ_TOOLS:
                            p = args.get("path") or args.get("file_path") or ""
                            if isinstance(p, str) and p:
                                m.read_paths.append(p)
                if n_calls == 0:
                    m.zero_action_turns += 1
            elif role == "toolResult":
                cid = msg.get("toolCallId")
                info = pending_calls.pop(cid, (msg.get("toolName", "?"), {}))
                name, args = info
                is_err = bool(msg.get("isError"))
                text = _text_of(msg.get("content"))
                if RTK_MARKER in text:
                    m.rtk_compacted += 1
                    p = args.get("path") or args.get("file_path") or ""
                    if isinstance(p, str) and p:
                        compacted_paths.add(p)
                elif name in READ_TOOLS and not is_err:
                    p = args.get("path") or args.get("file_path") or ""
                    if p and p in compacted_paths:
                        m.rtk_reopens += 1
                        compacted_paths.discard(p)
                lname = (name or "").lower()
                if not is_err and (lname in EDIT_TOOLS or "edit" in lname or lname == "write"):
                    m.edit_successes += 1
                if name == "bash" and not is_err:
                    cmd = norm_cmd(args.get("command", ""))
                    gp = match_gate_prefix(cmd, gate_prefixes)
                    if gp:
                        m.gate_attempts.append((gp, False))
                elif name == "bash" and is_err:
                    cmd = norm_cmd(args.get("command", ""))
                    gp = match_gate_prefix(cmd, gate_prefixes)
                    if gp:
                        m.gate_attempts.append((gp, True))

    if not m.turns and not meta["started_at"]:
        return None
    return m, meta


def session_gate_first_try(m):
    """True if every gate prefix's first attempt passed; None if no gates."""
    seen = {}
    for prefix, is_err in m.gate_attempts:
        if prefix not in seen:
            seen[prefix] = is_err
    if not seen:
        return None
    return not any(seen.values())


def percentile(sorted_vals, p):
    if not sorted_vals:
        return None
    k = (len(sorted_vals) - 1) * p
    f = int(k)
    c = min(f + 1, len(sorted_vals) - 1)
    if f == c:
        return sorted_vals[f]
    return sorted_vals[f] + (sorted_vals[c] - sorted_vals[f]) * (k - f)


def dist(vals):
    vals = sorted(vals)
    return {"n": len(vals), "p25": percentile(vals, 0.25),
            "p50": percentile(vals, 0.50), "p75": percentile(vals, 0.75),
            "max": vals[-1] if vals else None}


def chain_key(meta):
    subj = meta["chain_subject"] or meta["bead_id"]
    # worktree/cwd prefixes vary per bead; subject is the stable chain handle
    return re.sub(r"\s+", " ", subj).strip()[:120]


def cohort_report(name, sessions):
    """sessions: list of (metrics, meta). Aggregate to a campaign report."""
    rep = {"campaign": name, "sessions": len(sessions), "chains": 0,
           "turns": dist([]), "zero_action_rate": None,
           "tokens": {}, "thinking_chars": 0, "answer_chars": 0,
           "tool_mix": Counter(), "first8_tool_mix": Counter(),
           "rtk_compacted": 0, "rtk_reopens": 0,
           "repeat_read_rate": None, "cmd_rerun_rate": None,
           "phases": {}, "guardrails": {}, "target_files_cohort": {}}

    all_turns, zero, tokens = [], 0, Counter()
    reads_total, reads_repeated = 0, 0
    cmds_total, cmds_rerun = 0, 0
    gate_pass = gate_n = 0
    empty_patch = 0
    tf_turns, nontf_turns = [], []

    chains = defaultdict(list)
    for m, meta in sessions:
        chains[chain_key(meta)].append((m, meta))

    for m, meta in sessions:
        all_turns.append(m.turns)
        zero += m.zero_action_turns
        tokens.update(m.tokens)
        rep["tool_mix"].update(m.tool_calls)
        rep["first8_tool_mix"].update(m.first8_tool_mix)
        rep["rtk_compacted"] += m.rtk_compacted
        rep["rtk_reopens"] += m.rtk_reopens
        rep["thinking_chars"] += m.thinking_chars
        rep["answer_chars"] += m.answer_chars
        g = session_gate_first_try(m)
        if g is not None:
            gate_n += 1
            gate_pass += 1 if g else 0
        if m.edit_successes == 0:
            empty_patch += 1
        if meta["target_files"]:
            tf_turns.append(m.turns)
        else:
            nontf_turns.append(m.turns)

    for ckey, members in chains.items():
        read_counts = Counter()
        cmd_counts = Counter()
        for m, _ in members:
            read_counts.update(m.read_paths)
            cmd_counts.update(c for c in m.bash_cmds if c)
        reads_total += sum(read_counts.values())
        reads_repeated += sum(v for v in read_counts.values() if v > 1)
        cmds_total += sum(cmd_counts.values())
        cmds_rerun += sum(v for v in cmd_counts.values() if v > 1)
    rep["chains"] = len(chains)

    rep["turns"] = dist(all_turns)
    rep["tokens"] = dict(tokens)
    rep["zero_action_rate"] = (zero / sum(all_turns)) if sum(all_turns) else None
    rep["repeat_read_rate"] = (reads_repeated / reads_total) if reads_total else None
    rep["cmd_rerun_rate"] = (cmds_rerun / cmds_total) if cmds_total else None

    phases = defaultdict(list)
    for m, meta in sessions:
        phases[meta["phase"] or "UNSPECIFIED"].append(m.turns)
    rep["phases"] = {p: dist(v) for p, v in sorted(phases.items())}

    rep["guardrails"] = {
        "gate_first_try_pass_rate": (gate_pass / gate_n) if gate_n else None,
        "gate_sessions": gate_n,
        "empty_patch_rate": (empty_patch / len(sessions)) if sessions else None,
    }
    rep["target_files_cohort"] = {
        "with_target_files": dist(tf_turns),
        "without_target_files": dist(nontf_turns),
    }

    # JSON-serialize Counters
    rep["tool_mix"] = dict(rep["tool_mix"])
    rep["first8_tool_mix"] = dict(rep["first8_tool_mix"])
    return rep


def compare(before, after):
    """Guardrail comparison; returns list of regression strings."""
    regs = []
    bp = before["guardrails"]["gate_first_try_pass_rate"]
    ap = after["guardrails"]["gate_first_try_pass_rate"]
    if bp is not None and ap is not None and ap < bp:
        regs.append(
            f"gate first-try pass rate fell: {bp:.3f} -> {ap:.3f}")
    be = before["guardrails"]["empty_patch_rate"]
    ae = after["guardrails"]["empty_patch_rate"]
    if be is not None and ae is not None and ae > be:
        regs.append(
            f"empty-patch rate rose: {be:.3f} -> {ae:.3f}")
    return regs


def fmt_rate(v):
    return f"{v:.3f}" if isinstance(v, float) else "n/a"


def render_markdown(report):
    lines = ["# Turn-Economy Report", ""]
    cmp = report.get("comparison")
    for c in report["campaigns"]:
        lines.append(f"## Campaign {c['campaign']}")
        lines.append("")
        lines.append(f"- sessions: {c['sessions']} (chains: {c['chains']})")
        t = c["turns"]
        lines.append(f"- turns p25/p50/p75/max: {t['p25']}/{t['p50']}/{t['p75']}/{t['max']} (n={t['n']})")
        lines.append(f"- zero-action turn rate: {fmt_rate(c['zero_action_rate'])}")
        lines.append(f"- repeat-read rate: {fmt_rate(c['repeat_read_rate'])}  "
                     f"cmd-rerun rate: {fmt_rate(c['cmd_rerun_rate'])}")
        tk = c["tokens"]
        lines.append(f"- tokens: in={tk.get('input',0)} out={tk.get('output',0)} "
                     f"cacheRead={tk.get('cacheRead',0)} cacheWrite={tk.get('cacheWrite',0)}")
        think = c["thinking_chars"]
        total_chars = think + c["answer_chars"]
        share = (think / total_chars) if total_chars else 0
        lines.append(f"- thinking/answer char split: {share:.2f}/{1-share:.2f}")
        lines.append(f"- RTK: compacted={c['rtk_compacted']} reopens={c['rtk_reopens']}")
        g = c["guardrails"]
        lines.append(f"- guardrails: gate first-try pass={fmt_rate(g['gate_first_try_pass_rate'])} "
                     f"(n={g['gate_sessions']}), empty-patch={fmt_rate(g['empty_patch_rate'])}")
        tf = c["target_files_cohort"]
        if tf["with_target_files"]["n"]:
            lines.append(f"- target-files cohort turns p50: {tf['with_target_files']['p50']} "
                         f"(n={tf['with_target_files']['n']}) vs others {tf['without_target_files']['p50']} "
                         f"(n={tf['without_target_files']['n']})")
        lines.append("")
        lines.append("| phase | n | p25 | p50 | p75 | max |")
        lines.append("|---|---|---|---|---|---|")
        for p, d in c["phases"].items():
            lines.append(f"| {p} | {d['n']} | {d['p25']} | {d['p50']} | {d['p75']} | {d['max']} |")
        lines.append("")
    if cmp:
        lines.append("## Before/After comparison")
        lines.append("")
        for k in ("turns", "zero_action_rate", "repeat_read_rate", "cmd_rerun_rate"):
            b, a = cmp["before"].get(k), cmp["after"].get(k)
            if isinstance(b, dict):
                b, a = b.get("p50"), a.get("p50")
            lines.append(f"- {k}: {b} -> {a}")
        lines.append("")
        if cmp["regressions"]:
            lines.append("**GUARDRAIL REGRESSIONS — success claim blocked:**")
            for r in cmp["regressions"]:
                lines.append(f"- {r}")
        else:
            lines.append("Guardrails: no regression detected.")
        lines.append("")
    return "\n".join(lines)


def parse_campaign_spec(spec):
    name, _, rng = spec.partition("=")
    start, sep, end = rng.partition("..")
    if not name or not sep:
        raise ValueError(f"bad --campaign (want NAME=START..END): {spec}")
    return name.strip(), start.strip(), end.strip()


def in_range(ts, start, end):
    # ISO-8601 timestamps compare lexicographically
    return (not start or ts >= start) and (not end or ts < end)


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--sessions-dir", required=True)
    ap.add_argument("--campaign", action="append", default=[],
                    help="NAME=START..END (ISO timestamps; empty bound open)")
    ap.add_argument("--compare-before", help="comma-separated campaign names")
    ap.add_argument("--compare-after", help="comma-separated campaign names")
    ap.add_argument("--gate-cmds", help="JSON array of gate commands (default: HYDRA_GATE_CMDS)")
    ap.add_argument("--out-json")
    ap.add_argument("--out-md")
    ap.add_argument("--quiet", action="store_true")
    args = ap.parse_args(argv)

    if args.gate_cmds:
        try:
            gate_prefixes = [gate_prefix(c) for c in json.loads(args.gate_cmds)]
        except json.JSONDecodeError as e:
            print(f"--gate-cmds not valid JSON: {e}", file=sys.stderr)
            return 1
    else:
        gate_prefixes = gate_prefixes_from_env()

    campaigns = [parse_campaign_spec(s) for s in args.campaign]
    if not campaigns:
        campaigns = [("all", "", "")]

    by_campaign = {name: [] for name, _, _ in campaigns}
    unmatched = 0

    try:
        bead_dirs = sorted(os.listdir(args.sessions_dir))
    except OSError as e:
        print(f"cannot list {args.sessions_dir}: {e}", file=sys.stderr)
        return 1

    for bead in bead_dirs:
        sdir = os.path.join(args.sessions_dir, bead)
        if not os.path.isdir(sdir):
            continue
        for fn in sorted(os.listdir(sdir)):
            if not fn.endswith(".jsonl"):
                continue
            path = os.path.join(sdir, fn)
            res = analyze_session(path, gate_prefixes)
            if res is None:
                unmatched += 1
                continue
            m, meta = res
            if not meta["bead_id"]:
                meta["bead_id"] = bead
            matched = False
            for name, start, end in campaigns:
                if in_range(meta["started_at"], start, end):
                    by_campaign[name].append((m, meta))
                    matched = True
            if not matched:
                unmatched += 1

    report = {"sessions_dir": args.sessions_dir,
              "campaigns": [cohort_report(n, by_campaign[n]) for n, _, _ in campaigns],
              "unmatched_sessions": unmatched}

    if args.compare_before and args.compare_after:
        def merged(names):
            sess = []
            for n in names.split(","):
                n = n.strip()
                if n not in by_campaign:
                    print(f"unknown campaign in comparison: {n}", file=sys.stderr)
                    return None
                sess.extend(by_campaign[n])
            return cohort_report("+".join(names.split(",")), sess) if sess else None

        b, a = merged(args.compare_before), merged(args.compare_after)
        if b is not None and a is not None:
            regs = compare(b, a)
            report["comparison"] = {
                "before": {k: b[k] for k in
                           ("turns", "zero_action_rate", "repeat_read_rate",
                            "cmd_rerun_rate", "guardrails")},
                "after": {k: a[k] for k in
                          ("turns", "zero_action_rate", "repeat_read_rate",
                           "cmd_rerun_rate", "guardrails")},
                "regressions": regs,
            }

    md = render_markdown(report)
    if not args.quiet:
        print(md)
    if args.out_json:
        with open(args.out_json, "w") as f:
            json.dump(report, f, indent=2, default=str)
    if args.out_md:
        with open(args.out_md, "w") as f:
            f.write(md + "\n")

    regs = (report.get("comparison") or {}).get("regressions") or []
    return 2 if regs else 0


if __name__ == "__main__":
    sys.exit(main())
