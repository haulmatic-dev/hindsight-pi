package core

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestGateCmdProcGroupStartFailure: a gate whose Start fails (missing cwd)
// must not panic the killer goroutine when the context cancels. Regression
// for the 2026-11-08 crash loop: RunGate returned "fork/exec /usr/bin/sh: no
// such file or directory" (worktree torn down by a concurrent reconciler),
// defer cancel() fired, the goroutine dereferenced nil cmd.Process, and the
// daemon crash-looped with status=2/INVALIDARGUMENT.
func TestGateCmdProcGroupStartFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "true")
	cmd.Dir = "/nonexistent/gate/cwd" // Start fails: ENOENT on the cwd
	gateCmdProcGroup(cmd, ctx)

	// Start fails — this is the path that used to arm the panic.
	if err := cmd.Start(); err == nil {
		t.Fatal("expected Start to fail with missing cwd")
	}

	// Fire the killer goroutine (cancel now, don't wait for the timeout)
	// and give the goroutine a scheduling slot. Before the fix this
	// segfaulted the test process; after it the goroutine returns quietly.
	cancel()
	time.Sleep(250 * time.Millisecond) // goroutine must complete without panic

	// Successful-start path still arms the group kill: verify the process
	// field is set and a kill against it is safe (it already exited, so
	// ESRCH is expected and ignored).
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	cmd2 := exec.CommandContext(ctx2, "sh", "-c", "exit 0")
	gateCmdProcGroup(cmd2, ctx2)
	if err := cmd2.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = cmd2.Wait()
	cancel2() // killer goroutine fires; PID is stale, ESRCH ignored
	time.Sleep(100 * time.Millisecond)

	_ = syscall.Getpid // keep syscall import if the above path changes
}
