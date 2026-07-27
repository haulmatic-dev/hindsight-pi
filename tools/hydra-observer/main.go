package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point for the hydra-observer CLI.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hydra-observer", flag.ContinueOnError)
	fs.SetOutput(stderr)

	logPath := fs.String("log", "", "path to daemon log")
	sinceRaw := fs.String("since", "", "RFC3339 or daemon-log timestamp")
	metricsPath := fs.String("metrics", "", "path to hydra-eval metrics JSON")
	baselinePath := fs.String("baseline", "", "path to baseline metrics JSON")
	backlogDir := fs.String("backlog-dir", "", "path to backlog directory")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if *logPath == "" {
		fmt.Fprintln(stderr, "required flag --log not provided")
		return 2
	}
	if *backlogDir == "" {
		fmt.Fprintln(stderr, "required flag --backlog-dir not provided")
		return 2
	}

	var since time.Time
	if *sinceRaw != "" {
		var err error
		since, err = parseSince(*sinceRaw)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}

	logFile, err := os.Open(*logPath)
	if err != nil {
		fmt.Fprintf(stderr, "failed to open log: %v\n", err)
		return 2
	}
	defer logFile.Close()

	sigs := DefaultSignatures()
	hits := ScanLog(logFile, since, sigs)

	var regressions []Regression
	if *metricsPath != "" {
		current, err := LoadMetrics(*metricsPath)
		if err != nil {
			fmt.Fprintf(stderr, "failed to load metrics: %v\n", err)
			return 2
		}

		base := *baselinePath
		if base == "" {
			base = filepath.Join(*backlogDir, "..", "observer-baseline.json")
			base, _ = filepath.Abs(base)
		}

		var baseline Metrics
		haveBaseline := false
		if _, err := os.Stat(base); err == nil {
			baseline, err = LoadMetrics(base)
			if err != nil {
				fmt.Fprintf(stderr, "failed to load baseline: %v\n", err)
				return 2
			}
			haveBaseline = true
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "failed to check baseline: %v\n", err)
			return 2
		}

		if haveBaseline {
			regressions = Compare(baseline, current)
		}

		// Update the baseline file when we own the default path, or when an
		// explicit baseline path did not exist yet. An existing explicit
		// baseline is treated as a read-only reference for this run.
		if *baselinePath == "" || !haveBaseline {
			if err := storeMetrics(base, current); err != nil {
				fmt.Fprintf(stderr, "failed to write baseline: %v\n", err)
				return 2
			}
		}
	}

	severityByKey := make(map[string]string, len(sigs))
	for _, s := range sigs {
		severityByKey[s.Key] = s.Severity
	}

	entries := make([]Entry, 0, len(hits)+len(regressions))
	for _, h := range hits {
		entries = append(entries, Entry{
			Kind:     "signature",
			Key:      h.Key,
			Severity: severityByKey[h.Key],
			Evidence: h.First,
		})
	}
	for _, r := range regressions {
		evidence := fmt.Sprintf("%s: baseline=%v current=%v", r.Field, r.Baseline, r.Current)
		entries = append(entries, Entry{
			Kind:     "regression",
			Key:      r.Field,
			Severity: r.Severity,
			Evidence: evidence,
		})
	}

	written, err := WriteBacklog(*backlogDir, entries)
	if err != nil {
		fmt.Fprintf(stderr, "failed to write backlog: %v\n", err)
		return 2
	}

	for _, e := range entries {
		fmt.Fprintf(stdout, "new backlog entry: %s (%s)\n", e.Key, e.Severity)
	}

	summary := map[string]int{
		"new_entries": written,
		"hits":        len(hits),
		"regressions": len(regressions),
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(summary); err != nil {
		fmt.Fprintf(stderr, "failed to encode summary: %v\n", err)
		return 2
	}

	if written > 0 {
		return 1
	}
	return 0
}

// parseSince accepts an RFC3339 timestamp or the daemon-log timestamp layouts.
func parseSince(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006/01/02 15:04:05",
		"2006/01/02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid --since value %q: accepted layouts are RFC3339, 2006/01/02 15:04:05, 2006/01/02", s)
}

// storeMetrics writes m as JSON to path.
func storeMetrics(path string, m Metrics) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
