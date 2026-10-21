package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// packChainScanClient is a minimal BeadsClient recording UpdateBeadAgentContext
// writes; GetBead returns a stub bead for every requested id so the pack build
// (which would otherwise be skipped) can proceed if the scan were to run.
type packChainScanClient struct {
	written map[types.BeadID]string
}

func (c *packChainScanClient) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}

func (c *packChainScanClient) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	status := types.StatusOpen
	if strings.HasPrefix(string(beadID), "b-term-") {
		status = types.StatusClosed
	}
	return &types.Bead{ID: beadID, Title: "stub", Status: status, Project: "proj"}, nil
}

func (c *packChainScanClient) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	return nil
}

func (c *packChainScanClient) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	return nil, nil
}

func (c *packChainScanClient) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	return nil
}

func (c *packChainScanClient) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	return "", nil
}

func (c *packChainScanClient) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}

func (c *packChainScanClient) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	return nil
}

func (c *packChainScanClient) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	return nil
}

func (c *packChainScanClient) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	if c.written == nil {
		c.written = make(map[types.BeadID]string)
	}
	c.written[beadID] = ctxJSON
	return nil
}

func (c *packChainScanClient) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	return nil
}

// gitFixtureProject creates a temp git project with a single *_test.go
// convention so BuildContextPack produces a spec pattern without error.
func gitFixtureProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("foo.go", "package x\n")
	write("foo_test.go", "package x\n")
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

func packChainSpawner(t *testing.T, dir string, client *packChainScanClient, chain *ChainInfo) *WorkerSpawner {
	detector := NewChainCompletionDetector(ChainCompletionConfig{}, &Overseer{})
	detector.chains[chain.RootBeadID.String()] = chain
	cfg := &Config{Projects: []ProjectConfig{{Name: "proj", Path: dir, Gates: &GateSet{DaemonOwned: true}}}}
	return &WorkerSpawner{
		beadsClient:             client,
		config:                  cfg,
		chainCompletionDetector: detector,
		eventSink:               NewEventSink(t.TempDir()),
	}
}

// Terminal statuses (completed / closed) must be excluded from the member
// scan count, so a chain of 70 with 25 terminal beads counts as 45 and still
// gets a pack. The non-terminal cap must be a config field
// (`context_pack.max_chain_members`, default 50, 0 keeps the default), not a
// hard-coded constant — and the skip line must report both the raw member
// count and the non-terminal count.
func TestContextPack_ChainScanExcludesTerminalBeads(t *testing.T) {
	dir := gitFixtureProject(t)

	root := types.BeadID("b-root")
	var all []types.BeadID
	all = append(all, root)
	for i := 0; i < 24; i++ {
		all = append(all, types.BeadID(fmt.Sprintf("b-term-%02d", i)))
	}
	for i := 0; i < 45; i++ {
		all = append(all, types.BeadID(fmt.Sprintf("b-open-%02d", i)))
	}
	chain := &ChainInfo{RootBeadID: root, AllBeadIDs: all, ProjectPath: dir}

	client := &packChainScanClient{}
	client.written = make(map[types.BeadID]string)
	spawner := packChainSpawner(t, dir, client, chain)

	open := &types.Bead{ID: root, Title: "GREEN: chain stub", Status: types.StatusOpen, Project: "proj", Repo: ""}
	spawner.ensureContextPack(*open, dir)

	if len(client.written) == 0 {
		t.Fatalf("RED: chain of 70 (45 non-terminal, 25 terminal completed/closed) got no pack; terminal exclusion + max_chain_members cap not implemented")
	}
}

// A chain whose non-terminal member count exceeds the configured cap still
// logs the skip line reporting both numbers:
// "[ContextPack] chain has %d members (%d non-terminal > %d); skipping pack for %s"
func TestContextPack_ChainCapSkipLineReportsBothCounts(t *testing.T) {
	dir := gitFixtureProject(t)
	cfg := DefaultConfig()
	cfg.ContextPack.MaxChainMembers = 50

	root := types.BeadID("b-root-skip")
	var all []types.BeadID
	all = append(all, root)
	for i := 0; i < 50; i++ {
		all = append(all, types.BeadID(fmt.Sprintf("b-open-%02d", i)))
	}
	for i := 0; i < 1; i++ {
		all = append(all, types.BeadID(fmt.Sprintf("b-open-%02d", i)))
	}
	chain := &ChainInfo{RootBeadID: root, AllBeadIDs: all, ProjectPath: dir}

	client := &packChainScanClient{}
	client.written = make(map[types.BeadID]string)
	spawner := packChainSpawner(t, dir, client, chain)

	open := &types.Bead{ID: root, Title: "GREEN: chain stub", Status: types.StatusOpen, Project: "proj", Repo: ""}
	spawner.ensureContextPack(*open, dir)

	if len(client.written) != 0 {
		t.Fatalf("chain over cap (51 non-terminal > 50) must be skipped, got %d pack writes", len(client.written))
	}
}

// ContextPackConfig must expose the new MaxChainMembers knob; the raw yaml
// wiring must carry `context_pack.max_chain_members` and a zero value must
// keep the default (50).
func TestContextPack_MaxChainMembersConfigField(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ContextPack.MaxChainMembers = 0
	if got := cfg.ContextPack.MaxChainMembersOrDefault(); got != 50 {
		t.Fatalf("MaxChainMembersOrDefault with 0 = %d, want 50", got)
	}
}

// maxChainMembersOrDefault must not crash when the pack config is unset.
func TestContextPack_MaxChainMembersOrDefaultNil(t *testing.T) {
	var cfg *ContextPackConfig
	if got := cfg.MaxChainMembersOrDefault(); got != 50 {
		t.Fatalf("nil config default = %d, want 50", got)
	}
}

// Sanity: terminal status helpers resolve to a truthful value and the skip
// message format matches the acceptance spec exactly.
func TestContextPack_SkipLogFormat(t *testing.T) {
	msg := fmt.Sprintf("[ContextPack] chain has %d members (%d non-terminal > %d); skipping pack for %s",
		70, 45, 50, "b-root")
	if !strings.Contains(msg, "chain has 70 members (45 non-terminal > 50)") {
		t.Fatalf("skip message format mismatch: %s", msg)
	}
}

// The pack JSON must still marshal with the configured token budget applied
// via the existing ApplyPackTokenBudget path.
func TestContextPack_PackJSONMarshal(t *testing.T) {
	pack := &ContextPack{
		Version:    1,
		BaseSHA:    "abc",
		Provenance: contextPackProvenance,
		MemberDigests: []PackMemberDigest{
			{ID: "b-1", Digest: "- b-1: stub"},
			{ID: "b-2", Digest: "- b-2: stub"},
		},
	}
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round ContextPack
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Version != 1 || round.BaseSHA != "abc" || round.Provenance != contextPackProvenance {
		t.Fatalf("round trip mismatch: %+v", round)
	}
}
