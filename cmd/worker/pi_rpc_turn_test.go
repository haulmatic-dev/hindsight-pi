package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestPiConfig_RPCEnabled(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"rpc", true},
		{"RPC", true},
		{" rpc ", true},
		{"text", false},
		{"banana", false},
		{"", false},
		// Guards against a half-match enabling the mode under test.
		{"rpcx", false},
	}
	for _, tc := range cases {
		if got := (PiConfig{Mode: tc.mode}).RPCEnabled(); got != tc.want {
			t.Errorf("RPCEnabled(mode=%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestParsePiConfig_ModeAndAutoConfirm(t *testing.T) {
	t.Setenv("HYDRA_PI_MODE", "rpc")
	t.Setenv("HYDRA_PI_RPC_AUTO_CONFIRM", "true")
	cfg := parsePiConfig()
	if !cfg.RPCEnabled() {
		t.Errorf("HYDRA_PI_MODE=rpc must enable rpc mode, got Mode=%q", cfg.Mode)
	}
	if !cfg.RPCAutoConfirm {
		t.Error("HYDRA_PI_RPC_AUTO_CONFIRM=true must set RPCAutoConfirm")
	}
}

func TestParsePiConfig_DefaultsToTextAndDeniesConfirms(t *testing.T) {
	t.Setenv("HYDRA_PI_MODE", "")
	t.Setenv("HYDRA_PI_RPC_AUTO_CONFIRM", "")
	cfg := parsePiConfig()
	if cfg.RPCEnabled() {
		t.Error("unset HYDRA_PI_MODE must keep the existing text mode")
	}
	if cfg.RPCAutoConfirm {
		t.Error("unset HYDRA_PI_RPC_AUTO_CONFIRM must deny confirms by default")
	}
	// An unparseable value must not be read as opting in.
	t.Setenv("HYDRA_PI_RPC_AUTO_CONFIRM", "sure-why-not")
	if parsePiConfig().RPCAutoConfirm {
		t.Error("an unrecognised HYDRA_PI_RPC_AUTO_CONFIRM must deny, not approve")
	}
}

// TestBuildPiCommand_ModeFlag pins the actual argv, which is the only thing that
// makes Pi initialise its extensions. Text mode must stay byte-for-byte as it
// was, because that is what every production run uses today.
func TestBuildPiCommand_ModeFlag(t *testing.T) {
	bead := types.Bead{ID: types.BeadID("b1"), Title: "t", Type: "task"}

	// Match argv elements exactly. A substring check on the joined args is not
	// good enough: "--model" contains "--mode", so a naive Contains reports the
	// flag as present in text mode too.
	modeValue := func(args []string) (string, bool) {
		for i, a := range args {
			if a == "--mode" {
				if i+1 < len(args) {
					return args[i+1], true
				}
				return "", true
			}
		}
		return "", false
	}

	rpcCmd := buildPiCommand(bead, PiConfig{Mode: "rpc", Provider: "p", Model: "m"}, t.TempDir(), nil)
	got, present := modeValue(rpcCmd.Args)
	if !present || got != "rpc" {
		t.Errorf("rpc mode must pass --mode rpc, got args: %v", rpcCmd.Args)
	}

	textCmd := buildPiCommand(bead, PiConfig{Provider: "p", Model: "m"}, t.TempDir(), nil)
	if _, present := modeValue(textCmd.Args); present {
		t.Errorf("text mode must not pass --mode at all, got args: %v", textCmd.Args)
	}
}

func TestRPCPromptLine(t *testing.T) {
	line, err := rpcPromptLine("do the thing")
	if err != nil {
		t.Fatalf("rpcPromptLine: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("prompt line is not valid JSON: %v", err)
	}
	if got["type"] != "prompt" || got["message"] != "do the thing" {
		t.Errorf("unexpected prompt command: %s", line)
	}
}

func TestRPCTurn_AgentEndSucceeds(t *testing.T) {
	turn := newRPCTurn(false)
	if done := turn.handleLine(`{"type":"agent_start"}`, nil); done {
		t.Error("agent_start must not end the turn")
	}
	if done := turn.handleLine(`{"type":"agent_end","messages":[{"stopReason":"end_turn"}]}`, nil); !done {
		t.Fatal("agent_end must end the turn")
	}
	if !turn.Ended() || turn.Failed() {
		t.Errorf("clean agent_end: Ended=%v Failed=%v, want true/false", turn.Ended(), turn.Failed())
	}
}

// TestRPCTurn_AgentEndCarryingModelError is the regression that matters most.
// Pi reports model failures INSIDE the message, not in the envelope, so a reader
// that only checks for agent_end reports an expired API key as a passing turn —
// exactly what tools/pi-rpc did before this was handled. A worker doing that
// would close beads whose model call never ran.
func TestRPCTurn_AgentEndCarryingModelError(t *testing.T) {
	for _, stopReason := range []string{"error", "aborted"} {
		turn := newRPCTurn(false)
		line := `{"type":"agent_end","messages":[{"stopReason":"` + stopReason +
			`","errorMessage":"401 invalid api key"}]}`
		if done := turn.handleLine(line, nil); !done {
			t.Fatalf("%s: agent_end must end the turn", stopReason)
		}
		if !turn.Failed() {
			t.Errorf("%s: a turn carrying stopReason=%s must be a FAILURE, not a pass", stopReason, stopReason)
		}
		if !strings.Contains(turn.ErrMsg(), "401") {
			t.Errorf("%s: errorMessage must be surfaced, got %q", stopReason, turn.ErrMsg())
		}
	}
}

func TestRPCTurn_ErrorEventFails(t *testing.T) {
	turn := newRPCTurn(false)
	if done := turn.handleLine(`{"type":"error","error":{"errorMessage":"boom"}}`, nil); !done {
		t.Fatal("an error event must end the turn")
	}
	if !turn.Failed() || turn.ErrMsg() != "boom" {
		t.Errorf("Failed=%v ErrMsg=%q, want true/\"boom\"", turn.Failed(), turn.ErrMsg())
	}
}

func TestRPCTurn_ConfirmDeniedByDefault(t *testing.T) {
	var stdin bytes.Buffer
	turn := newRPCTurn(false)
	turn.handleLine(`{"type":"extension_ui_request","id":"u1","method":"confirm"}`, &stdin)

	var reply map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdin.Bytes()), &reply); err != nil {
		t.Fatalf("no valid reply written for a blocking confirm: %v (raw %q)", err, stdin.String())
	}
	if reply["type"] != "extension_ui_response" || reply["id"] != "u1" {
		t.Errorf("unexpected reply envelope: %s", stdin.String())
	}
	if reply["confirmed"] != false {
		t.Errorf("confirm must be DENIED by default, got %s", stdin.String())
	}
	if turn.UIAnswered() != 1 {
		t.Errorf("UIAnswered = %d, want 1", turn.UIAnswered())
	}
}

func TestRPCTurn_ConfirmAutoApprovedWhenConfigured(t *testing.T) {
	var stdin bytes.Buffer
	newRPCTurn(true).handleLine(`{"type":"extension_ui_request","id":"u2","method":"confirm"}`, &stdin)
	var reply map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdin.Bytes()), &reply); err != nil {
		t.Fatalf("bad reply: %v", err)
	}
	if reply["confirmed"] != true {
		t.Errorf("autoConfirm must approve, got %s", stdin.String())
	}
}

