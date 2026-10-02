package core

import "strings"

// Publish-path policy (FR-010 follow-up, operator policy 2026-09-29):
// push-first with a classified fallback. A failed push is classified into
// one of four outcomes and routed by a pure decision function:
//
//	PROTECTED → open a PR (idempotent: gh pr list --head before create)
//	STALE     → rebase onto the base and retry (capped, never force-push)
//	TRANSIENT → local commit + loud event + retry queue (no silent
//	            unpublished accumulation — the 16h wedge lesson)
//
// Ownership gate: a push that lands on a hydra-owned branch (hydra/,
// dogfood/, or configured-writable prefixes) counts as published; anything
// else is surfaced loudly rather than silently accepted.

// PublishPathClass is the classification of a git push failure.
type PublishPathClass string

const (
	// ClassAccepted means the push succeeded (no failure text).
	ClassAccepted PublishPathClass = "ACCEPTED"
	// ClassProtected means the remote refused the push via branch
	// protection / repository rules (GH013, GH006, protected branch,
	// required reviews). Permanent by definition — retrying the direct
	// push can never succeed; the fallback is a PR.
	ClassProtected PublishPathClass = "PROTECTED"
	// ClassStale means the remote diverged from local (non-fast-forward,
	// "fetch first"). Recoverable by rebase/merge + retry.
	ClassStale PublishPathClass = "STALE"
	// ClassTransient means network/auth flake (DNS, timeout, refused,
	// auth failure). Recoverable by backoff; publish locally + retry queue.
	ClassTransient PublishPathClass = "TRANSIENT"
	// ClassNotFound means the remote URL no longer resolves to an
	// accessible repository (renamed/moved/deleted, or the push credential
	// lacks visibility). Not transient — retrying against the same URL
	// can never succeed; publishing stays parked local + loud until a
	// human repairs the remote or the credential. Observed fleet-wide
	// 2026-10-01: 2,371 "Repository not found" failures over 8h while a
	// repo transfer flapped between orgs (hydra-4yuw).
	ClassNotFound PublishPathClass = "NOT_FOUND"
	// ClassCredential means the push credential itself is invalid or
	// expired ("Invalid username or token", "Password authentication is
	// not supported"). Not transient — retrying with the same token
	// can never succeed; a human must rotate the PAT. Observed 903
	// consecutive attempts in 12h on srv-telematics (2026-10-02) because
	// a stale token was classified TRANSIENT and retried every publish
	// tick with no backoff.
	ClassCredential PublishPathClass = "CREDENTIAL"
	// ClassUnknown means unrecognized failure text. Fail closed: treat
	// like transient (park local + loud) until a human classifies it.
	ClassUnknown PublishPathClass = "UNKNOWN"
)

// ClassifyPushFailure maps git push stderr (verbatim) to a PublishPathClass.
// Pure function: no network, no git, table-driven tested. Order matters —
// stale markers are checked before protected ones because GitHub can phrase
// a non-fast-forward as "remote rejected (non-fast-forward)".
func ClassifyPushFailure(stderr string) PublishPathClass {
	lower := strings.ToLower(stderr)
	switch {
	case strings.TrimSpace(stderr) == "":
		return ClassAccepted
	case hasAnySubstr(lower,
		"non-fast-forward", "fetch first", "[rejected]", "behind"):
		return ClassStale
	case hasAnySubstr(lower,
		"gh013", "gh006",
		"protected branch", "branch is protected",
		"pre-receive hook declined",
		"rule violations",
		"changes must be made through a pull request",
		"required review", "review required",
		"remote rejected"):
		return ClassProtected
	case hasAnySubstr(lower,
		"repository not found", "fatal: repository",
		"does not appear to be a git repository",
		"not a git repository"):
		return ClassNotFound
	case hasAnySubstr(lower,
		"invalid username or token",
		"password authentication is not supported",
		"authentication failed",
		"permission denied (publickey)"):
		return ClassCredential
	case hasAnySubstr(lower,
		"could not resolve host", "connection refused",
		"connection reset", "timed out", "timeout",
		"temporary failure", "network is unreachable",
		"ssh: connect", "tls", "early eof", "rpc failed",
		"terminal prompts disabled"):
		return ClassTransient
	default:
		return ClassUnknown
	}
}

func hasAnySubstr(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// IsHydraOwnedBranch reports whether a branch is hydra-owned: one of the
// built-in publish prefixes (hydra/, dogfood/) or a configured-writable
// prefix. A branch with no namespace prefix is never owned — pushes to
// trunk names like "main" or "development" must not silently count as
// published deliverables.
func IsHydraOwnedBranch(branch string, extraWritablePrefixes []string) bool {
	if branch == "" {
		return false
	}
	for _, p := range append([]string{"hydra/", "dogfood/"}, extraWritablePrefixes...) {
		if p != "" && strings.HasPrefix(branch, p) {
			return true
		}
	}
	return false
}

// PublishAction is the routing decision for a classified push result.
type PublishAction string

const (
	// ActPushDirect: the push was accepted onto a hydra-owned branch —
	// publish is done.
	ActPushDirect PublishAction = "PUSH_DIRECT"
	// ActCreatePR: push rejected as protected — create (idempotently) a
	// PR from a hydra-owned feature branch to the original branch.
	// Product repos stop here (human merges); tier-1 repos may auto-merge.
	ActCreatePR PublishAction = "CREATE_PR"
	// ActRebaseRetry: push rejected as stale — rebase onto the base and
	// retry (caller caps attempts at 2, never force-pushes).
	ActRebaseRetry PublishAction = "REBASE_RETRY"
	// ActParkLocalLoud: transient failure, or anything unrecognized —
	// keep the commit local, emit a loud typed event, and enter the retry
	// queue. Never silently accumulate unpublished work.
	ActParkLocalLoud PublishAction = "PARK_LOCAL_LOUD"
)

// ResolvePublishPath is the pure decision function mapping a classified
// push result plus the ownership gate to a publish action. tier1 marks a
// tier-1 repo (hydra itself): it only affects where CREATE_PR stops
// downstream (auto-merge allowed), not the routing here — protection and
// staleness route identically for both tiers.
func ResolvePublishPath(class PublishPathClass, owned, tier1 bool) PublishAction {
	switch class {
	case ClassAccepted:
		if owned {
			return ActPushDirect
		}
		// Pushed somewhere hydra does not own — loud, not silent.
		return ActParkLocalLoud
	case ClassProtected:
		return ActCreatePR
	case ClassStale:
		return ActRebaseRetry
	case ClassTransient, ClassUnknown, ClassNotFound, ClassCredential:
		// NOT_FOUND parks like TRANSIENT (local commit + loud event) but
		// carries its own class so the fleet can tell "credential/repo
		// access problem, human must fix" apart from "retry after backoff".
		return ActParkLocalLoud
	default:
		return ActParkLocalLoud
	}
}
