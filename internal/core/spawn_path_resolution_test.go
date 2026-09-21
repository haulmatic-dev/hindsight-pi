package core

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// spawnTestSpawner builds a minimal WorkerSpawner for guard tests. It leaves
// the worker binary unset so a guard miss would try to start a real process.
func spawnTestSpawner(ba *BeadAnalyzer) *WorkerSpawner {
	s := NewWorkerSpawner(
		make(map[string]*types.Worker),
		&sync.RWMutex{},
		nil, // workerTracker
		nil, // spawnRateLimiter
		nil, // errorLogger
		NewRetryManager(time.Second, time.Minute),
		&Config{},
		nil, // beadsClient
		ba,
		NewWorkerEnvBuilder(&Config{}),
		nil, // workerLifecycle
		nil, // userContexts
	)
	s.workerLifecycle = NewWorkerLifecycle(
		s.retryManager,
		nil, nil, nil, nil, &sync.RWMutex{},
		make(map[string]bool),
		func(string) {},
		func(*types.Worker, bool, int) error { return nil },
		func() {},
		nil,
		func() int { return 3 },
		nil,
	)
	return s
}

func TestSpawnInternal_RefusesEmptyProjectPath(t *testing.T) {
	ba := NewBeadAnalyzer(nil)
	s := spawnTestSpawner(ba)

	bead := types.Bead{ID: "orphan-xyz", Project: ""}
	_, err := s.SpawnInternal(bead)
	if err == nil {
		t.Fatal("expected spawn refusal for empty project path")
	}
	if !strings.Contains(err.Error(), "empty or relative") {
		t.Fatalf("error should say empty/relative: %v", err)
	}
}

func TestSpawnInternal_RefusesRelativeProjectPath(t *testing.T) {
	ba := NewBeadAnalyzer([]types.Project{
		{Name: "platform", Path: "projects-hydra-repro", Type: types.RepoTypeMulti, Repos: []types.Repo{{Name: "tiny-greeter"}}},
	})
	s := spawnTestSpawner(ba)

	bead := types.Bead{ID: "platform/tiny-greeter-u1i", Project: "platform", Repo: "tiny-greeter"}
	_, err := s.SpawnInternal(bead)
	if err == nil {
		t.Fatal("expected spawn refusal for relative project path")
	}
	if !strings.Contains(err.Error(), "empty or relative") {
		t.Fatalf("error should say empty/relative: %v", err)
	}
}
