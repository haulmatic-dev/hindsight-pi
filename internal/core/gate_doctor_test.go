package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestParseGateDoctorClass_ValidClasses(t *testing.T) {
	for _, tc := range []struct {
		class  GateFailureClass
		reason string
	}{
		{GateClassEnvironment, "npm ci missing"},
		{GateClassCodeBroken, "nil deref in handler"},
		{GateClassStaleGate, "obsolete assertion"},
		{GateClassFlaky, "timing race"},
		{GateClassUncertain, "ambiguous output"},
	} {
		in := fmt.Sprintf(`{"class":"%s","reasoning":"%s"}`, tc.class, tc.reason)
		got, reasoning := ParseGateDoctorClass(in)
		if got != tc.class {
			t.Errorf("class = %q, want %q", got, tc.class)
		}
		if reasoning != tc.reason {
			t.Errorf("reasoning = %q, want %q", reasoning, tc.reason)
		}
	}
}

func TestParseGateDoctorClass_MarkdownFence(t *testing.T) {
	in := "```json\n{\"class\":\"environment\",\"reasoning\":\"node_modules missing\"}\n```"
	class, reasoning := ParseGateDoctorClass(in)
	if class != GateClassEnvironment {
		t.Errorf("class = %q, want environment", class)
	}
	if reasoning != "node_modules missing" {
		t.Errorf("reasoning = %q", reasoning)
	}
}

func TestParseGateDoctorClass_FailClosed(t *testing.T) {
	for _, in := range []string{"", "garbage", "{\"class\":\"nonsense\"}", "{\"reasoning\":\"only\"}"} {
		class, _ := ParseGateDoctorClass(in)
		if class != GateClassUncertain {
			t.Errorf("ParseGateDoctorClass(%q) = %q, want uncertain (fail-closed)", in, class)
		}
	}
}

func TestParseGateDoctorClass_TolerantWithoutBraces(t *testing.T) {
	// Model output without valid JSON braces but mentioning a class token.
	class, _ := ParseGateDoctorClass("The failure is clearly \"code-broken\": test assertions failed.")
	if class != GateClassCodeBroken {
		t.Errorf("class = %q, want code-broken (tolerant)", class)
	}
}

func TestGateDoctorPrompt_ContainsContext(t *testing.T) {
	p := GateDoctorPrompt("bead-x", "unit-tests", 1, "FAIL: something broke")
	for _, want := range []string{"bead-x", "unit-tests", "Exit code: 1", "FAIL: something broke", "stale-gate", "fail-closed"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestGateDoctorTriage_Classes(t *testing.T) {
	for _, tc := range []struct {
		class  GateFailureClass
		action GateDoctorAction
	}{
		{GateClassEnvironment, GateDoctorAction{Class: GateClassEnvironment, Rerun: true, Comment: "env"}},
		{GateClassCodeBroken, GateDoctorAction{Class: GateClassCodeBroken, FileFixBead: true, Comment: "code"}},
		{GateClassStaleGate, GateDoctorAction{Class: GateClassStaleGate, Escalate: true, Comment: "stale"}},
		{GateClassFlaky, GateDoctorAction{Class: GateClassFlaky, Rerun: true, Comment: "flaky"}},
		{GateClassUncertain, GateDoctorAction{Class: GateClassUncertain, Escalate: true, Comment: "unknown"}},
	} {
		doctor := &GateDoctor{
			PiRunner: func(ctx context.Context, cfg GateDoctorConfig, prompt string) (string, error) {
				return fmt.Sprintf(`{"class":"%s","reasoning":"%s"}`, tc.class, tc.action.Comment), nil
			},
		}
		got := doctor.Triage(context.Background(), "b1", "title", "gate", 1, "out")
		if got.Class != tc.action.Class || got.Rerun != tc.action.Rerun ||
			got.FileFixBead != tc.action.FileFixBead || got.Escalate != tc.action.Escalate {
			t.Errorf("class %q: got %+v, want %+v", tc.class, got, tc.action)
		}
	}
}

func TestGateDoctorTriage_NilDoctorFailClosed(t *testing.T) {
	var doctor *GateDoctor
	got := doctor.Triage(context.Background(), "b1", "t", "g", 1, "s")
	if got.Class != GateClassUncertain || !got.Escalate {
		t.Errorf("nil doctor: got %+v, want uncertain+escalate", got)
	}
}

func TestGateDoctorTriage_PiFaultFailClosed(t *testing.T) {
	doctor := &GateDoctor{
		PiRunner: func(ctx context.Context, cfg GateDoctorConfig, prompt string) (string, error) {
			return "", fmt.Errorf("pi provider timeout")
		},
	}
	got := doctor.Triage(context.Background(), "b1", "t", "g", 1, "s")
	if got.Class != GateClassUncertain || !got.Escalate {
		t.Errorf("pi fault: got %+v, want uncertain+escalate", got)
	}
	if !strings.Contains(got.Comment, "pi provider timeout") {
		t.Errorf("comment missing exact error: %q", got.Comment)
	}
}

func TestGateDoctorFileFixBead_CreatesBeadWithRedOutput(t *testing.T) {
	mock := newMockBeadsClient()
	doctor := &GateDoctor{BeadsClient: mock}
	red := "FAIL: TestFoo - expected 3, got 4"
	doctor.FileFixBead("/proj", "", "hydra-abc", "unit-tests", 1, red)

	calls := mock.getCloseCalls()
	_ = calls // no closes expected
	var created types.BeadID
	for id := range mock.beads {
		created = types.BeadID(id)
	}
	if created == "" {
		t.Fatal("no fix bead created")
	}
	b := mock.beads[string(created)]
	if !strings.Contains(b.Title, "code-broken") {
		t.Errorf("title = %q", b.Title)
	}
	if !strings.Contains(b.Description, red) {
		t.Errorf("description missing exact RED output")
	}
	if !strings.Contains(b.Description, "hydra-abc") {
		t.Errorf("description missing original bead id")
	}
}

func TestGateDoctorFileFixBead_NilClientNoOp(t *testing.T) {
	doctor := &GateDoctor{}
	doctor.FileFixBead("/proj", "", "b", "g", 1, "s") // must not panic
}
