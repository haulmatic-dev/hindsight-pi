package core

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// codeChangeMarker is the text a Pan judgment must carry for a pan-filed
// finding to be WORK rather than human triage. Pan's judgment prompt directs
// proposals that name a hydra code change to include this marker (live
// evidence 2026-10-21: ajantha-srv-data-harbour-ukf "Stale worker claim
// wedges bead after retry exhaustion" and 5kx "Orphaned spawn registration
// wedges bead… hydra code change suggested" — both P0, both unspawnable
// while the pan-filed label excluded them from discovery). Matching is
// case-insensitive so the marker survives judgment prose drift.
const codeChangeMarker = "code change suggested"

// isCodeChangeProposal reports whether a pan-filed finding's text names a
// code change (the promotion marker). Title and description are both
// considered: the marker usually lands in the description tail but has
// appeared in titles.
func isCodeChangeProposal(title, description string) bool {
	hay := strings.ToLower(title + "\n" + description)
	return strings.Contains(hay, codeChangeMarker)
}

// panFiledPromotionInterval is the retroactive reconciler cadence. It only
// needs to beat the discovery tick's filter, not the scheduler: a promoted
// bead becomes spawnable on the next discovery pass after its label is
// stripped.
const panFiledPromotionInterval = 5 * time.Minute

// PanFiledPromoter is the retroactive half of the pan-filed promotion path.
// Filing time (filePanBead) routes code-change proposals straight to normal
// task beads; this reconciler catches beads filed BEFORE that routing
// existed: any non-terminal bead carrying the pan-filed label whose text
// carries codeChangeMarker has the label stripped and a typed
// pan_filed_promoted event emitted. Human-triage filings (anomalies,
// campaign findings) carry no marker and stay excluded from discovery.
//
// Fail-open: list/label errors are logged and retried on the next tick.
type PanFiledPromoter struct {
	beadsClient BeadsClient
	getProjects func() []types.Project
	events      *EventSink // nil-safe
	interval    time.Duration
	stopChan    chan struct{}
	stopOnce    sync.Once
}

// NewPanFiledPromoter creates the promoter. A nil beadsClient or getProjects
// makes scan() inert.
func NewPanFiledPromoter(beadsClient BeadsClient, getProjects func() []types.Project, events *EventSink) *PanFiledPromoter {
	return &PanFiledPromoter{
		beadsClient: beadsClient,
		getProjects: getProjects,
		events:      events,
		interval:    panFiledPromotionInterval,
		stopChan:    make(chan struct{}),
	}
}

// Start launches the periodic scan loop (one scan at start, then ticker).
func (p *PanFiledPromoter) Start() {
	if p == nil || p.beadsClient == nil || p.getProjects == nil {
		return
	}
	go func() {
		p.scan()
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.scan()
			case <-p.stopChan:
				return
			}
		}
	}()
}

// Stop terminates the scan loop. Safe to call more than once and on nil.
func (p *PanFiledPromoter) Stop() {
	if p == nil || p.stopChan == nil {
		return
	}
	p.stopOnce.Do(func() { close(p.stopChan) })
}

func (p *PanFiledPromoter) scan() {
	for _, project := range p.getProjects() {
		beads, err := p.beadsClient.ListBeads(project.Path, "")
		if err != nil {
			// Fail-open: a listing error must not gate the pipeline.
			log.Printf("[PanPromotion] list beads for %s: %v", project.Path, err)
			continue
		}
		for _, bead := range beads {
			if bead.Status.IsTerminal() {
				continue
			}
			if !hasBeadLabel(bead.Labels, panFiledLabel) {
				continue
			}
			if !isCodeChangeProposal(bead.Title, bead.Description) {
				continue // human-triage filing: stays excluded
			}
			if bead.Type != "task" {
				// Only task beads are worker-executable; promoting another
				// type would put an unspawnable bead into discovery.
				log.Printf("[PanPromotion] skipping %s: type %q is not task", bead.ID, bead.Type)
				continue
			}
			if err := p.beadsClient.RemoveBeadLabels(project.Path, "", bead.ID, []string{panFiledLabel}); err != nil {
				log.Printf("[PanPromotion] strip %s label from %s: %v", panFiledLabel, bead.ID, err)
				continue // retried next tick
			}
			log.Printf("[PanPromotion] promoted pan-filed code-change proposal %s in %s", bead.ID, project.Path)
			if p.events != nil {
				p.events.EmitRepo(project.Path, Event{
					Type:    "pan_filed_promoted",
					Project: project.Name,
					Bead:    string(bead.ID),
					Reason:  "code_change_marker",
				})
			}
		}
	}
}
