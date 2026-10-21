package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// readVerificationSubstantiveEvents reads every JSONL event file under the
// central sink dir and returns the raw lines whose "type" is
// "verification_substantive".
func readVerificationSubstantiveEvents(t *testing.T, centralDir string) []string {
	t.Helper()
	var found []string
	entries, err := os.ReadDir(centralDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadDir(centralDir): %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(centralDir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", e.Name(), err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.Contains(line, `"type":"verification_substantive"`) {
				found = append(found, line)
			}
		}
	}
	return found
}

func TestTrackVerificationFingerprint_FirstOccurrence_AddsLabel(t *testing.T) {
	mockBeads := newMockBeadsClient()
	bead := &types.Bead{ID: "fp-bead-1", Title: "t", Project: "proj", Labels: []string{}}
	mockBeads.beads[string(bead.ID)] = *bead

	sink := NewEventSink(t.TempDir())
	closer := NewBeadCloser(mockBeads, nil, nil, nil, DefaultConfig())
	closer.SetEventSink(sink)

	if err := closer.trackVerificationFingerprint(bead, "deadbeef", t.TempDir(), "proj"); err != nil {
		t.Fatalf("trackVerificationFingerprint() error = %v", err)
	}

	stored := mockBeads.beads[string(bead.ID)]
	want := gateFingerprintLabelPrefix + "deadbeef"
	if len(stored.Labels) != 1 || stored.Labels[0] != want {
		t.Fatalf("expected exactly one label %q on the bead, got %v", want, stored.Labels)
	}
	if events := readVerificationSubstantiveEvents(t, sink.centralDir); len(events) != 0 {
		t.Fatalf("first occurrence must not emit verification_substantive, got %d event(s): %v", len(events), events)
	}
}

func TestTrackVerificationFingerprint_Repeat_EmitsSubstantiveEvent(t *testing.T) {
	mockBeads := newMockBeadsClient()
	label := gateFingerprintLabelPrefix + "cafe1234"
	bead := &types.Bead{ID: "fp-bead-2", Title: "t", Project: "proj", Labels: []string{label}}
	mockBeads.beads[string(bead.ID)] = *bead

	sink := NewEventSink(t.TempDir())
	closer := NewBeadCloser(mockBeads, nil, nil, nil, DefaultConfig())
	closer.SetEventSink(sink)

	if err := closer.trackVerificationFingerprint(bead, "cafe1234", t.TempDir(), "proj"); err != nil {
		t.Fatalf("trackVerificationFingerprint() error = %v", err)
	}

	events := readVerificationSubstantiveEvents(t, sink.centralDir)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 verification_substantive event, got %d", len(events))
	}
	for _, want := range []string{
		`"bead":"fp-bead-2"`,
		`"fingerprint":"cafe1234"`,
		`"count":2`,
	} {
		if !strings.Contains(events[0], want) {
			t.Fatalf("verification_substantive event missing %s: %s", want, events[0])
		}
	}

	stored := mockBeads.beads[string(bead.ID)]
	if len(stored.Labels) != 1 || stored.Labels[0] != label {
		t.Fatalf("repeat occurrence must not duplicate the gate-fp label, got %v", stored.Labels)
	}
}

func TestTrackVerificationFingerprint_DifferentFingerprint_GetsOwnLabel(t *testing.T) {
	mockBeads := newMockBeadsClient()
	bead := &types.Bead{ID: "fp-bead-3", Title: "t", Project: "proj", Labels: []string{"gate-fp-11111111"}}
	mockBeads.beads[string(bead.ID)] = *bead

	sink := NewEventSink(t.TempDir())
	closer := NewBeadCloser(mockBeads, nil, nil, nil, DefaultConfig())
	closer.SetEventSink(sink)

	if err := closer.trackVerificationFingerprint(bead, "22222222", t.TempDir(), "proj"); err != nil {
		t.Fatalf("trackVerificationFingerprint() error = %v", err)
	}

	stored := mockBeads.beads[string(bead.ID)]
	if len(stored.Labels) != 2 {
		t.Fatalf("expected 2 labels after new fingerprint, got %v", stored.Labels)
	}
	if !containsLabel(stored.Labels, "gate-fp-22222222") {
		t.Fatalf("expected new label gate-fp-22222222, got %v", stored.Labels)
	}
	if events := readVerificationSubstantiveEvents(t, sink.centralDir); len(events) != 0 {
		t.Fatalf("a different fingerprint must not emit verification_substantive, got %d", len(events))
	}
}

func TestTrackVerificationFingerprint_NilClient_NoPanic(t *testing.T) {
	closer := NewBeadCloser(nil, nil, nil, nil, DefaultConfig())
	bead := &types.Bead{ID: "fp-bead-4", Title: "t", Project: "proj"}
	if err := closer.trackVerificationFingerprint(bead, "deadbeef", t.TempDir(), "proj"); err != nil {
		t.Fatalf("nil beadsClient must be a no-op returning nil, got %v", err)
	}
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestGetVerificationFingerprint_IsDeterministicAndCommandSensitive(t *testing.T) {
	mk := func(id, cmd, reason string) *types.Bead {
		return &types.Bead{
			ID:       types.BeadID(id),
			Title:    "t",
			Project:  "proj",
			Metadata: map[string]string{"test_command": cmd},
		}
	}
	a := getVerificationFingerprint(mk("b1", "go test ./...", "verification-failed"), "verification-failed")
	b := getVerificationFingerprint(mk("b1", "go test ./...", "verification-failed"), "verification-failed")
	if a != b {
		t.Fatalf("same inputs must give the same fingerprint: %s vs %s", a, b)
	}
	if len(a) != 8 {
		t.Fatalf("fingerprint must be 8 hex chars, got %q", a)
	}
	c := getVerificationFingerprint(mk("b2", "go test ./...", "verification-failed"), "verification-failed")
	if a == c {
		t.Fatalf("different bead IDs must give different fingerprints")
	}
	d := getVerificationFingerprint(mk("b1", "npm test", "verification-failed"), "verification-failed")
	if a == d {
		t.Fatalf("different verification commands must give different fingerprints")
	}
	e := getVerificationFingerprint(mk("b1", "go test ./...", "timeout"), "timeout")
	if a == e {
		t.Fatalf("different reasons must give different fingerprints")
	}
}

func TestVerificationFingerprintSource_IgnoresStderr(t *testing.T) {
	// AC1: the fingerprint is hashed from the classification string (e.g.
	// "verification-command-failed"), never the literal stderr — same-
	// classification failures with different output must collide so a repeat
	// emits verification_substantive.
	same := verificationFingerprintSource("verification-command-failed", []string{"panic: foo", "exit status 1"})
	other := verificationFingerprintSource("verification-command-failed", []string{"panic: bar"})
	if same != "verification-command-failed" {
		t.Fatalf("fingerprint source must be the classification reason, got %q", same)
	}
	if same != other {
		t.Fatalf("different stderr must not change the fingerprint source: %q vs %q", same, other)
	}
	if got := verificationFingerprintSource("timeout", nil); got != "timeout" {
		t.Fatalf("nil stderr must fall back to reason, got %q", got)
	}
}
