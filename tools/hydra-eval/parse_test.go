package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"

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

func TestParseLogTime_ValidTimestamp(t *testing.T) {
	input := []byte("2026/07/17 10:00:00 [CloseEvent] {} extra")
	tm, ok := ParseLogTime(input)

	require.True(t, ok)
	assert.Equal(t, 2026, tm.Year())
	assert.Equal(t, time.July, tm.Month())
	assert.Equal(t, 17, tm.Day())
	assert.Equal(t, 10, tm.Hour())
	assert.Equal(t, 0, tm.Minute())
	assert.Equal(t, 0, tm.Second())
}

func TestParseLogTime_ExactLength(t *testing.T) {
	input := []byte("2026/07/17 10:00:00")
	tm, ok := ParseLogTime(input)

	require.True(t, ok)
	assert.Equal(t, 17, tm.Day())
}

func TestParseLogTime_InvalidSeparator(t *testing.T) {
	input := []byte("2026/07/17 10:00:00X not a log line")
	_, ok := ParseLogTime(input)

	assert.False(t, ok)
}

func TestParseLogTime_TooShort(t *testing.T) {
	input := []byte("2026/07/17 10:00:0")
	_, ok := ParseLogTime(input)

	assert.False(t, ok)
}

func TestParseLogTime_NonDatePrefix(t *testing.T) {
	input := []byte("not a timestamp [CloseEvent] {}")
	_, ok := ParseLogTime(input)

	assert.False(t, ok)
}

func TestParseSince_FullLayout(t *testing.T) {
	tm, err := ParseSince("2026/07/17 10:00:00")

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC), tm)
}

func TestParseSince_DateOnlyLayout(t *testing.T) {
	tm, err := ParseSince("2026/07/17")

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.July, 17, 0, 0, 0, 0, time.UTC), tm)
}

func TestParseSince_InvalidMentionsLayouts(t *testing.T) {
	_, err := ParseSince("garbage")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "2006/01/02 15:04:05")
	assert.Contains(t, err.Error(), "2006/01/02")
}

func TestFilterSince_KeepsTimestampedAtOrAfterSince(t *testing.T) {
	since := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	lines := [][]byte{
		[]byte("2026/07/17 09:00:00 early"),
		[]byte("2026/07/17 10:00:00 exact"),
		[]byte("2026/07/17 11:00:00 late"),
	}

	filtered := FilterSince(lines, since)

	require.Len(t, filtered, 2)
	assert.Equal(t, "2026/07/17 10:00:00 exact", string(filtered[0]))
	assert.Equal(t, "2026/07/17 11:00:00 late", string(filtered[1]))
}

func TestFilterSince_KeepsContinuationLinesAfterFirstKeptTimestamp(t *testing.T) {
	since := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	lines := [][]byte{
		[]byte("2026/07/17 09:00:00 early"),
		[]byte("continuation of early"),
		[]byte("2026/07/17 10:00:00 exact"),
		[]byte("continuation of exact"),
		[]byte("another continuation"),
	}

	filtered := FilterSince(lines, since)

	require.Len(t, filtered, 3)
	assert.Equal(t, "2026/07/17 10:00:00 exact", string(filtered[0]))
	assert.Equal(t, "continuation of exact", string(filtered[1]))
	assert.Equal(t, "another continuation", string(filtered[2]))
}

func TestFilterSince_ZeroSinceReturnsUnchanged(t *testing.T) {
	lines := [][]byte{
		[]byte("2026/07/17 09:00:00 early"),
		[]byte("2026/07/17 11:00:00 late"),
	}

	filtered := FilterSince(lines, time.Time{})

	assert.Equal(t, lines, filtered)
}

func TestParseDaemonLogSince_FiltersEventsAndMarkers(t *testing.T) {
	since := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	input := `2026/07/17 09:00:00 [CloseEvent] {"bead_id":"b1","verdict":"accepted","reason":"verified","attempt":0}
2026/07/17 09:30:00 STUCK WORKER DETECTED on worker 1
2026/07/17 11:00:00 [CloseEvent] {"bead_id":"b2","verdict":"accepted","reason":"verified","attempt":0}
2026/07/17 11:30:00 STUCK WORKER DETECTED on worker 2
`
	markers := []string{"STUCK WORKER DETECTED"}

	events, counts := ParseDaemonLogSince(strings.NewReader(input), markers, since)

	require.Len(t, events, 1)
	assert.Equal(t, "b2", events[0].BeadID)
	assert.Equal(t, time.Date(2026, time.July, 17, 11, 0, 0, 0, time.UTC), events[0].Time)
	assert.Equal(t, 1, counts[0])
}

func TestParseDaemonLogSince_ZeroSinceDisablesFiltering(t *testing.T) {
	input := `2026/07/17 09:00:00 [CloseEvent] {"bead_id":"b1","verdict":"accepted","reason":"verified","attempt":0}
2026/07/17 11:00:00 [CloseEvent] {"bead_id":"b2","verdict":"accepted","reason":"verified","attempt":0}
`
	markers := []string{"STUCK WORKER DETECTED"}

	events, counts := ParseDaemonLogSince(strings.NewReader(input), markers, time.Time{})

	require.Len(t, events, 2)
	assert.Equal(t, 0, counts[0])
}
