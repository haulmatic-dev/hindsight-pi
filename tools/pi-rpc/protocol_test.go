package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUIRequestNeedsReply(t *testing.T) {
	asks := []string{"select", "confirm", "input", "editor"}
	notifications := []string{"notify", "setStatus", "setWidget", "setTitle", "set_editor_text"}
	for _, m := range asks {
		if !UIRequestNeedsReply(m) {
			t.Errorf("%q must be treated as a blocking ask", m)
		}
	}
	for _, m := range notifications {
		if UIRequestNeedsReply(m) {
			t.Errorf("%q is fire-and-forget; replying to it corrupts the stream", m)
		}
	}
}

func TestUIReply_DeniesByDefault(t *testing.T) {
	b, err := UIReply("id1", "confirm", false)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["confirmed"] != false || m["id"] != "id1" || m["type"] != "extension_ui_response" {
		t.Errorf("confirm must default to denied: %s", b)
	}
	b, _ = UIReply("id2", "select", false)
	_ = json.Unmarshal(b, &m)
	if m["cancelled"] != true {
		t.Errorf("select must cancel: %s", b)
	}
}

func TestUIReply_RejectsNotifications(t *testing.T) {
	if _, err := UIReply("x", "setStatus", false); err == nil {
		t.Error("must refuse to build a reply for a notification")
	}
}

// The regression that matters: pi reports model failures inside the message,
// so a client watching only the envelope calls a failed turn a success.
func TestMessageError_CatchesModelFailureInsideMessage(t *testing.T) {
	line := []byte(`{"type":"turn_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"401 authentication_error"}}`)
	ev, err := UnmarshalEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if got := ev.MessageError(); !strings.Contains(got, "401") {
		t.Errorf("MessageError() = %q, want the 401 surfaced", got)
	}
}

func TestMessageError_QuietOnHealthyTurn(t *testing.T) {
	line := []byte(`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"stopReason":"stop"}}`)
	ev, _ := UnmarshalEvent(line)
	if got := ev.MessageError(); got != "" {
		t.Errorf("healthy turn must report no error, got %q", got)
	}
}

func TestAssistantText_AndToolUseCount(t *testing.T) {
	line := []byte(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hi "},{"type":"tool_use","name":"bash"},{"type":"text","text":"there"}]}}`)
	ev, _ := UnmarshalEvent(line)
	txt, tools := ev.AssistantText()
	if txt != "hi there" || tools != 1 {
		t.Errorf("got (%q, %d), want (\"hi there\", 1)", txt, tools)
	}
}

func TestAssistantText_IgnoresUserMessages(t *testing.T) {
	line := []byte(`{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"prompt"}]}}`)
	ev, _ := UnmarshalEvent(line)
	if txt, _ := ev.AssistantText(); txt != "" {
		t.Errorf("user message must not be reported as assistant text: %q", txt)
	}
}

func TestCommandEncoders(t *testing.T) {
	b, _ := PromptCommand("do it")
	if !strings.Contains(string(b), `"type":"prompt"`) || !strings.Contains(string(b), `"message":"do it"`) {
		t.Errorf("bad prompt encoding: %s", b)
	}
	b, _ = SimpleCommand("get_state")
	if string(b) != `{"type":"get_state"}` {
		t.Errorf("bad simple command: %s", b)
	}
	b, _ = FollowUpCommand("more")
	if !strings.Contains(string(b), `"type":"follow_up"`) {
		t.Errorf("bad follow_up: %s", b)
	}
}

func TestIsTerminal(t *testing.T) {
	ev, _ := UnmarshalEvent([]byte(`{"type":"agent_end"}`))
	if !ev.IsTerminal() {
		t.Error("agent_end must be terminal")
	}
	ev, _ = UnmarshalEvent([]byte(`{"type":"turn_end"}`))
	if ev.IsTerminal() {
		t.Error("turn_end is not the end of the agent run")
	}
}
