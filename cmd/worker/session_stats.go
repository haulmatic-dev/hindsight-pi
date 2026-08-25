package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// maxSessionReadBytes caps per-file session telemetry reads so a huge
// session file does not blow up worker close-time memory.
const maxSessionReadBytes = 50 * 1024 * 1024

// toolMixStats aggregates toolCall telemetry from a bead's Pi session jsonl.
type toolMixStats struct {
	Counts       map[string]int
	Payload      map[string]int64 // bytes per tool name
	TotalCalls   int
	BashJestRuns int
	GateCmdRuns  map[string]int // gate-command invocations keyed by matched prefix
}

// gatherSessionToolMix reads the *.jsonl files in sessionDir and aggregates
// toolCall usage. It returns ok=false (and logs nothing) when the session
// directory is missing or contains no jsonl files. Any read error logs a debug
// line and aborts the aggregation so bad data never gates the worker result.
// Individual malformed jsonl lines are skipped.
func gatherSessionToolMix(sessionDir string) (toolMixStats, bool) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		// Missing session dir is normal (e.g. non-rpc mode or short-lived
		// sessions). Stay silent.
		return toolMixStats{}, false
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(sessionDir, e.Name()))
		}
	}
	if len(files) == 0 {
		return toolMixStats{}, false
	}

	stats := toolMixStats{
		Counts:      make(map[string]int),
		Payload:     make(map[string]int64),
		GateCmdRuns: make(map[string]int),
	}

	gatePrefixes := loadGateCommandPrefixes()

	for _, path := range files {
		fileStats, err := readSessionFileToolMix(path, gatePrefixes)
		if err != nil {
			log.Printf("[SessionStats] debug: skipping stats for %s: %v", sessionDir, err)
			return toolMixStats{}, false
		}
		mergeToolMixStats(&stats, fileStats)
	}

	return stats, true
}

// readSessionFileToolMix reads at most the last maxSessionReadBytes of a
// single jsonl session file and returns the per-tool aggregation. Individual
// malformed jsonl lines are debug-logged and skipped; a scanner-level error
// aborts and returns the error so the caller can fail open.
func readSessionFileToolMix(path string, gatePrefixes []string) (toolMixStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return toolMixStats{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return toolMixStats{}, err
	}

	offset := int64(0)
	discardFirst := false
	if info.Size() > maxSessionReadBytes {
		offset = info.Size() - maxSessionReadBytes
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return toolMixStats{}, err
		}
		discardFirst = true
	}

	stats := toolMixStats{
		Counts:      make(map[string]int),
		Payload:     make(map[string]int64),
		GateCmdRuns: make(map[string]int),
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxSessionReadBytes)
	if discardFirst {
		// Skip the likely-partial first line so every scanned line is whole.
		scanner.Scan()
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if err := accumulateToolCalls(&stats, line, gatePrefixes); err != nil {
			log.Printf("[SessionStats] debug: skipping malformed line in %s: %v", filepath.Base(path), err)
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return toolMixStats{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}

	return stats, nil
}

// accumulateToolCalls parses one jsonl line, finds every nested toolCall,
// increments counts and payload totals, detects jest/test-agent bash
// invocations, and counts bash invocations that match the daemon-declared
// gate-command prefixes. It returns an error for malformed JSON so the caller
// can skip the line and continue.
func accumulateToolCalls(stats *toolMixStats, line []byte, gatePrefixes []string) error {
	if len(line) == 0 {
		return nil
	}
	var root any
	if err := json.Unmarshal(line, &root); err != nil {
		return err
	}
	lineLen := int64(len(line))
	walkJSON(root, func(m map[string]any) {
		if t, _ := m["type"].(string); t != "toolCall" {
			return
		}
		name, ok := m["name"].(string)
		if !ok || name == "" {
			return
		}
		stats.Counts[name]++
		stats.Payload[name] += lineLen
		stats.TotalCalls++
		if name == "bash" {
			if cmd := bashCommand(m); cmd != "" {
				if isJestRun(cmd) {
					stats.BashJestRuns++
				}
				normCmd := normalizeWhitespace(cmd)
				for _, p := range gatePrefixes {
					if strings.Contains(normCmd, p) {
						stats.GateCmdRuns[p]++
					}
				}
			}
		}
	})
	return nil
}

// walkJSON recursively visits every JSON object, calling fn for each map.
func walkJSON(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, child := range x {
			walkJSON(child, fn)
		}
	case []any:
		for _, child := range x {
			walkJSON(child, fn)
		}
	}
}

