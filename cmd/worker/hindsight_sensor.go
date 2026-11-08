package main

import (
	"errors"
	"log"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/degrade"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Sentinel bootstrap-failure causes (exact strings are test keys).
var (
	errHindsightNoAgentDir = errors.New("hindsight extension: pi agent dir unresolved")
	errHindsightUnresolved = errors.New("hindsight extension: entry point did not resolve to a file")
)

// Hindsight extension-connectivity sensor.
//
// The worker prompt tells the agent to run `hindsight_search` once at bead
// start and to "skip silently if the tool is unavailable" — by design. That
// makes the hindsight extension's death invisible: if the extension fails to
// bootstrap (missing package, renamed org, 410-Gone class bugs), the agent
// reads its own prompt, finds no hindsight tools, and the whole bead runs
// with zero memory recall and nothing in any log saying so.
//
// This sensor closes the gap, fail-open: after execution it checks that the
// hindsight extension (a) is actually installed and resolvable when it was
// allowlisted, and (b) that the session actually called a hindsight tool.
// Findings land in the degrade registry under "hindsight-extension" plus a
// LOUD log line — never in the bead verdict, never a retry.

// hindsightSensorSubsystem is the degrade registry key this sensor records
// under. A subsystem at >=3 attempts with 0 successes renders degraded in the
// digest (see internal/degrade).
const hindsightSensorSubsystem = "hindsight-extension"

// hindsightReasonNone / ... are the machine-stable failure reasons.
const (
	hindsightReasonNone           = "" // extension not expected, nothing to sense
	hindsightReasonBootstrapDied  = "bootstrap-failed"
	hindsightReasonNoHindsightUse = "no-hindsight-tool-calls"
)

// hindsightVerdict is the sensor's decision for one bead run.
type hindsightVerdict struct {
	// Fired is true when the sensor observed the fail-open class.
	Fired bool
	// Reason is a machine-stable slug ("" when not fired or not observable).
	Reason string
}

// hindsightSensorVerdict is the pure decision core. expected: "hindsight" is
// in the phase allowlist; resolved: the extension entry point resolved to a
// file; statsOK: session telemetry was readable; calls: hindsight_* toolCall
// count from the session jsonl.
func hindsightSensorVerdict(expected, resolved, statsOK bool, calls int) hindsightVerdict {
	if !expected {
		return hindsightVerdict{}
	}
	if !resolved {
		return hindsightVerdict{Fired: true, Reason: hindsightReasonBootstrapDied}
	}
	// Without session telemetry we cannot observe tool use — fail open
	// silently (non-rpc mode and pre-2026 sessions have no jsonl files).
	if !statsOK {
		return hindsightVerdict{}
	}
	if calls == 0 {
		return hindsightVerdict{Fired: true, Reason: hindsightReasonNoHindsightUse}
	}
	return hindsightVerdict{}
}

// hindsightToolCalls counts session toolCall entries whose name starts with
// "hindsight_" (hindsight_search, hindsight_retain, …).
func hindsightToolCalls(stats toolMixStats) int {
	n := 0
	for name, c := range stats.Counts {
		if strings.HasPrefix(name, "hindsight_") {
			n += c
		}
	}
	return n
}

// runHindsightSensor runs the connectivity sensor once per bead, after
// execution. Fail-open by contract: any problem is logged and ignored; the
// sensor never gates the worker result and never returns an error.
func runHindsightSensor(piConfig PiConfig, hydraHome string, bead types.Bead) {
	// Not allowlisted for this phase → nothing is expected to be loaded; the
	// absence of hindsight tools is correct, not a failure.
	if !hasExtension(piConfig.Extensions, "hindsight") {
		return
	}

	// Resolve the extension entry exactly as buildPiCommand will. A bootstrap
	// failure here (missing package, renamed org, broken manifest) means the
	// agent will run with no hindsight tools and skip memory recall silently.
	resolved := false
	if entry, err := resolveHindsightEntry(piConfig); err != nil {
		log.Printf("[Worker:rpc] skipping extension \"hindsight\": %v", err)
	} else if entry != "" {
		resolved = true
	}

	calls := 0
	statsOK := false
	if sessionDir := hindsightSessionDir(hydraHome, bead); sessionDir != "" {
		if stats, ok := gatherSessionToolMix(sessionDir); ok {
			calls = hindsightToolCalls(stats)
			statsOK = true
		}
	}

	verdict := hindsightSensorVerdict(true, resolved, statsOK, calls)
	switch {
	case verdict.Fired && verdict.Reason == hindsightReasonBootstrapDied:
		degrade.RecordAttempt(hindsightSensorSubsystem)
		log.Printf("[HindsightSensor] FAIL-OPEN: hindsight extension allowlisted but bootstrap failed — "+
			"this bead ran with no memory recall and nothing else will say so "+
			"(subsystem %q recorded; check the hindsight extension install/entry point)",
			hindsightSensorSubsystem)
	case verdict.Fired && verdict.Reason == hindsightReasonNoHindsightUse:
		degrade.RecordAttempt(hindsightSensorSubsystem)
		log.Printf("[HindsightSensor] FAIL-OPEN: hindsight extension allowlisted and resolved but the session made "+
			"zero hindsight_* tool calls — memory recall was skipped silently "+
			"(subsystem %q recorded; check the hindsight server and extension bootstrap)",
			hindsightSensorSubsystem)
	case resolved && statsOK:
		degrade.RecordAttempt(hindsightSensorSubsystem)
		degrade.RecordSuccess(hindsightSensorSubsystem)
		log.Printf("[HindsightSensor] hindsight connectivity OK (%d hindsight_* tool call(s) in session)", calls)
	}
}

// resolveHindsightEntry resolves the hindsight extension entry point from the
// configured pi agent dir, mirroring buildPiCommand's extension loading.
// Empty agent dir counts as a bootstrap failure: the extension cannot load
// from nowhere.
func resolveHindsightEntry(piConfig PiConfig) (string, error) {
	agentDir := resolvePiAgentDir(piConfig.ConfigDir)
	if agentDir == "" {
		return "", errHindsightNoAgentDir
	}
	paths := resolveExtensionPaths(agentDir, []string{"hindsight"})
	if len(paths) == 0 {
		return "", errHindsightUnresolved
	}
	return paths[0], nil
}

// hindsightSessionDir rebuilds the session directory the run used, using the
// same derivation as the run itself.
func hindsightSessionDir(hydraHome string, bead types.Bead) string {
	if hydraHome == "" {
		return ""
	}
	_, sessionDir, err := buildPiSession(bead, hydraHome)
	if err != nil {
		return ""
	}
	return sessionDir
}
