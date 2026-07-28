package main

import (
	"strings"
	"testing"
	"time"
)

// TestParseDaemonLogSince_IgnoresWorkerPassthrough pins the fix for a metric that
// reported a number with no meaning.
//
// The daemon streams worker stderr into its own log, and that payload is the
// model's raw JSONL — including its thinking text. A substring marker therefore
// matches whatever the model happened to write. On run H the "closed as failed"
// marker matched 7,884 lines: 7,739 were worker passthrough and only 145 were
// genuine [closeBead] lines, yet failed_closes was reported as 6,707.
func TestParseDaemonLogSince_IgnoresWorkerPassthrough(t *testing.T) {
	log := strings.Join([]string{
		`2026/07/28 08:00:00 [closeBead] Bead hydra-dogfood-aaa closed as failed with close_blocked_on_failed=true`,
		`2026/07/28 08:00:01 [Worker:hydra-dogfood-bbb] [stderr] {"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"the bead was closed as failed so I should"}}`,
		`2026/07/28 08:00:02 [Worker:hydra-dogfood-bbb] [stderr] {"type":"thinking_delta","delta":"closed as failed again"}`,
		`2026/07/28 08:00:03 [closeBead] Bead hydra-dogfood-ccc closed as failed with close_blocked_on_failed=true`,
		`2026/07/28 08:00:04 [Worker:hydra-dogfood-ddd] [stdout] closed as failed`,
	}, "\n")

	_, counts := ParseDaemonLogSince(strings.NewReader(log), []string{"closed as failed"}, time.Time{})

	if len(counts) != 1 {
		t.Fatalf("counts length = %d, want 1", len(counts))
	}
	// Two genuine [closeBead] lines; the three passthrough lines must be ignored.
	if counts[0] != 2 {
		t.Errorf("failed_closes = %d, want 2 (only daemon-emitted lines — was the model's own text counted?)", counts[0])
	}
}

// TestIsWorkerPassthrough covers both stream prefixes the daemon uses, and
// confirms genuine daemon markers are still counted.
func TestIsWorkerPassthrough(t *testing.T) {
	yes := []string{
		`2026/07/28 08:00:01 [Worker:x] [stderr] anything`,
		`2026/07/28 08:00:01 [Worker:x] [stdout] anything`,
	}
	for _, l := range yes {
		if !isWorkerPassthrough([]byte(l)) {
			t.Errorf("isWorkerPassthrough(%q) = false, want true", l)
		}
	}
	no := []string{
		`2026/07/28 08:00:00 [closeBead] Bead x closed as failed`,
		`2026/07/28 08:00:00 [ScopeGate] warning`,
		`2026/07/28 08:00:00 STUCK WORKER DETECTED`,
	}
	for _, l := range no {
		if isWorkerPassthrough([]byte(l)) {
			t.Errorf("isWorkerPassthrough(%q) = true, want false — daemon lines must be counted", l)
		}
	}
}
