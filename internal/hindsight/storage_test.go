package hindsight

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// MockLogger implements Logger for testing
type MockLogger struct {
	InfoLogs  []string
	ErrorLogs []string
	DebugLogs []string
	WarnLogs  []string
	Fields    []LogField
}

func NewMockLogger() *MockLogger {
	return &MockLogger{
		InfoLogs:  make([]string, 0),
		ErrorLogs: make([]string, 0),
		DebugLogs: make([]string, 0),
		WarnLogs:  make([]string, 0),
		Fields:    make([]LogField, 0),
	}
}

func (m *MockLogger) Info(msg string, fields ...LogField) {
	m.InfoLogs = append(m.InfoLogs, msg)
	m.Fields = append(m.Fields, fields...)
}

func (m *MockLogger) Error(msg string, err error, fields ...LogField) {
	m.ErrorLogs = append(m.ErrorLogs, msg)
	m.Fields = append(m.Fields, fields...)
}

func (m *MockLogger) Debug(msg string, fields ...LogField) {
	m.DebugLogs = append(m.DebugLogs, msg)
	m.Fields = append(m.Fields, fields...)
}

func (m *MockLogger) Warn(msg string, fields ...LogField) {
	m.WarnLogs = append(m.WarnLogs, msg)
	m.Fields = append(m.Fields, fields...)
}

func TestNewStorage(t *testing.T) {
	client := NewClient()
	logger := NewMockLogger()

	storage := NewStorage(client, logger)
	if storage == nil {
		t.Fatal("NewStorage returned nil")
	}

	if storage.client != client {
		t.Error("Storage client not set correctly")
	}

	if storage.logger != logger {
		t.Error("Storage logger not set correctly")
	}
}

func TestNewStorage_NilClient(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewStorage should panic with nil client")
		}
	}()

	NewStorage(nil, NewMockLogger())
}

func TestNewStorage_NilLogger(t *testing.T) {
	client := NewClient()
	storage := NewStorage(client, nil)

	if storage == nil {
		t.Fatal("NewStorage returned nil")
	}

	if storage.logger == nil {
		t.Error("Storage should have default logger when nil provided")
	}
}

func TestStorage_Search(t *testing.T) {
	client := NewClient()
	logger := NewMockLogger()
	storage := NewStorage(client, logger)
	ctx := context.Background()

	// Test with disabled client (should return empty result)
	client = NewClient(WithEnabled(false))
	storage = NewStorage(client, logger)

	resp, err := storage.Search(ctx, "test query", "implement", 5)
	if err != nil {
		t.Errorf("Search with disabled client should not error: %v", err)
	}

	if resp == nil {
		t.Fatal("Search returned nil response")
	}

	if resp.Count != 0 {
		t.Errorf("Expected 0 results with disabled client, got %d", resp.Count)
	}
}

func TestStorage_SearchByBead(t *testing.T) {
	client := NewClient(WithEnabled(false))
	logger := NewMockLogger()
	storage := NewStorage(client, logger)
	ctx := context.Background()

	resp, err := storage.SearchByBead(ctx, "test title", "implement")
	if err != nil {
		t.Errorf("SearchByBead with disabled client should not error: %v", err)
	}

	if resp == nil {
		t.Fatal("SearchByBead returned nil response")
	}
}

func TestStorage_Retain(t *testing.T) {
	client := NewClient(WithEnabled(false))
	logger := NewMockLogger()
	storage := NewStorage(client, logger)
	ctx := context.Background()

	memory := &ExecutionMemory{
		BeadID: "test-bead-123",
	}

	result, err := storage.Retain(ctx, memory)
	if err != nil {
		t.Errorf("Retain with disabled client should not error: %v", err)
	}

	if result == nil {
		t.Fatal("Retain returned nil result")
	}

	if !result.Success {
		t.Error("Retain should return success with disabled client")
	}
}

func TestStorage_RetainContextResult(t *testing.T) {
	client := NewClient(WithEnabled(false))
	logger := NewMockLogger()
	storage := NewStorage(client, logger)
	ctx := context.Background()

	result := NewContextProcessorResult(types.Bead{ID: "test-bead-123"}, "test-processor")

	retentionResult, err := storage.RetainContextResult(ctx, result)
	if err != nil {
		t.Errorf("RetainContextResult with disabled client should not error: %v", err)
	}

	if retentionResult == nil {
		t.Fatal("RetainContextResult returned nil result")
	}
}

func TestStorage_Health(t *testing.T) {
	client := NewClient(WithEnabled(true))
	logger := NewMockLogger()
	storage := NewStorage(client, logger)
	ctx := context.Background()

	status, err := storage.Health(ctx)
	if err != nil {
		t.Errorf("Health should not error: %v", err)
	}

	if status == nil {
		t.Fatal("Health returned nil status")
	}

	if !status.Healthy {
		t.Error("Health should return healthy when client is enabled")
	}
}

func TestNewServerLifecycleLogger(t *testing.T) {
	logger := NewMockLogger()
	slLogger := NewServerLifecycleLogger(logger)

	if slLogger == nil {
		t.Fatal("NewServerLifecycleLogger returned nil")
	}

	if slLogger.logger != logger {
		t.Error("ServerLifecycleLogger logger not set correctly")
	}
}

