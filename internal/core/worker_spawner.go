package core

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerSpawner creates and manages worker processes.
type WorkerSpawner struct {
	mu               sync.Mutex
	activeWorkers    map[string]*types.Worker
	workerMutex      *sync.RWMutex
	workerTracker    *WorkerTracker
	spawnRateLimiter *SpawnRateLimiter
	errorLogger      *ErrorLogger
	retryManager     *RetryManager
	config           *Config
	beadsClient      BeadsClient
	beadAnalyzer     *BeadAnalyzer
	workerEnvBuilder *WorkerEnvBuilder
	workerLifecycle  *WorkerLifecycle
	userContexts     map[string]*UserContext // username -> context (for per-user git auth)
	worktreeManager  *WorktreeManager        // per-chain git isolation; nil = disabled
	chainKeyFn       func(types.Bead) string // maps a bead to its chain's worktree key
}

// SetWorktreeManager enables per-chain worktree isolation for spawned workers.
func (s *WorkerSpawner) SetWorktreeManager(m *WorktreeManager, chainKeyFn func(types.Bead) string) {
	s.worktreeManager = m
	s.chainKeyFn = chainKeyFn
}

// NewWorkerSpawner creates a new worker spawner.
func NewWorkerSpawner(
	activeWorkers map[string]*types.Worker,
	workerMutex *sync.RWMutex,
	workerTracker *WorkerTracker,
	spawnRateLimiter *SpawnRateLimiter,
	errorLogger *ErrorLogger,
	retryManager *RetryManager,
	config *Config,
	beadsClient BeadsClient,
	beadAnalyzer *BeadAnalyzer,
	workerEnvBuilder *WorkerEnvBuilder,
	workerLifecycle *WorkerLifecycle,
	userContexts map[string]*UserContext,
) *WorkerSpawner {
	return &WorkerSpawner{
		activeWorkers:    activeWorkers,
		workerMutex:      workerMutex,
		workerTracker:    workerTracker,
		spawnRateLimiter: spawnRateLimiter,
		errorLogger:      errorLogger,
		retryManager:     retryManager,
		config:           config,
		beadsClient:      beadsClient,
		beadAnalyzer:     beadAnalyzer,
		workerEnvBuilder: workerEnvBuilder,
		workerLifecycle:  workerLifecycle,
		userContexts:     userContexts,
	}
}

