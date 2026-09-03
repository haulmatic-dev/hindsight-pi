package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// trackedWC is an io.WriteCloser that records closure.
type trackedWC struct {
	safeBuffer
	mu     sync.Mutex
	closed bool
}

func (w *trackedWC) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *trackedWC) Closed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// reporterWithCommands installs a global reporter carrying the given queued
// commands and returns a cleanup func.
func reporterWithCommands(cmds ...registry.Command) func() {
	prev := activeReporter
	activeReporter = &stateReporter{commands: cmds}
	return func() { activeReporter = prev }
}

func wakeAndWait() {
	// parkSession wakes in a goroutine; wait well past the 20ms park.
	time.Sleep(200 * time.Millisecond)
}

func TestParkWake_SteerCommandReshapesReprompt(t *testing.T) {
	cleanup := reporterWithCommands(registry.Command{Kind: registry.CmdSteer, Payload: "focus on the failing tests"})
	defer cleanup()

	ps := newParkState(0)
	stdin := &trackedWC{}
	turn := newRPCTurn(false)

	parkSession(ps, 20*time.Millisecond, "provider-capacity", stdin, turn, "CONTINUE")
	wakeAndWait()

	out := stdin.String()
	if !strings.Contains(out, "focus on the failing tests") {
		t.Errorf("wake prompt must carry the steer payload, got: %q", out)
	}
	if !strings.Contains(out, "CONTINUE") {
		t.Errorf("wake prompt must still resume the task, got: %q", out)
	}
	if turn.Failed() {
		t.Errorf("steer must not fail the turn")
	}
}

func TestParkWake_ContinueNoteAppended(t *testing.T) {
	cleanup := reporterWithCommands(registry.Command{Kind: registry.CmdContinue, Payload: "provider has capacity again"})
	defer cleanup()

	ps := newParkState(0)
	stdin := &trackedWC{}
	turn := newRPCTurn(false)

	parkSession(ps, 20*time.Millisecond, "provider-capacity", stdin, turn, "CONTINUE")
	wakeAndWait()

	if !strings.Contains(stdin.String(), "provider has capacity again") {
		t.Errorf("continue payload should ride the wake prompt, got: %q", stdin.String())
	}
}

func TestParkWake_AbandonClosesTurn(t *testing.T) {
	cleanup := reporterWithCommands(registry.Command{Kind: registry.CmdAbandonWithSummary, Payload: "quota exhausted, no progress possible"})
	defer cleanup()

	ps := newParkState(0)
	stdin := &trackedWC{}
	turn := newRPCTurn(false)

	parkSession(ps, 20*time.Millisecond, "credential", stdin, turn, "CONTINUE")
	wakeAndWait()

	if stdin.Len() != 0 {
		t.Errorf("abandon must not reprompt pi, wrote: %q", stdin.String())
	}
	if !stdin.Closed() {
		t.Errorf("abandon must close stdin so pi exits")
	}
	if !turn.Failed() || !strings.Contains(turn.ErrMsg(), "abandoned by shepherd") {
		t.Errorf("turn must be failed with abandon reason, failed=%v err=%q", turn.Failed(), turn.ErrMsg())
	}
}

func TestParkWake_NoCommandsKeepsReprompt(t *testing.T) {
	cleanup := reporterWithCommands()
	defer cleanup()

	ps := newParkState(0)
	stdin := &trackedWC{}
	turn := newRPCTurn(false)

	parkSession(ps, 20*time.Millisecond, "provider-capacity", stdin, turn, "CONTINUE")
	wakeAndWait()

	out := stdin.String()
	if !strings.Contains(out, "CONTINUE") || strings.Contains(out, "Shepherd") {
		t.Errorf("clean wake must be the plain reprompt, got: %q", out)
	}
}
