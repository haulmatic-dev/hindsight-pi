package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIsNotableWorkerLine(t *testing.T) {
	notable := []string{
		"❌ Pi failed", "panic: nil deref", "Error: boom", "task failed",
		"exit code 1", "http 429 rate limit", "returned 401 unauthorized",
		"Unknown provider kimi", "authentication_error", "timed out",
	}
	for _, s := range notable {
		if !isNotableWorkerLine(s) {
			t.Errorf("expected notable: %q", s)
		}
	}
	chatter := []string{
		"Memory 12: 38366491-...", "Added skill flag: golang-pro",
		"reading file internal/core/foo.go", "ok", "Executing step: validate",
	}
	for _, s := range chatter {
		if isNotableWorkerLine(s) {
			t.Errorf("expected chatter (not notable): %q", s)
		}
	}
}

func TestStreamWorkerOutput_WritesToFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "wl-*.log")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	streamWorkerOutput(strings.NewReader("chatter line\n❌ bad thing\n"), "bead-1", "", f, &mu)
	_ = f.Close()

	data, _ := os.ReadFile(f.Name())
	s := string(data)
	if !strings.Contains(s, "chatter line") || !strings.Contains(s, "bad thing") {
		t.Errorf("per-bead file should contain all worker lines, got: %q", s)
	}
}

func TestWorkerLogDir_UsesHydraHome(t *testing.T) {
	t.Setenv("HYDRA_HOME", "/opt/hydra")
	if got := workersLogDir(); got != filepath.Join("/opt/hydra", "logs", "workers") {
		t.Errorf("workersLogDir = %q", got)
	}
}
