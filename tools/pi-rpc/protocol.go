package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Event is a decoded server→client JSONL message from `pi --mode rpc`. The
// protocol carries many message types (see pi's dist/modes/rpc/rpc-types.d.ts),
// so the shape is kept open and only the fields this client acts on are typed.
type Event struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Command string `json:"command,omitempty"`
	Success *bool  `json:"success,omitempty"`
	Error   string `json:"error,omitempty"`
	Raw     map[string]any
}

// UnmarshalEvent decodes one JSONL line, keeping the raw map for tracing.
func UnmarshalEvent(line []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(line, &e); err != nil {
		return Event{}, err
	}
	_ = json.Unmarshal(line, &e.Raw)
	return e, nil
}

// UIRequestNeedsReply reports whether an extension_ui_request method blocks on a
// client response. Per the protocol types, `select`, `confirm`, `input` and
// `editor` are asks; `notify`, `setStatus`, `setWidget`, `setTitle` and
// `set_editor_text` are fire-and-forget notifications that must NOT be answered.
func UIRequestNeedsReply(method string) bool {
	switch method {
	case "select", "confirm", "input", "editor":
		return true
	default:
		return false
	}
}

// UIReply builds the extension_ui_response for a blocking request.
//
// Policy: deny/cancel by default. A headless worker has no human to ask, so an
// interactive prompt reaching this client is itself the anomaly — answering
// "yes" could approve an action nobody reviewed. The caller logs every such
// request loudly; `--auto-confirm` flips confirmations to true for the cases
// where an operator has decided that is safe.
func UIReply(id, method string, autoConfirm bool) ([]byte, error) {
	if !UIRequestNeedsReply(method) {
		return nil, fmt.Errorf("method %q is a notification, not an ask", method)
	}
	resp := map[string]any{"type": "extension_ui_response", "id": id}
	switch method {
	case "confirm":
		resp["confirmed"] = autoConfirm
	default: // select, input, editor
		resp["cancelled"] = true
	}
	return json.Marshal(resp)
}

// PromptCommand is the initial turn request. Matches the format hydra's existing
// connector uses (internal/agents/pi.go): {"type":"prompt","message":"..."}.
func PromptCommand(message string) ([]byte, error) {
	return json.Marshal(map[string]any{"type": "prompt", "message": message})
}

// SimpleCommand builds a no-argument command such as get_state, abort,
// get_session_stats, get_last_assistant_text, cycle_model or compact.
func SimpleCommand(cmd string) ([]byte, error) {
	return json.Marshal(map[string]any{"type": cmd})
}

// FollowUpCommand continues an existing turn with more instruction — the
// capability that lets a stuck worker be nudged instead of killed and respawned.
func FollowUpCommand(message string) ([]byte, error) {
	return json.Marshal(map[string]any{"type": "follow_up", "message": message})
}

// IsTerminal reports whether an event ends the current agent turn.
func (e Event) IsTerminal() bool { return e.Type == "agent_end" }

// MessageError extracts a model-level failure from a message-bearing event.
// pi reports these INSIDE the message (`stopReason: "error"` plus
// `errorMessage`), not as a top-level protocol error — so a client that only
// watches the envelope will report a failed turn as a success. That is exactly
// the false-success this function exists to prevent: an expired API key
// produced `agent_end` with an empty assistant message and exit 0.
func (e Event) MessageError() string {
	msg, ok := e.Raw["message"].(map[string]any)
	if !ok {
		return ""
	}
	if s, _ := msg["errorMessage"].(string); s != "" {
		return s
	}
	if stop, _ := msg["stopReason"].(string); stop == "error" {
		return "stopReason=error (no errorMessage provided)"
	}
	return ""
}

// AssistantText concatenates the assistant text blocks of a message event, and
// reports how many tool_use blocks the model emitted.
func (e Event) AssistantText() (text string, toolUses int) {
	msg, ok := e.Raw["message"].(map[string]any)
	if !ok {
		return "", 0
	}
	if role, _ := msg["role"].(string); role != "assistant" {
		return "", 0
	}
	blocks, _ := msg["content"].([]any)
	var b strings.Builder
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "text":
			if s, _ := block["text"].(string); s != "" {
				b.WriteString(s)
			}
		case "tool_use", "toolUse":
			toolUses++
		}
	}
	return b.String(), toolUses
}
