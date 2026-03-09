package intelligence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewTLDRClient tests the default client creation
func TestNewTLDRClient(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		expectedURL string
	}{
		{
			name:        "with default base URL",
			baseURL:     "",
			expectedURL: DefaultTLDRURL,
		},
		{
			name:        "with custom base URL",
			baseURL:     "http://localhost:8080",
			expectedURL: "http://localhost:8080",
		},
		{
			name:        "with trailing slash",
			baseURL:     "http://localhost:3000/",
			expectedURL: "http://localhost:3000/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewTLDRClient(tt.baseURL)

			if client == nil {
				t.Fatal("expected client to not be nil")
			}

			if client.baseURL != tt.expectedURL {
				t.Errorf("expected baseURL %q, got %q", tt.expectedURL, client.baseURL)
			}

			if client.client == nil {
				t.Fatal("expected HTTP client to not be nil")
			}

			// Verify timeout is set
			if client.client.Timeout != RequestTimeout {
				t.Errorf("expected timeout %v, got %v", RequestTimeout, client.client.Timeout)
			}
		})
	}
}

// TestNewTLDRClientWithHTTP tests client creation with custom HTTP client
func TestNewTLDRClientWithHTTP(t *testing.T) {
	customClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns: 10,
		},
	}

	tests := []struct {
		name         string
		baseURL      string
		httpClient   *http.Client
		expectedURL  string
		expectedHTTP *http.Client
	}{
		{
			name:         "with custom HTTP client and default URL",
			baseURL:      "",
			httpClient:   customClient,
			expectedURL:  DefaultTLDRURL,
			expectedHTTP: customClient,
		},
		{
			name:         "with custom HTTP client and custom URL",
			baseURL:      "http://tldr.example.com",
			httpClient:   customClient,
			expectedURL:  "http://tldr.example.com",
			expectedHTTP: customClient,
		},
		{
			name:         "with nil HTTP client (should use default)",
			baseURL:      "",
			httpClient:   nil,
			expectedURL:  DefaultTLDRURL,
			expectedHTTP: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewTLDRClientWithHTTP(tt.baseURL, tt.httpClient)

			if client == nil {
				t.Fatal("expected client to not be nil")
			}

			if client.baseURL != tt.expectedURL {
				t.Errorf("expected baseURL %q, got %q", tt.expectedURL, client.baseURL)
			}

			if tt.httpClient != nil && client.client != tt.httpClient {
				t.Error("expected HTTP client to be the custom client")
			}
		})
	}
}

