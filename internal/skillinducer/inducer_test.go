package skillinducer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockProvider records calls and serves scripted responses.
type mockProvider struct {
	mu    sync.Mutex
	calls int
	resp  func(call int) (string, error)
}

func (m *mockProvider) CompleteWithSystem(ctx context.Context, system, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.resp(m.calls)
}

func (m *mockProvider) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func goodSkillJSON(name, desc string) string {
	b, _ := json.Marshal(induction{
		HasSkill:    true,
		Name:        name,
		Description: desc,
		Triggers:    []string{"when doing X"},
		Body:        "## Steps\n\n1. Do X\n2. Verify Y",
	})
	return string(b)
}

func writeRecord(t *testing.T, dir, name string, rec ExecutionRecord) {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// testSetup creates exec + candidates dirs under t.TempDir and returns a
// Config wired to them.
func testSetup(t *testing.T) (Config, string, string) {
	t.Helper()
	root := t.TempDir()
	cfg := Config{
		Enabled:             true,
		Interval:            time.Hour,
		ExecutionsDir:       filepath.Join(root, "execs"),
		CandidatesDir:       filepath.Join(root, "cands"),
		MinTranscriptChars:  10,
		MaxPromptChars:      1000,
		MaxProcessedPerRun:  10,
		MaxCandidatesPerRun: 3,
		MaxAttempts:         2,
		Model:               "test-model",
	}
	if err := os.MkdirAll(cfg.ExecutionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.CandidatesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg, cfg.ExecutionsDir, cfg.CandidatesDir
}

func newInducer(t *testing.T, cfg Config, p LLMProvider) *Inducer {
	t.Helper()
	in, err := New(cfg, p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return in
}

func TestRunOnce_InducesCandidate(t *testing.T) {
	cfg, execDir, candDir := testSetup(t)
	writeRecord(t, execDir, "bead-1-20260101-000000.json", ExecutionRecord{
		BeadID:    "bead-1",
		Success:   true,
		AgentType: "pi",
		Turns:     12,
		Output:    strings.Repeat("did the thing carefully ", 500),
	})

	p := &mockProvider{resp: func(int) (string, error) {
		return goodSkillJSON("Jest Retry Hygiene", "Gitignore jest-retry temp dirs before running retries"), nil
	}}
	in := newInducer(t, cfg, p)

	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Induced != 1 || stats.LLMCalls != 1 {
		t.Fatalf("stats = %+v, want Induced=1 LLMCalls=1", stats)
	}

	skillPath := filepath.Join(candDir, "jest-retry-hygiene", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("candidate SKILL.md missing: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"name: jest-retry-hygiene",
		`description: "Gitignore jest-retry temp dirs before running retries"`,
		"## Steps",
		"Induced from worker execution `bead-1`",
		"MACHINE-EXTRACTED CANDIDATE",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("SKILL.md missing %q:\n%s", want, content)
		}
	}
	if _, err := os.Stat(filepath.Join(candDir, "jest-retry-hygiene", "metadata.json")); err != nil {
		t.Errorf("metadata.json missing: %v", err)
	}

	// State marked processed; a second run makes no LLM calls.
	stats2, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if stats2.Induced != 0 || p.callCount() != 1 {
		t.Fatalf("second run reprocessed: stats=%+v calls=%d", stats2, p.callCount())
	}
}

func TestRunOnce_FiltersFailedAndShortRecords(t *testing.T) {
	cfg, execDir, _ := testSetup(t)
	writeRecord(t, execDir, "a-failed.json", ExecutionRecord{Success: false, Output: strings.Repeat("x", 5000)})
	writeRecord(t, execDir, "b-short.json", ExecutionRecord{Success: true, Output: "tiny"})

	p := &mockProvider{resp: func(int) (string, error) { return goodSkillJSON("n", "d"), nil }}
	in := newInducer(t, cfg, p)

	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Skipped != 2 || stats.LLMCalls != 0 {
		t.Fatalf("stats = %+v, want Skipped=2 LLMCalls=0", stats)
	}
}

func TestRunOnce_MalformedRecordIsTerminalSkip(t *testing.T) {
	cfg, execDir, _ := testSetup(t)
	if err := os.WriteFile(filepath.Join(execDir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &mockProvider{resp: func(int) (string, error) { return goodSkillJSON("n", "d"), nil }}
	in := newInducer(t, cfg, p)

	if _, err := in.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// Malformed record stays skipped across runs — no retry, no LLM call.
	if _, err := in.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.callCount() != 0 {
		t.Fatalf("malformed record triggered LLM calls: %d", p.callCount())
	}
}

func TestRunOnce_NoSkillIsTerminal(t *testing.T) {
	cfg, execDir, candDir := testSetup(t)
	writeRecord(t, execDir, "a.json", ExecutionRecord{Success: true, Output: strings.Repeat("routine work ", 400)})

	p := &mockProvider{resp: func(int) (string, error) {
		return `{"has_skill": false, "reason": "one-off repo fix"}`, nil
	}}
	in := newInducer(t, cfg, p)

	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.NoSkill != 1 || stats.Induced != 0 {
		t.Fatalf("stats = %+v, want NoSkill=1 Induced=0", stats)
	}
	if p.callCount() != 1 {
		t.Fatalf("no_skill record reprocessed: calls=%d", p.callCount())
	}
	entries, _ := os.ReadDir(candDir)
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("unexpected candidate dir: %s", e.Name())
		}
	}
}

func TestRunOnce_DedupAgainstExistingLibrary(t *testing.T) {
	cfg, execDir, _ := testSetup(t)
	cfg.SkillsDir = filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(cfg.SkillsDir, "jest-retry-hygiene"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := os.WriteFile(filepath.Join(cfg.SkillsDir, "jest-retry-hygiene", "SKILL.md"), []byte(
		"---\nname: jest-retry-hygiene\ndescription: Gitignore jest-retry temp dirs before running test retries\n---\n\nbody"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	writeRecord(t, execDir, "a.json", ExecutionRecord{Success: true, Output: strings.Repeat("cleaned retries ", 400)})
	p := &mockProvider{resp: func(int) (string, error) {
		return goodSkillJSON("jest retry hygiene", "Gitignore jest-retry temp dirs before running test retries"), nil
	}}
	in := newInducer(t, cfg, p)

	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Duplicates != 1 || stats.Induced != 0 {
		t.Fatalf("stats = %+v, want Duplicates=1 Induced=0", stats)
	}
}

func TestRunOnce_DedupAmongCandidatesInSameRun(t *testing.T) {
	cfg, execDir, candDir := testSetup(t)
	cfg.MaxCandidatesPerRun = 3
	for i, name := range []string{"a.json", "b.json"} {
		writeRecord(t, execDir, name, ExecutionRecord{
			BeadID:  "bead-" + string(rune('0'+i)),
			Success: true,
			Output:  strings.Repeat("same procedure ", 400),
		})
	}

	p := &mockProvider{resp: func(int) (string, error) {
		return goodSkillJSON("gate-runner-bootstrap", "Verify declared gates on the real checkout before enabling daemon ownership"), nil
	}}
	in := newInducer(t, cfg, p)

	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Induced != 1 || stats.Duplicates != 1 {
		t.Fatalf("stats = %+v, want Induced=1 Duplicates=1", stats)
	}
	if _, err := os.Stat(filepath.Join(candDir, "gate-runner-bootstrap", "SKILL.md")); err != nil {
		t.Errorf("first candidate missing: %v", err)
	}
}

func TestRunOnce_RetriesProviderErrorsThenFails(t *testing.T) {
	cfg, execDir, _ := testSetup(t)
	cfg.MaxAttempts = 2
	writeRecord(t, execDir, "a.json", ExecutionRecord{Success: true, Output: strings.Repeat("work ", 400)})

	p := &mockProvider{resp: func(int) (string, error) { return "", errors.New("provider down") }}
	in := newInducer(t, cfg, p)

	// Attempt 1: error, file not terminal.
	if _, err := in.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := p.callCount(); got != 1 {
		t.Fatalf("calls after run 1 = %d, want 1", got)
	}
	// Attempt 2: error again — attempts reach MaxAttempts but the record is
	// only marked failed on the NEXT pass (attempts >= MaxAttempts check).
	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 0 {
		t.Fatalf("stats = %+v, want Failed=0 (still retryable)", stats)
	}
	// Attempt 3: terminal, no more calls.
	stats, err = in.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 1 {
		t.Fatalf("stats = %+v, want Failed=1", stats)
	}
	if got := p.callCount(); got != 2 {
		t.Fatalf("calls after terminal fail = %d, want 2", got)
	}
}

func TestRunOnce_MissingExecutionsDirIsNoop(t *testing.T) {
	cfg, _, _ := testSetup(t)
	cfg.ExecutionsDir = filepath.Join(t.TempDir(), "does-not-exist")
	p := &mockProvider{resp: func(int) (string, error) { return goodSkillJSON("n", "d"), nil }}
	in := newInducer(t, cfg, p)

	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce should not fail on missing dir: %v", err)
	}
	if stats.Scanned != 0 {
		t.Fatalf("stats = %+v, want Scanned=0", stats)
	}
}

func TestRunOnce_CtxCancelledReturnsError(t *testing.T) {
	cfg, execDir, _ := testSetup(t)
	writeRecord(t, execDir, "a.json", ExecutionRecord{Success: true, Output: strings.Repeat("work ", 400)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockProvider{resp: func(int) (string, error) { return "", ctx.Err() }}
	in := newInducer(t, cfg, p)

	if _, err := in.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRunOnce_RespectsCandidateBudget(t *testing.T) {
	cfg, execDir, _ := testSetup(t)
	cfg.MaxCandidatesPerRun = 1
	for _, name := range []string{"a.json", "b.json"} {
		writeRecord(t, execDir, name, ExecutionRecord{Success: true, Output: strings.Repeat("procedure ", 400)})
	}
	p := &mockProvider{resp: func(call int) (string, error) {
		return goodSkillJSON("skill-"+strings.Repeat("a", call), "Distinct description number "+string(rune('0'+call))), nil
	}}
	in := newInducer(t, cfg, p)

	stats, err := in.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Induced != 1 {
		t.Fatalf("stats = %+v, want Induced=1 (budget)", stats)
	}
}

func TestParseInduction(t *testing.T) {
	cases := []struct {
		name    string
		resp    string
		wantErr bool
		wantHS  bool
	}{
		{"plain", `{"has_skill": true, "name": "x", "description": "d"}`, false, true},
		{"fenced", "```json\n{\"has_skill\": true, \"name\": \"x\"}\n```", false, true},
		{"think block", "<think>the transcript mentions {not json} and braces galore</think>\n\n{\"has_skill\": false, \"reason\": \"r\"}", false, false},
		{"fences inside body_markdown", "{\"has_skill\": true, \"name\": \"x\", \"body_markdown\": \"Use:\\n\\n```go\\nfmt.Println(1)\\n```\\n\\nthen verify.\"}", false, true},
		{"think only, no json", "<think>all reasoning, no answer</think>", true, false},
		{"prose wrapped", `Here is the verdict: {"has_skill": false, "reason": "one-off"} hope this helps`, false, false},
		{"empty", "", true, false},
		{"no json", "the transcript showed nothing special", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ind, err := parseInduction(tc.resp)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", ind)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if ind.HasSkill != tc.wantHS {
				t.Errorf("HasSkill = %v, want %v", ind.HasSkill, tc.wantHS)
			}
		})
	}
}

func TestNormalizeSkillName(t *testing.T) {
	cases := map[string]string{
		"Jest Retry Hygiene":     "jest-retry-hygiene",
		"  spaced -- out  ":      "spaced-out",
		"weird!@#chars&*":        "weird-chars",
		"UPPER_CASE_NAME":        "upper-case-name",
		"ab":                     "",                      // too short
		"!!!":                    "",                      // nothing left
		strings.Repeat("a", 100): strings.Repeat("a", 64), // capped at 64
	}
	for in, want := range cases {
		if got := normalizeSkillName(in); got != want {
			t.Errorf("normalizeSkillName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	name, desc := parseFrontmatter("---\nname: \"quoted\"\ndescription: plain text\n---\n\n# Body")
	if name != "quoted" || desc != "plain text" {
		t.Errorf("got name=%q desc=%q", name, desc)
	}
	if n, d := parseFrontmatter("# no frontmatter"); n != "" || d != "" {
		t.Errorf("no-frontmatter should be empty, got %q %q", n, d)
	}
}

func TestIsDuplicate(t *testing.T) {
	idx := []skillIndexEntry{
		{Name: "jest-retry-hygiene", Description: "Gitignore jest-retry temp dirs before running test retries"},
		{Name: "golang-patterns", Description: "Idiomatic Go patterns and conventions"},
		{Name: "verify-first-close", Description: "Validate an already-landed fix before editing or closing a coding task"},
	}
	score, entry := isDuplicate("jest-retry-hygiene", "Gitignore jest-retry temp dirs before retries", idx)
	if score < 0.9 || entry.Name != "jest-retry-hygiene" {
		t.Errorf("exact name should short-circuit: score=%.2f entry=%q", score, entry.Name)
	}
	score, _ = isDuplicate("golang-conventions", "Idiomatic Go patterns and conventions", idx)
	if score < 0.6 {
		t.Errorf("near-duplicate should score high: %.2f", score)
	}
	score, _ = isDuplicate("angular-signals", "Signal-based reactive state management in Angular", idx)
	if score > 0.3 {
		t.Errorf("unrelated should score low: %.2f", score)
	}
	score, entry = isDuplicate("verify-first-close-in-shared-worktree", "Validate an already-landed fix in a shared worktree before closing", idx)
	if score < 0.6 || entry.Name != "verify-first-close" {
		t.Errorf("variant-name sprawl must be caught by containment: %.2f vs %q", score, entry.Name)
	}
}

func TestBuildUserPrompt_Truncation(t *testing.T) {
	rec := &ExecutionRecord{BeadID: "b1", Output: strings.Repeat("x", 5000)}
	prompt := buildUserPrompt(rec, 1000)
	if !strings.Contains(prompt, "transcript truncated") {
		t.Error("truncation marker missing")
	}
	if !strings.Contains(prompt, "- bead_id: b1") {
		t.Error("bead metadata missing")
	}
}

func TestConfigApplyDefaults(t *testing.T) {
	cfg := Config{}.ApplyDefaults()
	if cfg.Interval != 6*time.Hour || cfg.Provider != "kimi" || cfg.APIKeyEnv != "KIMI_API_KEY" {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if cfg.MaxCandidatesPerRun != 3 || cfg.MaxProcessedPerRun != 10 || cfg.SimilarityThreshold != 0.6 {
		t.Errorf("budget defaults wrong: %+v", cfg)
	}
}

func TestNew_MissingModel(t *testing.T) {
	if _, err := New(Config{Enabled: true}, &mockProvider{}); err == nil {
		t.Fatal("want error when model missing")
	}
}
