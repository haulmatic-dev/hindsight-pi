package core

// PR aging sweep (bead hydra-rrtq): closed beads whose integration PR was
// left open waiting on a human merge (PRWaitingError) are recorded in a
// durable ledger; a weekly sweeper checks each PR's state via `gh` and
// produces a per-owner digest for every PR still open past the aging
// threshold. The digest distinguishes the two legitimate states:
//   - t2-human-merge-queue: the repo is NOT auto-merge-allowlisted
//     (output.pr_auto_merge_repos / deploy.repo_path) — a PR waiting for a
//     human is the designed Tier-2 stop state, the digest is a gentle
//     reminder that the human-merge queue is aging.
//   - stuck-auto-merge: the repo IS auto-merge-allowlisted, so an open PR
//     older than the threshold means the automated merge is wedged and
//     needs intervention.
//
// Fail-open throughout: an unreadable ledger, a failed gh probe, or an
// unresolvable owner identity only skips that record's digest entry for
// the current week; nothing here gates scheduling or closing.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// PRWaitingRecord is one closed bead whose integration PR is open waiting
// on a human merge.
type PRWaitingRecord struct {
	BeadID      string    `json:"bead_id"`
	ProjectPath string    `json:"project_path"`
	Owner       string    `json:"owner"`
	URL         string    `json:"url"`
	Since       time.Time `json:"since"`
}

// PRAgingThresholdDays is the default age at which an open waiting PR
// enters the owner digest (task contract: open >7d).
const PRAgingThresholdDays = 7

// PRAgingInterval is the weekly digest cadence.
const PRAgingInterval = 7 * 24 * time.Hour

// PRWaitingLedger is the durable store of waiting-on-human-merge PRs,
// backed by a single JSON file under HYDRA_HOME. nil / empty path = inert.
type PRWaitingLedger struct {
	path string
}

// NewPRWaitingLedger creates the ledger. An empty home disables it (dev
// runs, tests without HYDRA_HOME).
func NewPRWaitingLedger(home string) *PRWaitingLedger {
	if home == "" {
		return nil
	}
	return &PRWaitingLedger{path: filepath.Join(home, "pr-waiting.json")}
}

// Record upserts a record keyed by bead ID (a repeated waiting close of the
// same bead refreshes URL/since instead of duplicating).
func (l *PRWaitingLedger) Record(rec PRWaitingRecord) {
	if l == nil || rec.BeadID == "" || rec.URL == "" {
		return
	}
	recs := l.list()
	for i, r := range recs {
		if r.BeadID == rec.BeadID {
			recs[i] = rec
			l.save(recs)
			return
		}
	}
	recs = append(recs, rec)
	l.save(recs)
}

// List returns all records (empty when the ledger is inert or unreadable).
func (l *PRWaitingLedger) List() []PRWaitingRecord { return l.list() }

// Remove drops the record for beadID (e.g. the PR landed or was closed).
func (l *PRWaitingLedger) Remove(beadID string) {
	if l == nil || beadID == "" {
		return
	}
	recs := l.list()
	kept := recs[:0]
	for _, r := range recs {
		if r.BeadID != beadID {
			kept = append(kept, r)
		}
	}
	l.save(kept)
}

func (l *PRWaitingLedger) list() []PRWaitingRecord {
	if l == nil {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil // missing file = empty ledger, first Record creates it
	}
	var recs []PRWaitingRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		log.Printf("[PRAging] ledger %s unreadable, starting empty (fail open): %v", l.path, err)
		return nil
	}
	return recs
}

