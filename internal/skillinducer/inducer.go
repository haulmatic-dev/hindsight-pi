// Package skillinducer mines successful worker execution transcripts
// ($TMPDIR/hydra-executions/*.json, written by cmd/worker) into candidate Pi
// skills for human review.
//
// Design follows the skills module of ScholarEvolve (arXiv:2609.40169):
//
//   - induction: extract reusable procedures from successful trajectories
//   - curation:  dedup candidates against the existing skill library
//   - retrieval: already free — Pi loads skills by SKILL.md trigger
//     description; nothing here needs to change worker prompts
//
// Candidates are written to a staging directory (CandidatesDir) and NEVER
// into the shared Pi config directory: that directory is every human's live
// config (pi.manage_config must stay false), so promotion to the shared
// library is a human-reviewed PR. This is the gate that keeps a bad
// induction from shipping fleet-wide.
//
// The inducer is fail-open: a missing API key, a broken provider, or a
// malformed transcript disables induction for that run and logs — it must
// never block the daemon.
package skillinducer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/hindsight"
)

// Config configures skill induction. It is embedded in the core
// IntelligenceConfig as `intelligence.skill_induction`; zero values mean
// "use defaults" and Enabled=false (the default) disables the component.
type Config struct {
	Enabled bool `yaml:"enabled"`

	// Interval is the daemon loop period. Default 6h.
	Interval time.Duration `yaml:"interval"`

	// ExecutionsDir is where cmd/worker writes execution records.
	// Default: $TMPDIR/hydra-executions.
	ExecutionsDir string `yaml:"executions_dir"`

	// CandidatesDir is the staging directory for induced skills and state.
	// Default: $HYDRA_HOME/skill-candidates (fallback: $HOME/.hydra/skill-candidates).
	CandidatesDir string `yaml:"candidates_dir"`

	// SkillsDir is the existing shared skill library, read-only, used for
	// dedup. Typically {pi.config_dir}/skills, wired by cmd/hydra. Empty
	// disables library dedup (candidates still dedup among themselves).
	SkillsDir string `yaml:"skills_dir"`

	// MaxProcessedPerRun bounds LLM calls per RunOnce. Default 10.
	MaxProcessedPerRun int `yaml:"max_processed_per_run"`

	// MaxCandidatesPerRun bounds candidates written per RunOnce. Default 3.
	MaxCandidatesPerRun int `yaml:"max_candidates_per_run"`

	// MinTranscriptChars filters out trivial transcripts. Default 2000.
	MinTranscriptChars int `yaml:"min_transcript_chars"`

	// MaxPromptChars caps the transcript excerpt sent to the LLM. Default 40000.
	MaxPromptChars int `yaml:"max_prompt_chars"`

	// SimilarityThreshold is the Jaccard token-overlap level above which a
	// candidate is considered a duplicate of an existing skill. Default 0.6.
	SimilarityThreshold float64 `yaml:"similarity_threshold"`

	// MaxAttempts bounds LLM retries per execution file before it is marked
	// failed. Default 3.
	MaxAttempts int `yaml:"max_attempts"`

	// LLM provider settings (OpenAI-compatible). Provider "kimi" defaults
	// to the Moonshot API; point BaseURL at a router to use the fleet's
	// kimi-router instead. Model is REQUIRED when Enabled.
	Provider  string        `yaml:"provider"`
	Model     string        `yaml:"model"`
	BaseURL   string        `yaml:"base_url"`
	APIKeyEnv string        `yaml:"api_key_env"`
	Timeout   time.Duration `yaml:"timeout"`
	MaxTokens int           `yaml:"max_tokens"`
}

