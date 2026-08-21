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
			// Daemon-owned gates require isolation: the close-path gate runs in
			// the chain worktree and refuses main-checkout execution. Falling
			// back to the shared checkout also let hydra-5cl edit the daemon's
			// own live repo directly (2026-08-20). Fail closed instead.
			if daemonOwnedGates(s.config, bead.Project) {
				return nil, fmt.Errorf("worktree setup failed for %s and project has daemon_owned gates (no shared-checkout fallback): %w", bead.ID, wtErr)
			}
			log.Printf("[Worktree] setup failed for %s (using shared checkout): %v", bead.ID, wtErr)
		} else {
			// Fail-early dependency gate (bead hydra-code-jwrh): never spawn a
			// worker into an environment it cannot succeed in. A preflight
			// failure aborts the spawn; the bead stays open for retry.
			if pfErr := s.worktreeManager.Preflight(wt); pfErr != nil {
				return nil, fmt.Errorf("preflight failed for %s: %w", bead.ID, pfErr)
			}
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

	// Reflexion across worker processes: a retry spawn carries the previous
	// worker's failure so the new attempt's prompt can diagnose it instead of
	// re-running blind (in-process retries already do this via prompt state).
	if retryCount > 0 && s.retryManager != nil {
		if _, lastErr, _, ok := s.retryManager.RetryInfo(beadIDStr); ok && lastErr != nil {
			msg := lastErr.Error()
			if len(msg) > 500 {
				msg = "..." + msg[len(msg)-500:]
			}
			cmd.Env = append(cmd.Env, "HYDRA_PREV_FAILURE="+msg)
		}
	}

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

// openWorkerLogFile opens (creating parent dirs) the per-bead worker output
// log. Returns nil on failure, in which case callers fall back to streaming
// every line to the daemon log (previous behavior).
func openWorkerLogFile(beadID string, pid int) *os.File {
	dir := workersLogDir()
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
	written := 0
	capped := false
	for scanner.Scan() {
		line := scanner.Text()
		if f != nil {
			mu.Lock()
			if !capped {
				n, _ := fmt.Fprintf(f, "%s%s %s\n", time.Now().Format(time.RFC3339), tag, line)
				written += n
				if written > maxWorkerLogBytes {
					capped = true
					fmt.Fprintf(f, "%s%s [hydra] per-bead log capped at %d bytes; further output for this stream is dropped\n",
						time.Now().Format(time.RFC3339), tag, maxWorkerLogBytes)
				}
			}
			mu.Unlock()
			if isNotableWorkerLine(line) {
				log.Printf("[Worker:%s]%s %s", beadID, tag, truncateForDaemonLog(line))
			}
		} else {
			log.Printf("[Worker:%s]%s %s", beadID, tag, line)
		}
	}
}

// maxWorkerLogBytes bounds one stream's per-bead log.
//
// The per-bead log is the forensic record — fix_cause.go parses it to explain a
// failure — so it keeps full lines rather than the daemon log's 2 KB clip. It
// still needs a ceiling: on 2026-07-31 /opt/hydra/logs/workers held 30 GB across
// 1,590 files accumulated in two days, with single beads reaching 853 MB of pi
// stream JSON. Rotation cannot save a directory that grows at 15 GB a day.
//
// 32 MB is far more than any real failure needs to explain itself, and bounds
// the worst case to something a disk can hold. Notable lines still reach the
// daemon log after the cap, so a capped worker is not a silent worker.
const maxWorkerLogBytes = 32 << 20

// maxDaemonLogLine bounds a promoted worker line. A notable line's signal is at
// its start; the rest is payload that belongs in the per-bead log.
const maxDaemonLogLine = 2000

// truncateForDaemonLog shortens a worker line before it reaches the daemon log,
// marking the cut so nobody mistakes a clipped line for the whole story.
func truncateForDaemonLog(s string) string {
	if len(s) <= maxDaemonLogLine {
		return s
	}
	return s[:maxDaemonLogLine] + fmt.Sprintf(" …[truncated %d bytes; full line in the per-bead log]", len(s)-maxDaemonLogLine)
}

// isNotableWorkerLine reports whether a worker output line is important enough
// to also surface to the daemon log (errors, failures, rate limits), so those
// stay visible without the full per-bead firehose.
func isNotableWorkerLine(s string) bool {
	trimmed := strings.TrimSpace(s)

	// Hindsight memory recall lines quote past failures ("Task X failed
	// with...") — the recalled TEXT matching failure keywords does not make
	// the line itself notable.
	if strings.HasPrefix(trimmed, "Memory ") {
		return false
	}

	// pi's stream-JSON control events are machine traffic, and their payloads
	// (thinking text, tool results, echoed source) routinely contain "error",
	// "failed" and "timeout" as ordinary content. Promoting them wrote 1.56 GB
	// into the daemon log in 4h50m on 2026-07-31 at ~12 KB a line, and — worse
	// than the disk cost — it salted the log with phrases that made every
	// unanchored grep report failures that never happened. The per-bead log
	// keeps every one of these lines; the daemon log does not need them.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[{") {
		return false
	}

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