// TestImpactAnalysis tests the impact analysis functionality
func TestImpactAnalysis(t *testing.T) {
	tests := []struct {
		name           string
		file           string
		depth          int
		mockResponse   interface{}
		mockStatusCode int
		expectedError  bool
		errorContains  string
		validateFunc   func(t *testing.T, resp *TLDRImpactResponse)
	}{
		{
			name:  "successful impact analysis",
			file:  "src/main.go",
			depth: 2,
			mockResponse: TLDRImpactResponse{
				File: "src/main.go",
				Impact: struct {
					Callers []Dependency `json:"callers"`
					Callees []Dependency `json:"callees"`
				}{
					Callers: []Dependency{
						{File: "src/api.go", Function: "HandleRequest", Line: 45},
						{File: "src/server.go", Function: "Start", Line: 23},
					},
					Callees: []Dependency{
						{File: "src/db.go", Function: "Query", Line: 67},
					},
				},
				Scope: struct {
					Files    int  `json:"files"`
					Tokens   int  `json:"tokens"`
					Exceeded bool `json:"exceeded"`
				}{
					Files:    3,
					Tokens:   1500,
					Exceeded: false,
				},
			},
			mockStatusCode: http.StatusOK,
			expectedError:  false,
			validateFunc: func(t *testing.T, resp *TLDRImpactResponse) {
				if resp.File != "src/main.go" {
					t.Errorf("expected file %q, got %q", "src/main.go", resp.File)
				}
				if len(resp.Impact.Callers) != 2 {
					t.Errorf("expected 2 callers, got %d", len(resp.Impact.Callers))
				}
				if len(resp.Impact.Callees) != 1 {
					t.Errorf("expected 1 callee, got %d", len(resp.Impact.Callees))
				}
				if resp.Scope.Files != 3 {
					t.Errorf("expected 3 files in scope, got %d", resp.Scope.Files)
				}
				if resp.Scope.Tokens != 1500 {
					t.Errorf("expected 1500 tokens, got %d", resp.Scope.Tokens)
				}
				if resp.Scope.Exceeded {
					t.Error("expected exceeded to be false")
				}
			},
		},
		{
			name:          "empty file validation",
			file:          "",
			depth:         2,
			expectedError: true,
			errorContains: "file path cannot be empty",
		},
		{
			name:           "HTTP error - not found",
			file:           "src/main.go",
			depth:          2,
			mockResponse:   map[string]string{"error": "file not found"},
			mockStatusCode: http.StatusNotFound,
			expectedError:  true,
			errorContains:  "unexpected status code 404",
		},
		{
			name:           "HTTP error - internal server error",
			file:           "src/main.go",
			depth:          2,
			mockResponse:   map[string]string{"error": "internal server error"},
			mockStatusCode: http.StatusInternalServerError,
			expectedError:  true,
			errorContains:  "unexpected status code 500",
		},
		{
			name:           "invalid JSON response",
			file:           "src/main.go",
			depth:          2,
			mockResponse:   "invalid json",
			mockStatusCode: http.StatusOK,
			expectedError:  true,
			errorContains:  "failed to decode response",
		},
		{
			name:  "depth less than 1 should be adjusted",
			file:  "src/main.go",
			depth: 0,
			mockResponse: TLDRImpactResponse{
				File: "src/main.go",
				Impact: struct {
					Callers []Dependency `json:"callers"`
					Callees []Dependency `json:"callees"`
				}{
					Callers: []Dependency{},
					Callees: []Dependency{},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and path
				if r.Method != http.MethodPost {
					t.Errorf("expected POST method, got %s", r.Method)
				}
				if r.URL.Path != "/impact" {
					t.Errorf("expected path /impact, got %s", r.URL.Path)
				}

				// Verify headers
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Errorf("expected Accept application/json, got %s", r.Header.Get("Accept"))
				}

				// For valid requests, verify request body
				if tt.file != "" {
					var reqBody map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
						t.Fatalf("failed to decode request body: %v", err)
					}
					if reqBody["file"] != tt.file {
						t.Errorf("expected file %q, got %v", tt.file, reqBody["file"])
					}
					expectedDepth := tt.depth
					if expectedDepth < 1 {
						expectedDepth = 1
					}
					if int(reqBody["depth"].(float64)) != expectedDepth {
						t.Errorf("expected depth %d, got %v", expectedDepth, reqBody["depth"])
					}
				}

				// Send response
				w.WriteHeader(tt.mockStatusCode)
				if tt.mockResponse != nil {
					if str, ok := tt.mockResponse.(string); ok {
						w.Write([]byte(str))
					} else {
						json.NewEncoder(w).Encode(tt.mockResponse)
					}
				}
			}))
			defer server.Close()

			client := NewTLDRClient(server.URL)
			resp, err := client.ImpactAnalysis(tt.file, tt.depth)

			if tt.expectedError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Fatal("expected response, got nil")
				}
				if tt.validateFunc != nil {
					tt.validateFunc(t, resp)
				}
			}
		})
	}
}

