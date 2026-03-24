// Package hindsight provides the embedded Hindsight server for memory retention.
package hindsight

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// ServerConfig contains configuration for the embedded Hindsight server.
type ServerConfig struct {
	// Port is the port to run the server on. Defaults to 8888.
	Port int

	// Host is the host to bind to. Defaults to "127.0.0.1".
	Host string

	// Timeout for HTTP server operations.
	Timeout time.Duration
}

// DefaultServerConfig returns a ServerConfig with default values.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Port:    8888,
		Host:    "127.0.0.1",
		Timeout: 30 * time.Second,
	}
}

// Server is the embedded Hindsight HTTP server.
type Server struct {
	config     ServerConfig
	httpServer *http.Server
	listener   net.Listener
	mu         sync.RWMutex
	running    bool
	startErr   error
}

// NewServer creates a new embedded Hindsight server with the given configuration.
// If config.Port is 0, it defaults to 8888.
// If config.Host is empty, it defaults to "127.0.0.1".
// If config.Timeout is 0, it defaults to 30 seconds.
func NewServer(config ServerConfig) *Server {
	// Apply defaults for zero values
	if config.Port == 0 {
		config.Port = 8888
	}
	if config.Host == "" {
		config.Host = "127.0.0.1"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	return &Server{
		config: config,
	}
}

// Start starts the embedded Hindsight server.
// Returns an error if the server fails to start (fail-fast behavior).
// This method blocks until the server is successfully listening or fails to start.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already running
	if s.running {
		return fmt.Errorf("server is already running on %s", s.Addr())
	}

	// Create listener first to verify port availability
	addr := s.Addr()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.startErr = fmt.Errorf("failed to listen on %s: %w", addr, err)
		return s.startErr
	}
	s.listener = listener

	// Create HTTP server with configured timeout
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.createHandler(),
		ReadTimeout:  s.config.Timeout,
		WriteTimeout: s.config.Timeout,
		IdleTimeout:  120 * time.Second,
	}

	// Start serving in a goroutine
	s.running = true
	s.startErr = nil

	go func() {
		if err := s.httpServer.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			s.mu.Lock()
			s.running = false
			s.startErr = err
			s.mu.Unlock()
		}
	}()

	// Give the server a moment to start and verify it's listening
	time.Sleep(10 * time.Millisecond)

	// Verify server is actually running
	s.mu.RLock()
	currentRunning := s.running
	s.mu.RUnlock()

	if !currentRunning {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.startErr != nil {
			return s.startErr
		}
		return fmt.Errorf("server failed to start on %s", addr)
	}

	return nil
}

// Stop gracefully stops the embedded Hindsight server.
// Returns an error if the server is not running or fails to stop.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("server is not running")
	}

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.config.Timeout)
	defer cancel()

	// Gracefully shutdown the server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.running = false
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	s.running = false
	s.listener = nil
	s.httpServer = nil

	return nil
}

// Port returns the port the server is configured to run on.
func (s *Server) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Port
}

// IsRunning returns true if the server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// URL returns the base URL for the server (e.g., "http://127.0.0.1:8888").
func (s *Server) URL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("http://%s:%d", s.config.Host, s.config.Port)
}

// Addr returns the server address in the form "host:port".
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
}

// createHandler creates the HTTP handler for the server.
func (s *Server) createHandler() http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","service":"hindsight"}`))
	})

	// API v1 routes
	mux.HandleFunc("/api/v1/search", s.handleSearch)
	mux.HandleFunc("/api/v1/retain", s.handleRetain)

	return mux
}

// handleSearch handles search requests (placeholder for actual implementation).
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success":true,"count":0,"memories":[]}`))
}

// handleRetain handles retain requests (placeholder for actual implementation).
func (s *Server) handleRetain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success":true}`))
}
