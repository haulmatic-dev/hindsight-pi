package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestVerdictMemory_BuildAdjudicationMemory_Real(t *testing.T) {
	g := gap{
		ID:            "gap-1",
		Category:      "missing-tests",
		Severity:      "high",
		Description:   "No unit tests for the retainer.",
		PRDRef:        "FR-001",
		TargetFiles:   []string{"verdict_memory.go"},
		SuggestedBead: "hydra-abc",
	}
	adj := adjudication{
		Verdict:    "real",
		Confidence: 0.87,
		Reasoning:  "The gap description matches an actual uncovered requirement.",
		GapID:      "gap-1",
		Cycle:      3,
	}

	mem := buildAdjudicationMemory("hydra", g, adj, "deadbeef")
	if mem == nil {
		t.Fatal("expected non-nil memory for real verdict")
	}

	if got := string(mem.BeadID); got != "hydra-abc" {
		t.Errorf("BeadID = %q, want %q", got, "hydra-abc")
	}
	if mem.BeadMetadata.Title != "Adjudication: missing-tests" {
		t.Errorf("Title = %q, want %q", mem.BeadMetadata.Title, "Adjudication: missing-tests")
	}
	if mem.BeadMetadata.Type != "adjudication" {
		t.Errorf("Type = %q, want adjudication", mem.BeadMetadata.Type)
	}
	if mem.BeadMetadata.Project != "hydra" {
		t.Errorf("Project = %q, want hydra", mem.BeadMetadata.Project)
	}
	if mem.ExecutionResult.Success {
		t.Error("Success = true for real verdict, want false")
	}
	if mem.ExecutionResult.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", mem.ExecutionResult.ExitCode)
	}

	want := "Category: missing-tests\nSeverity: high\nDescription: No unit tests for the retainer.\nVerdict: real\nConfidence: 0.87\nReasoning: The gap description matches an actual uncovered requirement."
	if mem.ExecutionResult.Output != want {
		t.Errorf("Output mismatch:\ngot:\n%s\n\nwant:\n%s", mem.ExecutionResult.Output, want)
	}
}

func TestVerdictMemory_BuildAdjudicationMemory_FalsePositive(t *testing.T) {
	g := gap{
		Category:    "style-nit",
		Severity:    "low",
		Description: "Minor formatting preference.",
	}
	adj := adjudication{
		Verdict:    "false_positive",
		Confidence: 0.95,
		Reasoning:  "This is a style preference, not a real gap.",
	}

	mem := buildAdjudicationMemory("hydra", g, adj, "cafebabe")
	if mem == nil {
		t.Fatal("expected non-nil memory for false_positive verdict")
	}
	if !mem.ExecutionResult.Success {
		t.Error("Success = false for false_positive, want true")
	}
	if !strings.Contains(mem.ExecutionResult.Output, "Verdict: false_positive") {
		t.Errorf("Output missing false_positive verdict: %s", mem.ExecutionResult.Output)
	}
}

func TestVerdictMemory_BuildAdjudicationMemory_Ambiguous(t *testing.T) {
	g := gap{Category: "unclear", Severity: "medium", Description: "Vague"}
	adj := adjudication{Verdict: "ambiguous", Confidence: 0.5, Reasoning: "Not sure"}
	if mem := buildAdjudicationMemory("hydra", g, adj, "baddcafe"); mem != nil {
		t.Fatalf("expected nil for ambiguous verdict, got %+v", mem)
	}
}

func TestVerdictMemory_BuildAdjudicationMemory_FallbackFingerprint(t *testing.T) {
	g := gap{
		Category:    "missing-tests",
		Severity:    "high",
		Description: "No tests.",
	}
	adj := adjudication{Verdict: "real", Confidence: 0.8, Reasoning: "Reason"}
	mem := buildAdjudicationMemory("hydra", g, adj, "01234567")
	if string(mem.BeadID) != "01234567" {
		t.Errorf("BeadID = %q, want fallback fingerprint", mem.BeadID)
	}
}

func TestVerdictMemory_BuildFailedCloseMemory_WithCause(t *testing.T) {
	bead := &types.Bead{
		ID:    types.BeadID("hydra-fix-001"),
		Title: "Fix retainer nil safety",
	}
	cause := "panic: runtime error: invalid memory address or nil pointer dereference"
	exitCode := 2

	mem := buildFailedCloseMemory("hydra", bead, cause, exitCode)
	if mem == nil {
		t.Fatal("expected non-nil memory when cause is provided")
	}
	if string(mem.BeadID) != "hydra-fix-001" {
		t.Errorf("BeadID = %q, want hydra-fix-001", mem.BeadID)
	}
	if mem.BeadMetadata.Title != "Failed close: Fix retainer nil safety" {
		t.Errorf("Title = %q", mem.BeadMetadata.Title)
	}
	if mem.BeadMetadata.Type != "failed-close" {
		t.Errorf("Type = %q, want failed-close", mem.BeadMetadata.Type)
	}
	if mem.ExecutionResult.Success {
		t.Error("Success = true for failed close, want false")
	}
	if mem.ExecutionResult.ExitCode != exitCode {
		t.Errorf("ExitCode = %d, want %d", mem.ExecutionResult.ExitCode, exitCode)
	}
	if mem.ExecutionResult.Output != cause {
		t.Errorf("Output = %q, want cause verbatim %q", mem.ExecutionResult.Output, cause)
	}
}

