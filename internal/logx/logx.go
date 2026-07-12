// Package logx provides lightweight leveled logging with level-based stream
// routing. DEBUG/INFO go to stdout, WARN/ERROR to stderr, so operators can
// separate signal (errors) from chatter — and so systemd's StandardError sink
// stops accumulating every info line. The threshold is set from
// HYDRA_LOG_LEVEL (debug|info|warn|error; default info).
//
// It is intentionally a thin shim over the standard log package so call sites
// can be migrated incrementally; existing log.Printf calls keep working
// (writing to stderr) until converted.
package logx

import (
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// Level is a logging severity threshold.
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var (
	level  atomic.Int32
	stdout = log.New(os.Stdout, "", log.LstdFlags)
	stderr = log.New(os.Stderr, "", log.LstdFlags)
)

func init() { level.Store(int32(LevelInfo)) }

// Init sets the threshold from the HYDRA_LOG_LEVEL environment variable.
func Init() { SetLevel(ParseLevel(os.Getenv("HYDRA_LOG_LEVEL"))) }

// SetLevel sets the minimum level that will be emitted.
func SetLevel(l Level) { level.Store(int32(l)) }

// ParseLevel maps a string to a Level, defaulting to Info.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Enabled reports whether a message at level l would be emitted.
func Enabled(l Level) bool { return l >= Level(level.Load()) }

// Debugf logs at DEBUG to stdout (suppressed unless HYDRA_LOG_LEVEL=debug).
func Debugf(format string, args ...any) {
	if Enabled(LevelDebug) {
		stdout.Printf("[DEBUG] "+format, args...)
	}
}

// Infof logs at INFO to stdout.
func Infof(format string, args ...any) {
	if Enabled(LevelInfo) {
		stdout.Printf(format, args...)
	}
}

// Warnf logs at WARN to stderr.
func Warnf(format string, args ...any) {
	if Enabled(LevelWarn) {
		stderr.Printf("[WARN] "+format, args...)
	}
}

// Errorf logs at ERROR to stderr.
func Errorf(format string, args ...any) {
	if Enabled(LevelError) {
		stderr.Printf("[ERROR] "+format, args...)
	}
}
