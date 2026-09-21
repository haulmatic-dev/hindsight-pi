package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// fakeScoutRunner records calls and returns canned mappings/err.
type fakeScoutRunner struct {
	mappings []PackFileAnchor
	err      error
	calls    [][]string
	maxTurns []int
}

func (f *fakeScoutRunner) RunScout(_ context.Context, _ string, requirements []string, maxTurns int) ([]PackFileAnchor, error) {
	f.calls = append(f.calls, append([]string(nil), requirements...))
	f.maxTurns = append(f.maxTurns, maxTurns)
	return f.mappings, f.err
}

func scoutEnabledSpawner(t *testing.T, client BeadsClient, chainBeads []types.BeadID, projectPath string, runner ScoutRunner) (*WorkerSpawner, string) {
	t.Helper()
	s, _ := packTestSpawner(t, client, chainBeads, projectPath)
	s.config = &Config{ContextPack: ContextPackConfig{Scout: ScoutConfig{Enabled: true, MaxTurns: 7}}}
	s.scoutRunner = runner
	eventsDir := t.TempDir()
	s.eventSink = NewEventSink(eventsDir)
	return s, eventsDir
}

func readEvents(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil {
			sb.Write(raw)
		}
	}
	return sb.String()
}

// --- merge logic ---

func TestMergeScoutMappings_ReplacesRescoutedKeepsRest(t *testing.T) {
	pack := &ContextPack{Files: []PackFileAnchor{
		{Requirement: "flux", Path: "a.go", Confidence: 1, Provenance: PackProvenanceMechanical},
		{Requirement: "old flux", Path: "old.go", Confidence: 0.8, Provenance: PackProvenanceAgentic},
		{Requirement: "other", Path: "keep.go", Confidence: 0.9, Provenance: PackProvenanceAgentic},
	}}
	newMappings := []PackFileAnchor{
		{Requirement: "old flux", Path: "new.go", Confidence: 0.75, Symbol: "Flux", Snippet: "func Flux()"},
	}
	MergeScoutMappings(pack, newMappings, []string{"old flux"})

	if len(pack.Files) != 3 {
		t.Fatalf("files = %+v, want 3 anchors", pack.Files)
	}
	var foundOld, foundNew, foundKeep bool
	for _, a := range pack.Files {
		if a.Requirement == "flux" && a.Path == "a.go" && a.Provenance == PackProvenanceMechanical {
			foundOld = true
		}
		if a.Requirement == "old flux" {
			if a.Path != "new.go" {
				t.Errorf("re-scouted anchor not replaced: %+v", a)
			}
			if a.Provenance != PackProvenanceAgentic {
				t.Errorf("provenance = %q, want forced agentic", a.Provenance)
			}
			foundNew = true
		}
		if a.Requirement == "other" && a.Path == "keep.go" {
			foundKeep = true // unrelated agentic anchor must survive
		}
	}
	if !foundOld || !foundNew || !foundKeep {
		t.Errorf("missing anchors: mech=%v new=%v keep=%v in %+v", foundOld, foundNew, foundKeep, pack.Files)
	}
}

func TestMergeScoutMappings_DedupesByRequirementPath(t *testing.T) {
	pack := &ContextPack{Files: []PackFileAnchor{
		{Requirement: "r", Path: "x.go", Confidence: 0.9, Provenance: PackProvenanceAgentic},
	}}
	MergeScoutMappings(pack, []PackFileAnchor{
		{Requirement: "r", Path: "x.go", Confidence: 0.8},
		{Requirement: "r", Path: "y.go", Confidence: 0.7},
	}, nil)
	if len(pack.Files) != 2 {
		t.Errorf("files = %+v, want dedupe to x.go + y.go", pack.Files)
	}
}

// --- unmapped requirement detection ---

func TestUnmappedRequirements(t *testing.T) {
	anchors := []PackFileAnchor{
		{Requirement: "CreateShortlink", Path: "s.go", Confidence: 1.0, Provenance: PackProvenanceMechanical},
		{Requirement: "fuzzy", Path: "f.go", Confidence: 0.6, Provenance: PackProvenanceMechanical}, // below threshold
	}
	got := UnmappedRequirements([]string{
		"CreateShortlink handler",
		"Flux capacitor calibration",
		"fuzzy matcher",
	}, anchors, PackScoutConfidenceThreshold)
	if len(got) != 2 {
		t.Fatalf("unmapped = %v, want 2 (fuzzy conf 0.6 < 0.7 threshold)", got)
	}
	if got[0] != "Flux capacitor calibration" || got[1] != "fuzzy matcher" {
		t.Errorf("unmapped = %v", got)
	}
}

