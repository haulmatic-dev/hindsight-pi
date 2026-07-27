package agents

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUIRequestNeedsReply(t *testing.T) {
	asks := []string{"select", "confirm", "input", "editor"}
	for _, m := range asks {
		if !UIRequestNeedsReply(m) {
			t.Errorf("UIRequestNeedsReply(%q) = false, want true", m)
		}
	}

	notifications := []string{"notify", "setStatus", "setWidget", "setTitle", "set_editor_text"}
	for _, m := range notifications {
		if UIRequestNeedsReply(m) {
			t.Errorf("UIRequestNeedsReply(%q) = true, want false", m)
		}
	}
}

func TestUIResponseFor_ConfirmDenied(t *testing.T) {
	reply, ok := UIResponseFor("abc", "confirm", false)
	if !ok {
		t.Fatal("UIResponseFor(confirm, false) ok = false, want true")
	}
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(reply, &m))
	assert.Equal(t, "extension_ui_response", m["type"])
	assert.Equal(t, "abc", m["id"])
	assert.Equal(t, false, m["confirmed"])
}

func TestUIResponseFor_ConfirmAutoConfirm(t *testing.T) {
	reply, ok := UIResponseFor("abc", "confirm", true)
	if !ok {
		t.Fatal("UIResponseFor(confirm, true) ok = false, want true")
	}
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(reply, &m))
	assert.Equal(t, true, m["confirmed"])
	assert.Equal(t, "extension_ui_response", m["type"])
	assert.Equal(t, "abc", m["id"])
}

func TestUIResponseFor_SelectCancelled(t *testing.T) {
	reply, ok := UIResponseFor("abc", "select", false)
	if !ok {
		t.Fatal("UIResponseFor(select, false) ok = false, want true")
	}
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(reply, &m))
	assert.Equal(t, "extension_ui_response", m["type"])
	assert.Equal(t, "abc", m["id"])
	assert.Equal(t, true, m["cancelled"])
	_, hasConfirmed := m["confirmed"]
	assert.False(t, hasConfirmed, "select reply must not contain a confirmed key")
}

func TestUIResponseFor_Notification(t *testing.T) {
	reply, ok := UIResponseFor("abc", "setStatus", false)
	assert.False(t, ok, "setStatus is a notification and must not be answered")
	assert.Nil(t, reply)
}

func TestParseSessionStats(t *testing.T) {
	line := []byte(`{"type":"response","command":"get_session_stats","data":{"tokens":{"input":17474,"output":27,"cacheRead":256,"total":17757}},"success":true}`)
	stats, err := parseSessionStats(line)
	require.NoError(t, err)
	assert.Equal(t, SessionStats{Input: 17474, Output: 27, CacheRead: 256, Total: 17757}, stats)
}

func TestParseSessionStats_MissingTokens(t *testing.T) {
	line := []byte(`{"type":"response","command":"get_session_stats","data":{},"success":true}`)
	stats, err := parseSessionStats(line)
	require.Error(t, err)
	assert.Equal(t, SessionStats{}, stats)
	assert.True(t, strings.Contains(err.Error(), "tokens"), "error should mention missing tokens")
}

func TestSessionStats(t *testing.T) {
	// Set up pipes that simulate a running session.
	// connectorStdin is written by the connector and read by the fake server.
	stdinR, stdinW := io.Pipe()
	// connectorStdout is written by the fake server and read by the connector.
	stdoutR, stdoutW := io.Pipe()

	connector := NewPiConnector(PiConfig{}, nil)
	session := &AgentSession{
		metadata: map[string]interface{}{
			"stdin":  stdinW,
			"stdout": stdoutR,
		},
	}

	go func() {
		defer stdinR.Close()
		defer stdoutW.Close()

		// Read the get_session_stats command sent by the connector.
		buf := make([]byte, 1024)
		n, err := stdinR.Read(buf)
		if err != nil {
			return
		}

		// Verify the command shape.
		var cmd map[string]interface{}
		if err := json.Unmarshal(buf[:n], &cmd); err != nil {
			return
		}
		if cmd["type"] != "get_session_stats" {
			return
		}

		// Write the response.
		resp := `{"type":"response","command":"get_session_stats","data":{"tokens":{"input":10,"output":20,"cacheRead":30,"total":60}},"success":true}` + "\n"
		_, _ = stdoutW.Write([]byte(resp))
	}()

	stats, err := connector.SessionStats(context.Background(), session)
	require.NoError(t, err)
	assert.Equal(t, SessionStats{Input: 10, Output: 20, CacheRead: 30, Total: 60}, stats)
}

func TestSessionStats_MissingTokens(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	connector := NewPiConnector(PiConfig{}, nil)
	session := &AgentSession{
		metadata: map[string]interface{}{
			"stdin":  stdinW,
			"stdout": stdoutR,
		},
	}

	go func() {
		defer stdinR.Close()
		defer stdoutW.Close()

		buf := make([]byte, 1024)
		n, _ := stdinR.Read(buf)

		var cmd map[string]interface{}
		_ = json.Unmarshal(buf[:n], &cmd)
		if cmd["type"] != "get_session_stats" {
			return
		}

		resp := `{"type":"response","command":"get_session_stats","data":{},"success":true}` + "\n"
		_, _ = stdoutW.Write([]byte(resp))
	}()

	stats, err := connector.SessionStats(context.Background(), session)
	require.Error(t, err)
	assert.Equal(t, SessionStats{}, stats)
}
