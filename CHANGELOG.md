# Changelog

## [Unreleased]

### Fixed
- Session-boundary retains no longer block on server-side LLM extraction:
  `flushQueuedSession` now submits `retainBatch` with `async: true` (fire and
  durably queue server-side) instead of `{ async: false }`, which parked
  worker session shutdown/compaction on the extraction-LLM slot for up to the
  300s retain timeout whenever the provider degraded. Same-box durability is
  unchanged: the server creates the idempotent operation row on ACK, and both
  the local queue file and the server live on this host.
