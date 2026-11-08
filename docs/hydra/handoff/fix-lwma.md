# fix handoff

## What changed (files + one line each)
- internal/core/config.go: `PanConfig.MaxFiledBeadsPerProject` (yaml `shepherd.pan.max_filed_beads_per_project`) + `DefaultPanFiledBeadsPerProject = 4` default constant.
- internal/pan/pan.go: `MaxOpenFiledBeadsPerProject = 2` reclassified as the nil-safe legacy fallback (doc comment only; behavior via the knob now).
- internal/core/pan_health.go: `panFiledBeadsPerProject()` accessor (knob > 0 → knob, else default 4); `filePanBead` uses it, emits typed `pan_filed_cap_hit` event (fields `open`/`cap`) + log at cap hit, clears the marker on successful filing; `collectHealthDigest` adds stable `pan-filed cap saturated` anomaly for marked paths.
- internal/core/overseer.go: `panCapHits sync.Map` field (projectPath → cap-hit marker, cleared on successful filing).
- internal/core/pan_filer_test.go: existing cap test updated to effective cap + varied titles (dedupe vs new cap); new `TestFilePanBead_ConfigKnobCap` (knob honored, refusal + event + digest anomaly, marker cleared on success) and `TestFilePanBead_DefaultCapFour` (nil config → 4).
- CHANGELOG.md, docs/architecture/shepherd.md, hydra-config.yaml.example: operator docs.

## Verification (commands run + pass/fail)
- `go build ./internal/core/ ./internal/pan/` — PASS.
- `go vet ./internal/core/ ./internal/pan/` — PASS (clean).
- `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run TestFilePanBead -count=1` — PASS (5 tests incl. 2 new).
- `go test ./internal/pan/ -count=1` — PASS.
- Repro of defect on inherited worktree (before fix): `grep MaxOpenFiledBeadsPerProject internal/pan/pan.go` → `const ... = 2` hard-coded; knob absent from origin/development (`git grep max_filled... origin/development` — 0 hits). After fix: cap configurable, default 4.

## Open edges for the next phase
- Knob is read live from `o.config` (no hot-reload re-init needed since accessor reads each call).
- `pan_filed_cap_hit` is emitted per refused filing (not rate-limited) — a storm could emit many; digest wording is stable so no judgment churn.
- Pan repair verbs do not yet offer "close oldest pan-filed bead" — a saturated project needs a human close or an existing dedupe-free path; consider a Pan verb if storms recur.
