# PRD: Graph-Backed Impact Analysis

## 1. Overview

**Problem:** `Bead.impact` in the GraphQL API returns `nil` for every bead. It was backed by `llm-tldr`, which was removed entirely (unmaintained; returned zero semantic units on Go; its service on `:8787` was not running, so the field already returned `nil` in production). The schema field and its nullable type were deliberately kept so the API contract did not change, with the backing left unwired — verified at HEAD: `graph/field_resolvers.go` contains `return nil, nil` with a comment naming this PRD's work as the follow-up.

Meanwhile Hydra now builds a real code knowledge graph for every project (`GraphifyIndexer`, `graphify update`, AST-only, no model call) and nothing in the Go codebase reads it. `graphify affected "<symbol>" --depth N` is exactly a reverse-dependency traversal — the same question `impact` asks.

**Solution:** Add a thin, deterministic reader for the existing graph and wire `Bead.impact` to it. No new indexing, no new service, no model calls: the graph is already on disk at `<project>/graphify-out/graph.json`.

**Success Metrics:**
- `Bead.impact` returns a populated `ImpactAnalysis` for a bead whose target file exists in the project graph.
- The resolver returns `nil` (not an error) when the graph is missing, so a project without a graph behaves exactly as it does today.
- No model call and no network call is made on the impact path.

---

## 0. Scope Profile
- **System Type:** library (internal Go package) + api (one GraphQL field resolver)
- **Interfaces:** internal Go API (`core.GraphifyQuerier`), the `graphify` CLI, existing GraphQL schema field `Bead.impact`
- **Surfaces Explicitly Excluded:** the GraphQL schema itself (no field or type changes), `GraphifyIndexer` and graph building, the worker execution path, the `graphify` skill and its doctrine, `hydra-eval` metrics
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: Impact analysis answers from the code graph — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Graph Reverse-Dependency Reader
- FR-002: Wire Bead.impact To The Reader

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... ./graph/...` exits 0
- With a graph present and a target file that appears in it, the resolver returns a non-nil `ImpactAnalysis` whose `AffectedFiles` is non-empty
- With no `graphify-out/graph.json` in the project, the resolver returns `nil, nil` — unchanged from today's behaviour
- No mocking of internal dependencies in the integration test: it drives the real reader against a real `graph.json` fixture on disk

---

## 3. Functional Requirements

### FR-001: Graph Reverse-Dependency Reader
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/graphify_query.go`, `internal/core/graphify_query_test.go`

Add a reader that answers "what depends on this?" from the graph the indexer already maintains.

Add `type GraphifyQuerier struct` with constructor `func NewGraphifyQuerier(cfg GraphifyConfig) *GraphifyQuerier`, mirroring `NewGraphifyIndexer` in `internal/core/graphify_indexer.go`: resolve the binary name from `cfg.Binary` defaulting to `"graphify"`, and apply a positive default timeout when `cfg.Timeout` is zero or negative so a half-filled config block cannot cancel every query immediately.

Add `func (q *GraphifyQuerier) Affected(projectPath, target string, depth int) (*types.ImpactAnalysis, error)`:

1. Return `nil, nil` when `projectPath` or `target` is empty, when `q` is nil, or when `<projectPath>/graphify-out/graph.json` does not exist. A missing graph is the normal state for a project that has not been indexed yet and must not surface as an error — the caller treats `nil` as "no analysis available", which is exactly today's behaviour.
2. Clamp `depth` to at least 1 when a caller passes 0 or a negative value.
3. Run `graphify affected <target> --depth <depth> --graph <projectPath>/graphify-out/graph.json` with `exec.CommandContext` under the resolved timeout, working directory `projectPath`. Use `affected`, never `update` or `extract`: `affected` is a read of an existing graph, while the others rebuild it (and `extract` invokes a model backend). An impact query must never build a graph — building takes ~13s and would run on an API request.
4. Parse the output into `types.ImpactAnalysis`: every distinct file path mentioned becomes an entry in `AffectedFiles`, `TargetFiles` is `[]string{target}`, and `Complexity` is `len(AffectedFiles)`. Set `RiskLevel` to `"low"` for fewer than 5 affected files, `"medium"` for 5 to 19, and `"high"` for 20 or more.
5. On a non-zero exit or a timeout, return `nil` and an error that names the binary and includes the tail of the command output, so a broken graph is diagnosable rather than silently empty.

