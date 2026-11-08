// Package main — tools/turn-report: turn distribution extractor + budget
// calibration report.
//
// Reads `worker_turns` events from the daemon's central event stream (one
// `<HYDRA_HOME>/events/<YYYY-MM-DD>.jsonl` file per day), groups turn counts
// by phase (RED/GREEN/REFACTOR/SYNTAX/GATE/...), prints per-phase
// p25/p50/p75/p90/max percentiles, and emits a suggested `turn_budgets:` yaml
// block as a COMMENT (round-up-to-10 of each phase's p90) for operator review.
// The tool never writes any config file itself.
//
// This file is the RED-phase inert stub: run() is intentionally inert until
// the GREEN phase implements the contract pinned by main_test.go
// (TURN_REPORT_ENFORCE=1 proves the RED).
package main

import (
	"io"
)

// eventTypeWorkerTurns is the event type read by the extractor.
const eventTypeWorkerTurns = "worker_turns"

// turnReportImplemented gates the RED contract: false until GREEN.
var turnReportImplemented = false

// run parses args and prints the report to stdout, returning the process
// exit code. Inert in RED: it does nothing and never refuses.
func run(args []string, stdout io.Writer) int {
	_ = args
	_ = stdout
	return 0
}

func main() { //nolint:revive // entry point; exercised by GREEN.
	_ = run(nil, nil)
}
