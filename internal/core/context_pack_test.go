// Tests for the chain context pack: mechanical build from a fixture repo,
// SHA-keyed refresh, anchor validation, fail-open on br write error, and
// typed lifecycle events.
package core

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/mcp"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// packFixtureRepo builds a git repo with a dominant *_test.go convention and
// a graphify index, and returns its path.
func packFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("foo.go", "package x\n")
	write("foo_test.go", "package x\n")
	write("bar.go", "package x\n")
	write("bar_test.go", "package x\n")
	write("internal/baz/baz.go", "package baz\n")
	write("node_modules/junk/junk_test.go", "ignored\n") // vendored: must not count
	write("graphify-out/graph.json", `{"nodes":[
		{"name":"CreateShortlink","file":"shortlinks.go"},
		{"name":"ResolveShortlink","file":"shortlinks.go"},
		{"name":"UnrelatedThing","file":"other.go"}
	]}`)

	// git init + commit so currentGitHead returns a SHA (refresh test needs
	// a movable head).
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("-c", "user.email=t@t", "-c", "user.name=t", "add", ".")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	return dir
}

func TestBuildContextPack_FixtureRepo(t *testing.T) {
	dir := packFixtureRepo(t)
	gates := &GateSet{DaemonOwned: true, Test: GateSpec{Cmd: "go test ./..."}}

	pack := BuildContextPack(dir, gates, []string{"CreateShortlink", "missingterm"})

	if pack.Version != ContextPackVersion {
		t.Errorf("version = %d, want %d", pack.Version, ContextPackVersion)
	}
	if pack.BaseSHA == "" {
		t.Error("base_sha empty; fixture repo has a HEAD")
	}
	if len(pack.GateCommands) != 1 || pack.GateCommands[0] != "go test ./..." {
		t.Errorf("gate_commands = %v", pack.GateCommands)
	}
	if !strings.HasPrefix(pack.SpecPattern, "*_test.go") {
		t.Errorf("spec_pattern = %q, want *_test.go dominant", pack.SpecPattern)
	}
	if !strings.Contains(pack.SpecPattern, "colocated") {
		t.Errorf("spec_pattern = %q, want colocation hint", pack.SpecPattern)
	}
	foundInternal := false
	for _, d := range pack.TargetDirs {
		if d == "internal" || strings.HasPrefix(d, "internal"+string(filepath.Separator)) || d == "internal/baz" {
			foundInternal = true
		}
		if strings.Contains(d, "node_modules") {
			t.Errorf("target dirs leak node_modules: %v", pack.TargetDirs)
		}
	}
	if !foundInternal {
		t.Errorf("target_dirs = %v, want internal listed", pack.TargetDirs)
	}
	if len(pack.Files) != 1 || pack.Files[0].Path != "shortlinks.go" || pack.Files[0].Confidence != 1.0 {
		t.Errorf("files anchors = %+v, want exactly CreateShortlink→shortlinks.go", pack.Files)
	}
	if pack.Provenance != contextPackProvenance {
		t.Errorf("provenance = %q", pack.Provenance)
	}
}

