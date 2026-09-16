package pan

import (
	"fmt"
	"sort"
	"strings"
)

// campaignAnomalySuffix marks synthetic per-project candidates emitted by the
// fleet health digest when a project's anomaly flags fire (chained-parallel
// execution, stranded worktrees with no live workers). Like "-push-pending"
// they are not real beads: repair-only verdicts apply.
const campaignAnomalySuffix = "-campaign-anomaly"

// IsPseudoBeadID reports whether id names a synthetic project/fleet-level
// candidate rather than a real bead. Pseudo candidates accept repair:<name>
// verdicts only — close/abandon/worker commands can never succeed for them
// (2026-09-14: Pan abandoned webapp-push-pending every tick; each abandon
// failed and the judgment repeated forever).
func IsPseudoBeadID(id string) bool {
	return strings.HasSuffix(id, pseudoBeadSuffix) || strings.HasSuffix(id, campaignAnomalySuffix)
}

// CampaignAnomalyID returns the pseudo candidate ID for a project key.
func CampaignAnomalyID(projectKey string) string { return projectKey + campaignAnomalySuffix }

// ProjectKeyFromAnomalyID strips the anomaly suffix; ok=false when id is not
// a campaign-anomaly pseudo ID. park-campaign resolves its target project
// from this key.
func ProjectKeyFromAnomalyID(id string) (string, bool) {
	if !strings.HasSuffix(id, campaignAnomalySuffix) {
		return "", false
	}
	return strings.TrimSuffix(id, campaignAnomalySuffix), true
}

// ProjectHealth is one registered scan target's fleet glance. Counts only —
// no ages, no timestamps: volatile fields would make every digest look
// changed, defeat judgment dedup, and burn MiniMax calls on a stable fleet
// (PR #236 reason-normalization lesson, applied at the source).
type ProjectHealth struct {
	Key                  string // stable display key ("nipuna.j/webapp"); basis of the pseudo ID
	Open                 int
	InProgress           int
	DepBlocked           int
	LiveWorkers          int
	ChainedParallel      int // live-worker pairs whose beads are dep-related
	StrandedWorktrees    int // worktree dirs whose bead is terminal
	UnintegratedBranches int // hydra/wt-* branch count (informational)
	Anomalies            []string
}

// HealthDigest is the daemon-collected fleet glance: Pan's eyes. The
// collector runs every tick (cheap: bead lists already fetched, registry
// rows in memory, one readdir + one git call per project); Pan itself is
// only woken when a project carries an anomaly, so a healthy fleet costs
// zero tokens and zero shell noise.
type HealthDigest struct {
	Projects []ProjectHealth
}

// Anomalous returns the projects carrying at least one anomaly flag.
func (d HealthDigest) Anomalous() []ProjectHealth {
	var out []ProjectHealth
	for _, p := range d.Projects {
		if len(p.Anomalies) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// Text renders the compact prompt block: one line per project, stable field
// order, sorted by key. Capped so a large fleet cannot blow the prompt.
func (d HealthDigest) Text() string {
	const maxLen = 1500
	projs := append([]ProjectHealth(nil), d.Projects...)
	sort.Slice(projs, func(i, j int) bool { return projs[i].Key < projs[j].Key })
	var b strings.Builder
	for _, p := range projs {
		fmt.Fprintf(&b, "%s: open=%d inprog=%d depblocked=%d workers=%d chainedpairs=%d strandedwt=%d wtbranches=%d",
			p.Key, p.Open, p.InProgress, p.DepBlocked, p.LiveWorkers,
			p.ChainedParallel, p.StrandedWorktrees, p.UnintegratedBranches)
		if len(p.Anomalies) > 0 {
			fmt.Fprintf(&b, " ANOMALIES=[%s]", strings.Join(p.Anomalies, "; "))
		}
		b.WriteString("\n")
	}
	s := b.String()
	if len(s) > maxLen {
		s = s[:maxLen] + "\n...[truncated]"
	}
	return s
}
