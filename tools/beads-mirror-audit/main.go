// beads-mirror-audit compares a br bead store's issues.jsonl mirror against
// the live store reported by `br`, printing one line per divergence.
//
// Usage:
//
//	go run ./tools/beads-mirror-audit --store <dir> [--json]
//
// Exit codes:
//
//	0 - mirror clean (N beads)
//	1 - divergences found (see output)
//	2 - audit unavailable (br missing or br failure, or store unreadable) —
//	fail-open reporting: never blocks, never mutates either store.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Divergence is one bead whose status differs between the jsonl mirror
// and the live store.
type Divergence struct {
	BeadID      string `json:"bead_id"`
	JSONLStatus string `json:"jsonl"`
	DBStatus    string `json:"db"`
}

// Report is the machine-readable audit result (--json shape).
type Report struct {
	Divergences []Divergence `json:"divergences"`
	Checked     int          `json:"checked"`
}

// exit codes per the contract.
const (
	exitClean   = 0
	exitDrift   = 1
	exitUnavail = 2
)

// auditUnavailablePrefix is the fail-open reporting line printed on br
// absence/failure.
const auditUnavailablePrefix = "audit unavailable: "

// enforceEnv gates the GREEN-behavior contract tests; set to 1 to fail
// the suite when the implementation is still missing (RED discipline).
const enforceEnv = "BEADS_MIRROR_AUDIT_ENFORCE"

// auditImplemented gates the RED-phase skip in the behavior tests; the
// tool is implemented, so tests no longer skip.
const auditImplemented = true

// brRunner invokes `br` inside storeDir and returns its stdout. The
// injected seam exists for tests only; production uses execBr.
var brRunner = execBr

// execBr is the production runner: searches PATH for br with the store as
// cwd, captures combined stdout, and reports LookPath / Wait errors as-is.
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

// parseJSONLLine extracts an id and status from one issues.jsonl line.
// Real stores emit JSON objects; the fixture format uses "id status"
// tokens. Bare non-JSON lines fall back to whitespace split.
func parseJSONLLine(line string) (id, status string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	var rec struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(line), &rec); err == nil && rec.ID != "" {
		return rec.ID, rec.Status, true
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// parseDBList extracts id/status pairs from `br list --json` output
// (a JSON array of objects carrying id and status).
func parseDBList(out string) ([]Divergence, error) {
	var recs []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &recs); err != nil {
		return nil, err
	}
	var db []Divergence
	for _, r := range recs {
		db = append(db, Divergence{BeadID: r.ID, DBStatus: r.Status})
	}
	return db, nil
}

// readJSONLMirror reads <storeDir>/issues.jsonl and returns checked count
// plus the id→status mirror. Malformed lines are skipped.
func readJSONLMirror(storeDir string) (mirror map[string]string, checked int, err error) {
	data, err := os.ReadFile(filepath.Join(storeDir, "issues.jsonl"))
	if err != nil {
		return nil, 0, err
	}
	mirror = make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		id, status, ok := parseJSONLLine(line)
		if !ok {
			continue
		}
		mirror[id] = status
		checked++
	}
	return mirror, checked, nil
}

// runAudit compares <storeDir>/issues.jsonl against the live store via br
// and returns the report plus the process exit code
// (0 clean / 1 drift / 2 audit-unavailable).
func runAudit(storeDir string, jsonOut bool) (Report, string, int) {
	mirror, checked, err := readJSONLMirror(storeDir)
	if err != nil {
		return Report{}, auditUnavailablePrefix + err.Error(), exitUnavail
	}

	out, err := brRunner(storeDir, []string{"list", "--json"})
	if err != nil {
		return Report{}, auditUnavailablePrefix + err.Error(), exitUnavail
	}
	db, err := parseDBList(out)
	if err != nil {
		return Report{}, auditUnavailablePrefix + err.Error(), exitUnavail
	}

	rep := Report{Divergences: []Divergence{}, Checked: checked}
	var lines []string
	for _, d := range db {
		jsonlStatus, ok := mirror[d.BeadID]
		if !ok {
			continue
		}
		if jsonlStatus != d.DBStatus {
			div := Divergence{BeadID: d.BeadID, JSONLStatus: jsonlStatus, DBStatus: d.DBStatus}
			rep.Divergences = append(rep.Divergences, div)
			lines = append(lines, fmt.Sprintf("%s jsonl=%s db=%s", div.BeadID, div.JSONLStatus, div.DBStatus))
		}
	}
	// Beads present in the mirror but absent from the live store listing
	// also count as drift: jsonl status vs db=<missing>.
	for id, jsonlStatus := range mirror {
		found := false
		for _, d := range db {
			if d.BeadID == id {
				found = true
				break
			}
		}
		if !found {
			div := Divergence{BeadID: id, JSONLStatus: jsonlStatus, DBStatus: ""}
			rep.Divergences = append(rep.Divergences, div)
			lines = append(lines, fmt.Sprintf("%s jsonl=%s db=%s", id, jsonlStatus, div.DBStatus))
		}
	}

	if jsonOut {
		b, mErr := json.Marshal(rep)
		if mErr != nil {
			return rep, auditUnavailablePrefix + mErr.Error(), exitUnavail
		}
		return rep, string(b), exitCodeFor(rep)
	}
	if len(lines) == 0 {
		return rep, fmt.Sprintf("mirror clean (%d beads)", checked), exitClean
	}
	return rep, strings.Join(lines, "\n"), exitDrift
}

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

	rep, line, code := runAudit(*store, *jsonOut)
	if code == exitClean && !*jsonOut {
		fmt.Println(line)
	} else if code == exitDrift {
		fmt.Println(line)
	} else if code == exitUnavail {
		fmt.Fprintln(os.Stderr, line)
	} else {
		// jsonOut clean case: still print the JSON report.
		fmt.Println(line)
	}
	_ = rep
	os.Exit(code)
}
