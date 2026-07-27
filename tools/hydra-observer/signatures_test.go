package main

import (
	"strings"
	"testing"
	"time"
)

var emptyDiffSig = Signature{Key: "empty-diff", Marker: "(empty-diff)", Severity: "critical", Threshold: 3}
var stuckWorkerSig = Signature{Key: "stuck-worker", Marker: "STUCK WORKER DETECTED", Severity: "warning", Threshold: 1}
var panicSig = Signature{Key: "panic", Marker: "panic:", Severity: "critical", Threshold: 1}
var orphanRecoverySig = Signature{Key: "orphan-recovery", Marker: "reopened orphan bead", Severity: "warning", Threshold: 3}
var quotaExhaustedSig = Signature{Key: "quota-exhausted", Marker: "usage limit", Severity: "critical", Threshold: 1}

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

func TestScanLog_OrphanRecoveryAboveThreshold(t *testing.T) {
	input := `2026/07/27 09:00:00 [Charon] ✅ reopened orphan bead b1 for retry
2026/07/27 09:01:00 [Charon] ✅ reopened orphan bead b2 for retry
2026/07/27 09:02:00 [Charon] ✅ reopened orphan bead b3 for retry
`
	hits := ScanLog(strings.NewReader(input), time.Time{}, []Signature{orphanRecoverySig})

	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Key != "orphan-recovery" {
		t.Errorf("expected Key orphan-recovery, got %q", hits[0].Key)
	}
	if hits[0].Count != 3 {
		t.Errorf("expected Count 3, got %d", hits[0].Count)
	}
}

func TestScanLog_OrphanRecoveryBelowThreshold(t *testing.T) {
	input := `2026/07/27 09:00:00 [Charon] ✅ reopened orphan bead b1 for retry
2026/07/27 09:01:00 [Charon] ✅ reopened orphan bead b2 for retry
`
	hits := ScanLog(strings.NewReader(input), time.Time{}, []Signature{orphanRecoverySig})

	if len(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d: %+v", len(hits), hits)
	}
}

func TestScanLog_QuotaExhaustedSingleHit(t *testing.T) {
	line := `2026/07/27 09:50:16 403 {"error":{"type":"permission_error","message":"You've reached your usage limit for this billing cycle."}}`
	input := line + "\n"
	hits := ScanLog(strings.NewReader(input), time.Time{}, []Signature{quotaExhaustedSig})

	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].Key != "quota-exhausted" {
		t.Errorf("expected Key quota-exhausted, got %q", hits[0].Key)
	}
	if hits[0].Count != 1 {
		t.Errorf("expected Count 1, got %d", hits[0].Count)
	}
	if hits[0].First != line {
		t.Errorf("expected First %q, got %q", line, hits[0].First)
	}
}

func TestDefaultSignatures_ContainsAllBuiltins(t *testing.T) {
	want := map[string]bool{
		"empty-diff":      true,
		"stuck-worker":    true,
		"panic":           true,
		"blocked-user":    true,
		"escalation":      true,
		"worker-timeout":  true,
		"max-cycles":      true,
		"orphan-recovery": true,
		"quota-exhausted": true,
	}
	for _, sig := range DefaultSignatures() {
		if !want[sig.Key] {
			t.Errorf("unexpected signature key %q", sig.Key)
		}
		delete(want, sig.Key)
	}
	for key := range want {
		t.Errorf("missing signature key %q", key)
	}
}
