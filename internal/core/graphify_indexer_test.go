package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGraphify writes a shell script that stands in for the graphify binary and
// records every invocation, so these tests assert on the real argv without
// needing graphify installed.
func fakeGraphify(t *testing.T, body string) (binPath, logPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "graphify")
	logPath = filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + logPath + "\n" + body
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binPath, logPath
}

func waitForFile(t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

// TestGraphifyIndexer_RunsUpdateNotExtract pins the command choice, which is the
// whole cost argument: `update` is deterministic tree-sitter work with no model
// call, while `extract` performs semantic extraction through an LLM backend.
// Refreshing a graph must never spend tokens.
func TestGraphifyIndexer_RunsUpdateNotExtract(t *testing.T) {
	bin, calls := fakeGraphify(t, "exit 0\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin})
	if !idx.Refresh(project) {
		t.Fatal("Refresh should have started a build")
	}

	got := waitForFile(t, calls, 3*time.Second)
	if got == "" {
		t.Fatal("graphify was never invoked")
	}
	if want := "update " + project; got != want+"\n" {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// TestGraphifyIndexer_SkipClusteringFlag pins the flag name. --no-viz looks
// plausible and is NOT accepted by `update` (it belongs to `cluster-only`);
// passing it fails every build with "unknown update option". Found by running the
// real binary, which is why this asserts the exact argv.
func TestGraphifyIndexer_SkipClusteringFlag(t *testing.T) {
	bin, calls := fakeGraphify(t, "exit 0\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin, SkipClustering: true})
	idx.Refresh(project)

	got := waitForFile(t, calls, 3*time.Second)
	if want := "update " + project + " --no-cluster\n"; got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// TestGraphifyIndexer_HonoursRefreshInterval: graphs are refreshed per project on
// an interval. Without this a busy run would rebuild continuously.
func TestGraphifyIndexer_HonoursRefreshInterval(t *testing.T) {
	bin, _ := fakeGraphify(t, "exit 0\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{
		Enabled: true, Binary: bin, RefreshInterval: time.Hour,
	})
	if !idx.Refresh(project) {
		t.Fatal("first Refresh should start a build")
	}
	if idx.Refresh(project) {
		t.Error("second Refresh inside the interval must be skipped")
	}
	if _, ok := idx.LastBuilt(project); !ok {
		t.Error("LastBuilt should record the build")
	}
}

// TestGraphifyIndexer_ZeroIntervalAlwaysAllows: interval 0 means "no periodic
// refresh policy", so each explicit call is honoured once the previous finishes.
func TestGraphifyIndexer_ZeroIntervalAlwaysAllows(t *testing.T) {
	bin, calls := fakeGraphify(t, "exit 0\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin})
	idx.Refresh(project)
	waitForFile(t, calls, 3*time.Second)
	// Wait for the in-flight flag to clear before asking again.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if idx.Refresh(project) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("a later Refresh with no interval should be allowed once the build finished")
}

// TestGraphifyIndexer_FailingBuildIsSwallowed is the fail-open contract: a broken
// graph must never affect scheduling. The worst case is that the skill has
// nothing to query and the agent reads files instead.
func TestGraphifyIndexer_FailingBuildIsSwallowed(t *testing.T) {
	bin, calls := fakeGraphify(t, "echo boom >&2\nexit 3\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin})
	if !idx.Refresh(project) {
		t.Fatal("Refresh should start even though the build will fail")
	}
	if got := waitForFile(t, calls, 3*time.Second); got == "" {
		t.Fatal("graphify was never invoked")
	}
	// No panic, no blocking: reaching here is the assertion.
}

// TestGraphifyIndexer_TimeoutKillsRunawayBuild guards against the upstream
// O(V x E) pathology (graphify issue #341: 114 minutes on a 50k-file monorepo,
// never completing). A misbehaving build must be killed, not wedge the daemon.
func TestGraphifyIndexer_TimeoutKillsRunawayBuild(t *testing.T) {
	// The fake spawns sleep as a BACKGROUND child and waits, so killing the
	// shell leaves an orphan holding the output pipe — the exact shape CI caught
	// on Linux (a trailing command gets exec'd by some shells, which masked the
	// orphan case on macOS). WaitDelay is what unblocks CombinedOutput.
	bin, _ := fakeGraphify(t, "sleep 30 &\nwait\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{
		Enabled: true, Binary: bin, Timeout: 200 * time.Millisecond,
	})

	// build is called directly so the timeout is asserted deterministically
	// rather than through the background goroutine.
	start := time.Now()
	err := idx.build(project)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a build exceeding the timeout must return an error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("build took %s — the timeout did not kill it", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want a timeout error naming the limit", err)
	}
}

// TestGraphifyIndexer_RefreshNeverBlocks: refresh happens in the background, so a
// slow build must not stall the caller — the scheduler tick calls this.
func TestGraphifyIndexer_RefreshNeverBlocks(t *testing.T) {
	bin, _ := fakeGraphify(t, "sleep 10\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{
		Enabled: true, Binary: bin, Timeout: 2 * time.Second,
	})
	start := time.Now()
	idx.Refresh(project)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Refresh blocked for %s — it must return immediately", elapsed)
	}
}

func TestGraphifyIndexer_MissingBinaryIsNoOp(t *testing.T) {
	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: "graphify-does-not-exist-xyz"})
	if idx.Available() {
		t.Fatal("Available must be false for a missing binary")
	}
	if idx.Refresh(t.TempDir()) {
		t.Error("Refresh must be a no-op when the binary is missing")
	}
}

func TestGraphifyIndexer_NilAndEmptyInputs(t *testing.T) {
	var nilIdx *GraphifyIndexer
	if nilIdx.Available() || nilIdx.Refresh("/tmp") {
		t.Error("a nil indexer must be inert, not panic")
	}
	if _, ok := nilIdx.LastBuilt("/tmp"); ok {
		t.Error("nil indexer must report no builds")
	}

	bin, _ := fakeGraphify(t, "exit 0\n")
	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin})
	if idx.Refresh("   ") {
		t.Error("an empty project path must be rejected")
	}
}

