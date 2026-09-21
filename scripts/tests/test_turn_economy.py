#!/usr/bin/env python3
"""Unit tests for scripts/turn_economy.py over fixture session JSONL.

Run: python3 -m unittest discover -s scripts/tests -v
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

import turn_economy as te


def session_header(bead_id, ts):
    return {"type": "session", "version": 3, "id": bead_id, "timestamp": ts,
            "cwd": "/tmp/wt/" + bead_id}


def user_msg(text, ts="2026-09-01T00:00:01Z"):
    return {"type": "message", "timestamp": ts,
            "message": {"role": "user",
                        "content": [{"type": "text", "text": text}]}}


def assistant_msg(usage, blocks, stop="toolUse", ts="2026-09-01T00:00:02Z",
                  err=None):
    msg = {"role": "assistant", "content": blocks, "usage": usage,
           "stopReason": stop, "timestamp": ts}
    if err:
        msg["errorMessage"] = err
    return {"type": "message", "timestamp": ts, "message": msg}


def tool_result(cid, name, text, is_err=False, ts="2026-09-01T00:00:03Z"):
    return {"type": "message", "timestamp": ts,
            "message": {"role": "toolResult", "toolCallId": cid,
                        "toolName": name,
                        "content": [{"type": "text", "text": text}],
                        "isError": is_err, "timestamp": ts}}


def bash_call(cid, cmd):
    return {"type": "toolCall", "id": cid, "name": "bash",
            "arguments": {"command": cmd}}


def read_call(cid, path):
    return {"type": "toolCall", "id": cid, "name": "read",
            "arguments": {"path": path}}


CONTEXT = """task: RED: Widget Parser

## Context

```json
{"task": {"title": "Widget Parser", "phase": "red"},
 "target": {"files": ["src/widget.ts"]},
 "criteria": {"parent": []}}
```

## Acceptance

