package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerSpawner creates and manages worker processes.
type WorkerSpawner struct {
	mu                   sync.Mutex
	activeWorkers        map[string]*types.Worker
	workerMutex          *sync.RWMutex
	workerTracker        *WorkerTracker
	spawnRateLimiter     *SpawnRateLimiter
	errorLogger          *ErrorLogger
	retryManager         *RetryManager
	config               *Config
	beadsClient          BeadsClient
	beadAnalyzer         *BeadAnalyzer
	workerEnvBuilder     *WorkerEnvBuilder
	workerLifecycle      *WorkerLifecycle
	userContexts         map[string]*UserContext // username -> context (for per-user git auth)
	worktreeManager      *WorktreeManager        // per-chain git isolation; nil = disabled
	chainKeyFn           func(types.Bead) string // maps a bead to its chain's worktree key
	registryStore        *registry.Store         // daemon-owned source of worker truth
	parkedRespawnTimeout time.Duration           // parked rows older than this may be respawned
	// Records each chain's REAL worktree branch (possibly -vN versioned) at
	// chain-worktree creation, so completion integrates the branch that exists.
	chainCompletionDetector *ChainCompletionDetector
	// sessionResumePilot gates session-file resume on orphan respawn.
	sessionResumePilot *SessionResumePilot

	// eventSink carries pack_created/pack_refreshed/pack_miss. Nil-safe:
	// pack events are observability, never a spawn gate.
	eventSink *EventSink

	// scoutRunner is the agentic fallback for requirements the mechanical
	// pack cannot map. Nil (or scout disabled in config) = mechanical-only
	// packs, zero scout spend.
	scoutRunner ScoutRunner

	spawnFailureMu sync.RWMutex
	spawnFailures  map[string]*spawnFailureInfo // bead ID -> last spawn failure

	// packBreaker is the per-project circuit breaker for context-pack writes:
	// a store the daemon cannot write must not be hammered on every 2-3s
	// scheduler tick. Guarded by packBreakerMu; see context_pack.go.
	packBreakerMu sync.Mutex
	packBreakers  map[string]*packBreakerState
}

const defaultParkedRespawnTimeout = 10 * time.Minute

type spawnFailureInfo struct {
	err       string
	timestamp time.Time
}

// SpawnSuppressedError is returned when a spawn is aborted because the bead is
// already in a terminal state in the store. This prevents in-memory retry queues
// from resurrecting closed/completed beads.
type SpawnSuppressedError struct {
	BeadID string
	Status types.BeadStatus
}

func (e *SpawnSuppressedError) Error() string {
	return fmt.Sprintf("spawn suppressed: bead %s is already terminal (%s)", e.BeadID, e.Status)
}

// SetChainCompletionDetector lets the spawner record each chain's REAL
// worktree branch (possibly a -vN versioned name) at chain-worktree creation.
func (s *WorkerSpawner) SetChainCompletionDetector(d *ChainCompletionDetector) {
	s.chainCompletionDetector = d
}

// SetWorktreeManager enables per-chain worktree isolation for spawned workers.
func (s *WorkerSpawner) SetWorktreeManager(m *WorktreeManager, chainKeyFn func(types.Bead) string) {
	s.worktreeManager = m
	s.chainKeyFn = chainKeyFn
	if m != nil {
		m.SetEventSink(s.eventSink)
	}
}

// SetRegistryStore wires the daemon's registry store so the duplicate-spawn
// check can see parked and working rows that survive a scheduler tick or
// daemon restart.
func (s *WorkerSpawner) SetRegistryStore(st *registry.Store) {
	s.registryStore = st
	if s.parkedRespawnTimeout == 0 {
		s.parkedRespawnTimeout = defaultParkedRespawnTimeout
	}
}

// SetSessionResumePilot wires the session-file resume pilot.
func (s *WorkerSpawner) SetSessionResumePilot(p *SessionResumePilot) {
	s.sessionResumePilot = p
}

// SetEventSink wires the structured event sink used for pack lifecycle
// events (pack_created/pack_refreshed/pack_miss).
func (s *WorkerSpawner) SetEventSink(sink *EventSink) {
	s.eventSink = sink
	if s.worktreeManager != nil {
		s.worktreeManager.SetEventSink(sink)
	}
}

