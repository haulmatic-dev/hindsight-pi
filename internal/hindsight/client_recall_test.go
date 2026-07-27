package hindsight

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSearch_ParsesResultsAndText pins the recall-response parsing fix.
// The Hindsight recall API returns matches under a top-level "results"
// array with the memory body in "text" and no per-memory score. A prior
// version decoded into "memories"/"content" and so silently dropped every
// result ("No relevant memories found") despite a populated store.
func TestSearch_ParsesResultsAndText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[` +
			`{"id":"m1","text":"use temporal.DefaultTaskQueue as the single source of truth","type":"pattern","tags":["hydra"]},` +
			`{"id":"m2","text":"second memory body"}` +
			`],"trace":{},"entities":[]}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithEnabled(true), WithMaxTokens(800))
	resp, err := c.Search("task queue default", "task", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(resp.Memories) != 2 {
		t.Fatalf("expected 2 memories parsed from results, got %d", len(resp.Memories))
	}
	if got := resp.Memories[0].Content; got != "use temporal.DefaultTaskQueue as the single source of truth" {
		t.Fatalf("expected recall text mapped into Content, got %q", got)
	}
	if !resp.Success || resp.Count != 2 {
		t.Fatalf("expected Success=true Count=2, got Success=%v Count=%d", resp.Success, resp.Count)
	}
}

// TestSearch_TruncatesToLimit pins client-side limit enforcement. The live
// recall API ignores the request's "limit" and "min_score" fields entirely
// (verified: 60+ results returned for limit=2), so without truncation every
// worker prompt was enriched with the full unbounded result set.
func TestSearch_TruncatesToLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"results":[`
		for i := 0; i < 20; i++ {
			if i > 0 {
				body += ","
			}
			body += `{"id":"m` + string(rune('a'+i)) + `","text":"memory body"}`
		}
		body += `]}`
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithEnabled(true))
	resp, err := c.Search("anything", "task", 3)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(resp.Memories) != 3 || resp.Count != 3 {
		t.Fatalf("expected truncation to limit=3 (server sent 20), got %d/%d", len(resp.Memories), resp.Count)
	}
}

// TestSearchRequest_SendsOnlyAcceptedFields pins the request body against the
// live recall API's schema (v0.6.2 accepts query/types/budget/max_tokens/trace/
// query_timestamp/include/tags/tags_match/tag_groups). The client previously
// sent limit/min_score/days_back/bank_id, none of which exist — so they were
// silently dropped and every recall returned the entire bank (~80KB measured).
func TestSearchRequest_SendsOnlyAcceptedFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithEnabled(true), WithMaxTokens(800))
	if _, err := c.Search("some task", "pattern", 5); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	// max_tokens must be sent — it is the API's only supported size control.
	if got, ok := body["max_tokens"]; !ok || int(got.(float64)) != 800 {
		t.Errorf("max_tokens = %v, want 800", body["max_tokens"])
	}
	// The phantom fields must NOT be sent.
	for _, dead := range []string{"limit", "min_score", "days_back", "bank_id"} {
		if _, present := body[dead]; present {
			t.Errorf("request must not send %q — the recall API does not accept it", dead)
		}
	}
}
