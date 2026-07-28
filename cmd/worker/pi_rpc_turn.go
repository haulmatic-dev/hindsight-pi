package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	piagents "github.com/haulmatic-dev/hydra/internal/agents"
)

// rpcStatsGrace is how long to keep reading after asking for token stats before
// terminating pi. Short: the answer is a single line pi already has in memory,
// and every millisecond here is added to every bead's wall-clock.
const rpcStatsGrace = 1500 * time.Millisecond

// rpcTurn tracks the protocol state of one `pi --mode rpc` turn.
//
// Text mode and rpc mode end a turn in fundamentally different ways, and that
// difference is the whole reason this type exists. In text mode the worker
// closes stdin, pi processes the prompt and exits, and the exit code is the
// verdict. In rpc mode pi does NOT exit after finishing: it stays alive waiting
// for the next prompt, so
//
//  1. the verdict has to be read out of the event stream, and
//  2. the process has to be terminated deliberately.
//
// Both were verified in tools/pi-rpc, which ends every session with
// close-stdin + Kill for precisely this reason. A worker that just waited on
// cmd.Wait() would sit idle until the 10m activity timeout fired on every
// single bead.
type rpcTurn struct {
	mu          sync.Mutex
	autoConfirm bool

	ended      bool
	failed     bool
	errMsg     string
	text       strings.Builder
	uiAnswered int
	stats      *piagents.SessionStats

	// notifications counts fire-and-forget extension_ui_request methods by name.
	// These are the only trace an extension leaves in the event stream — rtk
	// reports its command rewrites this way — so without tallying them there is
	// no way to tell from a worker log whether the extensions loaded at all.
	notifications map[string]int

	finishOnce sync.Once
}

func newRPCTurn(autoConfirm bool) *rpcTurn {
	return &rpcTurn{autoConfirm: autoConfirm}
}

// rpcPromptLine builds the JSONL command that starts a turn.
func rpcPromptLine(prompt string) ([]byte, error) {
	return json.Marshal(map[string]any{"type": "prompt", "message": prompt})
}

