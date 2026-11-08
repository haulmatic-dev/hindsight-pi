package main

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// stateReporter heartbeats the worker's state to the daemon registry and
// drains shepherd commands (PRD worker-park-and-resume Parts 2 & 4).
//
// Fail-open by contract: a nil client (HYDRA_API_URL unset) or any daemon
// error degrades to no-op logging; reporting never blocks bead execution.
type stateReporter struct {
	client *registry.Client

	mu       sync.Mutex
	rep      registry.WorkerReport
	commands []registry.Command
	runDir   string // worker's working dir (HYDRA_PROJECT_PATH); canary target

	stop     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

const reporterInterval = 30 * time.Second

// activeReporter is the process-global reporter for code paths (like the
// rpc scanner goroutine) that do not receive it via the call chain. The
// worker is a single-bead process, so one global is exact.
var activeReporter *stateReporter

func newStateReporter(client *registry.Client, beadID, project string) *stateReporter {
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid)
	runDir := os.Getenv("HYDRA_PROJECT_PATH")
	return &stateReporter{
		client: client,
		rep: registry.WorkerReport{
			BeadID:      beadID,
			PID:         pid,
			PGID:        pgid,
			Project:     project,
			State:       string(registry.StateWorking),
			WorktreeKey: worktreeKeyFromDir(runDir),
		},
		runDir:  runDir,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// worktreeKeyFromDir returns the chain worktree key when dir is a hydra
// worktree path (<root>/.hydra-worktrees/<encoded-project>/<key>), else "".
// The daemon stores it on the registry row so the reconciler/GC can protect
// live sandboxes from durable state instead of in-memory chain registration.
func worktreeKeyFromDir(dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	if filepath.Base(filepath.Dir(filepath.Dir(dir))) != ".hydra-worktrees" {
		return ""
	}
	return filepath.Base(dir)
}

// workspaceDestroyed reports whether the worker's worktree lost its .git
// mid-run. 2026-09-16: a chain-timeout unregistered the chain, the
// reconciler's protection set silently dropped the key, and the sweep
// deleted a live worker's sandbox — the worker flailed in the empty husk
// for 25 minutes burning turns. Fail fast instead.
func workspaceDestroyed(runDir string) bool {
	if worktreeKeyFromDir(runDir) == "" {
		return false // not a worktree run; nothing to guard
	}
	_, err := os.Stat(filepath.Join(runDir, ".git"))
	return err != nil
}

// Enabled reports whether reporting is configured.
func (r *stateReporter) Enabled() bool { return r != nil && r.client != nil }

// Start launches the heartbeat/poll loop. No-op when disabled.
func (r *stateReporter) Start() {
	if !r.Enabled() {
		return
	}
	r.beat() // register immediately so the daemon sees the worker
	go func() {
		defer close(r.stopped)
		ticker := time.NewTicker(reporterInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.beat()
			case <-r.stop:
				return
			}
		}
	}()
	log.Printf("[Registry] reporting to daemon every %v", reporterInterval)
}

func (r *stateReporter) beat() {
	if workspaceDestroyed(r.runDir) {
		r.mu.Lock()
		r.rep.State = string(registry.StateParked)
		r.rep.ParkReason = "workspace-destroyed"
		rep := r.rep
		r.mu.Unlock()
		if err := r.client.Report(rep); err != nil {
			log.Printf("[Registry] workspace-destroyed report failed: %v", err)
		}
		log.Printf("[Registry] FATAL: worktree %s lost its .git mid-run — parked as workspace-destroyed, exiting instead of flailing in the husk", r.runDir)
		os.Exit(1)
	}
	r.mu.Lock()
	rep := r.rep
	r.mu.Unlock()

	if err := r.client.Report(rep); err != nil {
		log.Printf("[Registry] report failed (continuing): %v", err)
	}
	cmds, err := r.client.PollCommands(rep.BeadID)
	if err != nil {
		log.Printf("[Registry] command poll failed (continuing): %v", err)
		return
	}
	if len(cmds) > 0 {
		r.mu.Lock()
		r.commands = append(r.commands, cmds...)
		r.mu.Unlock()
		for _, c := range cmds {
			log.Printf("[Registry] shepherd command received: %s (%s)", c.Kind, truncateLog(c.Payload, 120))
		}
	}
}

// SetState updates the reported lifecycle state.
func (r *stateReporter) SetState(state registry.WorkerState) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.State = string(state)
	r.mu.Unlock()
}

