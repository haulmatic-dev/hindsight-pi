package core

// Live verification harness for the context pack (acceptance: "spawn one
// bead, confirm br show --json shows agent_context"). Runs only when
// HYDRA_LIVE_PACK_VERIFY=1; exercises the real production constructors
// (BeadFileReader, ChainRegistrar, ChainCompletionDetector, beads.Client
// against real br 0.2.10) and the real spawn-path entry ensureContextPack,
// against a temp git project. Skipped in normal CI because it shells out to
// git and br.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func liveRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func TestLiveVerify_ContextPackEndToEnd(t *testing.T) {
	if os.Getenv("HYDRA_LIVE_PACK_VERIFY") != "1" {
		t.Skip("set HYDRA_LIVE_PACK_VERIFY=1 to run the live pack verification")
	}
	dir := t.TempDir()
	keep := os.Getenv("HYDRA_LIVE_PACK_KEEP") != ""
	if keep {
		// Keep the store alive so the worker-prompt live verification can run
		// against it in a second process (see cmd/worker TestLiveVerify_PackInPromptTail).
		d, err := os.MkdirTemp("", "hydra-pack-live")
		if err != nil {
			t.Fatal(err)
		}
		dir = d
	}

	liveRun(t, dir, "git", "init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "graphify-out"), 0o755); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{
		"shortlinks.go":           "package x\n",
		"shortlinks_test.go":      "package x\n",
		"other.go":                "package x\n",
		"other_test.go":           "package x\n",
		"graphify-out/graph.json": `{"nodes":[{"name":"CreateShortlink","file":"shortlinks.go"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	liveRun(t, dir, "git", "-c", "user.email=t@t", "-c", "user.name=t", "add", ".")
	liveRun(t, dir, "git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	liveRun(t, dir, "br", "init", "--quiet")

	client := beads.NewClient()
	id1, err := client.CreateBead(dir, types.CreateBeadRequest{Title: "RED: CreateShortlink tests", Type: "test", Priority: 4})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := client.CreateBead(dir, types.CreateBeadRequest{Title: "GREEN: CreateShortlink impl", Type: "implement", Priority: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AddDependency(dir, "", id2, id1); err != nil {
		t.Fatal(err)
	}

	// Real production wiring: file reader -> registrar -> detector.
	projects := []types.Project{{Name: "live", Path: dir, Type: types.RepoTypeSingle}}
	detector := NewChainCompletionDetector(ChainCompletionConfig{}, &Overseer{})
	registrar := NewChainRegistrar(detector, NewBeadFileReader(projects))
	registrar.RegisterFromProjects(projects)
	if _, ok := detector.FindChainByBead(id2); !ok {
		t.Fatal("registrar did not register the dependency chain")
	}

	b1, err := client.GetBead(dir, "", id1)
	if err != nil {
		t.Fatal(err)
	}
	// br derives bead.Project from the store directory name; the config
	// project name must match it for gatesForProject to resolve.
	gates := &GateSet{DaemonOwned: true, Test: GateSpec{Cmd: "go test ./..."}}
	cfg := &Config{Projects: []ProjectConfig{{Name: b1.Project, Path: dir, Gates: gates}}}
	spawner := &WorkerSpawner{
		beadsClient:             client,
		config:                  cfg,
		chainCompletionDetector: detector,
		eventSink:               NewEventSink(t.TempDir()),
	}
	spawner.ensureContextPack(*b1, dir)

	// Acceptance: br show --json on a chain member exposes agent_context.
	showOut := liveRun(t, dir, "br", "show", string(id2), "--json")
	var detail []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(showOut), &detail); err != nil {
		t.Fatalf("br show --json not parseable: %v\n%s", err, showOut)
	}
	raw, ok := detail[0]["agent_context"]
	if !ok {
		t.Fatalf("br show --json has no agent_context field:\n%s", showOut)
	}
	// br emits agent_context as a JSON string, not an object.
	var acStr string
	if err := json.Unmarshal(raw, &acStr); err != nil {
		t.Fatalf("agent_context not a string: %v\n%s", err, raw)
	}
	var pack ContextPack
	if err := json.Unmarshal([]byte(acStr), &pack); err != nil {
		t.Fatalf("agent_context not a hydra ContextPack: %v\n%s", err, raw)
	}
	if pack.Provenance != contextPackProvenance {
		t.Errorf("provenance = %q", pack.Provenance)
	}
	if len(pack.GateCommands) != 1 || pack.GateCommands[0] != "go test ./..." {
		t.Errorf("gate_commands = %v", pack.GateCommands)
	}
	if !strings.HasPrefix(pack.SpecPattern, "*_test.go") {
		t.Errorf("spec_pattern = %q", pack.SpecPattern)
	}
	if len(pack.Files) != 1 || pack.Files[0].Path != "shortlinks.go" {
		t.Errorf("anchors = %+v", pack.Files)
	}
	t.Logf("LIVE VERIFY OK: br show %s --json carries agent_context pack (base_sha=%s, dir=%s)", id2, pack.BaseSHA, dir)
}
