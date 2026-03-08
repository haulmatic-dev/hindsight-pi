package gptcache

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestNewClient tests the Client constructor
func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		serverURL   string
		enabled     bool
		wantURL     string
		wantEnabled bool
	}{
		{
			name:        "default configuration with enabled=true",
			serverURL:   "http://localhost:8000",
			enabled:     true,
			wantURL:     "http://localhost:8000",
			wantEnabled: true,
		},
		{
			name:        "disabled client",
			serverURL:   "http://localhost:8000",
			enabled:     false,
			wantURL:     "http://localhost:8000",
			wantEnabled: false,
		},
		{
			name:        "custom base URL",
			serverURL:   "https://cache.example.com:9000",
			enabled:     true,
			wantURL:     "https://cache.example.com:9000",
			wantEnabled: true,
		},
		{
			name:        "localhost with different port",
			serverURL:   "http://127.0.0.1:3000",
			enabled:     true,
			wantURL:     "http://127.0.0.1:3000",
			wantEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.serverURL, tt.enabled)

			if client == nil {
				t.Fatal("NewClient returned nil")
			}

			if client.baseURL != tt.wantURL {
				t.Errorf("baseURL = %q, want %q", client.baseURL, tt.wantURL)
			}

			if client.enabled != tt.wantEnabled {
				t.Errorf("enabled = %v, want %v", client.enabled, tt.wantEnabled)
			}

			if client.client == nil {
				t.Error("http.Client is nil")
			}

			// Verify default timeout is set
			if client.client.Timeout != 5*time.Second {
				t.Errorf("timeout = %v, want %v", client.client.Timeout, 5*time.Second)
			}
		})
	}
}

// TestGet_CacheHit tests successful cache retrieval (200 response)
func TestGet_CacheHit(t *testing.T) {
	expectedResult := CacheResult{
		Answer:     "This is a cached answer",
		CacheHit:   true,
		Similarity: 0.95,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/get" {
			t.Errorf("expected path /get, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}

		// Verify request body
		var reqBody getRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody.Prompt != "test prompt" {
			t.Errorf("expected prompt 'test prompt', got %q", reqBody.Prompt)
		}

		// Return cache hit
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expectedResult)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)
	result, err := client.Get("test prompt")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
	}

	if result.Answer != expectedResult.Answer {
		t.Errorf("answer = %q, want %q", result.Answer, expectedResult.Answer)
	}

	if result.CacheHit != expectedResult.CacheHit {
		t.Errorf("cacheHit = %v, want %v", result.CacheHit, expectedResult.CacheHit)
	}

	if result.Similarity != expectedResult.Similarity {
		t.Errorf("similarity = %v, want %v", result.Similarity, expectedResult.Similarity)
	}
}

// TestGet_CacheMiss tests cache miss scenario (404 response)
func TestGet_CacheMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/get" {
			t.Errorf("expected path /get, got %s", r.URL.Path)
		}

		// Return cache miss
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)
	result, err := client.Get("unknown prompt")

	if err != nil {
		t.Fatalf("unexpected error on cache miss: %v", err)
	}

	if result != nil {
		t.Errorf("expected nil on cache miss, got %+v", result)
	}
}

// TestGet_DisabledClient tests that disabled client returns nil without making requests
func TestGet_DisabledClient(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(CacheResult{Answer: "cached"})
	}))
	defer server.Close()

	client := NewClient(server.URL, false)
	result, err := client.Get("test prompt")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Errorf("expected nil for disabled client, got %+v", result)
	}

	if requestCount > 0 {
		t.Errorf("disabled client made %d request(s), expected 0", requestCount)
	}
}

// TestGet_NetworkError tests graceful degradation on network errors
func TestGet_NetworkError(t *testing.T) {
	// Create a server that will be closed to simulate network error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Close() // Close immediately to simulate server down

	client := NewClient(server.URL, true)
	result, err := client.Get("test prompt")

	// Should gracefully degrade - no error, just nil result
	if err != nil {
		t.Errorf("expected graceful degradation (nil error), got: %v", err)
	}

	if result != nil {
		t.Errorf("expected nil result on network error, got %+v", result)
	}
}

