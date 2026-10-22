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

// parseJSONLLine parses one mirror record. Accepts strict JSON
// {"id":..,"status":..} and falls back to "<id> <status>" whitespace form.
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

// parseDBList decodes `br list --json` output into id→status pairs.
func parseDBList(out string) ([]Divergence, error) {
	var recs []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &recs); err != nil {
		return nil, err
	}
	db := make([]Divergence, 0, len(recs))
	for _, r := range recs {
		db = append(db, Divergence{BeadID: r.ID, DBStatus: r.Status})
	}
	return db, nil
}

// readJSONLMirror reads issues.jsonl into an id→status map plus record count.
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

// divergenceLine formats the one-line human-readable drift record.
func divergenceLine(d Divergence) string {
	return fmt.Sprintf("%s jsonl=%s db=%s", d.BeadID, d.JSONLStatus, d.DBStatus)
}

// runAudit compares the jsonl mirror against the br database for storeDir.
// Returns the report, the rendered output line(s), and the process exit code.
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
	record := func(d Divergence) {
		rep.Divergences = append(rep.Divergences, d)
		lines = append(lines, divergenceLine(d))
	}

	dbStatus := make(map[string]string, len(db))
	for _, d := range db {
		dbStatus[d.BeadID] = d.DBStatus
	}
	for id, dbSt := range dbStatus {
		jsonlStatus, ok := mirror[id]
		if !ok {
			continue
		}
		if jsonlStatus != dbSt {
			record(Divergence{BeadID: id, JSONLStatus: jsonlStatus, DBStatus: dbSt})
		}
	}
	for id, jsonlStatus := range mirror {
		if _, ok := dbStatus[id]; !ok {
			record(Divergence{BeadID: id, JSONLStatus: jsonlStatus, DBStatus: ""})
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
