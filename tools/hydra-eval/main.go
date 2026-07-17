package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	beadsPath := flag.String("beads-jsonl", "", "Path to beads JSONL file")
	daemonLogPath := flag.String("daemon-log", "", "Path to daemon log file")
	outPath := flag.String("out", "hydra-eval.json", "Output JSON file path")
	flag.Parse()

	if *beadsPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: hydra-eval --beads-jsonl <file> [--daemon-log <file>] [--out <file>]")
		os.Exit(1)
	}

	beadsFile, err := os.Open(*beadsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open beads JSONL: %v\n", err)
		os.Exit(1)
	}
	beads := ParseBeadsJSONL(beadsFile)
	beadsFile.Close()

	var events []CloseEvent
	stuckKills := 0
	scopeWarnings := 0
	failedCloses := 0

	if *daemonLogPath != "" {
		logData, err := os.ReadFile(*daemonLogPath)
		if err == nil {
			markers := []string{
				"STUCK WORKER DETECTED",
				"[ScopeGate]",
				"closed as failed",
			}
			var counts []int
			events, counts = ParseDaemonLog(bytes.NewReader(logData), markers)
			stuckKills = counts[0]
			scopeWarnings = counts[1]
			failedCloses = counts[2]
		}
	}

	metrics := ComputeMetrics(beads, events, stuckKills, scopeWarnings, failedCloses)

	outJSON, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal metrics: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outPath, outJSON, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(outJSON))
}
