package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// WorkerEnvBuilder builds environment variables for worker processes.
type WorkerEnvBuilder struct {
	config *Config
}

// NewWorkerEnvBuilder creates a new worker environment builder.
func NewWorkerEnvBuilder(config *Config) *WorkerEnvBuilder {
	return &WorkerEnvBuilder{config: config}
}

// Build creates the environment variable slice for a worker process.
func (b *WorkerEnvBuilder) Build(bead types.Bead, projectPath string, agentType string, retryCount int) []string {
	env := append(os.Environ(),
		"HYDRA_WORKER=true",
		fmt.Sprintf("HYDRA_BEAD_ID=%s", bead.ID),
		fmt.Sprintf("HYDRA_PROJECT=%s", bead.Project),
		fmt.Sprintf("HYDRA_PROJECT_PATH=%s", projectPath),
		fmt.Sprintf("HYDRA_REPO=%s", bead.Repo),
		fmt.Sprintf("HYDRA_AGENT_TYPE=%s", agentType),
		fmt.Sprintf("HYDRA_RETRY_COUNT=%d", retryCount),
		fmt.Sprintf("HYDRA_BEAD_TITLE=%s", bead.Title),
		fmt.Sprintf("HYDRA_BEAD_DESCRIPTION=%s", bead.Description),
	)

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
			fmt.Sprintf("HYDRA_HISTORIESIGHT_MIN_SCORE=%.2f", h.MinScore),
		)
		log.Printf("[Worker] Hindsight config passed to worker (baseURL: %s)", h.BaseURL)
	}

	// Add Pi configuration
	env = append(env, b.buildPiEnv()...)

	return env
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
			fmt.Sprintf("HYDRA_HISTORIESIGHT_MIN_SCORE=%.2f", h.MinScore),
		)
	}

	env = append(env, b.buildPiEnv()...)

	return env
}

func (b *WorkerEnvBuilder) buildPiEnv() []string {
	if b.config == nil {
		return nil
	}

	p := b.config.Agents.Pi
	std := b.config.Pi.StandardAgent.LLM

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

	log.Printf("[Worker] Pi config passed to worker (provider: %s, model: %s)", p.Provider, p.Model)

	return []string{
		"HYDRA_PI_ENABLED=true",
		fmt.Sprintf("HYDRA_PI_PROVIDER=%s", p.Provider),
		fmt.Sprintf("HYDRA_PI_MODEL=%s", p.Model),
		fmt.Sprintf("HYDRA_PI_THINKING_LEVEL=%s", p.ThinkingLevel),
		fmt.Sprintf("HYDRA_PI_TIMEOUT=%s", p.Timeout),
		fmt.Sprintf("HYDRA_PI_API_KEY_NAME=%s", p.ApiKeyName),
		fmt.Sprintf("HYDRA_PI_CONFIG_DIR=%s", b.config.Pi.ConfigDir),
	}
}
