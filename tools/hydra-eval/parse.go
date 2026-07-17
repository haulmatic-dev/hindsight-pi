package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

const maxLineSize = 1024 * 1024 // 1 MiB

var closeEventMarker = []byte("[CloseEvent] ")

// CloseEvent represents a parsed [CloseEvent] line from the daemon log.
type CloseEvent struct {
	BeadID  string `json:"bead_id"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	Attempt int    `json:"attempt"`
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
// are supplied.
func ParseDaemonLog(r io.Reader, markers []string) (events []CloseEvent, counts []int) {
	markerBytes := make([][]byte, len(markers))
	for i, m := range markers {
		markerBytes[i] = []byte(m)
	}
	counts = make([]int, len(markers))

	scanLines(r, func(line []byte) {
		if ev, ok := parseCloseEvent(line); ok {
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
