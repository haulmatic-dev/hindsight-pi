package validation

import (
	"os"
	"testing"
)

// TestSRVFileHandlerActivitiesIsNotAStub is a smoke test that
// proves the new stub detector would not flag the FR-009 GREEN
// implementation in srv-file-handler. Run with:
//
//	cd /opt/dev-tools/components/hydra
//	go test ./internal/validation/ -run TestSRVFileHandler
//
// This is a one-time regression test; the real validation runs
// from the hydra worker against the live repo at bead-closure time.
func TestSRVFileHandlerActivitiesIsNotAStub(t *testing.T) {
	path := "/home/buddhi/projects/source-to-pay/srv-file-handler/internal/extraction/activities.go"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("srv-file-handler not present at %s; skipping smoke test", path)
	}
	stubs := DetectStubs(path)
	if len(stubs) > 0 {
		for _, s := range stubs {
			t.Errorf("FR-009 GREEN implementation should not contain stubs, but %s:%d is flagged: %s",
				s.Function, s.Line, s.Detail)
		}
	}
}

func TestSRVFileHandlerWorkerIsNotAStub(t *testing.T) {
	path := "/home/buddhi/projects/source-to-pay/srv-file-handler/cmd/worker/main.go"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("srv-file-handler not present at %s; skipping smoke test", path)
	}
	stubs := DetectStubs(path)
	if len(stubs) > 0 {
		for _, s := range stubs {
			t.Errorf("worker should not contain stubs, but %s:%d is flagged: %s",
				s.Function, s.Line, s.Detail)
		}
	}
}