package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
)

// fakeCloseExecutor records br invocations so tests can verify whether a
// close was attempted without running the real br binary.
type fakeCloseExecutor struct {
	calls [][]string
}

func (f *fakeCloseExecutor) Execute(name string, arg ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, arg...))
	return []byte("{}"), nil
}

func (f *fakeCloseExecutor) ExecuteInDir(dir string, name string, arg ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{dir, name}, arg...))
	if name == "br" && len(arg) > 0 && arg[0] == "show" {
		return []byte(`[{"id":"hydra-test-123","status":"closed","title":"t","priority":0,"issue_type":"task","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`), nil
	}
	return []byte("{}"), nil
}

func (f *fakeCloseExecutor) closeCalls() [][]string {
	var out [][]string
	for _, c := range f.calls {
		// calls are recorded as [dir, br, subcommand, ...]
		if len(c) >= 3 && c[1] == "br" && c[2] == "close" {
			out = append(out, c)
		}
	}
	return out
}

func TestMarkBeadFailed_SkipsWhenNoWorkspace(t *testing.T) {
	tmp := t.TempDir()
	wt, err := NewWorkerTracker(tmp)
	if err != nil {
		t.Fatal(err)
	}

	// Track a worker whose project directory has no .beads workspace.
	nonWorkspace := filepath.Join(tmp, "nowhere")
	if err := os.MkdirAll(nonWorkspace, 0755); err != nil {
		t.Fatal(err)
	}
	if err := wt.TrackWorker(123, "hydra-test-123", nonWorkspace); err != nil {
		t.Fatal(err)
	}

	fake := &fakeCloseExecutor{}
	client := beads.NewClientWithExecutor(fake)
	charon := NewCharon(client, wt, 5*time.Minute)
	charon.markBeadFailed("hydra-test-123", "timeout")

	if got := len(fake.closeCalls()); got != 0 {
		t.Errorf("expected 0 close calls for non-workspace, got %d", got)
	}
}

func TestMarkBeadFailed_ClosesWhenWorkspace(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}

	wt, err := NewWorkerTracker(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.TrackWorker(123, "hydra-test-123", tmp); err != nil {
		t.Fatal(err)
	}

	fake := &fakeCloseExecutor{}
	client := beads.NewClientWithExecutor(fake)
	charon := NewCharon(client, wt, 5*time.Minute)
	charon.markBeadFailed("hydra-test-123", "timeout")

	if got := len(fake.closeCalls()); got != 1 {
		t.Errorf("expected 1 close call for workspace, got %d", got)
	}
}
