package core

// Daemon-side tier-1 trajectory reader for Pan (see internal/pan/trajectory.go
// for the signal definitions). Parses a live worker's pi session jsonl under
// $HYDRA_HOME/sessions/<session-id>/ — the same files the worker's
// [SessionStats] emitter reads — into a pan.Trajectory. Fail-open by design:
// any IO or parse problem returns ok=false and the candidate class silently
// does not fire.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/pan"
)

// maxTrajectoryReadBytes caps per-file reads so a runaway session cannot
// balloon the Pan tick (mirrors cmd/worker maxSessionReadBytes).
const maxTrajectoryReadBytes = 50 * 1024 * 1024

// trajectoryEditTools are the pi tool names that mutate files.
var trajectoryEditTools = map[string]bool{
	"edit": true, "write": true, "multiedit": true,
	"str_replace": true, "str_replace_editor": true, "create_file": true,
}

// trajectoryTestMarkers identify a bash command that runs tests.
var trajectoryTestMarkers = []string{
	"go test", "jest", "vitest", "pytest", "cargo test", "npm run test",
	"npm test", "make test", "test:agent", "go vet",
}

type trajectoryAccum struct {
	turns          int
	testRuns       int
	editsSinceTest int
	editPaths      map[string]bool
	bashCounts     map[string]int
	sawAnyToolCall bool
	newFiles       int
	// perFileEdits: per-path edit counts since the most recent test run
	// (reset with editsSinceTest). Max entry is the reedited-file signal.
	perFileEdits map[string]int
}

func newTrajectoryAccum() trajectoryAccum {
	return trajectoryAccum{editPaths: map[string]bool{}, bashCounts: map[string]int{}, perFileEdits: map[string]int{}}
}

// collectWorkerTrajectory builds the pan.Trajectory for a bead from its pi
// session directory. ok=false when no session files exist or parsing fails.
func (o *Overseer) collectWorkerTrajectory(beadID string) (pan.Trajectory, bool) {
	dir := PiSessionDir(beadID, os.Getenv("HYDRA_HOME"))
	if dir == "" {
		return pan.Trajectory{}, false
	}
	return gatherWorkerTrajectory(dir)
}

// gatherWorkerTrajectory is the pure-filesystem core (testable without an
// Overseer).
func gatherWorkerTrajectory(dir string) (pan.Trajectory, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return pan.Trajectory{}, false
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return pan.Trajectory{}, false
	}

	acc := newTrajectoryAccum()
	for _, path := range files {
		if err := accumulateTrajectoryFile(&acc, path); err != nil {
			return pan.Trajectory{}, false
		}
	}
	if !acc.sawAnyToolCall && acc.turns == 0 {
		return pan.Trajectory{}, false
	}

	// Most repeated normalized bash command and most-reedited file.
	cmd, count := "", 0
	for c, n := range acc.bashCounts {
		if n > count {
			cmd, count = c, n
		}
	}
	file, fcount := "", 0
	for f, n := range acc.perFileEdits {
		if n > fcount {
			file, fcount = f, n
		}
	}

	return pan.Trajectory{
		Turns:           acc.turns,
		TestRuns:        acc.testRuns,
		EditsSinceTest:  acc.editsSinceTest,
		DistinctEdits:   len(acc.editPaths),
		RepeatedCommand: cmd,
		RepeatedCount:   count,
		NewFiles:        acc.newFiles,
		ReeditedFile:    file,
		ReeditedCount:   fcount,
	}, true
}

func accumulateTrajectoryFile(acc *trajectoryAccum, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxTrajectoryReadBytes)
	if info.Size() > maxTrajectoryReadBytes {
		// Skip ahead: only the recent tail matters for loop detection.
		if _, err := f.Seek(info.Size()-maxTrajectoryReadBytes, 0); err != nil {
			return err
		}
		scanner.Scan() // discard the partial line
	}
	for scanner.Scan() {
		accumulateTrajectoryLine(acc, scanner.Bytes())
	}
	return scanner.Err()
}

// accumulateTrajectoryLine parses one session event. Malformed lines are
// skipped: session jsonl is the worker's file, fail-open.
func accumulateTrajectoryLine(acc *trajectoryAccum, line []byte) {
	if len(line) == 0 {
		return
	}
	var root struct {
		Type string `json:"type"`
		Name string `json:"name"`
		Args struct {
			Command  string `json:"command"`
			FilePath string `json:"file_path"`
			Path     string `json:"path"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(line, &root); err != nil {
		return
	}
	switch root.Type {
	case "turn_end":
		acc.turns++
	case "toolCall":
		acc.sawAnyToolCall = true
		switch {
		case root.Name == "bash":
			cmd := normalizeTrajectoryCommand(root.Args.Command)
			if cmd == "" {
				return
			}
			if isTrajectoryTestRun(cmd) {
				acc.testRuns++
				acc.editsSinceTest = 0
				acc.perFileEdits = map[string]int{}
				return
			}
			acc.bashCounts[cmd]++
		case trajectoryEditTools[root.Name]:
			p := root.Args.FilePath
			if p == "" {
				p = root.Args.Path
			}
			if p != "" {
				acc.editPaths[p] = true
			}
			if root.Name == "write" || root.Name == "create_file" {
				acc.newFiles++
			}
			acc.editsSinceTest++
			if p != "" {
				acc.perFileEdits[p]++
			}
		}
	}
}

func normalizeTrajectoryCommand(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func isTrajectoryTestRun(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, m := range trajectoryTestMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
