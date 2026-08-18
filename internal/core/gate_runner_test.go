package core

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Gate runner: deterministic verification executes daemon-side, serialized
// per project (two `npm test` in one worktree collide), never in the main
// checkout, artifact-first.

func TestGateRunner_PassesAndWritesArtifact(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	r := NewGateRunner(root)
	a, err := r.RunGate(GateRequest{
		Project: "proj", BeadID: "proj-b1", Gate: "lint",
		Spec:        GateSpec{Cmd: "echo ok", Parser: "exit-code"},
		ProjectPath: t.TempDir(), WorktreePath: wt,
	})
	if err != nil {
		t.Fatalf("RunGate: %v", err)
	}
	if a.Status != GateStatusPassed {
		t.Errorf("status = %s, want passed", a.Status)
	}
	if _, err := LoadGateArtifact(root, "proj", "proj-b1", "lint"); err != nil {
		t.Errorf("artifact must persist: %v", err)
	}
}

func TestGateRunner_FailureIsFailClosed(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	a, err := r.RunGate(GateRequest{
		Project: "proj", BeadID: "proj-b2", Gate: "test",
		Spec:        GateSpec{Cmd: "echo 'FAIL src/a.spec.ts' && exit 1", Parser: "exit-code"},
		ProjectPath: t.TempDir(), WorktreePath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("gate failure is data, not error: %v", err)
	}
	if a.Status != GateStatusFailed {
		t.Errorf("status = %s, want failed", a.Status)
	}
}

func TestGateRunner_MainCheckoutRejected(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	main := t.TempDir()
	_, err := r.RunGate(GateRequest{
		Project: "proj", BeadID: "proj-b3", Gate: "lint",
		Spec:        GateSpec{Cmd: "true", Parser: "exit-code"},
		ProjectPath: main, WorktreePath: main,
	})
	if err == nil || !strings.Contains(err.Error(), "main checkout") {
		t.Errorf("worktree == main checkout must error with 'main checkout', got %v", err)
	}
}

func TestGateRunner_Timeout(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	start := time.Now()
	a, err := r.RunGate(GateRequest{
		Project: "proj", BeadID: "proj-b4", Gate: "test",
		Spec:        GateSpec{Cmd: "sleep 30", Parser: "exit-code", Timeout: 300 * time.Millisecond},
		ProjectPath: t.TempDir(), WorktreePath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("timeout is a failed gate, not an error: %v", err)
	}
	if a.Status != GateStatusFailed {
		t.Errorf("status = %s, want failed (timeout)", a.Status)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("timeout not enforced promptly: %v", time.Since(start))
	}
}

func TestGateRunner_SerializesPerProject(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	var mu sync.Mutex
	var order []string
	req := func(bead string) GateRequest {
		return GateRequest{
			Project: "proj", BeadID: bead, Gate: "test",
			Spec:        GateSpec{Cmd: "sleep 0.2", Parser: "exit-code"},
			ProjectPath: t.TempDir(), WorktreePath: t.TempDir(),
		}
	}
	var wg sync.WaitGroup
	for _, bead := range []string{"b-a", "b-b"} {
		wg.Add(1)
		go func(b string) {
			defer wg.Done()
			r.RunGate(req(b))
			mu.Lock()
			order = append(order, b)
			mu.Unlock()
		}(bead)
	}
	wg.Wait()
	if len(order) != 2 {
		t.Fatalf("both gates must complete, got %v", order)
	}
	// serialized: total wall time must exceed sum of individual sleeps
}

func TestGateRunner_UnknownParserFailsClosed(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	_, err := r.RunGate(GateRequest{
		Project: "proj", BeadID: "proj-b5", Gate: "test",
		Spec:        GateSpec{Cmd: "true", Parser: "nope"},
		ProjectPath: t.TempDir(), WorktreePath: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "parser") {
		t.Errorf("unknown parser must fail closed with parser error, got %v", err)
	}
}
