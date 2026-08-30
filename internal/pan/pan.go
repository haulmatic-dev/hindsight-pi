// Package pan implements Pan, the shepherd: a pi-agent that watches the
// worker registry and decides the ambiguous tail — workers parked too long,
// hung candidates, and verdict-missing cases past the nudge cap (PRD:
// worker-park-and-resume-20260731, Part 6).
//
// Contract: Pan is FAIL-OPEN and never gates the pipeline. Deterministic
// rules (backoff, nudge cap, lifetime cap, Charon hung-kill) run without it.
// Pan only writes advisory queue commands; workers, the scheduler and Charon
// keep their own hard bounds.
package pan

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// Store is the slice of the registry Pan needs. An interface so tests can
// fake it; *registry.Store satisfies it.
type Store interface {
	ListWorkers(states ...registry.WorkerState) ([]registry.WorkerRecord, error)
	EnqueueCommand(beadID string, kind registry.CommandKind, payload string) error
	PendingCommands(beadID string) ([]registry.Command, error)
}

// BlockedBead surfaces a READY bead that the scheduler cannot start because a
// prerequisite is blocked (retry exhaustion, spawn failure, etc.). It has no
// live worker row, so Pan must judge it as a non-liveness candidate.
// The injected function may return nil; it is called inside Candidates and is
// fail-open (nil guard, swallowed panic).
type BlockedBead struct {
	BeadID string
	Reason string
	Since  time.Time
}

// BlockedBeadsFunc returns the current set of scheduler-level blocked beads.
// Nil means "none" and must never panic.
type BlockedBeadsFunc func() []BlockedBead

// Config tunes Pan. A zero Config is valid and disabled.
type Config struct {
	Enabled  bool
	Interval time.Duration // tick rate; default 60s

	// Attention thresholds (the ambiguous tail; everything else is handled
	// deterministically by worker/scheduler/Charon).
	HungAfter        time.Duration // heartbeat staleness → hung candidate; default 5m
	ParkOverdueGrace time.Duration // wake_at overdue → overdue candidate; default 10m
	NudgeCap         int           // verdict-missing nudges before attention; default 3

	// TokenBudget (mirrors overseer.max_bead_tokens) turns the ceiling into
	// a judgment candidate instead of a hard kill: crossing it means "needs
	// arbitration", and a fresh continue verdict stays Charon's backstop.
	// 0 disables. (2026-08-22: the hard-kill design would have killed ux5
	// and acx — beads that landed — and did kill cc0d6 mid-'continue'.)
	TokenBudget int64

	// TerminalBead reports whether a bead has reached a terminal state
	// (closed, completed, etc.). When true the worker row is silently skipped
	// by Candidates to avoid judging dead beads. Nil means "unknown, include
	// the candidate" (fail open).
	TerminalBead func(beadID string) bool

	// BlockedBeads returns READY beads that are blocked at the scheduler level
	// (retry exhaustion, spawn failure, worktree conflict, etc.). Pan unions
	// these with registry-derived candidates. Nil means "none" and is fail-open.
	BlockedBeads BlockedBeadsFunc

	// Pan's own pi identity. Its provider MUST be configurable separately
	// from the worker fleet's: Pan has to stay lucid during the congestion
	// storms it manages. APIKeyName names the env var holding its key.
	Provider   string
	Model      string
	Thinking   string
	APIKeyName string

	PiPath  string        // default $HYDRA_PI_PATH or "pi"
	Timeout time.Duration // per pi invocation; default 5m
}

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = 60 * time.Second
	}
	if c.HungAfter <= 0 {
		c.HungAfter = 5 * time.Minute
	}
	if c.ParkOverdueGrace <= 0 {
		c.ParkOverdueGrace = 10 * time.Minute
	}
	if c.NudgeCap <= 0 {
		c.NudgeCap = 3
	}
	if c.PiPath == "" {
		c.PiPath = "pi"
	}
	if c.Timeout <= 0 {
		c.Timeout = 5 * time.Minute
	}
	return c
}