func TestNewServerLifecycleLogger_NilLogger(t *testing.T) {
	slLogger := NewServerLifecycleLogger(nil)

	if slLogger == nil {
		t.Fatal("NewServerLifecycleLogger returned nil")
	}

	if slLogger.logger == nil {
		t.Error("ServerLifecycleLogger should have default logger when nil provided")
	}
}

func TestServerLifecycleLogger_LogServerStart(t *testing.T) {
	logger := NewMockLogger()
	slLogger := NewServerLifecycleLogger(logger)

	config := DefaultServerConfig()
	slLogger.LogServerStart("127.0.0.1:8888", 100*time.Millisecond, config)

	if len(logger.InfoLogs) != 1 {
		t.Errorf("Expected 1 info log, got %d", len(logger.InfoLogs))
	}
}

func TestServerLifecycleLogger_LogServerStop(t *testing.T) {
	logger := NewMockLogger()
	slLogger := NewServerLifecycleLogger(logger)

	// Test graceful stop
	slLogger.LogServerStop(50*time.Millisecond, true)
	if len(logger.InfoLogs) != 1 {
		t.Errorf("Expected 1 info log for graceful stop, got %d", len(logger.InfoLogs))
	}

	// Test non-graceful stop
	logger2 := NewMockLogger()
	slLogger2 := NewServerLifecycleLogger(logger2)
	slLogger2.LogServerStop(50*time.Millisecond, false)
	if len(logger2.WarnLogs) != 1 {
		t.Errorf("Expected 1 warn log for non-graceful stop, got %d", len(logger2.WarnLogs))
	}
}

func TestServerLifecycleLogger_LogHealthCheck(t *testing.T) {
	logger := NewMockLogger()
	slLogger := NewServerLifecycleLogger(logger)

	// Test healthy
	slLogger.LogHealthCheck(true, 10*time.Millisecond, false)
	if len(logger.DebugLogs) != 1 {
		t.Errorf("Expected 1 debug log for healthy check, got %d", len(logger.DebugLogs))
	}

	// Test unhealthy
	logger2 := NewMockLogger()
	slLogger2 := NewServerLifecycleLogger(logger2)
	slLogger2.LogHealthCheck(false, 10*time.Millisecond, false)
	if len(logger2.ErrorLogs) != 1 {
		t.Errorf("Expected 1 error log for unhealthy check, got %d", len(logger2.ErrorLogs))
	}
}

func TestServerLifecycleLogger_LogServerError(t *testing.T) {
	logger := NewMockLogger()
	slLogger := NewServerLifecycleLogger(logger)

	testErr := errors.New("test error")
	context := map[string]interface{}{
		"operation": "test-op",
		"detail":    "test detail",
	}

	slLogger.LogServerError(testErr, "test operation", context)

	if len(logger.ErrorLogs) != 1 {
		t.Errorf("Expected 1 error log, got %d", len(logger.ErrorLogs))
	}
}

func TestStorage_LogOperation(t *testing.T) {
	logger := NewMockLogger()
	client := NewClient()
	storage := NewStorage(client, logger)

	// Test successful operation
	storage.LogOperation(OperationResult{
		Operation: "test-op",
		Duration:  100 * time.Millisecond,
		Success:   true,
		Count:     5,
	})

	if len(logger.InfoLogs) != 1 {
		t.Errorf("Expected 1 info log for successful operation, got %d", len(logger.InfoLogs))
	}

	// Test failed operation
	logger2 := NewMockLogger()
	storage2 := NewStorage(client, logger2)
	storage2.LogOperation(OperationResult{
		Operation: "test-op",
		Duration:  100 * time.Millisecond,
		Success:   false,
		Count:     0,
		Error:     errors.New("test error"),
	})

	if len(logger2.ErrorLogs) != 1 {
		t.Errorf("Expected 1 error log for failed operation, got %d", len(logger2.ErrorLogs))
	}

	// Test unsuccessful but no error
	logger3 := NewMockLogger()
	storage3 := NewStorage(client, logger3)
	storage3.LogOperation(OperationResult{
		Operation: "test-op",
		Duration:  100 * time.Millisecond,
		Success:   false,
		Count:     0,
	})

	if len(logger3.WarnLogs) != 1 {
		t.Errorf("Expected 1 warn log for unsuccessful operation, got %d", len(logger3.WarnLogs))
	}
}

func TestLogFieldHelpers(t *testing.T) {
	// Test String
	sf := String("key", "value")
	if sf.Key != "key" || sf.Value != "value" {
		t.Error("String field not created correctly")
	}

	// Test Int
	intf := Int("count", 42)
	if intf.Key != "count" || intf.Value != 42 {
		t.Error("Int field not created correctly")
	}

	// Test Duration
	df := Duration("elapsed", 5*time.Second)
	if df.Key != "elapsed" || df.Value != 5*time.Second {
		t.Error("Duration field not created correctly")
	}

	// Test Float64
	ff := Float64("score", 0.95)
	if ff.Key != "score" || ff.Value != 0.95 {
		t.Error("Float64 field not created correctly")
	}

	// Test Bool
	bf := Bool("enabled", true)
	if bf.Key != "enabled" || bf.Value != true {
		t.Error("Bool field not created correctly")
	}

	// Test Error
	testErr := errors.New("test error")
	ef := Error(testErr)
	if ef.Key != "error" || ef.Value != testErr {
		t.Error("Error field not created correctly")
	}
}

// Mock types.Bead for testing
type mockBead struct {
	ID string
}

func (m mockBead) GetID() string {
	return m.ID
}
