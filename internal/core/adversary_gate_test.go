package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func setupAdversaryCloser(t *testing.T) (*BeadCloser, *mockBeadsClient, string) {
	t.Helper()
	client := newMockBeadsClient()
	cfg := &Config{}
	cfg.Validation.MaxRounds = 3
	cfg.Validation.AgentVerifier = AgentVerifierConfig{Enabled: true}
	rm := NewRetryManager(time.Millisecond, time.Millisecond)
	vs := NewValidationService(cfg, rm, client)
	eventsDir := t.TempDir()
	sink := NewEventSink(eventsDir)
	closer := NewBeadCloser(client, nil, nil, vs, cfg)
	closer.SetEventSink(sink)
	return closer, client, eventsDir
}

func stubAdversary(t *testing.T, output string, runErr error) {
	t.Helper()
	prevR, prevD := adversaryRunner, adversaryDiffFn
	adversaryRunner = func(_ context.Context, _ AgentVerifierConfig, _, _ string) (string, error) {
		return output, runErr
	}
	adversaryDiffFn = func(_, _ string, _ int) (string, string, error) {
		return " 1 file changed", "diff --git a/x b/x", nil
	}
	t.Cleanup(func() { adversaryRunner, adversaryDiffFn = prevR, prevD })
}

func adversaryTestBead() *types.Bead {
	return &types.Bead{ID: "adv-1", Title: "Implement thing", Type: "implement", Description: "AC: thing works"}
}

func sinkEvents(t *testing.T, eventsDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(eventsDir, e.Name()))
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var ev map[string]any
			if json.Unmarshal([]byte(line), &ev) == nil {
				if ty, ok := ev["type"].(string); ok {
					out = append(out, ty)
				}
			}
		}
	}
	return out
}

func TestAdversaryGate_DisabledOrClassSkipped(t *testing.T) {
	closer, _, _ := setupAdversaryCloser(t)
	closer.config.Validation.AgentVerifier.Enabled = false
	if err := closer.verifyAdversarial(adversaryTestBead(), "/p", "r", "/p", "development"); err != nil {
		t.Fatalf("disabled gate must pass: %v", err)
	}
	closer.config.Validation.AgentVerifier.Enabled = true
	b := adversaryTestBead()
	b.Type = "test" // not in default classes
	if err := closer.verifyAdversarial(b, "/p", "r", "/p", "development"); err != nil {
		t.Fatalf("non-gated class must pass: %v", err)
	}
}

func TestAdversaryGate_PassShipsFirstGo(t *testing.T) {
	closer, _, eventsDir := setupAdversaryCloser(t)
	stubAdversary(t, `{"score": 0.9, "findings": [{"class":"nit","summary":"naming","citation":""}]}`, nil)
	adversaryChurnMu.Lock()
	delete(adversaryChurn, "adv-1")
	adversaryChurnMu.Unlock()
	if err := closer.verifyAdversarial(adversaryTestBead(), "/p", "r", "/p", "development"); err != nil {
		t.Fatalf("clean verdict must ship: %v", err)
	}
	evs := sinkEvents(t, eventsDir)
	found := false
	for _, e := range evs {
		if e == "agent_verdict" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected agent_verdict event, got %v", evs)
	}
}

func TestAdversaryGate_BlockerRejectsAndInjectsFeedback(t *testing.T) {
	closer, client, _ := setupAdversaryCloser(t)
	stubAdversary(t, `{"score": 0.4, "findings": [{"class":"blocker","summary":"AC unmet","citation":"criterion 2"}]}`, nil)
	adversaryChurnMu.Lock()
	delete(adversaryChurn, "adv-1")
	adversaryChurnMu.Unlock()
	b := adversaryTestBead()
	client.beads["adv-1"] = *b
	if err := closer.verifyAdversarial(b, "/p", "r", "/p", "development"); err == nil {
		t.Fatal("blocker must reject the close")
	}
	// Feedback must reach the worker: description carries the findings.
	if !strings.Contains(client.beads["adv-1"].Description, "Adversary close-gate feedback") {
		t.Fatalf("feedback not injected, description: %q", client.beads["adv-1"].Description)
	}
}

func TestAdversaryGate_ChurnTripwireShips(t *testing.T) {
	closer, _, eventsDir := setupAdversaryCloser(t)
	stubAdversary(t, `{"score": 0.4, "findings": [{"class":"blocker","summary":"new finding each round","citation":"x"}]}`, nil)
	// Simulate a prior round with a DIFFERENT blocker set.
	adversaryChurnMu.Lock()
	adversaryChurn["adv-1"] = "different-signature"
	adversaryChurnMu.Unlock()
	t.Cleanup(func() {
		adversaryChurnMu.Lock()
		delete(adversaryChurn, "adv-1")
		adversaryChurnMu.Unlock()
	})
	if err := closer.verifyAdversarial(adversaryTestBead(), "/p", "r", "/p", "development"); err != nil {
		t.Fatalf("churn must ship with dissent, not reject: %v", err)
	}
	for _, e := range sinkEvents(t, eventsDir) {
		if e == "adversary_churn" {
			return
		}
	}
	t.Fatal("expected adversary_churn event")
}

func TestAdversaryGate_VerifierErrorFailsOpen(t *testing.T) {
	closer, _, eventsDir := setupAdversaryCloser(t)
	stubAdversary(t, "", context.DeadlineExceeded)
	if err := closer.verifyAdversarial(adversaryTestBead(), "/p", "r", "/p", "development"); err != nil {
		t.Fatalf("verifier error must fail open: %v", err)
	}
	for _, e := range sinkEvents(t, eventsDir) {
		if e == "adversary_error" {
			return
		}
	}
	t.Fatal("expected adversary_error event")
}

func TestParseAdversaryVerdict(t *testing.T) {
	// Raw JSON.
	if v, err := parseAdversaryVerdict(`{"score":0.9,"findings":[]}`); err != nil || v.Score != 0.9 {
		t.Fatalf("raw parse: %v %v", v, err)
	}
	// Prose + fenced block.
	fenced := "Here is my review.\n```json\n{\"score\": 0.5, \"findings\": [{\"class\":\"nit\",\"summary\":\"s\",\"citation\":\"\"}]}\n```\nThanks."
	v, err := parseAdversaryVerdict(fenced)
	if err != nil || v.Score != 0.5 || len(v.Findings) != 1 {
		t.Fatalf("fenced parse: %+v %v", v, err)
	}
	// Garbage.
	if _, err := parseAdversaryVerdict("no json here"); err == nil {
		t.Fatal("garbage must error")
	}
}

func TestAdversaryBlockerSignatureOrderStable(t *testing.T) {
	a := []adversaryFinding{{Class: "blocker", Summary: "one"}, {Class: "blocker", Summary: "two"}}
	b := []adversaryFinding{{Class: "blocker", Summary: "TWO"}, {Class: "blocker", Summary: "one"}}
	if adversaryBlockerSignature(a) != adversaryBlockerSignature(b) {
		t.Fatal("signature must be order- and case-stable")
	}
}
