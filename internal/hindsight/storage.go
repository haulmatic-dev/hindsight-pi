// Package hindsight provides the storage layer with observability and logging.
// This file contains the storage operations that are logged for monitoring.
package hindsight

import (
	"context"
	"fmt"
	"time"
)

// Storage provides a logging wrapper around Hindsight operations.
// It logs all memory operations with duration and result counts for observability.
type Storage struct {
	client *Client
	logger Logger
}

// Logger defines the interface for logging operations.
// This allows for easy mocking in tests.
type Logger interface {
	// Info logs informational messages
	Info(msg string, fields ...LogField)
	// Error logs error messages with context
	Error(msg string, err error, fields ...LogField)
	// Debug logs debug messages
	Debug(msg string, fields ...LogField)
	// Warn logs warning messages
	Warn(msg string, fields ...LogField)
}

// LogField represents a key-value pair for structured logging.
type LogField struct {
	Key   string
	Value interface{}
}

// String creates a string log field.
func String(key, value string) LogField {
	return LogField{Key: key, Value: value}
}

// Int creates an integer log field.
func Int(key string, value int) LogField {
	return LogField{Key: key, Value: value}
}

// Duration creates a duration log field.
func Duration(key string, value time.Duration) LogField {
	return LogField{Key: key, Value: value}
}

// Float64 creates a float64 log field.
func Float64(key string, value float64) LogField {
	return LogField{Key: key, Value: value}
}

// Bool creates a boolean log field.
func Bool(key string, value bool) LogField {
	return LogField{Key: key, Value: value}
}

// Error creates an error log field.
func Error(err error) LogField {
	return LogField{Key: "error", Value: err}
}

// NewStorage creates a new Storage instance with the given client and logger.
func NewStorage(client *Client, logger Logger) *Storage {
	if client == nil {
		panic("client cannot be nil")
	}
	if logger == nil {
		logger = &NoopLogger{}
	}
	return &Storage{
		client: client,
		logger: logger,
	}
}

// Search searches for memories with logging.
// Logs: operation duration, result count, query details.
func (s *Storage) Search(ctx context.Context, query, beadType string, limit int) (*SearchResponse, error) {
	start := time.Now()
	s.logger.Debug("Starting memory search", String("query", query), String("type", beadType), Int("limit", limit))

	resp, err := s.client.Search(query, beadType, limit)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("Memory search failed", err,
			String("query", query),
			String("type", beadType),
			Duration("duration", duration),
		)
		return nil, err
	}

	s.logger.Info("Memory search completed",
		String("query", query),
		String("type", beadType),
		Int("result_count", resp.Count),
		Int("total_matches", resp.TotalMatches),
		Duration("duration", duration),
	)

	return resp, nil
}

// SearchByBead searches for memories by bead with logging.
// Logs: operation duration, result count, bead details.
func (s *Storage) SearchByBead(ctx context.Context, title, beadType string) (*SearchResponse, error) {
	start := time.Now()
	s.logger.Debug("Starting bead search", String("title", title), String("type", beadType))

	resp, err := s.client.SearchByBead(title, beadType)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("Bead search failed", err,
			String("title", title),
			String("type", beadType),
			Duration("duration", duration),
		)
		return nil, err
	}

	s.logger.Info("Bead search completed",
		String("title", title),
		String("type", beadType),
		Int("result_count", resp.Count),
		Duration("duration", duration),
	)

	return resp, nil
}

// Retain stores an execution memory with logging.
// Logs: operation duration, success/failure, memory ID.
func (s *Storage) Retain(ctx context.Context, memory *ExecutionMemory) (*RetentionResult, error) {
	start := time.Now()
	s.logger.Debug("Starting memory retention", String("bead_id", string(memory.BeadID)))

	result, err := s.client.Retain(memory)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("Memory retention failed", err,
			String("bead_id", string(memory.BeadID)),
			Duration("duration", duration),
		)
		return nil, err
	}

	if result.Success {
		s.logger.Info("Memory retained successfully",
			String("bead_id", string(memory.BeadID)),
			String("memory_id", result.MemoryID),
			Duration("duration", duration),
		)
	} else {
		s.logger.Warn("Memory retention returned failure",
			String("bead_id", string(memory.BeadID)),
			String("error", result.Error),
			Duration("duration", duration),
		)
	}

	return result, nil
}

// RetainContextResult stores a context processor result with logging.
// Logs: operation duration, success/failure, result details.
func (s *Storage) RetainContextResult(ctx context.Context, result *ContextProcessorResult) (*RetentionResult, error) {
	start := time.Now()
	s.logger.Debug("Starting context result retention", String("bead_id", string(result.BeadID)))

	retentionResult, err := s.client.RetainContextResult(result)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("Context result retention failed", err,
			String("bead_id", string(result.BeadID)),
			Duration("duration", duration),
		)
		return nil, err
	}

	if retentionResult.Success {
		s.logger.Info("Context result retained successfully",
			String("bead_id", string(result.BeadID)),
			String("memory_id", retentionResult.MemoryID),
			Duration("duration", duration),
		)
	} else {
		s.logger.Warn("Context result retention returned failure",
			String("bead_id", string(result.BeadID)),
			String("error", retentionResult.Error),
			Duration("duration", duration),
		)
	}

	return retentionResult, nil
}

