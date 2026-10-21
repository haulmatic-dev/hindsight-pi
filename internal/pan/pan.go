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

// FixBead is an open auto-fix bead together with the recorded evidence of the
// original closure it exists to redo. Pan's closure-audit duty: judge whether
// a worker still needs to redo finished work before one is spawned.
type FixBead struct {
	BeadID          string
	OriginalID      string // original bead the fix was created for
	CloseReason     string // why the original closed as failed
	RecordedVerdict string // HYDRA_VERDICT (or equivalent) recorded with the original closure; "" if none
}

// FixVerification is the outcome of a cheap re-verification of the original
// verdict's own evidence in the inherited worktree. Status is one of
// "verified", "contradicted", "unknown".
type FixVerification struct {
	Status string
	Detail string
}

// FixBeadsFunc returns the current set of open auto-fix beads. Nil means
// "none" and must never panic.
type FixBeadsFunc func() []FixBead

// FixVerifierFunc cheaply re-verifies a fix bead's original verdict by running
// the verdict's own commands in the inherited worktree. Must never panic;
// return Status "unknown" on any doubt.
type FixVerifierFunc func(fixBeadID, originalID string) FixVerification

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

	// FixBeads returns open auto-fix beads for the closure-audit sweep. Pan
	// judges each: verdict says done AND re-verification confirms → close_satisfied
	// before a worker redoes finished work; verification contradicts the verdict
	// → let the fix proceed and flag the original verdict as hollow. Nil disables.
	FixBeads FixBeadsFunc

	// TrajectoryFunc gathers tier-1 mechanical trajectory telemetry for a live
	// worker (pi session parsing on the daemon side). Called from Candidates
	// for working workers only, and only past trajectoryMinAge — young workers
	// are mid-first-turn and always look idle. Nil disables the class.
	TrajectoryFunc func(beadID string) (Trajectory, bool)

	// BeadClass reports the bead's phase class ("syntax", "red", "green",
	// "fix", ...) for turn-baseline selection. Nil means "default".
	BeadClass func(beadID string) string

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

// repairWhitelist is the only set of daemon-side repair actions Pan may
// propose — the hygiene set: per-bead scheduler hygiene only. Campaign
// containment (park/unpark-campaign), push/integration and pruning verbs were
// removed (FR-022): they mutate fleet state and are now surfaced as filed
// improvement beads (file-bead) for human review instead.
// The prompt lists these names explicitly; the parser rejects anything else so
// the model cannot invent action names and burn MiniMax calls.
var repairWhitelist = map[string]bool{
	"repair-worktree":   true,
	"reap-placeholder":  true,
	"reset-retries":     true,
	"repair-bead-store": true,
	"clear-hard-park":   true,
	"clear-escalation":  true,
	"retry-quarantine":  true,
	// Process-hygiene kills (2026-10-20 incident): Charon records zero-token
	// stalls and rogue bare-pi processes; Pan may dispatch the explicit-PID
	// kill repair (NEVER by PGID — gates share the daemon process group).
	"kill-stalled-worker": true,
	"kill-rogue-pi":       true,

	// recover-spawn-cohort is project-scoped but not LLM-invocable yet
	// (deterministic PrunableWedgeSweep only) — kept out of repairWhitelist
	// so judgment cannot fire it per-bead (fmsfe-npa 2026-10-02).
}

// pseudoBeadSuffix marks synthetic project-level candidates (e.g.
// "<project>-push-pending") that are not real beads: close/abandon/worker
// commands can never succeed for them — only repair:<name> applies.
const pseudoBeadSuffix = "-push-pending"

// ActFileBead is the FR-022 bead-filing actuator: Pan files an improvement
// bead for human triage instead of mutating fleet state directly.
const ActFileBead = "file-bead"

// MaxOpenFiledBeadsPerProject is the nil-safe fallback for the per-project
// open pan-filed-bead cap (FR-022). The live cap comes from the config knob
// shepherd.pan.max_filed_beads_per_project (default
// DefaultPanFiledBeadsPerProject = 4); configs predating the knob keep this
// fallback so a nil config never widens the cap silently.
const MaxOpenFiledBeadsPerProject = 2