// bashCommand extracts the bash command string from a toolCall's arguments.
func bashCommand(m map[string]any) string {
	args, ok := m["arguments"].(map[string]any)
	if !ok {
		return ""
	}
	cmd, _ := args["command"].(string)
	return cmd
}

// isJestRun reports whether a bash command looks like a jest run or a
// test-agent invocation.
func isJestRun(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "jest") || strings.Contains(lower, "test:agent")
}

// loadGateCommandPrefixes parses HYDRA_GATE_CMDS (JSON array of declared gate
// commands) and returns the matching prefixes. The prefix is the command up to
// its first placeholder or flag token; e.g. "npm run test:agent -- <scope>"
// becomes "npm run test:agent". Fail-open: missing or malformed env var logs a
// debug line and returns nil.
func loadGateCommandPrefixes() []string {
	v := os.Getenv("HYDRA_GATE_CMDS")
	if v == "" {
		return nil
	}
	var cmds []string
	if err := json.Unmarshal([]byte(v), &cmds); err != nil {
		log.Printf("[SessionStats] debug: failed to parse HYDRA_GATE_CMDS: %v", err)
		return nil
	}
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		if p := gateCmdPrefix(c); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// gateCmdPrefix returns the command string up to (but not including) the
// first placeholder or flag token, with whitespace normalized. An empty or
// whitespace-only command returns "".
func gateCmdPrefix(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	n := len(fields)
	for i, tok := range fields {
		if strings.HasPrefix(tok, "<") || strings.HasPrefix(tok, "-") {
			n = i
			break
		}
	}
	return strings.Join(fields[:n], " ")
}

// normalizeWhitespace collapses runs of whitespace into a single space and
// trims leading/trailing whitespace.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// mergeToolMixStats adds fileStats into total.
func mergeToolMixStats(total *toolMixStats, fileStats toolMixStats) {
	for name, n := range fileStats.Counts {
		total.Counts[name] += n
	}
	for name, b := range fileStats.Payload {
		total.Payload[name] += b
	}
	total.TotalCalls += fileStats.TotalCalls
	total.BashJestRuns += fileStats.BashJestRuns
	for p, n := range fileStats.GateCmdRuns {
		total.GateCmdRuns[p] += n
	}
}

// formatSessionToolMix returns the canonical [SessionStats] log line.
func formatSessionToolMix(beadID string, stats toolMixStats) string {
	tools := formatSortedIntMap(filterCounts(stats.Counts))
	payload := formatSortedKBMap(filterPayload(stats.Payload))
	gateRuns := ""
	if len(stats.GateCmdRuns) > 0 {
		b, _ := json.Marshal(stats.GateCmdRuns)
		gateRuns = fmt.Sprintf(" gate_runs=%s", string(b))
	}
	return fmt.Sprintf(
		"[SessionStats] %s turns=%d tools=%s payloadKB=%s graphify=%d hindsight_search=%d jest_runs=%d%s",
		beadID,
		stats.TotalCalls,
		tools,
		payload,
		intValue(stats.Counts, "graphify"),
		intValue(stats.Counts, "hindsight_search"),
		stats.BashJestRuns,
		gateRuns,
	)
}

func filterCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		if k == "graphify" || k == "hindsight_search" {
			continue
		}
		out[k] = v
	}
	return out
}

func filterPayload(m map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		if k == "graphify" || k == "hindsight_search" {
			continue
		}
		out[k] = v
	}
	return out
}

func formatSortedIntMap(m map[string]int) string {
	keys := sortedKeys(m)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, m[k]))
	}
	return strings.Join(parts, ",")
}

func formatSortedKBMap(m map[string]int64) string {
	keys := sortedKeysInt64(m)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, m[k]/1024))
	}
	return strings.Join(parts, ",")
}

func sortedKeysInt64(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func intValue(m map[string]int, key string) int {
	if v, ok := m[key]; ok {
		return v
	}
	return 0
}

// emitSessionToolMixStats logs per-bead tool-mix telemetry from the session
// directory. It is fail-open: any problem is debug-logged and ignored.
func emitSessionToolMixStats(bead types.Bead, hydraHome string) {
	if hydraHome == "" {
		return
	}
	_, sessionDir, err := buildPiSession(bead, hydraHome)
	if err != nil || sessionDir == "" {
		return
	}
	stats, ok := gatherSessionToolMix(sessionDir)
	if !ok {
		return
	}
	log.Printf("%s", formatSessionToolMix(string(bead.ID), stats))
}