// TestNewGraphifyIndexer_DefaultsForUnsetFields: a half-filled config block must
// not produce a zero timeout, which would cancel every build immediately.
func TestNewGraphifyIndexer_DefaultsForUnsetFields(t *testing.T) {
	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true})
	if idx.binary != "graphify" {
		t.Errorf("binary = %q, want \"graphify\"", idx.binary)
	}
	if idx.timeout <= 0 {
		t.Errorf("timeout = %s, want a positive default", idx.timeout)
	}
	if cap(idx.sem) < 1 {
		t.Errorf("concurrency cap = %d, want at least 1", cap(idx.sem))
	}
}

// TestGraphifyIndexer_GitignoresOutput pins the containment rule. graphify writes
// graphify-out/ inside the repo (~26 MB here) and Hydra commits + pushes worker
// changes, so without this the graph gets committed into whatever repo it
// indexed — including user projects, which do not carry Hydra's .gitignore.
func TestGraphifyIndexer_GitignoresOutput(t *testing.T) {
	bin, calls := fakeGraphify(t, "exit 0\n")
	project := t.TempDir()

	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin})
	if err := idx.build(project); err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := waitForFile(t, calls, 3*time.Second); got == "" {
		t.Fatal("graphify was never invoked")
	}

	data, err := os.ReadFile(filepath.Join(project, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore should have been created: %v", err)
	}
	if !strings.Contains(string(data), "graphify-out/") {
		t.Errorf(".gitignore = %q, want it to ignore graphify-out/", string(data))
	}
}

// TestGraphifyIndexer_GitignoreIsIdempotent: repeated refreshes must not append
// the same rule over and over.
func TestGraphifyIndexer_GitignoreIsIdempotent(t *testing.T) {
	bin, _ := fakeGraphify(t, "exit 0\n")
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewGraphifyIndexer(GraphifyConfig{Enabled: true, Binary: bin})
	for i := 0; i < 3; i++ {
		if err := idx.build(project); err != nil {
			t.Fatalf("build %d: %v", i, err)
		}
	}

	data, _ := os.ReadFile(filepath.Join(project, ".gitignore"))
	if n := strings.Count(string(data), "graphify-out/"); n != 1 {
		t.Errorf("graphify-out/ appears %d times, want exactly 1", n)
	}
	if !strings.Contains(string(data), "node_modules/") {
		t.Error("existing .gitignore content must be preserved")
	}
}
