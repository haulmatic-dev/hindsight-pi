# Gap PRD: Auto-Generated from Compliance Analysis


**Source PRD:** docs/PRDs/url-shortener.md

**Cycle:** 1

**Summary:** 3 gap(s) found: 1 critical behavior, 1 warning route, 1 info AC

### FR-001: Fix behavior drift in foo.go

**Type:** fr

**Priority:** P0

**Description:**
Handler response code does not match PRD behaviour inventory.

**PRD Reference:** PRD §4.5.2

**Target Files:** `internal/core/foo.go`

**Test Command:** `go test ./...`

**Build Command:** `go build ./...`

**Acceptance Criteria:**
- Source evidence (Java): PRD §4.5.2 specifies HTTP 201 for POST /shorten
- Go evidence (current): Current handler returns HTTP 200 for POST /shorten
- REQUIRED: change the Go file so the value matches the source. The previous SuggestedBead was: 'Fix status code in shorten handler' — that was not sufficient; agents closed these as 'verified intentional' without making the change.
- `go build ./...` exits 0
- `go test ./...` exits 0
- Re-run `tools/verify-source-vs-go --source-language auto --source-ref development --go-source .` and confirm zero gaps of category 'behavior'

### FR-002: Fix route drift in server.go

**Type:** fr

**Priority:** P1

**Description:**
Missing health check route /healthz.

**PRD Reference:** PRD §3.1

**Target Files:** `server.go`

**Test Command:** `go test ./...`

**Build Command:** `go build ./...`

**Acceptance Criteria:**
- Source evidence (Java): PRD §3.1 lists /healthz as a required route
- Go evidence (current): No handler registered for /healthz
- Either change the Go file to match the source, OR update the PRD's §4.5.2 Behaviour Inventory to document the intentional divergence. The previous SuggestedBead was: 'Register /healthz route' — on its own that's not enough.
- `go build ./...` exits 0
- `go test ./...` exits 0
- Re-run `tools/verify-source-vs-go --source-language auto --source-ref development --go-source .` and confirm zero gaps of category 'route'

## US-001: Integration — Fix Missing Routes and ACs

**As a** system
**I want** all PRD routes wired and acceptance criteria tested
**So that** the system is fully compliant

**Aggregates:**
- FR-002

**Acceptance Criteria:**
- Register /healthz route

## NEEDS HUMAN TRIAGE: Gaps Without Target Files

- **A1** (ac): No automated test covers the rate-limit AC.

## COMPLIANCE GATE: Verify Gaps Fixed

**After all above FRs/US close:**
- Re-run `hydra-compliance-analyzer`
- Verify zero gaps remain
- If gaps found → create new Gap PRD (cycle ++)
- If zero gaps → PRD approved

