// Semantic failure clustering (PRD FR-020 follow-up, bead hydra-n5qt).
//
// The FR-020 signature clustering introduced with bead 5az8 groups close
// failures by exact equality of a normalized error string. That is
// mechanical — fine for contracts and safety gates, but clustering is a
// classification problem. During the 2026-09-29 telematics quarantine
// drain, exact-string matching split ONE root cause (stale integration
// base + handoff-doc conflicts on the same files) into 4 signatures, which
// produced 4 overlapping fix beads and 10+ open PRs targeting the same
// conflicts from different angles. A human had to triage duplicates the
// machine created.
//
// This file adds a semantic pass that runs after the exact-signature miss:
// it embeds the failure cause and compares it (cosine similarity) against
// the recorded causes of open meta fix beads. Near-duplicates are merged
// into the existing cluster. Design invariants:
//
//   - Deterministic signature stays the grouping key for exact matches
//     (it runs first; this pass only fills its gaps).
//   - Fail-open: any embedding failure (no API key, network error, bad
//     response) falls back to the current string clustering. Semantic
//     clustering must never block or gate fix-bead filing.
//   - Embeddings are cached in-process by signature hash, so repeated
//     failures of the same bead (retries) cost one embedding call.
package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/haulmatic-dev/hydra/pkg/types"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// SemanticClusterConfig configures the embedding-based clustering pass.
type SemanticClusterConfig struct {
	// Enabled turns the semantic pass on. Default false: it is inert
	// unless an operator opts in, because it spends provider budget.
	// The exact-signature clustering (FR-020) remains the default path.
	Enabled bool `yaml:"enabled"`

	// Threshold is the minimum cosine similarity for two failure causes
	// to be considered the same root cause. 0.82 is the tuned default:
	// low enough to merge paraphrased errors about the same conflict,
	// high enough not to merge distinct failures on the same repo.
	Threshold float64 `yaml:"threshold"`

	// EmbedBaseURL is the OpenAI-compatible embeddings endpoint base
	// (POST {EmbedBaseURL}/embeddings).
	EmbedBaseURL string `yaml:"embed_base_url"`

	// EmbedModel is the embeddings model id (default "text-embedding-3-small").
	EmbedModel string `yaml:"embed_model"`

	// APIKeyEnv is the environment variable holding the embeddings API
	// key (default "OPENAI_API_KEY").
	APIKeyEnv string `yaml:"embed_api_key_env"`

	// Timeout bounds a single embedding HTTP call.
	Timeout time.Duration `yaml:"embed_timeout"`
}

func (s SemanticClusterConfig) normalized() SemanticClusterConfig {
	if s.Threshold <= 0 || s.Threshold >= 1 {
		s.Threshold = 0.82
	}
	if s.EmbedBaseURL == "" {
		s.EmbedBaseURL = "https://api.openai.com/v1"
	}
	if s.EmbedModel == "" {
		s.EmbedModel = "text-embedding-3-small"
	}
	if s.APIKeyEnv == "" {
		s.APIKeyEnv = "OPENAI_API_KEY"
	}
	if s.Timeout <= 0 {
		s.Timeout = 10 * time.Second
	}
	return s
}

// embeddingClient is a minimal OpenAI-compatible /embeddings client.
type embeddingClient struct {
	cfg    SemanticClusterConfig
	apiKey string
	http   *http.Client
}

