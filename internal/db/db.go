// Package db provides a shared SQLite opener that applies WAL mode and busy
// timeout so concurrent writers retry instead of immediately returning SQLITE_BUSY.
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the SQLite database at path with:
//   - WAL journal mode  — allows concurrent reads alongside a single writer
//   - busy_timeout=5000 — writers wait up to 5 s before returning SQLITE_BUSY
//   - synchronous=NORMAL — safe for WAL; faster than FULL
//
// All packages that write to the shared hijarr.db must use this function
// instead of calling sql.Open("sqlite", ...) directly.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA synchronous=NORMAL`,
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma failed (%s): %w", p, err)
		}
	}
	return db, nil
}
