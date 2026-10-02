# Triage: orphanclaim digest grace (hydra-qmf7, 2026-10-02)

## Claim
Pan filed hydra-ctix / hydra-qmf7 / hydra-t2e1 / hydra-yz8z (all `hydra-campaign-anomaly` sources, 02:35–02:47 UTC) claiming "N in_progress beads with no live registry worker (orphanclaims=N)" and "host disk=99%".

## Verification — both claims FALSE

**Orphans:** every in_progress hydra bead had a live registry worker. Registry rows (sqlite, `$HYDRA_HOME/.hydra/registry.db`) showed `state=working`, heartbeats seconds old, PIDs confirmed running in `ps`. Charon scan every minute 02:38–02:55: `orphans_found=0, stuck_found=0, killed=0`.

**Disk:** `df -h /` = 185G/451G = **43%**. Watermark 85% not breached. The digest's `disk=99%` reading appears in the 02:4x prompt snapshot only; not reproducible against the live filesystem. /tmp churn (44G) can cause transient spikes.

## Root cause of the filings: self-perpetuating digest loop
The digest `OrphanClaims` counter (upstream bb75c4a) has no grace window. During a filing wave, a bead is claimed (status → in_progress) seconds before its worker registers in the registry. A digest computed in that window counts the claim as orphaned → Pan files a campaign-anomaly bead → that bead is itself claimed → counted orphaned → re-file. Chain evidence (pan_bead_filed events): ctix 02:35:38 → qmf7 02:41:48 → t2e1 02:43:32 → yz8z 02:47:38.

## Fix
`internal/core/pan_health.go` skips claims younger than `orphanClaimDigestGrace` (5m), mirroring Charon's `orphanBeadGrace` used before any destructive reopen. Test: `TestCollectHealthDigestOrphanClaims_FreshClaimGrace`.

## Host hygiene
Deleted 1,017 stale `/tmp/skills-*` dirs (>1 day, ~26GB). Disk 44% → 38%. Ongoing churn (~1,650 dirs in 2 days) needs a worker-side skills-install reuse fix — separate bead.