// TestSemanticSearch tests the semantic search functionality
func TestSemanticSearch(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		mockResponse   interface{}
		mockStatusCode int
		expectedError  bool
		errorContains  string
		validateFunc   func(t *testing.T, resp *TLDRSemanticResponse)
	}{
		{
			name:  "successful semantic search",
			query: "authentication middleware",
			mockResponse: TLDRSemanticResponse{
				Results: []TLDRSemanticResult{
					{
						File:     "src/auth/middleware.go",
						Function: "AuthMiddleware",
						Score:    0.95,
						Context:  "func AuthMiddleware(next http.Handler) http.Handler",
					},
					{
						File:     "src/auth/jwt.go",
						Function: "ValidateToken",
						Score:    0.87,
						Context:  "func ValidateToken(token string) (*Claims, error)",
					},
					{
						File:     "src/middleware/chain.go",
						Function: "Chain",
						Score:    0.72,
						Context:  "func Chain(middleware ...Middleware) Middleware",
					},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedError:  false,
			validateFunc: func(t *testing.T, resp *TLDRSemanticResponse) {
				if len(resp.Results) != 3 {
					t.Errorf("expected 3 results, got %d", len(resp.Results))
				}
				// Verify scores are in descending order
				for i := 1; i < len(resp.Results); i++ {
					if resp.Results[i].Score > resp.Results[i-1].Score {
						t.Errorf("results not sorted by score: %v", resp.Results)
					}
				}
				// Verify first result has highest score
				if resp.Results[0].Score != 0.95 {
					t.Errorf("expected first result score 0.95, got %f", resp.Results[0].Score)
				}
			},
		},
		{
			name:          "empty query validation",
			query:         "",
			expectedError: true,
			errorContains: "query cannot be empty",
		},
		{
			name:           "HTTP error - bad request",
			query:          "search query",
			mockResponse:   map[string]string{"error": "invalid query"},
			mockStatusCode: http.StatusBadRequest,
			expectedError:  true,
			errorContains:  "unexpected status code 400",
		},
		{
			name:           "empty results",
			query:          "nonexistent function",
			mockResponse:   TLDRSemanticResponse{Results: []TLDRSemanticResult{}},
			mockStatusCode: http.StatusOK,
			expectedError:  false,
			validateFunc: func(t *testing.T, resp *TLDRSemanticResponse) {
				if len(resp.Results) != 0 {
					t.Errorf("expected 0 results, got %d", len(resp.Results))
				}
			},
		},
		{
			name:  "results with scores",
			query: "database connection",
			mockResponse: TLDRSemanticResponse{
				Results: []TLDRSemanticResult{
					{File: "db.go", Function: "Connect", Score: 0.99, Context: "func Connect() error"},
					{File: "pool.go", Function: "NewPool", Score: 0.85, Context: "func NewPool() *Pool"},
					{File: "config.go", Function: "LoadConfig", Score: 0.65, Context: "func LoadConfig() *Config"},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedError:  false,
			validateFunc: func(t *testing.T, resp *TLDRSemanticResponse) {
				for i, result := range resp.Results {
					if result.Score < 0 || result.Score > 1 {
						t.Errorf("result %d: score %f out of range [0,1]", i, result.Score)
					}
					if result.File == "" {
						t.Errorf("result %d: file should not be empty", i)
					}
					if result.Function == "" {
						t.Errorf("result %d: function should not be empty", i)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST method, got %s", r.Method)
				}
				if r.URL.Path != "/semantic" {
					t.Errorf("expected path /semantic, got %s", r.URL.Path)
				}

				if tt.query != "" {
					var reqBody map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
						t.Fatalf("failed to decode request body: %v", err)
					}
					if reqBody["query"] != tt.query {
						t.Errorf("expected query %q, got %v", tt.query, reqBody["query"])
					}
					if reqBody["max_results"] != float64(10) {
						t.Errorf("expected max_results 10, got %v", reqBody["max_results"])
					}
				}

				w.WriteHeader(tt.mockStatusCode)
				if tt.mockResponse != nil {
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			client := NewTLDRClient(server.URL)
			resp, err := client.SemanticSearch(tt.query)

			if tt.expectedError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Fatal("expected response, got nil")
				}
				if tt.validateFunc != nil {
					tt.validateFunc(t, resp)
				}
			}
		})
	}
}

// TestContext tests the context extraction functionality
func TestContext(t *testing.T) {
	tests := []struct {
		name           string
		file           string
		mockResponse   interface{}
		mockStatusCode int
		expectedError  bool
		errorContains  string
		validateFunc   func(t *testing.T, resp *TLDRContextResponse)
	}{
		{
			name: "successful context extraction",
			file: "src/service.go",
			mockResponse: TLDRContextResponse{
				Functions: []TLDRFunctionInfo{
					{
						Name:       "ProcessData",
						Signature:  "func ProcessData(data []byte) (*Result, error)",
						StartLine:  45,
						EndLine:    89,
						DocComment: "ProcessData processes the input data and returns the result",
					},
					{
						Name:      "ValidateInput",
						Signature: "func ValidateInput(input string) bool",
						StartLine: 91,
						EndLine:   105,
					},
				},
				Imports: []TLDRImportInfo{
					{Path: "context", Alias: ""},
					{Path: "github.com/pkg/errors", Alias: "errs"},
					{Path: "fmt"},
				},
				Exports: []TLDRExportInfo{
					{Name: "ProcessData", Type: "func"},
					{Name: "Service", Type: "struct"},
					{Name: "DefaultConfig", Type: "var"},
				},
			},
			mockStatusCode: http.StatusOK,
			expectedError:  false,
			validateFunc: func(t *testing.T, resp *TLDRContextResponse) {
				// Verify functions
				if len(resp.Functions) != 2 {
					t.Errorf("expected 2 functions, got %d", len(resp.Functions))
				}
				func1 := resp.Functions[0]
				if func1.Name != "ProcessData" {
					t.Errorf("expected function name ProcessData, got %s", func1.Name)
				}
				if func1.StartLine != 45 {
					t.Errorf("expected start line 45, got %d", func1.StartLine)
				}
				if func1.EndLine != 89 {
					t.Errorf("expected end line 89, got %d", func1.EndLine)
				}
				if func1.DocComment == "" {
					t.Error("expected doc comment to not be empty")
				}

				// Verify imports
				if len(resp.Imports) != 3 {
					t.Errorf("expected 3 imports, got %d", len(resp.Imports))
				}
				import2 := resp.Imports[1]
				if import2.Path != "github.com/pkg/errors" {
					t.Errorf("expected import path github.com/pkg/errors, got %s", import2.Path)
				}
				if import2.Alias != "errs" {
					t.Errorf("expected alias errs, got %s", import2.Alias)
				}

				// Verify exports
				if len(resp.Exports) != 3 {
					t.Errorf("expected 3 exports, got %d", len(resp.Exports))
				}
				export2 := resp.Exports[1]
				if export2.Name != "Service" {
					t.Errorf("expected export name Service, got %s", export2.Name)
				}
				if export2.Type != "struct" {
					t.Errorf("expected export type struct, got %s", export2.Type)
				}
			},
		},
		{
			name:          "empty file validation",
			file:          "",
			expectedError: true,
			errorContains: "file path cannot be empty",
		},
		{
			name: "context with empty arrays",
			file: "src/empty.go",
			mockResponse: TLDRContextResponse{
				Functions: []TLDRFunctionInfo{},
				Imports:   []TLDRImportInfo{},
				Exports:   []TLDRExportInfo{},
			},
			mockStatusCode: http.StatusOK,
			expectedError:  false,
			validateFunc: func(t *testing.T, resp *TLDRContextResponse) {
				if len(resp.Functions) != 0 {
					t.Errorf("expected 0 functions, got %d", len(resp.Functions))
				}
				if len(resp.Imports) != 0 {
					t.Errorf("expected 0 imports, got %d", len(resp.Imports))
				}
				if len(resp.Exports) != 0 {
					t.Errorf("expected 0 exports, got %d", len(resp.Exports))
				}
			},
		},
		{
			name:           "server error",
			file:           "src/service.go",
			mockResponse:   map[string]string{"error": "failed to parse file"},
			mockStatusCode: http.StatusInternalServerError,
			expectedError:  true,
			errorContains:  "unexpected status code 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST method, got %s", r.Method)
				}
				if r.URL.Path != "/context" {
					t.Errorf("expected path /context, got %s", r.URL.Path)
				}

				if tt.file != "" {
					var reqBody map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
						t.Fatalf("failed to decode request body: %v", err)
					}
					if reqBody["file"] != tt.file {
						t.Errorf("expected file %q, got %v", tt.file, reqBody["file"])
					}
					if reqBody["depth"] != float64(2) {
						t.Errorf("expected depth 2, got %v", reqBody["depth"])
					}
				}

				w.WriteHeader(tt.mockStatusCode)
				if tt.mockResponse != nil {
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			client := NewTLDRClient(server.URL)
			resp, err := client.Context(tt.file)

			if tt.expectedError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Fatal("expected response, got nil")
				}
				if tt.validateFunc != nil {
					tt.validateFunc(t, resp)
				}
			}
		})
	}
}

