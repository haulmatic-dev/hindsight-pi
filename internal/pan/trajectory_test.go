package pan

import "testing"

func TestTrajectoryAnomaliesNone(t *testing.T) {
	tra := Trajectory{Turns: 10, TestRuns: 2, EditsSinceTest: 1, RepeatedCommand: "go build ./...", RepeatedCount: 3}
	if got := tra.Anomalies("green"); len(got) != 0 {
		t.Fatalf("healthy trajectory flagged: %v", got)
	}
}

func TestTrajectoryAnomaliesTurnOutlier(t *testing.T) {
	tra := Trajectory{Turns: 61} // > 2×30 (green)
	got := tra.Anomalies("green")
	if len(got) != 1 || !contains(got[0], "turn outlier") {
		t.Fatalf("want turn outlier, got %v", got)
	}
	// Same trajectory for a green-class baseline must not flag at 60 turns.
	tra.Turns = 60
	if got := tra.Anomalies("green"); len(got) != 0 {
		t.Fatalf("2x baseline must not flag: %v", got)
	}
}

func TestTrajectoryAnomaliesClassBaselines(t *testing.T) {
	// syntax baseline 5: 11 turns flags.
	if got := (Trajectory{Turns: 11}).Anomalies("syntax"); len(got) != 1 {
		t.Fatalf("syntax 11 turns should flag, got %v", got)
	}
	// fix baseline 10: 21 turns flags.
	if got := (Trajectory{Turns: 21}).Anomalies("fix"); len(got) != 1 {
		t.Fatalf("fix 21 turns should flag, got %v", got)
	}
	// unknown class uses default 40: 81 turns flags, 80 does not.
	if got := (Trajectory{Turns: 81}).Anomalies("mystery"); len(got) != 1 {
		t.Fatalf("mystery 81 turns should flag, got %v", got)
	}
	if got := (Trajectory{Turns: 80}).Anomalies("mystery"); len(got) != 0 {
		t.Fatalf("mystery 80 turns should not flag, got %v", got)
	}
}

func TestTrajectoryAnomaliesRepeatedCommand(t *testing.T) {
	tra := Trajectory{RepeatedCommand: "npm run build", RepeatedCount: 8}
	got := tra.Anomalies("default")
	if len(got) != 1 || !contains(got[0], "repeated 8 times") {
		t.Fatalf("want repeated-command loop, got %v", got)
	}
	tra.RepeatedCount = 7
	if got := tra.Anomalies("default"); len(got) != 0 {
		t.Fatalf("below floor must not flag: %v", got)
	}
}

func TestTrajectoryAnomaliesEditsWithoutTest(t *testing.T) {
	tra := Trajectory{EditsSinceTest: 6}
	got := tra.Anomalies("default")
	if len(got) != 1 || !contains(got[0], "re-editing without verifying") {
		t.Fatalf("want edits-without-test flag, got %v", got)
	}
}

func TestTrajectoryAnomaliesTestFixLoop(t *testing.T) {
	tra := Trajectory{TestRuns: 9}
	got := tra.Anomalies("default")
	if len(got) != 1 || !contains(got[0], "test-fix-test") {
		t.Fatalf("want test-fix-test flag, got %v", got)
	}
}

func TestTrajectoryAnomaliesNoNewFiles(t *testing.T) {
	// 20 turns, edits present, zero file-creating writes → flag.
	tra := Trajectory{Turns: 20, DistinctEdits: 3, EditsSinceTest: 1}
	got := tra.Anomalies("green")
	if len(got) != 1 || !stringsContains(got[0], "no new files") {
		t.Fatalf("want no-new-files flag, got %v", got)
	}
	// New files exist → no flag.
	tra.NewFiles = 2
	if got := tra.Anomalies("green"); len(got) != 0 {
		t.Fatalf("new files present must not flag: %v", got)
	}
	// Below floor turns → no flag.
	tra.Turns = 14
	tra.NewFiles = 0
	if got := tra.Anomalies("green"); len(got) != 0 {
		t.Fatalf("below turn floor must not flag: %v", got)
	}
}

func TestTrajectoryAnomaliesReeditedFile(t *testing.T) {
	tra := Trajectory{ReeditedFile: "main.go", ReeditedCount: 3}
	got := tra.Anomalies("fix")
	if len(got) != 1 || !stringsContains(got[0], "same-file-reedited") {
		t.Fatalf("want reedited-file flag, got %v", got)
	}
	tra.ReeditedCount = 2
	if got := tra.Anomalies("fix"); len(got) != 0 {
		t.Fatalf("below reedit floor must not flag: %v", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || stringsContains(s, sub))
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
