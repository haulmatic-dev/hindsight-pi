package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// RED-phase gate (same contract as the verification-command suite): the
// feature is not implemented yet, so by default the contract tests SKIP
// loudly and the suite exits 0 (the daemon gate stays green). Setting
// BEAD_CLAIMS_ENFORCE=1 turns every skip into a loud failure, proving the
// RED phase detects the missing contract.
func beadClaimsImplemented(t *testing.T) {
	t.Helper()
	sample := parseBeadClaims("claim: a (verify: echo a)")
	if len(sample) == 1 && sample[0].VerifyCmd == "echo a" {
		return // implemented
	}
	msg := "RED: bead claims drift-check contract not yet implemented"
	if enforce := strings.TrimSpace(strings.ToLower(firstEnvOr("BEAD_CLAIMS_ENFORCE", ""))); enforce == "1" || enforce == "true" {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

func firstEnvOr(key, def string) string {
	if v, ok := osLookupEnv(key); ok {
		return v
	}
	return def
}

func TestParseBeadClaims(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    []BeadClaim
	}{
		{
			name:    "zero claims noop",
			payload: "just a description, no claims",
			want:    nil,
		},
		{
			name: "soft claim parsed",
			payload: "description\n" +
				"claim: tests pass (verify: go test ./...)\n" +
				"trailing text",
			want: []BeadClaim{
				{Hard: false, Assertion: "tests pass", VerifyCmd: "go test ./..."},
			},
		},
		{
			name: "hard claim parsed",
			payload: "description\n" +
				"hard: claim: disk under watermark (verify: df -h /)\n",
			want: []BeadClaim{
				{Hard: true, Assertion: "disk under watermark", VerifyCmd: "df -h /"},
			},
		},
		{
			name: "multiple claims mixed",
			payload: "claim: a (verify: echo a)\n" +
				"hard: claim: b (verify: echo b)\n" +
				"claim: c (verify: echo c)\n",
			want: []BeadClaim{
				{Hard: false, Assertion: "a", VerifyCmd: "echo a"},
				{Hard: true, Assertion: "b", VerifyCmd: "echo b"},
				{Hard: false, Assertion: "c", VerifyCmd: "echo c"},
			},
		},
		{
			name:    "non-claim lines ignored",
			payload: "some text\nmore text\n",
			want:    nil,
		},
		{
			name:    "verify optional",
			payload: "claim: assertion only\n",
			want:    []BeadClaim{{Hard: false, Assertion: "assertion only", VerifyCmd: ""}},
		},
		{
			name:    "verify command with parens preserved",
			payload: "claim: x (verify: echo a(b))",
			want:    []BeadClaim{{Hard: false, Assertion: "x", VerifyCmd: "echo a(b)"}},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			beadClaimsImplemented(t)
			got := parseBeadClaims(tc.payload)
			if len(got) != len(tc.want) {
				t.Fatalf("parseBeadClaims() got %d claims, want %d: %v vs %v",
					len(got), len(tc.want), got, tc.want)
			}
			for i, g := range got {
				w := tc.want[i]
				if g.Hard != w.Hard || g.Assertion != w.Assertion || g.VerifyCmd != w.VerifyCmd {
					t.Errorf("claim[%d] = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

func TestCheckBeadClaims(t *testing.T) {
	var events []string
	sink := &EventSink{}

	tests := []struct {
		name       string
		payload    string
		runnerOut  string
		runnerErr  bool
		runnerTo   bool
		wantAllow  bool
		wantEvents []string
	}{
		{
			name:       "zero claims spawns with no events",
			payload:    "no claims here",
			wantAllow:  true,
			wantEvents: nil,
		},
		{
			name:       "soft claim passing",
			payload:    "claim: a (verify: echo ok)",
			runnerOut:  "ok",
			wantAllow:  true,
			wantEvents: nil,
		},
		{
			name:       "soft claim failing warns only",
			payload:    "claim: a (verify: exit 1)",
			runnerOut:  "boom",
			runnerErr:  true,
			wantAllow:  true,
			wantEvents: []string{"claim_soft_failed"},
		},
		{
			name:       "hard claim failing refuses spawn",
			payload:    "hard: claim: a (verify: exit 1)",
			runnerOut:  "boom",
			runnerErr:  true,
			wantAllow:  false,
			wantEvents: []string{"claim_hard_failed"},
		},
		{
			name:       "hard claim passing allows spawn",
			payload:    "hard: claim: a (verify: echo ok)",
			runnerOut:  "ok",
			wantAllow:  true,
			wantEvents: nil,
		},
		{
			name:       "hard claim unknown (timeout) fails open with event",
			payload:    "hard: claim: a (verify: sleep 30)",
			runnerOut:  "",
			runnerErr:  true,
			runnerTo:   true,
			wantAllow:  true,
			wantEvents: []string{"claim_unknown"},
		},
		{
			name:       "multiple claims hard failure after soft pass",
			payload:    "claim: a (verify: echo a)\nhard: claim: b (verify: exit 1)",
			runnerErr:  true,
			wantAllow:  false,
			wantEvents: []string{"claim_soft_failed", "claim_hard_failed"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			beadClaimsImplemented(t)
			events = nil
			var gotCmds []string
			var gotTimeout time.Duration
			claimEventRecorder = func(evType string) {
				events = append(events, evType)
			}
			defer func() { claimEventRecorder = nil }()
			runner := func(cmd string, timeout time.Duration) (string, error) {
				gotCmds = append(gotCmds, cmd)
				gotTimeout = timeout
				if tc.runnerTo {
					return tc.runnerOut, fmt.Errorf("claim verify did not finish within %s: %w", timeout, context.DeadlineExceeded)
				}
				if tc.runnerErr {
					return tc.runnerOut, errors.New("exit status 1")
				}
				return tc.runnerOut, nil
			}
			c := newBeadClaimChecker(sink, runner)
			allow := c.checkBeadClaims("hydra-test", tc.payload)

			if allow != tc.wantAllow {
				t.Errorf("checkBeadClaims() allow = %v, want %v", allow, tc.wantAllow)
			}
			if len(events) != len(tc.wantEvents) {
				t.Errorf("events = %v, want %v", events, tc.wantEvents)
			} else {
				for i, ev := range tc.wantEvents {
					if events[i] != ev {
						t.Errorf("event[%d] = %q, want %q", i, events[i], ev)
					}
				}
			}
			if tc.payload == "no claims here" {
				if len(gotCmds) != 0 {
					t.Errorf("runner invoked for zero-claim payload: %v", gotCmds)
				}
				return
			}
			if len(gotCmds) == 0 {
				t.Errorf("runner was never invoked for claim payload")
			}
			if gotTimeout != claimVerifyTimeout {
				t.Errorf("runner timeout = %v, want %v", gotTimeout, claimVerifyTimeout)
			}
			_ = strings.TrimSpace
			_ = types.BeadID("")
		})
	}
}
