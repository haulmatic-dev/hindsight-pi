package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCloseEvents_ValidLineWithPrefix(t *testing.T) {
	input := `2026/07/14 18:52:25 [CloseEvent] {"bead_id":"hydra-dogfood-x1","verdict":"accepted","reason":"verified","attempt":0}`
	events := ParseCloseEvents(strings.NewReader(input))

	require.Len(t, events, 1)
	assert.Equal(t, "hydra-dogfood-x1", events[0].BeadID)
	assert.Equal(t, "accepted", events[0].Verdict)
	assert.Equal(t, "verified", events[0].Reason)
	assert.Equal(t, 0, events[0].Attempt)
}

func TestParseCloseEvents_SkipsInvalidJSON(t *testing.T) {
	input := `[CloseEvent] not-json`
	events := ParseCloseEvents(strings.NewReader(input))

	assert.Empty(t, events)
}

func TestParseCloseEvents_ToleratesWorkerPrefix(t *testing.T) {
	input := `[Worker:0] [stderr] [CloseEvent] {"bead_id":"b1","verdict":"accepted","reason":"verified","attempt":0}`
	events := ParseCloseEvents(strings.NewReader(input))

	require.Len(t, events, 1)
	assert.Equal(t, "b1", events[0].BeadID)
}

func TestParseCloseEvents_NoMarkerReturnsEmpty(t *testing.T) {
	input := `some unrelated log line`
	events := ParseCloseEvents(strings.NewReader(input))

	assert.Empty(t, events)
}

func TestParseBeadsJSONL_ValidLines(t *testing.T) {
	input := `{"id":"a","status":"closed","issue_type":"task"}
{"id":"b","status":"open","issue_type":"epic"}`
	records := ParseBeadsJSONL(strings.NewReader(input))

	require.Len(t, records, 2)
	assert.Equal(t, "a", records[0].ID)
	assert.Equal(t, "closed", records[0].Status)
	assert.Equal(t, "task", records[0].IssueType)
	assert.Equal(t, "b", records[1].ID)
	assert.Equal(t, "open", records[1].Status)
	assert.Equal(t, "epic", records[1].IssueType)
}

func TestParseBeadsJSONL_SkipsBlankAndInvalidLines(t *testing.T) {
	input := `

{"id":"a","status":"closed","issue_type":"task"}

not-valid-json

{"id":"b","status":"open","issue_type":"epic"}
`
	records := ParseBeadsJSONL(strings.NewReader(input))

	require.Len(t, records, 2)
	assert.Equal(t, "a", records[0].ID)
	assert.Equal(t, "b", records[1].ID)
}

func TestCountMarker_MultipleOccurrences(t *testing.T) {
	input := `STUCK WORKER DETECTED on worker 1
normal log line
STUCK WORKER DETECTED on worker 2
another normal line
STUCK WORKER DETECTED on worker 3`
	count := CountMarker(strings.NewReader(input), "STUCK WORKER DETECTED")

	assert.Equal(t, 3, count)
}

func TestCountMarker_NoOccurrences(t *testing.T) {
	input := `normal log line
another normal line`
	count := CountMarker(strings.NewReader(input), "STUCK WORKER DETECTED")

	assert.Equal(t, 0, count)
}

// TestArtifactParsers_BuildAndVet covers the build and vet acceptance
// criteria for the hydra-eval tool.
func TestArtifactParsers_BuildAndVet(t *testing.T) {
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = "../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed: %v\n%s", err, string(output))
	}

	cmd = exec.Command("go", "vet", "./tools/hydra-eval/...")
	cmd.Dir = "../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet ./tools/hydra-eval/... failed: %v\n%s", err, string(output))
	}
}