func (l *PRWaitingLedger) save(recs []PRWaitingRecord) {
	if l == nil {
		return
	}
	if recs == nil {
		recs = []PRWaitingRecord{}
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err == nil {
		err = os.WriteFile(l.path, data, 0o644)
	}
	if err != nil {
		log.Printf("[PRAging] ledger save failed (fail open): %v", err)
	}
}

// recordPRWaiting appends a waiting-on-human-merge PR to the ledger. Wired
// by the overseer onto the bead closer; nil-safe so tests and tools skip it.
func (o *Overseer) recordPRWaiting(rec PRWaitingRecord) {
	o.prWaiting.Record(rec)
}

// prStateProbeFn is the live-state probe, overridable in tests.
var prStateProbeFn = func(url, token string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", url, "--json", "state", "--jq", ".state")
	if token != "" {
		cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PRAgingScanner produces the weekly per-owner digest of open waiting PRs.
type PRAgingScanner struct {
	ledger      *PRWaitingLedger
	getProjects func() []types.Project
	isAutoMerge func(projectPath string) bool
	days        int           // aging threshold in days (<=0 disables)
	interval    time.Duration // digest cadence
	events      *EventSink    // nil-safe
	stopChan    chan struct{}
}

// NewPRAgingScanner creates the weekly PR-aging sweeper. days <= 0 disables.
func NewPRAgingScanner(ledger *PRWaitingLedger, getProjects func() []types.Project, isAutoMerge func(string) bool, days int, events *EventSink) *PRAgingScanner {
	return &PRAgingScanner{
		ledger:      ledger,
		getProjects: getProjects,
		isAutoMerge: isAutoMerge,
		days:        days,
		interval:    PRAgingInterval,
		events:      events,
		stopChan:    make(chan struct{}),
	}
}

// Start launches the weekly scan loop. No-op when disabled.
func (s *PRAgingScanner) Start() {
	if s == nil || s.days <= 0 || s.ledger == nil {
		return
	}
	log.Printf("[PRAging] 📋 PR aging digest enabled (threshold: %dd, cadence: %v)", s.days, s.interval)
	go s.run()
}

// Stop halts the scan loop.
func (s *PRAgingScanner) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

func (s *PRAgingScanner) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.scan() // once at startup so a wedged queue surfaces without waiting a week
	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

type prAgingEntry struct {
	BeadID string `json:"bead_id"`
	URL    string `json:"url"`
	Age    string `json:"age"`
	Class  string `json:"class"`
}

// scan reconciles the ledger and emits one pr_aging_digest event per owner.
func (s *PRAgingScanner) scan() {
	if s == nil || s.ledger == nil || s.days <= 0 {
		return
	}
	threshold := time.Duration(s.days) * 24 * time.Hour
	byOwner := map[string][]prAgingEntry{}
	var landed []string
	for _, rec := range s.ledger.List() {
		id, err := resolvePublishIdentityFor(rec.ProjectPath)
		if err != nil {
			id = PublishIdentity{Owner: rec.Owner}
		}
		state, err := prStateProbeFn(rec.URL, id.Token)
		if err != nil {
			log.Printf("[PRAging] state probe for %s failed (fail open): %v", rec.BeadID, err)
			continue
		}
		if state == "MERGED" || state == "CLOSED" {
			landed = append(landed, rec.BeadID)
			continue
		}
		age := time.Since(rec.Since)
		if age <= threshold {
			continue // young PR, still within the normal merge window
		}
		class := "t2-human-merge-queue"
		if s.isAutoMerge != nil && s.isAutoMerge(rec.ProjectPath) {
			class = "stuck-auto-merge"
		}
		byOwner[id.Owner] = append(byOwner[id.Owner], prAgingEntry{
			BeadID: rec.BeadID, URL: rec.URL,
			Age:   age.Round(time.Hour).String(),
			Class: class,
		})
	}
	for _, id := range landed {
		s.ledger.Remove(id)
	}

	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		entries := byOwner[owner]
		stuck, queue := 0, 0
		for _, e := range entries {
			if e.Class == "stuck-auto-merge" {
				stuck++
			} else {
				queue++
			}
		}
		detail := fmt.Sprintf("%d integration PR(s) open >%dd awaiting merge (stuck-auto-merge: %d, t2-human-merge-queue: %d): %s",
			len(entries), s.days, stuck, queue, owner)
		log.Printf("[PRAging] digest for %s: %s", owner, detail)
		if s.events != nil {
			s.events.Emit(Event{
				Type:   "pr_aging_digest",
				Reason: "open_waiting_prs",
				Detail: detail,
				Fields: map[string]any{
					"owner":             owner,
					"threshold_days":    s.days,
					"stuck_auto_merge":  stuck,
					"human_merge_queue": queue,
					"prs":               entries,
				},
			})
		}
	}
}