func TestParseContextPack_RoundTrip(t *testing.T) {
	dir := packFixtureRepo(t)
	pack := BuildContextPack(dir, nil, nil)
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseContextPack(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseSHA != pack.BaseSHA {
		t.Errorf("round-trip base_sha mismatch")
	}
	if got.SpecPattern != pack.SpecPattern {
		t.Errorf("round-trip spec_pattern mismatch")
	}

	// Non-pack agent_context (human-written) must parse as "no pack".
	if p, err := ParseContextPack(`{"custom":"thing"}`); err != nil || p != nil {
		t.Errorf("non-pack agent_context: p=%v err=%v, want nil,nil", p, err)
	}
	if p, err := ParseContextPack(""); err != nil || p != nil {
		t.Errorf("empty agent_context: p=%v err=%v, want nil,nil", p, err)
	}
	if _, err := ParseContextPack("{not json"); err == nil {
		t.Error("malformed JSON must error")
	}
}

func TestValidatePackAnchors_DropsMissing(t *testing.T) {
	dir := packFixtureRepo(t) // foo.go, bar.go exist; gone.go does not
	pack := &ContextPack{
		Version:    ContextPackVersion,
		Provenance: contextPackProvenance,
		Files: []PackFileAnchor{
			{Requirement: "a", Path: "foo.go", Confidence: 1},
			{Requirement: "b", Path: "gone.go", Confidence: 1},
			{Requirement: "c", Path: "bar.go", Confidence: 0.6},
		},
	}
	dropped := ValidatePackAnchors(pack, dir)
	if len(dropped) != 1 || dropped[0].Path != "gone.go" {
		t.Errorf("dropped = %+v, want only gone.go", dropped)
	}
	if len(pack.Files) != 2 {
		t.Errorf("kept = %+v, want foo.go + bar.go", pack.Files)
	}
}

// packTestSpawner assembles a WorkerSpawner with just the fields
// ensureContextPack touches.
func packTestSpawner(t *testing.T, client BeadsClient, chainBeads []types.BeadID, projectPath string) (*WorkerSpawner, *ChainCompletionDetector) {
	t.Helper()
	d := NewChainCompletionDetector(ChainCompletionConfig{}, nil)
	root := chainBeads[0].String()
	d.chains[root] = &ChainInfo{
		RootBeadID:  chainBeads[0],
		AllBeadIDs:  chainBeads,
		ProjectPath: projectPath,
	}
	return &WorkerSpawner{
		beadsClient:             client,
		config:                  &Config{},
		chainCompletionDetector: d,
		eventSink:               NewEventSink(t.TempDir()),
	}, d
}

func TestEnsureContextPack_WritesPackToAllMembers(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	ids := []types.BeadID{"root-1", "member-2", "member-3"}
	for _, id := range ids {
		mock.beads[string(id)] = types.Bead{ID: id, Title: "CreateShortlink handler " + string(id)}
	}
	s, _ := packTestSpawner(t, mock, ids, dir)

	bead := mock.beads["root-1"]
	s.ensureContextPack(bead, dir)

	for _, id := range ids {
		b := mock.beads[string(id)]
		pack, err := ParseContextPack(b.AgentContext)
		if err != nil || pack == nil {
			t.Fatalf("member %s has no parseable pack: %q (err %v)", id, b.AgentContext, err)
		}
		if pack.BaseSHA == "" {
			t.Errorf("member %s pack has empty base_sha", id)
		}
	}
}

func TestEnsureContextPack_FailOpenOnWriteError(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	ids := []types.BeadID{"root-1", "member-2"}
	for _, id := range ids {
		mock.beads[string(id)] = types.Bead{ID: id, Title: "CreateShortlink"}
	}
	broken := &packErrBeadsClient{mockBeadsClient: mock}
	s, _ := packTestSpawner(t, broken, ids, dir)

	// Must not panic, must not return anything (fail-open by construction).
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.ensureContextPack(mock.beads["root-1"], dir)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ensureContextPack blocked on write error")
	}
	if got := mock.beads["root-1"].AgentContext; got != "" {
		t.Errorf("write error leaked into mock store: %q", got)
	}
}

func TestEnsureContextPack_SHARefresh(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	ids := []types.BeadID{"root-1", "member-2"}
	for _, id := range ids {
		mock.beads[string(id)] = types.Bead{ID: id, Title: "CreateShortlink"}
	}
	// Seed a stale pack: old SHA, one anchor that survives, one that is gone.
	stale := &ContextPack{
		Version:    ContextPackVersion,
		BaseSHA:    "0" + strings.Repeat("deadbeef", 5), // != current head
		Provenance: contextPackProvenance,
		Files: []PackFileAnchor{
			{Requirement: "keep", Path: "foo.go", Confidence: 1},
			{Requirement: "drop", Path: "gone.go", Confidence: 1},
		},
	}
	raw, _ := json.Marshal(stale)
	for _, id := range ids {
		b := mock.beads[string(id)]
		b.AgentContext = string(raw)
		mock.beads[string(id)] = b
	}
	s, _ := packTestSpawner(t, mock, ids, dir)

	s.ensureContextPack(mock.beads["root-1"], dir)

	pack, err := ParseContextPack(mock.beads["root-1"].AgentContext)
	if err != nil || pack == nil {
		t.Fatalf("no parseable pack after refresh: %v", err)
	}
	if pack.BaseSHA == stale.BaseSHA {
		t.Error("base_sha not refreshed")
	}
	// Mechanical sections regenerated:
	if !strings.HasPrefix(pack.SpecPattern, "*_test.go") {
		t.Errorf("mechanical section not regenerated, spec_pattern=%q", pack.SpecPattern)
	}
	// Agentic anchors re-validated, not regenerated: kept foo.go, dropped gone.go.
	if len(pack.Files) != 1 || pack.Files[0].Path != "foo.go" {
		t.Errorf("files after refresh = %+v, want only foo.go", pack.Files)
	}
}