// panFiledLabel marks beads filed by Pan via the file-bead actuator; the cap
// counts only beads carrying it.
const FiledLabel = "pan-filed"

// ErrFileBeadDuplicate signals that the filer refused to create the bead
// because an open pan-filed bead with the same normalized title already
// exists (persistent cross-session dedupe). The caller must treat this as a
// benign skip, not a filer failure.
var ErrFileBeadDuplicate = errors.New("file-bead duplicate: open pan-filed bead with the same normalized title already exists")

// ErrFileBeadPremiseStale signals that the filer re-verified the live state
// backing the file-bead decision and found the premise no longer true: the
// judgment source bead has resolved, a live worker reappeared, or the digest
// the judgment was built on is too old to act on (2026-10-05: srv-jobfile-jm2
// was filed claiming "no worktree, no worker" while the worktree existed and
// the worker had been live minutes earlier — the digest snapshot was stale by
// filing time). The caller must treat this as a benign skip, not a filer
// failure: keep the signature, a fresh digest re-fires when the wedge returns.
var ErrFileBeadPremiseStale = errors.New("file-bead premise stale: live state no longer matches the finding")

// NormalizeReason is the exported form of normalizeReason for filers that
// need to compare titles under the same volatile-stripping rules.
func NormalizeReason(reason string) string { return normalizeReason(reason) }

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
	"recover-spawn-cohort":        true,
	"re-push-integration":         true,
	"park-campaign":               true,
	"log-improvement":             true,
}

// isAllowedAction returns true for known worker command kinds, for
// whitelisted "repair:<name>" daemon-side actions, and for the file-bead
// actuator (FR-022).
func isAllowedAction(action string) bool {
	if action == ActFileBead {
		return true
	}
	if allowedActions[registry.CommandKind(action)] {
		return true
	}
	name, ok := parseRepairName(action)
	if !ok {
		return false
	}
	return repairWhitelist[name]
}

// IsAllowedRepair reports whether "repair:<name>" is in the Pan repair
// whitelist. Test seam for the repair vocabulary contract.
func IsAllowedRepair(action string) bool {
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
	// Volatile identity noise that made wedge filings look distinct:
	// filesystem paths (/home/x/projects/foo, worktrees) and bead-ID-like
	// tokens (hydra-ee5r, fmsfe-090) embedded in blocked reasons.
	volatilePathRe = regexp.MustCompile(`(?:/|~)[\w./~-]{2,}`)
	volatileBeadRe = regexp.MustCompile(`(?i)\b[a-z][a-z0-9]{1,14}-[0-9a-z]{2,5}\b`)
	// Cardinal-number variance ("2 orphaned" vs "3 in_progress", "17 beads")
	// is likewise non-identity for signature purposes.
	volatileNumberRe = regexp.MustCompile(`\d+`)
)

