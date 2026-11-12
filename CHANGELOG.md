# Changelog

## [Unreleased]

### Changed
- Every retain now carries the shared memory tag taxonomy (hydra
  `docs/operations/memory-tag-taxonomy.md`): `plane:experience`, `kind:`,
  `tier:claimed`, `source:` and `project:<repo>`. Auto retains are
  `kind:transcript` (was `kind:turn-summary`); explicit saves are
  `kind:lesson` (was `kind:explicit`). The explicit `hindsight_retain` tool
  now also adds `constantTags` and `project:`, which it never did. The source
  is `source:pi` unless `constantTags` names one (Hydra workers set
  `source:hydra-worker`). `origin:` is kept so older recall filters still
  match.

### Fixed
- Session-boundary retains no longer block on server-side LLM extraction:
  `flushQueuedSession` now submits `retainBatch` with `async: true` (fire and
  durably queue server-side) instead of `{ async: false }`, which parked
  worker session shutdown/compaction on the extraction-LLM slot for up to the
  300s retain timeout whenever the provider degraded. Same-box durability is
  unchanged: the server creates the idempotent operation row on ACK, and both
  the local queue file and the server live on this host.
