package core

import (
	"os"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// --- P1a/P1b: maxGateFpRepeat classification ---

func TestMaxGateFpRepeat(t *testing.T) {
	tests := []struct {
		labels []string
		want   int
	}{
		{nil, 0},
		{[]string{"gate-failed"}, 0},
		{[]string{"gate-fp-abc12345-1"}, 1},
		{[]string{"gate-fp-abc12345-1", "gate-fp-abc12345-2"}, 2},
		{[]string{"gate-fp-abc12345-1", "gate-fp-def67890-3"}, 3},
		{[]string{"unrelated", "gate-fp-041ea3e9-1", "fr"}, 1},
	}
	for _, tt := range tests {
		if got := maxGateFpRepeat(tt.labels); got != tt.want {
			t.Errorf("maxGateFpRepeat(%v) = %d, want %d", tt.labels, got, tt.want)
		}
	}
}

// --- E1: reference verification ---

// superseded without a named bead = unverifiable = reject.
func TestVerifyEmptyCloseReference_NoReference(t *testing.T) {
	c := &BeadCloser{}
	if c.verifyEmptyCloseReference(&types.Bead{ID: "hydra-ab12", Project: "p"}, "repo", "superseded: work done elsewhere", acceptedEmptySuperseded) {
		t.Error("summary without a bead reference must be rejected")
	}
}

// Naming itself is not a reference.
func TestVerifyEmptyCloseReference_SelfReference(t *testing.T) {
	c := &BeadCloser{}
	if c.verifyEmptyCloseReference(&types.Bead{ID: "hydra-ab12", Project: "p"}, "repo", "superseded by hydra-ab12", acceptedEmptySuperseded) {
		t.Error("self-reference must be rejected")
	}
}

// acceptedEmptyReason still token-matches (regression: E1 layering did not
// break the base classification).
func TestAcceptedEmptyReasonUnchanged(t *testing.T) {
	v := &emptyGateVerdict{Done: true, Summary: "superseded by 1nxj: impl landed"}
	if got := acceptedEmptyReason(v, nil); got != acceptedEmptySuperseded {
		t.Errorf("base classification changed: %q", got)
	}
	v2 := &emptyGateVerdict{Done: false, Summary: "superseded"}
	if got := acceptedEmptyReason(v2, nil); got != "" {
		t.Errorf("not-done verdict must not honour the token, got %q", got)
	}
}

// --- S1: resolver disabled ---

func TestForceCompleteBeadDisabled(t *testing.T) {
	// The disabled resolver is in package graph; assert the refusal text
	// exists there via a source probe (cross-package call needs the full
	// resolver wiring — the disable is a hard error return, not a path).
	src, err := readSourceFile("../../graph/schema.resolvers.go")
	if err != nil {
		t.Skip("source layout differs:", err)
	}
	if !strings.Contains(src, "forceCompleteBead is disabled") {
		t.Error("S1: resolver disable text missing — mutation may be live")
	}
}

func readSourceFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// --- P1c: stay-cap decision (behavioral) ---

func TestStayAllowed(t *testing.T) {
	const cap = int64(1_000_000)
	cases := []struct {
		stays int
		fresh int64
		allow bool
		num   int
	}{
		{0, 2_100_000, true, 1},  // first stay
		{1, 2_200_000, true, 2},  // second stay
		{2, 2_300_000, false, 0}, // cap exhausted
		{1, 3_000_001, false, 0}, // absolute 3x ceiling
		{0, 3_000_000, true, 1},  // exactly 3x still allowed
	}
	for _, tc := range cases {
		a, n := stayAllowed(tc.stays, tc.fresh, cap)
		if a != tc.allow || n != tc.num {
			t.Errorf("stayAllowed(%d, %d) = (%v,%d), want (%v,%d)", tc.stays, tc.fresh, a, n, tc.allow, tc.num)
		}
	}
}

// --- E1: prefix derivation ---

func TestBeadRefPrefix(t *testing.T) {
	cases := []struct{ id, want string }{
		{"hydra-q1nxj", "hydra"},
		{"hydra-xw3", "hydra"},
		{"srv-telematics-ab12", "srv-telematics"},
		{"srv-telematics/fe-ab12", "srv-telematics/fe"},
		{"url-shortener-x9zz9", "url-shortener"},
		{"nosuffix", ""},
	}
	for _, tc := range cases {
		if got := beadRefPrefix(types.BeadID(tc.id)); got != tc.want {
			t.Errorf("beadRefPrefix(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
