package beads

// Round-trip: br 0.2.10 emits agent_context (a JSON string) in both
// `br list --json` summaries and `br show --json` details; the parser maps
// it onto types.Bead.AgentContext.

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

const agentContextListJSON = `{
	"issues": [{
		"id": "proj-abc",
		"title": "build pack",
		"issue_type": "implement",
		"status": "open",
		"priority": 3,
		"labels": [],
		"created_at": "2026-09-20T00:00:00Z",
		"updated_at": "2026-09-20T00:00:00Z",
		"agent_context": "{\"version\":1,\"base_sha\":\"abc\",\"provenance\":\"hydra-context-pack\"}",
		"dependency_count": 0,
		"dependent_count": 0
	}],
	"total": 1, "limit": 100, "offset": 0, "has_more": false
}`

const agentContextDetailJSON = `[{
	"id": "proj-abc",
	"title": "build pack",
	"issue_type": "implement",
	"status": "open",
	"priority": 3,
	"labels": [],
	"created_at": "2026-09-20T00:00:00Z",
	"updated_at": "2026-09-20T00:00:00Z",
	"agent_context": "{\"version\":1,\"base_sha\":\"abc\",\"provenance\":\"hydra-context-pack\"}",
	"dependencies": []
}]`

func TestParseBeadList_AgentContext(t *testing.T) {
	p := NewParser()
	beads, err := p.ParseBeadList([]byte(agentContextListJSON), BeadContext{Project: "proj", Repo: ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(beads) != 1 {
		t.Fatalf("got %d beads", len(beads))
	}
	want := `{"version":1,"base_sha":"abc","provenance":"hydra-context-pack"}`
	if beads[0].AgentContext != want {
		t.Errorf("AgentContext = %q, want %q", beads[0].AgentContext, want)
	}
}

func TestParseBeadDetail_AgentContext(t *testing.T) {
	p := NewParser()
	b, err := p.ParseBeadDetail([]byte(agentContextDetailJSON), BeadContext{Project: "proj", Repo: ""})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"base_sha":"abc","provenance":"hydra-context-pack"}`
	if b.AgentContext != want {
		t.Errorf("AgentContext = %q, want %q", b.AgentContext, want)
	}
	// Beads without the field stay empty (br omits null fields).
	noField := `[{"id":"proj-x","title":"t","issue_type":"task","status":"open","priority":1,"labels":null,"created_at":"2026-09-20T00:00:00Z","updated_at":"2026-09-20T00:00:00Z"}]`
	b2, err := p.ParseBeadDetail([]byte(noField), BeadContext{Project: "proj", Repo: ""})
	if err != nil {
		t.Fatal(err)
	}
	if b2.AgentContext != "" {
		t.Errorf("AgentContext = %q, want empty", b2.AgentContext)
	}
}

// Compile-time guard: the concrete client satisfies the widened interface
// used by the daemon.
var _ interface {
	UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error
} = (*Client)(nil)
