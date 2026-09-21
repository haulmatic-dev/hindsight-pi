package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Pack scout defaults. The pass is deliberately bounded: one scout per chain
// per build, ~20 turns, and a per-turn wall budget so a hung provider call
// cannot pin the scout past the cap.
const (
	PackScoutDefaultMaxTurns   = 20
	PackScoutDefaultTurnBudget = 60 * time.Second
	PackScoutMaxRequirements   = 16
	PackScoutMaxMappingsPerReq = 3
	PackScoutMaxSnippetLen     = 200
	PackScoutEventType         = "pack_scout"
	PackScoutDefaultProvider   = "kimi-coding"
	PackScoutDefaultModel      = "kimi-for-coding"
	PackScoutDefaultAPIKeyName = "KIMI_API_KEY"
)

// ScoutRunner executes ONE bounded agentic scout pass: read-only
// orientation of the project for the given requirements, returning
// requirement→file mappings. Injectable for tests; the production
// implementation is PiScoutRunner.
type ScoutRunner interface {
	RunScout(ctx context.Context, projectPath string, requirements []string, maxTurns int) ([]PackFileAnchor, error)
}

// ScoutConfig configures the agentic scout fallback (context_pack.scout).
type ScoutConfig struct {
	// Enabled gates the scout pass. Default false: the mechanical pack alone
	// guides chains until an operator opts into scout spend.
	Enabled bool `yaml:"enabled"`
	// MaxTurns caps the scout pass (~20). Enforced as prompt instruction
	// plus a wall-clock timeout of TurnBudget × MaxTurns.
	MaxTurns int `yaml:"max_turns"`
	// TurnBudget is the per-turn wall time used to derive the timeout.
	TurnBudget string `yaml:"turn_budget"`
	// Provider/Model/APIKeyName default to the Pi worker fleet's identity so
	// scout spend lands in the same llm-router lane accounting as workers.
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	APIKeyName string `yaml:"api_key_name"`
}

// resolvedScoutConfig carries scout settings with defaults applied.
type resolvedScoutConfig struct {
	enabled    bool
	maxTurns   int
	turnBudget time.Duration
	provider   string
	model      string
	apiKeyName string
}

func resolveScoutConfig(cfg *Config) resolvedScoutConfig {
	r := resolvedScoutConfig{
		enabled:    false,
		maxTurns:   PackScoutDefaultMaxTurns,
		turnBudget: PackScoutDefaultTurnBudget,
		provider:   PackScoutDefaultProvider,
		model:      PackScoutDefaultModel,
		apiKeyName: PackScoutDefaultAPIKeyName,
	}
	if cfg == nil {
		return r
	}
	if cfg.Pi.StandardAgent.LLM.Provider != "" {
		r.provider = cfg.Pi.StandardAgent.LLM.Provider
	}
	if cfg.Pi.StandardAgent.LLM.Model != "" {
		r.model = cfg.Pi.StandardAgent.LLM.Model
	}
	sc := cfg.ContextPack.Scout
	if sc.Provider != "" {
		r.provider = sc.Provider
	}
	if sc.Model != "" {
		r.model = sc.Model
	}
	if sc.APIKeyName != "" {
		r.apiKeyName = sc.APIKeyName
	}
	r.enabled = sc.Enabled
	if sc.MaxTurns > 0 {
		r.maxTurns = sc.MaxTurns
	}
	if sc.TurnBudget != "" {
		if d, err := time.ParseDuration(sc.TurnBudget); err == nil && d > 0 {
			r.turnBudget = d
		}
	}
	return r
}

// PiScoutRunner is the production ScoutRunner: one headless Pi invocation,
// prompt on stdin, JSON answer parsed from stdout. Same process shape as the
// worker fleet's Pi calls, so the pass rides the normal llm-router lane
// accounting. Fail-open: any error returns err and the caller proceeds with
// the mechanical-only pack.
type PiScoutRunner struct {
	Provider   string
	Model      string
	APIKeyName string
	// PiPath overrides the pi binary (tests, HYDRA_PI_PATH).
	PiPath string
	// TurnBudget overrides the per-turn wall time used to derive the timeout.
	TurnBudget time.Duration
	// Env is prepended to the process environment (tests inject keys).
	Env []string
}