// ApplyDefaults fills zero-valued fields with proven defaults and returns
// the resulting config. Exported so callers can display effective settings.
func (c Config) ApplyDefaults() Config {
	out := c
	if out.Interval == 0 {
		out.Interval = 6 * time.Hour
	}
	if out.ExecutionsDir == "" {
		out.ExecutionsDir = filepath.Join(os.TempDir(), "hydra-executions")
	}
	if out.CandidatesDir == "" {
		out.CandidatesDir = defaultCandidatesDir()
	}
	if out.MaxProcessedPerRun == 0 {
		out.MaxProcessedPerRun = 10
	}
	if out.MaxCandidatesPerRun == 0 {
		out.MaxCandidatesPerRun = 3
	}
	if out.MinTranscriptChars == 0 {
		out.MinTranscriptChars = 2000
	}
	if out.MaxPromptChars == 0 {
		out.MaxPromptChars = 40000
	}
	if out.SimilarityThreshold == 0 {
		out.SimilarityThreshold = 0.6
	}
	if out.MaxAttempts == 0 {
		out.MaxAttempts = 3
	}
	if out.Provider == "" {
		out.Provider = "kimi"
	}
	if out.APIKeyEnv == "" {
		out.APIKeyEnv = "KIMI_API_KEY"
	}
	if out.Timeout == 0 {
		out.Timeout = 120 * time.Second
	}
	if out.MaxTokens == 0 {
		out.MaxTokens = 2000
	}
	return out
}

func defaultCandidatesDir() string {
	root := os.Getenv("HYDRA_HOME")
	if root == "" {
		root = os.Getenv("HOME")
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, "skill-candidates")
}

// LLMProvider is the minimal provider surface the inducer needs. It is
// satisfied by hindsight.LLMProvider (OpenAI-compatible).
type LLMProvider interface {
	CompleteWithSystem(ctx context.Context, system, prompt string) (string, error)
}

// ExecutionRecord mirrors the JSON cmd/worker writes to the executions dir
// (see writeExecutionOutputToFile in cmd/worker/main.go). Only the fields
// the inducer uses are modeled; unknown fields are ignored.
type ExecutionRecord struct {
	BeadID    string  `json:"bead_id"`
	Timestamp string  `json:"timestamp"`
	Success   bool    `json:"success"`
	ExitCode  int     `json:"exit_code"`
	Duration  float64 `json:"duration"`
	Attempts  int     `json:"attempts"`
	Turns     int     `json:"turns"`
	AgentType string  `json:"agent_type"`
	Output    string  `json:"output"`
	Error     string  `json:"error"`
}

// RunStats reports one RunOnce pass.
type RunStats struct {
	Scanned    int // files seen in ExecutionsDir
	Considered int // passed filters (successful, long enough, budget)
	LLMCalls   int
	Induced    int // candidates written
	Duplicates int // suppressed by curation
	NoSkill    int // LLM found no generalizable procedure
	Skipped    int // failed runs / short transcripts / malformed JSON
	Failed     int // exhausted MaxAttempts
}

func (s RunStats) String() string {
	return fmt.Sprintf("scanned=%d considered=%d llm=%d induced=%d dup=%d noskill=%d skipped=%d failed=%d",
		s.Scanned, s.Considered, s.LLMCalls, s.Induced, s.Duplicates, s.NoSkill, s.Skipped, s.Failed)
}

