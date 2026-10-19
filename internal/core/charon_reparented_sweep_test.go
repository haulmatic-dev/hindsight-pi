package core

import (
	"errors"
	"strconv"
	"syscall"
	"testing"
)

// Process-hygiene (c): worker death reparents node/jest children to PID 1;
// Charon's sweep must kill the whole group of any .hydra-worktrees process
// whose PPID is 1, and never touch processes with a live parent, the daemon's
// own group, or rows it cannot parse.
func TestParseReparentedWorktreeProcs(t *testing.T) {
	table := `  PID  PPID  PGID COMMAND
   100     1   100 node /tmp/x/.hydra-worktrees/-opt-dev-tools-components-hydra/hydra-6xhn/node
   200   100   100 jest --workers 2            <- live parent: keep
   300     1   300 sh -c sleep 300 && grep hydra-worktrees-other   <- no worktree marker in args: keep
   400     1   400 node /srv/.hydra-worktrees/proj/node
   500     1   ??? garbage row: keep
   600     1   600 bash /opt/dev-tools/components/hydra/.beads`
	// os.Getpgrp() inside the test process is the test's own group — never
	// collides with the fake table above.
	got := parseReparentedWorktreeProcs(table)
	if len(got) != 2 || got[0] != 100 || got[1] != 400 {
		t.Fatalf("pgids = %v, want [100 400]", got)
	}
}

func TestSweepReparentedWorktreeProcs_KillsAndDedups(t *testing.T) {
	j := &Charon{}
	table := `   100     1   100 node .hydra-worktrees/a/node
   101     1   100 jest .hydra-worktrees/a/jest
   200     1   200 node .hydra-worktrees/b/node`
	var killed []int
	n := j.sweepReparentedWorktreeProcs(table, func(pgid int) { killed = append(killed, pgid) })
	if n != 2 || len(killed) != 2 || killed[0] != 100 || killed[1] != 200 {
		t.Fatalf("killed = %v (n=%d), want [100 200] (n=2) — group dedup required", killed, n)
	}
}

func TestSweepReparentedWorktreeProcs_DaemonGroupSkipped(t *testing.T) {
	j := &Charon{}
	own := syscallGetpgrpForTest()
	table := `   700     1   ` + itoa(own) + ` node .hydra-worktrees/own/node`
	if n := j.sweepReparentedWorktreeProcs(table, func(int) {}); n != 0 {
		t.Fatalf("killed own process group (n=%d) — daemon must never be swept", n)
	}
}

func TestSweepReparentedWorktreeProcs_PsFailureFailOpen(t *testing.T) {
	j := &Charon{psRunner: func() ([]byte, error) { return nil, errFakeSentinel }}
	if got := j.listProcsTable(); got != "" {
		t.Fatalf("psRunner error must fail open to empty table, got %q", got)
	}
	if n := j.sweepReparentedWorktreeProcs(j.listProcsTable(), func(int) {}); n != 0 {
		t.Fatalf("ps failure must sweep nothing (n=%d)", n)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// syscallGetpgrpForTest bridges syscall.Getpgrp for the daemon-group skip test.
func syscallGetpgrpForTest() int { return syscall.Getpgrp() }

var errFakeSentinel = errors.New("fake ps failure")
