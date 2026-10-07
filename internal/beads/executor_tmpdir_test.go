package beads

import (
	"os"
	"strings"
	"testing"
)

// TestDefaultExecutorWithDirIsolatesTMPDIR guards the 2026-10-07 fix: br's
// fsqlite WAL layer stages files under fixed names in $TMPDIR, so concurrent
// br processes against different stores collide (cross-store WAL phantom
// writes, SYNC_CONFLICT lockouts). Each invocation must get a private TMPDIR.
func TestDefaultExecutorWithDirIsolatesTMPDIR(t *testing.T) {
	exec := &defaultExecutorWithDir{Dir: t.TempDir()}

	// A second invocation running concurrently must observe a DIFFERENT
	// TMPDIR than the first (the regression was a shared /tmp).
	first := make(chan string, 1)
	second := make(chan string, 1)
	run := func(out chan<- string) {
		b, err := exec.Execute("sh", "-c", "echo -n $TMPDIR")
		if err != nil {
			t.Errorf("Execute failed: %v", err)
		}
		out <- string(b)
	}
	go run(first)
	go run(second)
	d1, d2 := <-first, <-second

	if d1 == "" || d2 == "" {
		t.Fatalf("TMPDIR not set in child (got %q, %q)", d1, d2)
	}
	if d1 == d2 {
		t.Fatalf("concurrent invocations share TMPDIR %q — isolation broken", d1)
	}
	if !strings.Contains(d1, "hydra-br-") || !strings.Contains(d2, "hydra-br-") {
		t.Fatalf("TMPDIR not under hydra-br- prefix: %q, %q", d1, d2)
	}
	if _, err := os.Stat(d1); !os.IsNotExist(err) {
		t.Fatalf("TMPDIR %q not cleaned up after invocation", d1)
	}
}
