package main

import (
	"bufio"
	"bytes"
	"io"
	"time"
)

const (
	logTimeFormat   = "2006/01/02 15:04:05"
	logTimestampLen = len(logTimeFormat)
	maxLineSize     = 1 << 20 // 1 MiB
)

// Signature describes a known daemon-log anomaly pattern.
type Signature struct {
	Key       string // stable identifier, e.g. "empty-diff"
	Marker    string // substring to match, case-sensitive
	Severity  string // "critical" | "warning"
	Threshold int    // min hits in the window before it becomes an anomaly
}

// Hit is the result of scanning a log window for one signature.
type Hit struct {
	Key   string
	Count int
	First string // first matching line, verbatim (evidence)
}

// DefaultSignatures returns the built-in set of observer signatures.
// Markers are taken from the actual daemon log call sites:
//   - empty-diff: internal/core/bead_closer.go:339,407
//   - stuck-worker: internal/core/charon.go:221
//   - panic: Go runtime panic headers
//   - blocked-user: internal/core/multi_user_discovery.go:87
//   - escalation: internal/core/bead_closer.go:397
//   - worker-timeout: internal/core/agent_worker.go:139
//   - max-cycles: internal/core/compliance_cycle.go:168
//   - orphan-recovery: internal/core/charon.go
//   - quota-exhausted: br / API 403 usage-limit responses
func DefaultSignatures() []Signature {
	return []Signature{
		{Key: "empty-diff", Marker: "(empty-diff)", Severity: "critical", Threshold: 3},
		{Key: "stuck-worker", Marker: "STUCK WORKER DETECTED", Severity: "warning", Threshold: 1},
		{Key: "panic", Marker: "panic:", Severity: "critical", Threshold: 1},
		{Key: "blocked-user", Marker: "] BLOCKED ", Severity: "warning", Threshold: 1},
		{Key: "escalation", Marker: "escalating to human", Severity: "warning", Threshold: 1},
		{Key: "worker-timeout", Marker: "exceeded timeout", Severity: "warning", Threshold: 3},
		{Key: "max-cycles", Marker: "MAX CYCLES", Severity: "warning", Threshold: 1},
		{Key: "orphan-recovery", Marker: "reopened orphan bead", Severity: "warning", Threshold: 3},
		{Key: "quota-exhausted", Marker: "usage limit", Severity: "critical", Threshold: 1},
	}
}

// signatureCounter holds the compiled marker and running tally for one
// Signature. It is the internal, mutable representation used by the scanner.
type signatureCounter struct {
	Signature
	markerBytes []byte
	count       int
	firstMatch  string
}

// newSignatureCounters turns the public Signature slice into a flat, indexable
// table used by the single-pass scanner.
func newSignatureCounters(sigs []Signature) []signatureCounter {
	counters := make([]signatureCounter, len(sigs))
	for i, s := range sigs {
		counters[i] = signatureCounter{
			Signature:   s,
			markerBytes: []byte(s.Marker),
		}
	}
	return counters
}

// ScanLog parses the daemon log in r, applies the since filter (zero value
// disables filtering), counts each signature marker, and returns a Hit for
// every signature whose count meets or exceeds its threshold.
func ScanLog(r io.Reader, since time.Time, sigs []Signature) []Hit {
	counters := newSignatureCounters(sigs)
	filter := newSinceFilter(since)
	scanner := newBufferedScanner(r)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !filter.include(line) {
			continue
		}
		matchLine(counters, line)
	}

	return collectHits(counters)
}

// matchLine increments every counter whose marker appears in line and records
// the first matching line for each counter.
func matchLine(counters []signatureCounter, line []byte) {
	for i := range counters {
		c := &counters[i]
		if !bytes.Contains(line, c.markerBytes) {
			continue
		}
		if c.count == 0 {
			c.firstMatch = string(line)
		}
		c.count++
	}
}

// collectHits returns the public Hit representation for every counter that
// reached its threshold.
func collectHits(counters []signatureCounter) []Hit {
	hits := make([]Hit, 0, len(counters))
	for _, c := range counters {
		if c.count >= c.Threshold {
			hits = append(hits, Hit{
				Key:   c.Key,
				Count: c.count,
				First: c.firstMatch,
			})
		}
	}
	return hits
}

// newBufferedScanner returns a bufio.Scanner configured with a 1 MiB token
// buffer so long log lines do not cause scanner.Err() after a partial read.
func newBufferedScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, maxLineSize), maxLineSize)
	return scanner
}

// parseLogTimestamp parses the fixed-width "YYYY/MM/DD HH:MM:SS" prefix from a
// daemon log line and returns the corresponding time.
func parseLogTimestamp(line []byte) (time.Time, bool) {
	if len(line) < logTimestampLen {
		return time.Time{}, false
	}
	if len(line) > logTimestampLen && line[logTimestampLen] != ' ' {
		return time.Time{}, false
	}
	t, err := time.Parse(logTimeFormat, string(line[:logTimestampLen]))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// sinceFilter decides whether a log line falls inside the --since window. It
// carries state across calls because untimestamped continuation lines inherit
// the inclusion decision of the preceding timestamped line.
type sinceFilter struct {
	since time.Time
	keep  bool
}

func newSinceFilter(since time.Time) sinceFilter {
	return sinceFilter{since: since}
}

// include reports whether line should be included. Timestamped lines update
// the internal keep state; untimestamped continuation lines inherit it.
func (f *sinceFilter) include(line []byte) bool {
	if f.since.IsZero() {
		return true
	}
	if t, ok := parseLogTimestamp(line); ok {
		f.keep = !t.Before(f.since)
		return f.keep
	}
	return f.keep
}