func TestEnsureContextPack_StandaloneBeadGetsChainOfOnePack(t *testing.T) {
	dir := packFixtureRepo(t)
	mock := newMockBeadsClient()
	mock.beads["solo-1"] = types.Bead{ID: "solo-1", Title: "CreateShortlink"}
	s, _ := packTestSpawner(t, mock, []types.BeadID{"root-1"}, dir) // solo-1 not in the registered chain

	s.ensureContextPack(mock.beads["solo-1"], dir)
	pack, err := ParseContextPack(mock.beads["solo-1"].AgentContext)
	if err != nil || pack == nil {
		t.Fatalf("standalone bead got no parseable pack: %q (err %v)", mock.beads["solo-1"].AgentContext, err)
	}
	if pack.BaseSHA == "" {
		t.Error("standalone pack has empty base_sha")
	}
}

func TestRequirementTermsForBead(t *testing.T) {
	terms := RequirementTermsForBead("RED: Context Pack mechanical build")
	want := []string{"Context", "Pack", "mechanical", "build"}
	if strings.Join(terms, ",") != strings.Join(want, ",") {
		t.Errorf("terms = %v, want %v", terms, want)
	}
	if got := RequirementTermsForBead("FIX: a bug"); len(got) != 0 {
		t.Errorf("short-token title must yield no terms, got %v", got)
	}
}

type packErrBeadsClient struct {
	*mockBeadsClient
}

func (p *packErrBeadsClient) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	return errors.New("br update --agent-context: exit status 7")
}

func TestConsultPackOnReservation_OffAnchorEmitsEvent(t *testing.T) {
	dir := t.TempDir()
	mock := newMockBeadsClient()
	pack := &ContextPack{
		Version:    ContextPackVersion,
		BaseSHA:    "abc",
		Provenance: contextPackProvenance,
		Files:      []PackFileAnchor{{Requirement: "x", Path: "handler.go", Confidence: 1}},
	}
	raw, _ := json.Marshal(pack)
	mock.beads["web-1"] = types.Bead{ID: "web-1", Project: "web", AgentContext: string(raw)}

	sinkDir := t.TempDir()
	o := &Overseer{
		beadsClient: mock,
		beadSource:  &BeadProvenanceIndex{},
		eventSink:   NewEventSink(sinkDir),
	}
	o.beadSource.Note(dir, []string{"web-1"})

	o.consultPackOnReservation(mcpMessage("web-1", "evil/exploit.go"))

	events, err := ReadEvents(filepath.Join(sinkDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Type == "pack_reservation_offscope" && ev.Bead == "web-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("no pack_reservation_offscope event in %v", events)
	}
}

func TestConsultPackOnReservation_OnAnchorSilent(t *testing.T) {
	dir := t.TempDir()
	mock := newMockBeadsClient()
	pack := &ContextPack{
		Version:    ContextPackVersion,
		BaseSHA:    "abc",
		Provenance: contextPackProvenance,
		Files:      []PackFileAnchor{{Requirement: "x", Path: "handler.go", Confidence: 1}},
	}
	raw, _ := json.Marshal(pack)
	mock.beads["web-1"] = types.Bead{ID: "web-1", Project: "web", AgentContext: string(raw)}

	sinkDir := t.TempDir()
	o := &Overseer{
		beadsClient: mock,
		beadSource:  &BeadProvenanceIndex{},
		eventSink:   NewEventSink(sinkDir),
	}
	o.beadSource.Note(dir, []string{"web-1"})

	o.consultPackOnReservation(mcpMessage("web-1", "handler.go"))

	events, err := ReadEvents(filepath.Join(sinkDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("on-anchor reservation must be silent, got %v", events)
	}
}

func mcpMessage(beadID, filePath string) *mcp.Message {
	return &mcp.Message{
		Type: mcp.MsgTypeFileReserved,
		Payload: map[string]interface{}{
			"bead_id":   beadID,
			"file_path": filePath,
		},
	}
}