// TestRPCTurn_NotificationsAreNotAnswered guards the other half of the protocol:
// replying to a fire-and-forget notification desynchronises the stream.
func TestRPCTurn_NotificationsAreNotAnswered(t *testing.T) {
	for _, method := range []string{"notify", "setStatus", "setWidget", "setTitle", "set_editor_text"} {
		var stdin bytes.Buffer
		turn := newRPCTurn(false)
		line := `{"type":"extension_ui_request","id":"n1","method":"` + method + `"}`
		if done := turn.handleLine(line, &stdin); done {
			t.Errorf("%s: a notification must not end the turn", method)
		}
		if stdin.Len() != 0 {
			t.Errorf("%s: notifications must never be answered, wrote %q", method, stdin.String())
		}
	}
}

func TestRPCTurn_AccumulatesAssistantText(t *testing.T) {
	turn := newRPCTurn(false)
	turn.handleLine(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"Hello, "}}`, nil)
	turn.handleLine(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"world"}}`, nil)
	// A non-text_delta assistant event must contribute nothing.
	turn.handleLine(`{"type":"message_update","assistantMessageEvent":{"type":"tool_call"}}`, nil)
	if got := turn.Text(); got != "Hello, world" {
		t.Errorf("Text() = %q, want %q", got, "Hello, world")
	}
}

func TestRPCTurn_ParsesSessionStats(t *testing.T) {
	turn := newRPCTurn(false)
	turn.handleLine(`{"type":"response","data":{"tokens":{"input":120,"output":45,"cacheRead":7,"total":172}}}`, nil)
	stats := turn.Stats()
	if stats == nil {
		t.Fatal("get_session_stats response must populate Stats — token counts are what make a text-vs-rpc A/B possible")
	}
	if stats.Total != 172 || stats.Input != 120 || stats.Output != 45 || stats.CacheRead != 7 {
		t.Errorf("unexpected stats: %+v", *stats)
	}
}

func TestRPCTurn_KeepsNonJSONOutput(t *testing.T) {
	turn := newRPCTurn(false)
	if done := turn.handleLine("plain stderr-ish line", nil); done {
		t.Error("a non-JSON line must not end the turn")
	}
	if !strings.Contains(turn.Text(), "plain stderr-ish line") {
		t.Errorf("non-JSON output must be retained, got %q", turn.Text())
	}
	// Blank lines are noise, not output.
	turn2 := newRPCTurn(false)
	turn2.handleLine("   ", nil)
	if turn2.Text() != "" {
		t.Errorf("blank lines must be ignored, got %q", turn2.Text())
	}
}

// TestRPCTurn_FinishIsIdempotent: an error event arriving after agent_end must
// not trigger a second teardown (double kill / double stats request).
func TestRPCTurn_FinishIsIdempotent(t *testing.T) {
	var stdin nopWriteCloser
	turn := newRPCTurn(false)
	turn.handleLine(`{"type":"agent_end","messages":[{"stopReason":"end_turn"}]}`, &stdin)
	turn.finish(0, &stdin) // pid 0 => no signals sent
	before := stdin.writes
	turn.finish(0, &stdin)
	if stdin.writes != before {
		t.Errorf("finish must be idempotent: writes went %d -> %d", before, stdin.writes)
	}
}

type nopWriteCloser struct {
	buf    bytes.Buffer
	writes int
	closed bool
}

func (n *nopWriteCloser) Write(p []byte) (int, error) {
	n.writes++
	return n.buf.Write(p)
}

func (n *nopWriteCloser) Close() error {
	n.closed = true
	return nil
}
