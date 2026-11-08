package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Daily usage rollup: token/turn spend trends must survive worker-row
// pruning (ReapStale prunes completed rows after 24h, ReapGhosts removes
// dead workers), so every token-bearing row is additively merged into the
// durable token_usage_daily table before it disappears. Merge is additive
// for sums/counts, max for the max, and appends per-bead token values so a
// median can be recomputed exactly. Fail-open by construction: callers log
// and continue on error.
type DailyUsageRollup struct {
	Day            string  // YYYY-MM-DD UTC (the row's started-at day)
	Project        string  // project name from the worker row
	Completions    int     // completed worker rows merged in
	TokensFresh    int64   // sum of tokens_fresh
	TokensOutput   int64   // sum of tokens_output
	TokensCache    int64   // sum of tokens_cache_read + tokens_cache_creation
	MedianPerBead  float64 // median of per-bead tokens_so_far values
	MaxPerBead     int64   // max tokens_so_far
	RerunCount     int     // worker re-registrations over a completed row
	ZeroTokenCount int     // completed rows with tokens_so_far == 0
	BeadTokensList string  // JSON array of per-bead tokens_so_far values
}

const dailyUsageTable = `
CREATE TABLE IF NOT EXISTS token_usage_daily (
  day          TEXT NOT NULL,
  project      TEXT NOT NULL DEFAULT '',
  completions  INTEGER NOT NULL DEFAULT 0,
  tokens_fresh INTEGER NOT NULL DEFAULT 0,
  tokens_output INTEGER NOT NULL DEFAULT 0,
  tokens_cache INTEGER NOT NULL DEFAULT 0,
  max_per_bead INTEGER NOT NULL DEFAULT 0,
  rerun_count  INTEGER NOT NULL DEFAULT 0,
  zero_token_count INTEGER NOT NULL DEFAULT 0,
  bead_tokens  TEXT NOT NULL DEFAULT '[]',
  recorded_at  TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (day, project)
);
`

func ensureDailyUsageTable(db *sql.DB) error {
	if _, err := db.Exec(dailyUsageTable); err != nil {
		return fmt.Errorf("token_usage_daily schema: %w", err)
	}
	return nil
}

// execer abstracts *sql.DB and *sql.Tx (same Exec surface).
type execer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// mergeWorkerUsage additively merges one worker record into the daily rollup
// for (day, project). rerun>0 records a re-registration over a completed row.
func mergeWorkerUsage(tx execer, day string, rec WorkerRecord, rerun int) error {
	if _, err := tx.Exec(`INSERT OR IGNORE INTO token_usage_daily (day, project) VALUES (?, ?)`, day, rec.Project); err != nil {
		return fmt.Errorf("rollup insert %s/%s: %w", day, rec.Project, err)
	}
	var beadTokens string
	if err := tx.QueryRow(`SELECT bead_tokens FROM token_usage_daily WHERE day=? AND project=?`, day, rec.Project).Scan(&beadTokens); err != nil {
		return fmt.Errorf("rollup read %s/%s: %w", day, rec.Project, err)
	}
	values := []int64{}
	if beadTokens != "" && beadTokens != "[]" {
		if err := json.Unmarshal([]byte(beadTokens), &values); err != nil {
			values = []int64{} // unparseable history loses the list, not the sums
		}
	}
	if rec.TokensSoFar > 0 {
		values = append(values, rec.TokensSoFar)
	}
	newList, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("rollup marshal %s/%s: %w", day, rec.Project, err)
	}
	completions := 0
	zeroToken := 0
	if rec.State == StateCompleted {
		completions = 1
		if rec.TokensSoFar == 0 {
			zeroToken = 1
		}
	}
	if _, err := tx.Exec(`
UPDATE token_usage_daily SET
  completions = completions + ?,
  tokens_fresh = tokens_fresh + ?,
  tokens_output = tokens_output + ?,
  tokens_cache = tokens_cache + ?,
  max_per_bead = MAX(max_per_bead, ?),
  rerun_count = rerun_count + ?,
  zero_token_count = zero_token_count + ?,
  bead_tokens = ?,
  recorded_at = ?
WHERE day=? AND project=?`,
		completions, rec.TokensFresh, rec.TokensOutput,
		rec.TokensCacheRead+rec.TokensCacheCreation, rec.TokensSoFar,
		rerun, zeroToken, string(newList),
		time.Now().UTC().Format(timeFmt), day, rec.Project); err != nil {
		return fmt.Errorf("rollup merge %s/%s: %w", day, rec.Project, err)
	}
	return nil
}

