// Package integration contains end-to-end integration tests for Hydra.
//
// These tests require a full environment (Pi, br, bv, LSP tooling, user
// projects) and are gated behind the `integration` build tag so they do not
// run as part of the default `go test ./...` suite.
package integration
