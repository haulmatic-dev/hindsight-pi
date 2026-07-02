package core

import "testing"

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