// RunScout runs one bounded scout pass. The wall-clock timeout is
// maxTurns × turnBudget — the turn cap made enforceable.
func (r *PiScoutRunner) RunScout(ctx context.Context, projectPath string, requirements []string, maxTurns int) ([]PackFileAnchor, error) {
	if len(requirements) == 0 {
		return nil, nil
	}
	if maxTurns <= 0 {
		maxTurns = PackScoutDefaultMaxTurns
	}
	turnBudget := r.TurnBudget
	if turnBudget <= 0 {
		turnBudget = PackScoutDefaultTurnBudget
	}
	timeout := time.Duration(maxTurns) * turnBudget
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	piPath := r.PiPath
	if piPath == "" {
		piPath = os.Getenv("HYDRA_PI_PATH")
	}
	if piPath == "" {
		piPath = "pi"
	}

	args := []string{"--offline"}
	if r.Provider != "" {
		args = append(args, "--provider", r.Provider)
	}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}

	cmd := exec.CommandContext(ctx, piPath, args...)
	cmd.Dir = projectPath
	cmd.Stdin = strings.NewReader(buildScoutPrompt(projectPath, requirements, maxTurns))
	cmd.Env = append(os.Environ(), r.Env...)
	if r.APIKeyName != "" {
		if key := os.Getenv(r.APIKeyName); key != "" {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", r.APIKeyName, key))
		}
	}
	// Process group + explicit group kill: CommandContext only kills the
	// direct child, and a killed shell leaves its pipeline children holding
	// the stdout pipe open — Run() then blocks until they exit, defeating the
	// turn cap. Killing the group reaps the whole tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	startErr := cmd.Start()
	if startErr == nil {
		go func() {
			<-ctx.Done()
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}()
		err := cmd.Wait()
		if err != nil {
			tail := strings.TrimSpace(stderr.String())
			if len(tail) > 200 {
				tail = tail[len(tail)-200:]
			}
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("scout timed out after %s (turn cap %d)", timeout, maxTurns)
			}
			return nil, fmt.Errorf("pi scout exited: %w (stderr: %s)", err, tail)
		}
		return parseScoutOutput(stdout.String())
	}
	// Start itself failed — surface that directly.
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("scout timed out after %s (turn cap %d)", timeout, maxTurns)
	}
	return nil, fmt.Errorf("pi scout start failed: %w", startErr)
}