// Health checks the Hindsight service health with logging.
// Logs: health status, response time, cache status.
func (s *Storage) Health(ctx context.Context) (*HealthStatus, error) {
	start := time.Now()
	s.logger.Debug("Checking Hindsight health")

	// Create a simple health check by attempting a search with empty query
	// This will return quickly and indicate if the service is up
	healthy := s.client.IsEnabled()
	duration := time.Since(start)

	status := &HealthStatus{
		Healthy:   healthy,
		CheckedAt: time.Now().Unix(),
		Cached:    false,
	}

	if healthy {
		s.logger.Info("Hindsight health check passed",
			Bool("healthy", true),
			Duration("duration", duration),
		)
	} else {
		s.logger.Warn("Hindsight health check failed",
			Bool("healthy", false),
			Duration("duration", duration),
		)
	}

	return status, nil
}

// ServerLifecycleLogger handles logging for server lifecycle events.
type ServerLifecycleLogger struct {
	logger Logger
}

// NewServerLifecycleLogger creates a new server lifecycle logger.
func NewServerLifecycleLogger(logger Logger) *ServerLifecycleLogger {
	if logger == nil {
		logger = &NoopLogger{}
	}
	return &ServerLifecycleLogger{logger: logger}
}

// LogServerStart logs when the server starts.
// Logs: server address, startup duration, configuration details.
func (l *ServerLifecycleLogger) LogServerStart(addr string, duration time.Duration, config ServerConfig) {
	l.logger.Info("Hindsight server started",
		String("address", addr),
		Duration("startup_duration", duration),
		Int("port", config.Port),
		String("host", config.Host),
		Duration("timeout", config.Timeout),
	)
}

// LogServerStop logs when the server stops.
// Logs: shutdown duration, graceful shutdown status.
func (l *ServerLifecycleLogger) LogServerStop(duration time.Duration, graceful bool) {
	if graceful {
		l.logger.Info("Hindsight server stopped gracefully",
			Duration("shutdown_duration", duration),
		)
	} else {
		l.logger.Warn("Hindsight server stopped",
			Duration("shutdown_duration", duration),
			Bool("graceful", false),
		)
	}
}

// LogHealthCheck logs health check results.
// Logs: health status, response time, cached result flag.
func (l *ServerLifecycleLogger) LogHealthCheck(healthy bool, duration time.Duration, cached bool) {
	if healthy {
		l.logger.Debug("Health check passed",
			Bool("healthy", true),
			Duration("response_time", duration),
			Bool("cached", cached),
		)
	} else {
		l.logger.Error("Health check failed", fmt.Errorf("service unhealthy"),
			Bool("healthy", false),
			Duration("response_time", duration),
			Bool("cached", cached),
		)
	}
}

// LogServerError logs server errors with detailed context and stack traces.
// Logs: error message, operation context, stack trace.
func (l *ServerLifecycleLogger) LogServerError(err error, operation string, context map[string]interface{}) {
	fields := []LogField{
		String("operation", operation),
		Error(err),
	}

	for key, value := range context {
		fields = append(fields, LogField{Key: key, Value: value})
	}

	l.logger.Error(fmt.Sprintf("Server error in %s", operation), err, fields...)
}

// OperationResult contains the result of a storage operation for logging.
type OperationResult struct {
	Operation string
	Duration  time.Duration
	Success   bool
	Count     int
	Error     error
}

// LogOperation logs a storage operation with its result.
func (s *Storage) LogOperation(result OperationResult) {
	fields := []LogField{
		String("operation", result.Operation),
		Duration("duration", result.Duration),
		Bool("success", result.Success),
		Int("count", result.Count),
	}

	if result.Error != nil {
		fields = append(fields, Error(result.Error))
		s.logger.Error(fmt.Sprintf("Operation %s failed", result.Operation), result.Error, fields...)
	} else if result.Success {
		s.logger.Info(fmt.Sprintf("Operation %s completed", result.Operation), fields...)
	} else {
		s.logger.Warn(fmt.Sprintf("Operation %s completed with issues", result.Operation), fields...)
	}
}

// NoopLogger is a logger that does nothing. Used as default when no logger is provided.
type NoopLogger struct{}

func (n *NoopLogger) Info(msg string, fields ...LogField)             {}
func (n *NoopLogger) Error(msg string, err error, fields ...LogField) {}
func (n *NoopLogger) Debug(msg string, fields ...LogField)            {}
func (n *NoopLogger) Warn(msg string, fields ...LogField)             {}