func TestVerdictMemory_BuildFailedCloseMemory_EmptyCause(t *testing.T) {
	bead := &types.Bead{ID: types.BeadID("hydra-fix-002"), Title: "Fix"}
	if mem := buildFailedCloseMemory("hydra", bead, "", 1); mem != nil {
		t.Fatalf("expected nil for empty cause, got %+v", mem)
	}
}

func TestVerdictMemory_BuildFailedCloseMemory_NilBead(t *testing.T) {
	if mem := buildFailedCloseMemory("hydra", nil, "some cause", 1); mem != nil {
		t.Fatalf("expected nil for nil bead, got %+v", mem)
	}
}

func TestVerdictRetainer_NilReceiver(t *testing.T) {
	var r *VerdictRetainer
	mem := &hindsight.ExecutionMemory{}
	// Must not panic.
	r.Retain(mem)
}

func TestVerdictRetainer_NilClient(t *testing.T) {
	r := NewVerdictRetainer(nil)
	mem := &hindsight.ExecutionMemory{}
	// Must not panic.
	r.Retain(mem)
}

func TestVerdictRetainer_DisabledClient(t *testing.T) {
	r := NewVerdictRetainer(hindsight.NewClient(hindsight.WithEnabled(false)))
	mem := &hindsight.ExecutionMemory{}
	r.Retain(mem)
}

func TestVerdictRetainer_RetainPOSTsOnce(t *testing.T) {
	var requestCount int
	var body map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/default/banks/hydra/memories") {
			t.Errorf("Path = %q, want /v1/default/banks/hydra/memories", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"memory_id":"m1"}`))
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.WithBaseURL(srv.URL), hindsight.WithEnabled(true))
	r := NewVerdictRetainer(client)

	bead := &types.Bead{ID: types.BeadID("hydra-b1"), Title: "T"}
	mem := buildFailedCloseMemory("hydra", bead, "cause text", 1)
	r.Retain(mem)

	if requestCount != 1 {
		t.Fatalf("POST count = %d, want 1", requestCount)
	}
	if body == nil {
		t.Fatal("request body not decoded")
	}
	items, ok := body["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("items = %v, want 1 item", body["items"])
	}
	item := items[0].(map[string]interface{})
	content := item["content"].(string)
	if !strings.Contains(content, "cause text") {
		t.Errorf("retain request content missing cause: %s", content)
	}
}

func TestVerdictRetainer_500Swallowed(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.WithBaseURL(srv.URL), hindsight.WithEnabled(true))
	r := NewVerdictRetainer(client)

	bead := &types.Bead{ID: types.BeadID("hydra-b2"), Title: "T2"}
	mem := buildFailedCloseMemory("hydra", bead, "cause", 1)

	// Must not panic and must return normally.
	r.Retain(mem)

	if requestCount != 1 {
		t.Fatalf("POST count = %d, want 1", requestCount)
	}
}

func TestVerdictRetainer_RetainAdjudicationPOSTsExpectedContent(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"memory_id":"m2"}`))
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.WithBaseURL(srv.URL), hindsight.WithEnabled(true))
	r := NewVerdictRetainer(client)

	g := gap{
		Category:    "missing-tests",
		Severity:    "high",
		Description: "No unit tests for the retainer.",
	}
	adj := adjudication{
		Verdict:    "real",
		Confidence: 0.87,
		Reasoning:  "The gap description matches an actual uncovered requirement.",
	}
	mem := buildAdjudicationMemory("hydra", g, adj, "deadbeef")
	r.Retain(mem)

	if body == nil {
		t.Fatal("request body not decoded")
	}
	items := body["items"].([]interface{})
	item := items[0].(map[string]interface{})
	content := item["content"].(string)
	for _, want := range []string{
		"Category: missing-tests",
		"Reasoning: The gap description matches an actual uncovered requirement.",
		"Success: false",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("serialized content missing %q:\n%s", want, content)
		}
	}
}

func TestVerdictRetainer_DuplicateVerdictConsumed(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"memory_id":"m3"}`))
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.WithBaseURL(srv.URL), hindsight.WithEnabled(true))
	r := NewVerdictRetainer(client)

	bead := &types.Bead{ID: types.BeadID("hydra-gwce"), Title: "Dup"}
	mem := buildFailedCloseMemory("hydra", bead, "same cause", 1)
	r.Retain(mem)
	r.Retain(mem) // duplicate emission for the same bead — must be consumed

	if requestCount != 1 {
		t.Fatalf("POST count = %d, want 1 (duplicate verdict must not be re-emitted)", requestCount)
	}
}

func TestVerdictRetainer_DistinctVerdictsRetained(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"memory_id":"m4"}`))
	}))
	defer srv.Close()

	client := hindsight.NewClient(hindsight.WithBaseURL(srv.URL), hindsight.WithEnabled(true))
	r := NewVerdictRetainer(client)

	bead := &types.Bead{ID: types.BeadID("hydra-gwce2"), Title: "Distinct"}
	r.Retain(buildFailedCloseMemory("hydra", bead, "cause one", 1))
	r.Retain(buildFailedCloseMemory("hydra", bead, "cause two", 1))

	if requestCount != 2 {
		t.Fatalf("POST count = %d, want 2 (different verdict content must pass through)", requestCount)
	}
}