// handleLine consumes one line of pi's stdout and reports whether the turn is
// over. stdin is used to answer blocking UI asks and may be nil in tests.
func (t *rpcTurn) handleLine(line string, stdin io.Writer) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		// Not JSON. pi still prints the occasional plain line; keep it as
		// output rather than discarding it.
		t.mu.Lock()
		t.text.WriteString(line)
		t.text.WriteString("\n")
		t.mu.Unlock()
		return false
	}

	eventType, _ := event["type"].(string)

	switch eventType {
	case "extension_ui_request":
		method, _ := event["method"].(string)
		id, _ := event["id"].(string)
		if !piagents.UIRequestNeedsReply(method) {
			// Notifications (notify, setStatus, setWidget, …) must never be
			// answered; replying to one desynchronises the stream. Count them so
			// extension activity is at least observable.
			t.mu.Lock()
			if t.notifications == nil {
				t.notifications = make(map[string]int)
			}
			t.notifications[method]++
			t.mu.Unlock()
			// Log the message body for `notify`, which is how extensions report
			// what they actually did — rtk announces each rewrite this way
			// ("RTK rewrite: git status --short -> rtk git status --short").
			// Without this the log proves an extension spoke, but not what it did,
			// which is the difference between "loaded" and "earning its tokens".
			if method == "notify" {
				if msg, _ := event["message"].(string); msg != "" {
					log.Printf("[Worker:rpc] extension notice: %s", msg)
				}
			}
			return false
		}
		reply, ok := piagents.UIResponseFor(id, method, t.autoConfirm)
		if !ok || stdin == nil {
			return false
		}
		if _, err := fmt.Fprintf(stdin, "%s\n", reply); err != nil {
			log.Printf("[Worker:rpc] failed to answer blocking UI ask %q: %v", method, err)
			return false
		}
		t.mu.Lock()
		t.uiAnswered++
		t.mu.Unlock()
		log.Printf("[Worker:rpc] answered blocking UI ask %q (autoConfirm=%v)", method, t.autoConfirm)

	case "message_update":
		if ame, ok := event["assistantMessageEvent"].(map[string]any); ok {
			if kind, _ := ame["type"].(string); kind == "text_delta" {
				if delta, ok := ame["delta"].(string); ok {
					t.mu.Lock()
					t.text.WriteString(delta)
					t.mu.Unlock()
				}
			}
		}

	case "agent_end":
		t.mu.Lock()
		t.ended = true
		// A turn can reach agent_end and still have failed: pi reports model
		// failures INSIDE the message (stopReason/errorMessage), not in the
		// envelope. tools/pi-rpc reported an expired API key as a passing turn
		// until this was handled — an envelope-only reader sees success.
		if msgs, ok := event["messages"].([]any); ok {
			for _, m := range msgs {
				msg, ok := m.(map[string]any)
				if !ok {
					continue
				}
				stopReason, _ := msg["stopReason"].(string)
				if stopReason != "error" && stopReason != "aborted" {
					continue
				}
				t.failed = true
				if em, _ := msg["errorMessage"].(string); em != "" {
					t.errMsg = em
				} else if t.errMsg == "" {
					t.errMsg = "stopReason: " + stopReason
				}
			}
		}
		t.mu.Unlock()
		return true

	case "error":
		t.mu.Lock()
		t.ended = true
		t.failed = true
		if e, ok := event["error"].(map[string]any); ok {
			if em, _ := e["errorMessage"].(string); em != "" {
				t.errMsg = em
			}
		}
		if t.errMsg == "" {
			t.errMsg = "pi reported an error event"
		}
		t.mu.Unlock()
		return true

	default:
		// Command responses (get_session_stats) carry a data object. Only
		// attempt a stats parse when one is present, so ordinary events cannot
		// be misread as stats.
		if _, hasData := event["data"]; hasData {
			if s, err := piagents.ParseSessionStats([]byte(line)); err == nil && s.Total > 0 {
				t.mu.Lock()
				t.stats = &s
				t.mu.Unlock()
			}
		}
	}

	return false
}

// finish closes out a completed turn: ask for token counts, then close stdin and
// terminate pi's process group. Idempotent — an `error` event arriving after
// agent_end must not kill twice.
//
// The stats request and the teardown are deliberately split across a goroutine:
// sleeping inline would block the caller (the stdout scanner), so the very
// response being waited for could never be read.
func (t *rpcTurn) finish(pid int, stdin io.WriteCloser) {
	t.finishOnce.Do(func() {
		if stdin != nil {
			if req, err := json.Marshal(map[string]any{"type": "get_session_stats"}); err == nil {
				if _, err := fmt.Fprintf(stdin, "%s\n", req); err != nil {
					log.Printf("[Worker:rpc] could not request session stats: %v", err)
				}
			}
		}
		go func() {
			time.Sleep(rpcStatsGrace)
			if stdin != nil {
				_ = stdin.Close()
			}
			if pid > 0 {
				// pi will not exit on its own after agent_end.
				_ = syscall.Kill(-pid, syscall.SIGTERM)
				time.Sleep(500 * time.Millisecond)
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	})
}

func (t *rpcTurn) Ended() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ended
}

func (t *rpcTurn) Failed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failed
}

func (t *rpcTurn) ErrMsg() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.errMsg
}

func (t *rpcTurn) Text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.text.String()
}

func (t *rpcTurn) UIAnswered() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.uiAnswered
}

func (t *rpcTurn) Stats() *piagents.SessionStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stats
}

// Notifications returns a stable "method=count" summary of the fire-and-forget
// UI notifications seen this turn, or "" when none arrived. An empty result is
// itself a finding: it means no extension announced anything.
func (t *rpcTurn) Notifications() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.notifications) == 0 {
		return ""
	}
	methods := make([]string, 0, len(t.notifications))
	for m := range t.notifications {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	parts := make([]string, 0, len(methods))
	for _, m := range methods {
		parts = append(parts, fmt.Sprintf("%s=%d", m, t.notifications[m]))
	}
	return strings.Join(parts, " ")
}