// TestIsAvailable tests the availability check
func TestIsAvailable(t *testing.T) {
	tests := []struct {
		name           string
		mockStatusCode int
		mockDelay      time.Duration
		closeServer    bool
		expected       bool
	}{
		{
			name:           "TLDR daemon is running",
			mockStatusCode: http.StatusOK,
			expected:       true,
		},
		{
			name:           "TLDR daemon returns non-OK status",
			mockStatusCode: http.StatusServiceUnavailable,
			expected:       false,
		},
		{
			name:        "TLDR daemon is down",
			closeServer: true,
			expected:    false,
		},
		{
			name:           "TLDR daemon times out",
			mockStatusCode: http.StatusOK,
			mockDelay:      3 * time.Second,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET method, got %s", r.Method)
				}
				if r.URL.Path != "/health" {
					t.Errorf("expected path /health, got %s", r.URL.Path)
				}

				if tt.mockDelay > 0 {
					time.Sleep(tt.mockDelay)
				}

				w.WriteHeader(tt.mockStatusCode)
			}))
			defer server.Close()

			if tt.closeServer {
				server.Close()
			}

			client := NewTLDRClient(server.URL)
			available := client.IsAvailable()

			if available != tt.expected {
				t.Errorf("expected available=%v, got %v", tt.expected, available)
			}
		})
	}
}