// SetScoutRunner wires the agentic scout fallback for low-confidence pack
// mappings. Nil leaves packs mechanical-only (the default).
func (s *WorkerSpawner) SetScoutRunner(r ScoutRunner) {
	s.scoutRunner = r
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
		spawnFailures:    make(map[string]*spawnFailureInfo),
	}
}

// recordSpawnFailure stores the last non-nil error returned by Spawn for a
// bead. It is used by Pan to surface blocked beads that never created a worker
// row. Callers must not hold spawner mutexes.
func (s *WorkerSpawner) recordSpawnFailure(beadID string, err error) {
	if err == nil {
		return
	}
	s.spawnFailureMu.Lock()
	defer s.spawnFailureMu.Unlock()
	if s.spawnFailures == nil {
		s.spawnFailures = make(map[string]*spawnFailureInfo)
	}
	s.spawnFailures[beadID] = &spawnFailureInfo{err: err.Error(), timestamp: time.Now()}
}

// ClearSpawningPlaceholder removes a stale "spawning" placeholder from the
// active-workers map. Called by Pan's reap-placeholder repair for beads that
// never created a real worker row. Returns true if a placeholder was removed.
func (s *WorkerSpawner) ClearSpawningPlaceholder(beadID string) bool {
	s.workerMutex.Lock()
	defer s.workerMutex.Unlock()
	w, ok := s.activeWorkers[beadID]
	if !ok || w == nil || w.Status != "spawning" {
		return false
	}
	delete(s.activeWorkers, beadID)
	log.Printf("[WorkerSpawner] cleared spawning placeholder for %s", beadID)
	return true
}

// LastSpawnFailure returns the last recorded spawn failure for a bead.
// If no failure has been recorded it returns zero values.
func (s *WorkerSpawner) LastSpawnFailure(beadID string) (reason string, timestamp time.Time, ok bool) {
	s.spawnFailureMu.RLock()
	defer s.spawnFailureMu.RUnlock()
	if info, exists := s.spawnFailures[beadID]; exists {
		return info.err, info.timestamp, true
	}
	return "", time.Time{}, false
}

// ClearSpawnFailure removes the last recorded spawn failure for a bead.
func (s *WorkerSpawner) ClearSpawnFailure(beadID string) {
	s.spawnFailureMu.Lock()
	defer s.spawnFailureMu.Unlock()
	if _, exists := s.spawnFailures[beadID]; exists {
		delete(s.spawnFailures, beadID)
		log.Printf("[WorkerSpawner] cleared spawn failure for %s", beadID)
	}
}

// ListSpawnFailures returns a snapshot of all bead IDs that have a recorded
// spawn failure.
func (s *WorkerSpawner) ListSpawnFailures() []string {
	s.spawnFailureMu.RLock()
	defer s.spawnFailureMu.RUnlock()
	out := make([]string, 0, len(s.spawnFailures))
	for beadID := range s.spawnFailures {
		out = append(out, beadID)
	}
	return out
}

// HasLiveRegistryWorker reports whether the daemon's registry contains a
// live (non-terminal) worker row for the bead. It is exported for the
// scheduler's duplicate-spawn guard.
func (s *WorkerSpawner) HasLiveRegistryWorker(beadID string) bool {
	live, _ := s.isRegistryWorkerLive(beadID)
	return live
}

// isRegistryWorkerLive reports whether a non-terminal worker row exists in
// the daemon's registry. Parked rows are treated as live workers that block
// respawn, unless the park is older than parkedRespawnTimeout.
func (s *WorkerSpawner) isRegistryWorkerLive(beadID string) (bool, error) {
	if s.registryStore == nil {
		return false, nil
	}
	rec, ok, err := s.registryStore.GetWorker(beadID)
	if err != nil {
		log.Printf("[WorkerSpawner] registry lookup failed for %s: %v", beadID, err)
		return false, nil
	}
	if !ok {
		return false, nil
	}
	if rec.State.Terminal() {
		return false, nil
	}
	if rec.State == registry.StateParked {
		timeout := s.parkedRespawnTimeout
		if timeout <= 0 {
			timeout = defaultParkedRespawnTimeout
		}
		if !rec.ParkSince.IsZero() && time.Since(rec.ParkSince) > timeout {
			log.Printf("[WorkerSpawner] parked row for %s is older than %v; allowing respawn", beadID, timeout)
			return false, nil
		}
	}
	return true, nil
}

