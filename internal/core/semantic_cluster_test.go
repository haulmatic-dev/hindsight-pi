package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// embedServer builds an OpenAI-compatible /embeddings test server. Each
// entry maps a text-substring selector to a fixed vector; unmatched text
// gets a deterministic "other" vector far from every cluster.
func embedServer(t *testing.T, clusters map[string][]float64) *httptest.Server {
	t.Helper()
	other := []float64{-0.9, 0.9, -0.9}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req embeddingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		vec := other
		for sel, v := range clusters {
			if strings.Contains(req.Input[0], sel) {
				vec = v
				break
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": vec}},
		})
	}))
}

// writeFailureLog writes a minimal worker log that ExtractFailureCause will
// pick an "error:" line out of, keyed by bead ID.
func writeFailureLog(t *testing.T, beadID, cause string) {
	t.Helper()
	dir := filepath.Join(workersLogDir(), "..")
	if err := os.MkdirAll(filepath.Join(workersLogDir()), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = dir
	f := filepath.Join(workersLogDir(), safeBeadID(beadID)+"-20260929-120000.log")
	if err := os.WriteFile(f, []byte("2026/09/29 12:00:00 [Worker:"+beadID+"] [stderr] error: "+cause+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCosineSimilarity: parallel vectors = 1, orthogonal = 0.
func TestCosineSimilarity(t *testing.T) {
	if got := cosineSimilarity([]float64{1, 0}, []float64{2, 0}); got < 0.999 {
		t.Errorf("parallel vectors: got %f, want ~1", got)
	}
	if got := cosineSimilarity([]float64{1, 0}, []float64{0, 1}); got != 0 {
		t.Errorf("orthogonal vectors: got %f, want 0", got)
	}
	if got := cosineSimilarity([]float64{1}, []float64{1, 0}); got != 0 {
		t.Errorf("length mismatch must yield 0, got %f", got)
	}
}

// TestCauseFromFixDescription round-trips the description format written by
// createFixBeadForFailedCloseWithExitCode.
func TestCauseFromFixDescription(t *testing.T) {
	desc := "Auto-created fix bead because the original closed as `failed`.\n\n### What failed last time\n\n```\nerror: conflict in docs/handoff.md\n```\n\nVERIFY FIRST: ..."
	if got := causeFromFixDescription(desc); got != "error: conflict in docs/handoff.md" {
		t.Errorf("got %q", got)
	}
	if got := causeFromFixDescription("no marker here"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

// TestSemanticClustering_ReplayTelematics replays the 2026-09-29 quarantine
// set (pmgh/qj74/5gq + kher): four failures whose normalized string
// signatures differ (different bead IDs scrub to <bead> but the surrounding
// prose differs) yet share ONE root cause — a stale integration base
// producing conflicts on the same pmgh/5gq files. Expect 1-2 fix beads,
// not 4.
func TestSemanticClustering_ReplayTelematics(t *testing.T) {
	clusters := map[string][]float64{
		"stale integration base": {0.95, 0.1, 0.05},
	}
	srv := embedServer(t, clusters)
	defer srv.Close()

	t.Setenv("TEST_EMBED_KEY", "test-key")
	cfg := &Config{}
	cfg.Validation.SemanticClustering = SemanticClusterConfig{
		Enabled:      true,
		Threshold:    0.82,
		EmbedBaseURL: srv.URL,
		APIKeyEnv:    "TEST_EMBED_KEY",
	}
	mock := newMockBeadsClient()
	closer := &BeadCloser{beadsClient: mock, config: cfg}

	// Four distinct textual presentations of the same root cause. Their
	// string signatures differ (different file pairings), so the exact
	// FR-020 signature match cannot merge them; only the semantic pass can.
	causes := []string{
		"merge conflict in pmgh/5gq handoff docs: stale integration base",
		"rebase failed: stale integration base conflicts with pmgh changes",
		"conflict markers in handoff.md: stale integration base again",
		"git conflict on 5gq files, stale integration base",
	}
	ids := []string{"proj-pmgh", "proj-qj74", "proj-5gq", "proj-kher"}
	titles := []string{
		"GREEN: Implement telematics ingest",
		"REFACTOR: Tidy telematics ingest",
		"TEST: Cover telematics ingest",
		"DISCOVERY: Map telematics ingest",
	}
	for i, id := range ids {
		writeFailureLog(t, id, fmt.Sprintf("stale integration base: %s", causes[i]))
		orig := types.Bead{ID: types.BeadID(id), Title: titles[i], Priority: 2}
		mock.beads[id] = orig
		if err := closer.createFixBeadForFailedClose(&orig, "/tmp/proj", "", id); err != nil {
			t.Fatalf("failure %d (%s): %v", i, id, err)
		}
	}
	fixes := countFixBeads(mock)
	if len(fixes) > 2 {
		names := make([]string, 0, len(fixes))
		for _, f := range fixes {
			names = append(names, string(f.ID))
		}
		t.Errorf("expected 1-2 meta fix beads from 4 same-root-cause failures, got %d: %v", len(fixes), names)
	}
	if len(fixes) == 0 {
		t.Fatal("expected at least one meta fix bead")
	}
	// Parked originals must carry the fix-clustered label.
	parked := 0
	for _, id := range ids[1:] {
		if b, ok := mock.beads[id]; ok && hasBeadLabel(b.Labels, fixClusteredLabel) {
			parked++
		}
	}
	if parked < 2 {
		t.Errorf("expected at least 2 parked originals, got %d", parked)
	}
}

// TestSemanticClustering_FailOpen: when the embeddings endpoint is down the
// close path must not fail — behavior falls back to string clustering and
// fix beads still get filed.
func TestSemanticClustering_FailOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("TEST_EMBED_KEY", "test-key")
	cfg := &Config{}
	cfg.Validation.SemanticClustering = SemanticClusterConfig{
		Enabled:      true,
		EmbedBaseURL: srv.URL,
		APIKeyEnv:    "TEST_EMBED_KEY",
	}
	mock := newMockBeadsClient()
	closer := &BeadCloser{beadsClient: mock, config: cfg}

	orig := types.Bead{ID: "proj-f1", Title: "GREEN: Implement the thing", Priority: 2}
	mock.beads[string(orig.ID)] = orig
	if err := closer.createFixBeadForFailedClose(&orig, "/tmp/proj", "", string(orig.ID)); err != nil {
		t.Fatalf("embedding failure must not fail filing, got: %v", err)
	}
	if len(countFixBeads(mock)) != 1 {
		t.Error("fallback must still file a fix bead")
	}
}

// TestSemanticClustering_DisabledByDefault: without the config knob the
// semantic pass is inert (no embedding client, string clustering only).
func TestSemanticClustering_DisabledByDefault(t *testing.T) {
	closer := &BeadCloser{}
	if got := closer.getSemanticClusterer(); got != nil {
		t.Error("semantic clusterer must be nil when disabled")
	}
}

// TestSemanticClusterer_NoKeyInert: missing API key yields a nil clusterer,
// never an error path that could block filing.
func TestSemanticClusterer_NoKeyInert(t *testing.T) {
	t.Setenv("TEST_EMBED_KEY", "")
	if got := newSemanticClusterer(SemanticClusterConfig{Enabled: true, APIKeyEnv: "TEST_EMBED_KEY"}); got != nil {
		t.Error("no API key must yield nil clusterer (fail-open inert)")
	}
}

// TestSemanticClusterer_EmbedCache: same signature hash embeds once.
func TestSemanticClusterer_EmbedCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float64{1, 0}}}})
	}))
	defer srv.Close()

	t.Setenv("TEST_EMBED_KEY", "test-key")
	sc := newSemanticClusterer(SemanticClusterConfig{Enabled: true, EmbedBaseURL: srv.URL, APIKeyEnv: "TEST_EMBED_KEY"})
	if sc == nil {
		t.Fatal("clusterer nil with key set")
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := sc.cachedEmbed(ctx, "sig-a", "text a"); err != nil {
			t.Fatalf("embed: %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("expected 1 embedding call after cache hit, got %d", calls)
	}
}
