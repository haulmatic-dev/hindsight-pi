package main

import (
	"strings"
	"testing"
)

func BenchmarkParseCloseEvents(b *testing.B) {
	input := strings.Repeat(
		"2026/07/14 18:52:25 [CloseEvent] {\"bead_id\":\"b1\",\"verdict\":\"accepted\",\"reason\":\"verified\",\"attempt\":0}\n",
		1000,
	)
	r := strings.NewReader(input)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ParseCloseEvents(r)
		r.Reset(input)
	}
}

func BenchmarkParseBeadsJSONL(b *testing.B) {
	input := strings.Repeat(
		"{\"id\":\"b1\",\"status\":\"closed\",\"issue_type\":\"task\"}\n",
		1000,
	)
	r := strings.NewReader(input)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ParseBeadsJSONL(r)
		r.Reset(input)
	}
}

func BenchmarkCountMarker(b *testing.B) {
	input := strings.Repeat(
		"STUCK WORKER DETECTED on worker 1\nnormal log line\n",
		500,
	)
	r := strings.NewReader(input)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CountMarker(r, "STUCK WORKER DETECTED")
		r.Reset(input)
	}
}

func BenchmarkParseDaemonLog(b *testing.B) {
	input := strings.Repeat(
		"2026/07/14 18:52:25 [CloseEvent] {\"bead_id\":\"b1\",\"verdict\":\"accepted\",\"reason\":\"verified\",\"attempt\":0}\n"+
			"STUCK WORKER DETECTED on worker 1\n[ScopeGate] warning\nclosed as failed\n",
		250,
	)
	markers := []string{
		"STUCK WORKER DETECTED",
		"[ScopeGate]",
		"closed as failed",
	}
	r := strings.NewReader(input)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseDaemonLog(r, markers)
		r.Reset(input)
	}
}
