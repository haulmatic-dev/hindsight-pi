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

	// Cache-aware accumulators. These persist across turn resets (nudges) and
	// carry forward across resumed sessions because resumed sessions emit
	// message.usage events for the accumulated history. get_session_stats is
	// the periodic absolute checkpoint, but per-message usage is the only
	// place the cache breakdown appears.
	freshInput     int
	cacheReadInput int
	cacheCreation  int
	outputTokens   int

	// notifications counts fire-and-forget extension_ui_request methods by name.
	// These are the only trace an extension leaves in the event stream — rtk
	// reports its command rewrites this way — so without tallying them there is
	// no way to tell from a worker log whether the extensions loaded at all.
	notifications map[string]int

	// aborting is set when the worker asks pi to cancel the in-flight agent
	// turn before parking. Events emitted by the abort (agent_end/error with
	// stopReason "aborted") must not be treated as a new turn failure.
	aborting bool

	finishOnce sync.Once
}

func newRPCTurn(autoConfirm bool) *rpcTurn {
	return &rpcTurn{autoConfirm: autoConfirm}
}

// reset re-arms the turn for a nudge: the session stays alive and receives
// another prompt, so end-of-turn state and accumulated text are cleared and
// the next agent_end is evaluated fresh. finishOnce is deliberately NOT
// reset: a turn that was already finished must stay finished.
//
// Token accumulators are intentionally preserved across resets so a resumed
// or nudged session keeps its prior counters instead of under-reporting.
func (t *rpcTurn) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ended = false
	t.failed = false
	t.errMsg = ""
	t.text.Reset()
	t.aborting = false
}

// addUsage folds a per-message usage block into the turn's running counters.
// It is safe to call with a zero usage block.
func (t *rpcTurn) addUsage(u piagents.MessageUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.freshInput += u.FreshInputTokens()
	t.cacheReadInput += u.CacheReadInputTokens
	t.cacheCreation += u.CacheCreationTokens
	t.outputTokens += u.OutputTokens
}

// extractUsage pulls a MessageUsage out of common event shapes. It checks
// event["usage"] first, then event["message"]["usage"], because Pi's RPC
// stream may carry usage either at the top level of a message event or nested
// inside the message object depending on the event type.
func extractUsage(event map[string]any) (piagents.MessageUsage, bool) {
	var raw map[string]any
	if u, ok := event["usage"].(map[string]any); ok {
		raw = u
	} else if msg, ok := event["message"].(map[string]any); ok {
		if u, ok := msg["usage"].(map[string]any); ok {
			raw = u
		}
	}
	if raw == nil {
		return piagents.MessageUsage{}, false
	}
	num := func(k string) int {
		v, ok := raw[k]
		if !ok {
			return 0
		}
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		default:
			return 0
		}
	}
	u := piagents.MessageUsage{
		InputTokens:          num("input_tokens"),
		CacheReadInputTokens: num("cache_read_input_tokens"),
		CacheCreationTokens:  num("cache_creation_input_tokens"),
		OutputTokens:         num("output_tokens"),
	}
	// If the block has no countable fields, treat it as absent.
	if u.InputTokens == 0 && u.CacheReadInputTokens == 0 && u.CacheCreationTokens == 0 && u.OutputTokens == 0 {
		return piagents.MessageUsage{}, false
	}
	return u, true
}

// currentStatsLocked builds a SessionStats snapshot assuming the caller holds t.mu.
func (t *rpcTurn) currentStatsLocked() piagents.SessionStats {
	return piagents.SessionStats{
		Input:          t.freshInput + t.cacheReadInput + t.cacheCreation,
		Output:         t.outputTokens,
		CacheRead:      t.cacheReadInput,
		Total:          t.freshInput + t.cacheReadInput + t.cacheCreation + t.outputTokens,
		FreshInput:     t.freshInput,
		CacheReadInput: t.cacheReadInput,
		CacheCreation:  t.cacheCreation,
	}
}

// currentStats builds a SessionStats snapshot from the accumulated per-message
// counters. It is the checkpoint that feeds the registry heartbeat and the
// execution record.
func (t *rpcTurn) currentStats() piagents.SessionStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.currentStatsLocked()
}

// rpcActivity touches the process-global reporter's rpc-activity signal so
// Charon's zero-token stall classifier can tell an active session (extension
// notifications, turn events) from a hung one — even when no token usage has
// arrived yet.
func (t *rpcTurn) rpcActivity() {
	if activeReporter != nil {
		activeReporter.SetRpcActivity()
	}
}

