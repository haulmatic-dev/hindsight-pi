# implement handoff

## What changed (files + one line each)
- internal/pan/pan.go: `normalizeReason` extended — filesystem paths (`/home/...`, `~...`), bead-ID-like tokens (`hydra-ee5r`, `fmsfe-090`), and cardinal numbers (`\d+` → `#N`) are stripped in addition to the existing timestamps/durations/units, so wedge-filing titles that differ only by volatile identity noise share one signature. Used only for signatures/efficacy keys; the judgment prompt still sees raw reasons.
- internal/core/overseer.go: new `panSigMu`/`recentPanSigs` fields — per-project normalized-signature → filed-time map with mutex.
- internal/core/pan_health.go: `filePanBead` now (1) checks a 1h signature window (`panFileSigTTL`, package var, overridable in tests) before the open-bead dedupe and cap — a signature filed or deduped within the window returns `ErrFileBeadDuplicate` even after the earlier wedge bead was closed; (2) records the signature on successful create and on the open-title dedupe hit, never on filer/list errors (retry still works). Combined with `normalizeReason`, N blocked beads with one root cause produce exactly 1 wedge bead per project per window.
- internal/core/pan_filer_test.go: `TestFilePanBead_SignatureWindowDedupe` (count/bead-ID variant titles dedupe; dedupe survives closing the wedge bead; refiles after TTL expiry; genuinely different finding files immediately) + `TestFilePanBead_FilerErrorLeavesWindowOpen` (list error does not poison the window).
- internal/pan/file_bead_test.go: `TestFileBeadNormalizesBeadIDsPathsAndCounts` — three volatile variants (counts, paths, timestamps) file exactly 1 bead.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/pan/ ./internal/core/` — PASS.
- `go vet ./internal/pan/ ./internal/core/` — PASS.
- `gofmt -l` on touched files — clean (pre-existing drift in unrelated files untouched).
- `go test ./internal/pan/ -count=1` — ok.
- `go test ./internal/core/ -count=1` — ok (369.9s, full package).
- AC "verify MaxOpenFiledBeadsPerProject is set in /etc/hydra/hydra.yaml": NOT a yaml knob — it is a code constant (`internal/pan/pan.go:226`, value 2) enforced filer-side in `filePanBead`; no `max_open_filed` config key exists in /etc/hydra/hydra.yaml (pan block only has enabled/interval/hung/park-grace/nudge-cap/provider). Cap verified active by `TestFilePanBeadCapAndLabel`.

## Open edges for the next phase
- `normalizeReason` digit collapse also applies to repair-efficacy keys and `candidateSignature` (Pan repair limiter): repairs whose reasons differ only by counts/IDs now share a key — intended (fewer effective-key resets), but a repair reason that changes meaning purely via a number would be treated as unchanged (relapses to human-only sooner).
- Window TTL is in-memory: daemon restart resets it; persistent layer is the open-bead normalized-title dedupe (unchanged).
- Bead-ID regex is permissive (`[a-z][a-z0-9]{1,14}-[0-9a-z]{2,5}`) — over-normalization (e.g. "re-push-integration" → "#id-#id") only affects signatures, never prompt text.
