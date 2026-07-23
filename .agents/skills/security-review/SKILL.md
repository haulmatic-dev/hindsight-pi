---
name: security-review
description: Security review of hydra-produced or hydra-adjacent code changes. Runs the deterministic scanners first (tools/security-scan wrapping gitleaks + govulncheck), then applies a checklist for what scanners cannot catch — secrets handling, command injection, unsafe exec, path traversal, authz, PAT/token leakage. Emits findings as gap-report JSON compatible with the compliance adjudicator. Use when reviewing a diff, a worktree, a PR, or a whole project for security defects.
---

# Security Review Skill

## Mental Model

Two lines of defense, in order:

1. **Deterministic scanners** catch what pattern-matching can: committed secrets
   (gitleaks), known-vulnerable dependencies (govulncheck). Run them first,
   always. Never hand-review for something a scanner already checks.
2. **This skill's checklist** covers what scanners cannot: injection paths,
   command construction, authz logic, token handling, fail-open/fail-closed
   decisions. Reason about these only after the scanners have run.

This mirrors the prd-lint philosophy: mechanical checks gate first; judgment
is reserved for what machines can't decide.

Findings are DATA for the compliance pipeline, not prose for a human: output
gap-report JSON (schema below) so verdicts flow through the gap adjudicator
and become beads. A human-readable summary comes after the JSON, never
instead of it.

---

## Phase 1 — Deterministic Scanners

Run `tools/security-scan` against the project root:

```bash
# project-vendored (builds on demand), else $HYDRA_TOOLS_DIR, else system path
go build ./tools/security-scan 2>/dev/null || true
./tools/security-scan/security-scan --project . --output /tmp/security-scan.json
```

- Exit 0 = clean, 1 = findings (expected, not an error), 2 = config error.
- If the binary and its scanners are unavailable, note `scanners: unavailable`
  in your report and continue to Phase 2 — the checklist review still runs.
- Include every scanner gap verbatim in your final gap list. Do not
  re-adjudicate scanner findings; the adjudicator does that.
- NEVER quote a discovered secret value anywhere in your output. Refer to it
  as `<redacted>` plus file:line.

## Phase 2 — Checklist Review

Scope: the diff/worktree under review; when reviewing a whole project, focus
on the areas below. For each area, look at the code — do not assert from
memory.

### 2.1 Secrets & credentials
- New hardcoded credentials, API keys, connection strings — including in
  tests, fixtures, and docs (gitleaks covers committed files; you cover
  values built at runtime, e.g. string-concatenated keys).
- Tokens/PATs reaching logs or error messages. Hydra precedent to enforce:
  `internal/core/git_push.go` builds one-shot authenticated URLs
  (`x-access-token:<PAT>@host`) and scrubs output with `redactToken` — any
  new code path that touches `UserContext.Token` or `GitCredentials`
  (`internal/core/user_config.go`) must redact before logging and must never
  persist the token to disk or git config.

### 2.2 Command construction & unsafe exec
- `exec.Command` with attacker-influenced arguments: bead titles,
  descriptions, PRD content, file paths from gap reports are all
  LLM-or-repo-controlled — treat them as untrusted.
- `bash -c` with interpolated strings (see `ship_gate.go` for the pattern
  where the command string is operator config — that is trusted; a bead
  description is not).
- Shell metacharacters flowing into git arguments; prefer fixed argv arrays.

### 2.3 Path traversal & file writes
- Paths assembled from bead/gap data (`target_files`, worktree names)
  escaping the project dir: check `filepath.Clean` + prefix containment
  before writes/deletes.
- Writes outside the intended roots (project dir, `.hydra/`, worktrees,
  backlog dirs). The observer/compliance tools promise observation-only or
  scoped writes — verify new code keeps that promise.

### 2.4 AuthZ & trust boundaries
- Multi-user surfaces: per-user config, PAT resolution, and project discovery
  (`internal/core/user_config.go`, `multi_user_discovery.go`) — can user A's
  project get pushed with user B's token? Any new cross-user lookup needs an
  explicit ownership check.
- API endpoints (`internal/api`, `graph/`): new routes without the existing
  auth middleware; GraphQL resolvers exposing other users' data.

### 2.5 Fail-open vs fail-closed
- Security gates must fail CLOSED on their own logic errors but may fail
  OPEN on missing optional tooling (documented pattern: missing scanner
  binary = fail-open with visibility). Flag any inversion: a gate that
  silently passes on parse errors, or a hard dependency on an optional tool.

### 2.6 Injection into LLM contexts
- Repo/bead content interpolated into agent prompts (worker prompts,
  adjudicator prompts): flag places where untrusted content could smuggle
  instructions that change gate outcomes (e.g. text inside a gap description
  that asks the adjudicator to rule it false-positive).

## Phase 3 — Output

Emit a single JSON object first (this exact schema — it is the analyzer's
gap-report shape, consumable by the adjudicator), then a short human summary:

```json
{
  "passed": false,
  "checks": [
    {"name": "security-scan", "passed": false, "details": "2 gap(s) found"},
    {"name": "security-review-checklist", "passed": false, "details": "1 gap(s) found"}
  ],
  "gaps": [
    {
      "id": "SEC-001",
      "category": "security",
      "severity": "critical",
      "description": "What is wrong, where, and the concrete attack/failure path. No secret values.",
      "target_files": ["internal/core/example.go"],
      "suggested_bead": "One-sentence actionable fix"
    }
  ],
  "summary": "N security gap(s): M from scanners, K from checklist review."
}
```

Severity rules:
- `critical` — exploitable now: live secret, injection with an untrusted
  source reaching it, cross-user token misuse, writes escaping project roots.
- `warning` — defense-in-depth gaps: vulnerable dep without a reachable
  call path, missing redaction on an unlikely log path, fail-open where
  fail-closed is safer.

Discipline:
- Every gap needs evidence: file:line plus the specific code construct. A
  gap you cannot point to in code is not a gap — leave it out rather than
  hedge.
- Zero findings is a valid result: `{"passed": true, "gaps": []}` with the
  checks you actually ran. Do not manufacture findings to look thorough.
- One gap per root cause; list all affected files in `target_files` instead
  of duplicating the gap per file.
