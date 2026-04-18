package maintenance

import (
	"database/sql"
	"fmt"
	"time"

	hijarrdb "hijarr/internal/db"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS applied_maintenance (
	id          TEXT PRIMARY KEY,
	applied_at  INTEGER NOT NULL DEFAULT 0,
	run_count   INTEGER NOT NULL DEFAULT 0
);`

// AppliedRecord represents a row in applied_maintenance.
type AppliedRecord struct {
	ID        string
	AppliedAt int64
	RunCount  int
}

// TaskStore persists maintenance task state in SQLite.
type TaskStore struct {
	db *sql.DB
}

// NewTaskStore opens (or reuses) the SQLite file at dbPath.
func NewTaskStore(dbPath string) (*TaskStore, error) {
	db, err := hijarrdb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("task store: open db: %w", err)
	}
	if _, err := db.Exec(schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("task store: create table: %w", err)
	}
	return &TaskStore{db: db}, nil
}

// IsApplied returns true if the task with the given ID has been
// successfully applied (applied_at > 0).
func (s *TaskStore) IsApplied(id string) (bool, error) {
	var appliedAt int64
	err := s.db.QueryRow(
		`SELECT applied_at FROM applied_maintenance WHERE id = ?`, id,
	).Scan(&appliedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return appliedAt > 0, nil
}

// MarkApplied records the task as successfully completed.
func (s *TaskStore) MarkApplied(id string) error {
	_, err := s.db.Exec(`
		INSERT INTO applied_maintenance (id, applied_at, run_count) VALUES (?, ?, 1)
		ON CONFLICT(id) DO UPDATE SET applied_at = excluded.applied_at, run_count = run_count + 1
	`, id, time.Now().Unix())
	return err
}

// ResetApplied removes the applied record.
func (s *TaskStore) ResetApplied(id string) error {
	_, err := s.db.Exec(`DELETE FROM applied_maintenance WHERE id = ?`, id)
	return err
}

// ListAll returns every row in applied_maintenance.
func (s *TaskStore) ListAll() ([]AppliedRecord, error) {
	rows, err := s.db.Query(`SELECT id, applied_at, run_count FROM applied_maintenance`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppliedRecord
	for rows.Next() {
		var r AppliedRecord
		if err := rows.Scan(&r.ID, &r.AppliedAt, &r.RunCount); err == nil {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}
