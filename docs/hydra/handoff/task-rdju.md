# discovery handoff (rdju)

## What changed (files + one line each)
- docs/discovery/pr-create-nodiff-triage-rdju.md: triage of the deterministic gh pr create no-diff failure — root cause evolved from "unpushed stray duplicate branch" to "publisher mirrors head commits into dogfood/publish-hydra before PR creation, making head→publish-hydra structurally zero-diff"; strand landed via base-corrected PR #147.
- CHANGELOG.md: Documentation entry pointing at the triage.
- Live-store actions: pseudo-bead hydra-ybhh reopened after a stale close, then closed with the true disposition (landed via PR #147).
- Repo actions (main checkout, verified): in-flight strand committed as ae68584 mid-triage, preserved; PR #147 admin-merged; remote refs dogfood/orphan-digest-escalated + dogfood/spawn-cohort-recovery deleted; local stray branch deleted after checkout repoint.

## Verification (commands run + pass/fail)
- Read 1: `git ls-remote origin` — no orphan-digest ref; local tip aab8d18 identical to origin/publish-hydra (`rev-list --left-right --count` = `0 0`). PASS.
- Read 2 (08:50Z): origin orphan-digest-escalated = ae68584 (recover-spawn-cohort strand, authored 08:48Z); publish-hydra advanced to the same commit → head==base. PASS.
- `gh pr create --head dogfood/orphan-digest-escalated --base development` → "already exists: PR #147". PASS (precedent e2xc).
- `gh pr merge 147 --merge --admin` → MERGED 2026-10-02T08:52:06Z, mergeCommit 174da6a; `git ls-remote origin development` = 174da6a; `git log origin/development -3` shows ae68584 present. PASS.
- `git push origin --delete dogfood/orphan-digest-escalated dogfood/spawn-cohort-recovery` → both deleted; transient `dogfood/spawn-cohort-strand` deleted earlier. PASS.
- Main checkout: `git checkout development` ok after strand committed; `git branch -D dogfood/orphan-digest-escalated` → deleted (previously blocked: branch checked out). `git status --short` clean vs its HEAD. PASS.
- hindsight_search unavailable ("Hindsight is not connected") — skipped silently per protocol.

## Open edges for the next phase
- Publisher hardening candidate (not implemented, single-occurrence evidence): do not mirror head commits into dogfood/publish-hydra before PR creation, or make PR creation target the integration base (development) when head is a descendant/child of the vehicle branch.
- Local development ref (main checkout, tip 939b88d) is ff-diverged from origin/development tip (174da6a) by the daemon's parallel integration lines — the publisher owns that ref; left untouched.