make it so
"""


class TurnEconomyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.sessions = os.path.join(self.tmp.name, "sessions")
        os.makedirs(self.sessions)

    def tearDown(self):
        self.tmp.cleanup()

    def write_session(self, bead_id, entries, ts="2026-09-01T00:00:00Z"):
        d = os.path.join(self.sessions, bead_id)
        os.makedirs(d, exist_ok=True)
        path = os.path.join(d, f"{ts}_{bead_id}.jsonl")
        with open(path, "w") as f:
            for e in [session_header(bead_id, ts)] + entries:
                f.write(json.dumps(e) + "\n")
        return path

    def test_basic_counts_and_tokens(self):
        path = self.write_session("br-a", [
            user_msg(CONTEXT),
            assistant_msg({"input": 100, "output": 50, "cacheRead": 500,
                           "cacheWrite": 10},
                          [{"type": "thinking", "text": "hmm" * 10},
                           {"type": "toolCall", "id": "c1", "name": "bash",
                            "arguments": {"command": "go test ./..."}}]),
            tool_result("c1", "bash", "ok"),
            assistant_msg({"input": 10, "output": 5, "cacheRead": 0,
                           "cacheWrite": 0},
                          [{"type": "text", "text": "done"}],
                          stop="stop"),
        ])
        m, meta = te.analyze_session(path)
        self.assertEqual(m.turns, 2)
        self.assertEqual(m.zero_action_turns, 1)  # final text-only turn
        self.assertEqual(m.tokens["input"], 110)
        self.assertEqual(m.tokens["output"], 55)
        self.assertEqual(m.tokens["cacheRead"], 500)
        self.assertEqual(m.thinking_chars, 30)
        self.assertEqual(m.answer_chars, 4)
        self.assertEqual(meta["phase"], "RED")
        self.assertEqual(meta["chain_subject"], "widget parser")
        self.assertEqual(meta["target_files"], ["src/widget.ts"])

    def test_zero_action_and_error_turns(self):
        path = self.write_session("br-b", [
            user_msg("task: misc"),
            assistant_msg({}, [], stop="error", err="provider rejected"),
            assistant_msg({"input": 5, "output": 5},
                          [{"type": "text", "text": "nothing"}], stop="stop"),
        ])
        m, _ = te.analyze_session(path)
        self.assertEqual(m.turns, 2)
        self.assertEqual(m.zero_action_turns, 2)
        self.assertEqual(m.error_turns, 1)

    def test_first8_tool_mix_counts_only_first_eight(self):
        entries = [user_msg(CONTEXT)]
        for i in range(10):
            entries.append(assistant_msg(
                {"input": 1, "output": 1},
                [{"type": "toolCall", "id": f"c{i}", "name": "bash",
                  "arguments": {"command": f"echo {i}"}}]))
            entries.append(tool_result(f"c{i}", "bash", "ok"))
        path = self.write_session("br-c", entries)
        m, _ = te.analyze_session(path)
        self.assertEqual(m.tool_calls["bash"], 10)
        self.assertEqual(m.first8_tool_mix["bash"], 8)

    def test_rtk_reopen_after_compacted_read(self):
        compacted = te.RTK_MARKER + " source:minimal, smart-truncate"
        path = self.write_session("br-d", [
            user_msg(CONTEXT),
            assistant_msg({"input": 1, "output": 1},
                          [read_call("c1", "/big/file.go")]),
            tool_result("c1", "read", compacted),
            assistant_msg({"input": 1, "output": 1},
                          [read_call("c2", "/big/file.go")]),
            tool_result("c2", "read", "full content"),
        ])
        m, _ = te.analyze_session(path)
        self.assertEqual(m.rtk_compacted, 1)
        self.assertEqual(m.rtk_reopens, 1)

    def test_gate_first_try_pass_and_fail(self):
        gates = ["go test"]
        path = self.write_session("br-e", [
            user_msg(CONTEXT),
            assistant_msg({"input": 1, "output": 1},
                          [bash_call("c1", "go test ./... -count=1")]),
            tool_result("c1", "bash", "FAIL", is_err=True),
            assistant_msg({"input": 1, "output": 1},
                          [bash_call("c2", "go test ./... -count=1")]),
            tool_result("c2", "bash", "ok"),
        ])
        m, _ = te.analyze_session(path, gates)
        self.assertEqual(m.gate_attempts,
                         [("go test", True), ("go test", False)])
        self.assertFalse(te.session_gate_first_try(m))

        path2 = self.write_session("br-f", [
            user_msg(CONTEXT),
            assistant_msg({"input": 1, "output": 1},
                          [bash_call("c3", "go test ./...")]),
            tool_result("c3", "bash", "ok"),
        ], ts="2026-09-02T00:00:00Z")
        m2, _ = te.analyze_session(path2, gates)
        self.assertTrue(te.session_gate_first_try(m2))

    def test_edit_success_and_empty_patch(self):
        path = self.write_session("br-g", [
            user_msg(CONTEXT),
            assistant_msg({"input": 1, "output": 1},
                          [{"type": "toolCall", "id": "c1", "name": "edit",
                            "arguments": {"path": "a.go"}}]),
            tool_result("c1", "edit", "edited"),
        ])
        m, _ = te.analyze_session(path)
        self.assertEqual(m.edit_successes, 1)

    def test_cohort_report_and_guardrail_regression(self):
        self.write_session("br-h", [
            user_msg(CONTEXT),
            assistant_msg({"input": 10, "output": 5},
                          [bash_call("c1", "go test ./...")]),
            tool_result("c1", "bash", "ok"),
            assistant_msg({"input": 1, "output": 1},
                          [{"type": "toolCall", "id": "c2", "name": "edit",
                            "arguments": {"path": "x.go"}}]),
            tool_result("c2", "edit", "ok"),
        ], ts="2026-08-01T00:00:00Z")
        self.write_session("br-i", [
            user_msg(CONTEXT),
            assistant_msg({"input": 10, "output": 5},
                          [bash_call("c3", "go test ./...")]),
            tool_result("c3", "bash", "FAIL", is_err=True),
        ], ts="2026-09-01T00:00:00Z")

        import io
        old = sys.stdout
        sys.stdout = io.StringIO()
        try:
            rc = te.main([
                "--sessions-dir", self.sessions,
                "--campaign", "pre=2026-08-01..2026-09-01",
                "--campaign", "post=2026-09-01..2026-10-01",
                "--compare-before", "pre", "--compare-after", "post",
                "--gate-cmds", '["go test ./..."]',
                "--quiet",
            ])
        finally:
            sys.stdout = old
        self.assertEqual(rc, 2)  # gate pass rate fell 1.0 -> 0.0

    def test_malformed_lines_fail_open(self):
        path = self.write_session("br-j", [user_msg(CONTEXT)])
        with open(path, "a") as f:
            f.write("{not json\n")
            f.write('{"type":"message","message":{"role":"assistant",'
                    '"content":[{"type":"text","text":"x"}],'
                    '"usage":{"input":3,"output":2},"stopReason":"stop"}}\n')
        m, _ = te.analyze_session(path)
        self.assertEqual(m.turns, 1)
        self.assertEqual(m.tokens["input"], 3)

    def test_percentile_distribution(self):
        d = te.dist([1, 2, 3, 4])
        self.assertEqual(d["n"], 4)
        self.assertEqual(d["p50"], 2.5)
        self.assertEqual(d["max"], 4)
        self.assertEqual(te.dist([])["p50"], None)


if __name__ == "__main__":
    unittest.main()
