# discovery handoff

## What changed (files + one line each)
- docs/discovery/beads-mirror-audit-discovery-s8f0.md: discovery doc — verified RED stub state, resolved 6 ambiguities (br authoritative vs jsonl mirror; br list --json single-call; missing/malformed as drift; exit-2 precedence; standalone CLI; Pan wiring out of scope), AC mapping table, GREEN open edges.
- CHANGELOG.md: Added entry under Unreleased pointing at the discovery doc.

## Verification (commands run + pass/fail)
- `go test ./tools/beads-mirror-audit/ -count=1` — PASS (RED suite green pre-GREEN; behavior tests skip loudly).
- Red stub confirmed inert: `auditImplemented = false`, runAudit returns exitUnavail.
- graphify query --budget 2000 used for code discovery; graph.json present in cwd.

## Open edges for the next phase
- GREEN: implement runAudit + real brRunner (exec.Command("br", "list", "--json"), cmd.Dir = storeDir), flip auditImplemented = true, keep exported shapes byte-identical to stub.

HYDRA_VERDICT: {"done": true, "ac_met": ["discovery doc written", "CHANGELOG entry", "handoff"], "ac_unmet": [], "files_changed": ["docs/discovery/beads-mirror-audit-discovery-s8f0.md", "CHANGELOG.md", "docs/hydra/handoff/task-s8f0.md"], "verification": [{"cmd": "go test ./tools/beads-mirror-audit/ -count=1", "result": "PASS"}]}
