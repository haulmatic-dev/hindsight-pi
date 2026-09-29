package core

import (
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// escalationExpiredLabel is the audit marker left on a bead whose human
// escalation aged past the configured TTL. The `escalated` label is removed
// at the same time so the scheduler ready-filter, DAG navigator and retry
// queue stop skipping the bead; without the companion marker the demotion
// would be indistinguishable from an escalation that never happened.
const escalationExpiredLabel = "escalation-expired"

// EscalationTTLScanner periodically demotes stale human escalations.
//
// An `escalated` label is a durable park: the scheduler, retry queue, DAG
// navigator and Charon all skip beads carrying it, by design (respawning
// into the same death the escalation stopped). But escalations were never
// adjudicated — nothing expires them — so a bead whose human never responds
// wedges its scheduler slot forever (measured: 91h–290h stale, e.g.
// hydra-wdp, hydra-cip, hydra-gvh, hydra-ol7). This scanner is the aging
// mechanism: after EscalationTTL with no human action, the `escalated`
// label is removed (the bead becomes ready again) and `escalation-expired`
// is added for the audit trail, with a structured event emitted.
//
// Fail-open: scan errors are logged and retried on the next tick; they never
// gate scheduling.
type EscalationTTLScanner struct {
	beadsClient BeadsClient
	getProjects func() []types.Project
	ttl         time.Duration // max age of an escalation before demotion (<=0 disables)
	interval    time.Duration // scan cadence
	events      *EventSink    // nil-safe
	stopChan    chan struct{}
}

// NewEscalationTTLScanner creates the scanner. ttl <= 0 disables demotion
// (the scanner then never runs).
func NewEscalationTTLScanner(beadsClient BeadsClient, getProjects func() []types.Project, ttl time.Duration, events *EventSink) *EscalationTTLScanner {
	return &EscalationTTLScanner{
		beadsClient: beadsClient,
		getProjects: getProjects,
		ttl:         ttl,
		interval:    time.Hour,
		events:      events,
		stopChan:    make(chan struct{}),
	}
}

// Start launches the scan loop. No-op when disabled.
func (s *EscalationTTLScanner) Start() {
	if s == nil || s.ttl <= 0 || s.beadsClient == nil || s.getProjects == nil {
		return
	}
	log.Printf("[EscalationTTL] ⏳ Stale-escalation aging enabled (TTL: %v, interval: %v)", s.ttl, s.interval)
	go s.run()
}

// Stop halts the scan loop.
func (s *EscalationTTLScanner) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

func (s *EscalationTTLScanner) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.scan() // run once at startup so wedged beads free up without waiting an hour
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

// scan demotes every non-terminal bead whose `escalated` label has aged past
// the TTL. in_progress beads are skipped: a live worker may legitimately be
// finishing under the escalation stay, and demoting mid-flight would race
// the close path.
func (s *EscalationTTLScanner) scan() {
	now := time.Now()
	demoted := 0
	for _, project := range s.getProjects() {
		beads, err := s.beadsClient.ListBeads(project.Path, "")
		if err != nil {
			// Fail-open: a listing failure must never block scheduling.
			log.Printf("[EscalationTTL] listing beads for %s failed (fail open): %v", project.Name, err)
			continue
		}
		for _, b := range beads {
			if b.Status.IsTerminal() || b.Status == types.StatusInProgress {
				continue
			}
			if !hasBeadLabel(b.Labels, escalatedLabel) {
				continue
			}
			if now.Sub(b.UpdatedAt) <= s.ttl {
				continue
			}
			if err := s.demote(project.Path, b); err != nil {
				log.Printf("[EscalationTTL] demotion failed for %s (fail open): %v", b.ID, err)
				continue
			}
			demoted++
			log.Printf("[EscalationTTL] ⏳ Demoted stale escalation on %s (age %v > TTL %v); bead is ready again", b.ID, now.Sub(b.UpdatedAt).Round(time.Minute), s.ttl)
		}
	}
	if demoted > 0 {
		log.Printf("[EscalationTTL] scan complete: %d stale escalation(s) demoted", demoted)
	}
}

// demote swaps `escalated` for `escalation-expired` and emits the typed event.
func (s *EscalationTTLScanner) demote(projectPath string, b types.Bead) error {
	if err := s.beadsClient.RemoveBeadLabels(projectPath, "", b.ID, []string{escalatedLabel}); err != nil {
		return err
	}
	if err := s.beadsClient.UpdateBeadLabels(projectPath, "", b.ID, []string{escalationExpiredLabel}); err != nil {
		return err
	}
	if s.events != nil {
		s.events.EmitRepo(projectPath, Event{Type: "escalation_expired", Project: b.Project,
			Bead: b.ID.String(), Reason: "ttl_expired",
			Detail: "escalation aged past TTL with no human action; label demoted, bead returned to the ready queue"})
	}
	return nil
}