// Decision is one instruction Pan wants queued for a worker.
type Decision struct {
	BeadID  string `json:"bead_id"`
	Action  string `json:"action"`  // a registry.CommandKind
	Message string `json:"message"` // payload, e.g. the steer text or abandon reason
}

var allowedActions = map[registry.CommandKind]bool{
	registry.CmdContinue:           true,
	registry.CmdSteer:              true,
	registry.CmdCompactAndContinue: true,
	registry.CmdAbandonWithSummary: true,
	registry.CmdCloseSatisfied:     true,
}

// repairWhitelist is the only set of daemon-side repair actions Pan may propose.
// The prompt lists these names explicitly; the parser rejects anything else so
// the model cannot invent action names and burn MiniMax calls.
var repairWhitelist = map[string]bool{
	"repair-worktree":  true,
	"reap-placeholder": true,
	"reset-retries":    true,
}

// isAllowedAction returns true for known worker command kinds and for
// whitelisted "repair:<name>" daemon-side actions.
func isAllowedAction(action string) bool {
	if allowedActions[registry.CommandKind(action)] {
		return true
	}
	name, ok := parseRepairName(action)
	if !ok {
		return false
	}
	return repairWhitelist[name]
}

// parseRepairName extracts the handler name from "repair:<name>".
func parseRepairName(action string) (string, bool) {
	if !strings.HasPrefix(action, "repair:") {
		return "", false
	}
	return strings.TrimPrefix(action, "repair:"), true
}

// hasLiveWorker reports whether the bead has any non-completed worker row.
// A live row means the bead is not a scheduler-level blocker and must not be
// repaired.
func (s *Shepherd) hasLiveWorker(beadID string) bool {
	if s.store == nil {
		return false
	}
	workers, err := s.store.ListWorkers() // non-completed only
	if err != nil {
		log.Printf("[Pan] repair live-worker check failed for %s: %v", beadID, err)
		return false
	}
	for _, w := range workers {
		if w.BeadID == beadID {
			return true
		}
	}
	return false
}

// canRepair checks the one-repair-per-bead-per-hour rate limit.
func (s *Shepherd) canRepair(beadID string) (bool, time.Duration) {
	s.repairMu.Lock()
	defer s.repairMu.Unlock()
	if s.repairLimits == nil {
		s.repairLimits = make(map[string]time.Time)
	}
	if last, ok := s.repairLimits[beadID]; ok && time.Since(last) < time.Hour {
		return false, time.Hour - time.Since(last)
	}
	return true, 0
}

// recordRepair notes that a repair ran for the bead, capturing the error
// (nil = ok) so the rate-limit detail can report the last outcome.
func (s *Shepherd) recordRepair(beadID string, err error) {
	s.repairMu.Lock()
	defer s.repairMu.Unlock()
	if s.repairLimits == nil {
		s.repairLimits = make(map[string]time.Time)
	}
	if s.repairResults == nil {
		s.repairResults = make(map[string]error)
	}
	s.repairLimits[beadID] = time.Now()
	s.repairResults[beadID] = err
}

// emitRepairEvent emits the audit event for a repair attempt. It is fail-open:
// a failure to emit must not stop the pipeline.
func (s *Shepherd) emitRepairEvent(beadID, action, result, detail string) {
	payload, _ := json.Marshal(map[string]string{
		"bead_id": beadID,
		"action":  action,
		"result":  result,
		"detail":  detail,
	})
	if s.OnRepairEvent != nil {
		s.OnRepairEvent(beadID, action, result, detail)
	} else {
		log.Printf("[PanRepair] %s", payload)
	}
}

// Candidate is a registry record needing a judgment call, with the reason it
// surfaced.
type Candidate struct {
	Record registry.WorkerRecord `json:"record"`
	Reason string                `json:"reason"`
}

// Runner executes pi headless and returns stdout. Injectable for tests.
type Runner func(ctx context.Context, cfg Config, prompt string) (string, error)

