// pi-rpc — a standalone client for `pi --mode rpc`, deliberately isolated from
// hydra.
//
// Why this exists: hydra drives pi in piped text mode, where pi's extensions
// (rtk output compaction, context-mode, caveman, hindsight) never initialise —
// verified across three controlled runs. In rpc mode they do. rpc also exposes
// the session-control vocabulary hydra lacks today: get_state, abort,
// follow_up, cycle_model, cycle_thinking_level, compact.
//
// This tool proves the protocol end to end without touching the daemon, so a
// broken experiment can never stall a live run. Bringing the capability into
// hydra is a separate decision.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxLine = 8 * 1024 * 1024 // event payloads (tool output, messages) get large

type client struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	events        chan Event
	stderr        *strings.Builder
	mu            sync.Mutex
	trace         bool
	autoOK        bool
	counts        map[string]int
	uiAsks        []string
	lastErr       string
	modelErr      string
	assistantText string
	toolUses      int
	dump          io.Writer // when set, every raw protocol line is written here
}

func (c *client) send(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.trace {
		fmt.Fprintf(os.Stderr, "→ %s\n", payload)
	}
	_, err := fmt.Fprintf(c.stdin, "%s\n", payload)
	return err
}

// start spawns pi in rpc mode and begins streaming events.
func start(ctx context.Context, provider, model, cwd string, offline, trace, autoOK bool, dump io.Writer) (*client, error) {
	args := []string{"--mode", "rpc", "--no-session"}
	if offline {
		args = append(args, "--offline")
	}
	if provider != "" {
		args = append(args, "--provider", provider)
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.CommandContext(ctx, "pi", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	var errBuf strings.Builder
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start pi: %w", err)
	}

	c := &client{
		cmd: cmd, stdin: stdin, stderr: &errBuf,
		events: make(chan Event, 256),
		trace:  trace, autoOK: autoOK,
		counts: map[string]int{},
		dump:   dump,
	}

	go func() { _, _ = io.Copy(&errBuf, stderrPipe) }()
	go func() {
		defer close(c.events)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), maxLine)
		for sc.Scan() {
			line := sc.Bytes()
			if len(strings.TrimSpace(string(line))) == 0 {
				continue
			}
			if c.dump != nil {
				fmt.Fprintf(c.dump, "%s\n", line)
			}
			ev, err := UnmarshalEvent(line)
			if err != nil {
				if trace {
					fmt.Fprintf(os.Stderr, "← (unparsed) %s\n", line)
				}
				continue
			}
			if trace {
				fmt.Fprintf(os.Stderr, "← %s\n", summarize(ev, line))
			}
			c.events <- ev
		}
	}()
	return c, nil
}

func summarize(ev Event, line []byte) string {
	s := ev.Type
	if ev.Method != "" {
		s += "/" + ev.Method
	}
	if ev.Command != "" {
		s += " cmd=" + ev.Command
	}
	if ev.Error != "" {
		s += " error=" + ev.Error
	}
	if len(line) < 200 {
		s += "  " + string(line)
	}
	return s
}

// runTurn pumps events until the agent turn ends, answering blocking UI asks.
func (c *client) runTurn(timeout time.Duration) (terminated bool, err error) {
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return false, fmt.Errorf("pi exited before agent_end (stderr: %s)", tail(c.stderr.String(), 400))
			}
			c.counts[ev.Type]++
			if ev.Type == "extension_ui_request" {
				if UIRequestNeedsReply(ev.Method) {
					c.uiAsks = append(c.uiAsks, ev.Method)
					reply, rErr := UIReply(ev.ID, ev.Method, c.autoOK)
					if rErr == nil {
						_ = c.send(reply)
					}
					fmt.Fprintf(os.Stderr, "[pi-rpc] answered blocking UI ask %q (autoConfirm=%v) — a headless session should not be asking\n", ev.Method, c.autoOK)
				}
				continue
			}
			if ev.Error != "" {
				c.lastErr = ev.Error
			}
			// Model-level failures hide inside the message, not the envelope.
			if me := ev.MessageError(); me != "" {
				c.modelErr = me
			}
			if txt, tools := ev.AssistantText(); ev.Type == "message_end" {
				if txt != "" {
					c.assistantText = txt
				}
				c.toolUses += tools
			}
			if ev.IsTerminal() {
				return true, nil
			}
		case <-deadline:
			return false, fmt.Errorf("timeout after %s waiting for agent_end", timeout)
		}
	}
}

