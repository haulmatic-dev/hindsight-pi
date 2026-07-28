package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const maxLineSize = 1024 * 1024 // 1 MiB

const logTimeLayout = "2006/01/02 15:04:05"

var sinceLayouts = []string{
	"2006/01/02 15:04:05",
	"2006/01/02",
}

var closeEventMarker = []byte("[CloseEvent] ")

// CloseEvent represents a parsed [CloseEvent] line from the daemon log.
type CloseEvent struct {
	BeadID  string    `json:"bead_id"`
	Verdict string    `json:"verdict"`
	Reason  string    `json:"reason"`
	Attempt int       `json:"attempt"`
	Time    time.Time `json:"-"`
}

// BeadRecord represents one line from a beads JSONL export.
type BeadRecord struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	IssueType string `json:"issue_type"`
	CreatedAt string `json:"created_at"`
	ClosedAt  string `json:"closed_at"`
}

// ParseCloseEvents scans a daemon log for lines containing the
// "[CloseEvent] " marker, decodes the JSON payload after the marker, and
// returns the slice of successfully decoded events. Lines without the marker
// or with undecodable payloads are skipped.
func ParseCloseEvents(r io.Reader) []CloseEvent {
	return collectLines(r, parseCloseEvent)
}

// ParseBeadsJSONL reads one JSON object per line, skipping blank and
// undecodable lines, and returns the successfully parsed bead records.
func ParseBeadsJSONL(r io.Reader) []BeadRecord {
	return collectLines(r, parseBeadRecord)
}

// CountMarker returns the number of lines in r that contain marker as a
// substring.
func CountMarker(r io.Reader, marker string) int {
	markerBytes := []byte(marker)
	var count int
	scanLines(r, func(line []byte) {
		if bytes.Contains(line, markerBytes) {
			count++
		}
	})
	return count
}

// ParseDaemonLog parses a daemon log in a single pass, returning close
// events and the count of each requested marker in the order the markers
// are supplied. It does not apply any time-based filtering.
func ParseDaemonLog(r io.Reader, markers []string) (events []CloseEvent, counts []int) {
	return ParseDaemonLogSince(r, markers, time.Time{})
}

// ParseDaemonLogSince parses a daemon log in a single pass, applying a
// --since timestamp filter before extracting close events and counting
// markers. A zero since value disables filtering.
func ParseDaemonLogSince(r io.Reader, markers []string, since time.Time) (events []CloseEvent, counts []int) {
	markerBytes := make([][]byte, len(markers))
	for i, m := range markers {
		markerBytes[i] = []byte(m)
	}
	counts = make([]int, len(markers))

	f := newSinceFilter(since)
	scanLines(r, func(line []byte) {
		if !f.allow(line) {
			return
		}
		// Skip worker stderr that the daemon streams into its own log. Those
		// lines carry the model's raw JSONL — including its thinking text — so a
		// substring marker matches whatever the model happened to say.
		//
		// Measured on run H: the "closed as failed" marker matched 7,884 lines, of
		// which 7,739 were worker passthrough (mostly thinking_delta text) and only
		// 145 were genuine [closeBead] lines. failed_closes was reported as 6,707,
		// which is not a count of anything real.
		if isWorkerPassthrough(line) {
			return
		}
		if ev, ok := parseCloseEvent(line); ok {
			if t, ok := ParseLogTime(line); ok {
				ev.Time = t
			}
			events = append(events, ev)
		}
		for i, m := range markerBytes {
			if bytes.Contains(line, m) {
				counts[i]++
			}
		}
	})
	return events, counts
}

// isWorkerPassthrough reports whether a daemon-log line is streamed worker
// stderr rather than something the daemon itself emitted. The daemon prefixes
// those with "[Worker:<bead>] [stderr]", and their payload is the model's own
// output, so they must never be counted as daemon events.
func isWorkerPassthrough(line []byte) bool {
	return bytes.Contains(line, []byte("[stderr]")) || bytes.Contains(line, []byte("[stdout]"))
}