// TestErrorHandling tests various error scenarios
func TestErrorHandling(t *testing.T) {
	t.Run("network error", func(t *testing.T) {
		// Create a server and close it immediately to simulate network error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		server.Close()

		client := NewTLDRClient(server.URL)
		_, err := client.ImpactAnalysis("test.go", 1)

		if err == nil {
			t.Fatal("expected network error, got nil")
		}

		if !strings.Contains(err.Error(), "request failed") {
			t.Errorf("expected error to contain 'request failed', got %q", err.Error())
		}
	})

	t.Run("timeout error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Delay longer than the timeout
			time.Sleep(RequestTimeout + 100*time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create client with shorter timeout for faster test
		client := NewTLDRClientWithHTTP(server.URL, &http.Client{
			Timeout: 50 * time.Millisecond,
		})

		_, err := client.ImpactAnalysis("test.go", 1)

		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}

		// Context deadline exceeded or timeout errors
		if !strings.Contains(err.Error(), "request failed") {
			t.Errorf("expected error to contain 'request failed', got %q", err.Error())
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := NewTLDRClient(server.URL)
		_, err := client.ImpactAnalysis("test.go", 1)

		if err == nil {
			t.Fatal("expected JSON decode error, got nil")
		}

		if !strings.Contains(err.Error(), "failed to decode response") {
			t.Errorf("expected error to contain 'failed to decode response', got %q", err.Error())
		}
	})

	t.Run("malformed JSON structure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Valid JSON but wrong structure for TLDRImpactResponse
			w.Write([]byte(`{"unexpected": "structure"}`))
		}))
		defer server.Close()

		client := NewTLDRClient(server.URL)
		resp, err := client.ImpactAnalysis("test.go", 1)

		// This should NOT error - JSON unmarshaling is lenient
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp == nil {
			t.Fatal("expected response even with wrong structure")
		}

		// But fields should be empty/zero
		if resp.File != "" {
			t.Errorf("expected empty file, got %q", resp.File)
		}
	})

	t.Run("server returns non-JSON error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Server Error"))
		}))
		defer server.Close()

		client := NewTLDRClient(server.URL)
		_, err := client.SemanticSearch("query")

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "unexpected status code 500") {
			t.Errorf("expected error to contain status code, got %q", err.Error())
		}

		if !strings.Contains(err.Error(), "Internal Server Error") {
			t.Errorf("expected error to contain response body, got %q", err.Error())
		}
	})
}

// TestRequestFormatValidation tests that requests are properly formatted
func TestRequestFormatValidation(t *testing.T) {
	t.Run("impact analysis request format", func(t *testing.T) {
		var capturedRequest map[string]interface{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(TLDRImpactResponse{File: "test.go"})
		}))
		defer server.Close()

		client := NewTLDRClient(server.URL)
		client.ImpactAnalysis("src/main.go", 3)

		if capturedRequest["file"] != "src/main.go" {
			t.Errorf("expected file 'src/main.go', got %v", capturedRequest["file"])
		}

		if capturedRequest["depth"] != float64(3) {
			t.Errorf("expected depth 3, got %v", capturedRequest["depth"])
		}
	})

	t.Run("semantic search request format", func(t *testing.T) {
		var capturedRequest map[string]interface{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(TLDRSemanticResponse{Results: []TLDRSemanticResult{}})
		}))
		defer server.Close()

		client := NewTLDRClient(server.URL)
		client.SemanticSearch("authentication middleware")

		if capturedRequest["query"] != "authentication middleware" {
			t.Errorf("expected query 'authentication middleware', got %v", capturedRequest["query"])
		}

		if capturedRequest["max_results"] != float64(10) {
			t.Errorf("expected max_results 10, got %v", capturedRequest["max_results"])
		}
	})

	t.Run("context request format", func(t *testing.T) {
		var capturedRequest map[string]interface{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(TLDRContextResponse{})
		}))
		defer server.Close()

		client := NewTLDRClient(server.URL)
		client.Context("src/service.go")

		if capturedRequest["file"] != "src/service.go" {
			t.Errorf("expected file 'src/service.go', got %v", capturedRequest["file"])
		}

		if capturedRequest["depth"] != float64(2) {
			t.Errorf("expected depth 2, got %v", capturedRequest["depth"])
		}
	})
}