// Shepherd is Pan's runtime.
type Shepherd struct {
	cfg   Config
	store Store
	run   Runner
	stop  chan struct{}

	// ZeroDiff reports whether a bead's worktree is empty of changes relative
	// to the working branch. Optional; nil means "unknown, no marker".
	ZeroDiff func(beadID string) (zero bool, known bool)
	// OnCloseSatisfied closes a bead that Pan has decided is already satisfied.
	// Optional; nil means the close_satisfied decision is ignored.
	OnCloseSatisfied func(beadID string) error
	// RepairHandlers maps "<name>" → handler for "repair:<name>" verdicts.
	// Optional; nil means all repair actions are rejected.
	RepairHandlers map[string]func(beadID string) error
	// OnRepairEvent is called after a repair handler runs. If nil, Pan logs
	// the event with log.Printf. Optional; used by tests to capture events.
	OnRepairEvent func(beadID, action, result, detail string)

	verdictsMu sync.Mutex
	verdicts   map[string]time.Time // bead → last life-continuing verdict

	repairMu      sync.Mutex
	repairLimits  map[string]time.Time // bead → last repair run
	repairResults map[string]error     // bead → last repair error (for rate-limit detail)

	rejectedMu      sync.Mutex
	rejectedActions map[string]bool // beadID + "/" + action; unknown repair actions remembered per session
}

// recordVerdict notes a non-terminal decision (continue/steer/compact) so
// Charon's hard backstop can grant a stay of execution.
func (s *Shepherd) recordVerdict(beadID string, kind registry.CommandKind) {
	switch kind {
	case registry.CmdContinue, registry.CmdSteer, registry.CmdCompactAndContinue:
	default:
		return // terminal verdicts (abandon) do not extend life
	}
	s.verdictsMu.Lock()
	if s.verdicts == nil {
		s.verdicts = make(map[string]time.Time)
	}
	s.verdicts[beadID] = time.Now()
	s.verdictsMu.Unlock()
}

// ContinuedRecently reports whether Pan issued a life-continuing verdict for
// the bead within the window.
func (s *Shepherd) ContinuedRecently(beadID string, within time.Duration) bool {
	s.verdictsMu.Lock()
	defer s.verdictsMu.Unlock()
	t, ok := s.verdicts[beadID]
	return ok && time.Since(t) <= within
}

// New builds a Shepherd. run may be nil, in which case the real pi runner is
// used.
func New(cfg Config, store Store, run Runner) *Shepherd {
	cfg = cfg.withDefaults()
	if run == nil {
		run = RunPi
	}
	return &Shepherd{cfg: cfg, store: store, run: run, stop: make(chan struct{})}
}

// SetZeroDiff injects the optional zero-diff detector.
func (s *Shepherd) SetZeroDiff(fn func(beadID string) (bool, bool)) {
	s.ZeroDiff = fn
}

// SetOnCloseSatisfied injects the optional close-satisfied handler.
func (s *Shepherd) SetOnCloseSatisfied(fn func(beadID string) error) {
	s.OnCloseSatisfied = fn
}

// SetRepairHandlers injects the repair handler map. The map is copied so the
// caller can modify their own copy after the call without racing Pan.
func (s *Shepherd) SetRepairHandlers(handlers map[string]func(beadID string) error) {
	s.RepairHandlers = make(map[string]func(beadID string) error, len(handlers))
	for k, v := range handlers {
		s.RepairHandlers[k] = v
	}
}

// SetOnRepairEvent injects the optional repair event hook.
func (s *Shepherd) SetOnRepairEvent(fn func(beadID, action, result, detail string)) {
	s.OnRepairEvent = fn
}

// SetBlockedBeads injects the optional blocked-bead source.
func (s *Shepherd) SetBlockedBeads(fn BlockedBeadsFunc) {
	s.cfg.BlockedBeads = fn
}

// SetTerminalBead injects the optional terminal-bead detector.
func (s *Shepherd) SetTerminalBead(fn func(beadID string) bool) {
	s.cfg.TerminalBead = fn
}

// Start runs Pan's tick loop in the background.
func (s *Shepherd) Start() {
	if !s.cfg.Enabled {
		log.Printf("[Pan] disabled; deterministic rules only")
		return
	}
	log.Printf("[Pan] 🐐 Starting shepherd (interval: %v, provider: %s, model: %s)",
		s.cfg.Interval, s.cfg.Provider, s.cfg.Model)
	go func() {
		ticker := time.NewTicker(s.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.Tick(context.Background())
			case <-s.stop:
				log.Printf("[Pan] Stopped")
				return
			}
		}
	}()
}