// SetError updates the reported error triple.
func (r *stateReporter) SetError(status int, errType, message string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.ErrStatus = status
	r.rep.ErrType = errType
	r.rep.ErrMessage = truncateLog(message, 500)
	r.mu.Unlock()
}

// SetPark updates the reported park fields.
func (r *stateReporter) SetPark(since, wakeAt time.Time, reason string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.ParkSince = since
	r.rep.ParkWakeAt = wakeAt
	r.rep.ParkReason = reason
	r.mu.Unlock()
}

// SetTokens updates reported token progress.
func (r *stateReporter) SetTokens(total int64) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.TokensSoFar = total
	r.mu.Unlock()
}

// SetTokenBreakdown updates the cache-aware token counters that drive budgets
// and backstops. total is the provider-facing total; fresh is fresh_input +
// output and is what Charon/Pan arbitrate on. Every usage-bearing event also
// stamps LastRpcActivity — turn usage is an rpc-activity signal in its own
// right.
func (r *stateReporter) SetTokenBreakdown(total, fresh, cacheRead, cacheCreation, output int64) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.TokensSoFar = total
	r.rep.TokensFresh = fresh
	r.rep.TokensCacheRead = cacheRead
	r.rep.TokensCacheCreation = cacheCreation
	r.rep.TokensOutput = output
	r.rep.LastRpcActivity = time.Now()
	r.mu.Unlock()
}

// SetRpcActivity stamps LastRpcActivity for usage-free rpc signals (extension
// notifications) so Charon's zero-token classifier can tell an active session
// from a hung one.
func (r *stateReporter) SetRpcActivity() {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.LastRpcActivity = time.Now()
	r.mu.Unlock()
}

// SetTurns updates the reported agent-turn count (rpc turn_end events) so
// the registry shows turns_so_far alongside tokens_so_far (FR-004).
func (r *stateReporter) SetTurns(n int64) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.TurnsSoFar = n
	r.mu.Unlock()
}

// SetSessionID updates the reported pi session id.
func (r *stateReporter) SetSessionID(id string) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.PiSessionID = id
	r.mu.Unlock()
}

// SetNudges updates the reported verdict-nudge count.
func (r *stateReporter) SetNudges(n int) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.NudgeCount = n
	r.mu.Unlock()
}

// DrainCommands returns and clears commands received since the last drain.
func (r *stateReporter) DrainCommands() []registry.Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.commands
	r.commands = nil
	return out
}

// Stop posts the completion record and halts the loop. exitCode is the
// worker's own exit code.
func (r *stateReporter) Stop(exitCode int) {
	if !r.Enabled() {
		return
	}
	r.stopOnce.Do(func() {
		// Flush a final full heartbeat BEFORE the bare completion marker:
		// the completion endpoint carries only the exit code, and workers
		// typically exit <30s after the last turn — without this, the parsed
		// token total (SetTokens) never reaches the registry. Observed live
		// on hydra-code-zuo: worker logged total=616789, registry showed 0.
		//
		// The beat must NOT claim state=completed: the daemon's report
		// endpoint accepts only working/parked/hung and answers 400 for
		// anything else, silently dropping the flush (observed live: every
		// exit-0 row had tokens_so_far=0; exit-1 rows recorded tokens
		// because their final beat still said "working"). Terminal state is
		// the completion marker's job (MarkCompleted), not the liveness
		// report's. Fail-open per reporter contract: beat errors only log.
		r.beat()
		if err := r.client.Complete(r.rep.BeadID, exitCode); err != nil {
			log.Printf("[Registry] completion report failed: %v", err)
		}
		close(r.stop)
		<-r.stopped
	})
}

func truncateLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
