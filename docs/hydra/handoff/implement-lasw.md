# implement handoff

## What changed (files + one line each)
- internal/core/publish_path.go: new — `ClassifyPushFailure` (pure: push stderr → ACCEPTED/PROTECTED/STALE/TRANSIENT/UNKNOWN; STALE checked before PROTECTED since GitHub phrases divergence as "remote rejected (non-fast-forward)"), `IsHydraOwnedBranch` ownership gate (hydra/, dogfood/, configured extras; prefixless branches never owned), `ResolvePublishPath` decision fn (accepted+owned→PUSH_DIRECT; PROTECTED→CREATE_PR; STALE→REBASE_RETRY; TRANSIENT/UNKNOWN/unowned-accepted→PARK_LOCAL_LOUD).
- internal/core/publish_path_test.go: table-driven tests, no network (14 classifier cases, ownership matrix, 8-row decision table).
- internal/core/config.go: `output.writable_branch_prefixes []string` on OutputConfig + `Config.WritableBranchPrefixes()` accessor (nil-safe).
- internal/core/publisher.go: PublishRepo direct-push rejection path now classifies the failure and emits `publish_path_classified` event (class + action + verbatim reason).
- internal/core/bead_closer.go: close-path push failure also classified + `publish_path_classified` event before the fail-closed reopen (protected route to maybeOpenAutoPR unchanged, which is already idempotent: lists open PRs by head before create).
- CHANGELOG.md, docs/operations/host-setup.md: entries + knob row.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS
- `go vet ./internal/core/` — PASS
- `go test ./internal/core/ -run 'TestClassifyPushFailure|TestIsHydraOwnedBranch|TestResolvePublishPath' -count=1` — PASS
- `go test ./internal/core/ -count=1` — PASS (full package)

## Open edges for the next phase
- `writable_branch_prefixes` is threaded at the bead_closer call site only; PublishRepo's classifier call passes nil extras (no cfg in its signature) — built-in prefixes still apply. Thread cfg if per-project prefixes ever needed there.
- TRANSIENT backoff-x3/retry-queue is not a new mechanism: relies on existing bead reopen + scheduler retry; the classification event is the loud signal. A dedicated publish retry queue would be follow-up work.
- E2E telematics verification (parked on stale branch, PR to development) not run in this session — unit-level only.
