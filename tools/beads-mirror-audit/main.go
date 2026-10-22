package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/mirroraudit"
)

// Divergence is one bead whose jsonl status disagrees with the br database.
type Divergence struct {
	BeadID      string `json:"bead_id"`
	JSONLStatus string `json:"jsonl"`
	DBStatus    string `json:"db"`
}

// Report is the machine-readable audit summary (--json).
type Report struct {
	Divergences []Divergence `json:"divergences"`
	Checked     int          `json:"checked"`
}

const (
	exitClean   = 0
	exitDrift   = 1
	exitUnavail = 2
)

const auditUnavailablePrefix = "audit unavailable: "

const enforceEnv = "BEADS_MIRROR_AUDIT_ENFORCE"

const auditImplemented = true

// brRunner is the seam tests overwrite to stub the br CLI.
var brRunner = execBr

// execBr runs the real br binary in storeDir.
func execBr(dir string, args []string) (string, error) {
	path, err := exec.LookPath("br")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// comparer adapts the tool's brRunner seam to mirroraudit.Comparer.
type comparer struct{}

func (comparer) BrListJSON(storeDir string) (string, error) {
	return brRunner(storeDir, []string{"list", "--json"})
}

// runAudit compares the jsonl mirror against the br database for storeDir.
// Single source of comparison truth is mirroraudit; this tool maps its
// report to the CLI output shape. Returns the report, the rendered output
// line(s), and the process exit code.
func runAudit(storeDir string, jsonOut bool) (Report, string, int) {
	shared, err := mirroraudit.Audit(storeDir, comparer{})
	if err != nil {
		return Report{}, auditUnavailablePrefix + err.Error(), exitUnavail
	}

	rep := Report{Divergences: []Divergence{}, Checked: shared.Checked}
	var lines []string
	for _, rec := range shared.Records {
		if rec.Class == mirroraudit.ClassClean {
			continue
		}
		d := Divergence{BeadID: rec.BeadID, JSONLStatus: rec.JSONLStatus, DBStatus: rec.DBStatus}
		rep.Divergences = append(rep.Divergences, d)
		lines = append(lines, fmt.Sprintf("%s jsonl=%s db=%s", d.BeadID, d.JSONLStatus, d.DBStatus))
	}

	if jsonOut {
		b, mErr := json.Marshal(rep)
		if mErr != nil {
			return rep, auditUnavailablePrefix + mErr.Error(), exitUnavail
		}
		return rep, string(b), exitCodeFor(rep)
	}
	if len(lines) == 0 {
		return rep, fmt.Sprintf("mirror clean (%d beads)", shared.Checked), exitClean
	}
	return rep, strings.Join(lines, "\n"), exitDrift
}

// exitCodeFor maps a report to its contract exit code: 0 clean, 1 drift.
func exitCodeFor(rep Report) int {
	if len(rep.Divergences) == 0 {
		return exitClean
	}
	return exitDrift
}

func main() {
	store := flag.String("store", "", "bead store directory containing issues.jsonl")
	jsonOut := flag.Bool("json", false, "emit machine-readable JSON report")
	flag.Parse()

	if *store == "" {
		fmt.Fprintln(os.Stderr, "beads-mirror-audit: --store <dir> is required")
		os.Exit(exitUnavail)
	}
	if _, err := os.Stat(*store); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, auditUnavailablePrefix+err.Error())
		os.Exit(exitUnavail)
	}

	_, output, code := runAudit(*store, *jsonOut)
	if code == exitUnavail {
		fmt.Fprintln(os.Stderr, output)
	} else {
		fmt.Println(output)
	}
	os.Exit(code)
}