// fileState is the durable per-execution-file verdict. Terminal statuses are
// never revisited: processed, duplicate, no_skill, name_collision, skipped,
// failed. An empty status with Attempts > 0 means "retry next run".
type fileState struct {
	Attempts  int       `json:"attempts"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
	Candidate string    `json:"candidate,omitempty"` // name of induced skill
	Reason    string    `json:"reason,omitempty"`
}

type persistedState struct {
	Files map[string]fileState `json:"files"`
}

func terminal(status string) bool {
	switch status {
	case "processed", "duplicate", "no_skill", "name_collision", "skipped", "failed":
		return true
	}
	return false
}

// Inducer scans execution records and induces candidate skills.
type Inducer struct {
	cfg      Config
	provider LLMProvider
	log      *log.Logger
	mu       sync.Mutex // serializes RunOnce (daemon loop + manual run)
}

// New builds an Inducer. It fails if the provider cannot be constructed
// (missing API key, unsupported provider, empty model) — callers treat this
// as fail-open: log and disable, never block startup.
func New(cfg Config, provider LLMProvider) (*Inducer, error) {
	cfg = cfg.ApplyDefaults()
	if cfg.Model == "" {
		return nil, fmt.Errorf("skill_induction.model is required when skill induction is enabled")
	}
	if provider == nil {
		var err error
		provider, err = newProvider(cfg)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(cfg.CandidatesDir, 0o755); err != nil {
		return nil, fmt.Errorf("create candidates dir %s: %w", cfg.CandidatesDir, err)
	}
	return &Inducer{
		cfg:      cfg,
		provider: provider,
		log:      log.New(log.Writer(), "[SkillInducer] ", log.Flags()|0),
	}, nil
}

// newProvider builds the OpenAI-compatible provider via the hindsight
// factory so provider "kimi" gets its Moonshot base URL default.
func newProvider(cfg Config) (LLMProvider, error) {
	lc := hindsight.LLMConfig{
		Provider:    cfg.Provider,
		Model:       cfg.Model,
		BaseURL:     cfg.BaseURL,
		Timeout:     cfg.Timeout,
		MaxTokens:   cfg.MaxTokens,
		Temperature: 0.2,
		APIKeyEnv:   cfg.APIKeyEnv,
	}
	factory := hindsight.NewLLMProviderFactory(hindsight.DefaultLLMConfig())
	return factory.CreateProvider(lc)
}

// CandidatesDir returns the effective staging directory.
func (in *Inducer) CandidatesDir() string { return in.cfg.CandidatesDir }

// RunLoop runs RunOnce on the configured interval until ctx is cancelled.
// First pass starts immediately; errors are logged and never fatal.
func (in *Inducer) RunLoop(ctx context.Context) {
	in.runOnceLogged(ctx)
	ticker := time.NewTicker(in.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			in.runOnceLogged(ctx)
		}
	}
}

func (in *Inducer) runOnceLogged(ctx context.Context) {
	stats, err := in.RunOnce(ctx)
	if err != nil {
		in.log.Printf("run error: %v", err)
		return
	}
	if stats.Scanned > 0 {
		in.log.Printf("pass complete: %s", stats)
	}
}

// RunOnce scans the executions dir once, induces at most
// MaxCandidatesPerRun new skills, and persists state. A missing executions
// dir is not an error (daemon boot order); provider errors mark the file for
// retry until MaxAttempts, then failed.
func (in *Inducer) RunOnce(ctx context.Context) (*RunStats, error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	stats := &RunStats{}
	state, err := in.loadState()
	if err != nil {
		return stats, fmt.Errorf("load state: %w", err)
	}

	entries, err := os.ReadDir(in.cfg.ExecutionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil // nothing to do yet
		}
		return stats, fmt.Errorf("read executions dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // filenames embed a timestamp; chronological order
	stats.Scanned = len(names)

	// Existing candidates dedup against both the shared library and each other.
	var skillIndex []skillIndexEntry
	if in.cfg.SkillsDir != "" {
		if idx, err := loadSkillIndex(in.cfg.SkillsDir); err == nil {
			skillIndex = append(skillIndex, idx...)
		} else {
			in.log.Printf("skill library index unavailable (%v); library dedup degraded", err)
		}
	}
	if idx, err := loadSkillIndex(in.cfg.CandidatesDir); err == nil {
		skillIndex = append(skillIndex, idx...)
	}

	now := time.Now().UTC()
	for _, name := range names {
		if stats.Considered >= in.cfg.MaxProcessedPerRun || stats.Induced >= in.cfg.MaxCandidatesPerRun {
			break
		}
		fs := state.Files[name]
		if terminal(fs.Status) {
			continue
		}
		if fs.Attempts >= in.cfg.MaxAttempts {
			fs.Status = "failed"
			fs.UpdatedAt = now
			state.Files[name] = fs
			stats.Failed++
			continue
		}

		path := filepath.Join(in.cfg.ExecutionsDir, name)
		rec, err := readExecutionRecord(path)
		if err != nil {
			// Malformed records never heal — mark terminal.
			fs.Status = "skipped"
			fs.Reason = err.Error()
			fs.UpdatedAt = now
			state.Files[name] = fs
			stats.Skipped++
			continue
		}
		if !rec.Success || len(rec.Output) < in.cfg.MinTranscriptChars {
			fs.Status = "skipped"
			fs.Reason = fmt.Sprintf("success=%v output_chars=%d", rec.Success, len(rec.Output))
			fs.UpdatedAt = now
			state.Files[name] = fs
			stats.Skipped++
			continue
		}

		stats.Considered++
		fs.Attempts++
		fs.UpdatedAt = now

		ind, err := in.induce(ctx, rec)
		if err != nil {
			if ctx.Err() != nil {
				// Shutdown mid-call: do not persist the attempt bump for
				// a call we abandoned — but we already bumped fs.Attempts
				// above, so keep it; bounded by MaxAttempts anyway.
				state.Files[name] = fs
				return stats, ctx.Err()
			}
			state.Files[name] = fs
			stats.LLMCalls++
			in.log.Printf("induce %s failed (attempt %d/%d): %v", name, fs.Attempts, in.cfg.MaxAttempts, err)
			continue
		}
		stats.LLMCalls++

		if !ind.HasSkill {
			fs.Status = "no_skill"
			fs.Reason = ind.Reason
			fs.UpdatedAt = now
			state.Files[name] = fs
			stats.NoSkill++
			continue
		}

		normName := normalizeSkillName(ind.Name)
		if normName == "" {
			fs.Status = "failed"
			fs.Reason = fmt.Sprintf("unusable skill name %q", ind.Name)
			fs.UpdatedAt = now
			state.Files[name] = fs
			stats.Failed++
			continue
		}

		if dup, similar := isDuplicate(normName, ind.Description, skillIndex); dup >= in.cfg.SimilarityThreshold {
			fs.Status = "duplicate"
			fs.Reason = fmt.Sprintf("similar to %q (score %.2f)", similar.Name, dup)
			fs.UpdatedAt = now
			state.Files[name] = fs
			stats.Duplicates++
			continue
		}

		candDir := filepath.Join(in.cfg.CandidatesDir, normName)
		if _, err := os.Stat(candDir); err == nil {
			fs.Status = "name_collision"
			fs.Reason = "candidate dir already exists: " + normName
			fs.UpdatedAt = now
			state.Files[name] = fs
			stats.Duplicates++
			continue
		}

		if err := writeCandidate(candDir, normName, ind, rec, name, in.cfg.Model); err != nil {
			state.Files[name] = fs
			in.log.Printf("write candidate %s failed: %v", normName, err)
			continue
		}
		skillIndex = append(skillIndex, skillIndexEntry{Name: normName, Description: ind.Description})

		fs.Status = "processed"
		fs.Candidate = normName
		fs.UpdatedAt = now
		state.Files[name] = fs
		stats.Induced++
		in.log.Printf("induced candidate skill %q from %s (bead %s)", normName, name, rec.BeadID)
	}

	if err := in.saveState(state); err != nil {
		return stats, fmt.Errorf("save state: %w", err)
	}
	return stats, nil
}

const stateFileName = "state.json"

// induce runs one transcript through the LLM and parses the verdict.
func (in *Inducer) induce(ctx context.Context, rec *ExecutionRecord) (*induction, error) {
	prompt := buildUserPrompt(rec, in.cfg.MaxPromptChars)
	resp, err := in.provider.CompleteWithSystem(ctx, systemPrompt, prompt)
	if err != nil {
		return nil, err
	}
	return parseInduction(resp)
}

func (in *Inducer) statePath() string {
	return filepath.Join(in.cfg.CandidatesDir, stateFileName)
}

func (in *Inducer) loadState() (*persistedState, error) {
	st := &persistedState{Files: map[string]fileState{}}
	data, err := os.ReadFile(in.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", in.statePath(), err)
	}
	if st.Files == nil {
		st.Files = map[string]fileState{}
	}
	return st, nil
}

func (in *Inducer) saveState(st *persistedState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := in.statePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, in.statePath())
}

func readExecutionRecord(path string) (*ExecutionRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec ExecutionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("malformed record: %w", err)
	}
	return &rec, nil
}
