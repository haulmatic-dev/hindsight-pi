package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestGapFingerprint_StableAndOrderIndependent(t *testing.T) {
	a := gap{Category: "route", Description: "missing /v1/foo", PRDRef: "FR-1", TargetFiles: []string{"a.go", "b.go"}}
	b := gap{Category: "route", Description: "missing /v1/foo", PRDRef: "FR-1", TargetFiles: []string{"b.go", "a.go"}} // reordered
	c := gap{Category: "route", Description: "different gap", PRDRef: "FR-1", TargetFiles: []string{"a.go", "b.go"}}

	if gapFingerprint(a) != gapFingerprint(b) {
		t.Fatalf("fingerprint should be independent of target-file order")
	}
	if gapFingerprint(a) == gapFingerprint(c) {
		t.Fatalf("different descriptions should fingerprint differently")
	}
}

func TestParseAdjudication(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"clean", `{"verdict":"false_positive","confidence":0.9,"reasoning":"already at HEAD"}`, "false_positive"},
		{"trailing-prose-before", "Analysis...\nDecision:\n{\"verdict\":\"real\",\"confidence\":0.8}", "real"},
		{"uppercase", `{"verdict":"AMBIGUOUS","confidence":0.5}`, "ambiguous"},
		{"garbage", "the model rambled with no json", "real"},
		{"empty", "", "real"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAdjudication(tc.out).Verdict; got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSameStringSet(t *testing.T) {
	x := map[string]bool{"a": true, "b": true}
	y := map[string]bool{"b": true, "a": true}
	z := map[string]bool{"a": true, "c": true}
	if !sameStringSet(x, y) {
		t.Fatalf("equal sets should compare equal")
	}
	if sameStringSet(x, z) {
		t.Fatalf("different sets should compare unequal")
	}
	if sameStringSet(x, map[string]bool{"a": true}) {
		t.Fatalf("different sizes should compare unequal")
	}
}

func TestPriorVerdictQuery(t *testing.T) {
	g := gap{Category: "route", Description: "missing /v1/foo endpoint. also handler."}
	want := "route missing /v1/foo endpoint"
	if got := priorVerdictQuery(g); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestPriorVerdictsForGap_FailOpen(t *testing.T) {
	g := gap{ID: "g1", Category: "route", Description: "missing /v1/foo"}

	// nil overseer
	cm := &ComplianceCycleManager{}
	if got := cm.priorVerdictsForGap(g); got != nil {
		t.Fatalf("nil overseer should return nil, got %v", got)
	}

	// disabled client
	cm.overseer = &Overseer{hindsightClient: &stubHindsight{enabled: false}}
	if got := cm.priorVerdictsForGap(g); got != nil {
		t.Fatalf("disabled client should return nil, got %v", got)
	}

	// client returns error
	cm.overseer = &Overseer{hindsightClient: &stubHindsight{enabled: true, err: errors.New("down")}}
	if got := cm.priorVerdictsForGap(g); got != nil {
		t.Fatalf("error should return nil, got %v", got)
	}
}

func TestPriorVerdictsForGap_Results(t *testing.T) {
	g := gap{ID: "g1", Category: "route", Description: "missing /v1/foo"}
	cm := &ComplianceCycleManager{
		overseer: &Overseer{
			hindsightClient: &stubHindsight{
				enabled: true,
				results: []hindsight.Memory{
					{Title: "false_positive", Content: "already implemented"},
					{Title: "real", Content: "needs change"},
				},
			},
		},
	}
	got := cm.priorVerdictsForGap(g)
	if len(got) != 2 {
		t.Fatalf("want 2 prior verdicts, got %d", len(got))
	}
}

func TestBuildAdjudicationPrompt_PriorVerdicts(t *testing.T) {
	g := gap{ID: "g1", Category: "route", Severity: "high", Description: "missing /v1/foo endpoint."}
	prior := []hindsight.Memory{
		{Title: "false_positive: route gap", Content: "Already implemented in main.go:42."},
		{Title: "real: route gap", Content: "Needed handler."},
	}
	prompt := buildAdjudicationPrompt(g, prior...)

	if !strings.Contains(prompt, "## Prior verdicts on similar gaps") {
		t.Fatalf("prompt missing prior verdicts section\n%s", prompt)
	}
	sectionStart := strings.Index(prompt, "## Prior verdicts")
	section := prompt[sectionStart:]
	if strings.Count(section, "\n- ") != 2 {
		t.Fatalf("want 2 prior verdict lines, got %d\n%s", strings.Count(section, "\n- "), prompt)
	}
	jsonLine := strings.Index(prompt, "End your reply with ONE JSON object")
	if sectionStart < 0 || jsonLine < 0 || sectionStart > jsonLine {
		t.Fatalf("prior verdicts section must appear before the JSON output contract")
	}
}

func TestBuildAdjudicationPrompt_NoPriorIsByteIdentical(t *testing.T) {
	g := gap{ID: "g2", Category: "auth", Severity: "medium", Description: "missing RBAC check"}
	want := buildAdjudicationPrompt(g)
	got := buildAdjudicationPrompt(g)
	if want != got {
		t.Fatalf("zero-prior prompt should be byte-identical to the single-argument form\nwant:\n%s\ngot:\n%s", want, got)
	}
	if strings.Contains(want, "## Prior verdicts") {
		t.Fatalf("zero-prior prompt should not contain prior verdicts section")
	}
}

type stubHindsight struct {
	enabled bool
	results []hindsight.Memory
	err     error
}

func (s *stubHindsight) Search(query, beadType string, limit int) (*hindsight.SearchResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &hindsight.SearchResponse{Success: true, Memories: s.results}, nil
}

func (s *stubHindsight) SearchByBead(title, beadType string) (*hindsight.SearchResponse, error) {
	return nil, nil
}

func (s *stubHindsight) Reflect(beadID types.BeadID, context map[string]interface{}) ([]hindsight.ReflectionInsight, error) {
	return nil, nil
}

func (s *stubHindsight) Retain(memory *hindsight.ExecutionMemory) (*hindsight.RetentionResult, error) {
	return nil, nil
}

func (s *stubHindsight) IsEnabled() bool { return s.enabled }
