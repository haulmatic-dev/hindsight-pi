package main

import (
	"strings"
	"testing"
	"time"
)

var emptyDiffSig = Signature{Key: "empty-diff", Marker: "(empty-diff)", Severity: "critical", Threshold: 3}
var stuckWorkerSig = Signature{Key: "stuck-worker", Marker: "STUCK WORKER DETECTED", Severity: "warning", Threshold: 1}
var panicSig = Signature{Key: "panic", Marker: "panic:", Severity: "critical", Threshold: 1}

func TestScanLog_EmptyDiffAboveThresholdSinceBetween(t *testing.T) {
	input := `2026/07/17 08:00:00 [VerifiedClose] Bead b1 rejected (empty-diff)
2026/07/17 09:00:00 [VerifiedClose] Bead b2 rejected (empty-diff)
2026/07/17 10:00:00 [VerifiedClose] Bead b3 rejected (empty-diff)
2026/07/17 11:00:00 [VerifiedClose] Bead b4 rejected (empty-diff)
2026/07/17 12:00:00 [VerifiedClose] Bead b5 rejected (empty-diff)
`
	since := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	hits := ScanLog(strings.NewReader(input), since, []Signature{emptyDiffSig})

	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Key != "empty-diff" {
		t.Errorf("expected Key empty-diff, got %q", hits[0].Key)
	}
	if hits[0].Count != 3 {
		t.Errorf("expected Count 3, got %d", hits[0].Count)
	}
	wantFirst := "2026/07/17 10:00:00 [VerifiedClose] Bead b3 rejected (empty-diff)"
	if hits[0].First != wantFirst {
		t.Errorf("expected First %q, got %q", wantFirst, hits[0].First)
	}
}

func TestScanLog_EmptyDiffBelowThreshold(t *testing.T) {
	input := `2026/07/17 08:00:00 [VerifiedClose] Bead b1 rejected (empty-diff)
2026/07/17 09:00:00 [VerifiedClose] Bead b2 rejected (empty-diff)
2026/07/17 10:00:00 [VerifiedClose] Bead b3 rejected (empty-diff)
2026/07/17 11:00:00 [VerifiedClose] Bead b4 rejected (empty-diff)
2026/07/17 12:00:00 [VerifiedClose] Bead b5 rejected (empty-diff)
`
	since := time.Date(2026, time.July, 17, 11, 0, 0, 0, time.UTC)
	hits := ScanLog(strings.NewReader(input), since, []Signature{emptyDiffSig})

	if len(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d: %+v", len(hits), hits)
	}
}

func TestScanLog_StuckWorkerSingleLine(t *testing.T) {
	line := "2026/07/17 14:03:01 STUCK WORKER DETECTED on worker-7"
	input := line + "\n"
	hits := ScanLog(strings.NewReader(input), time.Time{}, []Signature{stuckWorkerSig})

	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Key != "stuck-worker" {
		t.Errorf("expected Key stuck-worker, got %q", hits[0].Key)
	}
	if hits[0].Count != 1 {
		t.Errorf("expected Count 1, got %d", hits[0].Count)
	}
	if hits[0].First != line {
		t.Errorf("expected First %q, got %q", line, hits[0].First)
	}
}

func TestScanLog_PanicMultiLineCountedOnce(t *testing.T) {
	input := `2026/07/17 14:03:01 panic: runtime error: index out of range
first stack frame
second stack frame
	/goroutine 1 [running]:
2026/07/17 14:03:02 normal recovery line
`
	hits := ScanLog(strings.NewReader(input), time.Time{}, []Signature{panicSig})

	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Key != "panic" {
		t.Errorf("expected Key panic, got %q", hits[0].Key)
	}
	if hits[0].Count != 1 {
		t.Errorf("expected Count 1, got %d", hits[0].Count)
	}
	wantFirst := "2026/07/17 14:03:01 panic: runtime error: index out of range"
	if hits[0].First != wantFirst {
		t.Errorf("expected First %q, got %q", wantFirst, hits[0].First)
	}
}

func TestScanLog_ZeroSinceCountsAllLines(t *testing.T) {
	input := `2026/07/17 08:00:00 [VerifiedClose] Bead b1 rejected (empty-diff)
2026/07/17 09:00:00 [VerifiedClose] Bead b2 rejected (empty-diff)
2026/07/17 10:00:00 [VerifiedClose] Bead b3 rejected (empty-diff)
2026/07/17 11:00:00 [VerifiedClose] Bead b4 rejected (empty-diff)
2026/07/17 12:00:00 [VerifiedClose] Bead b5 rejected (empty-diff)
`
	hits := ScanLog(strings.NewReader(input), time.Time{}, []Signature{emptyDiffSig})

	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Count != 5 {
		t.Errorf("expected Count 5, got %d", hits[0].Count)
	}
}