func newEmbeddingClient(cfg SemanticClusterConfig) (*embeddingClient, error) {
	cfg = cfg.normalized()
	key := strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
	if key == "" {
		return nil, fmt.Errorf("embedding API key env %s is not set", cfg.APIKeyEnv)
	}
	return &embeddingClient{cfg: cfg, apiKey: key, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// embed returns one embedding vector for text.
func (e *embeddingClient) embed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(embeddingsRequest{Model: e.cfg.EmbedModel, Input: []string{text}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(e.cfg.EmbedBaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings endpoint returned %d: %.200s", resp.StatusCode, string(raw))
	}
	var er embeddingsResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, err
	}
	if er.Error != nil && er.Error.Message != "" {
		return nil, fmt.Errorf("embeddings endpoint error: %s", er.Error.Message)
	}
	if len(er.Data) == 0 || len(er.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings endpoint returned no vectors")
	}
	return er.Data[0].Embedding, nil
}

// cosineSimilarity of two equal-length vectors; 0 on length mismatch.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// semanticClusterer owns the embedding pass. embedCache is keyed by
// signature hash so a retrying bead never pays for the same vector twice.
type semanticClusterer struct {
	mu         sync.Mutex
	cfg        SemanticClusterConfig
	client     *embeddingClient
	embedCache map[string][]float64
}

func newSemanticClusterer(cfg SemanticClusterConfig) *semanticClusterer {
	cfg = cfg.normalized()
	client, err := newEmbeddingClient(cfg)
	if err != nil {
		// Fail-open: no key configured means the semantic pass is inert;
		// string clustering continues to serve.
		log.Printf("[SemanticCluster] disabled: %v", err)
		return nil
	}
	return &semanticClusterer{cfg: cfg, client: client, embedCache: map[string][]float64{}}
}

// cachedEmbed returns the cached vector for the signature-hash key, calling
// the embeddings endpoint on miss. Errors are returned to the caller, which
// fails open.
func (s *semanticClusterer) cachedEmbed(ctx context.Context, sig, text string) ([]float64, error) {
	sum := sha256.Sum256([]byte(sig))
	key := fmt.Sprintf("%x", sum)
	s.mu.Lock()
	if v, ok := s.embedCache[key]; ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	v, err := s.client.embed(ctx, text)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if len(s.embedCache) > 1024 { // bounded in-process cache
		s.embedCache = map[string][]float64{}
	}
	s.embedCache[key] = v
	s.mu.Unlock()
	return v, nil
}

// findSemanticDuplicate compares the candidate failure cause against the
// recorded causes of open meta fix beads. It returns the ID of the first
// open fix bead whose cause is semantically equivalent (cosine >=
// threshold). Any embedding error returns empty ID + nil error: fail-open.
func (s *semanticClusterer) findSemanticDuplicate(ctx context.Context, sig, cause string, candidates []semanticCandidate) string {
	if s == nil || cause == "" || len(candidates) == 0 {
		return ""
	}
	candVec, err := s.cachedEmbed(ctx, sig, cause)
	if err != nil {
		log.Printf("[SemanticCluster] embedding failed for candidate (fail-open to string clustering): %v", err)
		return ""
	}
	for _, c := range candidates {
		if c.cause == "" {
			continue
		}
		vec, err := s.cachedEmbed(ctx, c.sig, c.cause)
		if err != nil {
			log.Printf("[SemanticCluster] embedding failed for %s (skipping, fail-open): %v", c.id, err)
			continue
		}
		if sim := cosineSimilarity(candVec, vec); sim >= s.cfg.Threshold {
			log.Printf("[SemanticCluster] failure cause of %s is semantically equivalent to meta fix bead %s (cosine %.2f >= %.2f); merging clusters", c.id, c.id, sim, s.cfg.Threshold)
			return c.id.String()
		}
	}
	return ""
}

// semanticCandidate is an open meta fix bead with its recorded cause.
type semanticCandidate struct {
	id    types.BeadID
	sig   string
	cause string
}

// openMetaFixBeads lists open/in-progress fix beads carrying a fix-sig-*
// label. Errors fail open (empty slice).
func (c *BeadCloser) openMetaFixBeads(projectPath, repoName string) []types.Bead {
	if c.beadsClient == nil {
		return nil
	}
	beads, err := c.beadsClient.ListBeads(projectPath, repoName)
	if err != nil {
		log.Printf("[SemanticCluster] could not list beads (fail-open): %v", err)
		return nil
	}
	var out []types.Bead
	for _, b := range beads {
		if b.Type != "fix" || b.Status.IsTerminal() || firstFixSigLabel(b.Labels) == "" {
			continue
		}
		out = append(out, b)
	}
	return out
}

// firstFixSigLabel returns the fix-sig-* label of a bead, or "".
func firstFixSigLabel(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "fix-sig-") {
			return l
		}
	}
	return ""
}

// causeFromFixDescription extracts the recorded failure cause block from a
// meta fix bead description (the "### What failed last time" fenced block
// written by createFixBeadForFailedCloseWithExitCode).
func causeFromFixDescription(desc string) string {
	marker := "### What failed last time"
	idx := strings.Index(desc, marker)
	if idx < 0 {
		return ""
	}
	rest := desc[idx+len(marker):]
	open := strings.Index(rest, "```")
	if open < 0 {
		return ""
	}
	rest = rest[open+3:]
	// Skip optional language tag on the fence line.
	if nl := strings.Index(rest, "\n"); nl >= 0 && !strings.Contains(rest[:nl], "```") {
		rest = rest[nl+1:]
	}
	close := strings.Index(rest, "```")
	if close < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:close])
}

// bgContext removed: callers build a timeout context inline with defer cancel.
