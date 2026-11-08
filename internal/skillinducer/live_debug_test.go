package skillinducer

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
)

// TestDebugLiveProvider reproduces the live "no JSON object" failures using
// the exact provider stack. Skipped unless MINIMAX_LIVE_DEBUG=1.
func TestDebugLiveProvider(t *testing.T) {
	if os.Getenv("MINIMAX_LIVE_DEBUG") != "1" {
		t.Skip("live debug probe")
	}
	lc := hindsight.LLMConfig{
		Provider: "openai", Model: "MiniMax-M2.7",
		BaseURL: "https://api.minimax.io/v1",
		Timeout: 120 * time.Second, MaxTokens: 2000, Temperature: 0.2,
		APIKeyEnv: "MINIMAX_API_KEY",
	}
	p, err := hindsight.NewOpenAIProvider(lc)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, system, user string
	}{
		{"short", "Respond with ONLY a JSON object: {\"has_skill\": false, \"reason\": \"x\"}", "Routine Go closeout transcript. Decide."},
		{"real-system", systemPrompt, "Transcript of a routine Go verification closeout. Decide."},
		{"real-both", systemPrompt, buildUserPrompt(&ExecutionRecord{
			BeadID: "x", AgentType: "pi", Turns: 5,
			Output: "Executed gofmt, vet, build, ran 9/9 targeted tests, all passed. Committed with verification quotes. Closed bead with verify-first close path.",
		}, 40000)},
	} {
		resp, err := p.CompleteWithSystem(context.Background(), tc.system, tc.user)
		t.Logf("[%s] err=%v resp=%q", tc.name, err, resp)
		if _, perr := parseInduction(resp); perr != nil {
			t.Errorf("[%s] parse: %v", tc.name, perr)
		}
	}

	// Full-size real record (the class that failed live): 40k-char transcript.
	if path := os.Getenv("LIVE_RECORD"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var rec ExecutionRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatal(err)
		}
		resp, err := p.CompleteWithSystem(context.Background(), systemPrompt, buildUserPrompt(&rec, 40000))
		t.Logf("[live-record %s] outputlen=%d err=%v resp=%q", path, len(rec.Output), err, resp)
		if _, perr := parseInduction(resp); perr != nil {
			t.Errorf("[live-record] parse: %v", perr)
		}
	}
}