// buildScoutPrompt is the read-only orientation brief. The JSON schema is
// repeated verbatim in the answer contract; the model is told to spend its
// budget on reading, not writing.
func buildScoutPrompt(projectPath string, requirements []string, maxTurns int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `You are a read-only orientation scout for a coding project at %s.
Your ONLY job: map each requirement below to the file(s) most likely to need
changes. Do NOT edit, create, or delete anything. Do NOT run tests or builds.

Requirements (JSON strings):
%s

Rules:
- Spend at most %d turns total. Prefer breadth: list dirs, grep, read a few
  files, then answer. Stop early once every requirement has a mapping.
- For each requirement output 1-%d mappings, best first. Confidence is your
  calibrated 0..1 estimate that the file is the primary change target.
- "anchor_snippet" is one short verbatim line from the file (max %d chars)
  justifying the mapping (a symbol, route, or comment).
- If a requirement has no plausible target, map it to the single most
  relevant directory with confidence <= 0.4 instead of omitting it.

Your FINAL message must be exactly one JSON array and nothing else:
[{"requirement": "<requirement>", "path": "<repo-relative path>", "symbol": "<symbol or ''>", "confidence": 0.0, "anchor_snippet": "<verbatim line>"}]
`,
		filepath.Base(projectPath),
		mustJSON(requirements),
		maxTurns,
		PackScoutMaxMappingsPerReq,
		PackScoutMaxSnippetLen,
	)
	return sb.String()
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// parseScoutOutput extracts the LAST JSON array from the model output and
// normalizes it into anchors. Tolerant by design: prose around the array is
// ignored; unparseable output yields an error (fail-open at the caller).
func parseScoutOutput(output string) ([]PackFileAnchor, error) {
	// Find the last '[' that starts a JSON array; bracket-match to its close.
	start := strings.LastIndex(output, "[")
	if start < 0 {
		return nil, fmt.Errorf("no JSON array in scout output")
	}
	depth := 0
	end := -1
	inString := false
	escaped := false
	for i := start; i < len(output); i++ {
		c := output[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if end <= 0 {
		return nil, fmt.Errorf("unterminated JSON array in scout output")
	}
	var raw []struct {
		Requirement string  `json:"requirement"`
		Path        string  `json:"path"`
		Symbol      string  `json:"symbol"`
		Confidence  float64 `json:"confidence"`
		Snippet     string  `json:"anchor_snippet"`
	}
	if err := json.Unmarshal([]byte(output[start:end]), &raw); err != nil {
		return nil, fmt.Errorf("scout output JSON unparseable: %w", err)
	}
	var anchors []PackFileAnchor
	for _, m := range raw {
		req := strings.TrimSpace(m.Requirement)
		path := filepath.ToSlash(strings.TrimSpace(m.Path))
		if req == "" || path == "" {
			continue
		}
		conf := m.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		if len(m.Snippet) > PackScoutMaxSnippetLen {
			m.Snippet = m.Snippet[:PackScoutMaxSnippetLen]
		}
		anchors = append(anchors, PackFileAnchor{
			Requirement: req,
			Path:        path,
			Symbol:      strings.TrimSpace(m.Symbol),
			Confidence:  conf,
			Snippet:     strings.TrimSpace(m.Snippet),
			Provenance:  PackProvenanceAgentic,
		})
	}
	if len(anchors) == 0 {
		return nil, fmt.Errorf("scout output contained no usable mappings")
	}
	return anchors, nil
}

// UnmappedRequirements returns the chain-member titles that the mechanical
// anchors did not map at or above threshold: no anchor for ANY of the
// title's requirement terms cleared PackScoutConfidenceThreshold. These are
// the scout-eligible requirements.
func UnmappedRequirements(titles []string, anchors []PackFileAnchor, threshold float64) []string {
	mapped := map[string]bool{}
	for _, a := range anchors {
		if a.Confidence >= threshold {
			mapped[strings.ToLower(a.Requirement)] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		covered := false
		for _, term := range RequirementTermsForBead(title) {
			if mapped[strings.ToLower(term)] {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, title)
		}
	}
	if len(out) > PackScoutMaxRequirements {
		out = out[:PackScoutMaxRequirements]
	}
	return out
}

// MergeScoutMappings folds scout output into the pack's Files list:
// existing agentic anchors for the re-scouted requirements are replaced,
// new mappings are appended (provenance forced to agentic, deduped by
// requirement+path), and everything else is untouched. Pure function — the
// caller persists.
func MergeScoutMappings(pack *ContextPack, mappings []PackFileAnchor, reScouted []string) {
	if pack == nil {
		return
	}
	replaced := map[string]bool{}
	for _, r := range reScouted {
		replaced[strings.ToLower(strings.TrimSpace(r))] = true
	}
	kept := pack.Files[:0]
	for _, a := range pack.Files {
		if a.Provenance == PackProvenanceAgentic && replaced[strings.ToLower(a.Requirement)] {
			continue
		}
		kept = append(kept, a)
	}
	pack.Files = kept

	seen := map[string]bool{}
	for _, a := range pack.Files {
		seen[strings.ToLower(a.Requirement)+"\x00"+a.Path] = true
	}
	for _, m := range mappings {
		m.Provenance = PackProvenanceAgentic
		key := strings.ToLower(m.Requirement) + "\x00" + m.Path
		if m.Path == "" || seen[key] {
			continue
		}
		seen[key] = true
		pack.Files = append(pack.Files, m)
	}
}

// DroppedAgenticRequirements groups dropped anchors by requirement for the
// targeted re-scout: only requirements that HAD agentic anchors that broke
// are returned — mechanical drops and never-scouted requirements never
// trigger a scout pass.
func DroppedAgenticRequirements(dropped []PackFileAnchor) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range dropped {
		if a.Provenance != PackProvenanceAgentic {
			continue
		}
		req := strings.TrimSpace(a.Requirement)
		if req == "" || seen[req] {
			continue
		}
		seen[req] = true
		out = append(out, req)
	}
	return out
}

// scoutGate serializes scout passes daemon-wide. Scouts are rare (one per
// chain build, targeted re-scouts after) but expensive; overlapping spawns
// of the same chain must not double-spend.
var scoutGate sync.Mutex

// emitPackScout logs the typed pack_scout event (cost attribution). Nil-sink
// safe; never gates.
func emitPackScout(sink *EventSink, project, bead string, requirements int, mappings int, d time.Duration, err error) {
	if sink == nil {
		return
	}
	detail := fmt.Sprintf("scout pass: %d requirements, %d mappings, %s", requirements, mappings, d.Round(time.Second))
	reason := "scout_ok"
	if err != nil {
		reason = "scout_failed"
		detail = fmt.Sprintf("scout pass failed (fail-open, mechanical-only pack): %v", err)
	}
	sink.Emit(Event{Type: PackScoutEventType, Project: project, Bead: bead, Reason: reason, Detail: detail})
	log.Printf("[ContextPack] pack_scout (%s): %s", reason, detail)
}