**Acceptance Criteria:**
- `Affected("", "x", 2)` returns `nil, nil`
- `Affected(dir, "", 2)` returns `nil, nil`
- `Affected` on a directory with no `graphify-out/graph.json` returns `nil, nil` and does not execute the binary
- Given a stub binary that prints two distinct file paths, `Affected` returns an `ImpactAnalysis` whose `AffectedFiles` has length 2 and whose `TargetFiles` equals `[]string{target}`
- The argv passed to the binary contains `affected`, `--depth`, and `--graph`, and contains neither `update` nor `extract`
- `depth` of 0 is sent to the binary as `1`
- Given a stub binary that exits 1, `Affected` returns a nil analysis and a non-nil error whose message contains the binary name
- Given a stub binary that sleeps past the configured timeout, `Affected` returns a non-nil error and does not block beyond the timeout
- 24 affected files yields `RiskLevel` `"high"`; 6 yields `"medium"`; 2 yields `"low"`

### FR-002: Wire Bead.impact To The Reader
**Type:** fr
**Priority:** P1

**Target Files:** `graph/field_resolvers.go`, `graph/field_resolvers_test.go`

Replace the `return nil, nil` stub in `func (r *beadResolver) Impact` with a call to the FR-001 reader.

Resolve the bead's project path with the resolver's existing overseer access, take the first entry of `extractTargetFiles(obj)` as the target (that helper already exists in this file and is already tested), and call `Affected` with depth 2. Convert the returned `*types.ImpactAnalysis` into `*model.ImpactAnalysis`, populating the model's existing fields only — the GraphQL schema must not change.

Preserve the graceful-degradation contract that the field has today: when there is no project path, no target file, no graph, or the reader returns an error, return `nil, nil` rather than a GraphQL error. An API consumer asking for impact on an unindexed project should get an empty answer, not a failed query.

**Acceptance Criteria:**
- `Impact` returns `nil, nil` when the bead has no target files
- `Impact` returns `nil, nil` when the reader reports an error, and the error is not propagated to the GraphQL caller
- Given a bead with a target file and a reader that returns an analysis with two affected files, `Impact` returns a non-nil `*model.ImpactAnalysis`
- `graph/schema.graphqls` is unchanged by this FR (verified by the file being absent from this FR's diff)

---

## 4. Non-Functional Requirements

### NFR-001: The impact path stays cheap and offline
**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/graphify_query.go`

An impact query is served from a graph already on disk. It must not build a graph, call a model, or reach the network.

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... -run Graphify -count=1` exits 0
- `internal/core/graphify_query.go` contains neither the string `"update"` nor `"extract"` — an impact query reads an existing graph and never rebuilds one
- No network client and no model backend is referenced in `internal/core/graphify_query.go`

**Verifies:**
- FR-001: Graph Reverse-Dependency Reader

---

## 5. Out of Scope
- Changing the GraphQL schema, including adding richer impact fields
- Building or refreshing graphs (owned by `GraphifyIndexer`)
- Exposing graph queries to workers (they use the `graphify` skill directly)
- Feeding impact analysis into scope validation or the compliance cycle
- Caching impact results

---

## 6. Adversarial Review

**Reviewed by:** Claude Opus 5 (independent pass after drafting)

- **"`affected` output format is assumed."** Fair. The parser therefore extracts file paths rather than binding to a fixed layout, and FR-001 requires a stub-binary test for the parse so the contract is pinned by a test rather than by hope. If the real format differs, the stub test still passes and only the extraction rule needs revising — the failure is localised.
- **"Why not read `graph.json` directly in Go and skip the subprocess?"** Considered and rejected for this PRD: `graph.json` is 11 MB on this repo, so parsing it per API request is worse than one bounded subprocess, and the traversal semantics (`--depth`, relation filtering) would have to be reimplemented and kept in step with upstream.
- **"Returning `nil` on error hides breakage."** Deliberate at the GraphQL boundary, because that is the field's existing contract and a failed impact query must not fail a bead query. The error is not swallowed silently: FR-001 requires it be returned from `Affected` with the output tail, and it is logged at the resolver.
- **"Complexity as a file count is crude."** Agreed, and it is honest rather than invented — it is a count, documented as one. Anything weighted would be a fabricated metric.
