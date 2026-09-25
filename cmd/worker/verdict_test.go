package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVerdictDone(t *testing.T) {
	text := `I implemented the endpoint and ran the tests.

HYDRA_VERDICT: {"done": true, "ac_met": ["req-001"], "ac_unmet": [],
"files_changed": ["main.go", "main_test.go"],
"verification": [{"cmd": "go test ./...", "result": "ok"}]}`
	v := parseVerdict(text)
	if v == nil {
		t.Fatal("no verdict parsed")
	}
	if !v.Done || len(v.ACUnmet) != 0 || len(v.FilesChanged) != 2 {
		t.Errorf("wrong verdict: %+v", v)
	}
}

func TestParseVerdictLastMatchWins(t *testing.T) {
	text := `HYDRA_VERDICT: {"done": false, "ac_unmet": ["req-002"], "summary": "early state"}
... more work ...
HYDRA_VERDICT: {"done": true, "ac_met": ["req-001", "req-002"], "ac_unmet": []}`
	v := parseVerdict(text)
	if v == nil || !v.Done {
		t.Fatalf("last verdict should win: %+v", v)
	}
}

func TestParseVerdictMissing(t *testing.T) {
	if v := parseVerdict("some prose with no verdict"); v != nil {
		t.Errorf("expected nil, got %+v", v)
	}
}

func TestParseVerdictMalformed(t *testing.T) {
	if v := parseVerdict("HYDRA_VERDICT: {not json}"); v != nil {
		t.Errorf("malformed verdict must be ignored, got %+v", v)
	}
}

func TestParseVerdictAbandon(t *testing.T) {
	text := `HYDRA_VERDICT: {"done": false, "summary": "implemented 2/5 ACs; req-003 blocked on schema decision"}`
	v := parseVerdict(text)
	if v == nil || v.Done || v.Summary == "" {
		t.Fatalf("abandon verdict: %+v", v)
	}
}

func TestNormalizeVerdictHollowGreen(t *testing.T) {
	v := parseVerdict(`HYDRA_VERDICT: {"done": true, "ac_met": ["exit 0, 9 tests"], "ac_unmet": [],
"verification": [{"cmd": "npx jest", "result": "exit 0, 9 tests (it.failing)"}, {"cmd": "npx jest e2e", "result": "Cannot find module '../e2e-harness'"}]}`)
	if v == nil || !v.Done {
		t.Fatalf("verdict should parse as done before normalization: %+v", v)
	}
	nv := normalizeVerdict(v)
	if nv.Done {
		t.Errorf("done verdict with load-crash evidence must be downgraded, got done=true")
	}
	if !nv.HollowGreen {
		t.Errorf("HollowGreen flag not set")
	}
	if len(nv.ACUnmet) == 0 {
		t.Errorf("load errors must be recorded as unmet ACs")
	}
}

func TestNormalizeVerdictCleanPassUnchanged(t *testing.T) {
	v := parseVerdict(`HYDRA_VERDICT: {"done": true, "verification": [{"cmd": "go test ./...", "result": "ok 0.5s, 17 passed"}]}`)
	nv := normalizeVerdict(v)
	if !nv.Done || nv.HollowGreen || len(nv.ACUnmet) != 0 {
		t.Errorf("clean pass must not be downgraded: %+v", nv)
	}
}

func TestDecideTurnEnd(t *testing.T) {
	cases := []struct {
		name       string
		verdict    string
		nudges     int
		wantNudge  bool
		wantReason string
	}{
		{"done verdict no nudge", `{"done": true, "ac_unmet": []}`, 0, false, ""},
		{"missing verdict nudges", ``, 0, true, "verdict missing"},
		{"unmet ACs nudge with list", `{"done": true, "ac_unmet": ["req-003"]}`, 0, true, "req-003"},
		{"abandon does not nudge", `{"done": false, "summary": "blocked"}`, 0, false, ""},
		{"cap stops nudging", ``, 3, false, ""},
		{"hollow-green nudges with reason", `{"done": false, "hollow_green": true, "ac_unmet": ["load errors"], "summary": "downgraded"}`, 0, true, "hollow-green"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var text string
			if tc.verdict != "" {
				text = "HYDRA_VERDICT: " + tc.verdict
			}
			nudge, reason := decideTurnEnd(parseVerdict(text), tc.nudges, verdictNudgeCap)
			if nudge != tc.wantNudge {
				t.Errorf("nudge = %v, want %v", nudge, tc.wantNudge)
			}
			if tc.wantReason != "" && !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason %q missing %q", reason, tc.wantReason)
			}
		})
	}
}

func TestNudgePromptContent(t *testing.T) {
	p := buildNudgePrompt("verdict missing — emit HYDRA_VERDICT or continue working", 1, PiConfig{})
	if !strings.Contains(p, "HYDRA_VERDICT") {
		t.Errorf("nudge prompt must demand the verdict: %q", p)
	}
	if !json.Valid([]byte(mustRPC(t, p))) {
		t.Errorf("nudge must encode as rpc prompt line")
	}
}

func mustRPC(t *testing.T, prompt string) string {
	t.Helper()
	b, err := rpcPromptLine(prompt)
	if err != nil {
		t.Fatalf("rpcPromptLine: %v", err)
	}
	return string(b)
}
