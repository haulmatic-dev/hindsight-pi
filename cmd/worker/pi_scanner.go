package main

import (
	"bufio"
	"io"
)

// maxPiEventLineBytes bounds one RPC event line from pi. Tool results carry
// full file reads and test-runner output as a single JSON line; 71,055 bytes
// was observed in production (srv-smart-engine-z4t) and jest output can be
// far larger. bufio's 64KB default silently kills the scanner mid-turn, which
// freezes the activity monitor and gets productive workers watchdog-killed.
const maxPiEventLineBytes = 8 << 20 // 8 MiB

// newPiScanner returns a line scanner sized for pi's RPC stream. Never use
// bufio.NewScanner directly on pi pipes.
func newPiScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), maxPiEventLineBytes)
	return sc
}