// Spawn creates and starts a worker for a bead with duplicate checking and limits.
func (s *WorkerSpawner) Spawn(bead types.Bead) (err error) {
	beadIDStr := bead.ID.String()
	if beadIDStr == "" {
		return fmt.Errorf("cannot spawn worker for bead with empty ID")
	}

	defer func() {
		if err != nil {
			s.recordSpawnFailure(beadIDStr, err)
		}
	}()

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
				// A {status: spawning} placeholder is an in-flight spawn that has
				// not reached the tracker yet — proof of life, NOT an orphan.
				// Deleting it here double-spawned j81fp (2026-08-21).
				if existing, ok := s.activeWorkers[beadIDStr]; ok && existing.Status == "spawning" {
					s.workerMutex.Unlock()
					s.errorLogger.Error("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("spawn already in flight"), "warn")
					return fmt.Errorf("spawn already in flight for bead %s", beadIDStr)
				}
				log.Printf("[Overseer] Cleaning up orphan worker %s (not in tracker)", beadIDStr)
				delete(s.activeWorkers, beadIDStr)
			}
		} else {
			s.workerMutex.Unlock()
			s.errorLogger.Error("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
			return fmt.Errorf("worker already exists for bead %s", beadIDStr)
		}
	}

	if live, _ := s.isRegistryWorkerLive(beadIDStr); live {
		s.workerMutex.Unlock()
		s.errorLogger.Error("spawnWorker", "duplicate_check", beadIDStr, fmt.Errorf("worker already exists"), "warn")
		return fmt.Errorf("worker already exists for bead %s", beadIDStr)
	}

	activeCount := len(s.activeWorkers)
	maxWorkers := s.config.Overseer.MaxWorkers
	if activeCount >= maxWorkers {
		s.workerMutex.Unlock()
		log.Printf("[Overseer] BLOCKING spawn for %s: at worker limit (%d/%d)", beadIDStr, activeCount, maxWorkers)
		return fmt.Errorf("worker limit reached: %d/%d workers active", activeCount, maxWorkers)
	}

	placeholderWorker := &types.Worker{
		ID:        beadIDStr,
		Status:    "spawning",
		StartTime: time.Now(),
	}
	s.activeWorkers[beadIDStr] = placeholderWorker
	s.workerMutex.Unlock()

	// The placeholder owns this slot for the duration of the spawn attempt. Any
	// failure after this point must remove it; otherwise a {status:"spawning"}
	// phantom blocks the bead forever (pool deadlock, 2026-08-24).
	spawned := false
	defer func() {
		if spawned {
			return
		}
		s.workerMutex.Lock()
		delete(s.activeWorkers, beadIDStr)
		s.workerMutex.Unlock()
	}()

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
		return err
	}

	spawned = true
	s.workerMutex.Lock()
	s.activeWorkers[beadIDStr] = worker
	s.workerMutex.Unlock()

	s.spawnRateLimiter.ClearFailures()
	return nil
}

// worktreeUnavailableLabel is the durable hard-park marker for a bead whose
// chain worktree could not be created or verified. Unlike a status, a label
// is visible to every consumer that decides respawn (retry queue, DAG ready
// filter, Charon orphan recovery), so the bead cannot be silently retried
// into the same spawn failure until a human fixes the worktree and clears
// the label.
const worktreeUnavailableLabel = "worktree-unavailable"

