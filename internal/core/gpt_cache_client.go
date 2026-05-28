package core

import (
	"fmt"
	"net/http"
	"time"
)

// GPTCacheClient provides access to GPTCache service.
type GPTCacheClient struct {
	serverURL  string
	httpClient *http.Client
}

// NewGPTCacheClient creates a new GPTCache client.
func NewGPTCacheClient(serverURL string, timeout time.Duration) *GPTCacheClient {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &GPTCacheClient{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// IsHealthy checks if the GPTCache service is available.
func (c *GPTCacheClient) IsHealthy() bool {
	if c.serverURL == "" {
		return false
	}

	url := fmt.Sprintf("%s/health", c.serverURL)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