// TestGet_JSONParsing tests JSON parsing of the response
func TestGet_JSONParsing(t *testing.T) {
	tests := []struct {
		name           string
		responseBody   string
		expectedResult *CacheResult
		expectError    bool
	}{
		{
			name:           "valid JSON with all fields",
			responseBody:   `{"answer": "test answer", "cache_hit": true, "similarity": 0.85}`,
			expectedResult: &CacheResult{Answer: "test answer", CacheHit: true, Similarity: 0.85},
			expectError:    false,
		},
		{
			name:           "valid JSON with minimal fields",
			responseBody:   `{"answer": "minimal", "cache_hit": false, "similarity": 0.0}`,
			expectedResult: &CacheResult{Answer: "minimal", CacheHit: false, Similarity: 0.0},
			expectError:    false,
		},
		{
			name:           "invalid JSON",
			responseBody:   `{"invalid json`,
			expectedResult: nil,
			expectError:    true,
		},
		{
			name:           "empty answer field",
			responseBody:   `{"answer": "", "cache_hit": true, "similarity": 1.0}`,
			expectedResult: &CacheResult{Answer: "", CacheHit: true, Similarity: 1.0},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient(server.URL, true)
			result, err := client.Get("test prompt")

			if tt.expectError {
				if err == nil {
					t.Error("expected error for invalid JSON, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("expected result, got nil")
			}

			if result.Answer != tt.expectedResult.Answer {
				t.Errorf("answer = %q, want %q", result.Answer, tt.expectedResult.Answer)
			}

			if result.CacheHit != tt.expectedResult.CacheHit {
				t.Errorf("cacheHit = %v, want %v", result.CacheHit, tt.expectedResult.CacheHit)
			}

			if result.Similarity != tt.expectedResult.Similarity {
				t.Errorf("similarity = %v, want %v", result.Similarity, tt.expectedResult.Similarity)
			}
		})
	}
}

// TestGet_UnexpectedStatusCode tests handling of unexpected status codes
func TestGet_UnexpectedStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, true)
	result, err := client.Get("test prompt")

	if err == nil {
		t.Error("expected error for 500 status, got nil")
	}

	if result != nil {
		t.Errorf("expected nil result on error, got %+v", result)
	}
}

// TestPut_Success tests successful cache storage
func TestPut_Success(t *testing.T) {
	var receivedRequest putRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/put" {
			t.Errorf("expected path /put, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}

		// Capture request body
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		// Return success
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)
	err := client.Put("test prompt", "test answer")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedRequest.Prompt != "test prompt" {
		t.Errorf("prompt = %q, want %q", receivedRequest.Prompt, "test prompt")
	}

	if receivedRequest.Answer != "test answer" {
		t.Errorf("answer = %q, want %q", receivedRequest.Answer, "test answer")
	}
}

// TestPut_CreatedStatus tests that 201 status code is also accepted
func TestPut_CreatedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)
	err := client.Put("test prompt", "test answer")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPut_DisabledClient tests that disabled client returns without making requests
func TestPut_DisabledClient(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, false)
	err := client.Put("test prompt", "test answer")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requestCount > 0 {
		t.Errorf("disabled client made %d request(s), expected 0", requestCount)
	}
}

// TestPut_NetworkError tests best-effort behavior on network errors
func TestPut_NetworkError(t *testing.T) {
	// Create a server that will be closed to simulate network error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Close() // Close immediately to simulate server down

	client := NewClient(server.URL, true)
	err := client.Put("test prompt", "test answer")

	// Should be best-effort - no error returned
	if err != nil {
		t.Errorf("expected best-effort (nil error), got: %v", err)
	}
}

// TestPut_EmptyPromptAndAnswer tests with empty strings
func TestPut_EmptyPromptAndAnswer(t *testing.T) {
	var receivedRequest putRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)
	err := client.Put("", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedRequest.Prompt != "" {
		t.Errorf("prompt = %q, want empty string", receivedRequest.Prompt)
	}

	if receivedRequest.Answer != "" {
		t.Errorf("answer = %q, want empty string", receivedRequest.Answer)
	}
}

// TestPut_NonSuccessStatusCode tests best-effort behavior for non-success status codes
func TestPut_NonSuccessStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{
			name:       "internal server error",
			statusCode: http.StatusInternalServerError,
		},
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
		},
		{
			name:       "service unavailable",
			statusCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClient(server.URL, true)
			err := client.Put("test prompt", "test answer")

			// Should be best-effort - no error returned
			if err != nil {
				t.Errorf("expected best-effort (nil error) for status %d, got: %v", tt.statusCode, err)
			}
		})
	}
}