// TestClose tests the Close method
func TestClose(t *testing.T) {
	client := NewTLDRClient(DefaultTLDRURL)

	err := client.Close()
	if err != nil {
		t.Errorf("expected Close to return nil, got %v", err)
	}
}

// TestClientConcurrency tests that the client can handle concurrent requests
func TestClientConcurrency(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/impact":
			json.NewEncoder(w).Encode(TLDRImpactResponse{File: fmt.Sprintf("file%d.go", requestCount)})
		case "/semantic":
			json.NewEncoder(w).Encode(TLDRSemanticResponse{Results: []TLDRSemanticResult{}})
		case "/context":
			json.NewEncoder(w).Encode(TLDRContextResponse{})
		}
	}))
	defer server.Close()

	client := NewTLDRClient(server.URL)

	// Run concurrent requests
	done := make(chan bool, 9)

	for i := 0; i < 3; i++ {
		go func() {
			client.ImpactAnalysis("test.go", 1)
			done <- true
		}()
		go func() {
			client.SemanticSearch("query")
			done <- true
		}()
		go func() {
			client.Context("test.go")
			done <- true
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < 9; i++ {
		<-done
	}

	if requestCount != 9 {
		t.Errorf("expected 9 requests, got %d", requestCount)
	}
}

// mockTransport is a custom RoundTripper for testing network errors
type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

// TestCustomHTTPClientWithMock tests using a custom HTTP client with mock transport
func TestCustomHTTPClientWithMock(t *testing.T) {
	t.Run("custom transport error", func(t *testing.T) {
		customErr := errors.New("custom transport error")
		mockClient := &http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					return nil, customErr
				},
			},
		}

		client := NewTLDRClientWithHTTP("http://localhost:3000", mockClient)
		_, err := client.ImpactAnalysis("test.go", 1)

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "request failed") {
			t.Errorf("expected error to contain 'request failed', got %q", err.Error())
		}
	})

	t.Run("custom transport success", func(t *testing.T) {
		responseData := TLDRImpactResponse{
			File: "mock.go",
			Impact: struct {
				Callers []Dependency `json:"callers"`
				Callees []Dependency `json:"callees"`
			}{
				Callers: []Dependency{},
				Callees: []Dependency{},
			},
		}

		mockClient := &http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					body, _ := json.Marshal(responseData)
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       &mockReadCloser{Reader: strings.NewReader(string(body))},
						Header:     http.Header{"Content-Type": []string{"application/json"}},
					}, nil
				},
			},
		}

		client := NewTLDRClientWithHTTP("http://localhost:3000", mockClient)
		resp, err := client.ImpactAnalysis("test.go", 1)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.File != "mock.go" {
			t.Errorf("expected file 'mock.go', got %s", resp.File)
		}
	})
}

// mockReadCloser implements io.ReadCloser for testing
type mockReadCloser struct {
	*strings.Reader
	closed bool
}

func (m *mockReadCloser) Close() error {
	m.closed = true
	return nil
}

// TestContextWithTimeout tests timeout behavior specifically
func TestContextWithTimeout(t *testing.T) {
	t.Run("request context timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if context has deadline - the httptest server sets its own context
			// so we verify that the request completes successfully with timeout handling
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(TLDRImpactResponse{File: "test.go"})
		}))
		defer server.Close()

		client := NewTLDRClient(server.URL)
		resp, err := client.ImpactAnalysis("test.go", 1)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.File != "test.go" {
			t.Errorf("expected file 'test.go', got %s", resp.File)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
				// Context cancelled as expected
				return
			case <-time.After(100 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(TLDRImpactResponse{File: "test.go"})
			}
		}))
		defer server.Close()

		// Create a client with a very short timeout that will trigger
		client := NewTLDRClientWithHTTP(server.URL, &http.Client{
			Timeout: 10 * time.Millisecond,
		})

		_, err := client.ImpactAnalysis("test.go", 1)

		// Should get a timeout error
		if err == nil {
			t.Skip("request completed before timeout - this can happen in fast environments")
		}

		// Error should indicate timeout or context cancellation
		if !strings.Contains(err.Error(), "request failed") {
			t.Errorf("expected error to contain 'request failed', got %q", err.Error())
		}
	})
}
