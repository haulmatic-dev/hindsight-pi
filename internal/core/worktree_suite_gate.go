package core

import "errors"

// ErrQuarantineSuiteGateBlocked is the typed error wrapped into the error
// chain when the full-suite gate fails on an integrate. The quarantine
// reconciler matches it with errors.Is (never string sniffing) to emit the
// quarantine_suite_gate_blocked event and keep the quarantine. GREEN for
// this chain wires the gate itself into IntegrateBranch on the
// integrate-scratch-<key> post-merge worktree.
var ErrQuarantineSuiteGateBlocked = errors.New("quarantine suite gate blocked")