// TestGracefulDegradation_ServerDown tests behavior when server is completely down
func TestGracefulDegradation_ServerDown(t *testing.T) {
	// Start and immediately stop a server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := NewClient(server.URL, true)

	// Test Get
	result, err := client.Get("test prompt")
	if err != nil {
		t.Errorf("Get: expected graceful degradation, got error: %v", err)
	}
	if result != nil {
		t.Errorf("Get: expected nil result, got %+v", result)
	}

	// Test Put
	err = client.Put("test prompt", "test answer")
	if err != nil {
		t.Errorf("Put: expected graceful degradation, got error: %v", err)
	}
}

// TestGracefulDegradation_Timeout tests timeout scenarios
func TestGracefulDegradation_Timeout(t *testing.T) {
	// Create a slow server that takes longer than the timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Longer than client timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)

	// Test Get with timeout
	done := make(chan struct{})
	var getResult *CacheResult
	var getErr error

	go func() {
		defer close(done)
		getResult, getErr = client.Get("test prompt")
	}()

	select {
	case <-done:
		// Should complete quickly due to timeout
		if getErr != nil {
			t.Errorf("Get: expected graceful degradation, got error: %v", getErr)
		}
		if getResult != nil {
			t.Errorf("Get: expected nil result after timeout, got %+v", getResult)
		}
	case <-time.After(7 * time.Second):
		t.Error("Get: operation did not timeout as expected")
	}

	// Test Put with timeout
	done = make(chan struct{})
	var putErr error

	go func() {
		defer close(done)
		putErr = client.Put("test prompt", "test answer")
	}()

	select {
	case <-done:
		// Should complete quickly due to timeout
		if putErr != nil {
			t.Errorf("Put: expected graceful degradation, got error: %v", putErr)
		}
	case <-time.After(7 * time.Second):
		t.Error("Put: operation did not timeout as expected")
	}
}

// TestGracefulDegradation_NoErrorsPropagated verifies no errors are propagated
func TestGracefulDegradation_NoErrorsPropagated(t *testing.T) {
	testCases := []struct {
		name       string
		serverFunc func(w http.ResponseWriter, r *http.Request)
	}{
		{
			name:       "connection refused",
			serverFunc: nil, // Will close server immediately
		},
		{
			name: "invalid response",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				hijacker, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hijacker.Hijack()
					conn.Close() // Force connection close
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var server *httptest.Server
			if tc.serverFunc != nil {
				server = httptest.NewServer(http.HandlerFunc(tc.serverFunc))
			} else {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
				server.Close()
			}
			defer server.Close()

			client := NewClient(server.URL, true)

			// Get should not return error
			result, err := client.Get("test prompt")
			if err != nil {
				t.Errorf("Get: unexpected error: %v", err)
			}
			if result != nil {
				t.Errorf("Get: expected nil result, got %+v", result)
			}

			// Put should not return error
			err = client.Put("test prompt", "test answer")
			if err != nil {
				t.Errorf("Put: unexpected error: %v", err)
			}
		})
	}
}

// TestConcurrency_ConcurrentGet tests concurrent Get operations
func TestConcurrency_ConcurrentGet(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()

		// Vary responses
		if count%2 == 0 {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(CacheResult{
				Answer:     "answer",
				CacheHit:   true,
				Similarity: 0.9,
			})
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			_, err := client.Get("test prompt")
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	if finalCount != numGoroutines {
		t.Errorf("expected %d server calls, got %d", numGoroutines, finalCount)
	}
}

// TestConcurrency_ConcurrentPut tests concurrent Put operations
func TestConcurrency_ConcurrentPut(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()

		// Small delay to increase concurrency contention
		time.Sleep(time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			err := client.Put("prompt", "answer")
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	if finalCount != numGoroutines {
		t.Errorf("expected %d server calls, got %d", numGoroutines, finalCount)
	}
}

// TestConcurrency_MixedOperations tests concurrent Get and Put operations
func TestConcurrency_MixedOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(CacheResult{
				Answer:     "cached",
				CacheHit:   true,
				Similarity: 0.95,
			})
		case "/put":
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Launch Get operations
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			_, err := client.Get("test prompt")
			if err != nil {
				t.Errorf("Get goroutine %d: unexpected error: %v", idx, err)
			}
		}(i)
	}

	// Launch Put operations
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			err := client.Put("test prompt", "test answer")
			if err != nil {
				t.Errorf("Put goroutine %d: unexpected error: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrency_DisabledClientConcurrent tests thread safety with disabled client
func TestConcurrency_DisabledClientConcurrent(t *testing.T) {
	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, false) // Disabled

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Launch concurrent Get and Put operations on disabled client
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			client.Get("test prompt")
		}()

		go func() {
			defer wg.Done()
			client.Put("test prompt", "test answer")
		}()
	}

	wg.Wait()

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount > 0 {
		t.Errorf("disabled client made %d requests, expected 0", finalCount)
	}
}

