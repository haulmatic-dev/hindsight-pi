package hindsight

import (
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

	c := NewClient(WithBaseURL(srv.URL), WithEnabled(true), WithMinScore(0.7))
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
