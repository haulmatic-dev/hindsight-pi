package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerEnvBuilder builds environment variables for worker processes.
type WorkerEnvBuilder struct {
	config  *Config
	breaker *ProviderBreaker
}

// NewWorkerEnvBuilder creates a new worker environment builder.
func NewWorkerEnvBuilder(config *Config) *WorkerEnvBuilder {
	return &WorkerEnvBuilder{config: config}
}

// SetProviderBreaker wires the provider circuit breaker consulted by
// buildPiEnv for rate-limit failover. Safe to leave unset (no failover).
func (b *WorkerEnvBuilder) SetProviderBreaker(pb *ProviderBreaker) {
	b.breaker = pb
}

// Build creates the environment variable slice for a worker process.
func (b *WorkerEnvBuilder) Build(bead types.Bead, projectPath string, agentType string, retryCount int) []string {
	env := append(os.Environ(),
		"HYDRA_WORKER=true",
		// Skip Pi's startup network operations (provider-list fetch). That
		// fetch flakes when many workers launch Pi concurrently and reports a
		// spurious "Unknown provider" for a valid provider; offline mode uses
		// the local models.json cache instead. Verified: 3 concurrent offline
		// Pi calls all succeed where online mode failed.
		"PI_OFFLINE=1",
		fmt.Sprintf("HYDRA_BEAD_ID=%s", bead.ID),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_PROJECT_PATH=%s", projectPath),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
		fmt.Sprintf("HYDRA_RETRY_COUNT=%d", retryCount),
		fmt.Sprintf("HYDRA_BEAD_TITLE=%s", bead.Title),
		fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", bead.Description),
	)

	// Daemon registry API: workers self-report state and poll shepherd
	// commands (PRD worker-park-and-resume Part 4). Loopback only; the worker
	// treats an unreachable registry as disabled (fail-open).
	if b.config.API.Enabled && b.config.API.Port > 0 {
		env = append(env, fmt.Sprintf("HYDRA_API_URL=http://127.0.0.1:%d", b.config.API.Port))
	}

	// Add full bead JSON for enhanced worker context
	if bead.ID != "" {
		cleanBead := bead
		cleanDeps := make([]types.BeadID, 0, len(bead.Dependencies))
		for _, dep := range bead.Dependencies {
			if dep != "" {
				cleanDeps = append(cleanDeps, dep)
			}
		}
		cleanBead.Dependencies = cleanDeps
		beadJSON, err := json.Marshal(cleanBead)
		if err == nil {
			env = append(env, fmt.Sprintf("HYDRA_BEAD_JSON=%s", string(beadJSON)))
		} else {
			log.Printf("[WARN] Failed to marshal bead to JSON: %v", err)
		}
	}

	// Pass API key to worker if configured (with fallback to pi.standard-agent.llm)
	apiKeyName := b.config.Agents.Pi.ApiKeyName
	if apiKeyName == "" && b.config.Pi.StandardAgent.LLM.ApiKeyName != "" {
		apiKeyName = b.config.Pi.StandardAgent.LLM.ApiKeyName
	}
	if apiKeyName == "" {
		apiKeyName = "KIMI_API_KEY"
	}
	if apiKey := os.Getenv(apiKeyName); apiKey != "" {
		env = append(env, fmt.Sprintf("%s=%s", apiKeyName, apiKey))
	}

	// Add Hindsight configuration
	if b.config != nil && b.config.Intelligence.Hindsight.Enabled {
		h := b.config.Intelligence.Hindsight
		env = append(env,
			"HYDRA_HISTORIESIGHT_ENABLED=true",
			fmt.Sprintf("HYDRA_HISTORIESIGHT_BASE_URL=%s", h.BaseURL),
			fmt.Sprintf("HYDRA_HISTORIESIGHT_TIMEOUT=%s", h.Timeout),
			fmt.Sprintf("HYDRA_HISTORIESIGHT_LIMIT=%d", h.Limit),
			fmt.Sprintf("HYDRA_HISTORIESIGHT_MAX_TOKENS=%d", h.MaxTokens),
		)
		log.Printf("[Worker] Hindsight config passed to worker (baseURL: %s)", h.BaseURL)
	}

	// Close-time pattern extraction: verdict → pattern store (fail-open).
	if b.config != nil && b.config.Intelligence.Patterns.Enabled {
		p := b.config.Intelligence.Patterns
		env = append(env,
			"HYDRA_PATTERNS_ENABLED=true",
			fmt.Sprintf("HYDRA_PATTERNS_STORAGE_PATH=%s", p.StoragePath),
			fmt.Sprintf("HYDRA_PATTERNS_MIN_CONFIDENCE=%g", p.MinConfidence),
			fmt.Sprintf("HYDRA_PATTERNS_MAX_PATTERNS_PER_BEAD=%d", p.MaxPatternsPerBead),
		)
	}

	// Add Pi configuration
	env = append(env, b.buildPiEnv(bead)...)

	return env
}

// beadPhase derives a TDD phase key from the bead's title or type.
// It returns one of "red", "green", "refactor", "syntax", "discovery",
// "fix", or empty string when the phase cannot be determined.
func beadPhase(bead types.Bead) string {
	title := strings.ToUpper(strings.TrimSpace(bead.Title))
	switch {
	case strings.HasPrefix(title, "RED:"):
		return "red"
	case strings.HasPrefix(title, "GREEN:"):
		return "green"
	case strings.HasPrefix(title, "REFACTOR:"):
		return "refactor"
	case strings.HasPrefix(title, "SYNTAX:"):
		return "syntax"
	case strings.HasPrefix(title, "DISCOVERY:"):
		return "discovery"
	case strings.HasPrefix(title, "FIX:"):
		return "fix"
	case strings.EqualFold(bead.Type, "fix"):
		return "fix"
	default:
		return ""
	}
}