// TestConcurrency_ThreadSafety verifies the client is thread-safe
func TestConcurrency_ThreadSafety(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(CacheResult{
				Answer:     "answer",
				CacheHit:   true,
				Similarity: 1.0,
			})
		case "/put":
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()

	// Create a single client to be shared across goroutines
	client := NewClient(server.URL, true)

	const numIterations = 100
	const numGoroutines = 20

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*numIterations*2)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				// Alternate between Get and Put
				if j%2 == 0 {
					_, err := client.Get("prompt")
					if err != nil {
						errChan <- err
					}
				} else {
					err := client.Put("prompt", "answer")
					if err != nil {
						errChan <- err
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	var errorCount int
	for err := range errChan {
		t.Errorf("concurrent operation error: %v", err)
		errorCount++
		if errorCount >= 10 {
			t.Fatal("too many errors, stopping test")
		}
	}
}

// TestIntegration_EndToEnd tests the complete flow
func TestIntegration_EndToEnd(t *testing.T) {
	// Simulate a simple cache server
	cache := make(map[string]string)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get":
			var req getRequest
			json.NewDecoder(r.Body).Decode(&req)

			mu.Lock()
			answer, exists := cache[req.Prompt]
			mu.Unlock()

			if exists {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(CacheResult{
					Answer:     answer,
					CacheHit:   true,
					Similarity: 1.0,
				})
			} else {
				w.WriteHeader(http.StatusNotFound)
			}

		case "/put":
			var req putRequest
			json.NewDecoder(r.Body).Decode(&req)

			mu.Lock()
			cache[req.Prompt] = req.Answer
			mu.Unlock()

			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, true)

	// Test 1: Get should miss initially
	result, err := client.Get("hello")
	if err != nil {
		t.Fatalf("unexpected error on initial get: %v", err)
	}
	if result != nil {
		t.Error("expected cache miss on initial get")
	}

	// Test 2: Put should succeed
	err = client.Put("hello", "world")
	if err != nil {
		t.Fatalf("unexpected error on put: %v", err)
	}

	// Test 3: Get should hit now
	result, err = client.Get("hello")
	if err != nil {
		t.Fatalf("unexpected error on second get: %v", err)
	}
	if result == nil {
		t.Fatal("expected cache hit after put")
	}
	if result.Answer != "world" {
		t.Errorf("expected answer 'world', got %q", result.Answer)
	}
	if !result.CacheHit {
		t.Error("expected CacheHit to be true")
	}

	// Test 4: Put with different answer
	err = client.Put("hello", "updated")
	if err != nil {
		t.Fatalf("unexpected error on second put: %v", err)
	}

	result, err = client.Get("hello")
	if err != nil {
		t.Fatalf("unexpected error on third get: %v", err)
	}
	if result.Answer != "updated" {
		t.Errorf("expected answer 'updated', got %q", result.Answer)
	}
}

// TestClient_WithRealisticServerError tests handling of realistic server errors
func TestClient_WithRealisticServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate intermittent failures
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL, true)

	// Get should return error for 503
	result, err := client.Get("test")
	if err == nil {
		t.Error("expected error for 503 status, got nil")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}

	// Put should be best-effort and not return error
	err = client.Put("test", "answer")
	if err != nil {
		t.Errorf("expected best-effort Put (nil error), got: %v", err)
	}
}

// TestClient_WithInvalidURL tests behavior with invalid base URL
func TestClient_WithInvalidURL(t *testing.T) {
	// Create client with URL that will cause connection issues
	client := NewClient("http://[invalid:url", true)

	// Get returns error for URL parsing failure (this is expected - it's a config error)
	result, err := client.Get("test")
	if err == nil {
		t.Error("expected error for malformed URL in Get")
	}
	if result != nil {
		t.Error("expected nil result with invalid URL")
	}

	// Put is best-effort and returns nil even on URL parsing errors
	err = client.Put("test", "answer")
	if err != nil {
		t.Errorf("expected best-effort Put (nil error) for malformed URL, got: %v", err)
	}
}
