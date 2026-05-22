// Package hindsight provides post-execution memory retention for Hydra workers.
// This file implements LLM provider abstraction for the Hindsight memory service.
package hindsight

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// LLMProvider defines the interface for LLM providers used by Hindsight.
// This abstraction allows Hindsight to work with different LLM backends.
type LLMProvider interface {
	// Complete sends a prompt to the LLM and returns the completion.
	Complete(ctx context.Context, prompt string) (string, error)

	// CompleteWithSystem sends a prompt with a system message to the LLM.
	CompleteWithSystem(ctx context.Context, system, prompt string) (string, error)

	// Health checks if the provider is healthy and accessible.
	Health(ctx context.Context) error
}

// LLMConfig contains configuration for LLM provider setup.
type LLMConfig struct {
	// Provider is the LLM provider name (e.g., "openai", "anthropic", "kimi")
	Provider string `yaml:"provider"`

	// Model is the specific model to use (e.g., "gpt-4", "claude-3", "kimi-for-coding")
	Model string `yaml:"model"`

	// BaseURL is the base URL for the LLM API (optional, uses provider default if empty)
	BaseURL string `yaml:"base_url"`

	// Timeout for LLM requests
	Timeout time.Duration `yaml:"timeout"`

	// MaxTokens is the maximum number of tokens to generate
	MaxTokens int `yaml:"max_tokens"`

	// Temperature controls randomness (0.0-2.0)
	Temperature float64 `yaml:"temperature"`

	// APIKeyEnv is the environment variable name containing the API key.
	// Default is "OPENAI_API_KEY" for OpenAI, "ANTHROPIC_API_KEY" for Anthropic, etc.
	APIKeyEnv string `yaml:"llm_api_key_env"`
}

// DefaultLLMConfig returns a default LLM configuration.
func DefaultLLMConfig() LLMConfig {
	return LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4",
		Timeout:     30 * time.Second,
		MaxTokens:   2048,
		Temperature: 0.7,
		APIKeyEnv:   "OPENAI_API_KEY",
	}
}

// getAPIKeyEnv returns the effective API key environment variable name.
// Returns the configured value or the default "OPENAI_API_KEY".
func (c *LLMConfig) getAPIKeyEnv() string {
	if c.APIKeyEnv != "" {
		return c.APIKeyEnv
	}
	return "OPENAI_API_KEY"
}

// GetAPIKey retrieves the API key from the configured environment variable.
// Returns an error if the environment variable is not set or empty.
func (c *LLMConfig) GetAPIKey() (string, error) {
	envVar := c.getAPIKeyEnv()
	apiKey := os.Getenv(envVar)
	if apiKey == "" {
		return "", fmt.Errorf("API key environment variable %s is not set", envVar)
	}
	return apiKey, nil
}

// LogKeyInfo logs information about the API key without exposing its value.
// Only the key length is logged for debugging purposes.
func (c *LLMConfig) LogKeyInfo() {
	envVar := c.getAPIKeyEnv()
	apiKey := os.Getenv(envVar)
	if apiKey == "" {
		log.Printf("[LLM] Warning: API key environment variable %s is not set", envVar)
		return
	}
	// Only log the length, never the actual key value
	log.Printf("[LLM] API key loaded from %s (%d characters)", envVar, len(apiKey))
}

// SanitizedKeyInfo returns a sanitized string describing the API key status.
// This is safe to log or include in error messages.
func (c *LLMConfig) SanitizedKeyInfo() string {
	envVar := c.getAPIKeyEnv()
	apiKey := os.Getenv(envVar)
	if apiKey == "" {
		return fmt.Sprintf("API key not set (expected in %s)", envVar)
	}
	// Only return the length, never the actual key
	return fmt.Sprintf("API key loaded from %s (%d chars)", envVar, len(apiKey))
}

// OpenAIProvider implements LLMProvider for OpenAI-compatible APIs.
type OpenAIProvider struct {
	config     LLMConfig
	apiKey     string
	httpClient *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider with the given configuration.
// The API key is read from the environment variable specified in config.APIKeyEnv.
func NewOpenAIProvider(config LLMConfig) (*OpenAIProvider, error) {
	apiKey, err := config.GetAPIKey()
	if err != nil {
		return nil, err
	}

	// Log key info securely (only length)
	config.LogKeyInfo()

	return &OpenAIProvider{
		config: config,
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: effectiveTimeout(config.Timeout),
		},
	}, nil
}

// effectiveTimeout returns the timeout value or a default if zero.
func effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout == 0 {
		return 30 * time.Second
	}
	return timeout
}

// OpenAIRequest represents a request to the OpenAI API.
type OpenAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

// Message represents a chat message for OpenAI API.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIResponse represents a response from the OpenAI API.
type OpenAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Complete sends a prompt to the OpenAI API and returns the completion.
func (p *OpenAIProvider) Complete(ctx context.Context, prompt string) (string, error) {
	return p.CompleteWithSystem(ctx, "", prompt)
}

