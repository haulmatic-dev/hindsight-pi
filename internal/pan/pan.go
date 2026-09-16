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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// ErrSatisfiedAbstain signals that a close_satisfied handler chose not to
// close because the bead's full acceptance evidence is missing or incomplete.
// Pan should treat this as a continue verdict instead of a failure.
var ErrSatisfiedAbstain = errors.New("satisfied gate abstains: bead requires full acceptance evidence, not just a green test suite")

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
	// AbandonStaleThreshold is how long an unconsumed abandon-with-summary
	// command can sit pending before Pan executes it daemon-side. Defaults
	// to 2x Interval.
	AbandonStaleThreshold time.Duration

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

	// DigestFunc returns the daemon-collected fleet health digest: Pan's eyes.
	// Called every tick (cheap). Projects carrying anomaly flags become
	// "<key>-campaign-anomaly" pseudo-candidates, so fleet-level trouble wakes
	// Pan even when no bead-level candidate class fires — the 2026-09-13→16
	// silence, where three days of campaign anomalies produced zero candidates
	// because every provider class is failure-residue only. Nil disables.
	DigestFunc func() HealthDigest

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
	if c.AbandonStaleThreshold <= 0 {
		c.AbandonStaleThreshold = 2 * c.Interval
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
	"repair-worktree":             true,
	"reap-placeholder":            true,
	"reset-retries":               true,
	"repair-bead-store":           true,
	"clear-hard-park":             true,
	"prune-worktree-registration": true,
	"re-push-integration":         true,
	"park-campaign":               true,
	"log-improvement":             true,
}

// pseudoBeadSuffix marks synthetic project-level candidates (e.g.
// "<project>-push-pending") that are not real beads: close/abandon/worker
// commands can never succeed for them — only repair:<name> applies.
const pseudoBeadSuffix = "-push-pending"

// reJudgeInterval is the valve that re-judges an unchanged candidate set
// anyway: a permanently stuck candidate must never become permanently blind,
// and verdict memory (ContinuedRecently etc.) decays on this horizon.
var reJudgeInterval = 15 * time.Minute

