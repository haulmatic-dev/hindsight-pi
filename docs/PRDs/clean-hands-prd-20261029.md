# PRD: Clean Hands — Scratch Scaffolding, Signature-Based Junk Gates, Publish Guard

**Version:** 1.0 · **Date:** 2026-10-29 · **System Type:** library · **Repo:** components/hydra

## Problem

Hydra pushed worker-invented cache and scratch artifacts to a product remote.
Measured on srv-data-harbour (verified 2026-10-29, adversarial review
round 1 applied):

- Publish branch `hydra/publish-srv-data-harbour` (PR #596, since closed
  unmerged) carried **3,296 junk files of 3,742 (88%)** against
  `origin/development`: `src/src/.tmp-jest/**` (2,407), `src/.jest-cache/**`
  (620 — junk with no `.tmp` in the name), `src/.tmp/jest-cache/` (198),
  `.tmp2/jest/` (62), root `.tmp-*` scratch (9). Real deliverables: **446
  files**. `origin/feat/unit-test-coverage-rollout` carries a further
  **1,334 pre-existing junk files**. Fourteen stale open PRs carry
  1.4K–39K junk files each.
- Root chain: workers needed cache/scratch space, **nothing provided it**, and
  five different ad-hoc paths were invented (`.tmp-jest/`, `.tmp2/jest/`,
  `src/.tmp/jest-cache/`, `src/.jest-cache/`, root `.tmp-*`); broad `git add`
  staged them; a worker's later `.gitignore` fix used non-matching patterns
  (`.tmp/`, `/src/.tmp-jest` — root-level paths unmatched); **ScopeGate is
  warn-only** for off-scope files (`config.go:907-915` default "warn";
  `/etc/hydra/hydra.yaml` sets no `scope.mode`; targetless beads stay
  warn-only even in block mode, `bead_closer.go:1715-1720`); the publisher
  has **zero junk filtering** (`publisher.go`); and publish backoff
  (`overseer.go:1232-1258` `notePublishBackoff`) is uncapped — any
  fail-closed guard added without an escalation exit becomes a new silent
  wedge.
- Existing machinery to build on: `ensureGitignoreLine` already injects
  ignores at worktree creation (`worktree_manager.go:354`); worktree removal
  happens at exactly five sites (Remove `:1012`, IntegrateAndRemove `:816`,
  QuarantineAndRemove `:980`, reconciler `RemoveAll` `:448`/`:1103`).

Design was adversarially reviewed before this PRD (verdict
SHIP-WITH-AMENDMENTS on layers, REWORK on remediation); all amendments are
incorporated below. Remediation of the already-pushed junk is deliberately
out of scope (product-repo bead).

## Goals

- Workers never need to invent scratch/cache locations, and scratch is
  structurally invisible to `git add`.
- Junk that still appears cannot close (distinct failure class) and cannot
  reach any remote (fail-closed publish with a bounded, visible exit).
- Patterns are content signatures that survive renames and cannot match real
  code (verified negative probes).

### FR-001: Scratch scaffolding + ignore injection + lifecycle pruning

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/worktree_manager.go`, `internal/core/worktree_manager_scratch_test.go`, `internal/core/worker_spawner.go`, `cmd/worker/main.go`
**Test Command:** `go test ./internal/core/ -run 'TestScratch' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

