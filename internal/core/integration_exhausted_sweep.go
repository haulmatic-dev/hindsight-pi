package core

import (
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// IntegrationExhaustedSweeper periodically surfaces integration-exhausted
// hard-parks that have gone stale.
//
// The `integration-exhausted` label is a durable park by design: the
// scheduler, DAG navigator, Charon and the orphan-retry path all skip beads
// carrying it, so the work the bead represents can sit invisible forever
// when the triggering integration cause was fixed elsewhere or never
// actually blocked (measured wedges: entire fleets carrying 145+ such
// labels). The integration-retest stage already re-attempts beads whose
// branch still merges — but only up to MaxAttempts, and only for beads it
// selects. Nothing looks at the ones left behind.
//
// This sweeper is the visibility mechanism: any non-terminal bead carrying
// the label whose UpdatedAt has aged past the configured threshold gets a
// `needs-human` label (idempotent — skipped if already present) and a typed
// `integration_exhausted_stale` event, so abandoned attempts surface for
// either manual intervention or the Pan retry-quarantine repair instead of
// staying hidden. It does NOT clear the hard-park: the label stays durable
// by design; surfacing is the point.
//
// UpdatedAt is the age proxy (same trade-off as EscalationTTL): any label
// touch resets the clock, which is the conservative direction.
//
// Fail-open: listing or labeling errors are logged and retried on the next
// tick; they never gate scheduling.
type IntegrationExhaustedSweeper struct {
	beadsClient BeadsClient
	getProjects func() []types.Project
	ttl         time.Duration // age threshold before a park is surfaced (<=0 disables)
	interval    time.Duration // scan cadence
	events      *EventSink    // nil-safe
	stopChan    chan struct{}
}

const (
	// needsHumanLabel marks a bead surfaced for operator attention.
	needsHumanLabel = "needs-human"
	// integrationExhaustedStaleEvent is emitted per surfaced bead.
	integrationExhaustedStaleEvent = "integration_exhausted_stale"
)

// NewIntegrationExhaustedSweeper creates the sweeper. ttl <= 0 disables it
// (the sweeper then never runs).
func NewIntegrationExhaustedSweeper(beadsClient BeadsClient, getProjects func() []types.Project, ttl time.Duration, events *EventSink) *IntegrationExhaustedSweeper {
	return &IntegrationExhaustedSweeper{
		beadsClient: beadsClient,
		getProjects: getProjects,
		ttl:         ttl,
		interval:    time.Hour,
		events:      events,
		stopChan:    make(chan struct{}),
	}
}

// Start launches the scan loop. No-op when disabled.
func (s *IntegrationExhaustedSweeper) Start() {
	if s == nil || s.ttl <= 0 || s.beadsClient == nil || s.getProjects == nil {
		return
	}
	log.Printf("[IntegrationSweep] 🔎 integration-exhausted sweep enabled (threshold: %v, interval: %v)", s.ttl, s.interval)
	go s.run()
}

// Stop halts the scan loop.
func (s *IntegrationExhaustedSweeper) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

func (s *IntegrationExhaustedSweeper) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.scan() // run once at startup so long-hidden parks surface without waiting an hour
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

// scan surfaces every non-terminal bead whose integration-exhausted park has
// aged past the threshold. in_progress beads are skipped: the integration-
// retest stage and the live close path own those.
func (s *IntegrationExhaustedSweeper) scan() {
	if s == nil || s.ttl <= 0 || s.beadsClient == nil || s.getProjects == nil {
		return
	}
	now := time.Now()
	surfaced := 0
	for _, project := range s.getProjects() {
		beads, err := s.beadsClient.ListBeads(project.Path, "")
		if err != nil {
			log.Printf("[IntegrationSweep] listing beads for %s failed (fail open): %v", project.Name, err)
			continue
		}
		for _, b := range beads {
			if b.Status.IsTerminal() || b.Status == types.StatusInProgress {
				continue
			}
			if !hasBeadLabel(b.Labels, integrationExhaustedLabel) {
				continue
			}
			if now.Sub(b.UpdatedAt) <= s.ttl {
				continue
			}
			// Idempotent: a bead already carrying needs-human was surfaced
			// before; touching it again would just reset its own clock.
			if hasBeadLabel(b.Labels, needsHumanLabel) {
				continue
			}
			if err := s.surface(project.Path, b); err != nil {
				log.Printf("[IntegrationSweep] surfacing %s failed (fail open): %v", b.ID, err)
				continue
			}
			surfaced++
			log.Printf("[IntegrationSweep] 🔎 Surfaced stale integration-exhausted park on %s (age %v > threshold %v); flagged needs-human", b.ID, now.Sub(b.UpdatedAt).Round(time.Minute), s.ttl)
		}
	}
	if surfaced > 0 {
		log.Printf("[IntegrationSweep] scan complete: %d stale integration-exhausted park(s) surfaced", surfaced)
	}
}

// surface adds the needs-human label and emits the typed event.
func (s *IntegrationExhaustedSweeper) surface(projectPath string, b types.Bead) error {
	if err := s.beadsClient.UpdateBeadLabels(projectPath, "", b.ID, []string{needsHumanLabel}); err != nil {
		return err
	}
	if s.events != nil {
		s.events.EmitRepo(projectPath, Event{Type: integrationExhaustedStaleEvent, Project: b.Project,
			Bead: b.ID.String(), Reason: "park_stale",
			Detail: "integration-exhausted park aged past threshold with no disposition; surfaced for manual intervention or retry-quarantine"})
	}
	return nil
}