// projectScopedRepairs rate-limit against the project, not the bead: one
// shared-wedge repair covers every bead parked by the same cause, so a
// per-bead limit would take hours to clear a fleet-wide wedge (2026-09-13:
// one stale webapp-xw3 registration wedged 13 beads).
var projectScopedRepairs = map[string]bool{
	"prune-worktree-registration": true,
	"re-push-integration":         true,
	"park-campaign":               true,
	"log-improvement":             true,
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
// repaired or abandoned daemon-side.
func (s *Shepherd) hasLiveWorker(beadID string) bool {
	return len(s.liveWorkerRows(beadID)) > 0
}

// liveWorkerRows returns the bead's non-completed worker rows. A store error
// is logged and reported as no rows (fail open, matching prior behavior).
func (s *Shepherd) liveWorkerRows(beadID string) []registry.WorkerRecord {
	if s.store == nil {
		return nil
	}
	workers, err := s.store.ListWorkers() // non-completed only
	if err != nil {
		log.Printf("[Pan] live-worker check failed for %s: %v", beadID, err)
		return nil
	}
	var out []registry.WorkerRecord
	for _, w := range workers {
		if w.BeadID == beadID {
			out = append(out, w)
		}
	}
	return out
}

// maxConsecutiveRepairs caps how often the same repair may be attempted for
// the same (action, scope, normalized reason) before Pan classifies the
// condition human-only and stops burning attempts (66 prunes on one wedge
// in a day, 2026-09-15). A changed reason is a new key — the natural reset.
const maxConsecutiveRepairs = 3

var (
	volatileDurationRe = regexp.MustCompile(`\d+(?:\.\d+)?(?:d|h|m|s)(?:\d+m?)?\b`)
	volatileDateTimeRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?)?`)
	volatileUnitsRe    = regexp.MustCompile(`\d+\s+(?:seconds?|minutes?|hours?|days?|weeks?)\b`)
)

// normalizeReason strips volatile substrings — ages ("94h27m"), timestamps,
// "N minutes" phrasings — from a candidate reason. Used ONLY for signatures
// and repair-efficacy keys, never for the judgment prompt: the model still
// sees the raw reason with its fresh age.
func normalizeReason(reason string) string {
	r := volatileDateTimeRe.ReplaceAllString(reason, "#")
	r = volatileDurationRe.ReplaceAllString(r, "#")
	r = volatileUnitsRe.ReplaceAllString(r, "# units")
	return r
}

// candidateSignature hashes the stable identity of a candidate set: IDs,
// states, and reasons — everything that could change the verdict. Volatile
// fields (token counts, heartbeats, ages) are excluded on purpose: they
// change every tick and would defeat the dedup.
func candidateSignature(cands []Candidate) string {
	parts := make([]string, 0, len(cands))
	for _, c := range cands {
		parts = append(parts, c.Record.BeadID+"\x00"+string(c.Record.State)+"\x00"+normalizeReason(c.Reason))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return hex.EncodeToString(sum[:8])
}

// judgmentUnchanged reports whether this candidate set is byte-identical
// (by signature) to the last judged one AND the re-judge valve has not
// expired. On skip it logs once; on change (or valve expiry) it records the
// new signature and lets the tick proceed to judgment.
func (s *Shepherd) judgmentUnchanged(cands []Candidate) bool {
	sig := candidateSignature(cands)
	s.dedupMu.Lock()
	defer s.dedupMu.Unlock()
	if sig == s.lastSig && time.Since(s.lastJudgedAt) < reJudgeInterval {
		return true
	}
	s.lastSig = sig
	s.lastJudgedAt = time.Now()
	return false
}

// shouldPresentPseudo rate-limits pseudo-bead candidacy: an unchanged
// reason is re-presented at most once per 30 minutes, so a human-only
// condition (diverged push, protection) does not consume a judgment call
// every tick. A changed reason (new failure mode) is presented immediately.
func (s *Shepherd) shouldPresentPseudo(beadID, reason string) bool {
	s.pseudoMu.Lock()
	defer s.pseudoMu.Unlock()
	if s.pseudoSeen == nil {
		s.pseudoSeen = make(map[string]pseudoSeenEntry)
	}
	if e, ok := s.pseudoSeen[beadID]; ok && e.reason == reason && time.Since(e.at) < 30*time.Minute {
		return false
	}
	s.pseudoSeen[beadID] = pseudoSeenEntry{reason: reason, at: time.Now()}
	return true
}

type pseudoSeenEntry struct {
	reason string
	at     time.Time
}

// liveAtLastScan reports whether the bead had a live worker row when
// Candidates() last ran. Empty (no scan yet) means false.
func (s *Shepherd) liveAtLastScan(beadID string) bool {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	return s.scanLive[beadID]
}

// describeHolders renders worker rows as "bead:state(pid=N)" for audit logs —
// the "which slot, which holder" half of the abandon rationale.
func describeHolders(workers []registry.WorkerRecord) string {
	parts := make([]string, 0, len(workers))
	for _, w := range workers {
		parts = append(parts, fmt.Sprintf("%s:%s(pid=%d)", w.BeadID, w.State, w.PID))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// canRepair checks the repair rate limit. Per-bead repairs are limited to
// one per bead per hour; project-scoped repairs (projectScopedRepairs) are
// limited per project with a shorter window — one shared-wedge repair must be
// able to run once per project without waiting an hour per parked bead
// (2026-09-13: one stale webapp-xw3 registration wedged 13 beads).
func (s *Shepherd) canRepair(beadID, name string) (bool, time.Duration) {
	key := beadID + "|" + name
	window := time.Hour
	if projectScopedRepairs[name] {
		if s.ProjectScope != nil {
			key = s.ProjectScope(beadID) + "|" + name
		}
		window = 30 * time.Minute
	}
	s.repairMu.Lock()
	defer s.repairMu.Unlock()
	if s.repairLimits == nil {
		s.repairLimits = make(map[string]time.Time)
	}
	if last, ok := s.repairLimits[key]; ok && time.Since(last) < window {
		return false, window - time.Since(last)
	}
	return true, 0
}

// recordRepair notes that a repair ran for the bead, capturing the error
// (nil = ok) so the rate-limit detail can report the last outcome.
func (s *Shepherd) recordRepair(beadID, name string, err error) {
	key := beadID + "|" + name
	if projectScopedRepairs[name] && s.ProjectScope != nil {
		key = s.ProjectScope(beadID) + "|" + name
	}
	s.repairMu.Lock()
	defer s.repairMu.Unlock()
	if s.repairLimits == nil {
		s.repairLimits = make(map[string]time.Time)
	}
	if s.repairResults == nil {
		s.repairResults = make(map[string]error)
	}
	s.repairLimits[key] = time.Now()
	s.repairResults[key] = err
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
	// OnAbandonWithSummary closes a workerless bead that Pan has decided to
	// abandon. Optional; nil means the command is enqueued as before.
	OnAbandonWithSummary func(beadID string) error
	// RepairHandlers maps "<name>" → handler for "repair:<name>" verdicts.
	// Optional; nil means all repair actions are rejected.
	RepairHandlers map[string]func(beadID string) error
	// LogImprovement persists repair:log-improvement verdicts as improvement
	// proposals for human review (beadID, decision reason). Optional.
	LogImprovement func(beadID, text string) error
	// OnRepairEvent is called after a repair handler runs. If nil, Pan logs
	// the event with log.Printf. Optional; used by tests to capture events.
	OnRepairEvent func(beadID, action, result, detail string)
	// ProjectScope maps a bead (or pseudo-bead like "<project>-push-pending")
	// to its project path. Used to rate-limit project-scoped repairs per
	// project instead of per bead. Optional; nil means per-bead keying.
	ProjectScope func(beadID string) string

	verdictsMu sync.Mutex
	verdicts   map[string]time.Time // bead → last life-continuing verdict

	repairMu      sync.Mutex
	repairLimits  map[string]time.Time // bead → last repair run
	repairResults map[string]error     // bead → last repair error (for rate-limit detail)

	// scanLive records which beads held a live (working/parked) worker row at
	// the most recent Candidates() scan. Tick uses it to tell worker-origin
	// decisions from blocked-origin ones at decision-execution time (br-4y5,
	// 2026-09-11: a blocked-bead abandon executed after the bead's own worker
	// row registered mid-judgment — a near-miss silent kill of healthy work).
	scanMu   sync.Mutex
	scanLive map[string]bool

	rejectedMu      sync.Mutex
	rejectedActions map[string]bool // beadID + "/" + action; unknown repair actions remembered per session

	pseudoMu   sync.Mutex
	pseudoSeen map[string]pseudoSeenEntry // pseudo-bead → last presented reason

	dedupMu      sync.Mutex
	lastSig      string    // candidate-set signature of the last judgment
	lastJudgedAt time.Time // when the last judgment ran (valve backstop)

	efficacyMu     sync.Mutex
	repairEfficacy map[string]int // repair key → consecutive attempts
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

// SetOnAbandonWithSummary injects the optional daemon-side abandon handler.
func (s *Shepherd) SetOnAbandonWithSummary(fn func(beadID string) error) {
	s.OnAbandonWithSummary = fn
}

// SetLogImprovement injects the improvement sink for repair:log-improvement
// verdicts. Nil sink → the verdict is rejected with an audit event.
func (s *Shepherd) SetLogImprovement(fn func(beadID, text string) error) {
	s.LogImprovement = fn
}

// SetRepairHandlers injects the repair handler map. The map is copied so the
// caller can modify their own copy after the call without racing Pan.
func (s *Shepherd) SetRepairHandlers(handlers map[string]func(beadID string) error) {
	s.RepairHandlers = make(map[string]func(beadID string) error, len(handlers))
	for k, v := range handlers {
		s.RepairHandlers[k] = v
	}
}

// SetProjectScope installs the bead→project-path resolver used to rate-limit
// project-scoped repairs per project.
func (s *Shepherd) SetProjectScope(fn func(beadID string) string) {
	s.ProjectScope = fn
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
	if s.judgmentUnchanged(cands) {
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
			s.handleRepair(d.BeadID, name, d.Message)
			repaired++
			continue
		}

		// Pseudo-beads are project-level signals, not beads: abandoning or
		// closing them can never succeed, and a worker command has no worker
		// to consume it (observed 2026-09-14: Pan abandoned
		// webapp-push-pending every tick; each abandon failed and the
		// judgment repeated forever). Repairs above already passed through.
		if IsPseudoBeadID(d.BeadID) {
			log.Printf("[Pan] ignoring %s for pseudo-bead %s (project-level signal; repair-only)", d.Action, d.BeadID)
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
				if errors.Is(err, ErrSatisfiedAbstain) {
					log.Printf("[Pan] satisfied gate for %s abstains (continuing): %v", d.BeadID, err)
					s.recordVerdict(d.BeadID, registry.CmdContinue)
					if err := s.store.EnqueueCommand(d.BeadID, registry.CmdContinue, ""); err != nil {
						log.Printf("[Pan] continue enqueue for %s failed: %v", d.BeadID, err)
					}
					continue
				}
				log.Printf("[Pan] close_satisfied for %s failed: %v", d.BeadID, err)
				continue
			}
			log.Printf("[Pan] closed-satisfied %s", d.BeadID)
			continue
		}
		// abandon-with-summary is terminal. Audit every abandon decision with
		// its rationale (reason, live rows, slot holders) and refuse it when
		// the target holds a live worker row it did not have at candidate-scan
		// time — the blocked evidence is then stale and executing it kills
		// healthy work (br-4y5, 2026-09-11: abandon queued citing "worker limit
		// 1/1 already saturated" while br-4y5's own worker held that slot).
		if kind == registry.CmdAbandonWithSummary {
			rows := s.liveWorkerRows(d.BeadID)
			all, lerr := s.store.ListWorkers()
			if lerr != nil {
				log.Printf("[Pan] abandon audit slot-holder check for %s failed (fail open): %v", d.BeadID, lerr)
				all = nil
			}
			origin := "blocked"
			if s.liveAtLastScan(d.BeadID) {
				origin = "worker"
			}
			log.Printf("[Pan] abandon-with-summary audit: bead=%s origin=%s reason=%q live_rows=%d holders=%s",
				d.BeadID, origin, truncate(d.Message, 120), len(rows), describeHolders(all))
			if origin == "blocked" && len(rows) > 0 {
				log.Printf("[Pan] abandon-with-summary for %s REFUSED: blocked-bead decision but the bead now holds a live worker row %s — the bead itself occupies a slot; blocked evidence is stale",
					d.BeadID, describeHolders(rows))
				continue
			}
			executed := false
			if pending, perr := s.store.PendingCommands(d.BeadID); perr == nil {
				for _, pc := range pending {
					if pc.Kind == kind && !pc.CreatedAt.IsZero() && time.Since(pc.CreatedAt) > s.cfg.AbandonStaleThreshold {
						if len(rows) > 0 {
							// Daemon-side abandon with a live row is a silent kill
							// of whatever the worker is still producing.
							log.Printf("[Pan] stale pending abandon for %s NOT executed daemon-side: live worker row %s still holds the slot",
								d.BeadID, describeHolders(rows))
						} else if s.OnAbandonWithSummary != nil {
							if err := s.OnAbandonWithSummary(d.BeadID); err != nil {
								log.Printf("[Pan] daemon-side abandon for %s failed: %v", d.BeadID, err)
							} else {
								log.Printf("[Pan] abandoned %s daemon-side (stale pending command)", d.BeadID)
							}
						} else {
							log.Printf("[Pan] abandon-with-summary for %s has stale pending command but no daemon-side handler; ignoring", d.BeadID)
						}
						executed = true
						break
					}
				}
			}
			if !executed && len(rows) == 0 {
				if s.OnAbandonWithSummary != nil {
					if err := s.OnAbandonWithSummary(d.BeadID); err != nil {
						log.Printf("[Pan] daemon-side abandon for %s failed: %v", d.BeadID, err)
					} else {
						log.Printf("[Pan] abandoned %s daemon-side (no live worker)", d.BeadID)
					}
				} else {
					log.Printf("[Pan] abandon-with-summary for %s has no live worker and no daemon-side handler; ignoring", d.BeadID)
				}
				continue
			}
			if executed {
				continue
			}
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
// reason carries the decision's justification; log-improvement persists it
// as the improvement text, other handlers ignore it.
func (s *Shepherd) handleRepair(beadID, name, reason string) {
	// log-improvement is a pure observation sink: no live-worker refusal
	// (the whole point is reporting on live situations) and no handler map —
	// the shepherd writes the improvement line itself.
	if name == "log-improvement" {
		s.handleLogImprovement(beadID, reason)
		return
	}
	// Safety gate: never repair a bead that still has a live worker row.
	if s.hasLiveWorker(beadID) {
		s.emitRepairEvent(beadID, name, "error", "refused: bead has a live worker row")
		log.Printf("[Pan] repair %s for %s refused: live worker row exists", name, beadID)
		return
	}
	// Rate limit: per-bead hourly; project-scoped repairs share a project key.
	if ok, remaining := s.canRepair(beadID, name); !ok {
		detail := fmt.Sprintf("rate limited: %v until next repair", remaining)
		s.emitRepairEvent(beadID, name, "error", detail)
		log.Printf("[Pan] repair %s for %s rate limited (%v remaining)", name, beadID, remaining)
		return
	}
	// Efficacy gate: refuse after maxConsecutiveRepairs attempts on the same
	// (action, scope, normalized reason). The wedge that spams repairs never
	// gets fixed by the 4th identical attempt either — classify human-only.
	if key := s.repairEfficacyKey(beadID, name); !s.noteRepairAttempt(key) {
		detail := "ineffective: same repair attempted " + itoa(maxConsecutiveRepairs) + "+ times without the condition changing; human-only"
		s.emitRepairEvent(beadID, name, "refused", detail)
		log.Printf("[Pan] repair %s for %s refused (ineffective — condition unchanged across %d attempts)", name, beadID, maxConsecutiveRepairs)
		return
	}

	handler, ok := s.RepairHandlers[name]
	if !ok || handler == nil {
		s.emitRepairEvent(beadID, name, "error", "unknown repair handler")
		log.Printf("[Pan] repair %s for %s rejected: no handler configured", name, beadID)
		return
	}

	if err := handler(beadID); err != nil {
		s.recordRepair(beadID, name, err)
		s.emitRepairEvent(beadID, name, "error", err.Error())
		log.Printf("[Pan] repair %s for %s failed: %v", name, beadID, err)
		return
	}
	s.recordRepair(beadID, name, nil)
	s.emitRepairEvent(beadID, name, "ok", "")
	log.Printf("[Pan] repair %s for %s succeeded", name, beadID)
}

// handleLogImprovement persists a Pan observation as an improvement
// proposal. Proposals, never self-modification: a human reviews the log and
// decides what becomes a bead. Rate-limited like any repair so a repeating
// observation cannot spam the log.
func (s *Shepherd) handleLogImprovement(beadID, text string) {
	if ok, remaining := s.canRepair(beadID, "log-improvement"); !ok {
		log.Printf("[Pan] log-improvement for %s rate limited (%v remaining)", beadID, remaining)
		return
	}
	if s.LogImprovement == nil {
		s.emitRepairEvent(beadID, "log-improvement", "error", "no improvement sink configured")
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		s.emitRepairEvent(beadID, "log-improvement", "error", "empty improvement text")
		return
	}
	if err := s.LogImprovement(beadID, text); err != nil {
		s.recordRepair(beadID, "log-improvement", err)
		s.emitRepairEvent(beadID, "log-improvement", "error", err.Error())
		log.Printf("[Pan] log-improvement for %s failed: %v", beadID, err)
		return
	}
	s.recordRepair(beadID, "log-improvement", nil)
	s.emitRepairEvent(beadID, "log-improvement", "ok", "")
	log.Printf("[Pan] improvement logged for %s", beadID)
}

// repairEfficacyKey builds the (action, scope, normalized reason) identity
// for efficacy counting. Scope is the project for project-scoped repairs
// (a shared wedge counts collectively), the bead otherwise. Reason is the
// candidate's current blocked reason, normalized; lookup failure yields ""
// and simply counts under an empty reason.
func (s *Shepherd) repairEfficacyKey(beadID, name string) string {
	scope := beadID
	if projectScopedRepairs[name] && s.ProjectScope != nil {
		scope = s.ProjectScope(beadID)
	}
	return name + "|" + scope + "|" + normalizeReason(s.blockedReasonFor(beadID))
}

// blockedReasonFor finds the bead's current blocked reason via the
// BlockedBeads provider. Fail-open: nil provider or missing bead → "".
func (s *Shepherd) blockedReasonFor(beadID string) string {
	if s.cfg.BlockedBeads == nil {
		return ""
	}
	for _, b := range s.cfg.BlockedBeads() {
		if b.BeadID == beadID {
			return b.Reason
		}
	}
	return ""
}

// noteRepairAttempt records an attempt under the key and reports whether
// the attempt is allowed (below the consecutive-attempt cap).
func (s *Shepherd) noteRepairAttempt(key string) bool {
	s.efficacyMu.Lock()
	defer s.efficacyMu.Unlock()
	if s.repairEfficacy == nil {
		s.repairEfficacy = make(map[string]int)
	}
	if s.repairEfficacy[key] >= maxConsecutiveRepairs {
		return false
	}
	s.repairEfficacy[key]++
	return true
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
	defer func() {
		s.scanMu.Lock()
		s.scanLive = live
		s.scanMu.Unlock()
	}()
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
		case w.State == registry.StateWorking && s.cfg.TokenBudget > 0 && w.TokensFresh > s.cfg.TokenBudget:
			reason = fmt.Sprintf(
				"token budget exceeded (fresh=%d > %d, total=%d) — converging or wandering?", w.TokensFresh, s.cfg.TokenBudget, w.TokensSoFar)
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
			if IsPseudoBeadID(b.BeadID) && !s.shouldPresentPseudo(b.BeadID, reason) {
				continue
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

	// Fleet health digest: Pan's eyes. Projects carrying anomaly flags become
	// campaign-anomaly pseudo-candidates even when no bead-level class fires —
	// the fix for the 2026-09-13→16 silence, where gate execution, close
	// loops, and stranded worktrees produced zero candidates for three days
	// because every other provider class is failure-residue only.
	if s.cfg.DigestFunc != nil {
		digest := s.cfg.DigestFunc()
		text := digest.Text()
		for _, ph := range digest.Anomalous() {
			id := CampaignAnomalyID(ph.Key)
			reason := "CAMPAIGN-ANOMALY: " + strings.Join(ph.Anomalies, "; ") + " | fleet digest: " + text
			if !s.shouldPresentPseudo(id, reason) {
				continue
			}
			out = append(out, Candidate{
				Record: registry.WorkerRecord{BeadID: id},
				Reason: reason,
			})
		}
	}

	// Wedge collapse: when >=3 BLOCKED candidates share a normalized reason,
	// they are one shared-wedge fault, not N per-bead faults. Collapse them
	// into a single candidate so Pan issues ONE project-scoped repair instead
	// of N rate-limited per-bead ones (2026-09-13: one stale worktree
	// registration wedged 13 webapp beads; per-bead clearing would take 13h).
	out = collapseWedges(out)

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

// collapseWedges replaces >=3 BLOCKED candidates sharing a normalized reason
// with one synthetic wedge candidate. The first candidate's bead ID is kept
// as the repair target — project-scoped repairs resolve the project from any
// bead of the wedge. Non-BLOCKED candidates pass through untouched.
func collapseWedges(cands []Candidate) []Candidate {
	const wedgeThreshold = 3
	type group struct {
		first int
		count int
	}
	groups := make(map[string]*group)
	for i, c := range cands {
		key, ok := wedgeKey(c.Reason)
		if !ok {
			continue
		}
		if g, ok := groups[key]; ok {
			g.count++
		} else {
			groups[key] = &group{first: i, count: 1}
		}
	}
	wedged := make(map[string]bool)
	for key, g := range groups {
		if g.count >= wedgeThreshold {
			wedged[key] = true
		}
	}
	if len(wedged) == 0 {
		return cands
	}
	out := make([]Candidate, 0, len(cands))
	emitted := make(map[string]bool)
	for _, c := range cands {
		key, ok := wedgeKey(c.Reason)
		if !ok || !wedged[key] {
			out = append(out, c)
			continue
		}
		if emitted[key] {
			continue
		}
		emitted[key] = true
		out = append(out, Candidate{
			Record: c.Record,
			Reason: fmt.Sprintf("BLOCKED: PROJECT-WEDGE: %d beads blocked by %q — prefer ONE project-scoped repair (e.g. repair:prune-worktree-registration) over per-bead repairs", groups[key].count, key),
		})
	}
	return out
}

// wedgeKey normalizes a BLOCKED reason into a grouping key: the phrase
// between "BLOCKED: " and the first colon. Returns ok=false for non-BLOCKED
// candidates.
func wedgeKey(reason string) (string, bool) {
	if !strings.HasPrefix(reason, "BLOCKED: ") {
		return "", false
	}
	rest := strings.TrimPrefix(reason, "BLOCKED: ")
	if idx := strings.Index(rest, ":"); idx > 0 {
		rest = rest[:idx]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
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
- "repair:<name>": execute a whitelisted daemon-side repair (payload: for log-improvement, the improvement text; ignored otherwise). Only for BLOCKED beads with no live worker row. The only valid repair names are: repair-worktree, reap-placeholder, reset-retries, repair-bead-store, clear-hard-park, prune-worktree-registration, re-push-integration, park-campaign, log-improvement. No other repair:<name> strings exist; if none fit, choose continue or abandon-with-summary.

Rescue recipes (hard-won; prefer the repair matching the cause):
- Hard-park labels (worktree-unavailable, integration-exhausted) are durable by design — the scheduler never retries them. Use repair:clear-hard-park when the named cause is plausibly fixed or transient (worktree directory exists, store parses clean, push path repaired). Do not clear a park whose cause is still present.
- Many BLOCKED beads with the same reason = a shared wedge. Prefer one repair:prune-worktree-registration (project-scoped) over per-bead repairs.
- repair:reset-retries clears the retry counter after the underlying cause is fixed; useless while the cause persists.
- repair:repair-bead-store restores a corrupted issues.jsonl from the newest clean backup; pick it when the reason mentions conflict markers or parse errors.
- PUSH-PENDING candidates mean commits exist locally but not on origin. Prefer repair:re-push-integration once; if the reason is branch protection or permissions and a retry already failed, that is a human problem — say so in reasoning instead of looping repairs.
- Candidates ending in -push-pending are project-level pseudo-beads, not beads: never abandon-with-summary or close_satisfied them — only repair:re-push-integration applies. If the reason is divergence or permissions, it is a human problem; say so in reasoning and move on.
- Candidates ending in -campaign-anomaly are fleet-level findings from the health digest (their reason carries the anomaly and a compact fleet snapshot). Only two verbs apply: repair:park-campaign to freeze that project's campaign behind a hold gate (reversible; the default for chain-discipline violations), and repair:log-improvement to record a hydra improvement proposal for human review (payload = the proposal). Never abandon or close them. Prefer log-improvement when the right fix is a hydra code change rather than a containment.
- Never repair for merges, PRs, deploys, or repo content edits — those are human-only; if the state needs one, explain it in reasoning.

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

func itoa(n int) string { return fmt.Sprintf("%d", n) }
