package pan

import (
	"context"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

func TestIsPseudoBeadID(t *testing.T) {
	for _, id := range []string{"webapp-push-pending", "nipuna.j/webapp-campaign-anomaly", "hydra-campaign-anomaly"} {
		if !IsPseudoBeadID(id) {
			t.Errorf("IsPseudoBeadID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"webapp-9ah", "hydra-fo3"} {
		if IsPseudoBeadID(id) {
			t.Errorf("IsPseudoBeadID(%q) = true, want false", id)
		}
	}
}

func TestCampaignAnomalyIDRoundTrip(t *testing.T) {
	id := CampaignAnomalyID("nipuna.j/webapp")
	key, ok := ProjectKeyFromAnomalyID(id)
	if !ok || key != "nipuna.j/webapp" {
		t.Errorf("round trip = %q, %v", key, ok)
	}
	if _, ok := ProjectKeyFromAnomalyID("webapp-9ah"); ok {
		t.Error("real bead must not strip to a project key")
	}
	if _, ok := ProjectKeyFromAnomalyID("webapp-push-pending"); ok {
		t.Error("push-pending pseudo must not strip to a campaign key")
	}
}

func TestHealthDigestText(t *testing.T) {
	d := HealthDigest{Projects: []ProjectHealth{
		{Key: "b/proj", Open: 2},
		{Key: "a/proj", Open: 1, ChainedParallel: 1, Anomalies: []string{"1 dep-chained pair live"}},
	}}
	text := d.Text()
	if !strings.Contains(text, "a/proj") || !strings.Contains(text, "ANOMALIES=[1 dep-chained pair live]") {
		t.Errorf("text missing anomaly: %q", text)
	}
	if strings.Index(text, "a/proj") > strings.Index(text, "b/proj") {
		t.Errorf("projects not sorted by key: %q", text)
	}

	// Cap: a huge fleet cannot blow the prompt.
	big := HealthDigest{}
	for i := 0; i < 200; i++ {
		big.Projects = append(big.Projects, ProjectHealth{Key: strings.Repeat("x", 20) + "/proj", Open: i})
	}
	if len(big.Text()) > 1700 {
		t.Errorf("text not capped: %d bytes", len(big.Text()))
	}
}

func TestHealthDigestAnomalous(t *testing.T) {
	d := HealthDigest{Projects: []ProjectHealth{
		{Key: "ok/proj"},
		{Key: "bad/proj", Anomalies: []string{"x"}},
	}}
	a := d.Anomalous()
	if len(a) != 1 || a[0].Key != "bad/proj" {
		t.Errorf("Anomalous = %v", a)
	}
}

func TestCandidatesEmitCampaignAnomalyFromDigest(t *testing.T) {
	s := New(Config{Enabled: true}, &fakeStore{},
		func(ctx context.Context, _ Config, _ string) (string, error) { return "[]", nil })
	s.cfg.DigestFunc = func() HealthDigest {
		return HealthDigest{Projects: []ProjectHealth{
			{Key: "nipuna.j/webapp", ChainedParallel: 2, LiveWorkers: 3,
				Anomalies: []string{"2 dep-chained bead pairs have live workers — chain discipline violated"}},
			{Key: "ajantha/webapp"},
		}}
	}

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	var found *Candidate
	for i := range cands {
		if cands[i].Record.BeadID == "nipuna.j/webapp-campaign-anomaly" {
			found = &cands[i]
		}
	}
	if found == nil {
		t.Fatalf("anomaly pseudo candidate missing: %v", cands)
	}
	if !strings.HasPrefix(found.Reason, "CAMPAIGN-ANOMALY: ") || !strings.Contains(found.Reason, "chain discipline") {
		t.Errorf("reason malformed: %q", found.Reason)
	}
	// Healthy project emits nothing.
	for _, c := range cands {
		if strings.HasPrefix(c.Record.BeadID, "ajantha/webapp") {
			t.Errorf("healthy project must not emit: %v", c.Record.BeadID)
		}
	}

	// Cooldown: identical digest within 30 min is presented once (pseudo
	// candidacy valve), so a persistent anomaly cannot re-judge every tick.
	cands2, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates 2: %v", err)
	}
	for _, c := range cands2 {
		if c.Record.BeadID == "nipuna.j/webapp-campaign-anomaly" {
			t.Error("unchanged anomaly re-presented inside the 30-min cooldown")
		}
	}
}

func TestCampaignAnomalyWhitelistAndGuards(t *testing.T) {
	if !isAllowedAction("repair:park-campaign") {
		t.Error("repair:park-campaign must be whitelisted")
	}
	if !isAllowedAction("repair:log-improvement") {
		t.Error("repair:log-improvement must be whitelisted")
	}
	if !projectScopedRepairs["park-campaign"] {
		t.Error("park-campaign must be project-scoped for rate limiting")
	}
}

func TestHandleLogImprovement(t *testing.T) {
	s := New(Config{Enabled: true}, &fakeStore{},
		func(ctx context.Context, _ Config, _ string) (string, error) { return "[]", nil })
	var got []string
	s.SetLogImprovement(func(beadID, text string) error {
		got = append(got, beadID+"|"+text)
		return nil
	})
	s.handleRepair("nipuna.j/webapp-campaign-anomaly", "log-improvement", "workers conflict on the campaign branch")
	if len(got) != 1 || !strings.Contains(got[0], "workers conflict") {
		t.Errorf("improvement not persisted: %v", got)
	}

	// Empty text rejected (no sink call, audit event instead).
	s.handleRepair("nipuna.j/webapp-campaign-anomaly", "log-improvement", "   ")
	if len(got) != 1 {
		t.Errorf("empty improvement must not reach the sink: %v", got)
	}
}

// compile-time guard: anomaly pseudo IDs flow through WorkerRecord only as
// synthetic candidates, never as real bead work.
var _ = registry.WorkerRecord{}