// SpawnInternal creates and starts a real worker process for a bead.
func (s *WorkerSpawner) SpawnInternal(bead types.Bead) (*types.Worker, error) {
	beadIDStr := bead.ID.String()

	// Last-line defense: never spawn a worker on a HOLD gate, no matter which
	// path reached the spawner. A worker treats any bead as work and closes
	// it; for a gate that silently releases every dependent (mz0v, zibq —
	// 2026-09-25).
	if isHoldBead(bead) {
		err := fmt.Errorf("refusing to spawn worker on HOLD gate bead: %s", beadIDStr)
		log.Printf("⏸️  %v", err)
		return nil, err
	}

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
	// No fallback to the raw Project field: it is a NAME, not a path, and is
	// empty on the scheduler path. Unresolved stays "" and downstream fails
	// closed (EnsureWorktree refuses empty; daemon_owned blocks shared
	// checkout) — never worktree the daemon's cwd (1xs7z, 2026-08-23).
	if projectPath == "" {
		log.Printf("[Overseer] project path unresolved for %s (project %q) — spawn will fail closed", bead.ID, bead.Project)
	}

	// Fail-closed spawn guard (telematics incident, 2026-09-21): an empty or
	// relative project path would run git/worktree operations in the daemon's
	// cwd or walk up into a parent repo (the /opt/dev-tools monorepo). Refuse
	// and emit a typed event so the failure is visible downstream.
	if projectPath == "" || !filepath.IsAbs(projectPath) {
		reason := "empty_project_path"
		if projectPath != "" {
			reason = "relative_project_path"
		}
		if s.eventSink != nil {
			s.eventSink.Emit(Event{
				Type:    "spawn_refused",
				Project: bead.Project,
				Bead:    beadIDStr,
				Reason:  reason,
				Detail:  fmt.Sprintf("refusing spawn for %s: project path %q is not absolute", beadIDStr, projectPath),
				Fields: map[string]any{
					"project_path": projectPath,
				},
			})
		}
		return nil, fmt.Errorf("refusing spawn for %s: project path %q is empty or relative", beadIDStr, projectPath)
	}

	// Context pack: built just-in-time at chain first-member spawn, SHA-keyed
	// refresh at each later member spawn. Fail-open by construction — a pack
	// failure must never block the spawn it was meant to guide.
	s.ensureContextPack(bead, projectPath)

	if projectPath != "" && s.beadsClient != nil {
		// Defense in depth: refuse to mark a terminal bead as in_progress.
		// The scheduler's pre-spawn gate should already have caught this, but
		// store status can change between scheduling and the actual fork.
		if current, err := s.beadsClient.GetBead(projectPath, "", bead.ID); err == nil && current != nil && current.Status.IsTerminal() {
			return nil, &SpawnSuppressedError{BeadID: beadIDStr, Status: current.Status}
		}

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
		key := s.chainKeyFn(bead)
		// Outcome is logged per spawn (gybo4, 2026-09-10: EnsureWorktree was
		// silent and the missing-dir failure surfaced 8s later as a bogus
		// 'fork/exec /usr/bin/pi' error). created/reused is derived from a
		// pre-check so EnsureWorktree's signature stays unchanged.
		_, existed := s.worktreeManager.ExistingWorktree(projectPath, key)
		if wt, wtErr := s.worktreeManager.EnsureWorktree(projectPath, key); wtErr != nil {
			// Fail closed, ALWAYS — never silently downgrade isolation by running
			// the worker in the shared (main) checkout. Observed 2026-09-11/12:
			// fallback workers committed merge-conflict markers into
			// .beads/issues.jsonl, wiped a project's .beads dir via git clean -fdx,
			// and landed janitorial commits on the user's working branch. The
			// daemon-owned-gates-only variant of this guard (hydra-5cl, 2026-08-20)
			// left the corruption door open for every non-gated project. Hard-park
			// the bead with a durable label so no respawn path retries it.
			if s.beadsClient != nil && projectPath != "" {
				if lblErr := s.beadsClient.UpdateBeadLabels(projectPath, "", bead.ID, []string{worktreeUnavailableLabel}); lblErr != nil {
					log.Printf("[Worktree] failed to hard-park %s with %q label: %v", bead.ID, worktreeUnavailableLabel, lblErr)
				}
			}
			return nil, fmt.Errorf("worktree setup failed for %s (%s, hard-parked): %w", bead.ID, worktreeUnavailableLabel, wtErr)
		} else {
			if existed {
				log.Printf("[Worktree] bead %s: reused worktree %s", bead.ID, wt)
			} else {
				log.Printf("[Worktree] bead %s: created worktree %s", bead.ID, wt)
			}
			// Record the REAL branch name on the chain. WorktreeManager versions
			// stale branches to hydra/wt-<key>-vN; chain completion must integrate
			// the branch that actually exists, not the one re-derived from the key
			// (webapp-tc5: 1,642 insertions stranded, 2026-09-16).
			if s.chainCompletionDetector != nil {
				s.chainCompletionDetector.RecordChainWorktree(key, s.worktreeManager.ResolveBranch(projectPath, key))
			}
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

	// A successful spawn supersedes any earlier failure record; leaving it
	// would keep the bead in Pan's blocked candidacy while a live worker runs.
	s.ClearSpawnFailure(beadIDStr)

	// Session-file resume pilot (Self-heal recovery A): only resume after an
	// infra-class death, with attempt-boundary injection, HEAD check, and
	// age/size caps. Post-reap safety is implicit: SpawnInternal is only
	// reached after the duplicate-spawn guards confirm no live worker.
	if s.sessionResumePilot != nil && s.workerTracker != nil && s.retryManager != nil && cmd.Process != nil {
		head := currentGitHead(runDir)
		if head != "" {
			if err := s.workerTracker.SetWorkerSpawnHead(cmd.Process.Pid, head); err != nil {
				log.Printf("[WorkerSpawner] failed to record spawn HEAD for %s: %v", beadIDStr, err)
			}
		}

		sessionID := PiSessionID(beadIDStr)
		sessionDir := PiSessionDir(beadIDStr, os.Getenv("HYDRA_HOME"))
		age, size, exists, serr := SessionFileStats(sessionDir)
		if serr != nil && exists {
			log.Printf("[WorkerSpawner] session stats error for %s: %v", beadIDStr, serr)
		}

		lastInfra := s.retryManager.IsInfra(beadIDStr)
		storedHead := ""
		if prev, ok := s.workerTracker.GetPreviousWorkerRecordByBead(beadIDStr, cmd.Process.Pid); ok {
			storedHead = prev.SpawnHead
		}

		resume := false
		resumeReason := ""
		if !exists {
			resumeReason = "no prior session"
		} else {
			resume, resumeReason = s.sessionResumePilot.ShouldResume(beadIDStr, retryCount, lastInfra, sessionDir, age, size, head, storedHead)
		}

		if resume {
			cmd.Env = append(cmd.Env,
				"HYDRA_RESUME_SESSION=1",
				fmt.Sprintf("HYDRA_RESUME_SESSION_ID=%s", sessionID),
				fmt.Sprintf("HYDRA_RESUME_SESSION_DIR=%s", sessionDir),
			)
			if s.beadsClient != nil && projectPath != "" {
				if err := s.beadsClient.UpdateBeadLabels(projectPath, "", bead.ID, []string{sessionResumedLabel}); err != nil {
					log.Printf("[WorkerSpawner] failed to label resumed bead %s: %v", beadIDStr, err)
				}
			}
			log.Printf("[WorkerSpawner] resuming Pi session for %s (attempt %d, dir=%s)", beadIDStr, retryCount+1, sessionDir)
		} else {
			log.Printf("[WorkerSpawner] not resuming session for %s: %s", beadIDStr, resumeReason)
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
// It matches both the configured project path and each configured repo path
// (relative or absolute) so per-repo paths for multi projects resolve to the
// owning user.
func (s *WorkerSpawner) findUserForProject(projectPath string) string {
	if s.userContexts == nil {
		return ""
	}
	for username, ctx := range s.userContexts {
		for _, p := range ctx.Config.Projects {
			if p.Path == projectPath {
				return username
			}
			if p.Type == "multi" {
				for _, r := range p.Repos {
					segment := r.Path
					if segment == "" {
						segment = r.Name
					}
					if filepath.IsAbs(segment) {
						if segment == projectPath {
							return username
						}
						continue
					}
					if filepath.Join(p.Path, segment) == projectPath {
						return username
					}
				}
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

// currentGitHead returns the HEAD commit of dir, or empty string on error.
func currentGitHead(dir string) string {
	if dir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// maxPackChainMembers bounds the per-spawn chain scan: fetching every
// member's bead to find an existing pack costs one `br show` each, fine for
// chains, not for epic-scale lists.
const maxPackChainMembers = 50

// ensureContextPack builds or refreshes the chain's context pack just-in-time
// at member spawn. First member spawn with no pack anywhere in the chain →
// build and write to every member (pack_created). Later spawns compare the
// pack's base_sha against the working-branch head: moved → regenerate the
// mechanical sections, re-validate (not regenerate) the agentic file anchors,
// drop broken ones with pack_miss events (pack_refreshed). Unchanged → reuse.
//
// Fail-open throughout: any error logs and the spawn proceeds unguided. The
// pack is an optimization; it must never gate execution.
func (s *WorkerSpawner) ensureContextPack(bead types.Bead, projectPath string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ContextPack] ensureContextPack panicked for %s (fail-open): %v", bead.ID, r)
		}
	}()
	if s.chainCompletionDetector == nil || s.beadsClient == nil || projectPath == "" {
		return
	}
	// Circuit breaker: if pack writes for this project keep failing (e.g. the
	// daemon user cannot write the br store), stop rebuilding/retrying on
	// every spawn and ride the exponential backoff instead. Fail-open: the
	// spawn proceeds unguided.
	if !s.packBreakerAllowed(bead.Project, time.Now()) {
		log.Printf("[ContextPack] breaker open for %s; skipping pack for %s (fail-open)", bead.Project, bead.ID)
		return
	}
	chain, ok := s.chainCompletionDetector.FindChainByBead(bead.ID)
	if !ok {
		// Chain-of-one fallback: standalone beads (fix-beads spawned outside a
		// chain) are exactly the unguided cohort the pack was built for
		// (measured: 68-turn p50 unguided vs 20 guided). Treat the bead as a
		// chain of itself so it gets the same mechanical pack + SHA refresh.
		chain = &ChainInfo{RootBeadID: bead.ID, AllBeadIDs: []types.BeadID{bead.ID}, ProjectPath: projectPath}
	}
	if len(chain.AllBeadIDs) == 0 {
		return
	}
	if len(chain.AllBeadIDs) > maxPackChainMembers {
		log.Printf("[ContextPack] chain has %d members (> %d); skipping pack for %s", len(chain.AllBeadIDs), maxPackChainMembers, bead.ID)
		return
	}

	// One pass over chain members: collect titles for requirement terms and
	// find any member already carrying a parseable pack.
	var existing *ContextPack
	var terms []string
	var titles []string
	for _, id := range chain.AllBeadIDs {
		b, err := s.beadsClient.GetBead(projectPath, bead.Repo, id)
		if err != nil || b == nil {
			continue
		}
		terms = append(terms, RequirementTermsForBead(b.Title)...)
		titles = append(titles, b.Title)
		if existing == nil {
			if p, err := ParseContextPack(b.AgentContext); err == nil && p != nil {
				existing = p
			}
		}
	}
	if len(terms) > 16 {
		terms = terms[:16]
	}

	gates := gatesForProject(s.config, bead.Project)
	head := currentGitHead(projectPath)

	switch {
	case existing == nil:
		pack := BuildContextPack(projectPath, gates, terms)
		// Agentic fallback: requirements the mechanical index could not map
		// at confidence get ONE bounded scout pass (chain-level, not per bead).
		unmapped := UnmappedRequirements(titles, pack.Files, PackScoutConfidenceThreshold)
		pack.Files = s.scoutUnmapped(bead, projectPath, unmapped, pack.Files)
		log.Printf("[ContextPack] built pack for chain of %s (base_sha=%s, gates=%d, anchors=%d)",
			bead.ID, pack.BaseSHA, len(pack.GateCommands), len(pack.Files))
		s.writePackToChain(chain, projectPath, bead, pack, "pack_created")
	case head != "" && existing.BaseSHA != head:
		// SHA moved: mechanical sections are stale (gate commands, spec
		// pattern, dir listing all derive from the tree). Regenerate them;
		// the agentic anchor mapping is re-validated, not regenerated —
		// except anchors that BROKE, which get a targeted per-requirement
		// re-scout, never a whole-chain redo.
		refreshed := BuildContextPack(projectPath, gates, nil)
		refreshed.Files = existing.Files
		dropped := ValidatePackAnchors(refreshed, projectPath)
		logPackMiss(s.eventSink, bead.Project, string(bead.ID), dropped)
		reScout := DroppedAgenticRequirements(dropped)
		if len(reScout) > 0 {
			refreshed.Files = s.scoutUnmapped(bead, projectPath, reScout, refreshed.Files)
		}
		log.Printf("[ContextPack] refreshed pack for chain of %s (base_sha %s → %s, dropped %d anchors)",
			bead.ID, existing.BaseSHA, refreshed.BaseSHA, len(dropped))
		s.writePackToChain(chain, projectPath, bead, refreshed, "pack_refreshed")
	default:
		log.Printf("[ContextPack] pack for chain of %s still fresh (base_sha=%s)", bead.ID, existing.BaseSHA)
	}
}

// scoutUnmapped runs ONE bounded scout pass for the given requirements and
// merges the mappings into files (provenance agentic). Fail-open: a scout
// error logs a pack_scout event and returns files untouched, so the spawn
// proceeds with the mechanical-only pack. Serialized daemon-wide so two
// spawns of the same chain cannot double-spend on the same pass.
func (s *WorkerSpawner) scoutUnmapped(bead types.Bead, projectPath string, requirements []string, files []PackFileAnchor) []PackFileAnchor {
	if len(requirements) == 0 {
		return files
	}
	scoutCfg := resolveScoutConfig(s.config)
	if !scoutCfg.enabled || s.scoutRunner == nil {
		return files
	}
	scoutGate.Lock()
	defer scoutGate.Unlock()
	start := time.Now()
	mappings, err := s.scoutRunner.RunScout(context.Background(), projectPath, requirements, scoutCfg.maxTurns)
	emitPackScout(s.eventSink, bead.Project, string(bead.ID), len(requirements), len(mappings), time.Since(start), err)
	if err != nil {
		log.Printf("[ContextPack] scout failed for chain of %s (fail-open): %v", bead.ID, err)
		return files
	}
	pack := &ContextPack{Files: files}
	MergeScoutMappings(pack, mappings, requirements)
	return pack.Files
}

// writePackToChain serializes the pack and writes it to every chain member's
// agent_context. Per-member failures log and continue (fail-open: a partially
// guided chain beats an unspawned one). Emits the typed event per successful
// write on the fail-open event stream.
func (s *WorkerSpawner) writePackToChain(chain *ChainInfo, projectPath string, bead types.Bead, pack *ContextPack, eventType string) {
	raw, err := json.Marshal(pack)
	if err != nil {
		log.Printf("[ContextPack] marshal failed for chain of %s (fail-open): %v", bead.ID, err)
		return
	}
	writeErr := error(nil)
	for _, id := range chain.AllBeadIDs {
		if err := s.beadsClient.UpdateBeadAgentContext(projectPath, bead.Repo, id, string(raw)); err != nil {
			log.Printf("[ContextPack] %s write failed for %s (fail-open): %v", eventType, id, err)
			writeErr = err
			continue
		}
		if s.eventSink != nil {
			s.eventSink.Emit(Event{Type: eventType, Project: bead.Project, Bead: string(id),
				Reason: "sha_" + pack.BaseSHA[:min(12, len(pack.BaseSHA))],
				Detail: "context pack written to chain member (base_sha " + pack.BaseSHA + ")"})
		}
	}
	// Breaker accounting: one failure/success per write pass (not per member),
	// so a large chain does not trip the threshold on a single tick.
	if writeErr != nil {
		s.packBreakerFailure(bead.Project, string(bead.ID), time.Now(), writeErr)
	} else {
		s.packBreakerSuccess(bead.Project, time.Now())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