(a) Scratch lives **outside every GC-scanned tree**:
`$HYDRA_HOME/.hydra-scratch/<sanitized-project>/<sanitized-key>/` (a NEW
root — `CleanupStaleWorktrees` reads only the worktrees dir, so active
scratch can never be swept as a stale/partial entry). `EnsureWorktree`
(:300 wrapper) performs **idempotent** scratch `MkdirAll` + `ensureGitignoreLine`
(existing, `:354`) for every FR-002 pattern **on every call** — create AND
reuse paths alike (the locked fast path at `:431-433` returns early today;
mirror `provisionGraphifyOut`'s idempotent pattern) — so legacy and reused
worktrees are protected too, and a broad `git add .` cannot stage junk even
when workers recreate their old habits.
(b) One shared prune helper removes the scratch dir, hooked at the actual
teardown sites: `removeWorktreeAndBranchName` (`:836`, covers Remove and
IntegrateAndRemove), the teardown inside `QuarantineBranchAndRemove`
(`~:1003`), and both `os.RemoveAll` sites in `CleanupStaleWorktrees`
(`:1068`, `:1103`). `ensureWorktreeLocked`'s `:448` path is
recreate-not-remove and must NOT prune. `CleanupStaleWorktrees` also sweeps
stale `$HYDRA_HOME/.hydra-scratch/<project>/<key>` entries past maxAge so
GC'd worktrees never orphan scratch. A table-driven test asserts each site
prunes and that the recreate path does not. (c) The spawner's env builder
(`worker_spawner.go:601` path) exports `HYDRA_SCRATCH_DIR`; the worker
(`cmd/worker/main.go` workspace scaffolding + phase guidance text) states:
scratch, caches, retry scripts and logs go in `$HYDRA_SCRATCH_DIR` (jest
`--cacheDirectory "$HYDRA_SCRATCH_DIR/jest"`, `TMPDIR` likewise), never
inside the repo tree. Update `CHANGELOG.md` (Added). Test functions are
named `TestScratch_*`.

**Acceptance Criteria:**
- `TestScratch_DirCreatedOutsideTree`: worktree creation makes the scratch
  sibling; `.gitignore` in the worktree contains every FR-002 signature
  pattern (probe files matching each pattern are ignored by
  `git check-ignore`).
- `TestScratch_PruneSites`: each teardown path removes the scratch dir and
  the recreate path (`ensureWorktreeLocked` recovery) does not
  (table-driven over the removal entry points); `CleanupStaleWorktrees`
  sweeps stale scratch entries past maxAge.
- `TestScratch_EnvExported`: env builder output contains
  `HYDRA_SCRATCH_DIR=<scratch path>`; worker scaffolding text mentions
  `HYDRA_SCRATCH_DIR` and forbids in-repo cache paths.
- The Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-002: Junk signature package (content signatures + negative probes)

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/junksig/junksig.go`, `internal/junksig/junksig_test.go`
**Test Command:** `go test ./internal/junksig/ -run 'TestJunksig_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

Pure, stdlib-only package: `IsJunkPath(p string) bool` matching **structural
signatures, not substrings**: any path segment `.tmp*` (`.tmp`,
`.tmp-jest`, `.tmp2` …), directory `.jest-cache`, basenames
`haste-map-*`, `jest-transform-cache-*`, files `.tmp-*`, and exactly
`.hydra-preflight-ok`, `.pi/task-breakdown/**`. `IgnorePatterns()` returns
the equivalent `.gitignore` lines. **Negative probes are load-bearing**:
`src/modules/agent/services/conversation/thread-cache.service.ts` and its
spec (real files in the live repo) must NOT match; neither may any
`*cache*`-named source file under `src/`. Patterns survive directory renames
of the same content (`foo/.jest-cache/x` matches wherever it sits). Update
`CHANGELOG.md` (Added). Test functions are named `TestJunksig_*`.

**Acceptance Criteria:**
- Table test: every observed real-world junk path from the incident matches
  (`src/src/.tmp-jest/r3/cache/haste-map-…`,
  `src/.jest-cache/0a/…`, `.tmp-cov-full.txt`, `.tmp-full-retry.sh`,
  `.tmp2/jest/…`, `.hydra-preflight-ok`, `.pi/task-breakdown/cli-x/log.jsonl`).
- Negative table: `thread-cache.service.ts`, `thread-cache.service.spec.ts`,
  `src/cache/redis.module.ts`, `src/modules/x/data-cache.service.ts` do NOT
  match.
- Renamed-parent test: `renamed/.jest-cache/blob` and `renamed/.tmp-jest/b`
  still match.
- `IgnorePatterns()` lines exactly reproduce the matcher (property test in
  a hermetic repo: `t.TempDir` + `git init` + write `IgnorePatterns()` to
  `.gitignore` + probe fixture paths with `git check-ignore`).
- Accepted residual, recorded here: jest's default `coverage/` output dir is
  deliberately NOT a signature (some repos commit lcov/coverage artifacts);
  coverage routing is handled by scratch guidance, not the gate.
- The Test Command output lists ≥8 `--- PASS` lines and no `[no tests to
  run]`; `go build ./...` exits 0.

### FR-003: Close-time junk gate — auto-drop once, then distinct hard reject

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/gate_junk.go`, `internal/core/gate_junk_test.go`, `internal/core/bead_closer_hard_gates.go`, `docs/architecture/shepherd.md`
**Test Command:** `go test ./internal/core/ -run 'TestGateJunk_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-002

New `gateJunkArtifacts(bead, projectPath, repoName, worktreePath) error`
wired as one call in `runCloseHardGates` (`bead_closer_hard_gates.go:30`,
one line only). Changed-set = committed `base...HEAD` unioned with staged
(`git diff --cached --name-only`), mirroring `collectCloseEvidence`
(`bead_closer.go:1511+`) semantics, with base = `ResolveIntegrationBase(cfg,
repoPath)` (`integration_base.go:24`) — the same resolver the verified-close
diff gates use. For each `junksig.IsJunkPath` hit: **staged-only** paths
(absent from `base...HEAD`) are unstaged via `git restore --staged --` with
**no commit** (a removal commit would die on a clean tree); **committed**
paths get `git rm --cached --` + one removal commit; then re-check. If junk
persists after the drop pass → reject with reason `junk-artifacts` (a **distinct label**
`junk-artifacts` — never overloaded onto `off-scope`, so the existing
off-scope warn ladder and escalation semantics stay untouched), surfaced as
a fixable condition (remove files, retry). Non-junk off-scope behavior is
unchanged (warn-only). The ignore-injection from FR-001 makes re-staging
impossible in new worktrees; the rm-once path covers legacy/staged files.
Update the shepherd.md gate table + `CHANGELOG.md` (Added). Test functions
are named `TestGateJunk_*`.

**Acceptance Criteria:**
- Test: worktree with committed `.tmp-jest/x` + real file → gate drops the
  junk path (removal commit exists, file gone from index, still on disk),
  re-check clean → gate passes, bead proceeds.
- Test: junk re-added on retry after two drops → rejection reason exactly
  `junk-artifacts`, label `junk-artifacts` present, `escalated` untouched.
- Test: `thread-cache.service.ts` modified in the chain → gate untouched
  (no false positive).
- Test: non-junk off-scope file (e.g. `README.md` edit on a scoped bead) →
  behavior identical to today (warn path, no rejection).
- The Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-004: Publish-path junk guard — fail-closed with a bounded, visible exit

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/publisher.go`, `internal/core/publisher_junk_test.go`, `internal/core/overseer.go`
**Test Command:** `go test ./internal/core/ -run 'TestPublisherJunk_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-002

Before any push: publish-base = `originDefaultBranch(projectPath)`
(`publisher.go:92-107`; on resolver error skip the guard, fail-open),
publish-head = the working branch; `git diff --name-only` between them → any
`junksig.IsJunkPath` hit → **do not push** and **never call
`notePublishBackoff` for junk blocks** (that loop, `overseer.go:1232-1258`,
is uncapped); emit event `publish_blocked_junk` (fields: project, junk paths
capped at 50, count). The junk attempt counter lives in `Overseer` beside
`publishBackoff` (keyed projectPath, **reset when the branch tip changes**);
after **3** blocked attempts stop retrying (mirror `fix-recursion-capped`)
and file a Pan human-triage bead via the existing `filePanBead` helper
(`overseer.go:359`, which also dedups) whose description embeds the junk
path summary (decision-packet payload if PRD-6b's packets are present on the
branch; otherwise the plain bead). The guard runs on the overseer side or is
injected at `PublishRepo`'s push choke (`PushBranchWithToken`, `:238`), with
counters/filing staying in `Overseer` (package-level `PublishRepo` has no
`*Overseer`). Update shepherd.md
(publish path + event) and `CHANGELOG.md` (Added). Test functions are named
`TestPublisherJunk_*`.

**Acceptance Criteria:**
- Test: branch whose diff contains one junk path → no push attempted, one
  `publish_blocked_junk` event with that path, attempt labeled
  `publish-junk-1`.
- Test: third consecutive block → no further retries (backoff loop
  exited), a triage bead exists whose description contains the junk summary.
- Test: branch containing `thread-cache.service.ts` changes → pushes
  normally (negative probe at the publisher too).
- Test: after junk is removed, the next publish attempt proceeds with no
  lingering label effects.
- The Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

## Success Metrics

- New campaign on any product repo: publish-branch diff contains zero
  `junksig` hits; worktrees show injected ignore lines; scratch siblings
  pruned within one removal tick.
- The srv-data-harbour incident class (junk in PR) cannot reproduce: close
  gate rejects before integrate; publisher blocks before remote.
- Zero false positives on the live `thread-cache.service.*` files (probes in
  CI forever).

## Out of Scope

- Remediation of already-pushed junk (regenerate clean publish branch,
  purge `origin/feat` pre-existing junk, preserve the d3z spec, close 14
  stale PRs) — product-repo fix bead, filed separately.
- Git history rewrite / repo shrinking of remote branches.
- Enforcing scope generally (target-file block mode) — only the junk
  signature class is fail-closed here.
- Sequencing note: PRD-7 FR-002 adds one call line to
  `runCloseHardGates` as well; land whichever chain is beaded first, the
  second rebases its single call line (noted to avoid merge surprises).
