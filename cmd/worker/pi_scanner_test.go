package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

// RED: pi's RPC stream emits single-line events that can exceed bufio's
// 64KB default token limit (a tool result carrying a full file read or
// jest output — observed 71,055 bytes in the wild, srv-smart-engine-z4t).
// A default bufio.Scanner stops scanning at that line, the goroutine exits
// silently, RecordActivity is never called again, and the activity watchdog
// kills the still-productive pi process 10m later. newPiScanner must deliver
// such lines.
func TestNewPiScannerDeliversOversizedEventLines(t *testing.T) {
	big := `{"type":"tool_result","content":"` + strings.Repeat("x", 100*1024) + `"}`
	input := "{\"type\":\"turn_start\"}\n" + big + "\n{\"type\":\"turn_end\"}\n"

	var got []string
	sc := newPiScanner(bytes.NewReader([]byte(input)))
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 lines delivered, got %d (oversized event line killed the scanner)", len(got))
	}
	if len(got) > 1 && len(got[1]) != len(big) {
		t.Fatalf("oversized line truncated: got %d bytes, want %d", len(got[1]), len(big))
	}
}

// The old default scanner proves the bug: it must FAIL on the same input.
func TestDefaultScannerDiesOnOversizedEventLines(t *testing.T) {
	big := strings.Repeat("x", 100*1024)
	sc := bufio.NewScanner(strings.NewReader(big + "\n"))
	if sc.Scan() {
		t.Fatal("default scanner unexpectedly handled a 100KB line — bug premise wrong")
	}
	if sc.Err() == nil {
		t.Fatal("default scanner stopped without error — expected bufio.ErrTooLong")
	}
}
