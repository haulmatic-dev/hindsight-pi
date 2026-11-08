package core

import (
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Evidence-of-use probes (deep tier).
//
// The intelligence stack (Hindsight recall, CASS playbook, graphify code
// graphs) is fail-open: every one of its query paths silently degrades to
// "no context" when its backing service is down, so a fleet-wide outage looks
// identical to a healthy fleet that simply has no useful memories. The
// evidence tier closes that observability gap two ways:
//
//   - Workers emit an `intel_usage` record per execution attempt before the
//     agent invocation, recording which intelligence services actually
//     contributed (hindsight / cass / graphify_fallback). See IntelUsageEvent.
//   - A periodic evidence sweep compares recorded worker activity against the
//     hindsight memory store: workers busy but zero memories returned is the
//     signature of a silently degraded Hindsight, and is surfaced as a
//     `tool_degraded` event with probe:"evidence" so Pan/digest can file it.

const (
	// intelSweepInterval is how often the evidence sweep runs per daemon tick
	// schedule (started once hourly, mirroring the other health sweepers).
	intelSweepInterval = time.Hour

	// evidenceActivityWindow bounds how recent a worker heartbeat counts as
	// "recorded activity" for the evidence sweep.
	evidenceActivityWindow = 24 * time.Hour
)

// ToolHealthEvidenceScanner sweeps projects for evidence-of-use degradation:
// workers are running (recorded activity) but Hindsight returns zero
// memories. Probe errors are fail-open — the sweep observes the system and
// must never break it.
type ToolHealthEvidenceScanner struct {
	getProjects    func() []types.Project
	events         *EventSink
	interval       time.Duration
	activityProbe  func(project string) bool
	hindsightProbe func(project string) (int, error)
	stopChan       chan struct{}
}

// NewToolHealthEvidenceScanner builds the evidence-tier scanner. The probes
// are injected (SetActivityProbe / SetHindsightProbe); scan() is inert until
// both are set, so a daemon without registry or Hindsight wiring stays quiet.
func NewToolHealthEvidenceScanner(getProjects func() []types.Project, events *EventSink) *ToolHealthEvidenceScanner {
	return &ToolHealthEvidenceScanner{
		getProjects: getProjects,
		events:      events,
		interval:    intelSweepInterval,
		stopChan:    make(chan struct{}),
	}
}

// SetActivityProbe installs the worker-activity probe: true when the project
// has recorded worker activity (recent registry rows).
func (s *ToolHealthEvidenceScanner) SetActivityProbe(fn func(project string) bool) {
	if s == nil {
		return
	}
	s.activityProbe = fn
}

// SetHindsightProbe installs the hindsight-memory probe: the number of
// memories Hindsight holds/serves for the project, or an error.
func (s *ToolHealthEvidenceScanner) SetHindsightProbe(fn func(project string) (int, error)) {
	if s == nil {
		return
	}
	s.hindsightProbe = fn
}

// ready reports whether both probes are installed and scan() has something
// to observe. A daemon without registry or Hindsight wiring stays quiet.
func (s *ToolHealthEvidenceScanner) ready() bool {
	return s != nil && s.getProjects != nil && s.activityProbe != nil && s.hindsightProbe != nil
}

// Start launches the hourly scan loop. No-op without both probes.
func (s *ToolHealthEvidenceScanner) Start() {
	if !s.ready() {
		return
	}
	log.Printf("[EvidenceSweep] 🔎 evidence-of-use sweep enabled (interval: %v)", s.interval)
	go s.run()
}

// Stop halts the scan loop.
func (s *ToolHealthEvidenceScanner) Stop() {
	if s == nil || s.stopChan == nil {
		return
	}
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

func (s *ToolHealthEvidenceScanner) run() {
	// One sweep at startup, then every interval — same shape as the other
	// health sweepers, so operators see a fresh verdict right after a restart.
	s.scan()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

// scan performs one evidence-tier sweep. Worker activity with zero
// hindsight memories emits one `tool_degraded` event per project
// (probe:"evidence"); no activity emits nothing; probe errors fail open.
func (s *ToolHealthEvidenceScanner) scan() {
	if !s.ready() {
		return
	}
	for _, project := range s.getProjects() {
		if !s.activityProbe(project.Name) {
			// Zero worker activity: nothing to judge — a quiet fleet has no
			// evidence either way, and must emit no evidence-tier events.
			continue
		}
		memories, err := s.hindsightProbe(project.Name)
		if err != nil {
			// Fail-open: a broken probe is the sweep's own defect, not a
			// signal about the intelligence stack.
			log.Printf("[EvidenceSweep] ⚠️  hindsight probe for %s failed (fail-open): %v", project.Name, err)
			continue
		}
		if memories == 0 {
			s.emitDegraded(project.Name, memories)
		}
	}
}

// emitDegraded surfaces one silently-degraded project: workers busy but the
// Hindsight memory store serves nothing.
func (s *ToolHealthEvidenceScanner) emitDegraded(project string, memories int) {
	s.events.Emit(Event{
		Type:    "tool_degraded",
		Project: project,
		Reason:  "hindsight_zero_memories",
		Detail:  "Workers are executing but Hindsight returned zero memories; recall is degraded or unreachable.",
		Fields: map[string]any{
			"probe":    "evidence",
			"memories": memories,
		},
	})
}

// IntelUsageEvent builds the `intel_usage` record a worker execution emits
// before the agent invocation, recording which intelligence services
// contributed context: hindsight recall, the CASS playbook, and whether the
// worker fell back to reading files because no graphify graph was available.
func IntelUsageEvent(hindsight, cass, graphifyFallback bool) Event {
	return Event{
		Type: "intel_usage",
		Fields: map[string]any{
			"hindsight":         hindsight,
			"cass":              cass,
			"graphify_fallback": graphifyFallback,
		},
	}
}
