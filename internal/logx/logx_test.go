package logx

import "testing"

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"debug": LevelDebug, "info": LevelInfo, "INFO": LevelInfo,
		"warn": LevelWarn, "warning": LevelWarn, "error": LevelError,
		"": LevelInfo, "garbage": LevelInfo, "  Debug ": LevelDebug,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestEnabled(t *testing.T) {
	SetLevel(LevelWarn)
	defer SetLevel(LevelInfo)

	if Enabled(LevelDebug) || Enabled(LevelInfo) {
		t.Error("debug/info must be suppressed at warn threshold")
	}
	if !Enabled(LevelWarn) || !Enabled(LevelError) {
		t.Error("warn/error must be enabled at warn threshold")
	}
}