// --- re-scout targeting ---

func TestDroppedAgenticRequirements_OnlyAgentic(t *testing.T) {
	dropped := []PackFileAnchor{
		{Requirement: "agentic-req", Path: "gone1.go", Provenance: PackProvenanceAgentic},
		{Requirement: "mech-req", Path: "gone2.go", Provenance: PackProvenanceMechanical},
		{Requirement: "agentic-req", Path: "gone3.go", Provenance: PackProvenanceAgentic}, // dup requirement
		{Requirement: "", Path: "gone4.go", Provenance: PackProvenanceAgentic},
	}
	got := DroppedAgenticRequirements(dropped)
	if len(got) != 1 || got[0] != "agentic-req" {
		t.Errorf("got %v, want only [agentic-req]", got)
	}
}

// --- scout output parsing ---

func TestParseScoutOutput_ExtractsLastJSONArray(t *testing.T) {
	out := `I looked at the repo. My answer:
[{"requirement": "Flux capacitor", "path": "flux/cap.go", "symbol": "Calibrate", "confidence": 0.82, "anchor_snippet": "func Calibrate() {"}]
That is the full mapping.`
	anchors, err := parseScoutOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(anchors) != 1 {
		t.Fatalf("anchors = %+v", anchors)
	}
	a := anchors[0]
	if a.Path != "flux/cap.go" || a.Symbol != "Calibrate" || a.Confidence != 0.82 || a.Provenance != PackProvenanceAgentic {
		t.Errorf("anchor = %+v", a)
	}
}

func TestParseScoutOutput_ErrorsOnGarbage(t *testing.T) {
	if _, err := parseScoutOutput("no json here"); err == nil {
		t.Error("want error for missing array")
	}
	if _, err := parseScoutOutput(`[{"unterminated": true`); err == nil {
		t.Error("want error for unterminated array")
	}
	if _, err := parseScoutOutput(`[]`); err == nil {
		t.Error("want error for empty mapping list")
	}
}

func TestBuildScoutPrompt_MentionsCapAndRequirements(t *testing.T) {
	p := buildScoutPrompt("/repo", []string{"Flux capacitor"}, 13)
	if !strings.Contains(p, "Flux capacitor") || !strings.Contains(p, "13 turns") {
		t.Errorf("prompt missing requirements or turn cap:\n%s", p)
	}
	if !strings.Contains(p, "READ-ONLY") && !strings.Contains(p, "read-only") {
		t.Errorf("prompt missing read-only constraint:\n%s", p)
	}
}

// --- cap enforcement (wall-clock bound) ---

func TestPiScoutRunner_TurnCapEnforced(t *testing.T) {
	dir := t.TempDir()
	pi := filepath.Join(dir, "fake-pi")
	// Sleeps far past the cap; CommandContext must kill it.
	if err := os.WriteFile(pi, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &PiScoutRunner{PiPath: pi, TurnBudget: 50 * time.Millisecond}
	start := time.Now()
	_, err := r.RunScout(context.Background(), dir, []string{"req"}, 2) // cap: 100ms
	if err == nil {
		t.Fatal("want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cap not enforced promptly: took %s", elapsed)
	}
	if !strings.Contains(err.Error(), "turn cap") {
		t.Errorf("error = %v, want mention of turn cap", err)
	}
}

// --- fail-open on scout failure ---

func TestEnsureContextPack_ScoutFailOpen(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	ids := []types.BeadID{"root-1", "member-2"}
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "Flux capacitor calibration"} // unmappable
	mock.beads["member-2"] = types.Bead{ID: "member-2", Title: "CreateShortlink handler"}
	runner := &fakeScoutRunner{err: errors.New("provider 429")}
	s, eventsDir := scoutEnabledSpawner(t, mock, ids, dir, runner)

	s.ensureContextPack(mock.beads["root-1"], dir)

	// Pack must still exist on every member, mechanical-only.
	for _, id := range ids {
		b := mock.beads[string(id)]
		pack, err := ParseContextPack(b.AgentContext)
		if err != nil || pack == nil {
			t.Fatalf("member %s lost pack on scout failure: %v", id, err)
		}
		for _, a := range pack.Files {
			if a.Provenance == PackProvenanceAgentic {
				t.Errorf("agentic anchor present despite scout failure: %+v", a)
			}
		}
	}
	// pack_scout event with scout_failed reason must be emitted (cost attribution).
	events := readEvents(t, eventsDir)
	if !strings.Contains(events, `"type":"pack_scout"`) || !strings.Contains(events, "scout_failed") {
		t.Errorf("no pack_scout/scout_failed event in:\n%s", events)
	}
}

