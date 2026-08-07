// Package registry is the daemon-owned single-writer store for worker state
// and per-worker commands (PRD: worker-park-and-resume-20260731, Part 4).
// It replaces workers.json: workers report status over the daemon API, the
// daemon writes here, and Charon, the scheduler and Pan read one truth.
package registry

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// WorkerState is the lifecycle state of a registered worker.
type WorkerState string

const (
	StateWorking   WorkerState = "working"
	StateParked    WorkerState = "parked"
	StateHung      WorkerState = "hung"
	StateCompleted WorkerState = "completed"
)

// Terminal reports whether the worker has left the fleet's responsibility.
func (s WorkerState) Terminal() bool { return s == StateCompleted }

// CommandKind identifies a queued instruction for a worker.
type CommandKind string

const (
	CmdContinue           CommandKind = "continue"
	CmdSteer              CommandKind = "steer"
	CmdCompactAndContinue CommandKind = "compact-and-continue"
	CmdAbandonWithSummary CommandKind = "abandon-with-summary"
)

// WorkerRecord is one row of fleet truth.
type WorkerRecord struct {
	BeadID        string
	PID           int
	PGID          int
	Project       string
	StartedAt     time.Time
	LastHeartbeat time.Time
	State         WorkerState
	PiSessionID   string
	ErrStatus     int
	ErrType       string
	ErrMessage    string
	TokensSoFar   int64
	ParkSince     time.Time
	ParkWakeAt    time.Time
	ParkReason    string
	NudgeCount    int
	ExitCode      int
	CompletedAt   time.Time
}

// Command is one queued instruction for a worker.
type Command struct {
	ID         int64
	BeadID     string
	Kind       CommandKind
	Payload    string
	CreatedAt  time.Time
	ConsumedAt time.Time // zero = pending
}

// Pending reports whether the command is still waiting for the worker.
func (c Command) Pending() bool { return c.ConsumedAt.IsZero() }

