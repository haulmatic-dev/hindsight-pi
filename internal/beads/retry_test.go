package beads

import (
	"errors"
	"testing"
)

// flakyExecutor fails a set number of times, then succeeds.
type flakyExecutor struct {
	failsLeft int
	failErr   error
	okOutput  []byte
	calls     int
}

func (f *flakyExecutor) Execute(name string, arg ...string) ([]byte, error) {
	f.calls++
	if f.failsLeft > 0 {
		f.failsLeft--
		return nil, f.failErr
	}
	return f.okOutput, nil
}

func (f *flakyExecutor) ExecuteInDir(dir, name string, arg ...string) ([]byte, error) {
	return f.Execute(name, arg...)
}

func TestExecInDir_RetriesTransientExit8(t *testing.T) {
	f := &flakyExecutor{failsLeft: 2, failErr: errors.New("br ready failed: exit status 8"), okOutput: []byte("[]")}
	c := NewClientWithExecutor(f)
	if _, err := c.GetReadyBeads("proj", "/tmp/proj", ""); err != nil {
		t.Fatalf("expected success after transient retries, got %v", err)
	}
	if f.calls != 3 { // 2 transient failures + 1 success
		t.Fatalf("expected 3 attempts, got %d", f.calls)
	}
}

func TestExecInDir_NoRetryOnNonTransient(t *testing.T) {
	f := &flakyExecutor{failsLeft: 5, failErr: errors.New("bead not found"), okOutput: []byte("[]")}
	c := NewClientWithExecutor(f)
	if _, err := c.GetReadyBeads("proj", "/tmp/proj", ""); err == nil {
		t.Fatal("expected error for non-transient failure")
	}
	if f.calls != 1 {
		t.Fatalf("non-transient must not retry; expected 1 attempt, got %d", f.calls)
	}
}

func TestIsTransientError(t *testing.T) {
	transient := []string{"br close X failed: exit status 8", "database is locked", "resource temporarily unavailable"}
	for _, s := range transient {
		if !isTransientError(errors.New(s)) {
			t.Fatalf("%q should be transient", s)
		}
	}
	if isTransientError(errors.New("bead not found")) {
		t.Fatal("logical error should not be transient")
	}
	if isTransientError(nil) {
		t.Fatal("nil should not be transient")
	}
}