// --- integration: unmappable requirement gets agentic entries ---

func TestEnsureContextPack_ScoutFillsUnmappedRequirement(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	ids := []types.BeadID{"root-1", "member-2"}
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "Flux capacitor calibration"}
	mock.beads["member-2"] = types.Bead{ID: "member-2", Title: "CreateShortlink handler"}
	runner := &fakeScoutRunner{mappings: []PackFileAnchor{
		{Requirement: "Flux capacitor calibration", Path: "flux/cap.go", Confidence: 0.85, Symbol: "Calibrate", Snippet: "func Calibrate()"},
	}}
	s, eventsDir := scoutEnabledSpawner(t, mock, ids, dir, runner)

	s.ensureContextPack(mock.beads["root-1"], dir)

	if len(runner.calls) != 1 {
		t.Fatalf("scout calls = %v, want exactly one pass per chain build", runner.calls)
	}
	if len(runner.calls[0]) != 1 || runner.calls[0][0] != "Flux capacitor calibration" {
		t.Errorf("scout requirements = %v, want only the unmappable title", runner.calls[0])
	}
	if runner.maxTurns[0] != 7 {
		t.Errorf("maxTurns = %d, want 7 from config", runner.maxTurns[0])
	}
	for _, id := range ids {
		pack, err := ParseContextPack(mock.beads[string(id)].AgentContext)
		if err != nil || pack == nil {
			t.Fatalf("member %s has no pack", id)
		}
		var agentic *PackFileAnchor
		for i := range pack.Files {
			if pack.Files[i].Provenance == PackProvenanceAgentic {
				agentic = &pack.Files[i]
			}
		}
		if agentic == nil {
			t.Fatalf("member %s pack has no agentic anchor: %+v", id, pack.Files)
		}
		if agentic.Path != "flux/cap.go" || agentic.Symbol != "Calibrate" || agentic.Confidence != 0.85 {
			t.Errorf("agentic anchor = %+v", *agentic)
		}
		// mechanical anchor from the graph must coexist
		foundMech := false
		for _, a := range pack.Files {
			if a.Provenance == PackProvenanceMechanical && a.Path == "shortlinks.go" {
				foundMech = true
			}
		}
		if !foundMech {
			t.Errorf("mechanical anchor lost: %+v", pack.Files)
		}
	}
	events := readEvents(t, eventsDir)
	if !strings.Contains(events, `"type":"pack_scout"`) || !strings.Contains(events, "scout_ok") {
		t.Errorf("no pack_scout/scout_ok event in:\n%s", events)
	}
}

// --- re-scout targeting at SHA refresh: broken-anchor subset only ---