// BuildForContextEnrichment builds a minimal environment for context-enriched workers.
func (b *WorkerEnvBuilder) BuildForContextEnrichment(bead types.Bead) []string {
	env := []string{
		fmt.Sprintf("HYDRA_BEAD_ID=%s", bead.ID),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_BEAD_TITLE=%s", bead.Title),
		fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", bead.Description),
	}

	if b.config != nil && b.config.Intelligence.Hindsight.Enabled {
		h := b.config.Intelligence.Hindsight
		env = append(env,
			"HYDRA_HISTORIESIGHT_ENABLED=true",
			fmt.Sprintf("HYDRA_HISTORIESIGHT_BASE_URL=%s", h.BaseURL),
			fmt.Sprintf("HYDRA_HISTORIESIGHT_TIMEOUT=%s", h.Timeout),
			fmt.Sprintf("HYDRA_HISTORIESIGHT_LIMIT=%d", h.Limit),
			fmt.Sprintf("HYDRA_HISTORIESIGHT_MAX_TOKENS=%d", h.MaxTokens),
		)
	}

	env = append(env, b.buildPiEnv(bead)...)

	return env
}

func (b *WorkerEnvBuilder) buildPiEnv(bead types.Bead) []string {
	if b.config == nil {
		return nil
	}

	p := b.config.Agents.Pi
	std := b.config.Pi.StandardAgent.LLM

	// Phase routing: apply a per-phase provider/model override before any
	// standard-agent fallbacks or hard defaults. The breaker is evaluated
	// last so it can still override a phase-routed provider when its circuit
	// is open.
	phase := beadPhase(bead)
	if phase != "" {
		provider, model := p.ModelForPhase(phase)
		if provider != p.Provider || model != p.Model {
			log.Printf("[Worker] Phase routing: %s -> %s/%s", phase, provider, model)
		}
		p.Provider = provider
		p.Model = model
	}

	if p.Provider == "" && std.Provider != "" {
		p.Provider = std.Provider
	}
	if p.Model == "" && std.Model != "" {
		p.Model = std.Model
	}
	if p.ThinkingLevel == "" && std.ThinkingLevel != "" {
		p.ThinkingLevel = std.ThinkingLevel
	}
	if p.Timeout == "" && std.Timeout != "" {
		p.Timeout = std.Timeout
	}
	if p.ApiKeyName == "" && std.ApiKeyName != "" {
		p.ApiKeyName = std.ApiKeyName
	}
	if p.Provider == "" {
		p.Provider = "kimi-coding"
	}
	if p.Model == "" {
		p.Model = "kimi-for-coding"
	}
	if p.ThinkingLevel == "" {
		p.ThinkingLevel = "on"
	}
	if p.Timeout == "" {
		p.Timeout = "10m"
	}
	if p.ApiKeyName == "" {
		p.ApiKeyName = "KIMI_API_KEY"
	}

	// Provider failover: when the selected provider's circuit is open due to
	// repeated rate-limit (429) failures, use the configured fallback.
	if b.breaker != nil {
		if prov, model := b.breaker.Override(p.Provider, p.Model); prov != "" && prov != p.Provider {
			log.Printf("[Worker] Provider failover: %s -> %s (circuit open)", p.Provider, prov)
			p.Provider = prov
			if model != "" {
				p.Model = model
			}
		}
	}

	log.Printf("[Worker] Pi config passed to worker (provider: %s, model: %s)", p.Provider, p.Model)

	// Use worker-specific Pi config directory when configured. This allows
	// workers to load a stripped-down Pi config (e.g. without context-mode)
	// for faster startup, while the main Hydra process retains the full
	// interactive Pi config.
	piConfigDir := b.config.Pi.ConfigDir
	if b.config.Pi.WorkerConfigDir != "" {
		piConfigDir = b.config.Pi.WorkerConfigDir
	}

	return []string{
		"HYDRA_PI_ENABLED=true",
		fmt.Sprintf("HYDRA_PI_PROVIDER=%s", p.Provider),
		fmt.Sprintf("HYDRA_PI_MODEL=%s", p.Model),
		fmt.Sprintf("HYDRA_PI_THINKING_LEVEL=%s", p.ThinkingLevel),
		fmt.Sprintf("HYDRA_PI_TIMEOUT=%s", p.Timeout),
		fmt.Sprintf("HYDRA_PI_API_KEY_NAME=%s", p.ApiKeyName),
		fmt.Sprintf("HYDRA_PI_CONFIG_DIR=%s", piConfigDir),
		// Protocol selection. Without these two the worker cannot know the
		// daemon's agents.pi.mode at all: workers are separate processes
		// configured purely through this env block, so `mode: rpc` in
		// hydra.yaml was inert until it was forwarded here.
		fmt.Sprintf("HYDRA_PI_MODE=%s", p.Mode),
		fmt.Sprintf("HYDRA_PI_RPC_AUTO_CONFIRM=%t", p.AutoConfirm()),
		// Extension allowlist for this bead's phase. Comma-separated so it
		// survives the env hand-off; the worker resolves names to entry paths
		// because only it knows the resolved package root.
		fmt.Sprintf("HYDRA_PI_EXTENSIONS=%s", strings.Join(p.ExtensionsForPhase(phase), ",")),
	}
}