// normalizeReason strips volatile substrings — ages ("94h27m"), timestamps,
// "N minutes" phrasings — from a candidate reason. Used ONLY for signatures
// and repair-efficacy keys, never for the judgment prompt: the model still
// sees the raw reason with its fresh age.
func normalizeReason(reason string) string {
	r := volatileDateTimeRe.ReplaceAllString(reason, "#")
	r = volatileDurationRe.ReplaceAllString(r, "#")
	r = volatileUnitsRe.ReplaceAllString(r, "# units")
	r = volatilePathRe.ReplaceAllString(r, "#path")
	r = volatileBeadRe.ReplaceAllString(r, "#id")
	r = volatileNumberRe.ReplaceAllString(r, "#N")
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

// freshCandidates filters out candidates whose state hash was already
// judged within the re-judge valve, returning only those that need a fresh
// judgment. Keyed PER CANDIDATE, not per set: a whole-set signature reset
// whenever ANY candidate's volatile fields moved (token-budget counts,
// digest text), re-judging stable parked candidates every tick — the
// dup-continue trickle (2026-10-04: Pan re-judged a parked, already-pending
// candidate every ~2min, each verdict a duplicate 'continue' the pending
// queue then skipped; constant small GLM burn, zero new information).
// The re-judge valve still backstops per candidate: a permanently stuck
// candidate is re-presented at most every reJudgeInterval so verdict memory
// (ContinuedRecently etc.) decays and heals. Entries older than the valve
// are pruned so the cache cannot grow unbounded under candidate churn.
func (s *Shepherd) freshCandidates(cands []Candidate) []Candidate {
	out := make([]Candidate, 0, len(cands))
	now := time.Now()
	s.dedupMu.Lock()
	defer s.dedupMu.Unlock()
	if s.judged == nil {
		s.judged = make(map[string]time.Time)
	}
	for sig, at := range s.judged {
		if now.Sub(at) > reJudgeInterval {
			delete(s.judged, sig)
		}
	}
	for _, c := range cands {
		sig := candidateSignature([]Candidate{c})
		if last, ok := s.judged[sig]; ok && now.Sub(last) < reJudgeInterval {
			continue
		}
		s.judged[sig] = now
		out = append(out, c)
	}
	return out
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
const (
	// judgeDegradedThreshold is the consecutive pi-invocation failure count
	// at which the judge is declared degraded (loud log + OnJudgeFailure).
	judgeDegradedThreshold = 5
	// judgeDegradedRepeat re-fires the degraded signal every N further
	// consecutive failures so a multi-day outage keeps surfacing.
	judgeDegradedRepeat = 25
)

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
	// BeadFiler files an improvement bead for human triage (file-bead
	// actuator, FR-022): (source bead ID, title, description). Signature
	// dedupe and the per-project open-filed cap are enforced around it.
	// Optional; nil means file-bead decisions are rejected.
	BeadFiler func(beadID, title, description string) error
	// OnRepairEvent is called after a repair handler runs. If nil, Pan logs
	// the event with log.Printf. Optional; used by tests to capture events.
	OnRepairEvent func(beadID, action, result, detail string)
	// ProjectScope maps a bead (or pseudo-bead like "<project>-push-pending")
	// to its project path. Used to rate-limit project-scoped repairs per
	// project instead of per bead. Optional; nil means per-bead keying.
	ProjectScope func(beadID string) string

	// OnJudgeFailure fires when consecutive pi-invocation failures cross
	// judgeDegradedThreshold (and every judgeDegradedRepeat-th after): the
	// judge is DOWN and every judgment is silently running on deterministic
	// rules. 2026-09-23: 6494/6494 invocations failed across Aug-Sep with no
	// signal — the fail-open fallback hid a dead MiniMax plan for weeks.
	// Optional; nil means the loud log line still prints.
	OnJudgeFailure func(consecutive int, err error)
	// OnJudgeRecovered fires once when the judge succeeds again after a
	// degraded outage, reporting how many failures the outage contained.
	OnJudgeRecovered func(failures int)

	verdictsMu sync.Mutex
	verdicts   map[string]time.Time // bead → last life-continuing verdict

	repairMu      sync.Mutex
	repairLimits  map[string]time.Time // bead → last repair run
	repairResults map[string]error     // bead → last repair error (for rate-limit detail)

	// fixVerify is the closure-audit re-verifier; fixVerCache rate-limits it:
	// re-verification runs real commands, so each bead is verified at most
	// once per fixVerTTL (unknown results retry on the same horizon).
	fixVerify   FixVerifierFunc
	fixVerMu    sync.Mutex
	fixVerCache map[string]fixVerEntry

	// scanLive records which beads held a live (working/parked) worker row at
	// the most recent Candidates() scan. Tick uses it to tell worker-origin
	// decisions from blocked-origin ones at decision-execution time (br-4y5,
	// 2026-09-11: a blocked-bead abandon executed after the bead's own worker
	// row registered mid-judgment — a near-miss silent kill of healthy work).
	scanMu   sync.Mutex
	scanLive map[string]bool

	rejectedMu      sync.Mutex
	rejectedActions map[string]bool // beadID + "/" + action; unknown repair actions remembered per session

	filedMu   sync.Mutex
	filedSigs map[string]bool // project-scope + normalized title; one bead per signature per session (FR-022)

	pseudoMu   sync.Mutex
	pseudoSeen map[string]pseudoSeenEntry // pseudo-bead → last presented reason

	dedupMu sync.Mutex
	judged  map[string]time.Time // per-candidate state-hash → last judged (valve backstop)

	efficacyMu     sync.Mutex
	repairEfficacy map[string]int // repair key → consecutive attempts

	judgeFailures int  // consecutive pi-invocation failures (reset on success)
	judgeDegraded bool // loud degraded signal already fired for this outage
}

const fixVerifyCacheTTL = 30 * time.Minute

type fixVerEntry struct {
	ver FixVerification
	at  time.Time
}

// fixVerification returns the (rate-limited) re-verification result for a fix
// bead. Verified/contradicted results are cached for fixVerifyCacheTTL; the
// verifier runs real gate commands, so it must never fire every tick.
func (s *Shepherd) fixVerification(fixBeadID, originalID string) FixVerification {
	s.fixVerMu.Lock()
	defer s.fixVerMu.Unlock()
	if e, ok := s.fixVerCache[fixBeadID]; ok && time.Since(e.at) < fixVerifyCacheTTL {
		return e.ver
	}
	ver := FixVerification{Status: "unknown", Detail: "no verifier configured"}
	if s.fixVerify != nil {
		ver = s.fixVerify(fixBeadID, originalID)
	}
	if s.fixVerCache == nil {
		s.fixVerCache = make(map[string]fixVerEntry)
	}
	s.fixVerCache[fixBeadID] = fixVerEntry{ver: ver, at: time.Now()}
	return ver
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

// SetBeadFiler injects the improvement-bead filer for the file-bead
// actuator (FR-022).
func (s *Shepherd) SetBeadFiler(fn func(beadID, title, description string) error) {
	s.BeadFiler = fn
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

// SetFixBeads injects the optional open auto-fix bead source (closure audit).
func (s *Shepherd) SetFixBeads(fn FixBeadsFunc) {
	s.cfg.FixBeads = fn
}

// SetFixVerifier injects the optional cheap re-verifier for fix-bead audits.
func (s *Shepherd) SetFixVerifier(fn FixVerifierFunc) {
	s.fixVerify = fn
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
	cands = s.freshCandidates(cands)
	if len(cands) == 0 {
		return
	}
	// Fleet-automation prune sweep: deterministic, zero-token batch prune of
	// stale worktree-registration wedge cohorts. Runs before LLM judgment so
	// shared wedges clear without burning judgment rounds or hitting the
	// repair-efficacy refusal ("retry exhausted after N attempts").
	// Fail-open: report is logged only.
	report := s.PrunableWedgeSweep(cands)
	if len(report.Successes) > 0 || len(report.Residuals) > 0 {
		log.Printf("[PruneSweep] done: %d cohort(s) pruned, %d residual(s)", len(report.Successes), len(report.Residuals))
		if len(report.Successes) > 0 {
			log.Printf("[PruneSweep] successes: %v", report.Successes)
		}
		for key, detail := range report.Residuals {
			log.Printf("[PruneSweep] residual: %s — %s", key, detail)
		}
	}
	log.Printf("[Pan] %d candidate(s) need judgment", len(cands))

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	out, err := s.run(ctx, s.cfg, BuildPrompt(cands))
	if err != nil {
		s.judgeFailures++
		log.Printf("[Pan] pi invocation failed (deterministic rules in force): %v", err)
		if !s.judgeDegraded && s.judgeFailures >= judgeDegradedThreshold {
			s.judgeDegraded = true
			log.Printf("[Pan] ERROR judge DEGRADED: %d consecutive pi invocation failures — every judgment is falling back to deterministic rules; last error: %v", s.judgeFailures, err)
			if s.OnJudgeFailure != nil {
				s.OnJudgeFailure(s.judgeFailures, err)
			}
		} else if s.judgeDegraded && (s.judgeFailures-judgeDegradedThreshold)%judgeDegradedRepeat == 0 {
			log.Printf("[Pan] ERROR judge STILL DEGRADED: %d consecutive pi invocation failures; last error: %v", s.judgeFailures, err)
			if s.OnJudgeFailure != nil {
				s.OnJudgeFailure(s.judgeFailures, err)
			}
		}
		return
	}
	if s.judgeFailures > 0 {
		if s.judgeDegraded {
			log.Printf("[Pan] judge RECOVERED after %d consecutive pi invocation failure(s)", s.judgeFailures)
			if s.OnJudgeRecovered != nil {
				s.OnJudgeRecovered(s.judgeFailures)
			}
		}
		s.judgeFailures = 0
		s.judgeDegraded = false
	}

	decisions, err := ParseDecisions(out)
	if err != nil {
		log.Printf("[Pan] could not parse pi decisions (skipping): %v", err)
		return
	}

	enqueued := 0
	repaired := 0
	filed := 0
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

		// FR-022 bead-filing actuator: daemon-side, valid for pseudo-beads
		// too (a campaign-anomaly finding becomes an improvement bead).
		if d.Action == ActFileBead {
			s.handleFileBead(d.BeadID, d.Message)
			filed++
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
	if filed > 0 {
		log.Printf("[Pan] processed %d file-bead decision(s)", filed)
	}
}

// handleRepair executes a whitelisted repair handler for a blocked bead.
// It is fail-open: errors are logged and emitted as audit events only.
// reason carries the decision's justification; log-improvement persists it
// as the improvement text, other handlers ignore it.
func (s *Shepherd) handleRepair(beadID, name, reason string) {
	s.runRepair(beadID, name, reason)
}

// runRepair executes a whitelisted repair handler for a blocked bead and
// returns the outcome status: "ok", "refused", "rate limited", or "error".
// It is fail-open: errors are logged and emitted as audit events only.
// reason carries the decision's justification; log-improvement persists it
// as the improvement text, other handlers ignore it. Shared by the verdict
// path (handleRepair) and the deterministic prune sweep (PrunableWedgeSweep).
func (s *Shepherd) runRepair(beadID, name, reason string) string {
	// log-improvement is a pure observation sink: no live-worker refusal
	// (the whole point is reporting on live situations) and no handler map —
	// the shepherd writes the improvement line itself.
	if name == "log-improvement" {
		s.handleLogImprovement(beadID, reason)
		return "ok"
	}
	// Safety gate: never repair a bead that still has a live worker row.
	if s.hasLiveWorker(beadID) {
		s.emitRepairEvent(beadID, name, "error", "refused: bead has a live worker row")
		log.Printf("[Pan] repair %s for %s refused: live worker row exists", name, beadID)
		return "refused"
	}
	// Rate limit: per-bead hourly; project-scoped repairs share a project key.
	if ok, remaining := s.canRepair(beadID, name); !ok {
		detail := fmt.Sprintf("rate limited: %v until next repair", remaining)
		s.emitRepairEvent(beadID, name, "error", detail)
		log.Printf("[Pan] repair %s for %s rate limited (%v remaining)", name, beadID, remaining)
		return "rate limited"
	}
	// Efficacy gate: refuse after maxConsecutiveRepairs attempts on the same
	// (action, scope, normalized reason). The wedge that spams repairs never
	// gets fixed by the 4th identical attempt either — classify human-only.
	if key := s.repairEfficacyKey(beadID, name); !s.noteRepairAttempt(key) {
		detail := "ineffective: same repair attempted " + itoa(maxConsecutiveRepairs) + "+ times without the condition changing; human-only"
		s.emitRepairEvent(beadID, name, "refused", detail)
		log.Printf("[Pan] repair %s for %s refused (ineffective — condition unchanged across %d attempts)", name, beadID, maxConsecutiveRepairs)
		return "refused"
	}

	handler, ok := s.RepairHandlers[name]
	if !ok || handler == nil {
		s.emitRepairEvent(beadID, name, "error", "unknown repair handler")
		log.Printf("[Pan] repair %s for %s rejected: no handler configured", name, beadID)
		return "error"
	}

	if err := handler(beadID); err != nil {
		s.recordRepair(beadID, name, err)
		s.emitRepairEvent(beadID, name, "error", err.Error())
		log.Printf("[Pan] repair %s for %s failed: %v", name, beadID, err)
		return "error"
	}
	s.recordRepair(beadID, name, nil)
	s.emitRepairEvent(beadID, name, "ok", "")
	log.Printf("[Pan] repair %s for %s succeeded", name, beadID)
	return "ok"
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

// handleFileBead implements the FR-022 bead-filing actuator: Pan files an
// improvement bead for human triage instead of mutating fleet state.
// Guards: rate limit (shared repair limiter), signature dedupe (one filed
// bead per project + normalized title per session), and the filer's own
// per-project open-filed cap.
func (s *Shepherd) handleFileBead(beadID, payload string) {
	if s.BeadFiler == nil {
		s.emitRepairEvent(beadID, ActFileBead, "error", "no bead filer configured")
		return
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		s.emitRepairEvent(beadID, ActFileBead, "error", "empty file-bead payload")
		return
	}
	title, description := payload, ""
	if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
		title = strings.TrimSpace(payload[:idx])
		description = strings.TrimSpace(payload[idx+1:])
	}
	// Signature dedupe: one filed bead per (project, normalized title) per
	// session. Fail-open on scope resolution: fall back to the bead ID.
	scope := s.fileScope(beadID)
	sig := scope + "|" + normalizeReason(title)
	s.filedMu.Lock()
	if s.filedSigs == nil {
		s.filedSigs = make(map[string]bool)
	}
	dup := s.filedSigs[sig]
	if !dup {
		s.filedSigs[sig] = true
	}
	s.filedMu.Unlock()
	if dup {
		s.emitRepairEvent(beadID, ActFileBead, "skipped", "duplicate signature")
		log.Printf("[Pan] file-bead for %s skipped: duplicate signature in %s", beadID, scope)
		return
	}
	if err := s.BeadFiler(beadID, title, description); err != nil {
		if errors.Is(err, ErrFileBeadDuplicate) {
			// Persistent dedupe (filer-side, survives restarts): benign skip,
			// keep the signature so this session stops retrying too.
			s.emitRepairEvent(beadID, ActFileBead, "skipped", "duplicate signature")
			log.Printf("[Pan] file-bead for %s skipped: %v", beadID, err)
			return
		}
		if errors.Is(err, ErrFileBeadPremiseStale) {
			// Filer re-verified live state before creating the bead and found
			// the premise resolved (jm2 class). Benign skip: keep the signature
			// so this session stops retrying; a fresh digest re-fires if the
			// wedge returns.
			s.emitRepairEvent(beadID, ActFileBead, "skipped", "premise stale")
			log.Printf("[Pan] file-bead for %s skipped: %v", beadID, err)
			return
		}
		// Filer failure (cap, store error) forgets the signature so a later
		// attempt can retry once the cap frees up.
		s.filedMu.Lock()
		delete(s.filedSigs, sig)
		s.filedMu.Unlock()
		s.recordRepair(beadID, ActFileBead, err)
		s.emitRepairEvent(beadID, ActFileBead, "error", err.Error())
		log.Printf("[Pan] file-bead for %s failed: %v", beadID, err)
		return
	}
	s.recordRepair(beadID, ActFileBead, nil)
	s.emitRepairEvent(beadID, ActFileBead, "ok", "")
	log.Printf("[Pan] filed bead for %s: %s", beadID, truncate(title, 120))
}

// fileScope resolves the dedupe scope for a file-bead decision: the
// project path when resolvable (pseudo-beads included). No scope → "":
// signature dedupe then keys on the normalized title alone, so the same
// finding surfacing from two beads still files one bead.
func (s *Shepherd) fileScope(beadID string) string {
	if s.ProjectScope != nil {
		if scope := s.ProjectScope(beadID); scope != "" {
			return scope
		}
	}
	return ""
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
		// Tier-1 trajectory pre-filters: mechanical signals over the worker's
		// pi session. Only outliers become candidates — tier-2 LLM judgment
		// budget follows the anomaly rate, healthy trajectories never burn it.
		if reason == "" && s.cfg.TrajectoryFunc != nil &&
			w.State == registry.StateWorking && now.Sub(w.StartedAt) >= trajectoryMinAge {
			if t, ok := s.cfg.TrajectoryFunc(w.BeadID); ok {
				class := "default"
				if s.cfg.BeadClass != nil {
					class = s.cfg.BeadClass(w.BeadID)
				}
				if anoms := t.Anomalies(class); len(anoms) > 0 {
					reason = "TRAJECTORY: " + strings.Join(anoms, "; ")
				}
			}
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

	// Closure-audit sweep: open auto-fix beads whose original closed as failed.
	// Judgment duty: verdict says done AND re-verification confirms → close
	// the fix before a worker redoes finished work; verification contradicts
	// the verdict → let the fix proceed and flag the verdict as hollow
	// (jx78, 2026-09-25: recorded done:true proved hollow — suites failed at
	// load, 0 tests ran).
	if s.cfg.FixBeads != nil {
		for _, f := range s.cfg.FixBeads() {
			if live[f.BeadID] {
				continue
			}
			if IsPseudoBeadID(f.BeadID) {
				continue
			}
			ver := s.fixVerification(f.BeadID, f.OriginalID)
			reason := fmt.Sprintf("FIX-AUDIT: open auto-fix bead for original %s (close reason: %s)",
				f.OriginalID, truncate(f.CloseReason, 160))
			if f.RecordedVerdict != "" {
				reason += "; recorded verdict: " + truncate(f.RecordedVerdict, 160)
			}
			reason += fmt.Sprintf("; re-verification: %s — %s", ver.Status, truncate(ver.Detail, 200))
			out = append(out, Candidate{
				Record: registry.WorkerRecord{BeadID: f.BeadID},
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
- "close_satisfied": the bead's acceptance criteria are already met on the working branch and the worker found nothing to do; close it without a worker. For FIX-AUDIT candidates this closes the redundant fix bead after the satisfied gate re-proves acceptance daemon-side.
- "file-bead": file an improvement bead for a human to triage (payload: first line = the bead title, remaining lines = the problem description). Use for fleet-level or product-level findings no repair verb covers: shared wedges, push/branch-protection problems, campaign anomalies. Deduplicated per project by signature and capped at 2 open pan-filed beads per project; duplicates are silently skipped.
- "repair:<name>": execute a whitelisted daemon-side repair (payload ignored). Only for BLOCKED beads with no live worker row. The only valid repair names — the hygiene set — are: repair-worktree, reap-placeholder, reset-retries, repair-bead-store, clear-hard-park, clear-escalation, retry-quarantine, kill-stalled-worker, kill-rogue-pi. No other repair:<name> strings exist; if none fit, choose continue, abandon-with-summary, or file-bead.

Rescue recipes (hard-won; prefer the repair matching the cause):
- FIX-AUDIT candidates are OPEN auto-fix beads whose original bead closed as failed; each reason carries the close reason, any recorded verdict, and a re-verification status of the original evidence in the inherited worktree. Judge whether a worker still needs to redo the work:
  - Recorded verdict claims done AND re-verification says "verified" → "close_satisfied" (closing the fix prevents redoing finished work). Never close on a verdict alone: a recorded verdict can be hollow (jx78: done:true recorded while the suites failed at load, 0 tests ran).
  - Re-verification says "contradicted" → "continue" (the fix must proceed); if the recorded verdict claims done, say in the message that the original verdict is hollow and cite the re-verification detail.
  - Re-verification "unknown" → "continue" (keep the fix) unless the fix-bead description clearly shows acceptance already met.
  - Never abandon-with-summary or repair: a FIX-AUDIT candidate.
- Hard-park labels (worktree-unavailable, integration-exhausted) are durable by design — the scheduler never retries them. Use repair:clear-hard-park when the named cause is plausibly fixed or transient (worktree directory exists, store parses clean, push path repaired). Do not clear a park whose cause is still present.
- Stale human escalations (BLOCKED, reason mentions "escalated to a human", aging in days, no live worker, no token progress): after roughly two days with no human response the escalation is stale — prefer repair:clear-escalation, which drops the escalated/needs-human labels and re-queues the bead for one scheduler retry (the retry counter is reset, so the bead gets a fair pass; if the same failure recurs, the escalation re-fires and human triage is the next step). If the bead's work is clearly obsolete, abandon-with-summary instead. Escalations younger than ~2 days may still get a human: leave blocked — UNLESS the reason reports the escalation is beyond the configured leaf grace (e.g. "beyond the 1h0m0s leaf grace"): the operator has deliberately shortened the human window for defect-class escalations, so prefer repair:clear-escalation with the same one-fair-retry contract (re-escalates on merit). EXCEPTION — wedged chain heads: when the reason reports WEDGING N dependent bead(s), the escalation is stalling a live dependency chain (the bead stays in_progress with no worker by construction, so orphan recovery cannot touch it and every dependent starves). Prefer repair:clear-escalation as soon as the wedge grace (~1h) has passed — a fresh retry either passes now that the original cause is gone, or re-escalates with fresh evidence; leaving it blocked guarantees the campaign stalls. Only leave a wedged escalation for the human when the failure was clearly substantive (code rejected on merit, not infra).
- Many BLOCKED beads with the same reason = a shared wedge. The deterministic PrunableWedgeSweep already runs ONE project-scoped batch repair (prune-worktree-registration for worktree cohorts, recover-spawn-cohort for spawn-failed cohorts) per sweep tick — do not attempt per-bead repairs for cohort members; fix one bead only if it diverges, and file-bead a proposal describing the shared wedge so a human can address the cause.
- repair:reset-retries clears the retry counter after the underlying cause is fixed; useless while the cause persists.
- Orphaned in_progress claims (the digest's orphanclaims counter / "P0 campaign anomaly: N in_progress bead(s) with no live registry worker") are the same P0 class as a fleet stall: the claims wedge the whole DAG ladder and progress is silent zero. The scheduler's orphan-claim reconciliation and Charon recovery are the mechanical fix; if the anomaly persists across judgments, file-bead it for human review naming the wedged bead IDs.
- repair:repair-bead-store restores a corrupted issues.jsonl from the newest clean backup; pick it when the reason mentions conflict markers or parse errors.
- Candidates ending in -campaign-anomaly are fleet-level findings from the health digest (their reason carries the anomaly and a compact fleet snapshot). Never abandon or close them — file-bead the finding for human review (payload = title line + proposal). When the right fix is a hydra code change, the proposal should say so. A publisher-wedge anomaly whose reason indicates branch protection, required approvals, or permissions (GH013 rule violations, approval-required, review-team gates) is a HUMAN problem: the proposal must name the human action (approve the PR / adjust the branch rule) and the rules URL when the reason carries one — do not propose automating it.
- PUSH-PENDING candidates mean commits exist locally but not on origin. Pushing is a human/pipeline action now: if the cause is branch protection, required approvals, or permissions, say so in reasoning and file-bead the proposal naming the human action. Otherwise choose the best judgment you can; never abandon-with-summary or close_satisfied them.
- Candidates whose reason starts with TRAJECTORY were flagged by mechanical
  tier-1 pre-filters (turn-count outliers vs their bead-class baseline,
  repeated commands, edits without test runs, test-fix-test loops, no new
  files, same file re-edited without verifying). This is your tier-2 deep
  judgment — the worker is mid-flight, not dead. Read the flags and decide:
  converging → "continue" (or "steer" with a specific redirect naming what
  to do differently); the bead's goal already satisfied on the working
  branch → "close_satisfied" (cite the evidence in the message); clearly
  looping with no convergence → "abandon-with-summary" (payload: what was
  done and what remains). Do NOT abandon a trajectory that is slow but
  making measurable progress.
- Stranded worktrees (a hydra/wt-<key> branch is ahead of the working branch, the owning bead is terminal or unknown, and there is no live worker or active in-progress chain) are repaired with repair:integrate-stranded. A clean merge integrates the branch and removes the worktree; a merge conflict quarantines it to hydra/quarantine/<key> and labels the bead stranded-conflict; a post-merge verification failure labels integration-unverified.
- Quarantined strands (bead labeled stranded-conflict, branch on hydra/quarantine/<key>): the base branch may have moved since the conflict. Use repair:retry-quarantine — it merge-trees the quarantine branch against the current base and, only if now clean, restores the branch and re-queues the bead through the ordinary integration paths; if it still conflicts, the repair is refused and the bead stays quarantined. Prefer retry-quarantine over clear-hard-park for stranded-conflict beads.
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
