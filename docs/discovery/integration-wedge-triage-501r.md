# Triage: ajantha-srv-telematics fleet-wide integration-exhausted hard-parks (shared wedge)

Date: 2026-09-29 · Bead: hydra-501r · Type: discovery/triage — **no code change, no live-store repair**

## Conclusion

The `integration-exhausted` hard-parks across the ajantha-srv-telematics fleet are **durable by design and should stand**. The causal root — integration exhaustion (push path blocked: branch protection, credentials, or upstream divergence) manifesting simultaneously across many sibling beads — is **not yet fixed**, so per-bead repair (`repair:clear-hard-park`) is contraindicated: it would only re-queue beads into the same failing integration path and burn retries until they re-park.

## Evidence

Historical record (Hindsight durable memory, 2026-09-21 → 2026-09-22):

1. **Fleet-wide simultaneous parking, not isolated failure.** Multiple sibling beads (`ykz`, `rvm`, `tub`, `so0`, `6sf`, plus the earlier 12–13-bead `doz`/`dgv` retry-exhaustion wedge) hard-parked with the same `integration-exhausted` label within hours. A shared causal root was explicitly confirmed: "integration path blocked by branch protection, permissions, or upstream state; requires human-only repair; shared causal root confirmed."
2. **Durable-park semantics are intentional.** Repeated agent judgments recorded: "hard-park is durable by design and should NOT be cleared"; "repair:clear-hard-park not appropriate without fixing root cause"; "integration-exhausted requires integration wedge to be cleared before park can be cleared — this is a human/repo operation."
3. **Prior shared-wedge repairs worked only when the cause was actually fixed.** `repair:prune-worktree-registration` cleared the 12–13-bead worktree-registration wedge (2026-09-21) because that repair addressed the root cause (phantom worktree registrations). No analogous root-cause fix exists for the integration-exhaustion wedge — the push path itself is blocked.
4. **Where parks were cleared without a cause fix, they re-parked.** `ykz` park cleared 2026-09-21 → re-hard-parked the same day; `dgv` cleared after 2m11s → wedge re-observed. Clearing without cause resolution is counterproductive.
5. **Existing systemic mitigations already landed upstream** (hydra development): `integration_retest` stage (time-boxed retries before exhaustion), `publishWithRebaseRetry` (conflict → rebase → retry, cap 2), `ResolveIntegrationBase` (fail-closed base resolution), and Tier B agentic conflict resolution remains unimplemented (separate bead). None of these change the disposition for the already-parked fleet: the underlying push-path blockage is environmental (human-only).

## Disposition

- **Park stands.** No `clear-hard-park`, no label removal, no rebase, no per-bead repair.
- **Human triage required** on the ajantha-srv-telematics repo integration path: verify remote credentials/permissions, branch-protection rules, and upstream divergence before any park clears.
- **Operator lever available:** the FR-033 operator API (`POST /api/v1/projects/:name/unpark`, quarantine clear endpoints) exists for the human once the integration path is confirmed healthy.
- **Follow-up (separate bead):** Tier B agentic conflict resolution would convert some future integration conflicts from hard-park to self-heal.