// SnapshotAndRollup merges every current worker row into the daily table
// (keyed by its started-at UTC day) and returns the rollup rows touched.
// Called by the 00:05 UTC job; also the only path that adds rows still
// living in the workers table to the durable record.
func (s *Store) SnapshotAndRollup() ([]DailyUsageRollup, error) {
	recs, err := s.listAllWorkers()
	if err != nil {
		return nil, err
	}
	touched := map[string]DailyUsageRollup{}
	for _, rec := range recs {
		day := rec.StartedAt.UTC().Format("2006-01-02")
		if day == "" {
			day = time.Now().UTC().Format("2006-01-02")
		}
		if err := mergeWorkerUsage(s.db, day, rec, 0); err != nil {
			return nil, err
		}
		key := day + "|" + rec.Project
		touched[key] = DailyUsageRollup{Day: day, Project: rec.Project}
	}
	out := make([]DailyUsageRollup, 0, len(touched))
	for k := range touched {
		row, ok, err := s.GetDailyRollup(touched[k].Day, touched[k].Project)
		if err == nil && ok {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day+out[i].Project < out[j].Day+out[j].Project })
	return out, nil
}

func (s *Store) listAllWorkers() ([]WorkerRecord, error) {
	rows, err := s.db.Query(`SELECT ` + workerCols + ` FROM workers`)
	if err != nil {
		return nil, fmt.Errorf("list all workers: %w", err)
	}
	defer rows.Close()
	var out []WorkerRecord
	for rows.Next() {
		rec, err := scanWorker(rows)
		if err != nil {
			return nil, fmt.Errorf("scan worker: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetDailyRollup returns the durable daily row for (day, project), with the
// median recomputed from the stored per-bead token list.
func (s *Store) GetDailyRollup(day, project string) (DailyUsageRollup, bool, error) {
	var r DailyUsageRollup
	var beadTokens string
	err := s.db.QueryRow(`SELECT day, project, completions, tokens_fresh, tokens_output,
		tokens_cache, max_per_bead, rerun_count, zero_token_count, bead_tokens
		FROM token_usage_daily WHERE day=? AND project=?`, day, project).
		Scan(&r.Day, &r.Project, &r.Completions, &r.TokensFresh, &r.TokensOutput,
			&r.TokensCache, &r.MaxPerBead, &r.RerunCount, &r.ZeroTokenCount, &beadTokens)
	if err == sql.ErrNoRows {
		return DailyUsageRollup{}, false, nil
	}
	if err != nil {
		return DailyUsageRollup{}, false, fmt.Errorf("get daily rollup %s/%s: %w", day, project, err)
	}
	values := parseBeadTokens(beadTokens)
	r.MedianPerBead = medianInt64(values)
	return r, true, nil
}

func parseBeadTokens(s string) []int64 {
	if s == "" {
		return nil
	}
	var out []int64
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func medianInt64(v []int64) float64 {
	if len(v) == 0 {
		return 0
	}
	sorted := make([]int64, len(v))
	copy(sorted, v)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := len(sorted)
	if n%2 == 1 {
		return float64(sorted[n/2])
	}
	return float64(sorted[n/2-1]+sorted[n/2]) / 2
}

// usageDay picks the UTC day a worker record belongs to.
func usageDay(rec WorkerRecord) string {
	day := rec.StartedAt.UTC().Format("2006-01-02")
	if day == "" {
		day = time.Now().UTC().Format("2006-01-02")
	}
	return day
}

// reapCandidates runs query on tx and returns the scanned worker records.
// On any error it returns nil — reaping then proceeds without persistence
// (fail-open: losing one rollup merge never blocks cleanup).
func reapCandidates(tx execer, query string, args ...interface{}) []WorkerRecord {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []WorkerRecord
	for rows.Next() {
		rec, err := scanWorker(rows)
		if err != nil {
			return nil
		}
		out = append(out, rec)
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}
