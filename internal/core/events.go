// Package core — structured event stream (Shepherd track C1/C2).
//
// The daemon's operational history currently lives in one text log that mixes
// daemon lines with the full worker-stderr firehose. That design has produced a
// repeating failure class: substring searches matching the model's own words
// (four separate incidents), an 8.3 GB/day file, and run scoring that broke on
// log rotation. Metrics derived from grep are only as honest as the grep.
//
// Events fix the class, not the instances: the daemon emits typed JSONL records
// to two destinations —
//
//   - a central stream (one file per day under <HYDRA_HOME>/events/) that
//     hydra-eval and the Shepherd cron read for cross-run analysis, and
//   - the affected repo's .hydra/events.jsonl, so each user can see what hydra
//     did to their repo without access to the daemon host. Per the two-tier
//     design: the repo copy is the user's window and responsibility; the
//     central copy is ours.
//
// Worker output never enters either stream. Every record is written by daemon
// code about a daemon decision.
package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event is one structured daemon decision. Fields are flat and stable: these
// records are a contract for tooling, not a debug log.
type Event struct {
	// Time is RFC3339 with millisecond precision, UTC.
	Time string `json:"time"`
	// Type is the event kind, e.g. "bead_closed", "fix_created",
	// "fix_capped", "fix_deduped", "worker_timeout", "provider_overload",
	// "graph_refreshed", "escalation".
	Type string `json:"type"`
	// Project is the project name when the event concerns one.
	Project string `json:"project,omitempty"`
	// Bead is the bead ID when the event concerns one.
	Bead string `json:"bead,omitempty"`
	// Reason is a short machine-stable slug ("failed", "recursion_capped",
	// "duplicate_fix_exists"), not prose.
	Reason string `json:"reason,omitempty"`
	// Detail is one human-readable sentence. Prose lives here and only here.
	Detail string `json:"detail,omitempty"`
	// Fields carries any extra structured values (exit codes, counts, token
	// totals). Values must be JSON-marshalable.
	Fields map[string]any `json:"fields,omitempty"`
}

// EventSink writes events to the central stream and mirrors repo-scoped ones
// into the repo's .hydra directory. Safe for concurrent use. A nil *EventSink
// is a no-op on every method, so callers never need nil checks.
type EventSink struct {
	mu         sync.Mutex
	centralDir string
	now        func() time.Time // injectable for tests
}

// NewEventSink builds a sink writing its central stream under centralDir
// (created on first write). An empty centralDir disables the central stream but
// keeps repo mirroring functional.
func NewEventSink(centralDir string) *EventSink {
	return &EventSink{centralDir: centralDir, now: time.Now}
}

// Emit writes ev to the central stream, stamping Time. Failures are swallowed
// after best effort: the event stream observes the system and must never be
// able to break it. Returns the stamped event so callers can mirror it.
func (s *EventSink) Emit(ev Event) Event {
	if s == nil {
		return ev
	}
	if ev.Time == "" {
		ev.Time = s.now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	if ev.Type == "" {
		// A typeless event is a programming error; make it visible in the
		// stream rather than dropping it silently.
		ev.Type = "unknown"
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return ev
	}
	if s.centralDir != "" {
		s.mu.Lock()
		s.appendLocked(filepath.Join(s.centralDir, s.now().UTC().Format("2006-01-02")+".jsonl"), line)
		s.mu.Unlock()
	}
	return ev
}

// EmitRepo writes ev to the central stream AND mirrors it into
// <repoPath>/.hydra/events.jsonl, ensuring .hydra/ stays gitignored via the
// same helper the activity log uses. Use for events a repo's owner should see:
// closes, fix beads, escalations touching their code.
func (s *EventSink) EmitRepo(repoPath string, ev Event) Event {
	ev = s.Emit(ev)
	if s == nil || strings.TrimSpace(repoPath) == "" {
		return ev
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return ev
	}
	hydraDir := filepath.Join(repoPath, ".hydra")
	if err := os.MkdirAll(hydraDir, 0o755); err != nil {
		return ev
	}
	s.mu.Lock()
	s.appendLocked(filepath.Join(hydraDir, "events.jsonl"), line)
	s.mu.Unlock()
	// .hydra/ must never be committed into the user's repo.
	ensureGitignoreLine(filepath.Join(repoPath, ".gitignore"), ".hydra/")
	return ev
}

// appendLocked appends one line, creating parents as needed. Caller holds mu.
func (s *EventSink) appendLocked(path string, line []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s\n", line)
}

// ReadEvents parses a JSONL event file, skipping blank and undecodable lines —
// a torn final line from a crash must not poison the whole read. This is the
// reader the Shepherd cron and hydra-eval build on.
func ReadEvents(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Event
	for _, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

// DefaultEventsDir resolves the central events directory from HYDRA_HOME.
// With HYDRA_HOME unset (dev runs, go test) it returns "", which disables the
// central stream — a cwd-relative fallback wrote day-files into whatever
// directory constructed an overseer, including the repo during tests.
func DefaultEventsDir() string {
	if home := os.Getenv("HYDRA_HOME"); home != "" {
		return filepath.Join(home, "events")
	}
	return ""
}
