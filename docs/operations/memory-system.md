# Memory System (Hindsight) — Architecture & Operations

**Status:** current as of 2026-11-08 · **Owner:** platform ops · **Canonical config:** `infra-ai-dev` repo, `services/hindsight/`

This document is the hydra-side reference for the shared memory system: what runs, where knowledge comes from, how quality is maintained, and where the runbooks live. Server-side operational detail (bank config, systemd, burn-kill procedure) lives in the infra repo and is linked, not duplicated.

---

## 1. What is running

| Component | State | Notes |
|---|---|---|
| `hindsight-api` 0.10.1 (`hindsight.service`, port 8888) | live | Postgres `hindsight` DB (~29 GB, single copy — backup still pending) |
| Bank **`coding-brain`** | **the** memory bank | ~612K units / ~29K docs. All consumers read/write it |
| Pi extension (`haulmatic-dev/hindsight-pi` @ main) | live for workers + manual pi | timeout fail-open guards, async flush (session boundaries never block on extraction) |
| Daemon pre-query (`internal/hindsight/client.go`) | live | recall per bead (concurrent with spawn), `prefer_observations=true`, `max_tokens=800`, budget low |
| Mental models (6) | live, auto-refreshing | injected into worker prompts (≤4K chars, list→detail stitch) |
| **Pipelines** (infra repo): `cass-sync` (nightly 02:30), `hs-ingest` (on demand) | live | see §3 |
| **Golden queries** (monthly, 1st, 03:00) | live | 20 probes + 3 dup sentinels; exit-code contract |

Legacy banks (`hydra` — the old split-brain daemon bank, merged then deleted 2026-11-08; `hermes`/`pi`/`default` — stale debris, left in place) are gone or dormant. Nothing writes them.

## 2. Architecture decisions (and why)

**Single bank.** No privacy boundaries exist internally, so everything shares one entity graph — incidents link to verdicts link to patterns across daemon memory, pi sessions, and CASS rules. Multiple banks would re-create the cross-blindness that was this system's biggest defect (see §6 history).

**One taxonomy, enforced at write time.** Facts carry tags: `domain:product|execution`, `service:<x>`, `type:endpoint|contract|runbook|pattern|incident`, `project:<x>`, `source:cass|ingest|pi`. Tag-scoped recall (`tags_match`) and knowledge pages filter on these. Untagged knowledge is invisible to strict scopes — pipelines attach tags mechanically; humans/pi use the ingest paths.

**Derived ≠ stored.** Consolidation folds raw facts into observations (dedup + synthesis); `prefer_observations` serves the folded view. Stale facts are superseded by newer corrective facts (recency), not deleted.

## 3. Knowledge pipelines (infra-ai-dev, `services/hindsight/pipelines/`)

All pipelines import via the **transfer-archive path** with a **stable document id + `document_conflict=replace`**: the previous version's facts are invalidated automatically — deletions/edits propagate, no orphan tombstones. Digest state files skip unchanged runs.

| Pipeline | Direction | Cadence | Content |
|---|---|---|---|
| `cass-sync` | CASS playbook → bank | nightly 02:30 | curated rules as tagged facts (deduped — CASS itself was 40% duplicated until `cass-dedupe` ran) |
| `hs-ingest` | file → bank | on demand (`hs-ingest <file> --service X`) | verbatim chunks, taxonomy tags; NOT `files/retain` (that path produces untagged docs) |
| *(planned)* extractor | graphify + OpenAPI → bank | CI | service connectivity/contract facts — the remaining Phase-2 build |

Known importer traps (learned the hard way, all fixed in the pipeline code): metadata must be string-valued; every chunk needs a unique `chunk_index`; per-fact DELETE does not exist (curation is `PATCH state=invalidated`).

## 4. Quality maintenance

- **Consolidation**: auto, batch 32 / round 500 (config-as-code, see §5). The 2026-11-04/05 backlog burn drained 45.8K raws with zero LLM failures; dup clusters collapsed ~20×.
- **Golden queries** (`infra services/hindsight/golden/`): 20 known-answer probes + 3 dup sentinels (baseline: hung-workers 2, four-actions 1, coding-brain 0; metric is signature-normalized — template-sharing incident facts are NOT dupes). A FAIL means a coverage gap: retain the missing fact, never weaken the probe.
- **Judgment dedupe** (manual, as needed): classify true restatements vs distinct incidents first; invalidate restatements via `PATCH {"state":"invalidated"}` (reversible, archived).

## 5. Configuration & runbooks

- **Bank config as code**: `infra services/hindsight/bank-config.json` + `apply-bank-config.sh` (idempotent diff→PATCH→verify). Bank config is process-cached — **restart the service to apply**.
- **Burn kill procedure** (config flags CANNOT stop an active consolidation chain — rounds re-queue themselves): infra `services/hindsight/README.md`.
- **Big-statement Postgres protocol** (zombie backends, chunked autocommit deletes): infra README + CASS pattern `verification`.
- **GitHub credential map**: hydra-operator skill §3 (rewritten 2026-11-05) + `host-setup.md` §11.

## 6. History (the short version)

| Date | Event |
|---|---|
| ≤ 2026-11-03 | **Split-brain era**: daemon/workers wrote bank `hydra` (hard-coded URL literals) while pi sessions wrote `coding-brain` — two cross-blind memory systems, months of accumulated blindness |
| 2026-11-04 | Literals fixed (`fix/hindsight-bank-id`, PR #464); hydra bank merged into coding-brain (4,112 docs via chunked doc-scoped transfer — whole-bank export times out on op-history tables); backlog burn started, tuned (batch 32/round 500, 109K already-clean imports marked consolidated) |
| 2026-11-05 | Burn self-terminated (45.8K drained, 0 failures); `prefer_observations` flipped; worker efficiency landed (first-turn recall, mental-model injection, async flush — PR #464 + hindsight-pi #6); PAT unblocked, all work pushed+merged; golden-query harness born (found+filled 2 coverage gaps); skill §3 credential map fixed |
| 2026-11-08 | 1.4 judgment dedupe (23 true restatements invalidated; dup metric fixed to signature-based); stray-bank audit; old `hydra` bank deleted (chunked SQL, zombie-backend saga); cass-sync + hs-ingest + cass-dedupe landed (CASS 2,657→1,591) |

## 7. Open work

1. **Extractor** (Phase-2 #3): graphify `graph.json` + OpenAPI → connectivity facts. Import path proven twice; needs fact templates + service catalog + CI wiring.
2. **Supersession pass** (Phase-1.3 remainder): corrective facts for the broader fixed-bug set — stale-but-unique facts still serve as current (e.g. the duplicate-worker history pre-fix).
3. Pan-cluster dedupe (×6 "Pan is a shepherd…") — same 1.4 procedure.
4. **Remote access**: API key + Tailscale/proxy for the team; extension is hardened, exposure is the remaining work.
5. **GCS backup** (nightly `pg_dump` + restic plan approved) — **the 29 GB DB is still single-copy**; blocked on bucket + service-account key.