// Spawn creates and starts a worker for a bead with duplicate checking and limits.
func (s *WorkerSpawner) Spawn(bead types.Bead) error {
	beadIDStr := bead.ID.String()
	if beadIDStr == "" {
		return fmt.Errorf("cannot spawn worker for bead with empty ID")
	}

	if s.spawnRateLimiter.shouldBackoff() {
		backoff := time.Duration(1<<uint(s.spawnRateLimiter.FailureCount()-1)) * time.Second
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
		return fmt.Errorf("spawn backoff active: waiting %v before retry", backoff)
	}

	s.workerMutex.Lock()

	if _, exists := s.activeWorkers[beadIDStr]; exists {
		if s.workerTracker != nil {
			if workerRecord, found := s.workerTracker.GetWorkerByBead(beadIDStr); found {
				if workerRecord.Status == "completed" || workerRecord.Status == "failed" {
					log.Printf("[Overseer] Cleaning up stale worker %s (status: %s) from activeWorkers", beadIDStr, workerRecord.Status)
					delete(s.activeWorkers, beadIDStr)
				} else {
					s.workerMutex.Unlock()
					s.errorLogger.Error("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
					return fmt.Errorf("worker already exists for bead %s", beadIDStr)
				}
			} else {
				log.Printf("[Overseer] Cleaning up orphan worker %s (not in tracker)", beadIDStr)
				delete(s.activeWorkers, beadIDStr)
			}
		} else {
			s.workerMutex.Unlock()
			s.errorLogger.Error("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
			return fmt.Errorf("worker already exists for bead %s", beadIDStr)
		}
	}

	activeCount := len(s.activeWorkers)
	maxWorkers := s.config.Overseer.MaxWorkers
	if activeCount >= maxWorkers {
		s.workerMutex.Unlock()
		log.Printf("[Overseer] BLOCKING spawn for %s: at worker limit (%d/%d)", beadIDStr, activeCount, maxWorkers)
		return fmt.Errorf("worker limit reached: %d/%d workers active", activeCount, maxWorkers)
	}

	placeholderWorker := &types.Worker{
		ID:     beadIDStr,
		Status: "spawning",
	}
	s.activeWorkers[beadIDStr] = placeholderWorker
	s.workerMutex.Unlock()

	if count, lastErr, _, exists := s.retryManager.RetryInfo(beadIDStr); exists {
		maxRetries := s.config.Overseer.DefaultRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetries
		}
		if count >= maxRetries {
			err := &RetryExhaustedError{
				BeadID:    beadIDStr,
				Attempts:  count,
				LastError: lastErr,
			}
			s.errorLogger.Error("spawnWorker", "retry_exhausted", beadIDStr, err, "error")
			return err
		}
	}

	worker, err := s.SpawnInternal(bead)
	if err != nil {
		s.workerMutex.Lock()
		delete(s.activeWorkers, beadIDStr)
		s.workerMutex.Unlock()
		return err
	}

	s.workerMutex.Lock()
	s.activeWorkers[beadIDStr] = worker
	s.workerMutex.Unlock()

	s.spawnRateLimiter.ClearFailures()
	return nil
}

// SpawnInternal creates and starts a real worker process for a bead.
func (s *WorkerSpawner) SpawnInternal(bead types.Bead) (*types.Worker, error) {
	beadIDStr := bead.ID.String()

	agentTypes := types.DetermineAgentTypes(bead.Type)
	if len(agentTypes) == 0 {
		err := fmt.Errorf("no agent type found for bead type: %s", bead.Type)
		s.errorLogger.Error("spawnWorker", "agent_type", beadIDStr, err, "error")
		return nil, err
	}

	agentType := agentTypes[0]
	retryCount := s.retryManager.GetRetryCount(beadIDStr)
	if retryCount > 0 {
		log.Printf("[Worker] Spawning retry worker for bead: %s (attempt %d, agent: %s)", beadIDStr, retryCount+1, agentType)
	} else {
		log.Printf("[Worker] Spawning worker for bead: %s (agent: %s)", beadIDStr, agentType)
	}

	projectPath := s.beadAnalyzer.FindProjectPath(&bead)
	if projectPath == "" {
		projectPath = bead.Project
	}

	if s.beadsClient != nil && projectPath != "" {
		log.Printf("[Overseer] Marking bead %s as in_progress", beadIDStr)
		if err := s.beadsClient.UpdateBeadStatus(projectPath, bead.ID, types.StatusInProgress); err != nil {
			log.Printf("[Overseer] Debug: could not mark bead %s as in_progress: %v", beadIDStr, err)
		}
	}

	workerBinary := os.Getenv("HYDRA_WORKER_BINARY")
	if workerBinary == "" {
		workerBinary = filepath.Join(os.Getenv("HYDRA_HOME"), "bin", "worker")
	}

	// Per-chain worktree isolation: point the worker at the chain's dedicated
	// worktree (via HYDRA_PROJECT_PATH) instead of the shared checkout, when
	// enabled. The worker reads HYDRA_PROJECT_PATH as its working dir, so we
	// do not set cmd.Dir (which would break the `go run` fallback path).
	runDir := projectPath
	if s.worktreeManager != nil && s.chainKeyFn != nil {
		if wt, wtErr := s.worktreeManager.EnsureWorktree(projectPath, s.chainKeyFn(bead)); wtErr != nil {
			log.Printf("[Worktree] setup failed for %s (using shared checkout): %v", bead.ID, wtErr)
		} else {
			runDir = wt
		}
	}

	var cmd *exec.Cmd
	if _, err := os.Stat(workerBinary); err == nil {
		cmd = exec.Command(workerBinary)
	} else {
		cmd = exec.Command("go", "run", "cmd/worker/main.go")
	}
	cmd.Env = s.workerEnvBuilder.Build(bead, runDir, string(agentType), retryCount)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		spawnErr := &WorkerSpawnError{
			BeadID:    beadIDStr,
			AgentType: string(agentType),
			Err:       err,
		}
		s.errorLogger.Error("spawnWorker", "process_start", beadIDStr, spawnErr, "error")

		if strings.Contains(err.Error(), "resource temporarily unavailable") ||
			strings.Contains(err.Error(), "EAGAIN") ||
			strings.Contains(err.Error(), "fork") {
			s.spawnRateLimiter.RecordFailure()
		}

		return nil, spawnErr
	}

	log.Printf("[Worker] Process started with PID %d for bead %s", cmd.Process.Pid, beadIDStr)

	// Stream worker output to a per-bead file to keep the daemon log lean. The
	// full firehose (Pi reasoning, tool output, Hindsight recalls) lands in
	// <HYDRA_HOME>/logs/workers/<bead>-<pid>.log; only notable lines (errors,
	// failures, rate limits) are also surfaced to the daemon log.
	workerLog := openWorkerLogFile(beadIDStr, cmd.Process.Pid)
	var wlMu sync.Mutex
	var streamWG sync.WaitGroup
	streamWG.Add(2)
	go func() { defer streamWG.Done(); streamWorkerOutput(stdoutPipe, beadIDStr, "", workerLog, &wlMu) }()
	go func() {
		defer streamWG.Done()
		streamWorkerOutput(stderrPipe, beadIDStr, " [stderr]", workerLog, &wlMu)
	}()
	if workerLog != nil {
		go func() { streamWG.Wait(); _ = workerLog.Close() }()
	}

	// Resolve user context from project path
	username := s.findUserForProject(projectPath)

	worker := &types.Worker{
		ID:          beadIDStr,
		BeadID:      bead.ID,
		PID:         cmd.Process.Pid,
		AgentType:   agentType,
		StartTime:   time.Now(),
		Status:      "running",
		ProjectPath: projectPath,
		Username:    username,
	}

	if s.workerTracker != nil {
		if cmd.Process.Pid > 0 {
			if err := s.workerTracker.TrackWorker(cmd.Process.Pid, beadIDStr, projectPath); err != nil {
				log.Printf("[Overseer] Warning: failed to track worker PID %d: %v", cmd.Process.Pid, err)
			}
		}
	}

	go s.workerLifecycle.Monitor(worker, cmd)

	return worker, nil
}

