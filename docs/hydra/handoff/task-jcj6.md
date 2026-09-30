# GREEN handoff

## What changed (files + one line each)
- internal/pan/pan.go: new exported `ErrFileBeadDuplicate` sentinel + `NormalizeReason` wrapper; `handleFileBead` now treats a filer `ErrFileBeadDuplicate` as a benign skip ("skipped"/"duplicate signature" event, signature retained) instead of a filer error.
- internal/core/pan_health.go: `filePanBead` dedupes against open pan-filed beads by normalized title before create — persistent cross-session dedupe (the session-level `filedSigs` map is lost on daemon restart, which is how `hydra-jcj6` "(dedupe skip)" got filed as a twin of `hydra-mhk2`'s finding).
- internal/core/pan_filer_test.go: `TestFilePanBead_DuplicateOpenTitleSkipped` — volatile-title variant returns ErrFileBeadDuplicate, different title still files.
- internal/pan/file_bead_test.go: `TestFileBeadDuplicateFilerResultSkips` — sentinel → one filer call, repeat skipped via retained signature.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/pan/ ./internal/core/` — PASS (clean).
- `go test ./internal/pan/ -run TestFileBead -count=1` — PASS.
- `go test ./internal/core/ -run TestFilePanBead -count=1` — PASS.
- `go test ./internal/pan/ ./internal/core/ -count=1` — PASS (2/2 packages).

## Open edges for the next phase
- Root artifact: the Pan judgment that produced `hydra-jcj6` emitted a degenerate payload ("(dedupe skip)" as the whole title). Persistent dedupe prevents duplicates of a real finding, but a degenerate/placeholder title like "(dedupe skip)" should ideally be refused at filing time (empty-after-normalize or marker-only title). Not implemented here — needs a decision on the valid-title contract.
- Live-store disposition: `hydra-jcj6` (pan-filed, no description, title "(dedupe skip)") is a junk artifact of `hydra-mhk2` (event `pan_bead_filed` fields.source=hydra-mhk2) — candidate for close as duplicate once its gate cycle completes; `hydra-mhk2`'s rebase-conflict wedge remains human-only.