// query sends a command and returns the first response event for it.
func (c *client) query(cmd string, timeout time.Duration) (Event, error) {
	payload, err := SimpleCommand(cmd)
	if err != nil {
		return Event{}, err
	}
	if err := c.send(payload); err != nil {
		return Event{}, err
	}
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return Event{}, fmt.Errorf("pi exited while awaiting %s", cmd)
			}
			c.counts[ev.Type]++
			if ev.Type == "response" && (ev.Command == cmd || ev.Command == "") {
				return ev, nil
			}
			if ev.Type == "extension_ui_request" && UIRequestNeedsReply(ev.Method) {
				if reply, rErr := UIReply(ev.ID, ev.Method, c.autoOK); rErr == nil {
					_ = c.send(reply)
				}
			}
		case <-deadline:
			return Event{}, fmt.Errorf("timeout awaiting response to %s", cmd)
		}
	}
}

func (c *client) close() {
	_ = c.stdin.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func (c *client) printSummary(d time.Duration) {
	fmt.Printf("\n=== event summary (%.1fs) ===\n", d.Seconds())
	keys := make([]string, 0, len(c.counts))
	for k := range c.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-28s %d\n", k, c.counts[k])
	}
	if len(c.uiAsks) > 0 {
		fmt.Printf("  blocking UI asks: %v\n", c.uiAsks)
	}
	fmt.Printf("  tool_use blocks:            %d\n", c.toolUses)
	if c.assistantText != "" {
		fmt.Printf("  assistant: %s\n", tail(c.assistantText, 300))
	}
	if c.lastErr != "" {
		fmt.Printf("  last error: %s\n", c.lastErr)
	}
	if c.modelErr != "" {
		fmt.Printf("  MODEL ERROR: %s\n", tail(c.modelErr, 300))
	}
	if s := tail(c.stderr.String(), 300); s != "" {
		fmt.Printf("  stderr tail: %s\n", s)
	}
}

func main() {
	var (
		prompt    = flag.String("prompt", "", "prompt to send (required for probe)")
		provider  = flag.String("provider", "kimi-coding", "pi provider")
		model     = flag.String("model", "kimi-for-coding", "pi model")
		cwd       = flag.String("cwd", "", "working directory for the pi process")
		offline   = flag.Bool("offline", true, "pass --offline (skip pi's startup network fetch)")
		trace     = flag.Bool("trace", false, "print every protocol message to stderr")
		autoOK    = flag.Bool("auto-confirm", false, "answer confirm asks with true instead of false")
		probeCmds = flag.String("probe-commands", "", "comma-separated commands to issue after the turn (e.g. get_state,get_session_stats)")
		timeout   = flag.Duration("timeout", 3*time.Minute, "per-turn timeout")
		dumpPath  = flag.String("dump", "", "write every raw protocol line to this file (full JSONL, untruncated)")
	)
	flag.Parse()

	if *prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: pi-rpc --prompt \"...\" [--trace] [--probe-commands get_state,...]")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout+time.Minute)
	defer cancel()

	started := time.Now()
	var dumpW io.Writer
	if *dumpPath != "" {
		f, dErr := os.Create(*dumpPath)
		if dErr != nil {
			fmt.Fprintf(os.Stderr, "pi-rpc: cannot open dump file: %v\n", dErr)
			os.Exit(2)
		}
		defer f.Close()
		dumpW = f
	}

	c, err := start(ctx, *provider, *model, *cwd, *offline, *trace, *autoOK, dumpW)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pi-rpc: %v\n", err)
		os.Exit(1)
	}
	defer c.close()

	payload, err := PromptCommand(*prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pi-rpc: encode prompt: %v\n", err)
		os.Exit(1)
	}
	if err := c.send(payload); err != nil {
		fmt.Fprintf(os.Stderr, "pi-rpc: send prompt: %v\n", err)
		os.Exit(1)
	}

	done, turnErr := c.runTurn(*timeout)

	// Session-control probes: the capability hydra lacks in text mode.
	if done && *probeCmds != "" {
		for _, cmd := range strings.Split(*probeCmds, ",") {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				continue
			}
			ev, qErr := c.query(cmd, 30*time.Second)
			if qErr != nil {
				fmt.Printf("probe %-24s ERROR %v\n", cmd, qErr)
				continue
			}
			raw, _ := json.Marshal(ev.Raw)
			fmt.Printf("probe %-24s %s\n", cmd, tail(string(raw), 220))
		}
	}

	c.printSummary(time.Since(started))

	if turnErr != nil {
		fmt.Fprintf(os.Stderr, "\npi-rpc: turn did not complete: %v\n", turnErr)
		os.Exit(1)
	}
	// A turn that reached agent_end but carried a model-level error is a
	// FAILURE, not a success — reporting otherwise is how an expired API key
	// masqueraded as a passing run.
	if c.modelErr != "" {
		fmt.Fprintf(os.Stderr, "\npi-rpc: turn reached agent_end but the model call FAILED: %s\n", tail(c.modelErr, 400))
		os.Exit(1)
	}
	fmt.Println("\npi-rpc: turn completed (agent_end)")
}
