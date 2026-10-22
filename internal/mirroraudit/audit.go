// Package mirroraudit is the shared, single-source-of-truth audit for the
// beads issues.jsonl mirror versus the br database (FR-1). Both the daemon
// sweep (internal/core/pan_mirror_health.go) and the CLI
// (tools/beads-mirror-audit) must use this package so their classification
// can never drift.
//
// Classification contract per record:
//   - clean:         jsonl and db agree (same status, or absent from one side
//     is never clean for paired records — see below).
//   - db_ahead:      the db has a row the jsonl mirror lacks (db row never
//     imported to the mirror) — jsonl status is empty.
//   - jsonl_ahead:   the jsonl has a record the db lacks, OR the record's
//     jsonl status disagrees with the db (the mirror is ahead or stale).
package mirroraudit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Record is one classified mirror record.
type Record struct {
	BeadID      string `json:"id"`
	JSONLStatus string `json:"jsonl"` // "" = absent from mirror
	DBStatus    string `json:"db"`    // "" = absent from db
	Class       Class  `json:"class"`
}

// Class is the per-record classification.
type Class string

const (
	ClassClean      Class = "clean"
	ClassDBAhead    Class = "db_ahead"
	ClassJSONLAhead Class = "jsonl_ahead"
)

// Report is the audit result for one store.
type Report struct {
	StorePath string  `json:"store_path"`
	Checked   int     `json:"checked"`
	Records   []Record `json:"records"`
	Counts    Counts  `json:"counts"`
}

// Counts summarizes the report by class.
type Counts struct {
	Clean      int `json:"clean"`
	DBAhead    int `json:"db_ahead"`
	JSONLAhead int `json:"jsonl_ahead"`
}

// Drift reports whether the store has any non-clean record.
func (r Report) Drift() bool {
	return r.Counts.DBAhead > 0 || r.Counts.JSONLAhead > 0
}

// Comparer is the seam for shell-out in tests: it produces
// `br list --json`-shaped output for the store. Prod impl shells out.
type Comparer interface {
	BrListJSON(storeDir string) (string, error)
}

// comparer is the package default so storeless Audit calls still work.
type comparer struct{}

func (comparer) BrListJSON(storeDir string) (string, error) {
	path, err := execLookPath("br")
	if err != nil {
		return "", err
	}
	return execCommand(path, []string{"list", "--json", "--all", "--limit", "0"}, storeDir)
}

// Audit classifies each record in the jsonl mirror vs br db for storeDir,
// per the FR-1 contract. Fail-closed on parse errors: malformed jsonl lines
// are classified jsonl_ahead with their raw line as the id, never panic.
func Audit(storePath string, c Comparer) (Report, error) {
	if c == nil {
		c = comparer{}
	}
	var rep Report
	rep.StorePath = storePath

	data, err := os.ReadFile(filepath.Join(storePath, "issues.jsonl"))
	if err != nil {
		return rep, err
	}
	mirror := make(map[string]string)
	rep.Checked = 0
	for _, line := range strings.Split(string(data), "\n") {
		id, status, ok := parseJSONLLine(line)
		if !ok {
			continue
		}
		mirror[id] = status
		rep.Checked++
	}

	// Shell out / stubbed compare.
	out, err := c.BrListJSON(storePath)
	if err != nil {
		return rep, err
	}
	dbRecs, err := parseBrList(out)
	if err != nil {
		return rep, err
	}
	dbStatus := make(map[string]string, len(dbRecs))
	for _, r := range dbRecs {
		dbStatus[r.ID] = r.Status
	}

	// Paired records: compare statuses.
	for id, dbSt := range dbStatus {
		jsonlSt, ok := mirror[id]
		if !ok {
			rep.add(Record{BeadID: id, DBStatus: dbSt, Class: ClassDBAhead})
			continue
		}
		if jsonlSt == dbSt {
			rep.add(Record{BeadID: id, JSONLStatus: jsonlSt, DBStatus: dbSt, Class: ClassClean})
			continue
		}
		rep.add(Record{BeadID: id, JSONLStatus: jsonlSt, DBStatus: dbSt, Class: ClassJSONLAhead})
	}
	// Mirror-only records: never seen by br — jsonl_ahead.
	for id, jsonlSt := range mirror {
		if _, ok := dbStatus[id]; !ok {
			rep.add(Record{BeadID: id, JSONLStatus: jsonlSt, Class: ClassJSONLAhead})
		}
	}
	return rep, nil
}

type brRec struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// parseBrList accepts both `br list --json` shapes: a bare array of
// records or the envelope object {"issues":[...],"total":N} used by
// newer br versions.
func parseBrList(out string) ([]brRec, error) {
	var recs []brRec
	if err := json.Unmarshal([]byte(out), &recs); err == nil {
		return recs, nil
	}
	var env struct {
		Issues []brRec `json:"issues"`
	}
	json.Unmarshal([]byte(out), &env)
	if env.Issues == nil {
		return nil, fmt.Errorf("unrecognized br list --json shape")
	}
	return env.Issues, nil
}

func (r *Report) add(rec Record) {
	switch rec.Class {
	case ClassClean:
		r.Counts.Clean++
	case ClassDBAhead:
		r.Counts.DBAhead++
	case ClassJSONLAhead:
		r.Counts.JSONLAhead++
	}
	r.Records = append(r.Records, rec)
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