// CompleteWithSystem sends a prompt with a system message to the OpenAI API.
func (p *OpenAIProvider) CompleteWithSystem(ctx context.Context, system, prompt string) (string, error) {
	messages := []Message{}

	if system != "" {
		messages = append(messages, Message{
			Role:    "system",
			Content: system,
		})
	}

	messages = append(messages, Message{
		Role:    "user",
		Content: prompt,
	})

	reqBody := OpenAIRequest{
		Model:       p.config.Model,
		Messages:    messages,
		MaxTokens:   p.config.MaxTokens,
		Temperature: p.config.Temperature,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	url := fmt.Sprintf("%s/chat/completions", baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result OpenAIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s (%s)", result.Error.Message, result.Error.Type)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return result.Choices[0].Message.Content, nil
}

// Health checks if the OpenAI provider is healthy.
func (p *OpenAIProvider) Health(ctx context.Context) error {
	// Simple health check by making a minimal request
	// In production, this might use the models endpoint
	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := p.Complete(testCtx, "test")
	if err != nil {
		// If the error is not related to auth or connectivity, it's likely healthy
		if !strings.Contains(err.Error(), "connection") && !strings.Contains(err.Error(), "timeout") {
			return nil
		}
		return err
	}
	return nil
}

// LLMProviderFactory creates LLM providers based on configuration.
type LLMProviderFactory struct {
	defaultConfig LLMConfig
}

// NewLLMProviderFactory creates a new provider factory with default configuration.
func NewLLMProviderFactory(config LLMConfig) *LLMProviderFactory {
	return &LLMProviderFactory{
		defaultConfig: config,
	}
}

// CreateProvider creates an LLM provider based on the provider name in configuration.
func (f *LLMProviderFactory) CreateProvider(config LLMConfig) (LLMProvider, error) {
	// Merge with defaults
	merged := f.mergeWithDefaults(config)

	switch strings.ToLower(merged.Provider) {
	case "openai", "kimi":
		// Kimi uses OpenAI-compatible API
		if merged.Provider == "kimi" && merged.BaseURL == "" {
			merged.BaseURL = "https://api.moonshot.cn/v1"
		}
		return NewOpenAIProvider(merged)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", merged.Provider)
	}
}

// mergeWithDefaults merges the provided config with factory defaults.
func (f *LLMProviderFactory) mergeWithDefaults(config LLMConfig) LLMConfig {
	if config.Provider == "" {
		config.Provider = f.defaultConfig.Provider
	}
	if config.Model == "" {
		config.Model = f.defaultConfig.Model
	}
	if config.Timeout == 0 {
		config.Timeout = f.defaultConfig.Timeout
	}
	if config.MaxTokens == 0 {
		config.MaxTokens = f.defaultConfig.MaxTokens
	}
	if config.Temperature == 0 {
		config.Temperature = f.defaultConfig.Temperature
	}
	if config.APIKeyEnv == "" {
		config.APIKeyEnv = f.defaultConfig.APIKeyEnv
	}
	return config
}

// ProviderOption is a functional option for configuring LLM providers.
type ProviderOption func(*LLMConfig)

// WithProvider sets the LLM provider.
func WithProvider(provider string) ProviderOption {
	return func(c *LLMConfig) {
		c.Provider = provider
	}
}

// WithModel sets the LLM model.
func WithModel(model string) ProviderOption {
	return func(c *LLMConfig) {
		c.Model = model
	}
}

// WithLLMBaseURL sets the API base URL for the LLM provider.
func WithLLMBaseURL(url string) ProviderOption {
	return func(c *LLMConfig) {
		c.BaseURL = url
	}
}

// WithLLMTimeout sets the request timeout for the LLM provider.
func WithLLMTimeout(timeout time.Duration) ProviderOption {
	return func(c *LLMConfig) {
		c.Timeout = timeout
	}
}

// WithAPIKeyEnv sets the environment variable name for the API key.
// This is the configurable setting mentioned in FR-026B.
func WithAPIKeyEnv(envVar string) ProviderOption {
	return func(c *LLMConfig) {
		c.APIKeyEnv = envVar
	}
}

// NewLLMProvider creates an LLM provider with the given options.
// This is a convenience function for simple provider creation.
func NewLLMProvider(opts ...ProviderOption) (LLMProvider, error) {
	config := DefaultLLMConfig()
	for _, opt := range opts {
		opt(&config)
	}

	factory := NewLLMProviderFactory(config)
	return factory.CreateProvider(config)
}

// APIKeyLength returns the length of the API key without exposing its value.
// This is useful for logging and diagnostics.
func APIKeyLength(config LLMConfig) int {
	envVar := config.APIKeyEnv
	if envVar == "" {
		envVar = "OPENAI_API_KEY"
	}

	apiKey := os.Getenv(envVar)
	return len(apiKey)
}

// IsAPIKeyConfigured checks if an API key is configured for the given config.
// Returns true if the environment variable is set and non-empty.
func IsAPIKeyConfigured(config LLMConfig) bool {
	envVar := config.APIKeyEnv
	if envVar == "" {
		envVar = "OPENAI_API_KEY"
	}

	apiKey := os.Getenv(envVar)
	return apiKey != ""
}