// collectLines scans input and collects every value for which parse returns
// true. It is the common driver behind ParseCloseEvents and ParseBeadsJSONL
// so both parsers share the same line-reading machinery.
func collectLines[T any](r io.Reader, parse func([]byte) (T, bool)) []T {
	var result []T
	scanLines(r, func(line []byte) {
		if v, ok := parse(line); ok {
			result = append(result, v)
		}
	})
	return result
}

// parseCloseEvent extracts a CloseEvent from a single log line. It returns
// false when the line does not contain the marker or the payload is not
// valid JSON.
func parseCloseEvent(line []byte) (CloseEvent, bool) {
	payload, ok := payloadAfter(line, closeEventMarker)
	if !ok {
		return CloseEvent{}, false
	}
	var ev CloseEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return CloseEvent{}, false
	}
	return ev, true
}

// parseBeadRecord extracts a BeadRecord from a single JSONL line. It
// returns false for blank or invalid lines.
func parseBeadRecord(line []byte) (BeadRecord, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return BeadRecord{}, false
	}
	var rec BeadRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return BeadRecord{}, false
	}
	return rec, true
}

// scanLines reads r line by line using a 1 MiB buffered scanner and calls
// fn for each line. Scanner errors are ignored; any line exceeding the
// configured limit is silently dropped, matching the tool's best-effort
// log parsing semantics.
func scanLines(r io.Reader, fn func([]byte)) {
	scanner := newScanner(r)
	for scanner.Scan() {
		fn(scanner.Bytes())
	}
}

// newScanner returns a bufio.Scanner configured with a 1 MiB token buffer
// so long log lines do not cause scanner.Err() after a partial read.
func newScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, maxLineSize), maxLineSize)
	return scanner
}

// ParseLogTime parses the 19-character "YYYY/MM/DD HH:MM:SS" prefix from a
// daemon log line and returns the corresponding time. It returns false when
// the line is too short, the prefix is not followed by a space or end of
// line, or the prefix does not match the expected layout.
func ParseLogTime(line []byte) (time.Time, bool) {
	if len(line) < len(logTimeLayout) {
		return time.Time{}, false
	}
	if len(line) > len(logTimeLayout) && line[len(logTimeLayout)] != ' ' {
		return time.Time{}, false
	}
	t, err := time.Parse(logTimeLayout, string(line[:len(logTimeLayout)]))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ParseSince parses a user-supplied --since value. It accepts both the full
// "2006/01/02 15:04:05" layout and the date-only "2006/01/02" layout,
// which is interpreted as midnight UTC.
func ParseSince(s string) (time.Time, error) {
	for _, layout := range sinceLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid --since value %q: accepted layouts are %q and %q", s, sinceLayouts[0], sinceLayouts[1])
}

// FilterSince returns the lines from lines that are either timestamped at or
// after since, or untimestamped continuation lines that appear after the
// first kept timestamped line. When since is the zero time, the input slice
// is returned unchanged.
func FilterSince(lines [][]byte, since time.Time) [][]byte {
	if since.IsZero() {
		return lines
	}
	f := newSinceFilter(since)
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if f.allow(line) {
			out = append(out, line)
		}
	}
	return out
}

// sinceFilter tracks whether subsequent log lines should be included based on
// the --since threshold and the timestamp/continuation semantics of the
// daemon log.
type sinceFilter struct {
	since time.Time
	keep  bool
}

// newSinceFilter returns a filter that includes all lines when since is the
// zero time.
func newSinceFilter(since time.Time) sinceFilter {
	return sinceFilter{since: since}
}

// allow reports whether line should be included. Timestamped lines update
// the keep state; untimestamped lines are included only if the previous
// timestamped line was kept.
func (f *sinceFilter) allow(line []byte) bool {
	if f.since.IsZero() {
		return true
	}
	if t, ok := ParseLogTime(line); ok {
		f.keep = !t.Before(f.since)
		return f.keep
	}
	return f.keep
}

// payloadAfter returns the portion of line following the last occurrence of
// marker. Using the last occurrence allows arbitrary prefix text before the
// marker while still producing the JSON payload that follows it.
func payloadAfter(line, marker []byte) ([]byte, bool) {
	idx := bytes.LastIndex(line, marker)
	if idx == -1 {
		return nil, false
	}
	return line[idx+len(marker):], true
}