// forwardUsage accumulates a usage-bearing event into the turn counters and
// immediately forwards the accumulated totals to the process-global reporter
// (FR-003): the rpc scanner must forward parsed usage on every usage-bearing
// event, not only at run exit — otherwise the registry shows tokens_so_far = 0
// for the whole session and Charon's zero-token stall probe flags a working
// worker (PID 2353988 class). Zero-usage blocks are skipped (extractUsage
// treats them as absent), so a session with no real usage never fabricates
// tokens.
func (t *rpcTurn) forwardUsage(event map[string]any) {
	u, ok := extractUsage(event)
	if !ok {
		return
	}
	t.addUsage(u)
	if s := t.currentStats(); s.Total > 0 {
		reportTurnTokens(&s)
	}
}
func (t *rpcTurn) abort(stdin io.Writer) {
	t.mu.Lock()
	t.aborting = true
	t.mu.Unlock()

	if stdin == nil {
		return
	}
	line, err := json.Marshal(map[string]any{"type": "abort"})
	if err != nil {
		log.Printf("[Worker:rpc] abort: encode failed: %v", err)
		return
	}
	if _, err := stdin.Write(append(line, '\n')); err != nil {
		log.Printf("[Worker:rpc] abort: write failed: %v", err)
	} else {
		log.Printf("[Worker:rpc] sent abort for in-flight turn")
	}
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
			t.rpcActivity()
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

	case "turn_end", "message_end":
		// Usage rides turn_end/message_end too — forward it here, not only
		// in the default branch below.
		t.forwardUsage(event)
		msg, _ := event["message"].(map[string]any)
		if msg == nil {
			return false
		}
		// turn_end carries the final assistant message (and, critically, model
		// failures via stopReason/errorMessage). message_end carries completed
		// assistant/user messages. Neither ends the run — agent_end does — but
		// a turn_end with stopReason==error must surface its errorMessage so
		// the park classifier can treat provider-capacity 403s as parkable.
		if eventType == "turn_end" {
			stopReason, _ := msg["stopReason"].(string)
			if stopReason == "error" || stopReason == "aborted" {
				t.mu.Lock()
				t.failed = true
				if em, _ := msg["errorMessage"].(string); em != "" && t.errMsg == "" {
					t.errMsg = em
				} else if t.errMsg == "" {
					t.errMsg = "stopReason: " + stopReason
				}
				t.mu.Unlock()
			}
		}
		if role, _ := msg["role"].(string); role == "assistant" {
			if blocks, ok := msg["content"].([]any); ok {
				t.mu.Lock()
				for _, raw := range blocks {
					block, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					if block["type"] == "text" {
						if s, _ := block["text"].(string); s != "" {
							t.text.WriteString(s)
						}
					}
				}
				t.mu.Unlock()
			}
		}

	case "agent_end":
		t.mu.Lock()
		if t.aborting {
			t.mu.Unlock()
			return false
		}
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
		if t.aborting {
			t.mu.Unlock()
			return false
		}
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
		// Per-message usage is the only place the cache breakdown appears.
		// Accumulate it before falling through to the absolute get_session_stats
		// checkpoint so resumed sessions carry prior counters forward.
		t.forwardUsage(event)
		// Command responses (get_session_stats) carry a data object. Only
		// attempt a stats parse when one is present, so ordinary events cannot
		// be misread as stats.
		if _, hasData := event["data"]; hasData {
			if s, err := piagents.ParseSessionStats([]byte(line)); err == nil && s.Total > 0 {
				// Prefer the accumulated breakdown for fresh/cache columns, but
				// trust the provider's absolute total if it differs.
				cur := t.currentStatsLocked()
				s.FreshInput = cur.FreshInput
				s.CacheReadInput = cur.CacheReadInput
				s.CacheCreation = cur.CacheCreation
				if s.Output == 0 {
					s.Output = cur.Output
				}
				t.stats = &s
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

// abandon marks the turn failed with an external reason and closes stdin so
// pi exits. Used when the shepherd orders abandon-with-summary while the
// worker is parked (PRD Part 3): the reason must not match any park marker,
// so the failure path classifies and propagates instead of re-parking.
func (t *rpcTurn) abandon(reason string, stdin io.Closer) {
	t.mu.Lock()
	t.ended = true
	t.failed = true
	t.errMsg = reason
	t.mu.Unlock()
	if stdin != nil {
		if err := stdin.Close(); err != nil {
			log.Printf("[Worker:rpc] abandon: stdin close failed: %v", err)
		}
	}
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
	if t.stats == nil {
		cur := t.currentStatsLocked()
		return &cur
	}
	// get_session_stats gave us an absolute checkpoint, but its breakdown may
	// be empty; overlay the accumulated breakdown and output.
	s := *t.stats
	cur := t.currentStatsLocked()
	if s.FreshInput == 0 {
		s.FreshInput = cur.FreshInput
	}
	if s.CacheReadInput == 0 {
		s.CacheReadInput = cur.CacheReadInput
	}
	if s.CacheCreation == 0 {
		s.CacheCreation = cur.CacheCreation
	}
	if s.Output == 0 {
		s.Output = cur.Output
	}
	// Reconcile totals: trust the provider absolute, but if it is missing use
	// the accumulated sum so budget decisions still see real consumption.
	if s.Total == 0 {
		s.Total = cur.Total
	}
	return &s
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