// Stop halts the tick loop.
func (s *Shepherd) Stop() { close(s.stop) }

// Tick runs one attention pass. Every failure is logged and swallowed:
// Pan is fail-open by contract.
func (s *Shepherd) Tick(ctx context.Context) {
	if s.store == nil {
		log.Printf("[Pan] no store configured; skipping tick")
		return
	}
	cands, err := s.Candidates()
	if err != nil {
		log.Printf("[Pan] candidate scan failed (skipping tick): %v", err)
		return
	}
	if len(cands) == 0 {
		return
	}
	log.Printf("[Pan] %d candidate(s) need judgment", len(cands))

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	out, err := s.run(ctx, s.cfg, BuildPrompt(cands))
	if err != nil {
		log.Printf("[Pan] pi invocation failed (deterministic rules in force): %v", err)
		return
	}

	decisions, err := ParseDecisions(out)
	if err != nil {
		log.Printf("[Pan] could not parse pi decisions (skipping): %v", err)
		return
	}

	enqueued := 0
	repaired := 0
	for _, d := range decisions {
		if !isAllowedAction(d.Action) {
			if strings.HasPrefix(d.Action, "repair:") {
				key := d.BeadID + "/" + d.Action
				s.rejectedMu.Lock()
				if s.rejectedActions == nil {
					s.rejectedActions = make(map[string]bool)
				}
				already := s.rejectedActions[key]
				if !already {
					s.rejectedActions[key] = true
				}
				s.rejectedMu.Unlock()
				if !already {
					log.Printf("[Pan] unknown repair action %s rejected for %s", d.Action, d.BeadID)
				}
			} else {
				log.Printf("[Pan] rejecting unknown action %q for %s", d.Action, d.BeadID)
			}
			continue
		}
		if d.BeadID == "" {
			log.Printf("[Pan] rejecting decision with empty bead_id")
			continue
		}
		// Daemon-side repair verdict: execute whitelisted handler, do not
		// enqueue a worker command.
		if name, ok := parseRepairName(d.Action); ok {
			s.handleRepair(d.BeadID, name)
			repaired++
			continue
		}

		kind := registry.CommandKind(d.Action)
		// close_satisfied is a daemon-side action, not a worker command.
		if kind == registry.CmdCloseSatisfied {
			if s.OnCloseSatisfied == nil {
				log.Printf("[Pan] close_satisfied for %s has no handler configured; ignoring", d.BeadID)
				continue
			}
			if err := s.OnCloseSatisfied(d.BeadID); err != nil {
				log.Printf("[Pan] close_satisfied for %s failed: %v", d.BeadID, err)
				continue
			}
			log.Printf("[Pan] closed-satisfied %s", d.BeadID)
			continue
		}
		// Dedup against the pending queue: a dead worker never consumes its
		// commands, so without this Pan re-queues the same verdict every tick
		// (observed live 2026-08-06: 511 duplicate abandon-with-summary
		// commands on srv-smart-engine-g1m). Fail-open: a pending-list error
		// still enqueues — duplicates are cheaper than a lost decision.
		if pending, perr := s.store.PendingCommands(d.BeadID); perr == nil {
			dup := false
			for _, pc := range pending {
				if pc.Kind == kind {
					log.Printf("[Pan] skipping duplicate %s for %s (already pending)", kind, d.BeadID)
					dup = true
					break
				}
			}
			if dup {
				continue
			}
		}
		if err := s.store.EnqueueCommand(d.BeadID, kind, d.Message); err != nil {
			log.Printf("[Pan] enqueue %s for %s failed: %v", kind, d.BeadID, err)
			continue
		}
		s.recordVerdict(d.BeadID, kind)
		enqueued++
		log.Printf("[Pan] queued %s for %s: %s", kind, d.BeadID, truncate(d.Message, 120))
	}
	if enqueued > 0 {
		log.Printf("[Pan] queued %d decision(s)", enqueued)
	}
	if repaired > 0 {
		log.Printf("[Pan] ran %d repair(s)", repaired)
	}
}