// Store wraps the SQLite registry. A single *Store is safe for concurrent
// use; the daemon is the only writer by contract.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS workers (
  bead_id        TEXT PRIMARY KEY,
  pid            INTEGER NOT NULL,
  pgid           INTEGER NOT NULL,
  project        TEXT NOT NULL DEFAULT '',
  started_at     TEXT NOT NULL,
  last_heartbeat TEXT NOT NULL,
  state          TEXT NOT NULL,
  pi_session_id  TEXT NOT NULL DEFAULT '',
  err_status     INTEGER NOT NULL DEFAULT 0,
  err_type       TEXT NOT NULL DEFAULT '',
  err_message    TEXT NOT NULL DEFAULT '',
  tokens_so_far  INTEGER NOT NULL DEFAULT 0,
  park_since     TEXT,
  park_wake_at   TEXT,
  park_reason    TEXT NOT NULL DEFAULT '',
  nudge_count    INTEGER NOT NULL DEFAULT 0,
  exit_code      INTEGER NOT NULL DEFAULT 0,
  completed_at   TEXT
);
CREATE TABLE IF NOT EXISTS commands (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  bead_id     TEXT NOT NULL,
  kind        TEXT NOT NULL,
  payload     TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  consumed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_commands_bead_pending ON commands (bead_id) WHERE consumed_at IS NULL;
`

const timeFmt = time.RFC3339Nano

// Open opens (creating if needed) the registry at path. ":memory:" works for
// tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("registry open: %w", err)
	}
	// Single connection: SQLite is a single-writer store, and the daemon is
	// the single writer by contract. This serializes all access.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("registry schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func marshalTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(timeFmt)
}

func scanTime(v interface{}) time.Time {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(timeFmt, s)
	return t
}

// UpsertWorker inserts or replaces a worker record. Completed records are
// never resurrected: an upsert for a completed bead keeps it completed.
// A PID change means a NEW worker instance for the same bead; shepherd
// commands queued against the previous instance are void and cleared
// (they were judged on the old row's heartbeat/tokens — live incident
// 2026-08-07: srv-smart-engine-pes self-aborted on its predecessor's
// abandon-with-summary 2m20s into a healthy turn).
func (s *Store) UpsertWorker(rec WorkerRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin upsert worker %s: %w", rec.BeadID, err)
	}
	defer func() { _ = tx.Rollback() }()

	var prevPID int
	_ = tx.QueryRow(`SELECT pid FROM workers WHERE bead_id=?`, rec.BeadID).Scan(&prevPID)

	_, err = tx.Exec(`
INSERT INTO workers (bead_id, pid, pgid, project, started_at, last_heartbeat,
  state, pi_session_id, err_status, err_type, err_message, tokens_so_far,
  park_since, park_wake_at, park_reason, nudge_count, exit_code, completed_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(bead_id) DO UPDATE SET
  pid=excluded.pid, pgid=excluded.pgid, project=excluded.project,
  started_at=excluded.started_at, last_heartbeat=excluded.last_heartbeat,
  state=CASE WHEN workers.state='completed' THEN workers.state ELSE excluded.state END,
  pi_session_id=excluded.pi_session_id, err_status=excluded.err_status,
  err_type=excluded.err_type, err_message=excluded.err_message,
  tokens_so_far=excluded.tokens_so_far, park_since=excluded.park_since,
  park_wake_at=excluded.park_wake_at, park_reason=excluded.park_reason,
  nudge_count=excluded.nudge_count, exit_code=excluded.exit_code,
  completed_at=excluded.completed_at`,
		rec.BeadID, rec.PID, rec.PGID, rec.Project,
		marshalTime(rec.StartedAt), marshalTime(rec.LastHeartbeat),
		string(rec.State), rec.PiSessionID, rec.ErrStatus, rec.ErrType,
		rec.ErrMessage, rec.TokensSoFar, marshalTime(rec.ParkSince),
		marshalTime(rec.ParkWakeAt), rec.ParkReason, rec.NudgeCount,
		rec.ExitCode, marshalTime(rec.CompletedAt))
	if err != nil {
		return fmt.Errorf("upsert worker %s: %w", rec.BeadID, err)
	}

	if prevPID != 0 && prevPID != rec.PID {
		if _, err := tx.Exec(`DELETE FROM commands WHERE bead_id=? AND consumed_at IS NULL`, rec.BeadID); err != nil {
			return fmt.Errorf("clear stale commands for %s: %w", rec.BeadID, err)
		}
	}
	return tx.Commit()
}

// Heartbeat updates liveness and token progress for a worker.
func (s *Store) Heartbeat(beadID string, tokens int64) error {
	res, err := s.db.Exec(
		`UPDATE workers SET last_heartbeat=?, tokens_so_far=? WHERE bead_id=? AND state != 'completed'`,
		time.Now().UTC().Format(timeFmt), tokens, beadID)
	if err != nil {
		return fmt.Errorf("heartbeat %s: %w", beadID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("heartbeat %s: no live worker", beadID)
	}
	return nil
}

// SetState transitions a worker's state. Park fields are set only when
// entering StateParked.
func (s *Store) SetState(beadID string, state WorkerState) error {
	res, err := s.db.Exec(
		`UPDATE workers SET state=?, last_heartbeat=? WHERE bead_id=? AND state != 'completed'`,
		string(state), time.Now().UTC().Format(timeFmt), beadID)
	if err != nil {
		return fmt.Errorf("set state %s→%s: %w", beadID, state, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set state %s→%s: no live worker", beadID, state)
	}
	return nil
}

// MarkCompleted moves a worker to the terminal state.
func (s *Store) MarkCompleted(beadID string, exitCode int) error {
	_, err := s.db.Exec(
		`UPDATE workers SET state='completed', exit_code=?, completed_at=? WHERE bead_id=?`,
		exitCode, time.Now().UTC().Format(timeFmt), beadID)
	if err != nil {
		return fmt.Errorf("mark completed %s: %w", beadID, err)
	}
	return nil
}

// ReapStale deletes worker registry rows that are terminal (completed) or
// whose last heartbeat is older than cutoff, plus any queued commands for
// those beads. It returns the number of worker rows removed.
func (s *Store) ReapStale(cutoff time.Time) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("reap begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT bead_id FROM workers WHERE state=? OR last_heartbeat < ?`,
		StateCompleted, cutoff.Format(timeFmt))
	if err != nil {
		return 0, fmt.Errorf("reap select: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("reap scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("reap rows: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")

	if _, err := tx.Exec(
		`DELETE FROM commands WHERE bead_id IN (`+inClause+`)`,
		args...); err != nil {
		return 0, fmt.Errorf("reap commands: %w", err)
	}

	res, err := tx.Exec(
		`DELETE FROM workers WHERE bead_id IN (`+inClause+`)`,
		args...)
	if err != nil {
		return 0, fmt.Errorf("reap workers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("reap commit: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

const workerCols = `bead_id, pid, pgid, project, started_at, last_heartbeat,
  state, pi_session_id, err_status, err_type, err_message, tokens_so_far,
  park_since, park_wake_at, park_reason, nudge_count, exit_code, completed_at`

func scanWorker(row *sql.Rows) (WorkerRecord, error) {
	var rec WorkerRecord
	var started, heartbeat, parkSince, parkWake, completed interface{}
	var state string
	err := row.Scan(&rec.BeadID, &rec.PID, &rec.PGID, &rec.Project,
		&started, &heartbeat, &state, &rec.PiSessionID, &rec.ErrStatus,
		&rec.ErrType, &rec.ErrMessage, &rec.TokensSoFar, &parkSince,
		&parkWake, &rec.ParkReason, &rec.NudgeCount, &rec.ExitCode, &completed)
	if err != nil {
		return rec, err
	}
	rec.State = WorkerState(state)
	rec.StartedAt = scanTime(started)
	rec.LastHeartbeat = scanTime(heartbeat)
	rec.ParkSince = scanTime(parkSince)
	rec.ParkWakeAt = scanTime(parkWake)
	rec.CompletedAt = scanTime(completed)
	return rec, nil
}

// ListWorkers returns workers, optionally filtered to the given states.
// No filter returns all non-completed workers.
func (s *Store) ListWorkers(states ...WorkerState) ([]WorkerRecord, error) {
	query := `SELECT ` + workerCols + ` FROM workers`
	args := []interface{}{}
	if len(states) > 0 {
		query += ` WHERE state IN (`
		for i, st := range states {
			if i > 0 {
				query += `,`
			}
			query += `?`
			args = append(args, string(st))
		}
		query += `)`
	} else {
		query += ` WHERE state != 'completed'`
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
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

// GetWorker returns one worker by bead ID.
func (s *Store) GetWorker(beadID string) (WorkerRecord, bool, error) {
	rows, err := s.db.Query(`SELECT `+workerCols+` FROM workers WHERE bead_id=?`, beadID)
	if err != nil {
		return WorkerRecord{}, false, fmt.Errorf("get worker %s: %w", beadID, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return WorkerRecord{}, false, nil
	}
	rec, err := scanWorker(rows)
	if err != nil {
		return WorkerRecord{}, false, fmt.Errorf("scan worker %s: %w", beadID, err)
	}
	return rec, true, nil
}

// IncrementNudges bumps the nudge counter for a worker.
func (s *Store) IncrementNudges(beadID string) error {
	_, err := s.db.Exec(
		`UPDATE workers SET nudge_count=nudge_count+1 WHERE bead_id=? AND state != 'completed'`,
		beadID)
	if err != nil {
		return fmt.Errorf("increment nudges %s: %w", beadID, err)
	}
	return nil
}

// EnqueueCommand queues an instruction for a worker.
func (s *Store) EnqueueCommand(beadID string, kind CommandKind, payload string) error {
	_, err := s.db.Exec(
		`INSERT INTO commands (bead_id, kind, payload, created_at) VALUES (?,?,?,?)`,
		beadID, string(kind), payload, time.Now().UTC().Format(timeFmt))
	if err != nil {
		return fmt.Errorf("enqueue %s for %s: %w", kind, beadID, err)
	}
	return nil
}

// PendingCommands returns unconsumed commands for a worker, oldest first.
func (s *Store) PendingCommands(beadID string) ([]Command, error) {
	rows, err := s.db.Query(
		`SELECT id, bead_id, kind, payload, created_at, consumed_at
		 FROM commands WHERE bead_id=? AND consumed_at IS NULL ORDER BY id`, beadID)
	if err != nil {
		return nil, fmt.Errorf("pending commands %s: %w", beadID, err)
	}
	defer rows.Close()

	var out []Command
	for rows.Next() {
		var c Command
		var created, consumed interface{}
		var kind string
		if err := rows.Scan(&c.ID, &c.BeadID, &kind, &c.Payload, &created, &consumed); err != nil {
			return nil, fmt.Errorf("scan command: %w", err)
		}
		c.Kind = CommandKind(kind)
		c.CreatedAt = scanTime(created)
		c.ConsumedAt = scanTime(consumed)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConsumeCommand marks a command delivered to the worker.
func (s *Store) ConsumeCommand(id int64) error {
	res, err := s.db.Exec(
		`UPDATE commands SET consumed_at=? WHERE id=? AND consumed_at IS NULL`,
		time.Now().UTC().Format(timeFmt), id)
	if err != nil {
		return fmt.Errorf("consume command %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("consume command %d: already consumed or missing", id)
	}
	return nil
}