func TestEnsureContextPack_RescoutOnlyBrokenAgenticAnchors(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	ids := []types.BeadID{"root-1", "member-2"}
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "Flux capacitor calibration"}
	mock.beads["member-2"] = types.Bead{ID: "member-2", Title: "CreateShortlink handler"}

	// Seed an existing pack: one agentic anchor whose file WILL be deleted
	// (broken at refresh), one whose file survives, plus a mechanical anchor.
	existing := &ContextPack{
		Version:    ContextPackVersion,
		BaseSHA:    currentGitHead(dir),
		Provenance: contextPackProvenance,
		Files: []PackFileAnchor{
			{Requirement: "CreateShortlink", Path: "shortlinks.go", Confidence: 1, Provenance: PackProvenanceMechanical},
			{Requirement: "Flux capacitor calibration", Path: "gone.go", Confidence: 0.8, Provenance: PackProvenanceAgentic},
			{Requirement: "Stable req", Path: "bar.go", Confidence: 0.9, Provenance: PackProvenanceAgentic},
		},
	}
	raw, _ := json.Marshal(existing)
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "Flux capacitor calibration", AgentContext: string(raw)}

	runner := &fakeScoutRunner{mappings: []PackFileAnchor{
		{Requirement: "Flux capacitor calibration", Path: "flux/cap2.go", Confidence: 0.7},
	}}
	s, _ := scoutEnabledSpawner(t, mock, ids, dir, runner)

	// Move HEAD: refresh path triggers.
	packScoutTestCommit(t, dir)

	s.ensureContextPack(mock.beads["root-1"], dir)

	if len(runner.calls) != 1 {
		t.Fatalf("scout calls = %v, want one targeted re-scout", runner.calls)
	}
	if len(runner.calls[0]) != 1 || runner.calls[0][0] != "Flux capacitor calibration" {
		t.Errorf("re-scout targeted %v, want only the broken-anchor requirement", runner.calls[0])
	}
	pack, err := ParseContextPack(mock.beads["member-2"].AgentContext)
	if err != nil || pack == nil {
		t.Fatalf("refreshed pack missing: %v", err)
	}
	var hasNew, hasStable bool
	for _, a := range pack.Files {
		switch {
		case a.Requirement == "Flux capacitor calibration" && a.Path == "flux/cap2.go":
			hasNew = true
		case a.Requirement == "Stable req" && a.Path == "bar.go":
			hasStable = true
		}
		if a.Path == "gone.go" {
			t.Errorf("broken anchor not dropped: %+v", a)
		}
	}
	if !hasNew || !hasStable {
		t.Errorf("refreshed files = %+v, want new agentic mapping + stable anchor kept", pack.Files)
	}
}

// --- scout disabled / runner nil: zero spend, mechanical only ---

func TestEnsureContextPack_ScoutDisabledNoSpend(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	ids := []types.BeadID{"root-1"}
	mock.beads["root-1"] = types.Bead{ID: "root-1", Title: "Flux capacitor calibration"}
	runner := &fakeScoutRunner{mappings: []PackFileAnchor{{Requirement: "Flux capacitor calibration", Path: "x.go", Confidence: 0.9}}}

	s, _ := packTestSpawner(t, mock, ids, dir) // default config: scout disabled, nil runner
	s.scoutRunner = runner

	s.ensureContextPack(mock.beads["root-1"], dir)
	if len(runner.calls) != 0 {
		t.Errorf("scout ran despite being disabled: %v", runner.calls)
	}
	pack, _ := ParseContextPack(mock.beads["root-1"].AgentContext)
	for _, a := range pack.Files {
		if a.Provenance == PackProvenanceAgentic {
			t.Errorf("agentic anchor in disabled-scout pack: %+v", a)
		}
	}
}

// --- resolveScoutConfig defaults ---

func TestResolveScoutConfig(t *testing.T) {
	r := resolveScoutConfig(nil)
	if r.enabled || r.maxTurns != PackScoutDefaultMaxTurns || r.turnBudget != PackScoutDefaultTurnBudget {
		t.Errorf("defaults = %+v", r)
	}
	cfg := &Config{Pi: PiRootConfig{StandardAgent: PiAgentConfig{LLM: LLMConfig{Provider: "p1", Model: "m1"}}}}
	cfg.ContextPack.Scout = ScoutConfig{Enabled: true, MaxTurns: 3, TurnBudget: "2s", Provider: "p2", Model: "m2", APIKeyName: "KEY2"}
	r = resolveScoutConfig(cfg)
	if !r.enabled || r.maxTurns != 3 || r.turnBudget != 2*time.Second ||
		r.provider != "p2" || r.model != "m2" || r.apiKeyName != "KEY2" {
		t.Errorf("resolved = %+v", r)
	}
	// Partial scout override: provider empty → falls back to fleet identity.
	cfg.ContextPack.Scout = ScoutConfig{Enabled: true}
	r = resolveScoutConfig(cfg)
	if r.provider != "p1" || r.model != "m1" {
		t.Errorf("fallback identity = %s/%s, want p1/m1", r.provider, r.model)
	}
}

func packScoutTestCommit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-qm", "move head")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}