// handleRepair executes a whitelisted repair handler for a blocked bead.
// It is fail-open: errors are logged and emitted as audit events only.
func (s *Shepherd) handleRepair(beadID, name string) {
	// Safety gate: never repair a bead that still has a live worker row.
	if s.hasLiveWorker(beadID) {
		s.emitRepairEvent(beadID, name, "error", "refused: bead has a live worker row")
		log.Printf("[Pan] repair %s for %s refused: live worker row exists", name, beadID)
		return
	}
	// Rate limit: one repair per bead per hour.
	if ok, remaining := s.canRepair(beadID); !ok {
		detail := fmt.Sprintf("rate limited: %v until next repair", remaining)
		s.emitRepairEvent(beadID, name, "error", detail)
		log.Printf("[Pan] repair %s for %s rate limited (%v remaining)", name, beadID, remaining)
		return
	}
	handler, ok := s.RepairHandlers[name]
	if !ok || handler == nil {
		s.emitRepairEvent(beadID, name, "error", "unknown repair handler")
		log.Printf("[Pan] repair %s for %s rejected: no handler configured", name, beadID)
		return
	}

	if err := handler(beadID); err != nil {
		s.recordRepair(beadID, err)
		s.emitRepairEvent(beadID, name, "error", err.Error())
		log.Printf("[Pan] repair %s for %s failed: %v", name, beadID, err)
		return
	}
	s.recordRepair(beadID, nil)
	s.emitRepairEvent(beadID, name, "ok", "")
	log.Printf("[Pan] repair %s for %s succeeded", name, beadID)
}

// Candidates returns the ambiguous tail: parked-overdue, hung,
// nudge-cap-reached workers, and scheduler-level blocked beads with no live
// worker row. Everything else belongs to deterministic rules.
func (s *Shepherd) Candidates() ([]Candidate, error) {
	workers, err := s.store.ListWorkers(registry.StateWorking, registry.StateParked)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}

	now := time.Now()
	var out []Candidate
	live := make(map[string]bool)
	for _, w := range workers {
		// Any live worker row suppresses the bead's blocked candidacy below —
		// a stale spawn-failure/retry record must not speak over a running
		// worker (Pan abandoned 2-minute-old v4bpl over a 1.5h-stale record,
		// 2026-08-30). Set unconditionally, not only when the worker itself
		// has a candidacy reason.
		live[w.BeadID] = true
		var reason string
		switch {
		case w.NudgeCount >= s.cfg.NudgeCap:
			reason = fmt.Sprintf(
				"verdict missing after %d nudges (cap %d)", w.NudgeCount, s.cfg.NudgeCap)
		case w.State == registry.StateParked && !w.ParkWakeAt.IsZero() &&
			now.After(w.ParkWakeAt.Add(s.cfg.ParkOverdueGrace)):
			reason = fmt.Sprintf(
				"parked past wake_at + %v grace (reason: %s)", s.cfg.ParkOverdueGrace, w.ParkReason)
		case w.State == registry.StateWorking && s.cfg.TokenBudget > 0 && w.TokensSoFar > s.cfg.TokenBudget:
			reason = fmt.Sprintf(
				"token budget exceeded (%d > %d) — converging or wandering?", w.TokensSoFar, s.cfg.TokenBudget)
			if s.ZeroDiff != nil {
				if zero, _ := s.ZeroDiff(w.BeadID); zero {
					reason += "; zero diff on target files — wandering or already-satisfied?"
				}
			}
		case w.State == registry.StateWorking && !w.LastHeartbeat.IsZero() &&
			now.Sub(w.LastHeartbeat) > s.cfg.HungAfter &&
			now.Sub(w.StartedAt) >= 2*s.cfg.HungAfter:
			// Young-worker grace: a worker younger than 2×HungAfter is mid-first-
			// turn — tokens legitimately 0 (they land at turn end) and its
			// heartbeat can predate it in the registration race. Pan abandoned
			// exactly such a worker live (srv-smart-engine-pes, 2026-08-07).
			reason = fmt.Sprintf(
				"no heartbeat for %v (threshold %v)", now.Sub(w.LastHeartbeat), s.cfg.HungAfter)
		}
		if reason == "" {
			continue
		}
		if s.cfg.TerminalBead != nil && s.cfg.TerminalBead(w.BeadID) {
			continue
		}
		out = append(out, Candidate{Record: w, Reason: reason})
	}

	if s.cfg.BlockedBeads != nil {
		for _, b := range s.cfg.BlockedBeads() {
			if live[b.BeadID] {
				continue
			}
			if s.cfg.TerminalBead != nil && s.cfg.TerminalBead(b.BeadID) {
				continue
			}
			reason := b.Reason
			if !strings.HasPrefix(reason, "BLOCKED: ") {
				reason = "BLOCKED: " + reason
			}
			if !b.Since.IsZero() {
				reason += fmt.Sprintf(" (since %s)", time.Since(b.Since).Round(time.Second))
			}
			out = append(out, Candidate{
				Record: registry.WorkerRecord{BeadID: b.BeadID},
				Reason: reason,
			})
		}
	}

	// Belt-and-braces: retry-manager and spawn-failure residue can survive a
	// verified close, and a stale TerminalBead cache or unresolved project path
	// can let a terminal bead leak into the judgment prompt. Re-check terminal
	// status on every collected candidate before returning.
	if s.cfg.TerminalBead != nil {
		filtered := out[:0]
		for _, cand := range out {
			if s.cfg.TerminalBead(cand.Record.BeadID) {
				continue
			}
			filtered = append(filtered, cand)
		}
		out = filtered
	}

	return out, nil
}