// SetUserContexts sets the user context map for per-user project lookup.
func (s *WorkerSpawner) SetUserContexts(ctxs map[string]*UserContext) {
	s.userContexts = ctxs
}

// findUserForProject returns the username associated with a project path.
func (s *WorkerSpawner) findUserForProject(projectPath string) string {
	if s.userContexts == nil {
		return ""
	}
	for username, ctx := range s.userContexts {
		for _, p := range ctx.Config.Projects {
			if p.Path == projectPath {
				return username
			}
		}
	}
	return ""
}

// workerLogDir is the directory holding per-bead worker output logs.
func workerLogDir() string {
	base := os.Getenv("HYDRA_HOME")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "logs", "workers")
}

// openWorkerLogFile opens (creating parent dirs) the per-bead worker output
// log. Returns nil on failure, in which case callers fall back to streaming
// every line to the daemon log (previous behavior).
func openWorkerLogFile(beadID string, pid int) *os.File {
	dir := workerLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[Worker] could not create worker log dir %s: %v (streaming to daemon log)", dir, err)
		return nil
	}
	name := strings.NewReplacer("/", "-", " ", "-").Replace(beadID)
	f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%s-%d.log", name, pid)),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("[Worker] could not open worker log for %s: %v (streaming to daemon log)", beadID, err)
		return nil
	}
	return f
}

// streamWorkerOutput copies a worker pipe line-by-line into its per-bead log
// file, and additionally surfaces notable lines to the daemon log. When no
// file is available it falls back to logging every line to the daemon log.
func streamWorkerOutput(pipe io.Reader, beadID, tag string, f *os.File, mu *sync.Mutex) {
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if f != nil {
			mu.Lock()
			fmt.Fprintf(f, "%s%s %s\n", time.Now().Format(time.RFC3339), tag, line)
			mu.Unlock()
			if isNotableWorkerLine(line) {
				log.Printf("[Worker:%s]%s %s", beadID, tag, line)
			}
		} else {
			log.Printf("[Worker:%s]%s %s", beadID, tag, line)
		}
	}
}

// isNotableWorkerLine reports whether a worker output line is important enough
// to also surface to the daemon log (errors, failures, rate limits), so those
// stay visible without the full per-bead firehose.
func isNotableWorkerLine(s string) bool {
	l := strings.ToLower(s)
	for _, k := range []string{
		"❌", "panic", "fatal", "error:", "failed", "timed out", "timeout",
		"exit code", " 401", " 429", "unknown provider", "authentication_error",
	} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}
