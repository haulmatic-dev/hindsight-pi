package core

import "log"

// ErrorLogger provides structured error logging for the overseer.
type ErrorLogger struct {
	getRetryCount func(string) int
}

// NewErrorLogger creates a new error logger with the given retry count function.
func NewErrorLogger(getRetryCount func(string) int) *ErrorLogger {
	return &ErrorLogger{getRetryCount: getRetryCount}
}

// Error logs an error with structured context information.
func (el *ErrorLogger) Error(component, operation, beadID string, err error, severity string) {
	retryCount := el.getRetryCount(beadID)

	switch severity {
	case "fatal":
		log.Printf("[FATAL] [%s] %s failed | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	case "error":
		log.Printf("[ERROR] [%s] %s failed | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	case "warn", "warning":
		log.Printf("[WARN] [%s] %s issue | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	default:
		log.Printf("[INFO] [%s] %s | bead=%s | retries=%d | error=%v",
			component, operation, beadID, retryCount, err)
	}
}

// GracefulDegradation logs a non-fatal error that the system recovered from.
func (el *ErrorLogger) GracefulDegradation(component, operation, action string, err error) {
	log.Printf("[DEGRADED] [%s] %s failed but continuing | recovery=%s | error=%v",
		component, operation, action, err)
}
