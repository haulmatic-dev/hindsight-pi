package main

import (
	"errors"
	"testing"
)

// TestIsTransientStartupFailure verifies the classifier only treats no-token
// Pi startup flakes (provider-list fetch) as transient — not real errors.
func TestIsTransientStartupFailure(t *testing.T) {
	cases := []struct {
		name   string
		result *ExecutionResult
		err    error
		want   bool
	}{
		{"unknown provider in result.Error", &ExecutionResult{Error: `Error: Unknown provider "kimi-coding-plan".`}, nil, true},
		{"list-models hint in err", nil, errors.New("pi failed: Use --list-models to see available providers"), true},
		{"unknown provider in output", &ExecutionResult{Output: `Error: Unknown provider "minimax"`}, nil, true},
		{"real compile error", &ExecutionResult{Error: "undefined: Foo"}, nil, false},
		{"rate limit mid-run (not startup)", &ExecutionResult{Error: "429 rate_limit_error"}, nil, false},
		{"nil/nil", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientStartupFailure(tc.result, tc.err); got != tc.want {
				t.Errorf("isTransientStartupFailure = %v, want %v", got, tc.want)
			}
		})
	}
}
