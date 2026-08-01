package main

import (
	"log"
	"os"
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
	return &stateReporter{
		client: client,
		rep: registry.WorkerReport{
			BeadID:  beadID,
			PID:     pid,
			PGID:    pgid,
			Project: project,
			State:   string(registry.StateWorking),
		},
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
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
func (r *stateReporter) SetTokens(n int64) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	r.rep.TokensSoFar = n
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
