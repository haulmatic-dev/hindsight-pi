package core

// RED-phase inert stub for the Node dependency preflight close gate
// (companion to the executable-acceptance hard gate, FR-001 chain).
//
// GREEN must implement the behavior pinned by gate_deps_preflight_test.go:
//
//   - a package manifest (package.json) present with no node_modules
//     directory: run the project's package install command in the worktree
//     BEFORE the verification command;
//   - no manifest: no install at all — the verification command runs
//     directly;
//   - node_modules already present: no install;
//   - an install that leaves the worktree dirty (tree-clean guard, e.g. a
//     non-empty `git status --porcelain`) is a failure: the verification
//     command must NOT run and an error must be returned;
//   - the verification command's output is returned verbatim (--- PASS lines
//     and all, nothing swallowed).
//
// The one-line call site inside gateExecutableAcceptance belongs to the
// feature requirement's edit budget, not to this RED bead.

// gateDepsEnforceEnv turns the RED contract's loud skips into failures so the
// missing feature is provable: `GATE_DEPS_ENFORCE=1 go test ...`.
const gateDepsEnforceEnv = "GATE_DEPS_ENFORCE"

// depsPreflightImplemented is flipped to true by the GREEN phase; the
// contract tests skip loudly while it is false.
const depsPreflightImplemented = false

// gateDepsPreflight ensures Node dependencies are installed in worktreePath
// when needed (see the contract above), guards that the worktree stays clean
// after any install, then runs command via run and returns its output
// verbatim.
//
// RED phase: inert — runs nothing, always allows.
func gateDepsPreflight(worktreePath string, command string, run func(dir, command string) (string, error)) (string, error) {
	return "", nil
}