// BuildPrompt renders candidates into a judgment prompt for pi.
func BuildPrompt(cands []Candidate) string {
	payload, _ := json.MarshalIndent(cands, "", "  ")
	return `You are Pan, the shepherd of a hydra agent fleet. Each worker below
needs a judgment call. For EVERY worker decide exactly one action:

- "continue": tell the worker to resume its session now (payload: why it is safe)
- "steer": inject guidance into the live session (payload: the steering message)
- "compact-and-continue": session context is exhausted; compact then continue
- "abandon-with-summary": give up gracefully (payload: what was done and what remains)
- "close_satisfied": the bead's acceptance criteria are already met on the working branch and the worker found nothing to do; close it without a worker
- "repair:<name>": execute a whitelisted daemon-side repair (payload: ignored). Only for BLOCKED beads with no live worker row. The only valid repair names are: repair-worktree, reap-placeholder, reset-retries. No other repair:<name> strings exist; if none fit, choose continue or abandon-with-summary.

Rules:
- Parked on provider-capacity errors: prefer "continue" unless the park reason
  is credential-class (401, 402, quota), where waiting cannot help — then
  "abandon-with-summary".
- Hung workers: prefer "abandon-with-summary" unless the record shows recent
  token progress, then "steer".
- Verdict missing after nudge cap: "steer" with a message demanding the
  HYDRA_VERDICT block, or "abandon-with-summary" if tokens show no progress.
- Repairs: only issue "repair:<name>" for BLOCKED beads that have no live healthy
  worker. Each bead can receive at most one repair of each name per hour.
- Never invent bead IDs. One decision per candidate.

Reply with ONLY a JSON array, no prose:
[{"bead_id": "...", "action": "...", "message": "..."}]

Candidates:
` + string(payload)
}

// ParseDecisions extracts the JSON decision array from pi's output,
// tolerating prose around it and code fences.
func ParseDecisions(out string) ([]Decision, error) {
	start := -1
	depth := 0
	end := -1
	for i := 0; i < len(out); i++ {
		switch out[i] {
		case '[':
			if depth == 0 && start < 0 {
				start = i
			}
			depth++
		case ']':
			depth--
			if depth == 0 && start >= 0 {
				end = i + 1
			}
		}
	}
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("no JSON array in output (%d bytes)", len(out))
	}
	var decisions []Decision
	if err := json.Unmarshal([]byte(out[start:end]), &decisions); err != nil {
		return nil, fmt.Errorf("parse decisions: %w", err)
	}
	return decisions, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
